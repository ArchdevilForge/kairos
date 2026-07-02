package scanner

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ArchdevilForge/kairos/internal/data"
	"github.com/ArchdevilForge/kairos/internal/exchange"
	"github.com/ArchdevilForge/kairos/internal/indicators"
	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
	"github.com/ArchdevilForge/kairos/internal/utils"
)

// ---------------------------------------------------------------------------
// btcContext holds the BTC reference data used during setup analysis.
// ---------------------------------------------------------------------------

type btcContext struct {
	Symbol string
	OHLCV  *types.OHLCVArrays
	Cycle  types.MarketCycle
}

// ---------------------------------------------------------------------------
// MarketScanner — deterministic scanner and setup analyzer.
// Ported from src/kairos/scanner.py MarketScanner.
// ---------------------------------------------------------------------------

type MarketScanner struct {
	config          *types.Config
	blacklist       *utils.Blacklist
	boxDetector     *indicators.BoxDetector
	cycleDetector   *indicators.CycleDetector
	hints           *storage.HintStore
	log             *slog.Logger
	exchangeFactory func(string) (exchange.Exchange, error) // tests only
	rsiLoader       func(context.Context) (map[string]data.CoinRSI, string) // tests only
}

// NewMarketScanner creates a new scanner from the application config.
func NewMarketScanner(cfg *types.Config) *MarketScanner {
	hints, err := storage.NewHintStore(cfg.Storage)
	if err != nil {
		slog.Default().Warn("scanner hint store disabled", "error", err)
	}
	return &MarketScanner{
		config:        cfg,
		blacklist:     utils.NewBlacklist(),
		boxDetector:   indicators.NewBoxDetector(indicators.DefaultBoxDetectorConfig()),
		cycleDetector: indicators.NewCycleDetector(cycleDetectorConfig(cfg.Scoring)),
		hints:         hints,
		log:           slog.Default().With("component", "scanner"),
	}
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// ScanMarket runs the full scanner workflow on the named exchange.
// If exchangeName is empty the configured primary is used.
func (s *MarketScanner) ScanMarket(ctx context.Context, exchangeName string) *types.SignalEnvelope {
	if exchangeName == "" {
		exchangeName = s.config.Exchanges.Primary
	}
	if sec := s.config.Scanner.TotalTimeoutSeconds; sec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(sec)*time.Second)
		defer cancel()
	}
	var warnings, errors []string

	exch, err := s.getExchange(exchangeName)
	if err != nil {
		return makeSignalEnvelope(false, map[string]any{
			"exchange": exchangeName, "candidates": []any{}, "setups": []any{}, "qualified_setups": []any{},
		}, "", nil, nil, nil, []string{fmt.Sprintf("Cannot connect to %s: %v", exchangeName, err)})
	}
	defer func() { _ = exch.Close() }()

	var btcCtx *btcContext
	var btcWarnings []string
	var rsiMap map[string]data.CoinRSI
	var rsiWarn string
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		btcCtx, btcWarnings = s.loadBTCContext(ctx, exch)
	}()
	go func() {
		defer wg.Done()
		rsiMap, rsiWarn = s.loadRSIMap(ctx)
	}()
	wg.Wait()

	if rsiWarn != "" {
		warnings = append(warnings, rsiWarn)
	}

	candidates, universeSize, discWarnings := s.discoverCandidates(ctx, exch, exchangeName, rsiMap)
	warnings = append(warnings, discWarnings...)
	warnings = append(warnings, btcWarnings...)

	// Backup exchange fallback
	if len(candidates) == 0 {
		for _, backup := range s.config.Exchanges.Backups {
			backupExch, bErr := s.getExchange(backup)
			if bErr != nil {
				warnings = append(warnings, fmt.Sprintf("%s backup discovery failed: %v", backup, bErr))
				continue
			}
			backupCandidates, backUnivSize, backWarnings := s.discoverCandidates(ctx, backupExch, backup, rsiMap)
			warnings = append(warnings, backWarnings...)
			if len(backupCandidates) > 0 {
				warnings = append(warnings, fmt.Sprintf("%s returned no candidates; using %s backup universe.", exchangeName, backup))
				_ = exch.Close()
				exch = backupExch
				exchangeName = backup
				candidates = backupCandidates
				universeSize = backUnivSize
				var btcReloadWarnings []string
				btcCtx, btcReloadWarnings = s.loadBTCContext(ctx, exch)
				warnings = append(warnings, btcReloadWarnings...)
				break
			}
			_ = backupExch.Close()
		}
	}

	var setups []types.Setup
	var qualifiedSetups []types.Setup
	if btcCtx == nil {
		warnings = append(warnings, "BTC critical context unavailable; candidates returned but trade setups withheld.")
	} else {
		limit := s.config.Scanner.DeepAnalysisLimit
		if limit > len(candidates) {
			limit = len(candidates)
		}
		// ponytail: parallel deep analysis — each candidate pulls 3 TF OHLCV independently.
		maxParallel := 6
		if limit < maxParallel {
			maxParallel = limit
		}
		sem := make(chan struct{}, maxParallel)
		var mu sync.Mutex
		var analysisWg sync.WaitGroup
		for _, cand := range candidates[:limit] {
			analysisWg.Add(1)
			go func(c types.Candidate) {
				defer analysisWg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				candCtx := ctx
				if sec := s.config.Scanner.SymbolAnalysisTimeoutSeconds; sec > 0 {
					var cancel context.CancelFunc
					candCtx, cancel = context.WithTimeout(ctx, time.Duration(sec)*time.Second)
					defer cancel()
				}
				setup := s.analyzeCandidate(candCtx, exch, c, btcCtx)
				mu.Lock()
				setups = append(setups, setup)
				if setup.ActionState == string(types.ActionStateTradeCandidate) {
					qualifiedSetups = append(qualifiedSetups, setup)
				}
				mu.Unlock()
			}(cand)
		}
		analysisWg.Wait()
	}

	// Serialize candidates
	candData := make([]map[string]any, len(candidates))
	for i, c := range candidates {
		candData[i] = candidateToMap(c)
	}
	setupData := make([]map[string]any, len(setups))
	for i, st := range setups {
		setupData[i] = setupToMap(st)
	}
	qualData := make([]map[string]any, len(qualifiedSetups))
	for i, st := range qualifiedSetups {
		qualData[i] = setupToMap(st)
	}

	scoreMap := map[string]any{
		"candidate_count":      len(candidates),
		"analyzed_count":       len(setups),
		"qualified_setup_count": len(qualifiedSetups),
	}
	if btcCtx != nil {
		scoreMap["btc_cycle"] = string(btcCtx.Cycle.Phase)
	}

	data := map[string]any{
		"exchange": exchangeName,
		"universe": map[string]any{
			"source":         exchangeName + "_futures_volume_top",
			"requested_size": s.config.Scanner.UniverseSize,
			"actual_size":    universeSize,
			"default":        exchangeName == "okx",
		},
		"limits": map[string]any{
			"candidate_limit":              s.config.Scanner.CandidateLimit,
			"deep_analysis_limit":          s.config.Scanner.DeepAnalysisLimit,
			"total_timeout_seconds":        s.config.Scanner.TotalTimeoutSeconds,
			"exchange_request_timeout_seconds": s.config.Scanner.ExchangeRequestTimeoutSeconds,
			"symbol_analysis_timeout_seconds":  s.config.Scanner.SymbolAnalysisTimeoutSeconds,
		},
		"candidates":       candData,
		"setups":           setupData,
		"qualified_setups": qualData,
		"scanner_policy": map[string]any{
			"primary_workflow": "scanner",
			"websocket_role":   "candidate_hint_only",
			"charts_generated": false,
			"telegram_pushed":  false,
			"execution_enabled": false,
		},
	}

	return makeSignalEnvelope(true, data, "", scoreMap,
		[]string{"scanner workflow completed with deterministic Kairos scoring"},
		warnings, errors)
}

// AnalyzeSymbolSetup runs the setup analyzer for a single manually-requested symbol.
func (s *MarketScanner) AnalyzeSymbolSetup(ctx context.Context, symbol, exchangeName string) *types.SignalEnvelope {
	if exchangeName == "" {
		exchangeName = s.config.Exchanges.Primary
	}
	var warnings, errors []string

	canonical, err := utils.NormalizeSymbol(symbol)
	if err != nil {
		return makeSignalEnvelope(false, map[string]any{}, "", nil, nil, nil, []string{err.Error()})
	}

	exch, err := s.getExchange(exchangeName)
	if err != nil {
		return makeSignalEnvelope(false, map[string]any{"exchange": exchangeName}, canonical, nil, nil, nil,
			[]string{fmt.Sprintf("Cannot connect to %s: %v", exchangeName, err)})
	}
	defer func() { _ = exch.Close() }()

	ticker := s.fetchTicker(ctx, exch, canonical)
	rsiMap, rsiWarn := s.loadRSIMap(ctx)
	if rsiWarn != "" {
		warnings = append(warnings, rsiWarn)
	}
	cand := s.scoreCandidate(canonical, exchangeName, ticker, nil, 0, rsiMap)
	minLiq := s.config.Scoring.MinimumLiquidityQuoteVolume

	quoteVolume := 0.0
	if ticker != nil && ticker.QuoteVolume != nil {
		quoteVolume = *ticker.QuoteVolume
	}

	if quoteVolume < minLiq {
		actionState := string(types.ActionStateWatch)
		if quoteVolume <= 0 {
			actionState = string(types.ActionStateNoTrade)
		}
		warning := fmt.Sprintf("%s quoteVolume %.0f is below minimum %.0f; not eligible for trade_candidate.",
			canonical, quoteVolume, minLiq)
		warnings = append(warnings, warning)
		setup := s.emptySetup(canonical, actionState, []string{"minimum liquidity not satisfied"}, []string{warning})
		return makeSignalEnvelope(true,
			map[string]any{"exchange": exchangeName, "candidate": candidateToMap(cand), "setup": setupToMap(setup)},
			canonical,
			map[string]any{"candidate_score": cand.CandidateScore, "setup_score": 0.0},
			[]string{"manual symbol analysis completed with liquidity gate"},
			warnings, errors)
	}

	btcCtx, btcWarnings := s.loadBTCContext(ctx, exch)
	warnings = append(warnings, btcWarnings...)

	var setup types.Setup
	if btcCtx == nil {
		setup = s.emptySetup(canonical, string(types.ActionStateWatch),
			[]string{"BTC context is required before a trade candidate can be returned"},
			btcWarnings)
	} else {
		setup = s.analyzeCandidate(ctx, exch, cand, btcCtx)
	}

	var thresholdPtr *float64
	if setup.Threshold != nil {
		thresholdPtrVal := *setup.Threshold
		thresholdPtr = &thresholdPtrVal
	}

	return makeSignalEnvelope(true,
		map[string]any{"exchange": exchangeName, "candidate": candidateToMap(cand), "setup": setupToMap(setup)},
		canonical,
		map[string]any{
			"candidate_score": cand.CandidateScore,
			"setup_score":     setup.SetupScore,
			"threshold":       thresholdPtr,
		},
		[]string{"manual symbol analysis completed with deterministic Kairos scoring"},
		DedupeStrings(append(warnings, setup.Warnings...)),
		errors)
}

// ---------------------------------------------------------------------------
// Exchange helpers
// ---------------------------------------------------------------------------

func (s *MarketScanner) getExchange(name string) (exchange.Exchange, error) {
	if s.exchangeFactory != nil {
		return s.exchangeFactory(name)
	}
	return exchange.New(name)
}

// ---------------------------------------------------------------------------
// Candidate discovery  (ported from _discover_candidates)
// ---------------------------------------------------------------------------

type universeItem struct {
	symbol string
	ticker *types.Ticker
	volume float64
}

func (s *MarketScanner) discoverCandidates(ctx context.Context, exch exchange.Exchange, exchangeName string, rsiMap map[string]data.CoinRSI) ([]types.Candidate, int, []string) {
	tickers, err := exch.FetchTickers(ctx)
	if err != nil || len(tickers) == 0 {
		if err != nil {
			return nil, 0, []string{fmt.Sprintf("%s did not return ticker data: %v", exchangeName, err)}
		}
		return nil, 0, []string{fmt.Sprintf("%s did not return ticker data.", exchangeName)}
	}

	var warnings []string
	var universe []universeItem

	for sym, ticker := range tickers {
		// Filter for USDT perpetuals
		tickerInfo := make(map[string]any)
		if ticker != nil && ticker.Info != nil {
			tickerInfo = ticker.Info
		}
		if !utils.LooksLikeUSDTPerpetual(sym, tickerInfo) {
			continue
		}

		volume := float64(0)
		if ticker != nil && ticker.QuoteVolume != nil {
			volume = *ticker.QuoteVolume
		}
		if volume <= 0 {
			warnings = append(warnings, fmt.Sprintf("%s missing quoteVolume; skipped from volume Top universe.", sym))
			continue
		}
		if s.blacklist.IsBlocked(sym) {
			continue
		}
		universe = append(universe, universeItem{symbol: sym, ticker: ticker, volume: volume})
	}

	// Sort by 24h quote volume descending
	sort.Slice(universe, func(i, j int) bool {
		return universe[i].volume > universe[j].volume
	})

	universeSize := len(universe)
	if universeSize > s.config.Scanner.UniverseSize {
		universe = universe[:s.config.Scanner.UniverseSize]
	}

	if fe, ok := exch.(exchange.FundingEnricher); ok {
		symbols := make([]string, len(universe))
		for i, item := range universe {
			symbols[i] = item.symbol
		}
		fe.EnrichFunding(ctx, tickers, symbols)
	}

	btcChange := s.btcChangeFromUniverse(universe)
	boosts := map[string]float64{}
	if s.hints != nil {
		boosts = s.hints.ActiveBoosts()
	}

	scored := make([]types.Candidate, len(universe))
	for i, item := range universe {
		scored[i] = s.scoreCandidate(item.symbol, exchangeName, item.ticker, btcChange, boosts[item.symbol], rsiMap)
	}

	// Sort by candidate score desc, break ties by volume desc
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].CandidateScore != scored[j].CandidateScore {
			return scored[i].CandidateScore > scored[j].CandidateScore
		}
		return scored[i].QuoteVolume24h > scored[j].QuoteVolume24h
	})

	if len(scored) > s.config.Scanner.CandidateLimit {
		scored = scored[:s.config.Scanner.CandidateLimit]
	}

	return scored, universeSize, warnings
}

func (s *MarketScanner) btcChangeFromUniverse(universe []universeItem) *float64 {
	btcSym, err := utils.NormalizeSymbol("BTC/USDT")
	if err != nil {
		return nil
	}
	for _, item := range universe {
		if item.symbol != btcSym || item.ticker == nil || item.ticker.ChangePct == nil {
			continue
		}
		v := *item.ticker.ChangePct
		return &v
	}
	return nil
}

// ---------------------------------------------------------------------------
// Ticker fetching
// ---------------------------------------------------------------------------

func (s *MarketScanner) fetchTicker(ctx context.Context, exch exchange.Exchange, symbol string) *types.Ticker {
	t, err := exch.FetchTicker(ctx, symbol)
	if err != nil {
		s.log.Debug("fetch ticker failed", "symbol", symbol, "error", err)
		return nil
	}
	return t
}

func (s *MarketScanner) loadRSIMap(ctx context.Context) (map[string]data.CoinRSI, string) {
	if s.rsiLoader != nil {
		return s.rsiLoader(ctx)
	}
	timeout := 8 * time.Second
	if sec := s.config.Scanner.ExchangeRequestTimeoutSeconds; sec > 0 {
		timeout = time.Duration(sec) * time.Second
	}
	_ = ctx
	m, err := data.FetchSpotRSIMap(timeout)
	if err != nil {
		s.log.Debug("coinglass rsi unavailable", "error", err)
		return nil, fmt.Sprintf("CoinGlass RSI unavailable: %v", err)
	}
	return m, ""
}

// ---------------------------------------------------------------------------
// OHLCV fetching  (ported from _fetch_ohlcv)
// ---------------------------------------------------------------------------

func (s *MarketScanner) fetchOHLCV(ctx context.Context, exch exchange.Exchange, symbol, timeframe string, limit int) *types.OHLCVArrays {
	candles, err := exch.FetchOHLCV(ctx, symbol, timeframe, limit, 0)
	if err != nil {
		s.log.Debug("fetch ohlcv failed", "symbol", symbol, "timeframe", timeframe, "error", err)
		return nil
	}
	return OHLCVToArrays(candles)
}

// ---------------------------------------------------------------------------
// BTC context  (ported from _load_btc_context)
// ---------------------------------------------------------------------------

func (s *MarketScanner) loadBTCContext(ctx context.Context, exch exchange.Exchange) (*btcContext, []string) {
	var warnings []string
	btcSymbol, err := utils.NormalizeSymbol("BTC/USDT")
	if err != nil {
		return nil, []string{"BTC symbol normalization failed"}
	}

	ohlcv := s.fetchOHLCV(ctx, exch, btcSymbol, "1d", 100)
	if ohlcv == nil || len(ohlcv.Closes) < 30 {
		return nil, []string{"BTC 1d OHLCV unavailable or insufficient; setup scoring withheld."}
	}

	cycle := s.cycleDetector.DetectPhase(ohlcv.Closes, ohlcv.Volumes, 0, 0, 0)
	if cycle.Confidence < 0.4 {
		warnings = append(warnings, "BTC cycle confidence is low; setup confidence degraded.")
	}

	return &btcContext{
		Symbol: btcSymbol,
		OHLCV:  ohlcv,
		Cycle:  cycle,
	}, warnings
}

// ---------------------------------------------------------------------------
// Candidate analysis  (ported from _analyze_candidate)
// ---------------------------------------------------------------------------

func (s *MarketScanner) analyzeCandidate(
	ctx context.Context,
	exch exchange.Exchange,
	candidate types.Candidate,
	btcCtx *btcContext,
) types.Setup {
	symbol := candidate.Symbol
	var warnings []string
	timeframeData := make(map[string]*types.OHLCVArrays)

	var tfMu sync.Mutex
	var tfWg sync.WaitGroup
	for _, tf := range s.config.Scanner.Timeframes {
		tfWg.Add(1)
		go func(timeframe string) {
			defer tfWg.Done()
			limit := 100
			if timeframe == "4h" || timeframe == "15m" {
				limit = 120
			}
			ohlcv := s.fetchOHLCV(ctx, exch, symbol, timeframe, limit)
			if ohlcv == nil || len(ohlcv.Closes) < 30 {
				tfMu.Lock()
				warnings = append(warnings, fmt.Sprintf("%s OHLCV unavailable or insufficient", timeframe))
				tfMu.Unlock()
				return
			}
			tfMu.Lock()
			timeframeData[timeframe] = ohlcv
			tfMu.Unlock()
		}(tf)
	}
	tfWg.Wait()

	// Check all required timeframes
	var missing []string
	for _, tf := range s.config.Scanner.Timeframes {
		if _, ok := timeframeData[tf]; !ok {
			missing = append(missing, tf)
		}
	}
	if len(missing) > 0 {
		return s.emptySetup(symbol, string(types.ActionStateWatch),
			[]string{fmt.Sprintf("missing required timeframes: %s", strings.Join(missing, ", "))},
			warnings)
	}

	currentPrice := timeframeData["15m"].Closes[len(timeframeData["15m"].Closes)-1]
	dailyTrend := Trend(timeframeData["1d"])
	structure := s.structure(symbol, "4h", timeframeData["4h"], currentPrice)
	boxLow := getMapFloat64(structure, "low")
	if boxLow > 0 {
		supportTriggered, supportNear := DetectSupportAtBoxLow(timeframeData["15m"], boxLow)
		if supportTriggered || supportNear {
			structure["entry_mode"] = "support"
			structure["support_triggered"] = supportTriggered
			structure["support_near"] = supportNear
		}
	}
	volConfirmed := VolumeConfirmed(timeframeData["15m"])

	longSetup := s.scoreDirection(types.DirectionLong, symbol, candidate,
		dailyTrend, structure, currentPrice, volConfirmed, btcCtx)
	shortSetup := s.scoreDirection(types.DirectionShort, symbol, candidate,
		dailyTrend, structure, currentPrice, volConfirmed, btcCtx)

	setup := longSetup
	if shortSetup.SetupScore > longSetup.SetupScore {
		setup = shortSetup
	}
	setup.Warnings = append(setup.Warnings, warnings...)
	return setup
}

// ---------------------------------------------------------------------------
// Structure detection  (ported from _structure)
// ---------------------------------------------------------------------------

func (s *MarketScanner) structure(symbol, timeframe string, ohlcv *types.OHLCVArrays, currentPrice float64) map[string]any {
	boxes := s.boxDetector.Detect(symbol, timeframe,
		ohlcv.Highs, ohlcv.Lows, ohlcv.Closes, ohlcv.Volumes, ohlcv.Timestamps)

	if len(boxes) > 0 {
		box := boxes[len(boxes)-1]
		return map[string]any{
			"type":          "box",
			"source":        "box_detector",
			"timeframe":     timeframe,
			"high":          box.High,
			"low":           box.Low,
			"height":        box.Height(),
			"height_pct":    box.HeightPct(),
			"status":        string(box.Status),
			"ready":         box.IsReady(),
			"current_price": currentPrice,
		}
	}

	// Fallback: recent 40-bar range
	n := len(ohlcv.Highs)
	start := n - 40
	if start < 0 {
		start = 0
	}
	high := maxVal(ohlcv.Highs[start:])
	low := minVal(ohlcv.Lows[start:])
	height := high - low
	heightPct := 0.0
	if low > 0 {
		heightPct = height / low * 100
	}
	ready := heightPct >= 1.0 && heightPct <= 15.0

	status := "range_unusable"
	if ready {
		status = "range_ready"
	}

	return map[string]any{
		"type":          "range",
		"source":        "recent_range",
		"timeframe":     timeframe,
		"high":          high,
		"low":           low,
		"height":        height,
		"height_pct":    heightPct,
		"status":        status,
		"ready":         ready,
		"current_price": currentPrice,
	}
}

// ---------------------------------------------------------------------------
// Empty setup  (ported from _empty_setup)
// ---------------------------------------------------------------------------

func (s *MarketScanner) emptySetup(symbol, actionState string, reasons, warnings []string) types.Setup {
	if reasons == nil {
		reasons = []string{}
	}
	if warnings == nil {
		warnings = []string{}
	}
	return types.Setup{
		Symbol:      symbol,
		Direction:   "",
		SetupType:   nil,
		ActionState: actionState,
		SetupScore:  0.0,
		Threshold:   nil,
		RequiredRiskReward: nil,
		Structure:   map[string]any{},
		Risk: types.RiskBounds{
			MaxPositionPct:   0.0,
			MaxLeverage:      0.0,
			EntryZone:        []float64{},
			StructuralStop:   nil,
			Targets:          []float64{},
			RiskReward:       0.0,
			RiskRewardTarget: nil,
			Invalidation:     nil,
			Triggered:        false,
			NearTrigger:      false,
			AccountSizing:    false,
		},
		ChartSpec:   s.chartSpec(symbol, 0.0, "none"),
		Fingerprint: "",
		Reasons:     reasons,
		Warnings:    warnings,
		Execution:   map[string]any{"enabled": false},
	}
}

// ---------------------------------------------------------------------------
// Serialisation helpers
// ---------------------------------------------------------------------------

func candidateToMap(c types.Candidate) map[string]any {
	m := map[string]any{
		"symbol":            c.Symbol,
		"exchange":          c.Exchange,
		"quote_volume_24h":  c.QuoteVolume24h,
		"candidate_score":   c.CandidateScore,
		"score_reasons":     c.ScoreReasons,
		"warnings":          c.Warnings,
	}
	if c.LastPrice != nil {
		m["last_price"] = *c.LastPrice
	}
	if c.Change24hPct != nil {
		m["change_24h_pct"] = *c.Change24hPct
	}
	return m
}

func setupToMap(st types.Setup) map[string]any {
	m := map[string]any{
		"symbol":       st.Symbol,
		"direction":    st.Direction,
		"action_state": st.ActionState,
		"setup_score":  st.SetupScore,
		"structure":    st.Structure,
		"risk":         riskBoundsToMap(st.Risk),
		"chart_spec":   st.ChartSpec,
		"fingerprint":  st.Fingerprint,
		"reasons":      st.Reasons,
		"warnings":     st.Warnings,
		"execution":    st.Execution,
	}
	if st.SetupType != nil {
		m["setup_type"] = *st.SetupType
	}
	if st.Threshold != nil {
		m["threshold"] = *st.Threshold
	}
	if st.RequiredRiskReward != nil {
		m["required_risk_reward"] = *st.RequiredRiskReward
	}
	return m
}

func riskBoundsToMap(r types.RiskBounds) map[string]any {
	m := map[string]any{
		"max_position_pct": r.MaxPositionPct,
		"max_leverage":     r.MaxLeverage,
		"entry_zone":       r.EntryZone,
		"targets":          r.Targets,
		"risk_reward":      r.RiskReward,
		"triggered":        r.Triggered,
		"near_trigger":     r.NearTrigger,
		"account_sizing":   r.AccountSizing,
	}
	if r.StructuralStop != nil {
		m["structural_stop"] = *r.StructuralStop
	}
	if r.RiskRewardTarget != nil {
		m["risk_reward_target"] = *r.RiskRewardTarget
	}
	if r.Invalidation != nil {
		m["invalidation"] = *r.Invalidation
	}
	return m
}
