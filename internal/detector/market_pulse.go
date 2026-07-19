package detector

import (
	"context"
	"log/slog"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
)

const marketPulseSymbol = "MARKET"

// MarketPulseDetector aggregates primary-exchange tickers into a market-level
// state machine (QUIET / IMPULSE / TRENDING / STRESS / DECAY).
//
// OnTicker only updates caches. Snapshot evaluation runs on an interval
// (or via EvaluateAt for tests).
type MarketPulseDetector struct {
	cfg types.MarketPulseConfig
	log *slog.Logger

	mu sync.RWMutex

	symbols  map[string]*mpSymbolSeries
	eligible map[string]struct{}

	state      types.MarketState
	stateSince float64
	// activeDir is "up" or "down" while in impulse/trend/stress.
	activeDir string

	// Rolling confirmation samples for impulse / trend direction.
	impulseSamples []conditionSample
	trendSamples   []conditionSample

	// Decay / quiet bookkeeping.
	decaySince      float64
	decayActive     bool
	quietResetSince float64

	// Universe change protection window.
	universeChangedAt float64

	lastEmitted map[string]float64
	lastSnap    types.MarketSnapshot

	// lastGateLog / lastHeartbeat throttle INFO noise in production shadow.
	lastGateLog    float64
	lastGateReason string
	lastHeartbeat  float64

	events chan types.AnomalyEvent

	// nowFunc is overridable in tests (unix seconds, float).
	nowFunc func() float64
}

type mpPricePoint struct {
	ts    float64
	price float64
}

type mpSymbolSeries struct {
	points      []mpPricePoint
	lastSeen    float64
	warmupSince float64
	// lastRecordedSec buckets to at most one sample per second.
	lastRecordedSec int64
	// EWMA of squared 60s returns (pct^2).
	ewmaVar   float64
	ewmaReady bool
	lastRet60 float64
	hasRet60  bool
}

type conditionSample struct {
	ts  float64
	dir string // "up", "down", or ""
	ok  bool
}

// NewMarketPulseDetector builds a detector from config. Defaults are filled
// for zero-value fields so unit tests can pass partial configs.
func NewMarketPulseDetector(cfg types.MarketPulseConfig) *MarketPulseDetector {
	cfg = normalizeMarketPulseConfig(cfg)
	d := &MarketPulseDetector{
		cfg:         cfg,
		log:         slog.Default().With("detector", "market_pulse"),
		symbols:     make(map[string]*mpSymbolSeries),
		eligible:    make(map[string]struct{}),
		state:       types.MarketStateQuiet,
		lastEmitted: make(map[string]float64),
		events:      make(chan types.AnomalyEvent, 64),
		nowFunc:     func() float64 { return float64(time.Now().UnixMilli()) / 1000 },
	}
	return d
}

func normalizeMarketPulseConfig(cfg types.MarketPulseConfig) types.MarketPulseConfig {
	if cfg.SnapshotIntervalSeconds <= 0 {
		cfg.SnapshotIntervalSeconds = 5
	}
	if cfg.FreshnessSeconds <= 0 {
		cfg.FreshnessSeconds = 10
	}
	if cfg.MaxLookupGapSeconds <= 0 {
		cfg.MaxLookupGapSeconds = 15
	}
	if cfg.WarmupSeconds <= 0 {
		cfg.WarmupSeconds = 300
	}
	if cfg.HistoryRetentionSeconds <= 0 {
		cfg.HistoryRetentionSeconds = 900
	}
	if cfg.MinFreshRatio <= 0 {
		cfg.MinFreshRatio = 0.80
	}
	if cfg.MinValidSymbols <= 0 {
		cfg.MinValidSymbols = 15
	}
	if cfg.NoiseReturnPct <= 0 {
		cfg.NoiseReturnPct = 0.08
	}
	if len(cfg.PrimarySymbols) == 0 {
		cfg.PrimarySymbols = []string{"BTC/USDT:USDT", "ETH/USDT:USDT"}
	}
	if cfg.Volatility.EWMAAlpha <= 0 {
		cfg.Volatility.EWMAAlpha = 0.10
	}
	if cfg.Volatility.FloorPct <= 0 {
		cfg.Volatility.FloorPct = 0.03
	}
	if cfg.Impulse.WindowSeconds <= 0 {
		cfg.Impulse.WindowSeconds = 60
	}
	if cfg.Impulse.MinBreadth <= 0 {
		cfg.Impulse.MinBreadth = 0.65
	}
	if cfg.Impulse.MinMedianReturnPct <= 0 {
		cfg.Impulse.MinMedianReturnPct = 0.18
	}
	if cfg.Impulse.MinMedianZ <= 0 {
		cfg.Impulse.MinMedianZ = 1.5
	}
	if cfg.Impulse.ConfirmationSamples <= 0 {
		cfg.Impulse.ConfirmationSamples = 3
	}
	if cfg.Impulse.ConfirmationWindowSamples <= 0 {
		cfg.Impulse.ConfirmationWindowSamples = 4
	}
	if cfg.Trend.WindowSeconds <= 0 {
		cfg.Trend.WindowSeconds = 300
	}
	if cfg.Trend.MinBreadth <= 0 {
		cfg.Trend.MinBreadth = 0.60
	}
	if cfg.Trend.MinMedianReturnPct <= 0 {
		cfg.Trend.MinMedianReturnPct = 0.45
	}
	if cfg.Trend.MinPersistSeconds <= 0 {
		cfg.Trend.MinPersistSeconds = 180
	}
	if cfg.Trend.ConfirmationSamples <= 0 {
		cfg.Trend.ConfirmationSamples = 5
	}
	if cfg.Trend.ConfirmationWindowSamples <= 0 {
		cfg.Trend.ConfirmationWindowSamples = 6
	}
	if cfg.Stress.WindowSeconds <= 0 {
		cfg.Stress.WindowSeconds = 60
	}
	if cfg.Stress.MinBreadth <= 0 {
		cfg.Stress.MinBreadth = 0.80
	}
	if cfg.Stress.MinMedianReturnPct <= 0 {
		cfg.Stress.MinMedianReturnPct = 0.35
	}
	if cfg.Stress.MinMedianZ <= 0 {
		cfg.Stress.MinMedianZ = 2.5
	}
	if cfg.Decay.MaxBreadth <= 0 {
		cfg.Decay.MaxBreadth = 0.50
	}
	if cfg.Decay.MaxMedianReturnPct <= 0 {
		cfg.Decay.MaxMedianReturnPct = 0.08
	}
	if cfg.Decay.PersistSeconds <= 0 {
		cfg.Decay.PersistSeconds = 60
	}
	if cfg.Decay.QuietResetSeconds <= 0 {
		cfg.Decay.QuietResetSeconds = 60
	}
	if cfg.Leaders.Limit <= 0 {
		cfg.Leaders.Limit = 5
	}
	if cfg.CooldownSeconds <= 0 {
		cfg.CooldownSeconds = 600
	}
	if cfg.StressCooldownSeconds <= 0 {
		cfg.StressCooldownSeconds = 300
	}
	return cfg
}

// Name implements detector naming.
func (d *MarketPulseDetector) Name() string { return "market_pulse" }

// Events returns the anomaly event channel.
func (d *MarketPulseDetector) Events() <-chan types.AnomalyEvent { return d.events }

// State returns the current market state (thread-safe).
func (d *MarketPulseDetector) State() types.MarketState {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.state
}

// LastSnapshot returns the most recent computed snapshot.
func (d *MarketPulseDetector) LastSnapshot() types.MarketSnapshot {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.lastSnap
}

// ShadowMode reports whether the detector is in shadow (log-only delivery) mode.
func (d *MarketPulseDetector) ShadowMode() bool {
	return d.cfg.ShadowMode
}

// Enabled reports whether the detector is active.
func (d *MarketPulseDetector) Enabled() bool {
	return d.cfg.Enabled
}

// Config returns a copy of the active config.
func (d *MarketPulseDetector) Config() types.MarketPulseConfig {
	return d.cfg
}

// Reset clears all series and returns to QUIET.
func (d *MarketPulseDetector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.symbols = make(map[string]*mpSymbolSeries)
	d.eligible = make(map[string]struct{})
	d.state = types.MarketStateQuiet
	d.stateSince = 0
	d.activeDir = ""
	d.impulseSamples = nil
	d.trendSamples = nil
	d.decaySince = 0
	d.decayActive = false
	d.quietResetSince = 0
	d.universeChangedAt = 0
	clear(d.lastEmitted)
	d.lastSnap = types.MarketSnapshot{}
}

// UpdateUniverse replaces the eligible symbol set. New symbols start a warmup
// window; removed symbols leave history until retention pruning.
func (d *MarketPulseDetector) UpdateUniverse(symbols []string) {
	now := d.nowFunc()
	d.mu.Lock()
	defer d.mu.Unlock()

	oldN := len(d.eligible)
	next := make(map[string]struct{}, len(symbols))
	warmup := 0
	for _, s := range symbols {
		if s == "" {
			continue
		}
		next[s] = struct{}{}
		if _, ok := d.eligible[s]; !ok {
			// New eligible member.
			ser := d.symbols[s]
			if ser == nil {
				ser = &mpSymbolSeries{warmupSince: now}
				d.symbols[s] = ser
			} else if ser.warmupSince == 0 {
				ser.warmupSince = now
			}
			warmup++
		}
	}
	d.eligible = next
	// Protect transitions only when replacing a non-empty universe (refresh churn).
	if oldN > 0 {
		d.universeChangedAt = now
	}
	d.log.Info("market universe updated",
		"old", oldN, "new", len(next), "warmup", warmup)
}

// OnTicker records a primary-exchange ticker into the rolling history.
// exchange is accepted for API clarity; caller must already filter to primary.
func (d *MarketPulseDetector) OnTicker(_ context.Context, ticker types.Ticker) {
	if ticker.LastPrice == nil || *ticker.LastPrice <= 0 || ticker.Symbol == "" {
		return
	}
	now := d.nowFunc()
	price := *ticker.LastPrice

	d.mu.Lock()
	defer d.mu.Unlock()

	ser := d.symbols[ticker.Symbol]
	if ser == nil {
		// Accept ticks even before universe marks eligibility so history is ready.
		ser = &mpSymbolSeries{warmupSince: now}
		d.symbols[ticker.Symbol] = ser
	}

	sec := int64(now)
	if len(ser.points) > 0 && now < ser.points[len(ser.points)-1].ts {
		// Out-of-order tick (tests / rare WS reorder): upsert by timestamp order.
		upsertPricePoint(ser, now, price)
	} else if ser.lastRecordedSec == sec && len(ser.points) > 0 {
		// Update last point in the same second.
		ser.points[len(ser.points)-1].price = price
		ser.points[len(ser.points)-1].ts = now
	} else {
		ser.points = append(ser.points, mpPricePoint{ts: now, price: price})
		ser.lastRecordedSec = sec
	}
	if now >= ser.lastSeen {
		ser.lastSeen = now
	}
	if ser.warmupSince == 0 {
		ser.warmupSince = now
	}

	// Update EWMA variance from 60s returns when possible.
	if ret, ok := seriesReturn(ser, now, 60, float64(d.cfg.MaxLookupGapSeconds)); ok {
		alpha := d.cfg.Volatility.EWMAAlpha
		if !ser.ewmaReady {
			ser.ewmaVar = ret * ret
			ser.ewmaReady = true
		} else {
			ser.ewmaVar = alpha*ret*ret + (1-alpha)*ser.ewmaVar
		}
		ser.lastRet60 = ret
		ser.hasRet60 = true
	}

	d.pruneSeriesLocked(ser, now)
}

// Start runs the snapshot evaluation loop until ctx is cancelled.
func (d *MarketPulseDetector) Start(ctx context.Context) {
	if !d.cfg.Enabled {
		return
	}
	interval := time.Duration(d.cfg.SnapshotIntervalSeconds) * time.Second
	t := time.NewTicker(interval)
	defer t.Stop()

	// Initial evaluation after a short delay is fine; wait for first tick.
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.EvaluateAt(d.nowFunc())
		}
	}
}

// EvaluateAt computes a snapshot at the given timestamp and advances the
// state machine. Safe for concurrent use with OnTicker.
func (d *MarketPulseDetector) EvaluateAt(now float64) *types.MarketSnapshot {
	if !d.cfg.Enabled {
		return nil
	}

	snap := d.computeSnapshot(now)

	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastSnap = snap
	d.advanceStateLocked(now, snap)
	return &snap
}

// ── Snapshot computation ───────────────────────────────────────

func (d *MarketPulseDetector) computeSnapshot(now float64) types.MarketSnapshot {
	d.mu.RLock()
	defer d.mu.RUnlock()

	cfg := d.cfg
	snap := types.MarketSnapshot{
		Timestamp:       now,
		UniverseSize:    len(d.eligible),
		EligibleSymbols: len(d.eligible),
	}

	if len(d.eligible) == 0 {
		snap.DataOK = false
		snap.GateReason = "empty_universe"
		return snap
	}

	freshN := 0
	type move struct {
		symbol string
		ret60  float64
		ret180 float64
		ret300 float64
		z60    float64
		ok60   bool
		ok180  bool
		ok300  bool
	}
	var moves []move

	for sym := range d.eligible {
		ser := d.symbols[sym]
		if ser == nil {
			continue
		}
		if now-ser.lastSeen <= float64(cfg.FreshnessSeconds) && lastPrice(ser) > 0 {
			freshN++
		}
		// Warmup gate.
		if now-ser.warmupSince < float64(cfg.WarmupSeconds) {
			continue
		}
		if lastPrice(ser) <= 0 {
			continue
		}
		if now-ser.lastSeen > float64(cfg.FreshnessSeconds) {
			continue
		}

		m := move{symbol: sym}
		if r, ok := seriesReturn(ser, now, 60, float64(cfg.MaxLookupGapSeconds)); ok {
			m.ret60 = r
			m.ok60 = true
			m.z60 = zScore(r, ser, cfg.Volatility)
		}
		if r, ok := seriesReturn(ser, now, 180, float64(cfg.MaxLookupGapSeconds)); ok {
			m.ret180 = r
			m.ok180 = true
		}
		if r, ok := seriesReturn(ser, now, 300, float64(cfg.MaxLookupGapSeconds)); ok {
			m.ret300 = r
			m.ok300 = true
		}
		if m.ok60 {
			moves = append(moves, m)
		}
	}

	snap.FreshRatio = float64(freshN) / float64(len(d.eligible))
	snap.ValidSymbols = len(moves)

	if snap.FreshRatio < cfg.MinFreshRatio {
		snap.DataOK = false
		snap.GateReason = "insufficient_fresh_data"
		return snap
	}
	if snap.ValidSymbols < cfg.MinValidSymbols {
		snap.DataOK = false
		snap.GateReason = "insufficient_valid_symbols"
		return snap
	}

	rets60 := make([]float64, 0, len(moves))
	rets180 := make([]float64, 0, len(moves))
	rets300 := make([]float64, 0, len(moves))
	zs := make([]float64, 0, len(moves))
	noise := cfg.NoiseReturnPct
	adv, dec, neu := 0, 0, 0

	for _, m := range moves {
		rets60 = append(rets60, m.ret60)
		zs = append(zs, m.z60)
		if m.ok180 {
			rets180 = append(rets180, m.ret180)
		}
		if m.ok300 {
			rets300 = append(rets300, m.ret300)
		}
		switch {
		case m.ret60 > noise:
			adv++
		case m.ret60 < -noise:
			dec++
		default:
			neu++
		}
	}

	snap.MedianReturn60s = median(rets60)
	snap.MedianReturn180s = median(rets180)
	snap.MedianReturn300s = median(rets300)
	snap.MedianZ60s = median(zs)
	snap.Advancers = adv
	snap.Decliners = dec
	snap.Neutral = neu
	n := float64(len(moves))
	snap.UpBreadth60s = float64(adv) / n
	snap.DownBreadth60s = float64(dec) / n

	// BTC / ETH returns.
	for _, psym := range cfg.PrimarySymbols {
		if r, ok := d.symbolReturnLocked(psym, now, 60); ok {
			v := r
			switch {
			case isBTC(psym):
				snap.BTCReturn60s = &v
			case isETH(psym):
				snap.ETHReturn60s = &v
			}
		}
	}
	// Also try standard symbols if primary list didn't match.
	if snap.BTCReturn60s == nil {
		if r, ok := d.symbolReturnLocked("BTC/USDT:USDT", now, 60); ok {
			v := r
			snap.BTCReturn60s = &v
		}
	}
	if snap.ETHReturn60s == nil {
		if r, ok := d.symbolReturnLocked("ETH/USDT:USDT", now, 60); ok {
			v := r
			snap.ETHReturn60s = &v
		}
	}

	// Leaders / laggards by relative return vs median.
	type ranked struct {
		sym string
		ret float64
		rel float64
		z   float64
	}
	rankedMoves := make([]ranked, 0, len(moves))
	for _, m := range moves {
		rankedMoves = append(rankedMoves, ranked{
			sym: m.symbol,
			ret: m.ret60,
			rel: m.ret60 - snap.MedianReturn60s,
			z:   m.z60,
		})
	}
	sort.Slice(rankedMoves, func(i, j int) bool {
		return rankedMoves[i].rel > rankedMoves[j].rel
	})
	limit := cfg.Leaders.Limit
	if limit > len(rankedMoves) {
		limit = len(rankedMoves)
	}
	for i := 0; i < limit; i++ {
		r := rankedMoves[i]
		snap.Leaders = append(snap.Leaders, types.SymbolMove{
			Symbol: r.sym, ReturnPct: round(r.ret, 4), RelativePct: round(r.rel, 4), ZScore: round(r.z, 4),
		})
	}
	for i := 0; i < limit; i++ {
		r := rankedMoves[len(rankedMoves)-1-i]
		snap.Laggards = append(snap.Laggards, types.SymbolMove{
			Symbol: r.sym, ReturnPct: round(r.ret, 4), RelativePct: round(r.rel, 4), ZScore: round(r.z, 4),
		})
	}

	snap.DataOK = true
	return snap
}

func (d *MarketPulseDetector) symbolReturnLocked(sym string, now float64, window int) (float64, bool) {
	ser := d.symbols[sym]
	if ser == nil {
		return 0, false
	}
	return seriesReturn(ser, now, window, float64(d.cfg.MaxLookupGapSeconds))
}

// ── State machine ──────────────────────────────────────────────

func (d *MarketPulseDetector) advanceStateLocked(now float64, snap types.MarketSnapshot) {
	if !snap.DataOK {
		// Throttle: log on reason change or every 60s (avoid 5s spam).
		if snap.GateReason != d.lastGateReason || now-d.lastGateLog >= 60 {
			d.log.Info("market snapshot gated", "reason", snap.GateReason,
				"fresh_ratio", round(snap.FreshRatio, 3),
				"valid_symbols", snap.ValidSymbols)
			d.lastGateLog = now
			d.lastGateReason = snap.GateReason
		}
		return
	}
	d.lastGateReason = ""

	// Suppress state transitions solely driven by universe membership churn.
	if d.universeChangedAt > 0 && now-d.universeChangedAt < 60 {
		// Still record samples so confirmation can build after the window.
		d.recordSamplesLocked(now, snap)
		d.log.Debug("market snapshot during universe protect",
			"state", d.state, "fresh_ratio", snap.FreshRatio)
		return
	}

	d.recordSamplesLocked(now, snap)

	// Shadow-friendly heartbeat so operators can see live metrics without DEBUG.
	if now-d.lastHeartbeat >= 60 {
		d.lastHeartbeat = now
		d.log.Info("market snapshot ok",
			"state", d.state,
			"valid", snap.ValidSymbols,
			"fresh", round(snap.FreshRatio, 3),
			"med60", round(snap.MedianReturn60s, 4),
			"upB", round(snap.UpBreadth60s, 3),
			"downB", round(snap.DownBreadth60s, 3),
			"shadow", d.cfg.ShadowMode,
		)
	}

	impulseDir, impulseOK := d.impulseConfirmedLocked()
	trendDir, trendOK := d.trendConfirmedLocked(now, snap)
	stressDir, stressOK := stressCondition(snap, d.cfg)
	decayOK := d.decayConditionLocked(now, snap)

	from := d.state

	// Stress can overlay / take priority for event emission.
	if stressOK {
		to := types.MarketStateStressUp
		if stressDir == "down" {
			to = types.MarketStateStressDown
		}
		// From trending reverse path: force through DECAY first if opposite.
		if isTrending(from) && oppositeDir(d.activeDir, stressDir) {
			d.transitionLocked(now, types.MarketStateDecay, "", snap, "market_decay", false)
			d.transitionLocked(now, stateForImpulse(stressDir), stressDir, snap, "market_impulse", true)
			d.maybeEmitStressLocked(now, stressDir, snap)
			return
		}
		if from != to {
			// Entering stress from quiet uses impulse semantics first if not already active.
			if from == types.MarketStateQuiet || from == types.MarketStateDecay {
				d.transitionLocked(now, stateForImpulse(stressDir), stressDir, snap, "market_impulse", true)
			}
			d.transitionLocked(now, to, stressDir, snap, "market_stress", true)
		} else {
			d.maybeEmitStressLocked(now, stressDir, snap)
		}
		return
	}

	switch from {
	case types.MarketStateQuiet, types.MarketStateDecay:
		if impulseOK {
			// From DECAY, allow impulse in either direction.
			d.transitionLocked(now, stateForImpulse(impulseDir), impulseDir, snap, "market_impulse", true)
			return
		}
		if from == types.MarketStateDecay {
			// Reset to QUIET after quiet period without impulse.
			if d.quietResetSince == 0 {
				d.quietResetSince = now
			} else if now-d.quietResetSince >= float64(d.cfg.Decay.QuietResetSeconds) {
				d.transitionLocked(now, types.MarketStateQuiet, "", snap, "", false)
			}
		}

	case types.MarketStateImpulseUp, types.MarketStateImpulseDown:
		// Opposite strong impulse → decay then reverse.
		if impulseOK && oppositeDir(d.activeDir, impulseDir) {
			d.transitionLocked(now, types.MarketStateDecay, "", snap, "market_decay", d.cfg.Decay.Notify)
			d.transitionLocked(now, stateForImpulse(impulseDir), impulseDir, snap, "market_impulse", true)
			return
		}
		if trendOK && trendDir == d.activeDir {
			d.transitionLocked(now, stateForTrend(trendDir), trendDir, snap, "market_trend", true)
			return
		}
		// Fade back if impulse conditions lost for a while.
		if decayOK {
			d.transitionLocked(now, types.MarketStateDecay, "", snap, "market_decay", d.cfg.Decay.Notify)
		}

	case types.MarketStateTrendingUp, types.MarketStateTrendingDown:
		if impulseOK && oppositeDir(d.activeDir, impulseDir) {
			d.transitionLocked(now, types.MarketStateDecay, "", snap, "market_decay", d.cfg.Decay.Notify)
			d.transitionLocked(now, stateForImpulse(impulseDir), impulseDir, snap, "market_impulse", true)
			return
		}
		if decayOK {
			d.transitionLocked(now, types.MarketStateDecay, "", snap, "market_decay", d.cfg.Decay.Notify)
		}

	case types.MarketStateStressUp, types.MarketStateStressDown:
		// Leave stress into trend/decay based on current conditions.
		if impulseOK && oppositeDir(d.activeDir, impulseDir) {
			d.transitionLocked(now, types.MarketStateDecay, "", snap, "market_decay", d.cfg.Decay.Notify)
			d.transitionLocked(now, stateForImpulse(impulseDir), impulseDir, snap, "market_impulse", true)
			return
		}
		if trendOK && trendDir == d.activeDir {
			d.transitionLocked(now, stateForTrend(trendDir), trendDir, snap, "market_trend", true)
			return
		}
		if decayOK {
			d.transitionLocked(now, types.MarketStateDecay, "", snap, "market_decay", d.cfg.Decay.Notify)
			return
		}
		// Soften stress → impulse same direction when stress no longer holds.
		if !stressOK {
			d.transitionLocked(now, stateForImpulse(d.activeDir), d.activeDir, snap, "", false)
		}
	}

	_ = from
}

func (d *MarketPulseDetector) recordSamplesLocked(now float64, snap types.MarketSnapshot) {
	dir, ok := impulseRawCondition(snap, d.cfg)
	d.impulseSamples = append(d.impulseSamples, conditionSample{ts: now, dir: dir, ok: ok})
	maxImp := d.cfg.Impulse.ConfirmationWindowSamples
	if len(d.impulseSamples) > maxImp*2 {
		d.impulseSamples = d.impulseSamples[len(d.impulseSamples)-maxImp:]
	}

	tdir, tok := trendRawCondition(snap, d.cfg)
	d.trendSamples = append(d.trendSamples, conditionSample{ts: now, dir: tdir, ok: tok})
	maxTr := d.cfg.Trend.ConfirmationWindowSamples
	if len(d.trendSamples) > maxTr*2 {
		d.trendSamples = d.trendSamples[len(d.trendSamples)-maxTr:]
	}

	// Decay tracking.
	if isTrending(d.state) || isImpulse(d.state) {
		if decayRawCondition(snap, d.cfg, d.activeDir) {
			if !d.decayActive {
				d.decayActive = true
				d.decaySince = now
			}
		} else {
			d.decayActive = false
			d.decaySince = 0
		}
	}
}

func (d *MarketPulseDetector) impulseConfirmedLocked() (string, bool) {
	win := d.cfg.Impulse.ConfirmationWindowSamples
	need := d.cfg.Impulse.ConfirmationSamples
	samples := d.impulseSamples
	if len(samples) < need {
		return "", false
	}
	if len(samples) > win {
		samples = samples[len(samples)-win:]
	}
	up, down := 0, 0
	for _, s := range samples {
		if !s.ok {
			continue
		}
		switch s.dir {
		case "up":
			up++
		case "down":
			down++
		}
	}
	if up >= need && up >= down {
		d.log.Info("market condition met", "direction", "up",
			"confirmations", up, "window", len(samples))
		return "up", true
	}
	if down >= need && down > up {
		d.log.Info("market condition met", "direction", "down",
			"confirmations", down, "window", len(samples))
		return "down", true
	}
	return "", false
}

func (d *MarketPulseDetector) trendConfirmedLocked(now float64, snap types.MarketSnapshot) (string, bool) {
	if d.stateSince > 0 && now-d.stateSince < float64(d.cfg.Trend.MinPersistSeconds) {
		return "", false
	}
	// Must currently satisfy trend raw condition.
	dir, ok := trendRawCondition(snap, d.cfg)
	if !ok {
		return "", false
	}
	win := d.cfg.Trend.ConfirmationWindowSamples
	need := d.cfg.Trend.ConfirmationSamples
	samples := d.trendSamples
	if len(samples) < need {
		return "", false
	}
	if len(samples) > win {
		samples = samples[len(samples)-win:]
	}
	count := 0
	for _, s := range samples {
		if s.ok && s.dir == dir {
			count++
		}
	}
	if count >= need {
		return dir, true
	}
	return "", false
}

func (d *MarketPulseDetector) decayConditionLocked(now float64, snap types.MarketSnapshot) bool {
	if !d.cfg.Decay.Enabled {
		return false
	}
	if !isImpulse(d.state) && !isTrending(d.state) && !isStress(d.state) {
		return false
	}
	if !d.decayActive {
		return false
	}
	return now-d.decaySince >= float64(d.cfg.Decay.PersistSeconds)
}

func (d *MarketPulseDetector) transitionLocked(
	now float64,
	to types.MarketState,
	dir string,
	snap types.MarketSnapshot,
	eventType string,
	emit bool,
) {
	from := d.state
	if from == to {
		return
	}
	d.state = to
	d.stateSince = now
	if dir != "" {
		d.activeDir = dir
	}
	if to == types.MarketStateQuiet || to == types.MarketStateDecay {
		if to == types.MarketStateQuiet {
			d.activeDir = ""
			d.quietResetSince = 0
		}
		d.decayActive = false
		d.decaySince = 0
		if to == types.MarketStateDecay {
			d.quietResetSince = now
		}
	}

	d.log.Info("market state changed",
		"from", from, "to", to, "direction", dir,
		"median_return_60s", round(snap.MedianReturn60s, 4),
		"breadth_up", round(snap.UpBreadth60s, 3),
		"breadth_down", round(snap.DownBreadth60s, 3),
		"shadow", d.cfg.ShadowMode,
	)

	if !emit || eventType == "" {
		return
	}
	// Decay notify is optional.
	if eventType == "market_decay" && !d.cfg.Decay.Notify {
		d.log.Info("market event gated", "reason", "decay_notify_disabled", "event", eventType)
		return
	}
	d.emitLocked(now, eventType, dir, from, to, snap)
}

func (d *MarketPulseDetector) maybeEmitStressLocked(now float64, dir string, snap types.MarketSnapshot) {
	key := "market_stress:" + dir
	last := d.lastEmitted[key]
	if last > 0 && now-last < float64(d.cfg.StressCooldownSeconds) {
		d.log.Info("market event gated", "reason", "cooldown", "event", "market_stress")
		return
	}
	d.emitLocked(now, "market_stress", dir, d.state, d.state, snap)
}

func (d *MarketPulseDetector) emitLocked(
	now float64,
	eventType, dir string,
	from, to types.MarketState,
	snap types.MarketSnapshot,
) {
	// Cooldown for same event+direction unless this is a real state change
	// to a different state (state-change events bypass same-state cooldown).
	key := eventType + ":" + dir
	last := d.lastEmitted[key]
	cd := float64(d.cfg.CooldownSeconds)
	if eventType == "market_stress" {
		cd = float64(d.cfg.StressCooldownSeconds)
	}
	if from == to && last > 0 && now-last < cd {
		d.log.Info("market event gated", "reason", "cooldown", "event", eventType)
		return
	}
	// Also gate identical re-emits within cooldown even on state change of same type.
	if last > 0 && now-last < cd && from == to {
		d.log.Info("market event gated", "reason", "cooldown", "event", eventType)
		return
	}

	breadth := snap.UpBreadth60s
	if dir == "down" {
		breadth = snap.DownBreadth60s
	}

	leaders := make([]string, 0, len(snap.Leaders))
	for _, m := range snap.Leaders {
		leaders = append(leaders, m.Symbol)
	}
	laggards := make([]string, 0, len(snap.Laggards))
	for _, m := range snap.Laggards {
		laggards = append(laggards, m.Symbol)
	}

	data := map[string]any{
		"direction":              dir,
		"state_from":             string(from),
		"state_to":               string(to),
		"window_seconds":         d.cfg.Impulse.WindowSeconds,
		"median_return_60s_pct":  round(snap.MedianReturn60s, 4),
		"median_return_300s_pct": round(snap.MedianReturn300s, 4),
		"median_z_60s":           round(snap.MedianZ60s, 4),
		"breadth":                round(breadth, 4),
		"up_breadth":             round(snap.UpBreadth60s, 4),
		"down_breadth":           round(snap.DownBreadth60s, 4),
		"advancers":              snap.Advancers,
		"decliners":              snap.Decliners,
		"neutral":                snap.Neutral,
		"valid_symbols":          snap.ValidSymbols,
		"fresh_ratio":            round(snap.FreshRatio, 4),
		"leaders":                leaders,
		"laggards":               laggards,
		"shadow_mode":            d.cfg.ShadowMode,
	}
	if snap.BTCReturn60s != nil {
		data["btc_return_pct"] = round(*snap.BTCReturn60s, 4)
	}
	if snap.ETHReturn60s != nil {
		data["eth_return_pct"] = round(*snap.ETHReturn60s, 4)
	}

	sev := types.SeverityHigh
	if eventType == "market_trend" {
		sev = types.SeverityMedium
	}
	if eventType == "market_decay" {
		sev = types.SeverityLow
	}

	evt := types.AnomalyEvent{
		Symbol:    marketPulseSymbol,
		EventType: eventType,
		Severity:  sev,
		Data:      data,
		Timestamp: now,
	}

	select {
	case d.events <- evt:
		d.lastEmitted[key] = now
		d.log.Info("market event emitted",
			"event", eventType, "direction", dir,
			"from", from, "to", to, "shadow", d.cfg.ShadowMode)
	default:
		d.log.Warn("market_pulse event channel full, dropping event",
			"event", eventType)
	}
}

// ── Condition helpers ──────────────────────────────────────────

func impulseRawCondition(snap types.MarketSnapshot, cfg types.MarketPulseConfig) (string, bool) {
	if !snap.DataOK {
		return "", false
	}
	upOK := snap.UpBreadth60s >= cfg.Impulse.MinBreadth &&
		snap.MedianReturn60s >= cfg.Impulse.MinMedianReturnPct
	downOK := snap.DownBreadth60s >= cfg.Impulse.MinBreadth &&
		snap.MedianReturn60s <= -cfg.Impulse.MinMedianReturnPct

	if cfg.Volatility.Enabled {
		if upOK && snap.MedianZ60s < cfg.Impulse.MinMedianZ {
			upOK = false
		}
		if downOK && snap.MedianZ60s > -cfg.Impulse.MinMedianZ {
			downOK = false
		}
	}

	if cfg.RequirePrimaryConfirmation {
		if upOK && !primaryConfirms(snap, "up") {
			upOK = false
		}
		if downOK && !primaryConfirms(snap, "down") {
			downOK = false
		}
	}

	switch {
	case upOK && !downOK:
		return "up", true
	case downOK && !upOK:
		return "down", true
	case upOK && downOK:
		// Prefer the side with larger median magnitude.
		if snap.MedianReturn60s >= 0 {
			return "up", true
		}
		return "down", true
	default:
		return "", false
	}
}

func trendRawCondition(snap types.MarketSnapshot, cfg types.MarketPulseConfig) (string, bool) {
	if !snap.DataOK {
		return "", false
	}
	upOK := snap.UpBreadth60s >= cfg.Trend.MinBreadth &&
		snap.MedianReturn300s >= cfg.Trend.MinMedianReturnPct
	downOK := snap.DownBreadth60s >= cfg.Trend.MinBreadth &&
		snap.MedianReturn300s <= -cfg.Trend.MinMedianReturnPct
	switch {
	case upOK && !downOK:
		return "up", true
	case downOK && !upOK:
		return "down", true
	default:
		return "", false
	}
}

func stressCondition(snap types.MarketSnapshot, cfg types.MarketPulseConfig) (string, bool) {
	if !snap.DataOK {
		return "", false
	}
	upOK := snap.UpBreadth60s >= cfg.Stress.MinBreadth &&
		snap.MedianReturn60s >= cfg.Stress.MinMedianReturnPct
	downOK := snap.DownBreadth60s >= cfg.Stress.MinBreadth &&
		snap.MedianReturn60s <= -cfg.Stress.MinMedianReturnPct
	if cfg.Volatility.Enabled {
		if upOK && snap.MedianZ60s < cfg.Stress.MinMedianZ {
			upOK = false
		}
		if downOK && snap.MedianZ60s > -cfg.Stress.MinMedianZ {
			downOK = false
		}
	}
	switch {
	case upOK && !downOK:
		return "up", true
	case downOK && !upOK:
		return "down", true
	default:
		return "", false
	}
}

func decayRawCondition(snap types.MarketSnapshot, cfg types.MarketPulseConfig, activeDir string) bool {
	if !snap.DataOK {
		return false
	}
	// Breadth collapse in active direction, or median near zero, or reverse breadth high.
	var activeBreadth float64
	var reverseBreadth float64
	switch activeDir {
	case "up":
		activeBreadth = snap.UpBreadth60s
		reverseBreadth = snap.DownBreadth60s
	case "down":
		activeBreadth = snap.DownBreadth60s
		reverseBreadth = snap.UpBreadth60s
	default:
		activeBreadth = math.Max(snap.UpBreadth60s, snap.DownBreadth60s)
		reverseBreadth = math.Min(snap.UpBreadth60s, snap.DownBreadth60s)
	}
	if activeBreadth < cfg.Decay.MaxBreadth {
		return true
	}
	if math.Abs(snap.MedianReturn60s) <= cfg.Decay.MaxMedianReturnPct {
		return true
	}
	if reverseBreadth > 0.45 {
		return true
	}
	return false
}

func primaryConfirms(snap types.MarketSnapshot, dir string) bool {
	check := func(v *float64) bool {
		if v == nil {
			return false
		}
		if dir == "up" {
			return *v > 0
		}
		return *v < 0
	}
	return check(snap.BTCReturn60s) || check(snap.ETHReturn60s)
}

func stateForImpulse(dir string) types.MarketState {
	if dir == "down" {
		return types.MarketStateImpulseDown
	}
	return types.MarketStateImpulseUp
}

func stateForTrend(dir string) types.MarketState {
	if dir == "down" {
		return types.MarketStateTrendingDown
	}
	return types.MarketStateTrendingUp
}

func isImpulse(s types.MarketState) bool {
	return s == types.MarketStateImpulseUp || s == types.MarketStateImpulseDown
}

func isTrending(s types.MarketState) bool {
	return s == types.MarketStateTrendingUp || s == types.MarketStateTrendingDown
}

func isStress(s types.MarketState) bool {
	return s == types.MarketStateStressUp || s == types.MarketStateStressDown
}

func oppositeDir(a, b string) bool {
	return a != "" && b != "" && a != b
}

// ── Series helpers ─────────────────────────────────────────────

func lastPrice(ser *mpSymbolSeries) float64 {
	if ser == nil || len(ser.points) == 0 {
		return 0
	}
	return ser.points[len(ser.points)-1].price
}

// seriesReturn returns pct return over windowSeconds ending at now.
func seriesReturn(ser *mpSymbolSeries, now float64, windowSeconds int, maxGap float64) (float64, bool) {
	if ser == nil || len(ser.points) == 0 {
		return 0, false
	}
	// Current price: latest point at or before now (not necessarily last append).
	cur, ok := lookupPrice(ser.points, now, maxGap)
	if !ok || cur <= 0 {
		// Allow current point slightly ahead of now within 1s for float noise.
		cur, ok = lookupPrice(ser.points, now+1, maxGap+1)
		if !ok || cur <= 0 {
			return 0, false
		}
	}
	target := now - float64(windowSeconds)
	past, ok := lookupPrice(ser.points, target, maxGap)
	if !ok || past <= 0 {
		return 0, false
	}
	return (cur/past - 1) * 100, true
}

// lookupPrice finds the price at or before target within maxGap (full scan).
func lookupPrice(points []mpPricePoint, target, maxGap float64) (float64, bool) {
	var (
		best   float64
		bestTS float64
		found  bool
	)
	for _, p := range points {
		if p.ts <= target && (!found || p.ts >= bestTS) {
			best = p.price
			bestTS = p.ts
			found = true
		}
	}
	if !found {
		return 0, false
	}
	if target-bestTS > maxGap {
		return 0, false
	}
	return best, true
}

// upsertPricePoint inserts or updates a point keeping timestamps sorted.
func upsertPricePoint(ser *mpSymbolSeries, ts, price float64) {
	for i, p := range ser.points {
		if int64(p.ts) == int64(ts) {
			ser.points[i].price = price
			ser.points[i].ts = ts
			return
		}
		if p.ts > ts {
			ser.points = append(ser.points, mpPricePoint{})
			copy(ser.points[i+1:], ser.points[i:])
			ser.points[i] = mpPricePoint{ts: ts, price: price}
			return
		}
	}
	ser.points = append(ser.points, mpPricePoint{ts: ts, price: price})
	ser.lastRecordedSec = int64(ts)
}

func (d *MarketPulseDetector) pruneSeriesLocked(ser *mpSymbolSeries, now float64) {
	cut := now - float64(d.cfg.HistoryRetentionSeconds)
	if len(ser.points) == 0 {
		return
	}
	i := 0
	for i < len(ser.points) && ser.points[i].ts < cut {
		i++
	}
	if i > 0 {
		ser.points = append([]mpPricePoint(nil), ser.points[i:]...)
	}
}

func zScore(ret float64, ser *mpSymbolSeries, vol types.MarketPulseVolatilityConfig) float64 {
	floor := vol.FloorPct
	if floor <= 0 {
		floor = 0.03
	}
	sigma := floor
	if ser != nil && ser.ewmaReady && ser.ewmaVar > 0 {
		sigma = math.Sqrt(ser.ewmaVar)
		if sigma < floor {
			sigma = floor
		}
	}
	return ret / sigma
}

// median returns the median of a float slice (NaN/Inf filtered).
func median(vals []float64) float64 {
	clean := make([]float64, 0, len(vals))
	for _, v := range vals {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		clean = append(clean, v)
	}
	n := len(clean)
	if n == 0 {
		return 0
	}
	sort.Float64s(clean)
	if n%2 == 1 {
		return clean[n/2]
	}
	return (clean[n/2-1] + clean[n/2]) / 2
}

func isBTC(sym string) bool {
	return sym == "BTC/USDT:USDT" || sym == "BTC/USDT" || sym == "BTC"
}

func isETH(sym string) bool {
	return sym == "ETH/USDT:USDT" || sym == "ETH/USDT" || sym == "ETH"
}

// SetNowFunc overrides the clock (tests only).
func (d *MarketPulseDetector) SetNowFunc(fn func() float64) {
	if fn != nil {
		d.nowFunc = fn
	}
}
