package detector

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
)

func testMPConfig() types.MarketPulseConfig {
	return types.MarketPulseConfig{
		Enabled:                    true,
		ShadowMode:                 true,
		SnapshotIntervalSeconds:    5,
		FreshnessSeconds:           30,
		MaxLookupGapSeconds:        15,
		WarmupSeconds:              1, // tests skip long warmup
		HistoryRetentionSeconds:    900,
		MinFreshRatio:              0.80,
		MinValidSymbols:            15,
		NoiseReturnPct:             0.08,
		PrimarySymbols:             []string{"BTC/USDT:USDT", "ETH/USDT:USDT"},
		RequirePrimaryConfirmation: true,
		// Keep Z as soft metric in most unit tests so impulse samples do not
		// immediately promote into STRESS (which has no confirmation window).
		Volatility: types.MarketPulseVolatilityConfig{
			Enabled:   false,
			EWMAAlpha: 0.10,
			FloorPct:  0.03,
		},
		Impulse: types.MarketPulseImpulseConfig{
			MinBreadth:                0.65,
			MinMedianReturnPct:        0.18,
			MinMedianZ:                1.5,
			ConfirmationSamples:       3,
			ConfirmationWindowSamples: 4,
		},
		Trend: types.MarketPulseTrendConfig{
			MinBreadth:                0.60,
			MinMedianReturnPct:        0.45,
			MinPersistSeconds:         20, // short for tests
			ConfirmationSamples:       3,
			ConfirmationWindowSamples: 4,
		},
		Stress: types.MarketPulseStressConfig{
			MinBreadth:         0.80,
			MinMedianReturnPct: 0.35,
			MinMedianZ:         2.5,
		},
		Decay: types.MarketPulseDecayConfig{
			Enabled:            true,
			MaxBreadth:         0.50,
			MaxMedianReturnPct: 0.08,
			PersistSeconds:     5,
			Notify:             false,
			QuietResetSeconds:  5,
		},
		Leaders:               types.MarketPulseLeadersConfig{Limit: 5},
		CooldownSeconds:       600,
		StressCooldownSeconds: 300,
	}
}

func mpSymbols(n int) []string {
	out := make([]string, 0, n)
	out = append(out, "BTC/USDT:USDT", "ETH/USDT:USDT")
	for i := 2; i < n; i++ {
		out = append(out, "ALT"+itoa(i)+"/USDT:USDT")
	}
	return out
}

// feedUniverse writes past and current prices for all symbols.
// upFraction of non-core symbols (and BTC/ETH) move by retPct over 60s.
func feedUniverse(d *MarketPulseDetector, symbols []string, t0, t1 float64, base float64, retPct float64, upFraction float64) {
	nUp := int(math.Ceil(float64(len(symbols)) * upFraction))
	for i, sym := range symbols {
		p0 := base * (1 + float64(i)*0.001)
		var p1 float64
		if i < nUp {
			p1 = p0 * (1 + retPct/100)
		} else {
			// mild opposite or flat
			p1 = p0 * (1 - 0.01/100)
		}
		d.SetNowFunc(func() float64 { return t0 })
		px := p0
		d.OnTicker(context.Background(), types.Ticker{Symbol: sym, LastPrice: &px})
		d.SetNowFunc(func() float64 { return t1 })
		px2 := p1
		d.OnTicker(context.Background(), types.Ticker{Symbol: sym, LastPrice: &px2})
	}
}

func drainEvents(d *MarketPulseDetector) []types.AnomalyEvent {
	var out []types.AnomalyEvent
	for {
		select {
		case e := <-d.Events():
			out = append(out, e)
		default:
			return out
		}
	}
}

// ── Stats helpers ──────────────────────────────────────────────

func TestMedian_OddEvenEmpty(t *testing.T) {
	if median(nil) != 0 {
		t.Fatal("empty")
	}
	if median([]float64{3}) != 3 {
		t.Fatal("single")
	}
	if median([]float64{1, 3, 2}) != 2 {
		t.Fatal("odd")
	}
	if median([]float64{1, 2, 3, 4}) != 2.5 {
		t.Fatal("even")
	}
	if median([]float64{math.NaN(), 1, 2}) != 1.5 {
		t.Fatal("nan filter")
	}
	if median([]float64{math.Inf(1), 2}) != 2 {
		t.Fatal("inf filter")
	}
}

func TestLookupPrice(t *testing.T) {
	pts := []mpPricePoint{
		{ts: 100, price: 10},
		{ts: 110, price: 11},
		{ts: 120, price: 12},
	}
	if p, ok := lookupPrice(pts, 110, 15); !ok || p != 11 {
		t.Fatalf("exact: %v %v", p, ok)
	}
	if p, ok := lookupPrice(pts, 115, 15); !ok || p != 11 {
		t.Fatalf("between: %v %v", p, ok)
	}
	if _, ok := lookupPrice(pts, 90, 15); ok {
		t.Fatal("before history should fail")
	}
	// gap too large: target 140, last <= target is 120, gap=20 > 15
	if _, ok := lookupPrice(pts, 140, 15); ok {
		t.Fatal("gap exceeded")
	}
	if p, ok := lookupPrice(pts, 125, 15); !ok || p != 12 {
		t.Fatalf("within gap: %v %v", p, ok)
	}
}

func TestSeriesReturnAndPrune(t *testing.T) {
	d := NewMarketPulseDetector(testMPConfig())
	var clock float64 = 1000
	d.SetNowFunc(func() float64 { return clock })
	d.UpdateUniverse([]string{"BTC/USDT:USDT"})

	p := 100.0
	d.OnTicker(context.Background(), types.Ticker{Symbol: "BTC/USDT:USDT", LastPrice: &p})
	clock = 1060
	p2 := 101.0
	d.OnTicker(context.Background(), types.Ticker{Symbol: "BTC/USDT:USDT", LastPrice: &p2})

	d.mu.RLock()
	ser := d.symbols["BTC/USDT:USDT"]
	ret, ok := seriesReturn(ser, clock, 60, 15)
	d.mu.RUnlock()
	if !ok {
		t.Fatal("expected return")
	}
	if math.Abs(ret-1.0) > 0.01 {
		t.Fatalf("ret=%v", ret)
	}

	// pruning
	clock = 1000 + 1000
	d.mu.Lock()
	d.pruneSeriesLocked(ser, clock)
	d.mu.Unlock()
	d.mu.RLock()
	n := len(d.symbols["BTC/USDT:USDT"].points)
	d.mu.RUnlock()
	// retention 900: cut at clock-900=1100; points at 1000 and 1060 both pruned.
	if n != 0 {
		t.Fatalf("expected pruned history, got %d points", n)
	}
}

// ── Snapshot / freshness ───────────────────────────────────────

func TestSnapshot_FreshAndValidGates(t *testing.T) {
	cfg := testMPConfig()
	cfg.MinValidSymbols = 15
	d := NewMarketPulseDetector(cfg)
	var clock float64 = 10_000
	d.SetNowFunc(func() float64 { return clock })

	syms := mpSymbols(20)
	d.UpdateUniverse(syms)

	// Only current prices — no 60s history → invalid
	for _, s := range syms {
		p := 100.0
		d.OnTicker(context.Background(), types.Ticker{Symbol: s, LastPrice: &p})
	}
	snap := d.computeSnapshot(clock)
	if snap.DataOK {
		t.Fatal("expected insufficient history")
	}

	// Stale: history exists but lastSeen too old
	feedUniverse(d, syms, clock-60, clock, 100, 0.3, 0.8)
	// advance clock beyond freshness
	clock += 100
	snap = d.computeSnapshot(clock)
	if snap.DataOK {
		t.Fatalf("expected stale gate, got ok fresh=%v", snap.FreshRatio)
	}
	if snap.GateReason != "insufficient_fresh_data" {
		t.Fatalf("reason=%s", snap.GateReason)
	}
}

func TestSnapshot_BreadthAndLeaders(t *testing.T) {
	cfg := testMPConfig()
	cfg.RequirePrimaryConfirmation = false
	d := NewMarketPulseDetector(cfg)
	var clock float64 = 20_000
	d.SetNowFunc(func() float64 { return clock })
	syms := mpSymbols(20)
	d.UpdateUniverse(syms)
	// Force warmup complete
	d.mu.Lock()
	for _, ser := range d.symbols {
		ser.warmupSince = clock - 10
	}
	d.mu.Unlock()

	feedUniverse(d, syms, clock-60, clock, 100, 0.5, 0.75)
	// ensure warmup
	d.mu.Lock()
	for _, ser := range d.symbols {
		ser.warmupSince = clock - 10
	}
	d.mu.Unlock()

	snap := d.computeSnapshot(clock)
	if !snap.DataOK {
		t.Fatalf("snap gated: %s valid=%d fresh=%v", snap.GateReason, snap.ValidSymbols, snap.FreshRatio)
	}
	if snap.UpBreadth60s < 0.7 {
		t.Fatalf("up breadth=%v", snap.UpBreadth60s)
	}
	if snap.MedianReturn60s <= 0 {
		t.Fatalf("median=%v", snap.MedianReturn60s)
	}
	if len(snap.Leaders) == 0 || len(snap.Laggards) == 0 {
		t.Fatal("expected leaders/laggards")
	}
}

// ── Impulse ────────────────────────────────────────────────────

func TestImpulse_BroadRally3of4(t *testing.T) {
	cfg := testMPConfig()
	// Disable Z hard gate noise for deterministic unit test by using high returns.
	d := NewMarketPulseDetector(cfg)
	var clock float64 = 30_000
	d.SetNowFunc(func() float64 { return clock })
	syms := mpSymbols(20)
	d.UpdateUniverse(syms)
	d.mu.Lock()
	for _, ser := range d.symbols {
		ser.warmupSince = clock - 100
	}
	// Clear any protect
	d.universeChangedAt = 0
	d.mu.Unlock()

	// Seed history at t-60 with flat, then ramp.
	for step := 0; step < 4; step++ {
		clock += 5
		d.SetNowFunc(func() float64 { return clock })
		// past point
		for i, s := range syms {
			p0 := 100.0 * (1 + float64(i)*0.001)
			tsPast := clock - 60
			d.SetNowFunc(func() float64 { return tsPast })
			d.OnTicker(context.Background(), types.Ticker{Symbol: s, LastPrice: &p0})
			// 80% up by 0.25% (above impulse 0.18, below stress 0.35)
			p1 := p0
			if i < 16 { // 16/20 = 0.8
				p1 = p0 * 1.0025
			} else {
				p1 = p0 * 0.9999
			}
			d.SetNowFunc(func() float64 { return clock })
			d.OnTicker(context.Background(), types.Ticker{Symbol: s, LastPrice: &p1})
		}
		d.mu.Lock()
		for _, ser := range d.symbols {
			ser.warmupSince = clock - 100
			// Seed volatility history so Z is meaningful but not blocking
			ser.volSamples = seedVolSamples(0.1)
		}
		d.universeChangedAt = 0
		d.mu.Unlock()
		d.EvaluateAt(clock)
	}

	if d.State() != types.MarketStateImpulseUp {
		t.Fatalf("state=%s want IMPULSE_UP snap=%+v", d.State(), d.LastSnapshot())
	}
	evts := drainEvents(d)
	found := false
	for _, e := range evts {
		if e.EventType == "market_impulse" && e.Symbol == "MARKET" {
			found = true
			if e.Data["direction"] != "up" {
				t.Fatalf("dir=%v", e.Data["direction"])
			}
		}
	}
	if !found {
		t.Fatalf("expected market_impulse event, got %d events", len(evts))
	}
}

func TestImpulse_BelowBreadthNoTrigger(t *testing.T) {
	cfg := testMPConfig()
	d := NewMarketPulseDetector(cfg)
	var clock float64 = 40_000
	d.SetNowFunc(func() float64 { return clock })
	syms := mpSymbols(20)
	d.UpdateUniverse(syms)

	for step := 0; step < 4; step++ {
		clock += 5
		for i, s := range syms {
			p0 := 100.0
			tsPast := clock - 60
			d.SetNowFunc(func() float64 { return tsPast })
			d.OnTicker(context.Background(), types.Ticker{Symbol: s, LastPrice: &p0})
			// only 50% advance — below 65%
			p1 := p0
			if i < 10 {
				p1 = p0 * 1.0025
			}
			d.SetNowFunc(func() float64 { return clock })
			d.OnTicker(context.Background(), types.Ticker{Symbol: s, LastPrice: &p1})
		}
		d.mu.Lock()
		for _, ser := range d.symbols {
			ser.warmupSince = clock - 100
			ser.volSamples = seedVolSamples(0.1)
		}
		d.universeChangedAt = 0
		d.mu.Unlock()
		d.EvaluateAt(clock)
	}
	if d.State() != types.MarketStateQuiet {
		t.Fatalf("state=%s want QUIET", d.State())
	}
}

func TestImpulse_NoPrimaryConfirmation(t *testing.T) {
	cfg := testMPConfig()
	cfg.RequirePrimaryConfirmation = true
	d := NewMarketPulseDetector(cfg)
	var clock float64 = 50_000
	d.SetNowFunc(func() float64 { return clock })
	syms := mpSymbols(20)
	d.UpdateUniverse(syms)

	for step := 0; step < 4; step++ {
		clock += 5
		for _, s := range syms {
			p0 := 100.0
			tsPast := clock - 60
			d.SetNowFunc(func() float64 { return tsPast })
			d.OnTicker(context.Background(), types.Ticker{Symbol: s, LastPrice: &p0})
			p1 := p0 * 1.0025
			// BTC and ETH flat/down
			if s == "BTC/USDT:USDT" || s == "ETH/USDT:USDT" {
				p1 = p0 * 0.999
			}
			d.SetNowFunc(func() float64 { return clock })
			d.OnTicker(context.Background(), types.Ticker{Symbol: s, LastPrice: &p1})
		}
		d.mu.Lock()
		for _, ser := range d.symbols {
			ser.warmupSince = clock - 100
			ser.volSamples = seedVolSamples(0.1)
		}
		d.universeChangedAt = 0
		d.mu.Unlock()
		d.EvaluateAt(clock)
	}
	if d.State() != types.MarketStateQuiet {
		t.Fatalf("state=%s — BTC/ETH must block impulse", d.State())
	}
}

func TestImpulse_DownSymmetric(t *testing.T) {
	cfg := testMPConfig()
	d := NewMarketPulseDetector(cfg)
	var clock float64 = 60_000
	d.SetNowFunc(func() float64 { return clock })
	syms := mpSymbols(20)
	d.UpdateUniverse(syms)

	for step := 0; step < 4; step++ {
		clock += 5
		for i, s := range syms {
			p0 := 100.0
			tsPast := clock - 60
			d.SetNowFunc(func() float64 { return tsPast })
			d.OnTicker(context.Background(), types.Ticker{Symbol: s, LastPrice: &p0})
			p1 := p0
			if i < 16 {
				p1 = p0 * 0.9975 // -0.25%
			} else {
				p1 = p0 * 1.0001
			}
			d.SetNowFunc(func() float64 { return clock })
			d.OnTicker(context.Background(), types.Ticker{Symbol: s, LastPrice: &p1})
		}
		d.mu.Lock()
		for _, ser := range d.symbols {
			ser.warmupSince = clock - 100
			ser.volSamples = seedVolSamples(0.1)
		}
		d.universeChangedAt = 0
		d.mu.Unlock()
		d.EvaluateAt(clock)
	}
	if d.State() != types.MarketStateImpulseDown {
		t.Fatalf("state=%s want IMPULSE_DOWN med=%v downB=%v",
			d.State(), d.LastSnapshot().MedianReturn60s, d.LastSnapshot().DownBreadth60s)
	}
}

func TestImpulse_TwoOfFourNotEnough(t *testing.T) {
	cfg := testMPConfig()
	cfg.Impulse.ConfirmationSamples = 3
	cfg.Impulse.ConfirmationWindowSamples = 4
	d := NewMarketPulseDetector(cfg)
	var clock float64 = 70_000
	d.SetNowFunc(func() float64 { return clock })
	syms := mpSymbols(20)
	d.UpdateUniverse(syms)

	// 2 good samples then 2 flat
	for step := 0; step < 4; step++ {
		clock += 5
		ret := 0.0
		frac := 0.5
		if step < 2 {
			ret = 0.25
			frac = 0.8
		}
		for i, s := range syms {
			p0 := 100.0
			tsPast := clock - 60
			d.SetNowFunc(func() float64 { return tsPast })
			d.OnTicker(context.Background(), types.Ticker{Symbol: s, LastPrice: &p0})
			p1 := p0
			nUp := int(math.Ceil(float64(len(syms)) * frac))
			if ret != 0 && i < nUp {
				p1 = p0 * (1 + ret/100)
			}
			d.SetNowFunc(func() float64 { return clock })
			d.OnTicker(context.Background(), types.Ticker{Symbol: s, LastPrice: &p1})
		}
		d.mu.Lock()
		for _, ser := range d.symbols {
			ser.warmupSince = clock - 100
			ser.volSamples = seedVolSamples(0.1)
		}
		d.universeChangedAt = 0
		d.mu.Unlock()
		d.EvaluateAt(clock)
	}
	if d.State() != types.MarketStateQuiet {
		t.Fatalf("2/4 should not trigger, state=%s", d.State())
	}
}

// ── Trend / decay ──────────────────────────────────────────────

func TestTrend_AfterImpulse(t *testing.T) {
	cfg := testMPConfig()
	cfg.Trend.MinPersistSeconds = 10
	cfg.Trend.ConfirmationSamples = 3
	cfg.Trend.ConfirmationWindowSamples = 3
	cfg.MaxLookupGapSeconds = 30
	// Keep stress from stealing the transition while we exercise trend confirm.
	cfg.Stress.MinMedianReturnPct = 9.0
	cfg.Stress.MinBreadth = 0.99
	d := NewMarketPulseDetector(cfg)
	var clock float64 = 80_000
	d.SetNowFunc(func() float64 { return clock })
	syms := mpSymbols(20)
	d.UpdateUniverse(syms)

	// Build impulse (sub-stress magnitude)
	for step := 0; step < 4; step++ {
		clock += 5
		pushBroad(d, &clock, syms, 0.25, 0.8)
		d.EvaluateAt(clock)
	}
	if d.State() != types.MarketStateImpulseUp {
		t.Fatalf("need impulse first, got %s", d.State())
	}

	// Seed chronological 300s history then evaluate trend confirmations.
	// Base prices at t0, higher prices at t0+300.
	t0 := clock
	for i, s := range syms {
		p0 := 100.0 * (1 + float64(i)*0.001)
		d.SetNowFunc(func() float64 { return t0 })
		d.OnTicker(context.Background(), types.Ticker{Symbol: s, LastPrice: &p0})
	}
	// Intermediate ticks so 60s window also works.
	for _, s := range syms {
		pMid := 100.0 * 1.003
		d.SetNowFunc(func() float64 { return t0 + 240 })
		d.OnTicker(context.Background(), types.Ticker{Symbol: s, LastPrice: &pMid})
	}
	for step := 0; step < 4; step++ {
		clock = t0 + 300 + float64(step*5)
		for i, s := range syms {
			p0 := 100.0 * (1 + float64(i)*0.001)
			pNow := p0 * 1.006
			if i >= 16 {
				pNow = p0 * 1.001
			}
			d.SetNowFunc(func() float64 { return clock })
			d.OnTicker(context.Background(), types.Ticker{Symbol: s, LastPrice: &pNow})
		}
		d.mu.Lock()
		for _, ser := range d.symbols {
			ser.warmupSince = t0 - 100
			ser.volSamples = seedVolSamples(0.1)
		}
		d.universeChangedAt = 0
		d.state = types.MarketStateImpulseUp
		d.activeDir = "up"
		d.stateSince = clock - 30 // persist satisfied
		d.mu.Unlock()
		d.EvaluateAt(clock)
	}

	if d.State() != types.MarketStateTrendingUp {
		t.Fatalf("state=%s want TRENDING_UP med300=%v upB=%v valid=%d",
			d.State(), d.LastSnapshot().MedianReturn300s, d.LastSnapshot().UpBreadth60s, d.LastSnapshot().ValidSymbols)
	}
}

func pushBroad(d *MarketPulseDetector, clock *float64, syms []string, retPct, frac float64) {
	c := *clock
	for i, s := range syms {
		p0 := 100.0
		d.SetNowFunc(func() float64 { return c - 60 })
		d.OnTicker(context.Background(), types.Ticker{Symbol: s, LastPrice: &p0})
		p1 := p0
		nUp := int(math.Ceil(float64(len(syms)) * frac))
		if i < nUp {
			p1 = p0 * (1 + retPct/100)
		} else {
			p1 = p0 * 0.9999
		}
		d.SetNowFunc(func() float64 { return c })
		d.OnTicker(context.Background(), types.Ticker{Symbol: s, LastPrice: &p1})
	}
	d.mu.Lock()
	for _, ser := range d.symbols {
		ser.warmupSince = c - 100
		ser.volSamples = seedVolSamples(0.1)
	}
	d.universeChangedAt = 0
	d.mu.Unlock()
}

func TestDecay_FromTrending(t *testing.T) {
	cfg := testMPConfig()
	cfg.Decay.PersistSeconds = 5
	cfg.Decay.QuietResetSeconds = 60
	d := NewMarketPulseDetector(cfg)
	var clock float64 = 90_000
	d.SetNowFunc(func() float64 { return clock })
	syms := mpSymbols(20)
	d.UpdateUniverse(syms)

	// Force state to TRENDING_UP
	d.mu.Lock()
	d.state = types.MarketStateTrendingUp
	d.stateSince = clock - 200
	d.activeDir = "up"
	d.universeChangedAt = 0
	d.mu.Unlock()

	sawDecay := false
	// Flat market for several samples
	for step := 0; step < 4; step++ {
		clock += 5
		for _, s := range syms {
			p0 := 100.0
			d.SetNowFunc(func() float64 { return clock - 60 })
			d.OnTicker(context.Background(), types.Ticker{Symbol: s, LastPrice: &p0})
			p1 := p0 * 1.0001 // ~0
			d.SetNowFunc(func() float64 { return clock })
			d.OnTicker(context.Background(), types.Ticker{Symbol: s, LastPrice: &p1})
		}
		d.mu.Lock()
		for _, ser := range d.symbols {
			ser.warmupSince = clock - 100
			ser.volSamples = seedVolSamples(0.1)
		}
		d.universeChangedAt = 0
		d.mu.Unlock()
		d.EvaluateAt(clock)
		if d.State() == types.MarketStateDecay {
			sawDecay = true
		}
	}
	if !sawDecay && d.State() != types.MarketStateDecay {
		t.Fatalf("state=%s want DECAY med=%v upB=%v", d.State(), d.LastSnapshot().MedianReturn60s, d.LastSnapshot().UpBreadth60s)
	}
}

func TestNoDirectTrendFlip(t *testing.T) {
	cfg := testMPConfig()
	d := NewMarketPulseDetector(cfg)
	var clock float64 = 100_000
	d.SetNowFunc(func() float64 { return clock })
	syms := mpSymbols(20)
	d.UpdateUniverse(syms)

	d.mu.Lock()
	d.state = types.MarketStateTrendingUp
	d.stateSince = clock - 200
	d.activeDir = "up"
	d.universeChangedAt = 0
	d.mu.Unlock()

	// Strong down impulse samples (sub-stress)
	for step := 0; step < 4; step++ {
		clock += 5
		pushBroadDown(d, clock, syms, 0.25, 0.8)
		d.EvaluateAt(clock)
	}
	st := d.State()
	// Must not be TRENDING_DOWN directly
	if st == types.MarketStateTrendingDown {
		t.Fatal("must not flip directly to TRENDING_DOWN")
	}
	// Expect IMPULSE_DOWN (via DECAY) or DECAY
	if st != types.MarketStateImpulseDown && st != types.MarketStateDecay && st != types.MarketStateStressDown {
		t.Fatalf("state=%s unexpected", st)
	}
}

func pushBroadDown(d *MarketPulseDetector, clock float64, syms []string, retPct, frac float64) {
	for i, s := range syms {
		p0 := 100.0
		d.SetNowFunc(func() float64 { return clock - 60 })
		d.OnTicker(context.Background(), types.Ticker{Symbol: s, LastPrice: &p0})
		p1 := p0
		n := int(math.Ceil(float64(len(syms)) * frac))
		if i < n {
			p1 = p0 * (1 - retPct/100)
		}
		d.SetNowFunc(func() float64 { return clock })
		d.OnTicker(context.Background(), types.Ticker{Symbol: s, LastPrice: &p1})
	}
	d.mu.Lock()
	for _, ser := range d.symbols {
		ser.warmupSince = clock - 100
		ser.volSamples = seedVolSamples(0.1)
	}
	d.universeChangedAt = 0
	d.mu.Unlock()
}

// ── Stress / cooldown / channel ────────────────────────────────

func TestStress_ExtremeMove(t *testing.T) {
	cfg := testMPConfig()
	cfg.Volatility.Enabled = true
	d := NewMarketPulseDetector(cfg)
	var clock float64 = 110_000
	d.SetNowFunc(func() float64 { return clock })
	syms := mpSymbols(20)
	d.UpdateUniverse(syms)

	for step := 0; step < 2; step++ {
		clock += 5
		for i, s := range syms {
			p0 := 100.0
			d.SetNowFunc(func() float64 { return clock - 60 })
			d.OnTicker(context.Background(), types.Ticker{Symbol: s, LastPrice: &p0})
			p1 := p0 * 0.995 // -0.5%, 90% of names
			if i >= 18 {
				p1 = p0
			}
			d.SetNowFunc(func() float64 { return clock })
			d.OnTicker(context.Background(), types.Ticker{Symbol: s, LastPrice: &p1})
		}
		d.mu.Lock()
		for _, ser := range d.symbols {
			ser.warmupSince = clock - 100
			ser.volSamples = seedVolSamples(0.1)
		}
		d.universeChangedAt = 0
		d.mu.Unlock()
		d.EvaluateAt(clock)
	}
	// Stress may require z; with sigma=0.1, z = -0.5/0.1 = -5 → ok
	st := d.State()
	if st != types.MarketStateStressDown && st != types.MarketStateImpulseDown {
		t.Fatalf("state=%s med=%v downB=%v z=%v", st, d.LastSnapshot().MedianReturn60s, d.LastSnapshot().DownBreadth60s, d.LastSnapshot().MedianZ60s)
	}
}

func TestChannelFullNoBlock(t *testing.T) {
	cfg := testMPConfig()
	d := NewMarketPulseDetector(cfg)
	// Fill channel
	for i := 0; i < 64; i++ {
		d.events <- types.AnomalyEvent{EventType: "fill"}
	}
	// emitLocked should not block
	done := make(chan struct{})
	go func() {
		d.mu.Lock()
		d.emitLocked(1, "market_impulse", "up", types.MarketStateQuiet, types.MarketStateImpulseUp, types.MarketSnapshot{DataOK: true})
		d.mu.Unlock()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("emit blocked on full channel")
	}
}

func TestWarmupExcludesNewSymbol(t *testing.T) {
	cfg := testMPConfig()
	cfg.WarmupSeconds = 300
	cfg.MinValidSymbols = 2
	d := NewMarketPulseDetector(cfg)
	var clock float64 = 120_000
	d.SetNowFunc(func() float64 { return clock })
	d.UpdateUniverse([]string{"BTC/USDT:USDT", "ETH/USDT:USDT", "NEW/USDT:USDT"})

	// BTC/ETH fully warmed; NEW just joined
	d.mu.Lock()
	d.symbols["BTC/USDT:USDT"].warmupSince = clock - 400
	d.symbols["ETH/USDT:USDT"].warmupSince = clock - 400
	d.symbols["NEW/USDT:USDT"].warmupSince = clock // not warm
	d.mu.Unlock()

	for _, s := range []string{"BTC/USDT:USDT", "ETH/USDT:USDT", "NEW/USDT:USDT"} {
		p0 := 100.0
		d.SetNowFunc(func() float64 { return clock - 60 })
		d.OnTicker(context.Background(), types.Ticker{Symbol: s, LastPrice: &p0})
		p1 := p0 * 1.003
		d.SetNowFunc(func() float64 { return clock })
		d.OnTicker(context.Background(), types.Ticker{Symbol: s, LastPrice: &p1})
	}
	// restore warmup after OnTicker may have reset
	d.mu.Lock()
	d.symbols["NEW/USDT:USDT"].warmupSince = clock
	d.mu.Unlock()

	snap := d.computeSnapshot(clock)
	// Only 2 valid if NEW excluded — minValid=2 so may be ok
	if snap.ValidSymbols > 2 {
		t.Fatalf("NEW should be excluded, valid=%d", snap.ValidSymbols)
	}
}

func TestRestartFromQuiet(t *testing.T) {
	d := NewMarketPulseDetector(testMPConfig())
	d.mu.Lock()
	d.state = types.MarketStateTrendingUp
	d.mu.Unlock()
	d.Reset()
	if d.State() != types.MarketStateQuiet {
		t.Fatal(d.State())
	}
}

func TestSameSecondTickerUpdate(t *testing.T) {
	d := NewMarketPulseDetector(testMPConfig())
	clock := 130_000.2
	d.SetNowFunc(func() float64 { return clock })
	p1 := 10.0
	d.OnTicker(context.Background(), types.Ticker{Symbol: "X", LastPrice: &p1})
	p2 := 11.0
	d.OnTicker(context.Background(), types.Ticker{Symbol: "X", LastPrice: &p2})
	d.mu.RLock()
	n := len(d.symbols["X"].points)
	last := d.symbols["X"].points[n-1].price
	d.mu.RUnlock()
	if n != 1 {
		t.Fatalf("expected 1 point same second, got %d", n)
	}
	if last != 11 {
		t.Fatalf("price=%v", last)
	}
}

func TestOutcome_TracksHorizonsAndPrecision(t *testing.T) {
	cfg := testMPConfig()
	cfg.Volatility.Enabled = false
	// Make impulse easy so we can focus on outcome tracking.
	cfg.Impulse.MinBreadth = 0.50
	cfg.Impulse.MinMedianReturnPct = 0.10
	cfg.Impulse.ConfirmationSamples = 1
	cfg.Impulse.ConfirmationWindowSamples = 1
	cfg.RequirePrimaryConfirmation = false
	cfg.CooldownSeconds = 0
	d := NewMarketPulseDetector(cfg)

	syms := make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		syms = append(syms, fmt.Sprintf("S%d/USDT:USDT", i))
	}
	syms = append(syms, "BTC/USDT:USDT", "ETH/USDT:USDT")

	clock := 200_000.0
	d.SetNowFunc(func() float64 { return clock })
	d.UpdateUniverse(syms)

	// Warmup baseline prices.
	base := 100.0
	for _, sym := range syms {
		p := base
		d.OnTicker(context.Background(), types.Ticker{Symbol: sym, LastPrice: &p})
	}
	// Advance past warmup with same prices so history exists.
	clock += float64(cfg.WarmupSeconds) + 5
	for _, sym := range syms {
		p := base
		d.OnTicker(context.Background(), types.Ticker{Symbol: sym, LastPrice: &p})
	}
	// Broad rally +0.5%.
	clock += 60
	for i, sym := range syms {
		p := base * 1.005
		if i >= 16 { // a few flat
			p = base
		}
		d.OnTicker(context.Background(), types.Ticker{Symbol: sym, LastPrice: &p})
	}
	d.EvaluateAt(clock)
	if d.State() != types.MarketStateImpulseUp && d.State() != types.MarketStateStressUp {
		// Force-track an outcome if state machine did not fire under synthetic feed.
		d.mu.Lock()
		d.trackOutcomeLocked(clock, "market_impulse", "up")
		d.mu.Unlock()
	}

	d.mu.RLock()
	nPending := len(d.pendingOutcomes)
	d.mu.RUnlock()
	if nPending == 0 {
		t.Fatal("expected pending outcome after impulse")
	}

	// Continue up another ~0.5% over 15 minutes so precision flags trip
	// (impulse needs +0.20% by 5m; trend needs +0.30% by 15m from event prices).
	start := clock
	for _, h := range []float64{60, 180, 300, 900} {
		clock = start + h
		// Linear path: ~+0.27% by 5m, ~+0.80% by 15m relative to event prices.
		add := 0.008 * (h / 900)
		for _, sym := range syms {
			p := base * (1.005 + add)
			d.OnTicker(context.Background(), types.Ticker{Symbol: sym, LastPrice: &p})
		}
		d.EvaluateAt(clock)
	}

	var outcomes []types.AnomalyEvent
	for _, e := range drainEvents(d) {
		if e.EventType == "market_outcome" {
			outcomes = append(outcomes, e)
		}
	}
	if len(outcomes) == 0 {
		t.Fatal("expected market_outcome after 15m")
	}
	o := outcomes[len(outcomes)-1]
	if o.Data["median_return_5m"] == nil || o.Data["median_return_15m"] == nil {
		t.Fatalf("missing horizons: %#v", o.Data)
	}
	if o.Data["impulse_precision"] != true {
		t.Fatalf("expected impulse_precision true, data=%#v", o.Data)
	}
	if mfe, _ := o.Data["mfe"].(float64); mfe <= 0 {
		t.Fatalf("mfe=%v", o.Data["mfe"])
	}
}

// During a data outage (prices stale), pending outcomes must not sample from
// last-known prices: missed horizons are reported as null, and precision is
// false rather than fabricated from a flat return.
func TestOutcome_DataOutageDoesNotFabricateHorizons(t *testing.T) {
	cfg := testMPConfig()
	cfg.Volatility.Enabled = false
	cfg.CooldownSeconds = 0
	d := NewMarketPulseDetector(cfg)

	syms := make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		syms = append(syms, fmt.Sprintf("S%d/USDT:USDT", i))
	}

	clock := 300_000.0
	d.SetNowFunc(func() float64 { return clock })
	d.UpdateUniverse(syms)

	base := 100.0
	for _, sym := range syms {
		p := base
		d.OnTicker(context.Background(), types.Ticker{Symbol: sym, LastPrice: &p})
	}

	// Track an outcome directly, then stop all data (no more ticks).
	d.mu.Lock()
	d.trackOutcomeLocked(clock, "market_impulse", "up")
	d.mu.Unlock()

	// Walk far past every horizon with a dead feed: FreshRatio collapses, so
	// no sampling may happen; the outcome expires with null horizons.
	for _, dt := range []float64{60, 180, 300, 900, 1300} {
		clock = 300_000.0 + dt
		d.EvaluateAt(clock)
	}

	var outcome *types.AnomalyEvent
	for _, e := range drainEvents(d) {
		if e.EventType == "market_outcome" {
			ec := e
			outcome = &ec
		}
	}
	if outcome == nil {
		t.Fatal("expired outcome must still be emitted")
	}
	for _, k := range []string{"median_return_1m", "median_return_5m", "median_return_15m"} {
		if outcome.Data[k] != nil {
			t.Fatalf("stale-data horizon %s must be null, got %v", k, outcome.Data[k])
		}
	}
	if outcome.Data["impulse_precision"] != false || outcome.Data["trend_precision"] != false {
		t.Fatalf("precision must not be fabricated from missing samples: %+v", outcome.Data)
	}
}

// seedVolSamples fills a series with enough non-overlapping 60s return samples
// for seriesSigma to report the requested sigma (RMS of ±sigma is sigma).
func seedVolSamples(sigma float64) []float64 {
	out := make([]float64, 0, volMinSamples+2)
	for i := 0; i < volMinSamples+2; i++ {
		v := sigma
		if i%2 == 1 {
			v = -sigma
		}
		out = append(out, v)
	}
	return out
}
