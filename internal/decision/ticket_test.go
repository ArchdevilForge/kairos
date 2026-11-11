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

func TestPhase3b_SetupPointEnumAndTicketFormatting(t *testing.T) {
	if len(types.ValidSetupPoints()) != 6 {
		t.Fatalf("want 6 fixed §3 setup points, got %d", len(types.ValidSetupPoints()))
	}
	for i, want := range types.ValidSetupPoints() {
		shorthand := string(rune('1' + i))
		got, ok := types.ParseSetupPoint(shorthand)
		if !ok || got != want {
			t.Fatalf("ParseSetupPoint(%q) = (%q, %v), want (%q, true)", shorthand, got, ok, want)
		}
		if types.SetupPointLabelZH(want) == "" {
			t.Fatalf("SetupPointLabelZH(%q) empty", want)
		}
	}
	if _, ok := types.ParseSetupPoint("POINT_9_MADE_UP"); ok {
		t.Fatal("unknown setup point must fail")
	}

	match := types.PlaybookMatch{
		PlaybookID:   types.PlaybookLeaderPullbackV1,
		SessionID:    "s-3b",
		Symbol:       "SOL/USDT:USDT",
		Matched:      true,
		Grade:        types.TicketGradeA,
		Direction:    types.CycleDirectionUp,
		TradeClass:   types.TradeClassAlignedLong,
		RiskTemplate: types.RiskTemplateAlignedSpring,
	}
	tk := BuildTicket(BuildInput{
		TicketID:       "t-3b",
		Match:          match,
		SetupPoint:     types.SetupPoint2MinorDiv,
		StopBasis:      "15m HL swing low 97.50",
		Invalidations:  []string{"15m close < 97.50"},
		EntryPrice:     100.0,
		StopPrice:      97.5,
		Equity:         10_000,
		TrancheBaseUSD: 10_000,
		CreatedAt:      1_700_000_000,
		SignalAt:       1_700_000_000,
	})

	warn := []string{"WARN_TILT_REVERSE [上头反手]: 同币 SOL/USDT:USDT 止损后 12.0 分钟内反手开票"}
	cliCard := FormatTicketWithWarnings(tk, warn)
	for _, want := range []string{
		"setup=POINT_2_MINOR_DIV(点位2(主升小分歧回踩))",
		"stop: entry=100.0000  stop_price=97.5000  1R=2.5000 (2.50%)  basis=15m HL swing low 97.50",
		"tranche: base=$10000.00  max_lev=5.0x  suggested_notional=$3000.00  max_notional=$3750.00",
		"behavior_warnings: WARN_TILT_REVERSE [上头反手]",
	} {
		if !strings.Contains(cliCard, want) {
			t.Fatalf("FormatTicketWithWarnings missing %q in:\n%s", want, cliCard)
		}
	}

	tgCard := FormatTicketTelegram(tk, warn)
	for _, want := range []string{
		"非入场指令，决定权在你。",
		"<b>点位标签</b>: POINT_2_MINOR_DIV (点位2(主升小分歧回踩))",
		"<b>结构止损</b>: 损 97.5000 (入 100.0000) | 1R=2.5000 (2.50%) | 依据: 15m HL swing low 97.50",
		"<b>分仓计划</b>: 基数 $10000 | 杠杆上限 <=5.0x | 建议名义 $3000.00 (上限 $3750.00)",
		"⚠ <b>行为提示</b>: WARN_TILT_REVERSE [上头反手]",
	} {
		if !strings.Contains(tgCard, want) {
			t.Fatalf("FormatTicketTelegram missing %q in:\n%s", want, tgCard)
		}
	}
}

// TestDecisionTicket_ByteLevelStreamRegression verifies §4 acceptance:
// existing ticket stream without new Phase 3b fields produces byte-identical JSON
// to the pre-Phase-3b baseline, and never fabricates setup_point or stop_basis.
func TestDecisionTicket_ByteLevelStreamRegression(t *testing.T) {
	match := types.PlaybookMatch{
		PlaybookID:   types.PlaybookLeaderPullbackV1,
		SessionID:    "sess-reg",
		Symbol:       "ETH/USDT:USDT",
		Matched:      true,
		Grade:        types.TicketGradeA,
		Direction:    types.CycleDirectionUp,
		TradeClass:   types.TradeClassAlignedLong,
		Reasons:      []string{"leader rank 1"},
		RiskTemplate: types.RiskTemplateAlignedSpring,
	}
	tk := BuildTicket(BuildInput{
		TicketID:       "tkt-sess-reg-1",
		Match:          match,
		Invalidations:  []string{"15m hl breaks"},
		EntryPrice:     2500,
		StopPrice:      2450,
		Equity:         10_000,
		TrancheBaseUSD: 10_000,
		Trigger:        "leader_pullback restart",
		CreatedAt:      1_720_000_000,
		SignalAt:       1_720_000_000,
	})
	gotBytes, err := json.Marshal(tk)
	if err != nil {
		t.Fatal(err)
	}
	const wantBaselineJSON = `{"schema_version":"decision_ticket.v1","id":"tkt-sess-reg-1","session_id":"sess-reg","symbol":"ETH/USDT:USDT","playbook_id":"leader_pullback_v1","direction":"up","grade":"A","trade_class":"aligned_long","market_cycle":{"schema_version":"","as_of_unix":0,"legacy_climate":"","nodes":null,"primary_direction":"","alignment":"","trade_class":"","summary":null},"symbol_cycle":{"schema_version":"","as_of_unix":0,"legacy_climate":"","nodes":null,"primary_direction":"","alignment":"","trade_class":"","summary":null},"entry_plan":{"mode":"closed_bar","zone_low":0,"zone_high":0,"trigger":"leader_pullback restart"},"risk_plan":{"template_id":"aligned_spring","risk_budget_pct":0.75,"entry_price":2500,"stop_price":2450,"stop_distance_pct":2,"effective_stop_pct":2,"suggested_notional":3750,"max_notional":4687.5,"max_leverage":10,"tranche_base_usd":10000,"add_on_allowed":true},"reasons":["leader rank 1"],"warnings":[],"invalidations":["15m hl breaks"],"status":"open","created_at":1720000000,"signal_at":1720000000}`
	if string(gotBytes) != wantBaselineJSON {
		t.Fatalf("ticket stream byte regression failed:\ngot:  %s\nwant: %s", string(gotBytes), wantBaselineJSON)
	}
}
