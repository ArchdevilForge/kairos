package detector

import (
	"context"
	"math"
	"testing"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// The old estimator folded the current observation into its own denominator,
// so z collapsed to ±1 for every symbol: 17 of 17 production events reported
// |z| within [0.91, 1.08]. A calm symbol that suddenly jumps must now score
// far above 1, and a symbol that always moves that much must score near 1.
func TestZScore_NoLongerCollapsesToOne(t *testing.T) {
	calm := &mpSymbolSeries{}
	for i := 0; i < volWindowSamples; i++ {
		v := 0.05
		if i%2 == 1 {
			v = -0.05
		}
		calm.volSamples = append(calm.volSamples, v)
	}
	volatile := &mpSymbolSeries{}
	for i := 0; i < volWindowSamples; i++ {
		v := 1.0
		if i%2 == 1 {
			v = -1.0
		}
		volatile.volSamples = append(volatile.volSamples, v)
	}

	vol := types.MarketPulseVolatilityConfig{Enabled: true, EWMAAlpha: 0.1, FloorPct: 0.03}

	zCalm, ok := zScore(1.0, calm, vol)
	if !ok {
		t.Fatal("calm symbol should have a usable z")
	}
	zVol, ok := zScore(1.0, volatile, vol)
	if !ok {
		t.Fatal("volatile symbol should have a usable z")
	}

	// Same 1% move: extraordinary for the calm symbol, routine for the other.
	if zCalm < 15 {
		t.Fatalf("a 1%% move on a 0.05%% sigma symbol must be extreme, got z=%.2f", zCalm)
	}
	if math.Abs(zVol-1.0) > 0.01 {
		t.Fatalf("a 1%% move on a 1%% sigma symbol should be ~1σ, got z=%.2f", zVol)
	}
	if zCalm <= zVol {
		t.Fatalf("z must discriminate: calm=%.2f volatile=%.2f", zCalm, zVol)
	}
}

// Without enough history the detector must report z as unavailable rather than
// inventing a value that gates would then act on.
func TestZScore_UnavailableWithoutHistory(t *testing.T) {
	vol := types.MarketPulseVolatilityConfig{Enabled: true, FloorPct: 0.03}
	short := &mpSymbolSeries{volSamples: []float64{0.1, -0.1, 0.2}}
	if _, ok := zScore(0.5, short, vol); ok {
		t.Fatal("z must be unavailable below volMinSamples")
	}
	if _, ok := zScore(0.5, nil, vol); ok {
		t.Fatal("nil series must not produce a z")
	}
}

// Volatility samples must be spaced so consecutive 60s windows do not overlap;
// per-tick sampling was the root cause of the degenerate estimator.
func TestVolSamples_AreNonOverlapping(t *testing.T) {
	cfg := testMPConfig()
	d := NewMarketPulseDetector(cfg)
	clock := 500_000.0
	d.SetNowFunc(func() float64 { return clock })
	d.UpdateUniverse([]string{"AAA/USDT:USDT"})

	// Feed a tick every 5 seconds for 10 minutes.
	for i := 0; i < 120; i++ {
		p := 100.0 + float64(i)*0.01
		d.OnTicker(context.Background(), types.Ticker{Symbol: "AAA/USDT:USDT", LastPrice: &p})
		clock += 5
	}

	d.mu.RLock()
	ser := d.symbols["AAA/USDT:USDT"]
	n := len(ser.volSamples)
	d.mu.RUnlock()

	// 600 seconds of ticks at one sample per 60s window: ~9-10 samples, never
	// the 120 that per-tick sampling would have produced.
	if n == 0 || n > 12 {
		t.Fatalf("expected roughly one sample per 60s window, got %d", n)
	}
}

// A blind detector must announce the outage and the recovery.
func TestDataHealth_AnnouncesOutageAndRecovery(t *testing.T) {
	cfg := testMPConfig()
	cfg.DataHealthAlertSeconds = 600
	d := NewMarketPulseDetector(cfg)
	clock := 700_000.0
	d.SetNowFunc(func() float64 { return clock })

	syms := mpSymbols(20)
	d.UpdateUniverse(syms)

	// No data at all: snapshots gate, and after the window an alert fires.
	for clock < 700_000+1300 {
		d.EvaluateAt(clock)
		clock += 5
	}

	var stale, recovered int
	for _, e := range drainEvents(d) {
		switch e.EventType {
		case "market_data_stale":
			stale++
			if e.Data["gate_reason"] == "" || e.Data["gate_reason"] == nil {
				t.Fatalf("stale alert must name the gate reason: %+v", e.Data)
			}
		case "market_data_recovered":
			recovered++
		}
	}
	if stale != 1 {
		t.Fatalf("expected exactly one outage alert, got %d", stale)
	}
	if recovered != 0 {
		t.Fatalf("no recovery expected while still blind, got %d", recovered)
	}

	// Restore data: warm up and feed a healthy flat market.
	feedUniverse(d, syms, clock-60, clock, 100, 0, 1.0)
	d.mu.Lock()
	for _, ser := range d.symbols {
		ser.warmupSince = clock - 1000
	}
	d.universeChangedAt = 0
	d.mu.Unlock()

	for i := 0; i < 40; i++ {
		for _, s := range syms {
			p := 100.0
			d.OnTicker(context.Background(), types.Ticker{Symbol: s, LastPrice: &p})
		}
		d.EvaluateAt(clock)
		clock += 5
	}

	for _, e := range drainEvents(d) {
		if e.EventType == "market_data_recovered" {
			recovered++
		}
	}
	if recovered != 1 {
		t.Fatalf("expected exactly one recovery alert, got %d", recovered)
	}
}

// One market move must produce one notification: entering stress from QUIET
// previously emitted an impulse and a stress event bearing the same timestamp.
func TestStress_DoesNotDoubleEmitWithImpulse(t *testing.T) {
	cfg := testMPConfig()
	cfg.RequirePrimaryConfirmation = false
	cfg.Volatility.Enabled = false
	cfg.Impulse.ConfirmationSamples = 1
	cfg.Impulse.ConfirmationWindowSamples = 1
	cfg.Impulse.MinBreadth = 0.6
	cfg.Impulse.MinMedianReturnPct = 0.2
	cfg.Stress.MinBreadth = 0.7
	cfg.Stress.MinMedianReturnPct = 0.3
	d := NewMarketPulseDetector(cfg)

	clock := 800_000.0
	d.SetNowFunc(func() float64 { return clock })
	syms := mpSymbols(20)
	d.UpdateUniverse(syms)

	// A move large enough to satisfy stress directly from QUIET.
	feedUniverse(d, syms, clock-60, clock, 100, 0.9, 1.0)
	d.mu.Lock()
	for _, ser := range d.symbols {
		ser.warmupSince = clock - 1000
		ser.volSamples = seedVolSamples(0.1)
	}
	d.universeChangedAt = 0
	d.mu.Unlock()
	d.EvaluateAt(clock)

	byTS := map[float64][]string{}
	for _, e := range drainEvents(d) {
		if isMarketEventType(e.EventType) {
			byTS[e.Timestamp] = append(byTS[e.Timestamp], e.EventType)
		}
	}
	for ts, kinds := range byTS {
		if len(kinds) > 1 {
			t.Fatalf("timestamp %.0f produced %d notifications: %v", ts, len(kinds), kinds)
		}
	}
	if d.State() != types.MarketStateStressUp {
		t.Fatalf("expected STRESS_UP, got %s", d.State())
	}
}

// Coverage must ride along with every event so a partial-outage reading is
// visibly weaker rather than silently equivalent to full coverage.
func TestEmit_CarriesCoverage(t *testing.T) {
	cfg := testMPConfig()
	cfg.RequirePrimaryConfirmation = false
	cfg.Volatility.Enabled = false
	cfg.Impulse.ConfirmationSamples = 1
	cfg.Impulse.ConfirmationWindowSamples = 1
	d := NewMarketPulseDetector(cfg)

	clock := 900_000.0
	d.SetNowFunc(func() float64 { return clock })
	syms := mpSymbols(20)
	d.UpdateUniverse(syms)
	feedUniverse(d, syms, clock-60, clock, 100, 0.5, 1.0)
	d.mu.Lock()
	for _, ser := range d.symbols {
		ser.warmupSince = clock - 1000
	}
	d.universeChangedAt = 0
	d.mu.Unlock()
	d.EvaluateAt(clock)

	for _, e := range drainEvents(d) {
		if !isMarketEventType(e.EventType) {
			continue
		}
		if _, ok := e.Data["coverage"]; !ok {
			t.Fatalf("event %s missing coverage: %+v", e.EventType, e.Data)
		}
		if _, ok := e.Data["universe_size"]; !ok {
			t.Fatalf("event %s missing universe_size", e.EventType)
		}
		return
	}
	t.Fatal("no market event emitted")
}
