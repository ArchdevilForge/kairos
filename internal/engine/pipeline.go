// Pipeline orchestrates the data flow:
// exchanges → tickers → detectors → resonance scorer → Telegram
//
// Key goroutines:
//  1. WS reader (1 per exchange): reads from exchange WS, sends tickers to detector input
//  2. Ticker fan-out (1 per exchange): reads ticker channel, routes to per-exchange detectors
//  3. Futures metrics poller: periodic REST polling → FuturesMetricsDetector
//  4. CoinGlass poller (L/S): periodic polling → LongShortRatioDetector
//  5. CoinGlass poller (liq): periodic polling → LiquidationDetector
//  6. Event aggregator: reads all detector event channels, feeds resonance scorer + delivery
//  7. Resonance deliverer: reads ResonanceEvent channel, sends alerts to Telegram
//  8. Telegram deliverer: reads delivery channel, applies dedup + alert policy, sends
//
// Graceful shutdown with context cancellation.
// Uses errgroup for goroutine management.
package engine

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ArchdevilForge/kairos/internal/data"
	"github.com/ArchdevilForge/kairos/internal/detector"
	"github.com/ArchdevilForge/kairos/internal/exchange"
	"github.com/ArchdevilForge/kairos/internal/notify"
	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
	"github.com/ArchdevilForge/kairos/internal/utils"
	"golang.org/x/sync/errgroup"
)

// exchangeNew creates exchange adapters; overridden in tests.
var exchangeNew = exchange.New

// ────────────────────────────────────────────────────────────────
// Pipeline
// ────────────────────────────────────────────────────────────────

// Pipeline orchestrates exchange WebSocket feeds → detectors → Delivery Channels.
type Pipeline struct {
	cfg *types.Config
	log *slog.Logger

	// Exchanges keyed by name
	exchanges map[string]exchange.Exchange

	// Symbols per exchange (discovered at start)
	symbolsByExchange map[string][]string

	// Per-exchange states (holds detectors and ticker channels)
	exchangeStates map[string]*exchangeState

	// Cross-exchange detectors (CoinGlass-polled)
	longShortDet *detector.LongShortRatioDetector
	liqDet       *detector.LiquidationDetector

	// Resonance scorer (aggregates all detectors)
	resonanceScorer *detector.ResonanceScorer

	// Market-level cross-sectional detector (primary exchange only).
	marketPulseDet *detector.MarketPulseDetector

	// Delivery channels (optional, independent).
	tg       *notify.TelegramClient
	dingTalk *notify.DingTalkClient

	// Blacklist
	blacklist *utils.Blacklist

	// Watch hints for scanner boosting (cross-runtime).
	hints *storage.HintStore

	// Market pulse event log (JSONL; works in shadow mode).
	mpStore *storage.MarketPulseStore

	// Dedup state: "symbol__event_type" → timestamp
	dedupMu   sync.Mutex
	dedupLast map[string]float64

	// Config-derived thresholds (cached from alertPolicy)
	allowedEventTypes     map[string]bool // nil means all allowed
	minSeverityRank       int
	minPriceChangePct     float64
	minVolumeRatio        float64
	minOIChangePct        float64
	minFundingRateAbs     float64
	minFundingRateChange  float64
	dedupWindowSeconds    float64
	symbolCooldownSeconds float64

	// Cached CoinGlass market caps (BTC-denominated weight reference).
	liqMu           sync.RWMutex
	marketCapByCoin map[string]float64
	marketCapRefUSD float64

	// symMu guards symbolsByExchange: refreshLoop writes while WS
	// subscription, metrics and CoinGlass pollers read snapshots.
	symMu sync.RWMutex

	// refreshInterval overrides cfg.DataManager.RefreshIntervalHours when
	// non-zero (test seam for fast refresh ticks).
	refreshInterval time.Duration

	// Lifecycle
	cancel context.CancelFunc
}

// exchangeState holds per-exchange state: the exchange handle and the
// real-time + periodic detectors registered for it. The live symbol universe
// is read from Pipeline.symbolsSnapshot so refreshes reach every consumer.
type exchangeState struct {
	name string
	ex   exchange.Exchange

	// Ticker channel from WS reader, consumed by fan-out goroutine
	tickerCh chan types.Ticker

	// resubCh signals the subscription loop that the universe changed and the
	// WS feed must be restarted with the new symbol list (buffered, len 1).
	resubCh chan struct{}

	// Real-time detectors
	velocity *detector.PriceVelocityDetector
	spike    *detector.VolumeSpikeDetector

	// Periodic futures metrics detector
	metrics *detector.FuturesMetricsDetector
}

// NewPipeline creates a Pipeline from config and optional Telegram client.
// When no Delivery Channel is configured, gated events are not sent.
// Call SetDingTalk to attach a DingTalk mirror client.
func NewPipeline(cfg *types.Config, tg *notify.TelegramClient) *Pipeline {
	log := slog.Default().With("component", "pipeline")

	p := &Pipeline{
		cfg:                   cfg,
		log:                   log,
		exchanges:             make(map[string]exchange.Exchange),
		symbolsByExchange:     make(map[string][]string),
		exchangeStates:        make(map[string]*exchangeState),
		dedupLast:             make(map[string]float64),
		marketCapByCoin:       make(map[string]float64),
		tg:                    tg,
		blacklist:             utils.NewBlacklist(),
		dedupWindowSeconds:    5,
		symbolCooldownSeconds: 1800,
	}

	dm := cfg.DataManager
	p.dedupWindowSeconds = float64(dm.DedupWindowSeconds)
	if p.dedupWindowSeconds <= 0 {
		p.dedupWindowSeconds = 5
	}
	p.symbolCooldownSeconds = float64(dm.SymbolCooldownMinutes) * 60
	if p.symbolCooldownSeconds <= 0 {
		p.symbolCooldownSeconds = 1800
	}

	// Alert policy
	policy := cfg.AlertPolicy
	if policy.Enabled && len(policy.AllowedEventTypes) > 0 {
		p.allowedEventTypes = make(map[string]bool, len(policy.AllowedEventTypes))
		for _, et := range policy.AllowedEventTypes {
			p.allowedEventTypes[et] = true
		}
	}
	p.minSeverityRank = severityRank(policy.MinSeverity)
	p.minPriceChangePct = policy.MinPriceChangePct
	if p.minPriceChangePct <= 0 {
		p.minPriceChangePct = 0.9
	}
	p.minVolumeRatio = policy.MinVolumeRatio
	if p.minVolumeRatio <= 0 {
		p.minVolumeRatio = 6.0
	}
	p.minOIChangePct = policy.MinOpenInterestChangePct
	if p.minOIChangePct <= 0 {
		p.minOIChangePct = 5.0
	}
	p.minFundingRateAbs = policy.MinFundingRateAbs
	if p.minFundingRateAbs <= 0 {
		p.minFundingRateAbs = 0.0005
	}
	p.minFundingRateChange = policy.MinFundingRateChangeAbs
	if p.minFundingRateChange <= 0 {
		p.minFundingRateChange = 0.0003
	}

	hints, err := storage.NewHintStore(cfg.Storage)
	if err != nil {
		log.Warn("hint store disabled", "error", err)
	} else {
		p.hints = hints
	}

	if cfg.MarketPulse.Enabled {
		mpStore, err := storage.NewMarketPulseStore(cfg.Storage)
		if err != nil {
			log.Warn("market pulse store disabled", "error", err)
		} else {
			p.mpStore = mpStore
			log.Info("market pulse store ready", "path", mpStore.Path())
		}
	}

	return p
}

// ── Start ──────────────────────────────────────────────────────

// Start initialises exchanges, discovers symbols, registers detectors,
// starts WebSocket feeds, and launches all goroutines.  Blocks until all
// goroutines finish (or the context is cancelled).
func (p *Pipeline) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	p.cancel = cancel

	exchangeNames := p.cfg.DataManager.Exchanges
	if len(exchangeNames) == 0 {
		exchangeNames = []string{"okx", "binance", "bybit"}
	}

	// 1. Create exchange instances.
	for _, name := range exchangeNames {
		ex, err := exchangeNew(name)
		if err != nil {
			p.log.Warn("exchange creation skipped", "exchange", name, "error", err)
			continue
		}
		p.exchanges[name] = ex
		p.log.Info("exchange created", "exchange", name)
	}

	// 2. Discover top symbols per exchange.
	for name, ex := range p.exchanges {
		symbols, err := p.discoverSymbols(ctx, ex, p.cfg.DataManager.TopSymbols)
		if err != nil {
			p.log.Warn("symbol discovery failed, skipping WS", "exchange", name, "error", err)
			symbols = nil
		}
		p.setSymbols(name, symbols)
		p.log.Info("symbols discovered", "exchange", name, "count", len(symbols), "symbols", strings.Join(symbols, ","))
	}

	p.loadMarketCaps(ctx)

	// 3. Create exchange states with detectors.
	for name, ex := range p.exchanges {
		es := &exchangeState{
			name:     name,
			ex:       ex,
			tickerCh: make(chan types.Ticker, 512),
			resubCh:  make(chan struct{}, 1),
		}
		p.registerDetectors(es)
		p.exchangeStates[name] = es
	}

	// 3b. Register CoinGlass-based detectors (cross-exchange).
	p.longShortDet = p.newLongShortDetector()
	p.liqDet = p.newLiquidationDetector()

	// 3c. Register resonance scorer.
	if p.cfg.ResonanceScorer.Enabled {
		p.resonanceScorer = detector.NewResonanceScorer(p.cfg.ResonanceScorer)
		p.log.Info("resonance scorer registered",
			"min_score", p.cfg.ResonanceScorer.MinScore,
		)
	}

	// 3d. Market pulse (primary exchange only; after symbol discovery).
	if p.cfg.MarketPulse.Enabled {
		p.marketPulseDet = detector.NewMarketPulseDetector(p.cfg.MarketPulse)
		primary := p.primaryExchange()
		syms := p.symbolsSnapshot(primary)
		if len(syms) > 0 {
			p.marketPulseDet.UpdateUniverse(syms)
		}
		p.log.Info("market pulse detector registered",
			"shadow", p.cfg.MarketPulse.ShadowMode,
			"primary", primary,
			"universe", len(syms),
		)
	}

	// 4. Build event channel list for the aggregator.
	var eventChs []<-chan types.AnomalyEvent
	for _, es := range p.exchangeStates {
		if es.velocity != nil {
			eventChs = append(eventChs, es.velocity.Events())
		}
		if es.spike != nil {
			eventChs = append(eventChs, es.spike.Events())
		}
		if es.metrics != nil {
			eventChs = append(eventChs, es.metrics.Events())
		}
	}
	if p.longShortDet != nil {
		eventChs = append(eventChs, p.longShortDet.Events())
	}
	if p.liqDet != nil {
		eventChs = append(eventChs, p.liqDet.Events())
	}
	if p.marketPulseDet != nil {
		eventChs = append(eventChs, p.marketPulseDet.Events())
	}

	// 5. Launch goroutines via errgroup.
	g, gCtx := errgroup.WithContext(ctx)

	// 5a. WS subscription loop per exchange (restartable on universe refresh).
	for _, es := range p.exchangeStates {
		es := es
		g.Go(func() error {
			return p.runSubscription(gCtx, es)
		})
	}

	// 5b. Ticker fan-out goroutine per exchange: reads tickerCh,
	// routes tickers to per-exchange velocity + spike detectors.
	for _, es := range p.exchangeStates {
		es := es
		g.Go(func() error {
			p.consumeTickers(gCtx, es)
			return nil
		})
	}

	// 5c. Futures metrics polling per exchange.
	for _, es := range p.exchangeStates {
		if es.metrics == nil {
			continue
		}
		es := es
		g.Go(func() error {
			p.metricsPollLoop(gCtx, es)
			return nil
		})
	}

	// 5d. CoinGlass L/S ratio polling.
	if p.longShortDet != nil {
		g.Go(func() error {
			p.coinglassLSLoop(gCtx)
			return nil
		})
	}

	// 5e. CoinGlass liquidation polling.
	if p.liqDet != nil {
		g.Go(func() error {
			p.coinglassLiqLoop(gCtx)
			return nil
		})
	}

	// 5f. Event aggregator: merges all detector event channels into one,
	// feeds resonance scorer, and sends alertable events to the delivery channel.
	deliveryCh := make(chan types.AnomalyEvent, 128)
	g.Go(func() error {
		p.eventAggregator(gCtx, eventChs, deliveryCh)
		return nil
	})

	// 5g. Resonance deliverer: reads resonance events and sends to Telegram.
	if p.resonanceScorer != nil {
		g.Go(func() error {
			p.resonanceDeliverer(gCtx)
			return nil
		})
	}

	// 5h. Telegram deliverer: reads deliveryCh, applies dedup + alert policy, sends.
	g.Go(func() error {
		p.telegramDeliverer(gCtx, deliveryCh)
		return nil
	})

	// 5i. Symbol refresh loop.
	g.Go(func() error {
		p.refreshLoop(gCtx)
		return nil
	})

	// 5j. Market pulse snapshot loop.
	if p.marketPulseDet != nil {
		g.Go(func() error {
			p.marketPulseDet.Start(gCtx)
			return nil
		})
	}

	p.log.Info("pipeline started",
		"exchanges", len(p.exchangeStates),
		"symbols_total", p.totalSymbols(),
		"telegram", p.tg != nil && p.tg.IsConfigured(),
		"dingtalk", p.dingTalk != nil && p.dingTalk.IsConfigured(),
	)

	// Wait for all goroutines.
	err := g.Wait()
	if err != nil && err != context.Canceled {
		return err
	}
	return nil
}

// ── Symbol universe (locked) ───────────────────────────────────

// symbolsSnapshot returns a copy of the current universe for one exchange.
func (p *Pipeline) symbolsSnapshot(name string) []string {
	p.symMu.RLock()
	defer p.symMu.RUnlock()
	syms := p.symbolsByExchange[name]
	out := make([]string, len(syms))
	copy(out, syms)
	return out
}

// allSymbolsSnapshot returns a copy of the whole universe map.
func (p *Pipeline) allSymbolsSnapshot() map[string][]string {
	p.symMu.RLock()
	defer p.symMu.RUnlock()
	out := make(map[string][]string, len(p.symbolsByExchange))
	for name, syms := range p.symbolsByExchange {
		cp := make([]string, len(syms))
		copy(cp, syms)
		out[name] = cp
	}
	return out
}

// setSymbols replaces the universe for one exchange.
func (p *Pipeline) setSymbols(name string, symbols []string) {
	p.symMu.Lock()
	p.symbolsByExchange[name] = symbols
	p.symMu.Unlock()
}

// primaryExchange resolves the primary authority (Normalize guarantees it is
// set for loaded configs; the legacy field covers hand-built test configs).
func (p *Pipeline) primaryExchange() string {
	if p.cfg.Exchanges.Primary != "" {
		return p.cfg.Exchanges.Primary
	}
	return p.cfg.Exchange
}

// coinglassSymbols picks the universe used for CoinGlass per-symbol polling:
// the primary exchange when it has symbols, otherwise the first enabled
// exchange with a non-empty universe.
func (p *Pipeline) coinglassSymbols() []string {
	if syms := p.symbolsSnapshot(p.primaryExchange()); len(syms) > 0 {
		return syms
	}
	p.symMu.RLock()
	names := make([]string, 0, len(p.symbolsByExchange))
	for name := range p.symbolsByExchange {
		names = append(names, name)
	}
	p.symMu.RUnlock()
	sort.Strings(names)
	for _, name := range names {
		if syms := p.symbolsSnapshot(name); len(syms) > 0 {
			return syms
		}
	}
	return nil
}

// ── WS subscription loop ───────────────────────────────────────

// runSubscription owns the WS feed for one exchange. It subscribes with the
// current universe snapshot and restarts the subscription whenever the
// refresh loop signals a universe change, so realtime data always follows
// the advertised symbol list.
func (p *Pipeline) runSubscription(ctx context.Context, es *exchangeState) error {
	for {
		symbols := p.symbolsSnapshot(es.name)
		if len(symbols) == 0 {
			// No universe yet — wait for a refresh to provide one.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-es.resubCh:
				continue
			}
		}

		subCtx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		p.log.Info("connecting WS feed", "exchange", es.name, "symbols", len(symbols))
		go func() {
			done <- es.ex.SubscribeTickers(subCtx, symbols, es.tickerCh)
		}()

		select {
		case <-ctx.Done():
			cancel()
			<-done
			return ctx.Err()
		case <-es.resubCh:
			p.log.Info("universe changed; resubscribing WS feed", "exchange", es.name)
			cancel()
			<-done // wait for the old subscription to fully exit
			continue
		case err := <-done:
			cancel()
			return err
		}
	}
}

// Stop gracefully cancels the pipeline and waits for shutdown.
func (p *Pipeline) Stop() {
	if p.cancel != nil {
		p.cancel()
	}
}

// Close releases exchange connections after shutdown.
func (p *Pipeline) Close() {
	for name, ex := range p.exchanges {
		if err := ex.Close(); err != nil {
			p.log.Warn("exchange close", "exchange", name, "error", err)
		}
	}
}

// ── Symbol Discovery ───────────────────────────────────────────

func (p *Pipeline) discoverSymbols(ctx context.Context, ex exchange.Exchange, topN int) ([]string, error) {
	tickers, err := ex.FetchTickers(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch tickers: %w", err)
	}

	type kv struct {
		symbol string
		vol    float64
	}
	var candidates []kv
	for sym, t := range tickers {
		if t == nil {
			continue
		}
		// ponytail: exchange.FetchTickers already returns SWAP/perpetual tickers;
		// filter by symbol suffix as a safety check.
		if !strings.HasSuffix(sym, ":USDT") || !strings.Contains(sym, "/USDT:") {
			continue
		}
		vol := 0.0
		if t.QuoteVolume != nil {
			vol = *t.QuoteVolume
		}
		if vol <= 0 {
			// Fall back to the raw ticker info if available.
			if len(t.Info) > 0 {
				vol = utils.ExtractQuoteVolume(t.Info)
			}
		}
		if vol <= 0 {
			continue
		}
		if p.blacklist.IsBlocked(sym) {
			continue
		}
		candidates = append(candidates, kv{symbol: sym, vol: vol})
	}

	// Sort descending by volume.
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].vol > candidates[i].vol {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	if len(candidates) > topN {
		candidates = candidates[:topN]
	}

	symbols := make([]string, len(candidates))
	for i, kv := range candidates {
		symbols[i] = kv.symbol
	}
	return symbols, nil
}

// ── Detector Registration ─────────────────────────────────────

func (p *Pipeline) registerDetectors(es *exchangeState) {
	cfg := p.cfg

	if cfg.PriceVelocity.Enabled {
		d := detector.NewPriceVelocityDetector(cfg.PriceVelocity)
		es.velocity = d
		p.log.Info("velocity detector registered", "exchange", es.name)
	}

	if cfg.VolumeSpike.Enabled {
		d := detector.NewVolumeSpikeDetector(cfg.VolumeSpike)
		es.spike = d
		p.log.Info("spike detector registered", "exchange", es.name)
	}

	if cfg.FuturesMetrics.Enabled {
		d := detector.NewFuturesMetricsDetector(cfg.FuturesMetrics)
		es.metrics = d
		p.log.Info("futures metrics detector registered", "exchange", es.name)
	}
}

func (p *Pipeline) newLongShortDetector() *detector.LongShortRatioDetector {
	if !p.cfg.LongShortRatio.Enabled {
		return nil
	}
	d := detector.NewLongShortRatioDetector(p.cfg.LongShortRatio)
	p.log.Info("long/short ratio detector registered")
	return d
}

func (p *Pipeline) newLiquidationDetector() *detector.LiquidationDetector {
	if !p.cfg.Liquidation.Enabled {
		return nil
	}
	d := detector.NewLiquidationDetector(p.cfg.Liquidation)
	p.log.Info("liquidation detector registered")
	return d
}

// ── Ticker Consumer ────────────────────────────────────────────

// consumeTickers reads from the exchange's ticker channel and feeds
// the per-exchange real-time detectors (velocity, spike).
func (p *Pipeline) consumeTickers(ctx context.Context, es *exchangeState) {
	for {
		select {
		case <-ctx.Done():
			return
		case t, ok := <-es.tickerCh:
			if !ok {
				return
			}
			if es.velocity != nil {
				es.velocity.OnTicker(ctx, t)
			}
			if es.spike != nil {
				es.spike.OnTicker(ctx, t)
			}
			// Market pulse: primary exchange only.
			if p.marketPulseDet != nil && es.name == p.primaryExchange() {
				p.marketPulseDet.OnTicker(ctx, t)
			}
		}
	}
}

// ── Futures Metrics Polling ────────────────────────────────────

// metricsPollLoop periodically fetches open-interest and funding-rate
// snapshots for the exchange's symbols and feeds them to the
// FuturesMetricsDetector.
func (p *Pipeline) metricsPollLoop(ctx context.Context, es *exchangeState) {
	interval := time.Duration(p.cfg.FuturesMetrics.PollIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 300 * time.Second
	}

	// Initial poll.
	if err := p.pollMetrics(ctx, es); err != nil {
		p.log.Warn("initial metrics poll failed", "exchange", es.name, "error", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.pollMetrics(ctx, es); err != nil {
				p.log.Warn("metrics poll failed", "exchange", es.name, "error", err)
			}
		}
	}
}

func (p *Pipeline) pollMetrics(ctx context.Context, es *exchangeState) error {
	symbols := p.symbolsSnapshot(es.name)
	if es.metrics == nil || len(symbols) == 0 {
		return nil
	}

	tickers, err := es.ex.FetchTickers(ctx)
	if err != nil {
		return fmt.Errorf("fetch tickers for metrics: %w", err)
	}

	for _, sym := range symbols {
		t, ok := tickers[sym]
		if !ok || t == nil {
			continue
		}
		price := 0.0
		if t.LastPrice != nil {
			price = *t.LastPrice
		}
		// Pass optional metrics through as pointers: absent stays absent,
		// a real zero stays a real zero.
		es.metrics.OnMetricsUpdate(ctx, sym, price, t.OpenInterest, t.FundingRate)
	}
	return nil
}

// ── CoinGlass L/S Ratio Polling ────────────────────────────────

func (p *Pipeline) coinglassLSLoop(ctx context.Context) {
	interval := time.Duration(p.cfg.LongShortRatio.PollIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 300 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Initial poll.
	p.pollLongShort(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.pollLongShort(ctx)
		}
	}
}

func (p *Pipeline) pollLongShort(ctx context.Context) {
	if p.longShortDet == nil {
		return
	}

	symbols := p.coinglassSymbols()
	if len(symbols) == 0 {
		return
	}

	now := float64(time.Now().UnixMilli()) / 1000
	for _, rawSym := range symbols {
		if ctx.Err() != nil {
			return
		}
		base, err := data.NormalizeCoinSymbol(rawSym)
		if err != nil || base == "" {
			continue
		}

		payload, err := data.FetchCoinGlassEndpoint(ctx,
			"/api/futures/longShortRate",
			map[string]string{"symbol": base, "timeType": "2"},
			10*time.Second,
		)
		if err != nil {
			p.log.Debug("L/S poll failed", "symbol", rawSym, "error", err)
			continue
		}

		longRate := extractAvgLongRate(payload)
		if longRate != nil {
			shortRate := 100.0 - *longRate
			p.longShortDet.OnLSSnapshot(rawSym, *longRate, shortRate)
		}
		_ = now
	}
}

// extractAvgLongRate extracts the volume-weighted average long rate from
// a CoinGlass longShortRate response (a list of exchange-level entries).
func extractAvgLongRate(payload any) *float64 {
	list, ok := payload.([]any)
	if !ok {
		return nil
	}

	var totalLongVol, totalShortVol float64
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		exchanges, _ := m["list"].([]any)
		if len(exchanges) == 0 {
			exchanges = []any{m}
		}
		for _, ex := range exchanges {
			exM, ok := ex.(map[string]any)
			if !ok {
				continue
			}
			longVol, _ := floatFromMap(exM, "longVolUsd")
			shortVol, _ := floatFromMap(exM, "shortVolUsd")
			totalLongVol += longVol
			totalShortVol += shortVol
		}
	}

	totalVol := totalLongVol + totalShortVol
	if totalVol <= 0 {
		return nil
	}
	rate := math.Round(totalLongVol/totalVol*10000) / 100
	return &rate
}

// ── CoinGlass Liquidation Polling ──────────────────────────────

func (p *Pipeline) coinglassLiqLoop(ctx context.Context) {
	interval := time.Duration(p.cfg.Liquidation.PollIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 300 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	p.pollLiquidations(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.pollLiquidations(ctx)
		}
	}
}

func (p *Pipeline) pollLiquidations(ctx context.Context) {
	if p.liqDet == nil {
		return
	}

	symbols := p.coinglassSymbols()
	if len(symbols) == 0 {
		return
	}

	now := float64(time.Now().UnixMilli()) / 1000
	for _, rawSym := range symbols {
		if ctx.Err() != nil {
			return
		}
		base, err := data.NormalizeCoinSymbol(rawSym)
		if err != nil || base == "" {
			continue
		}

		payload, err := data.FetchCoinGlassEndpoint(ctx,
			"/api/futures/liquidation/today",
			map[string]string{"symbol": base},
			10*time.Second,
		)
		if err != nil {
			p.log.Debug("liquidation poll failed", "symbol", rawSym, "error", err)
			continue
		}

		m, ok := payload.(map[string]any)
		if !ok {
			continue
		}
		total := floatFromMapDefault(m, "liquidationUsd", 0)
		longLiq := floatFromMapDefault(m, "longLiquidationUsd", 0)
		shortLiq := floatFromMapDefault(m, "shortLiquidationUsd", 0)
		if total > 0 {
			p.liqDet.OnLiquidationSnapshot(rawSym, total, longLiq, shortLiq)
		}
	}
	_ = now
}

// ── Event Aggregator ───────────────────────────────────────────

// eventAggregator merges all detector event channels, feeds the resonance
// scorer, and sends alertable events to the delivery channel.
func (p *Pipeline) eventAggregator(
	ctx context.Context,
	eventChs []<-chan types.AnomalyEvent,
	deliveryCh chan<- types.AnomalyEvent,
) {
	// Merge all event channels using goroutines.
	merged := p.mergeChannels(ctx, eventChs)
	if merged == nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-merged:
			if !ok {
				return
			}

			// Feed resonance scorer (but not resonance events themselves).
			if p.resonanceScorer != nil && evt.EventType != "resonance" {
				// Non-blocking send to avoid back-pressure.
				p.resonanceScorer.OnEvent(evt)
			}

			// Persist market events / outcomes even in shadow mode (for calibration).
			if (isMarketPulseEvent(evt.EventType) || evt.EventType == "market_outcome") && p.mpStore != nil {
				var err error
				if evt.EventType == "market_outcome" {
					err = p.mpStore.RecordOutcome(evt)
				} else {
					err = p.mpStore.Record(evt)
				}
				if err != nil {
					p.log.Warn("market pulse store failed", "error", err, "event", evt.EventType)
				}
			}

			// Outcomes are calibration-only — never Telegram.
			if evt.EventType == "market_outcome" {
				continue
			}

			// Send to delivery channel (dedup + policy applied downstream).
			select {
			case deliveryCh <- evt:
			case <-ctx.Done():
				return
			}
		}
	}
}

// mergeChannels merges multiple AnomalyEvent channels into one.
// Returns nil if no source channels are provided.
func (p *Pipeline) mergeChannels(ctx context.Context, chs []<-chan types.AnomalyEvent) <-chan types.AnomalyEvent {
	if len(chs) == 0 {
		return nil
	}
	out := make(chan types.AnomalyEvent, 256)
	var wg sync.WaitGroup
	for _, ch := range chs {
		wg.Add(1)
		ch := ch
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case evt, ok := <-ch:
					if !ok {
						return
					}
					select {
					case out <- evt:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}

// ── Resonance Deliverer ────────────────────────────────────────

// resonanceDeliverer reads ResonanceEvents from the scorer and sends
// formatted alerts to Telegram with cooldown-based dedup.
func (p *Pipeline) resonanceDeliverer(ctx context.Context) {
	rs := p.resonanceScorer
	if rs == nil {
		return
	}

	// We can't range over rs.Events() directly with select, so use a goroutine.
	// Actually we can — the channel is never closed by the scorer. Use select.
	for {
		select {
		case <-ctx.Done():
			return
		case re, ok := <-rs.Events():
			if !ok {
				return
			}
			p.sendResonanceAlert(ctx, re)
		}
	}
}

// SetDingTalk attaches an optional DingTalk Delivery Channel (mirror fan-out).
func (p *Pipeline) SetDingTalk(d *notify.DingTalkClient) {
	p.dingTalk = d
}

func (p *Pipeline) hasDeliveryChannel() bool {
	return (p.tg != nil && p.tg.IsConfigured()) || (p.dingTalk != nil && p.dingTalk.IsConfigured())
}

// sendToChannels fans out one AlertEvent to every configured channel.
// Failures are independent; returns true if any channel succeeded.
func (p *Pipeline) sendToChannels(alert types.AlertEvent) bool {
	anyOK := false
	if p.tg != nil && p.tg.IsConfigured() {
		if err := p.tg.SendEvent(alert); err != nil {
			p.log.Warn("telegram send failed", "symbol", alert.Symbol, "event", alert.Event, "error", err)
		} else {
			anyOK = true
			p.log.Info("telegram sent", "symbol", alert.Symbol, "event", alert.Event,
				"severity", alert.Severity, "change_pct", alert.ChangePct)
		}
	}
	if p.dingTalk != nil && p.dingTalk.IsConfigured() {
		if err := p.dingTalk.SendEvent(alert); err != nil {
			p.log.Warn("dingtalk send failed", "symbol", alert.Symbol, "event", alert.Event, "error", err)
		} else {
			anyOK = true
			p.log.Info("dingtalk sent", "symbol", alert.Symbol, "event", alert.Event,
				"severity", alert.Severity)
		}
	}
	return anyOK
}

func (p *Pipeline) sendResonanceAlert(ctx context.Context, re detector.ResonanceEvent) {
	if !p.hasDeliveryChannel() {
		return
	}

	weight := p.liquidityWeight(re.Symbol)
	requiredScore := float64(p.cfg.ResonanceScorer.MinScore)
	if requiredScore <= 0 {
		requiredScore = 55
	}
	requiredScore = requiredScore / weight
	if re.SignalScore < requiredScore {
		p.log.Debug("resonance liquidity drop",
			"symbol", re.Symbol, "score", re.SignalScore, "required", requiredScore, "weight", weight)
		return
	}

	// Cooldown dedup per symbol+score combo.
	dedupKey := fmt.Sprintf("%s__resonance__%d", re.Symbol, int(math.Round(re.SignalScore)))
	now := time.Now().Unix()

	p.dedupMu.Lock()
	last := p.dedupLast[dedupKey]
	if float64(now)-last < p.symbolCooldownSeconds {
		p.dedupMu.Unlock()
		p.log.Debug("resonance cooldown drop", "symbol", re.Symbol, "key", dedupKey)
		return
	}
	p.dedupLast[dedupKey] = float64(now)
	p.dedupMu.Unlock()

	// Build AlertEvent.
	dimKeys := make([]string, 0, len(re.Dimensions))
	for k := range re.Dimensions {
		dimKeys = append(dimKeys, k)
	}
	data := map[string]any{
		"signal_score":     re.SignalScore,
		"dimension_count":  re.DimensionCount,
		"dimensions":       dimKeys,
		"liquidity_weight": math.Round(weight*1000) / 1000,
	}
	for dim, ev := range re.Dimensions {
		data[dim+"_data"] = ev.Data
	}

	alert := types.AlertEvent{
		Event:     "resonance",
		Symbol:    re.Symbol,
		Price:     0,
		Condition: fmt.Sprintf("🎯 信号质量=%.0f", re.SignalScore),
		Severity:  types.SeverityHigh,
		Data:      data,
	}

	select {
	case <-ctx.Done():
	default:
		if p.sendToChannels(alert) {
			p.recordWatchHint(re.Symbol, "resonance", p.cfg.Exchanges.Primary)
		}
	}
}

// ── Alert Deliverer ────────────────────────────────────────────

// telegramDeliverer reads from the delivery channel, applies dedup + alert
// policy, and fans out qualifying events to Delivery Channels.
func (p *Pipeline) telegramDeliverer(ctx context.Context, deliveryCh <-chan types.AnomalyEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-deliveryCh:
			if !ok {
				return
			}
			p.deliverEvent(ctx, evt)
		}
	}
}

func (p *Pipeline) deliverEvent(ctx context.Context, evt types.AnomalyEvent) {
	if !p.hasDeliveryChannel() {
		return
	}

	// Calibration outcomes never notify.
	if evt.EventType == "market_outcome" {
		return
	}

	// Shadow: market events are computed/logged only.
	if isMarketPulseEvent(evt.EventType) && p.marketPulseDet != nil && p.marketPulseDet.ShadowMode() {
		p.log.Info("alert gated", "reason", "market_pulse_shadow", "event", evt.EventType)
		return
	}

	// Blacklist check.
	if p.blacklist.IsBlocked(evt.Symbol) {
		p.log.Debug("blacklist drop", "symbol", evt.Symbol)
		return
	}

	// Quiet-market gate for ordinary single-symbol velocity (Phase 3).
	if evt.EventType == "price_velocity" && p.shouldGateIndividualAlert(evt) {
		p.log.Info("alert gated", "reason", "market_quiet", "symbol", evt.Symbol, "event", evt.EventType)
		return
	}

	// Alert policy.
	if !p.passesAlertPolicy(evt) {
		return
	}

	// Dedup.
	dedupKey := fmt.Sprintf("%s__%s", evt.Symbol, evt.EventType)
	now := time.Now().Unix()

	p.dedupMu.Lock()
	last := p.dedupLast[dedupKey]
	if float64(now)-last < p.dedupWindowSeconds {
		p.dedupMu.Unlock()
		p.log.Info("alert gated", "reason", "dedup", "symbol", evt.Symbol, "event", evt.EventType)
		return
	}
	symbolEventLast := p.dedupLast[dedupKey]
	if float64(now)-symbolEventLast < p.symbolCooldownSeconds {
		p.dedupMu.Unlock()
		p.log.Info("alert gated", "reason", "cooldown", "symbol", evt.Symbol, "event", evt.EventType)
		return
	}
	p.dedupLast[dedupKey] = float64(now)
	p.dedupMu.Unlock()

	alert := p.buildAlert(evt)

	select {
	case <-ctx.Done():
	default:
		if p.sendToChannels(alert) {
			p.recordWatchHint(evt.Symbol, evt.EventType, p.cfg.Exchanges.Primary)
		}
	}
}

func (p *Pipeline) recordWatchHint(symbol, eventType, exchange string) {
	if p.hints == nil || symbol == "" {
		return
	}
	// MARKET is not a tradeable symbol for scanner boost.
	if symbol == "MARKET" || isMarketPulseEvent(eventType) {
		return
	}
	if err := p.hints.Record(symbol, eventType, exchange); err != nil {
		p.log.Warn("watch hint record failed", "symbol", symbol, "error", err)
	}
}

func isMarketPulseEvent(eventType string) bool {
	switch eventType {
	case "market_impulse", "market_trend", "market_stress", "market_decay":
		return true
	default:
		return false
	}
}

// passesMarketEventPolicy applies severity allow-list only — no liquidity weight.
func (p *Pipeline) passesMarketEventPolicy(evt types.AnomalyEvent) bool {
	if severityRank(string(evt.Severity)) < p.minSeverityRank {
		p.log.Info("alert gated", "reason", "severity", "symbol", evt.Symbol, "event", evt.EventType,
			"severity", evt.Severity)
		return false
	}
	return true
}

// ── Alert Policy ───────────────────────────────────────────────

func (p *Pipeline) passesAlertPolicy(evt types.AnomalyEvent) bool {
	if !p.cfg.AlertPolicy.Enabled {
		return true
	}

	eventType := evt.EventType
	if p.allowedEventTypes != nil && !p.allowedEventTypes[eventType] {
		p.log.Info("alert gated", "reason", "event_type", "symbol", evt.Symbol, "event", eventType)
		return false
	}

	// Market-level events: no per-symbol liquidity weight (Phase 2 path).
	if isMarketPulseEvent(eventType) {
		return p.passesMarketEventPolicy(evt)
	}

	weight := p.liquidityWeight(evt.Symbol)
	adjRank := severityRank(string(evt.Severity)) - liquiditySeverityPenalty(weight)
	if adjRank < p.minSeverityRank {
		p.log.Info("alert gated", "reason", "severity", "symbol", evt.Symbol, "event", eventType,
			"severity", evt.Severity, "weight", weight)
		return false
	}

	strict := liquidityStrictness(weight)
	data := evt.Data
	switch eventType {
	case "price_velocity":
		changePct := math.Abs(floatFromMapDefault(data, "change_pct", 0.0))
		need := p.minPriceChangePct * strict
		if changePct < need {
			p.log.Info("alert gated", "reason", "price_change", "symbol", evt.Symbol, "event", eventType,
				"change_pct", changePct, "need", need, "weight", weight)
			return false
		}
	case "volume_spike":
		ratio := floatFromMapDefault(data, "ratio", 0.0)
		need := p.minVolumeRatio * strict
		if ratio < need {
			p.log.Info("alert gated", "reason", "volume_ratio", "symbol", evt.Symbol, "event", eventType,
				"ratio", ratio, "need", need, "weight", weight)
			return false
		}
	case "open_interest_change":
		changePct := math.Abs(floatFromMapDefault(data, "change_pct", 0.0))
		need := p.minOIChangePct * strict
		if changePct < need {
			p.log.Info("alert gated", "reason", "oi_change", "symbol", evt.Symbol, "event", eventType,
				"change_pct", changePct, "need", need, "weight", weight)
			return false
		}
	case "funding_rate_anomaly":
		fundingRate := math.Abs(floatFromMapDefault(data, "funding_rate", 0.0))
		changeAbs := math.Abs(floatFromMapDefault(data, "change_abs", 0.0))
		if fundingRate < p.minFundingRateAbs*strict && changeAbs < p.minFundingRateChange*strict {
			p.log.Info("alert gated", "reason", "funding", "symbol", evt.Symbol, "event", eventType,
				"funding_rate", fundingRate, "change_abs", changeAbs, "weight", weight)
			return false
		}
	case "liquidation":
		totalUSD := floatFromMapDefault(data, "total_liquidation_usd", 0.0)
		need := p.cfg.Liquidation.AbsThresholdUsd * strict
		if totalUSD > 0 && totalUSD < need {
			p.log.Info("alert gated", "reason", "liquidation_usd", "symbol", evt.Symbol, "event", eventType,
				"total_usd", totalUSD, "need", need, "weight", weight)
			return false
		}
	}
	return true
}

// ── Alert Building ─────────────────────────────────────────────

func (p *Pipeline) buildAlert(evt types.AnomalyEvent) types.AlertEvent {
	data := evt.Data
	price := 0.0

	// Extract best price from data.
	for _, key := range []string{"price_to", "price", "last_price"} {
		if v, ok := floatFromMap(data, key); ok && v > 0 {
			price = v
			break
		}
	}

	return types.AlertEvent{
		Event:     evt.EventType,
		Symbol:    evt.Symbol,
		Price:     price,
		Condition: buildCondition(evt),
		ChangePct: floatFromMapDefault(data, "change_pct", 0),
		Severity:  evt.Severity,
		Data:      data,
	}
}

// shouldGateIndividualAlert reports whether a single-symbol alert should be
// suppressed because the market is QUIET (Phase 3). Shadow mode never gates:
// it must not change single-symbol delivery behaviour.
func (p *Pipeline) shouldGateIndividualAlert(evt types.AnomalyEvent) bool {
	if p.marketPulseDet == nil || !p.cfg.MarketPulse.GateIndividualAlertsWhenQuiet {
		return false
	}
	if p.cfg.MarketPulse.ShadowMode {
		return false
	}
	return p.marketPulseDet.State() == types.MarketStateQuiet
}

// MarketState returns the current MarketPulse state, or QUIET if disabled.
func (p *Pipeline) MarketState() types.MarketState {
	if p.marketPulseDet == nil {
		return types.MarketStateQuiet
	}
	return p.marketPulseDet.State()
}

func buildCondition(evt types.AnomalyEvent) string {
	data := evt.Data
	switch evt.EventType {
	case "market_impulse", "market_trend", "market_stress", "market_decay":
		dir := fmt.Sprintf("%v", data["direction"])
		from := fmt.Sprintf("%v", data["state_from"])
		to := fmt.Sprintf("%v", data["state_to"])
		return dir + "_" + from + "_" + to
	case "price_velocity":
		ws := fmt.Sprintf("%v", data["window_seconds"])
		th := fmt.Sprintf("%v", data["threshold"])
		return ws + "s_" + th + "pct"
	case "volume_spike":
		ratio := fmt.Sprintf("%v", data["ratio"])
		wm := fmt.Sprintf("%v", data["window_minutes"])
		return ratio + "x_" + wm + "min"
	case "open_interest_change":
		change := fmt.Sprintf("%v", data["change_pct"])
		current := fmt.Sprintf("%v", data["open_interest"])
		previous := fmt.Sprintf("%v", data["previous_open_interest"])
		return "oi_change=" + change + "% current=" + current + " previous=" + previous
	case "funding_rate_anomaly":
		rate := fmt.Sprintf("%v", data["funding_rate"])
		change := fmt.Sprintf("%v", data["change_abs"])
		reason := fmt.Sprintf("%v", data["reason"])
		return "funding_rate=" + rate + " change_abs=" + change + " reason=" + reason
	}
	return "unknown"
}

// ── Symbol Refresh Loop ────────────────────────────────────────

func (p *Pipeline) refreshLoop(ctx context.Context) {
	interval := p.refreshInterval
	if interval <= 0 {
		interval = time.Duration(p.cfg.DataManager.RefreshIntervalHours) * time.Hour
	}
	if interval <= 0 {
		interval = 4 * time.Hour
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.log.Info("periodic symbol refresh starting")
			p.refreshOnce(ctx)
		}
	}
}

// refreshOnce re-discovers the universe on every exchange, publishes the new
// snapshots, updates MarketPulse (primary only), and asks the affected WS
// subscription loops to resubscribe.
func (p *Pipeline) refreshOnce(ctx context.Context) {
	primary := p.primaryExchange()
	for name, ex := range p.exchanges {
		newSymbols, err := p.discoverSymbols(ctx, ex, p.cfg.DataManager.TopSymbols)
		if err != nil {
			p.log.Warn("refresh failed", "exchange", name, "error", err)
			continue
		}
		old := p.symbolsSnapshot(name)
		added := diffSymbols(newSymbols, old)
		removed := diffSymbols(old, newSymbols)
		p.setSymbols(name, newSymbols)
		if len(added) == 0 && len(removed) == 0 {
			continue
		}
		p.log.Warn("symbol universe changed",
			"exchange", name,
			"added", len(added),
			"removed", len(removed),
		)
		if p.marketPulseDet != nil && name == primary {
			p.marketPulseDet.UpdateUniverse(newSymbols)
		}
		if es := p.exchangeStates[name]; es != nil {
			select {
			case es.resubCh <- struct{}{}:
			default: // a resubscribe is already pending
			}
		}
	}
	p.loadMarketCaps(ctx)
}

func diffSymbols(a, b []string) []string {
	set := make(map[string]struct{}, len(b))
	for _, s := range b {
		set[s] = struct{}{}
	}
	var diff []string
	for _, s := range a {
		if _, ok := set[s]; !ok {
			diff = append(diff, s)
		}
	}
	return diff
}

// ── Helpers ────────────────────────────────────────────────────

func (p *Pipeline) totalSymbols() int {
	p.symMu.RLock()
	defer p.symMu.RUnlock()
	n := 0
	for _, symbols := range p.symbolsByExchange {
		n += len(symbols)
	}
	return n
}

// floatFromMap extracts a float64 value from a map.
func floatFromMap(m map[string]any, key string) (float64, bool) {
	v, ok := m[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	case uint:
		return float64(n), true
	case int32:
		return float64(n), true
	case uint32:
		return float64(n), true
	case float32:
		return float64(n), true
	case uint8:
		return float64(n), true
	case int8:
		return float64(n), true
	case bool:
		if n {
			return 1, true
		}
		return 0, true
	default:
		return 0, false
	}
}

// floatFromMapDefault extracts a float64 with a fallback default.
func floatFromMapDefault(m map[string]any, key string, defaultVal float64) float64 {
	v, ok := floatFromMap(m, key)
	if !ok {
		return defaultVal
	}
	return v
}

// severityRank maps a severity string to an integer rank (LOW=1, MEDIUM=2, HIGH=3).
func severityRank(s string) int {
	switch s {
	case "LOW":
		return 1
	case "MEDIUM":
		return 2
	case "HIGH":
		return 3
	default:
		return 1
	}
}
