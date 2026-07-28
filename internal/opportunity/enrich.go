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
}

// DefaultEnrichConfig returns production fail-closed defaults.
func DefaultEnrichConfig() EnrichConfig {
	return EnrichConfig{
		Timeframes:     []string{"1d", "4h", "15m", "5m"},
		BarLimit:       90,
		MaxSymbols:     3,
		MarketSymbol:   "BTC/USDT:USDT",
		Timeout:        20 * time.Second,
		MinQuoteVol:    1_000_000,
		AssumeSpreadOK: false,
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

	marketCycle, err := buildSymbolCycle(fetchCtx, req.Fetcher, cfg.MarketSymbol, cfg)
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
		symCycle, err := buildSymbolCycle(fetchCtx, req.Fetcher, sym, cfg)
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

		trig := DetectPullbackTrigger(dir, candles5m)
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

func buildSymbolCycle(ctx context.Context, fetch OHLCVFetcher, symbol string, cfg EnrichConfig) (types.CycleMap, error) {
	var series []cycle.Series
	for _, tf := range cfg.Timeframes {
		candles, err := fetch.FetchOHLCV(ctx, symbol, tf, cfg.BarLimit, 0)
		if err != nil {
			return types.CycleMap{}, fmt.Errorf("%s %s: %w", symbol, tf, err)
		}
		if len(candles) < 41 {
			return types.CycleMap{}, fmt.Errorf("%s %s: need >=41 bars got %d", symbol, tf, len(candles))
		}
		candles = candles[:len(candles)-1]
		closes := make([]float64, len(candles))
		highs := make([]float64, len(candles))
		lows := make([]float64, len(candles))
		vols := make([]float64, len(candles))
		for i, c := range candles {
			closes[i], highs[i], lows[i], vols[i] = c.Close, c.High, c.Low, c.Volume
		}
		series = append(series, cycle.Series{
			Timeframe: tf,
			Role:      roleForTimeframe(tf),
			Closes:    closes,
			Highs:     highs,
			Lows:      lows,
			Volumes:   vols,
		})
	}
	return cycle.MapFromOHLCV(symbol, time.Now().Unix(), types.MarketPhaseSummer, series), nil
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
