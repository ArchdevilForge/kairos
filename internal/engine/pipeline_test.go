package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArchdevilForge/kairos/internal/config"
	"github.com/ArchdevilForge/kairos/internal/detector"
	"github.com/ArchdevilForge/kairos/internal/storage"
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

	// A detector with no data must not gate: no vision, no opinion.
	blind := detector.NewMarketPulseDetector(cfg.MarketPulse)
	p.marketPulseDet = blind
	if blind.State() != types.MarketStateQuiet {
		t.Fatalf("state=%s", blind.State())
	}
	if p.shouldGateIndividualAlert(evt) {
		t.Fatal("a detector without data must not suppress single-symbol alerts")
	}

	// Healthy but quiet market → suppress.
	d := quietHealthyDetector(t, cfg.MarketPulse)
	p.marketPulseDet = d
	if d.State() != types.MarketStateQuiet {
		t.Fatalf("state=%s", d.State())
	}
	if !d.LastSnapshot().DataOK {
		t.Fatalf("test setup: snapshot must be healthy, gate=%s", d.LastSnapshot().GateReason)
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

func TestRecordMarketPulseSnapshot(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewMarketPulseStore(types.StorageConfig{
		DatabasePath: filepath.Join(dir, "kairos.db"),
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := config.LoadString(`
marketPulse:
  enabled: true
  shadowMode: true
  minValidSymbols: 15
  minFreshRatio: 0.5
  warmupSeconds: 1
  freshnessSeconds: 60
`)
	if err != nil {
		t.Fatal(err)
	}
	p := NewPipeline(cfg, nil)

	// Nil detector / store: no-op.
	p.recordMarketPulseSnapshot()

	d := quietHealthyDetector(t, cfg.MarketPulse)
	p.marketPulseDet = d
	p.mpStore = store
	p.recordMarketPulseSnapshot()

	b, err := os.ReadFile(store.SnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	line := string(b)
	if !strings.Contains(line, "QUIET") || !strings.Contains(line, "data_ok") {
		t.Fatalf("snapshot line: %s", line)
	}
	// Second call within 60s of snap time is throttled → still one line.
	p.recordMarketPulseSnapshot()
	b2, err := os.ReadFile(store.SnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(b2), "\n") != 1 {
		t.Fatalf("want 1 line after throttle, got %q", b2)
	}
}

// quietHealthyDetector returns a MarketPulse detector fed with a flat but
// fully-covered market: DataOK is true and the state stays QUIET, which is the
// only situation in which the quiet gate is allowed to suppress anything.
func quietHealthyDetector(t *testing.T, cfg types.MarketPulseConfig) *detector.MarketPulseDetector {
	t.Helper()
	d := detector.NewMarketPulseDetector(cfg)
	clock := 1_000_000.0
	d.SetNowFunc(func() float64 { return clock })

	syms := make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		syms = append(syms, fmt.Sprintf("Q%02d/USDT:USDT", i))
	}
	d.UpdateUniverse(syms)

	price := 100.0
	for step := 0; step < 90; step++ { // 450s > warmup, flat prices → QUIET
		for _, s := range syms {
			p := price
			d.OnTicker(context.Background(), types.Ticker{Symbol: s, LastPrice: &p})
		}
		d.EvaluateAt(clock)
		clock += 5
	}
	return d
}
