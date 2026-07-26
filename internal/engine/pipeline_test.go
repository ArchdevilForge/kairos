package engine

import (
	"testing"

	"github.com/ArchdevilForge/kairos/internal/config"
	"github.com/ArchdevilForge/kairos/internal/detector"
	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestNewPipeline_Defaults(t *testing.T) {
	cfg, err := config.LoadString("")
	if err != nil {
		t.Fatal(err)
	}
	p := NewPipeline(cfg, nil)
	if p == nil {
		t.Fatal("nil pipeline")
	}
	if p.minSeverityRank != severityRank("LOW") {
		t.Fatalf("min severity rank: %d", p.minSeverityRank)
	}
}

func TestSeverityRank(t *testing.T) {
	if severityRank("HIGH") <= severityRank("MEDIUM") {
		t.Fatal("HIGH should outrank MEDIUM")
	}
	if severityRank("LOW") >= severityRank("MEDIUM") {
		t.Fatal("LOW should rank below MEDIUM")
	}
}

func TestDiffSymbols(t *testing.T) {
	added := diffSymbols([]string{"A", "B", "C"}, []string{"A", "B"})
	if len(added) != 1 || added[0] != "C" {
		t.Fatalf("added: %v", added)
	}
}

func TestFloatFromMap(t *testing.T) {
	m := map[string]any{"x": 1.5, "y": 2.5}
	v, ok := floatFromMap(m, "x")
	if !ok || v != 1.5 {
		t.Fatalf("x: %v %v", v, ok)
	}
	v, ok = floatFromMap(m, "y")
	if !ok || v != 2.5 {
		t.Fatalf("y: %v %v", v, ok)
	}
	if def := floatFromMapDefault(m, "missing", 9); def != 9 {
		t.Fatalf("default: %v", def)
	}
}

func TestBuildCondition_OpenInterest(t *testing.T) {
	evt := types.AnomalyEvent{
		EventType: "open_interest_change",
		Data: map[string]any{
			"open_interest":          100.0,
			"previous_open_interest": 90.0,
			"change_pct":             11.1,
		},
	}
	cond := buildCondition(evt)
	if cond == "" {
		t.Fatal("expected non-empty condition")
	}
}

func TestPassesMarketEventPolicy_NoLiquidityWeight(t *testing.T) {
	cfg, err := config.LoadString(`
alertPolicy:
  enabled: true
  allowedEventTypes: ["market_impulse", "price_velocity"]
  minSeverity: "LOW"
`)
	if err != nil {
		t.Fatal(err)
	}
	p := NewPipeline(cfg, nil)
	evt := types.AnomalyEvent{
		Symbol:    "MARKET",
		EventType: "market_impulse",
		Severity:  types.SeverityHigh,
		Data:      map[string]any{"direction": "up"},
	}
	if !p.passesAlertPolicy(evt) {
		t.Fatal("market event should pass without liquidity weight")
	}
}

func TestShouldGateIndividualAlert_Quiet(t *testing.T) {
	cfg, err := config.LoadString(`
marketPulse:
  enabled: true
  shadowMode: false
  gateIndividualAlertsWhenQuiet: true
`)
	if err != nil {
		t.Fatal(err)
	}
	p := NewPipeline(cfg, nil)
	evt := types.AnomalyEvent{EventType: "price_velocity", Symbol: "DOGE/USDT:USDT"}

	// Without detector, gate is inactive.
	p.marketPulseDet = nil
	if p.shouldGateIndividualAlert(evt) {
		t.Fatal("nil detector must not gate")
	}

	// QUIET + gate on → suppress.
	d := detector.NewMarketPulseDetector(cfg.MarketPulse)
	p.marketPulseDet = d
	if d.State() != types.MarketStateQuiet {
		t.Fatalf("state=%s", d.State())
	}
	if !p.shouldGateIndividualAlert(evt) {
		t.Fatal("QUIET should gate price_velocity")
	}

	// Shadow mode must never change single-symbol behaviour.
	p.cfg.MarketPulse.ShadowMode = true
	if p.shouldGateIndividualAlert(evt) {
		t.Fatal("shadow mode must not gate single-symbol alerts")
	}
	p.cfg.MarketPulse.ShadowMode = false

	// Gate switch off → allow.
	p.cfg.MarketPulse.GateIndividualAlertsWhenQuiet = false
	if p.shouldGateIndividualAlert(evt) {
		t.Fatal("gate switch off must allow")
	}
}

func TestBuildCondition_MarketImpulse(t *testing.T) {
	cond := buildCondition(types.AnomalyEvent{
		EventType: "market_impulse",
		Data: map[string]any{
			"direction":  "up",
			"state_from": "QUIET",
			"state_to":   "IMPULSE_UP",
		},
	})
	if cond != "up_QUIET_IMPULSE_UP" {
		t.Fatalf("cond=%q", cond)
	}
}

func TestIsMarketPulseEvent(t *testing.T) {
	if !isMarketPulseEvent("market_impulse") || isMarketPulseEvent("price_velocity") {
		t.Fatal("isMarketPulseEvent")
	}
}
