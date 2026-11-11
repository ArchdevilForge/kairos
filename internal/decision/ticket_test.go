package decision

import (
	"encoding/json"
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

func TestReasonCodeHelpers(t *testing.T) {
	if !types.IsValidReasonCode(types.ReasonTooExtended) {
		t.Fatal("too_extended must be valid")
	}
	if types.IsValidReasonCode("made_up") {
		t.Fatal("made_up must be invalid")
	}
	valid := types.ValidReasonCodes()
	if len(valid) != 12 {
		t.Fatalf("want 12 canonical codes, got %d: %v", len(valid), valid)
	}
	for _, c := range valid {
		if !types.IsValidReasonCode(c) {
			t.Fatalf("code %q from ValidReasonCodes must validate", c)
		}
	}
}

func TestMaxAssetLeverageAndSingleTicketSizing(t *testing.T) {
	if got := MaxAssetLeverage("BTC/USDT:USDT", 10, 5); got != 10 {
		t.Fatalf("BTC max lev=%v want 10", got)
	}
	if got := MaxAssetLeverage("ETHUSDT", 10, 5); got != 10 {
		t.Fatalf("ETH max lev=%v want 10", got)
	}
	if got := MaxAssetLeverage("SOL/USDT:USDT", 10, 5); got != 5 {
		t.Fatalf("SOL max lev=%v want 5", got)
	}

	// Altcoin: TrancheBaseUSD=10000, MaxAssetLev=5 => cap=50000.
	// RiskBudgetUSD=200, EffectiveStopPct=0.2% => RiskBudget/Stop = 100000 => capped at 50000!
	notional := ComputeSingleTicketNotional(10_000, 5, 200, 0.2)
	if notional != 50_000 {
		t.Fatalf("alt notional=%v want 50000", notional)
	}

	// Wide stop: RiskBudgetUSD=100, EffectiveStopPct=2% => RiskBudget/Stop = 5000 < 50000 => 5000.
	notionalWide := ComputeSingleTicketNotional(10_000, 5, 100, 2.0)
	if notionalWide != 5_000 {
		t.Fatalf("wide stop notional=%v want 5000", notionalWide)
	}
}

func TestExitReasonSingleKeyParser(t *testing.T) {
	cases := map[string]types.ExitReason{
		"1":               types.ExitReasonProtectiveStop,
		"p":               types.ExitReasonProtectiveStop,
		"PROTECTIVE_STOP": types.ExitReasonProtectiveStop,
		"2":               types.ExitReasonFakeoutAbort,
		"f":               types.ExitReasonFakeoutAbort,
		"3":               types.ExitReasonStructuralTrail,
		"s":               types.ExitReasonStructuralTrail,
		"4":               types.ExitReasonTargetPartial,
		"t":               types.ExitReasonTargetPartial,
		"5":               types.ExitReasonImpulseEarly,
		"i":               types.ExitReasonImpulseEarly,
	}
	for k, want := range cases {
		got, ok := types.ParseExitReasonKey(k)
		if !ok || got != want {
			t.Fatalf("ParseExitReasonKey(%q) = (%q, %v), want (%q, true)", k, got, ok, want)
		}
	}
	if _, ok := types.ParseExitReasonKey("invalid"); ok {
		t.Fatal("invalid exit reason key must fail")
	}
}

func TestBuildTicket_WinterDefenseHintShadowOnly(t *testing.T) {
	match := types.PlaybookMatch{
		PlaybookID:   types.PlaybookLeaderPullbackV1,
		SessionID:    "s-win",
		Symbol:       "SOL/USDT:USDT",
		Matched:      true,
		Grade:        types.TicketGradeA,
		Direction:    types.CycleDirectionUp,
		TradeClass:   types.TradeClassAlignedLong,
		RiskTemplate: types.RiskTemplateAlignedSummer,
	}

	// 0. No cross-sectional input (SeasonRegime == ""), even if MarketCycle has winter nodes:
	// BOTH season_regime and winter_defense_hint MUST be absent ("", omitted in JSON).
	tUnmeasured := BuildTicket(BuildInput{
		TicketID: "t-unmeasured",
		Match:    match,
		Context: types.PlaybookContext{
			MarketCycle: types.CycleMap{
				LegacyClimate: types.MarketPhaseWinter,
				Nodes: map[string]types.CycleNode{
					"15m": {Timeframe: "15m", Phase: types.WavePhaseWinter},
				},
			},
		},
		Invalidations: []string{"hl breaks"},
		EntryPrice:    100,
		StopPrice:     98,
		Equity:        10_000,
	})
	if tUnmeasured.SeasonRegime != "" {
		t.Fatalf("unmeasured SeasonRegime=%q, want empty", tUnmeasured.SeasonRegime)
	}
	if tUnmeasured.WinterDefenseHint != "" {
		t.Fatalf("unmeasured WinterDefenseHint=%q, want empty", tUnmeasured.WinterDefenseHint)
	}
	rawUnmeasured, err := json.Marshal(tUnmeasured)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rawUnmeasured), `"season_regime"`) ||
		strings.Contains(string(rawUnmeasured), `"winter_defense_hint"`) {
		t.Fatalf("unmeasured ticket JSON must omit both fields, got %s", string(rawUnmeasured))
	}

	// 1. Genuine WINTER regime: winter_defense_hint MUST be present, sizing MUST NOT change.
	tWinter := BuildTicket(BuildInput{
		TicketID:      "t-winter",
		Match:         match,
		Invalidations: []string{"hl breaks"},
		EntryPrice:    100,
		StopPrice:     98,
		Equity:        10_000,
		SeasonRegime:  types.SeasonRegimeWinter,
	})
	if tWinter.SeasonRegime != types.SeasonRegimeWinter {
		t.Fatalf("SeasonRegime=%q, want WINTER", tWinter.SeasonRegime)
	}
	if !strings.Contains(tWinter.WinterDefenseHint, "0.25x") ||
		!strings.Contains(tWinter.WinterDefenseHint, "winter tickets historically negative expectancy, defense sub-test passed twice") {
		t.Fatalf("unexpected WinterDefenseHint=%q", tWinter.WinterDefenseHint)
	}
	if tUnmeasured.RiskPlan != tWinter.RiskPlan {
		t.Fatalf("shadow red line violated: RiskPlan changed between unmeasured (%+v) and WINTER (%+v)",
			tUnmeasured.RiskPlan, tWinter.RiskPlan)
	}

	// 2. Non-WINTER regimes (SPRING, SUMMER, AUTUMN): winter_defense_hint MUST be empty,
	// and RiskPlan sizing MUST be byte-identical to WINTER.
	for _, reg := range []types.SeasonRegime{
		types.SeasonRegimeSpring,
		types.SeasonRegimeSummer,
		types.SeasonRegimeAutumn,
	} {
		tk := BuildTicket(BuildInput{
			TicketID:      "t-other",
			Match:         match,
			Invalidations: []string{"hl breaks"},
			EntryPrice:    100,
			StopPrice:     98,
			Equity:        10_000,
			SeasonRegime:  reg,
		})
		if tk.SeasonRegime != reg {
			t.Fatalf("SeasonRegime=%q, want %q", tk.SeasonRegime, reg)
		}
		if tk.WinterDefenseHint != "" {
			t.Fatalf("non-WINTER regime %s must have empty WinterDefenseHint, got %q", reg, tk.WinterDefenseHint)
		}
		if tk.RiskPlan != tWinter.RiskPlan {
			t.Fatalf("shadow red line violated: RiskPlan changed between %s (%+v) and WINTER (%+v)",
				reg, tk.RiskPlan, tWinter.RiskPlan)
		}
	}
}
