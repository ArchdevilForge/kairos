package evaluation

import (
	"math"
	"testing"

	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestFullFrictionNetRAndBreakevenCost(t *testing.T) {
	// A "breakeven" stop exit at EntryPrice on an altcoin is charged taker fee + dynamic slippage
	// and realistically lands around -0.08R to -0.15R (Work Item 2 & 12).
	res := ComputeFullFrictionNetR(FullFrictionInput{
		Symbol:        "SOL/USDT:USDT",
		Direction:     types.CycleDirectionUp,
		EntryPrice:    100.0,
		ExitPrice:     100.0,
		StopPrice:     98.0, // 2% structural stop
		NotionalUSD:   10_000.0,
		EntryUnix:     1_700_000_100,
		ExitUnix:      1_700_000_500,
		IsStopExit:    true,
		ATR1mOpen:     0.25,
		ADV5mOpenUSD:  2_000_000.0,
		ATR1mClose:    0.25,
		ADV5mCloseUSD: 2_000_000.0,
	})
	if res.NetR >= -0.05 || res.NetR <= -0.20 {
		t.Fatalf("breakeven stop exit NetR=%.4f, expected in [-0.20, -0.05]", res.NetR)
	}
	if res.Initial1RPct <= 0.02 {
		t.Fatalf("Initial1RPct=%.6f must include pre-entry slippage buffer > 0.02", res.Initial1RPct)
	}
}

func TestOrthogonalHumanAlphaZeroResidual(t *testing.T) {
	// Multi-candidate wave cluster with accepted & rejected tickets.
	// Every accepted trade must satisfy NetR_actual = PoolMean_mech + SelectionAlpha + TimingAlpha + ManagementAlpha
	// with Residual == 0.000R (Work Item 13).
	w1 := 0.45
	w2 := -1.08
	w3 := -1.05
	rows := []TicketRow{
		{
			Ticket: types.DecisionTicket{
				ID: "t1", SessionID: "s1", Symbol: "SOL/USDT:USDT",
				Grade:     types.TicketGradeA,
				Direction: types.CycleDirectionUp, CreatedAt: 1_700_000_000,
				EntryTriggeredAt: 1_700_000_060,
				RiskPlan:         types.RiskPlan{EntryPrice: 100, StopPrice: 98, SuggestedNotional: 10_000},
			},
			Decision: types.DecisionAccepted,
			Outcome: storage.CounterfactualOutcome{
				TicketID: "t1", Symbol: "SOL/USDT:USDT", Decision: types.DecisionAccepted,
				ExitPrice: 103.5, PolicyWaveR: &w1,
				MechanicalR: 0.52, NetR: 1.42, Complete: true,
			},
			HasOut: true,
		},
		{
			Ticket: types.DecisionTicket{
				ID: "t2", SessionID: "s1", Symbol: "ARB/USDT:USDT",
				Grade:     types.TicketGradeB,
				Direction: types.CycleDirectionUp, CreatedAt: 1_700_001_200, // <24h -> same wave_cluster_id
				RiskPlan: types.RiskPlan{EntryPrice: 1.0, StopPrice: 0.98, SuggestedNotional: 10_000},
			},
			Decision: types.DecisionRejected,
			Outcome: storage.CounterfactualOutcome{
				TicketID: "t2", Symbol: "ARB/USDT:USDT", Decision: types.DecisionRejected,
				PolicyWaveR: &w2, MechanicalR: -1.08, NetR: -1.08, Complete: true,
			},
			HasOut: true,
		},
		{
			Ticket: types.DecisionTicket{
				ID: "t3", SessionID: "s2", Symbol: "BTC/USDT:USDT",
				Grade:     types.TicketGradeA,
				Direction: types.CycleDirectionUp, CreatedAt: 1_700_200_000, // >24h -> wave_cluster_0002
				EntryTriggeredAt: 1_700_200_060,
				RiskPlan:         types.RiskPlan{EntryPrice: 40_000, StopPrice: 39_600, SuggestedNotional: 25_000},
			},
			Decision: types.DecisionAccepted,
			Outcome: storage.CounterfactualOutcome{
				TicketID: "t3", Symbol: "BTC/USDT:USDT", Decision: types.DecisionAccepted,
				ExitPrice: 39_580, PolicyWaveR: &w3,
				MechanicalR: -1.05, NetR: -1.05, Complete: true,
				StopHitFirst: true, TouchedPresetStop: true,
			},
			HasOut: true,
		},
	}

	decomps := ComputeOrthogonalHumanAlpha(rows)
	if len(decomps) != 2 {
		t.Fatalf("len(decomps)=%d want 2 accepted trades", len(decomps))
	}
	for _, d := range decomps {
		sum := d.PoolMeanMech + d.SelectionAlpha + d.TimingAlpha + d.ManagementAlpha
		if math.Abs(d.NetRActual-sum) > 1e-9 || math.Abs(d.Residual) > 1e-9 {
			t.Fatalf("ticket %s orthogonal identity violated: actual=%.6f sum=%.6f residual=%.6f (%+v)",
				d.TicketID, d.NetRActual, sum, d.Residual, d)
		}
	}

	rep := Summarize(rows)
	if rep.MaxAbsIdentityResidual != 0 {
		t.Fatalf("MaxAbsIdentityResidual=%v want 0.000R", rep.MaxAbsIdentityResidual)
	}
}
