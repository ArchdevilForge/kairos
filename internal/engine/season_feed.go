package engine

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ArchdevilForge/kairos/internal/cycle"
	"github.com/ArchdevilForge/kairos/internal/exchange"
	"github.com/ArchdevilForge/kairos/internal/opportunity"
	"github.com/ArchdevilForge/kairos/internal/types"
)

const (
	// seasonBTCSymbol is always included in the cross-sectional season universe
	// alongside the engine's tracked watchlist, matching research h1_experiment.py.
	seasonBTCSymbol = "BTC/USDT:USDT"

	// seasonWindowBars is the bounded closed-bar retention per symbol (24 * 5m = 2h).
	seasonWindowBars = cycle.DefaultSeasonSlowBars

	// seasonMinValidSymbols is the cross-sectional symbol floor (<4 -> nil snapshot).
	seasonMinValidSymbols = cycle.DefaultSeasonMinValidSymbols

	// seasonMaxBarStaleness excludes any symbol whose latest closed 5m bar is older
	// than 10 minutes relative to the evaluation timestamp.
	seasonMaxBarStaleness = 10 * time.Minute

	// defaultSeasonPollInterval aligns with opportunity outcome tracking (2m),
	// keeping closed 5m bars well inside the 10m staleness bound.
	defaultSeasonPollInterval = 2 * time.Minute

	defaultSeasonPerSymTimeout = 10 * time.Second
	seasonFetchConcurrency     = 4
)

// seasonFeed maintains a bounded rolling window of the latest 24 closed 5m bars
// (with native QuoteVol) across the engine's live symbol universe ∪ {BTC}.
//
// All errors degrade silently to absent fields; network I/O never holds the
// snapshot lock so ticket evaluation reads never block or slow down.
type seasonFeed struct {
	mu            sync.RWMutex
	barsBySym     map[string][]cycle.SeasonBar5m
	nowFn         func() time.Time
	perSymTimeout time.Duration
	log           *slog.Logger
}

func newSeasonFeed(log *slog.Logger) *seasonFeed {
	if log == nil {
		log = slog.Default()
	}
	return &seasonFeed{
		barsBySym:     make(map[string][]cycle.SeasonBar5m),
		nowFn:         time.Now,
		perSymTimeout: defaultSeasonPerSymTimeout,
		log:           log.With("subcomponent", "season_feed"),
	}
}

func (f *seasonFeed) setNowFunc(fn func() time.Time) {
	if f == nil || fn == nil {
		return
	}
	f.mu.Lock()
	f.nowFn = fn
	f.mu.Unlock()
}

func (f *seasonFeed) now() time.Time {
	f.mu.RLock()
	fn := f.nowFn
	f.mu.RUnlock()
	if fn == nil {
		return time.Now()
	}
	return fn()
}

// buildSeasonUniverse returns the sorted deduplicated union: tracked ∪ {BTC/USDT:USDT}.
func buildSeasonUniverse(tracked []string) []string {
	set := make(map[string]struct{}, len(tracked)+1)
	set[seasonBTCSymbol] = struct{}{}
	for _, raw := range tracked {
		s := strings.TrimSpace(raw)
		if s != "" {
			set[s] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// pruneUniverse reclaims bar windows for symbols that left the tracked universe.
func (f *seasonFeed) pruneUniverse(universe []string) {
	if f == nil {
		return
	}
	keep := make(map[string]struct{}, len(universe))
	for _, s := range universe {
		keep[s] = struct{}{}
	}
	f.mu.Lock()
	for sym := range f.barsBySym {
		if _, ok := keep[sym]; !ok {
			delete(f.barsBySym, sym)
		}
	}
	f.mu.Unlock()
}

// windowCount reports how many symbol windows are currently retained (for tests).
func (f *seasonFeed) windowCount() int {
	if f == nil {
		return 0
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.barsBySym)
}

// poll refreshes the 24-bar closed 5m window for tracked ∪ {BTC} using fetch.
// Errors are logged at Debug and never propagated; network calls run outside f.mu.
func (f *seasonFeed) poll(ctx context.Context, fetch opportunity.OHLCVFetcher, tracked []string) {
	if f == nil || fetch == nil || ctx.Err() != nil {
		return
	}
	universe := buildSeasonUniverse(tracked)
	f.pruneUniverse(universe)

	sem := make(chan struct{}, seasonFetchConcurrency)
	var wg sync.WaitGroup
	for _, sym := range universe {
		if ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case sem <- struct{}{}:
		}
		wg.Add(1)
		sym := sym
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			f.pollSymbol(ctx, fetch, sym)
		}()
	}
	wg.Wait()
}

func (f *seasonFeed) pollSymbol(ctx context.Context, fetch opportunity.OHLCVFetcher, sym string) {
	f.mu.RLock()
	timeout := f.perSymTimeout
	f.mu.RUnlock()
	if timeout <= 0 {
		timeout = defaultSeasonPerSymTimeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	// Request 24 closed bars + 1 forming bar, then drop the forming tail bar
	// following internal/opportunity/enrich.go:172.
	candles, err := fetch.FetchOHLCV(reqCtx, sym, "5m", seasonWindowBars+1, 0)
	cancel()
	if err != nil {
		f.log.Debug("season 5m fetch failed", "symbol", sym, "error", err)
		return
	}
	if len(candles) < 2 {
		return
	}

	ordered := ensureCandlesAscending(candles)
	// Drop the unclosed (forming) last bar, matching enrich.go:172.
	closedCandles := ordered[:len(ordered)-1]

	incoming := make([]cycle.SeasonBar5m, 0, len(closedCandles))
	for _, c := range closedCandles {
		b := cycle.SeasonBar5mFromCandle(c)
		if b.CloseTS > 0 {
			incoming = append(incoming, b)
		}
	}
	if len(incoming) == 0 {
		return
	}

	f.mu.Lock()
	f.barsBySym[sym] = mergeBoundedSeasonBars(f.barsBySym[sym], incoming, seasonWindowBars)
	f.mu.Unlock()
}

func ensureCandlesAscending(candles []types.Candle) []types.Candle {
	sorted := true
	for i := 1; i < len(candles); i++ {
		if candles[i].Timestamp < candles[i-1].Timestamp {
			sorted = false
			break
		}
	}
	if sorted {
		return candles
	}
	cp := make([]types.Candle, len(candles))
	copy(cp, candles)
	sort.Slice(cp, func(i, j int) bool { return cp[i].Timestamp < cp[j].Timestamp })
	return cp
}

// mergeBoundedSeasonBars deduplicates existing + incoming bars by CloseTS (incoming wins),
// sorts ascending by CloseTS, and retains at most maxBars in a compact slice.
func mergeBoundedSeasonBars(existing, incoming []cycle.SeasonBar5m, maxBars int) []cycle.SeasonBar5m {
	byTS := make(map[int64]cycle.SeasonBar5m, len(existing)+len(incoming))
	for _, b := range existing {
		if b.CloseTS > 0 {
			byTS[b.CloseTS] = b
		}
	}
	for _, b := range incoming {
		if b.CloseTS > 0 {
			byTS[b.CloseTS] = b
		}
	}
	merged := make([]cycle.SeasonBar5m, 0, len(byTS))
	for _, b := range byTS {
		merged = append(merged, b)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].CloseTS < merged[j].CloseTS })
	if maxBars > 0 && len(merged) > maxBars {
		merged = merged[len(merged)-maxBars:]
	}
	out := make([]cycle.SeasonBar5m, len(merged))
	copy(out, merged)
	return out
}

// snapshot returns a concurrency-safe copy of the cross-sectional 5m window at f.now().
// If allowed is non-nil, only symbols in allowed are considered (evaluation-time universe).
func (f *seasonFeed) snapshot(allowed []string) map[string][]cycle.SeasonBar5m {
	if f == nil {
		return nil
	}
	return f.snapshotAt(f.now(), allowed)
}

// snapshotAt evaluates the cross-sectional window at asOf:
//   - Only closed bars with CloseTS <= asOf.Unix() are eligible.
//   - Each symbol must have a full 24-bar window.
//   - Latest closed bar must satisfy asOf.Unix() - latest.CloseTS <= 600s (10m).
//   - Fewer than 4 valid symbols -> returns nil so ticket fields stay absent.
func (f *seasonFeed) snapshotAt(asOf time.Time, allowed []string) map[string][]cycle.SeasonBar5m {
	if f == nil {
		return nil
	}
	evalUnix := asOf.Unix()
	maxStaleSec := int64(seasonMaxBarStaleness / time.Second)

	var allowSet map[string]struct{}
	if allowed != nil {
		allowSet = make(map[string]struct{}, len(allowed))
		for _, s := range allowed {
			allowSet[s] = struct{}{}
		}
	}

	f.mu.RLock()
	defer f.mu.RUnlock()

	out := make(map[string][]cycle.SeasonBar5m, len(f.barsBySym))
	for sym, bars := range f.barsBySym {
		if allowSet != nil {
			if _, ok := allowSet[sym]; !ok {
				continue
			}
		}
		if len(bars) < seasonWindowBars {
			continue
		}
		// Causality guard: exclude any bar with CloseTS > evalUnix.
		end := len(bars)
		for end > 0 && bars[end-1].CloseTS > evalUnix {
			end--
		}
		if end < seasonWindowBars {
			continue
		}
		win := bars[end-seasonWindowBars : end]
		latestClose := win[len(win)-1].CloseTS
		if evalUnix-latestClose > maxStaleSec {
			continue
		}
		cp := make([]cycle.SeasonBar5m, seasonWindowBars)
		copy(cp, win)
		out[sym] = cp
	}

	if len(out) < seasonMinValidSymbols {
		return nil
	}
	return out
}

// ── Pipeline integration ───────────────────────────────────────

func (p *Pipeline) seasonTarget() (exchange.Exchange, []string, bool) {
	primary := p.primaryExchange()
	if ex := p.exchanges[primary]; ex != nil {
		return ex, p.symbolsSnapshot(primary), true
	}
	if len(p.exchanges) > 0 {
		names := make([]string, 0, len(p.exchanges))
		for name := range p.exchanges {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if ex := p.exchanges[name]; ex != nil {
				return ex, p.symbolsSnapshot(name), true
			}
		}
	}
	if syms := p.coinglassSymbols(); len(syms) > 0 {
		return nil, syms, true
	}
	return nil, nil, false
}

func (p *Pipeline) pollSeasonFeed(ctx context.Context) {
	if p == nil || p.seasonFeed == nil {
		return
	}
	ex, syms, ok := p.seasonTarget()
	if !ok || ex == nil {
		return
	}
	p.seasonFeed.poll(ctx, ex, syms)
}

func (p *Pipeline) seasonFeedLoop(ctx context.Context) {
	interval := p.seasonPollInterval
	if interval <= 0 {
		interval = defaultSeasonPollInterval
	}
	p.pollSeasonFeed(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.pollSeasonFeed(ctx)
		}
	}
}

func (p *Pipeline) seasonBarsSnapshot() map[string][]cycle.SeasonBar5m {
	if p == nil || p.seasonFeed == nil {
		return nil
	}
	syms := p.symbolsSnapshot(p.primaryExchange())
	if len(syms) == 0 {
		syms = p.coinglassSymbols()
	}
	return p.seasonFeed.snapshot(buildSeasonUniverse(syms))
}
