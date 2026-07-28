package decision

import (
	"strings"
	"testing"

	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestBuildTicket_CounterTrendNoAddOn(t *testing.T) {
	match := types.PlaybookMatch{
		SchemaVersion: types.PlaybookMatchSchemaVersion,
		PlaybookID:    types.PlaybookLeaderPullbackV1,
		SessionID:     "s1",
		Symbol:        "ETH/USDT:USDT",
		Matched:       true,
		Grade:         types.TicketGradeB,
		Direction:     types.CycleDirectionUp,
		TradeClass:    types.TradeClassCounterTrendLong,
		Reasons:       []string{"counter"},
		RiskTemplate:  types.RiskTemplateCounterTrend,
	}
	ticket := BuildTicket(BuildInput{
		TicketID: "t1",
		Match:    match,
		Context: types.PlaybookContext{
			SessionID: "s1",
			Symbol:    match.Symbol,
		},
		Invalidations: []string{"5m low breaks"},
		EntryPrice:    100,
		StopPrice:     97,
		Equity:        10_000,
	})
	if ticket.RiskPlan.AddOnAllowed {
		t.Fatal("counter trend must disallow add-on")
	}
	if ticket.RiskPlan.TemplateID != types.RiskTemplateCounterTrend {
		t.Fatalf("template=%s", ticket.RiskPlan.TemplateID)
	}
	if ticket.RiskPlan.StopDistancePct != 3 {
		t.Fatalf("stop_dist=%v", ticket.RiskPlan.StopDistancePct)
	}
	// loss budget = 10000 * 0.25/100 = 25; dist 3% → notional ≈ 25/0.03
	if ticket.RiskPlan.SuggestedNotional < 800 || ticket.RiskPlan.SuggestedNotional > 900 {
		t.Fatalf("notional=%v", ticket.RiskPlan.SuggestedNotional)
	}
	if len(ticket.Invalidations) != 1 {
		t.Fatal("invalidations required on ticket")
	}
	if ticket.SchemaVersion != types.DecisionTicketSchemaVersion {
		t.Fatalf("schema=%s", ticket.SchemaVersion)
	}
}

func TestBuildTicket_AlignedAllowsAddOn(t *testing.T) {
	match := types.PlaybookMatch{
		PlaybookID:   types.PlaybookLeaderPullbackV1,
		SessionID:    "s1",
		Symbol:       "SOL/USDT:USDT",
		Matched:      true,
		Grade:        types.TicketGradeA,
		Direction:    types.CycleDirectionUp,
		TradeClass:   types.TradeClassAlignedLong,
		RiskTemplate: types.RiskTemplateAlignedSpring,
	}
	ticket := BuildTicket(BuildInput{
		TicketID:      "t2",
		Match:         match,
		Invalidations: []string{"hl breaks"},
		EntryPrice:    50,
		StopPrice:     48,
	})
	if !ticket.RiskPlan.AddOnAllowed {
		t.Fatal("aligned spring should allow add-on")
	}
}

func TestFormatTicket_ContainsGrade(t *testing.T) {
	s := FormatTicket(types.DecisionTicket{
		ID: "t", Symbol: "X", Direction: types.CycleDirectionUp,
		Grade: types.TicketGradeA, PlaybookID: "p", TradeClass: types.TradeClassAlignedLong,
		RiskPlan: types.RiskPlan{TemplateID: types.RiskTemplateAlignedSummer},
	})
	if !strings.Contains(s, "grade=A") {
		t.Fatalf("format=%s", s)
	}
}

func TestRecordDecision_ReasonCodes(t *testing.T) {
	r := RecordDecision("t1", types.DecisionRejected, []string{types.ReasonTooExtended}, "late", 9)
	if r.Decision != types.DecisionRejected || r.ReasonCodes[0] != types.ReasonTooExtended {
		t.Fatalf("%+v", r)
	}
}
