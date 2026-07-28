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
}

// DefaultEnrichConfig returns stage-1 multi-TF defaults.
func DefaultEnrichConfig() EnrichConfig {
	return EnrichConfig{
		Timeframes:   []string{"1d", "4h", "15m", "5m"},
		BarLimit:     90,
		MaxSymbols:   3,
		MarketSymbol: "BTC/USDT:USDT",
		Timeout:      20 * time.Second,
	}
}

// EnrichRequest is pulse + OHLCV fetch → tickets.
type EnrichRequest struct {
	Event   types.AnomalyEvent
	Fetcher OHLCVFetcher
	Config  EnrichConfig
	Equity  float64
}

// EnrichAndEvaluate fetches OHLCV, builds CycleMaps, attaches tickets to the pulse session.
// Fail closed per symbol on fetch errors; market cycle failure → error, no tickets.
func (s *Service) EnrichAndEvaluate(ctx context.Context, req EnrichRequest) (EvaluateResult, error) {
	var empty EvaluateResult
	if s == nil || !s.cfg.Enabled || req.Fetcher == nil {
		return empty, nil
	}
	cfg := req.Config
	if len(cfg.Timeframes) == 0 {
		cfg = DefaultEnrichConfig()
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
	if req.Equity > 0 {
		s.cfg.Equity = req.Equity
	}

	dir := pulseDirectionFromEvent(req.Event)
	state := pulseStateFromEvent(req.Event)
	eventID := req.Event.EventID
	if eventID == "" {
		eventID = fmt.Sprintf("%s-%.0f", req.Event.EventType, req.Event.Timestamp)
	}

	inputs := RankInputsFromPulse(req.Event)
	if len(inputs) == 0 {
		return empty, nil
	}

	var orderedSyms []string
	switch dir {
	case types.CycleDirectionUp:
		for _, c := range ranker.RankLong(inputs, ranker.DefaultConfig()) {
			orderedSyms = append(orderedSyms, c.Symbol)
		}
	case types.CycleDirectionDown:
		for _, c := range ranker.RankShort(inputs, ranker.DefaultConfig()) {
			orderedSyms = append(orderedSyms, c.Symbol)
		}
	default:
		for _, in := range inputs {
			orderedSyms = append(orderedSyms, in.Symbol)
		}
	}
	if len(orderedSyms) > cfg.MaxSymbols {
		orderedSyms = orderedSyms[:cfg.MaxSymbols]
	}

	bySym := map[string]ranker.Input{}
	for _, in := range inputs {
		bySym[in.Symbol] = in
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
	rankInputs := make([]ranker.Input, 0, len(orderedSyms))

	for _, sym := range orderedSyms {
		in, ok := bySym[sym]
		if !ok {
			continue
		}
		symCycle, err := buildSymbolCycle(fetchCtx, req.Fetcher, sym, cfg)
		if err != nil {
			s.log.Warn("symbol cycle fetch failed", "symbol", sym, "error", err)
			continue
		}
		symbolCycles[sym] = symCycle

		candles5m, err := req.Fetcher.FetchOHLCV(fetchCtx, sym, "5m", cfg.BarLimit, 0)
		if err != nil || len(candles5m) < 10 {
			s.log.Warn("trigger bars missing", "symbol", sym, "error", err)
			continue
		}
		if len(candles5m) > 10 {
			candles5m = candles5m[:len(candles5m)-1] // closed bar
		}
		entry, stop, inv, ok := triggerPlan(dir, candles5m)
		if !ok {
			continue
		}
		entryPx[sym] = entry
		stopPx[sym] = stop
		invalidations[sym] = inv
		structureOK[sym] = true
		if n, ok := symCycle.Nodes["1d"]; ok {
			in.RoomUpPct = n.RoomUpPct
			in.RoomDownPct = n.RoomDownPct
		}
		rankInputs = append(rankInputs, in)
	}

	if len(rankInputs) == 0 {
		return empty, nil
	}

	created := int64(req.Event.Timestamp)
	if created <= 0 {
		created = time.Now().Unix()
	}
	return s.EvaluateOrAttach(EvaluateRequest{
		EventID:        eventID,
		CreatedAt:      created,
		PulseState:     state,
		PulseDirection: dir,
		MarketCycle:    marketCycle,
		SymbolCycles:   symbolCycles,
		RankInputs:     rankInputs,
		Invalidations:  invalidations,
		StructureValid: structureOK,
		EntryPrice:     entryPx,
		StopPrice:      stopPx,
	})
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
		// drop last as potentially forming
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

// triggerPlan derives entry/stop/invalidation from 5m closed bars.
func triggerPlan(dir types.CycleDirection, candles []types.Candle) (entry, stop float64, inv []string, ok bool) {
	if len(candles) < 8 {
		return 0, 0, nil, false
	}
	last := candles[len(candles)-1]
	entry = last.Close
	window := candles[len(candles)-6 : len(candles)-1]
	switch dir {
	case types.CycleDirectionDown:
		stop = window[0].High
		for _, c := range window {
			if c.High > stop {
				stop = c.High
			}
		}
		if stop <= entry {
			stop = entry * 1.015
		}
		inv = []string{
			fmt.Sprintf("5m swing high %.6g breaks", stop),
			"15m flips UP with structure break",
		}
		return entry, stop, inv, true
	default: // up or neutral → long plan
		stop = window[0].Low
		for _, c := range window {
			if c.Low < stop {
				stop = c.Low
			}
		}
		if stop >= entry || stop <= 0 {
			stop = entry * 0.985
		}
		inv = []string{
			fmt.Sprintf("5m swing low %.6g breaks", stop),
			"15m flips DOWN with structure break",
		}
		return entry, stop, inv, true
	}
}
