package opportunity

import (
	"context"
	"fmt"
	"time"

	"github.com/ArchdevilForge/kairos/internal/cycle"
	"github.com/ArchdevilForge/kairos/internal/ranker"
	"github.com/ArchdevilForge/kairos/internal/types"
)

// OHLCVFetcher is the exchange surface needed for cycle enrichment.
type OHLCVFetcher interface {
	FetchOHLCV(ctx context.Context, symbol, timeframe string, limit int, beforeMs int64) ([]types.Candle, error)
}

// EnrichConfig controls auto ticket generation after a pulse.
type EnrichConfig struct {
	Timeframes   []string
	BarLimit     int
	MaxSymbols   int
	MarketSymbol string
	Timeout      time.Duration
	MinQuoteVol  float64
	// AssumeSpreadOK is test/shadow-only when no L2 feed exists.
	// Production must leave false (fail closed on unmeasured spread).
	AssumeSpreadOK bool
	// WatchInterval re-checks for post-pulse pullback until SessionTTL.
	WatchInterval time.Duration
	// RequireInSessionPullback: pullback extreme must also be after pulse.
	RequireInSessionPullback bool
}

// DefaultEnrichConfig returns production fail-closed defaults.
func DefaultEnrichConfig() EnrichConfig {
	return EnrichConfig{
		Timeframes:               []string{"1d", "4h", "15m", "5m"},
		BarLimit:                 90,
		MaxSymbols:               3,
		MarketSymbol:             "BTC/USDT:USDT",
		Timeout:                  20 * time.Second,
		MinQuoteVol:              1_000_000,
		AssumeSpreadOK:           false,
		WatchInterval:            5 * time.Minute,
		RequireInSessionPullback: true, // leader_pullback_v1: first pullback after pulse
	}
}

// EnrichRequest is pulse + OHLCV fetch → tickets.
type EnrichRequest struct {
	Event   types.AnomalyEvent
	Fetcher OHLCVFetcher
	Config  EnrichConfig
	Equity  float64
}

// EnrichAndEvaluate fetches OHLCV, measures features, detects pullback trigger, attaches tickets.
func (s *Service) EnrichAndEvaluate(ctx context.Context, req EnrichRequest) (EvaluateResult, error) {
	var empty EvaluateResult
	if s == nil || !s.cfg.Enabled || req.Fetcher == nil {
		return empty, nil
	}
	cfg := req.Config
	if len(cfg.Timeframes) == 0 {
		cfg.Timeframes = DefaultEnrichConfig().Timeframes
	}
	if cfg.BarLimit <= 0 {
		cfg.BarLimit = 90
	}
	if cfg.MaxSymbols <= 0 {
		cfg.MaxSymbols = 3
	}
	if cfg.MarketSymbol == "" {
		cfg.MarketSymbol = "BTC/USDT:USDT"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 20 * time.Second
	}
	if cfg.MinQuoteVol <= 0 {
		cfg.MinQuoteVol = 1_000_000
	}
	if req.Equity > 0 {
		s.cfg.Equity = req.Equity
	}

	dir := pulseDirectionFromEvent(req.Event)
	state := pulseStateFromEvent(req.Event)
	eventID := req.Event.EventID
	if eventID == "" {
		eventID = fmt.Sprintf("%s-%.0f", req.Event.EventType, req.Event.Timestamp)
	}

	// soft list from pulse for symbol order only
	softInputs := RankInputsFromPulse(req.Event)
	if len(softInputs) == 0 {
		return empty, nil
	}
	var orderedSyms []string
	for _, c := range ranker.Rank(softInputs, ranker.SoftConfig()) {
		orderedSyms = append(orderedSyms, c.Symbol)
		if len(orderedSyms) >= cfg.MaxSymbols {
			break
		}
	}
	// prefer side order
	switch dir {
	case types.CycleDirectionUp:
		orderedSyms = nil
		for _, c := range ranker.RankLong(softInputs, ranker.SoftConfig()) {
			orderedSyms = append(orderedSyms, c.Symbol)
			if len(orderedSyms) >= cfg.MaxSymbols {
				break
			}
		}
	case types.CycleDirectionDown:
		orderedSyms = nil
		for _, c := range ranker.RankShort(softInputs, ranker.SoftConfig()) {
			orderedSyms = append(orderedSyms, c.Symbol)
			if len(orderedSyms) >= cfg.MaxSymbols {
				break
			}
		}
	}

	bySoft := map[string]ranker.Input{}
	for _, in := range softInputs {
		bySoft[in.Symbol] = in
	}

	fetchCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	marketCycle, err := s.buildSymbolCycle(fetchCtx, req.Fetcher, cfg.MarketSymbol, cfg)
	if err != nil {
		s.log.Warn("market cycle fetch failed", "error", err, "symbol", cfg.MarketSymbol)
		return empty, fmt.Errorf("market cycle: %w", err)
	}

	symbolCycles := map[string]types.CycleMap{}
	invalidations := map[string][]string{}
	structureOK := map[string]bool{}
	entryPx := map[string]float64{}
	stopPx := map[string]float64{}
	triggeredAt := map[string]int64{}
	rankInputs := make([]ranker.Input, 0, len(orderedSyms))
	median := 0.0
	if len(softInputs) > 0 {
		median = softInputs[0].MarketMedianChange
	}

	for _, sym := range orderedSyms {
		base := bySoft[sym]
		symCycle, err := s.buildSymbolCycle(fetchCtx, req.Fetcher, sym, cfg)
		if err != nil {
			s.log.Warn("symbol cycle fetch failed", "symbol", sym, "error", err)
			continue
		}
		symbolCycles[sym] = symCycle

		candles5m, err := req.Fetcher.FetchOHLCV(fetchCtx, sym, "5m", cfg.BarLimit, 0)
		if err != nil || len(candles5m) < 30 {
			s.log.Warn("trigger bars missing", "symbol", sym, "error", err)
			continue
		}
		// closed bars only
		candles5m = candles5m[:len(candles5m)-1]

		pulseAt := int64(req.Event.Timestamp)
		if pulseAt > 1_000_000_000_000 {
			pulseAt = pulseAt / 1000
		}
		trig := DetectPullbackTrigger(dir, candles5m, TriggerOpts{
			MinRestartAt:             pulseAt,
			MinPullbackAt:            pulseAt,
			RequireInSessionPullback: cfg.RequireInSessionPullback,
		})
		if !trig.OK {
			s.log.Info("pullback trigger not matched", "symbol", sym, "failures", trig.Failures)
			continue
		}

		// measure quote volume proxy from recent 5m notional
		qVol := measureQuoteVolume(candles5m)
		liqOK := qVol >= cfg.MinQuoteVol

		in := ranker.Input{
			Symbol:             sym,
			ChangePct:          base.ChangePct,
			MarketMedianChange: median,
			BTCChange:          base.BTCChange,
			BTCChangeSet:       base.BTCChangeSet,
			QuoteVolume:        qVol,
			MinLiquidity:       cfg.MinQuoteVol,
			DataOK:             true,
			LiquidityMeasured:  true,
			LiquidityOK:        liqOK,
			SpreadMeasured:     cfg.AssumeSpreadOK,
			SpreadOK:           cfg.AssumeSpreadOK,
			PullbackMeasured:   dir != types.CycleDirectionDown,
			PullbackDepthPct:   trig.PullbackDepthPct,
			ReboundMeasured:    dir == types.CycleDirectionDown,
			ReboundPct:         trig.ReboundPct,
			RoomMeasured:       true,
			RoomUpPct:          0,
			RoomDownPct:        0,
		}
		if n, ok := symCycle.Nodes["1d"]; ok {
			in.RoomUpPct = n.RoomUpPct
			in.RoomDownPct = n.RoomDownPct
		}
		if !liqOK {
			s.log.Info("liquidity fail closed", "symbol", sym, "quote_vol", qVol)
			continue
		}
		if !in.SpreadMeasured {
			s.log.Info("spread unmeasured fail closed", "symbol", sym)
			continue
		}

		entryPx[sym] = trig.Entry
		stopPx[sym] = trig.Stop
		invalidations[sym] = trig.Invalidations
		structureOK[sym] = true
		triggeredAt[sym] = trig.EntryTriggeredAt
		rankInputs = append(rankInputs, in)
	}

	if len(rankInputs) == 0 {
		return empty, nil
	}

	created := int64(req.Event.Timestamp)
	if created <= 0 {
		created = time.Now().Unix()
	}
	res, err := s.EvaluateOrAttach(EvaluateRequest{
		EventID:        eventID,
		CreatedAt:      created,
		SignalAt:       created,
		PulseState:     state,
		PulseDirection: dir,
		MarketCycle:    marketCycle,
		SymbolCycles:   symbolCycles,
		RankInputs:     rankInputs,
		Invalidations:  invalidations,
		StructureValid: structureOK,
		EntryPrice:     entryPx,
		StopPrice:      stopPx,
		TriggeredAt:    triggeredAt,
	})
	return res, err
}

func measureQuoteVolume(candles []types.Candle) float64 {
	if len(candles) == 0 {
		return 0
	}
	// last 24 bars of 5m ≈ 2h notional proxy; scale toward 24h-ish *12 for gate ballpark
	n := 24
	if len(candles) < n {
		n = len(candles)
	}
	var sum float64
	for i := len(candles) - n; i < len(candles); i++ {
		sum += candles[i].Close * candles[i].Volume
	}
	// crude 24h extrapolation from 2h window
	return sum * (24.0 / (float64(n) * 5.0 / 60.0))
}

// buildSymbolCycle is a Service method so CycleService hysteresis is not shared via globals.
func (s *Service) buildSymbolCycle(ctx context.Context, fetch OHLCVFetcher, symbol string, cfg EnrichConfig) (types.CycleMap, error) {
	var series []cycle.Series
	var asOf int64
	for _, tf := range cfg.Timeframes {
		candles, err := fetch.FetchOHLCV(ctx, symbol, tf, cfg.BarLimit, 0)
		if err != nil {
			return types.CycleMap{}, fmt.Errorf("%s %s: %w", symbol, tf, err)
		}
		if len(candles) < 41 {
			return types.CycleMap{}, fmt.Errorf("%s %s: need >=41 bars got %d", symbol, tf, len(candles))
		}
		// drop potentially forming last bar
		candles = candles[:len(candles)-1]
		closes := make([]float64, len(candles))
		highs := make([]float64, len(candles))
		lows := make([]float64, len(candles))
		vols := make([]float64, len(candles))
		for i, c := range candles {
			closes[i], highs[i], lows[i], vols[i] = c.Close, c.High, c.Low, c.Volume
		}
		last := candles[len(candles)-1]
		openTS := normCandleTS(last.Timestamp)
		closeTS := barCloseUnix(openTS, tf)
		if closeTS > asOf {
			asOf = closeTS
		}
		series = append(series, cycle.Series{
			Timeframe:        tf,
			Role:             roleForTimeframe(tf),
			LastBarUnix:      openTS,
			LastBarCloseUnix: closeTS,
			Closes:           closes,
			Highs:            highs,
			Lows:             lows,
			Volumes:          vols,
		})
	}
	// legacy climate unknown — do not invent summer
	var legacy types.MarketPhase
	if s != nil && s.cycles != nil {
		return s.cycles.Map(symbol, asOf, legacy, series), nil
	}
	return cycle.MapStateless(symbol, asOf, legacy, series), nil
}

func normCandleTS(ts int64) int64 {
	if ts > 1_000_000_000_000 {
		return ts / 1000
	}
	return ts
}

// barCloseUnix assumes exchange timestamps are bar *open* times.
func barCloseUnix(openUnix int64, tf string) int64 {
	if openUnix <= 0 {
		return 0
	}
	sec := int64(300) // default 5m
	switch tf {
	case "1m", "1M":
		sec = 60
	case "5m", "5M":
		sec = 300
	case "15m", "15M":
		sec = 900
	case "1h", "1H":
		sec = 3600
	case "4h", "4H":
		sec = 14400
	case "1d", "1D":
		sec = 86400
	}
	return openUnix + sec
}

func roleForTimeframe(tf string) types.TimeframeRole {
	switch tf {
	case "1d", "1D", "4h", "4H":
		return types.TimeframeRoleContext
	case "1h", "1H", "15m", "15M":
		return types.TimeframeRoleSetup
	case "5m", "5M":
		return types.TimeframeRoleTrigger
	default:
		return types.TimeframeRoleSetup
	}
}
