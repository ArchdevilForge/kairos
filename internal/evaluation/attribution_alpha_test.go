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

func TestSimulateHoldingPolicyHorizonsAndWaveTrail(t *testing.T) {
	// Build a 120-minute 1m bar series with a 15m higher-low pivot and breakeven activation,
	// plus a second path for short direction, gap-stop, and intra-bar stop-first.
	baseTS := int64(1_700_000_000)
	upBars := make([]Bar, 0, 120)
	px := 100.0
	for m := 0; m < 120; m++ {
		ts := baseTS + int64(m)*60
		openPx := px
		var closePx, highPx, lowPx float64
		switch {
		case m < 15:
			closePx = 100.0 + float64(m+1)*0.18 // reaches ~102.7 (> +1.3R on 2% stop)
			highPx = closePx + 0.1
			lowPx = openPx - 0.05
		case m < 30:
			// pullback forming a higher-low at 101.2 (> initial stop 98.0)
			closePx = 101.4
			highPx = 101.8
			lowPx = 101.2
		case m < 50:
			// rally to confirm the 15m pivot low
			closePx = 103.5
			highPx = 103.8
			lowPx = 101.5
		default:
			// drop through trailed stop 101.2
			closePx = 100.5
			highPx = 101.5
			lowPx = 100.2
		}
		upBars = append(upBars, Bar{TS: ts, Open: openPx, High: highPx, Low: lowPx, Close: closePx})
		px = closePx
	}

	for _, pol := range []PolicyKind{PolicyFixed5m, PolicyFixed30m, PolicyFixedBracket, PolicyWave} {
		res := SimulateHoldingPolicy(PolicySimInput{
			SignalID:       "sig-up",
			WaveClusterID:  "wave_cluster_0001",
			Symbol:         "BTC/USDT:USDT",
			Direction:      types.CycleDirectionUp,
			EntryUnix:      baseTS,
			EntryPrice:     100.0,
			StopPrice:      98.0,
			TickSize:       0.01,
			ATR1mEntry:     0.15,
			ADV5mEntryUSD:  5_000_000,
			NotionalUSD:    10_000,
			Bars:           upBars,
			BarPeriodSec:   60,
			Policy:         pol,
			KBE:            0.8,
			TargetR:        1.5,
			UseLimitTarget: pol == PolicyFixedBracket,
		})
		if res.ExitPrice <= 0 || math.IsNaN(res.NetR()) {
			t.Fatalf("policy %s invalid result: %+v", pol, res)
		}
	}

	// Short direction wave simulation + k_be = +Inf + breakeven stop
	downBars := make([]Bar, 0, 60)
	px = 100.0
	for m := 0; m < 60; m++ {
		ts := baseTS + int64(m)*60
		openPx := px
		var closePx, highPx, lowPx float64
		switch {
		case m < 15:
			closePx = 97.2 // > +1R move down for short (stop=102)
			highPx = openPx + 0.05
			lowPx = closePx - 0.1
		case m < 30:
			closePx = 98.5 // lower-high pivot at 98.8 (< 102)
			highPx = 98.8
			lowPx = 98.2
		case m < 45:
			closePx = 96.8
			highPx = 98.0
			lowPx = 96.5
		default:
			closePx = 100.2 // retraces through breakeven / trail stop
			highPx = 100.5
			lowPx = 97.0
		}
		downBars = append(downBars, Bar{TS: ts, Open: openPx, High: highPx, Low: lowPx, Close: closePx})
		px = closePx
	}
	resShortWave := SimulateHoldingPolicy(PolicySimInput{
		SignalID:      "sig-down",
		WaveClusterID: "wave_cluster_0001",
		Symbol:        "SOL/USDT:USDT",
		Direction:     types.CycleDirectionDown,
		EntryUnix:     baseTS,
		EntryPrice:    100.0,
		StopPrice:     102.0,
		Bars:          downBars,
		BarPeriodSec:  60,
		Policy:        PolicyWave,
		KBE:           0.8,
	})
	if resShortWave.ExitKind != "trail_stop" && resShortWave.ExitKind != "breakeven_stop" {
		t.Fatalf("expected short wave trail/breakeven stop exit, got %s", resShortWave.ExitKind)
	}

	resInf := SimulateHoldingPolicy(PolicySimInput{
		SignalID:     "sig-inf",
		Symbol:       "SOL/USDT:USDT",
		Direction:    types.CycleDirectionDown,
		EntryUnix:    baseTS,
		EntryPrice:   100.0,
		StopPrice:    102.0,
		Bars:         downBars[:10],
		BarPeriodSec: 60,
		Policy:       PolicyWave,
		KBE:          math.Inf(1),
	})
	if resInf.PolicyName != "wave_k_be=inf" {
		t.Fatalf("PolicyName=%q want wave_k_be=inf", resInf.PolicyName)
	}

	// Intra-bar worst-case: one bar touches both stop (99.0) and target (101.5) -> must settle stop-first!
	ambiguous := SimulateHoldingPolicy(PolicySimInput{
		SignalID:   "sig-ambig",
		Symbol:     "SOL/USDT:USDT",
		Direction:  types.CycleDirectionUp,
		EntryUnix:  baseTS,
		EntryPrice: 100.0,
		StopPrice:  99.0,
		TargetR:    1.5,
		Policy:     PolicyFixedBracket,
		Bars: []Bar{
			{TS: baseTS + 60, Open: 100.0, High: 102.0, Low: 98.8, Close: 101.8},
		},
	})
	if ambiguous.ExitKind != "initial_stop" || ambiguous.NetR() >= 0 {
		t.Fatalf("intra-bar stop+target touch must settle stop-first, got %+v", ambiguous)
	}

	// Gap-stop at open
	gapRes := SimulateHoldingPolicy(PolicySimInput{
		SignalID:   "sig-gap",
		Symbol:     "SOL/USDT:USDT",
		Direction:  types.CycleDirectionUp,
		EntryUnix:  baseTS,
		EntryPrice: 100.0,
		StopPrice:  99.0,
		Policy:     PolicyWave,
		Bars: []Bar{
			{TS: baseTS + 60, Open: 98.5, High: 99.2, Low: 98.0, Close: 99.0},
		},
	})
	if gapRes.ExitKind != "initial_stop" || gapRes.ExitPrice != 98.5 {
		t.Fatalf("gap stop at open failed: %+v", gapRes)
	}
}

func TestFrictionFundingLimitAndClusterStats(t *testing.T) {
	// Maker limit order fills only if price trades through by >= 1 tick
	if LimitOrderFills(true, 100.0, 100.0, 101.0, 0.01) {
		t.Fatal("buy limit touched but not traded through by 1 tick must not fill")
	}
	if !LimitOrderFills(true, 100.0, 99.99, 101.0, 0.01) {
		t.Fatal("buy limit traded through by 1 tick must fill")
	}
	if LimitOrderFills(false, 100.0, 99.0, 100.0, 0.01) {
		t.Fatal("sell limit touched but not traded through by 1 tick must not fill")
	}
	if !LimitOrderFills(false, 100.0, 99.0, 100.01, 0.01) {
		t.Fatal("sell limit traded through by 1 tick must fill")
	}

	// Funding across 8h ticks
	t0 := int64(0)
	t1 := EightHoursSec * 2
	sched := map[int64]float64{
		EightHoursSec:     0.0001,
		EightHoursSec * 2: 0.0002,
	}
	fLong := ComputeFundingUSD(types.CycleDirectionUp, 10_000, t0, t1, sched, 0.0001)
	fShort := ComputeFundingUSD(types.CycleDirectionDown, 10_000, t0, t1, sched, 0.0001)
	if math.Abs(fLong-3.0) > 1e-6 || math.Abs(fShort+3.0) > 1e-6 {
		t.Fatalf("fLong=%v fShort=%v want +3/-3", fLong, fShort)
	}

	// Maker open + close + realized fee/funding override in ComputeFullFrictionNetR
	bd := ComputeFullFrictionNetR(FullFrictionInput{
		Symbol:             "BTC/USDT:USDT",
		Direction:          types.CycleDirectionDown,
		EntryPrice:         100.0,
		ExitPrice:          97.0,
		StopPrice:          101.0,
		EntryUnix:          t0,
		ExitUnix:           t1,
		NotionalUSD:        10_000,
		OpenIsMaker:        true,
		CloseIsMaker:       true,
		RealizedFeeUSD:     4.0,
		RealizedFundingUSD: -2.0,
		HasRealizedFunding: true,
	})
	if bd.FeeUSD != 4.0 || bd.FundingUSD != -2.0 || bd.NetR <= 0 {
		t.Fatalf("unexpected breakdown: %+v", bd)
	}

	// ClusterRobustMean across 3 wave clusters
	stat := ClusterRobustMean(
		[]float64{0.5, 0.3, -0.2, 0.8, 0.1},
		[]string{"c1", "c1", "c2", "c2", "c3"},
	)
	if stat.N != 5 || stat.NClusters != 3 || stat.ClusterSE <= 0 {
		t.Fatalf("ClusterRobustMean=%+v", stat)
	}
}

func TestComputeOutcomeFullFrictionAndHumanEntryReanchor(t *testing.T) {
	baseTS := int64(1_700_000_000)
	bars := []Bar{
		{TS: baseTS + 300, Open: 100.0, High: 101.0, Low: 99.5, Close: 100.8},
		{TS: baseTS + 600, Open: 100.8, High: 103.5, Low: 100.5, Close: 103.2},
		{TS: baseTS + 900, Open: 103.2, High: 104.0, Low: 102.8, Close: 103.6},
	}
	out := ComputeOutcome(PathInput{
		TicketID:           "t-ff",
		SessionID:          "s-ff",
		WaveClusterID:      "wave_cluster_0001",
		Symbol:             "SOL/USDT:USDT",
		Direction:          types.CycleDirectionUp,
		Decision:           types.DecisionAccepted,
		Bars:               bars,
		Entry:              100.0,
		Stop:               98.0,
		Target:             103.0,
		UseFullFriction:    true,
		NotionalUSD:        10_000,
		ATR1mEntry:         0.20,
		ADV5mEntryUSD:      2_000_000,
		RealizedFeeUSD:     10.0,
		RealizedFundingUSD: 1.5,
		HasRealizedFunding: true,
	}, DefaultHorizons())
	if !out.TargetHitFirst || out.PolicyWaveR == nil || out.Initial1RPct <= 0 {
		t.Fatalf("ComputeOutcome full-friction missing fields: %+v", out)
	}

	// HumanEntryPrice re-anchoring in ComputeOrthogonalHumanAlpha (TimingAlpha != 0, Residual == 0)
	planned := 99.5
	rows := []TicketRow{
		{
			Ticket: types.DecisionTicket{
				ID: "t-timing", Symbol: "SOL/USDT:USDT", Grade: types.TicketGradeA,
				Direction: types.CycleDirectionUp, CreatedAt: baseTS,
				RiskPlan: types.RiskPlan{EntryPrice: 100.0, StopPrice: 98.0},
			},
			Decision:       types.DecisionAccepted,
			DecisionRecord: &types.DecisionRecord{TicketID: "t-timing", Decision: types.DecisionAccepted, PlannedEntry: &planned},
			Outcome:        out,
			HasOut:         true,
		},
	}
	// Clear PolicyWaveR on copy to also exercise ticketMechanicalWaveR fallback path
	rows[0].Outcome.PolicyWaveR = nil
	decomps := ComputeOrthogonalHumanAlpha(rows)
	if len(decomps) != 1 || decomps[0].Residual != 0 || decomps[0].TimingAlpha == 0 {
		t.Fatalf("expected non-zero TimingAlpha and 0 residual: %+v", decomps)
	}
}
