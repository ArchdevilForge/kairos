// Package backtest runs historical backtests for the kairos box-breakout strategy.
//
// Ported from kairos/backtest.py:
//   - OHLCV data fetching with CCXT pagination
//   - Box breakout detection + entry logic
//   - Fee + slippage model (round-trip cost)
//   - Stop loss / take profit exit logic
//   - Position tracking + trade log
//   - Summary statistics (Sharpe, Calmar, profit factor, max drawdown, etc.)
package backtest

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"gonum.org/v1/gonum/stat"

	"github.com/ArchdevilForge/kairos/internal/exchange"
	"github.com/ArchdevilForge/kairos/internal/indicators"
	"github.com/ArchdevilForge/kairos/internal/types"
)

// ExitReason enumerates why a trade was closed.
type ExitReason string

const (
	ExitReasonStopLoss    ExitReason = "stop_loss"
	ExitReasonTakeProfit  ExitReason = "take_profit"
	ExitReasonEndOfPeriod ExitReason = "end_of_period"
)

// Trade holds all fields for one completed trade.
type Trade struct {
	Symbol       string  `json:"symbol"`
	Direction    string  `json:"direction"`
	EntryPrice   float64 `json:"entry_price"`
	EntryTime    string  `json:"entry_time"`
	ExitPrice    float64 `json:"exit_price"`
	ExitTime     string  `json:"exit_time"`
	PnlPct       float64 `json:"pnl_pct"`
	ExitReason   string  `json:"exit_reason"`
	EntryTimeMs  float64 `json:"entry_time_ms"`
	ExitTimeMs   float64 `json:"exit_time_ms"`
	HoldingHours float64 `json:"holding_hours"`
}

// Summary holds aggregate backtest statistics.
type Summary struct {
	TotalTrades     int     `json:"total_trades"`
	WinningTrades   int     `json:"winning_trades"`
	LosingTrades    int     `json:"losing_trades"`
	WinRatePct      float64 `json:"win_rate_pct"`
	AvgPnlPct       float64 `json:"avg_pnl_pct"`
	TotalPnlPct     float64 `json:"total_pnl_pct"`
	MaxDrawdownPct  float64 `json:"max_drawdown_pct"`
	AvgWinPnlPct    float64 `json:"avg_win_pnl_pct"`
	AvgLossPnlPct   float64 `json:"avg_loss_pnl_pct"`
	SharpeRatio     float64 `json:"sharpe_ratio"`
	CalmarRatio     float64 `json:"calmar_ratio"`
	ProfitFactor    float64 `json:"profit_factor"`
	AvgHoldingHours float64 `json:"avg_holding_hours"`
	PositionPct     float64 `json:"position_pct"`
}

// Result bundles the summary and trade log returned from Run.
type Result struct {
	Summary Summary        `json:"summary"`
	Trades  []Trade        `json:"trades"`
}

// BacktestRunner executes the box-breakout backtest on historical OHLCV data.
type BacktestRunner struct {
	exch          exchange.Exchange
	boxDetector   *indicators.BoxDetector
	cycleDetector *indicators.CycleDetector
}

// New creates a BacktestRunner wrapping the given exchange.
func New(exch exchange.Exchange) *BacktestRunner {
	return NewWithConfig(exch, indicators.DefaultBoxDetectorConfig())
}

// NewWithConfig creates a BacktestRunner with custom box detector settings.
func NewWithConfig(exch exchange.Exchange, boxCfg indicators.BoxDetectorConfig) *BacktestRunner {
	return &BacktestRunner{
		exch:          exch,
		boxDetector:   indicators.NewBoxDetector(boxCfg),
		cycleDetector: indicators.NewCycleDetector(indicators.DefaultCycleDetectorConfig()),
	}
}

// RunOption allows optional configuration of the backtest run.
type RunOption struct {
	Timeframe   string
	FeePct      float64
	SlippagePct float64
	PositionPct float64
	BtcData     *types.OHLCVArrays // pre-fetched BTC 1d data for cycle detection
}

// defaultRunOption returns sensible defaults matching the Python CLI defaults.
func defaultRunOption() RunOption {
	return RunOption{
		Timeframe:   "4h",
		FeePct:      0.04,
		SlippagePct: 0.02,
		PositionPct: 100.0,
	}
}

// Run executes the backtest for one symbol over start–end and returns results.
// symbol: trading pair (e.g. "BTC/USDT").
// start, end: date range in "YYYY-MM-DD" format.
// opt: optional fields; zero-valued fields are filled with defaults.
func (r *BacktestRunner) Run(ctx context.Context, symbol, start, end string, opt RunOption) (*Result, error) {
	if opt.Timeframe == "" {
		opt.Timeframe = "4h"
	}
	if opt.FeePct == 0 && opt.SlippagePct == 0 && opt.PositionPct == 0 {
		d := defaultRunOption()
		opt.FeePct = d.FeePct
		opt.SlippagePct = d.SlippagePct
		opt.PositionPct = d.PositionPct
	}
	if opt.FeePct == 0 {
		opt.FeePct = 0.04
	}
	if opt.SlippagePct == 0 {
		opt.SlippagePct = 0.02
	}
	if opt.PositionPct == 0 {
		opt.PositionPct = 100.0
	}

	data, err := r.fetchOHLCV(ctx, symbol, start, end, opt.Timeframe)
	if err != nil {
		return nil, fmt.Errorf("backtest %s %s→%s: %w", symbol, start, end, err)
	}
	n := len(data.Closes)
	if n < 50 {
		return nil, fmt.Errorf("backtest %s: insufficient data (%d bars, need ≥50)", symbol, n)
	}

	// Round-trip cost: fee + slippage on both entry and exit
	roundTripCost := (opt.FeePct + opt.SlippagePct) * 2

	// BTC cycle data: use pre-fetched 1d data, fall back to symbol's own data
	btcCloses := data.Closes
	btcVolumes := data.Volumes
	var btcTimestamps []float64
	if opt.BtcData != nil && len(opt.BtcData.Closes) >= 30 {
		btcCloses = opt.BtcData.Closes
		btcVolumes = opt.BtcData.Volumes
		btcTimestamps = opt.BtcData.Timestamps
	}

	// Detect all boxes upfront; the bar loop filters by end_time
	allBoxes := r.boxDetector.Detect(symbol, opt.Timeframe,
		data.Highs, data.Lows, data.Closes, data.Volumes, data.Timestamps)

	var trades []Trade
	var pos *position

	// ponytail: BoxDetector default MinBars is 10, so 10*2=20 but we floor at 50.
	const warmup = 50

	// Initial cycle computation
	cycle := r.computeCycle(btcCloses[:maxInt(30, minInt(50, len(btcCloses)))],
		btcVolumes[:maxInt(30, minInt(50, len(btcVolumes)))])

	const cycleInterval = 12

	for i := warmup; i < n; i++ {
		currentTsMs := data.Timestamps[i]
		currentTsStr := tsToISO(currentTsMs)

		// Per-bar cycle recomputation every cycleInterval bars
		if i >= 30 && i%cycleInterval == 0 {
			if opt.BtcData != nil && len(btcTimestamps) > 0 {
				btcIdx := sort.SearchFloat64s(btcTimestamps, currentTsMs)
				if btcIdx < 30 {
					btcIdx = 30
				}
				if btcIdx > len(btcCloses) {
					btcIdx = len(btcCloses)
				}
				cycle = r.computeCycle(btcCloses[:btcIdx], btcVolumes[:btcIdx])
			} else {
				cycle = r.computeCycle(data.Closes[:i+1], data.Volumes[:i+1])
			}
		}

		// --- Check exit on existing position ---
		if pos != nil {
			t := checkExit(pos, data.Highs[i], data.Lows[i], currentTsStr, currentTsMs, roundTripCost)
			if t != nil {
				t.Symbol = symbol
				trades = append(trades, *t)
				pos = nil
				continue
			}
		}

		// --- Entry signals ---
		for j := range allBoxes {
			b := &allBoxes[j]
			if b.EndTime > currentTsMs {
				continue
			}
			if b.Status != types.BoxStatusConverging && b.Status != types.BoxStatusForming {
				continue
			}

			direction := detectBreakout(b, data.Highs[i], data.Lows[i], data.Volumes, i)
			if direction == "" {
				continue
			}
			if !cycleSupports(cycle.Phase, direction) {
				continue
			}

			entryPrice, stop, target := calcEntryPrice(b.High, b.Low, direction)

			pos = &position{
				direction:   direction,
				entryPrice:  entryPrice,
				entryTime:   currentTsStr,
				entryTimeMs: currentTsMs,
				stop:        stop,
				target:      target,
			}
			// Mark box as broken out
			if direction == types.DirectionLong {
				b.Status = types.BoxStatusBreakoutUp
			} else {
				b.Status = types.BoxStatusBreakoutDown
			}
			break
		}
	}

	// Close any remaining position at end of data
	if pos != nil {
		lastClose := data.Closes[n-1]
		lastTs := tsToISO(data.Timestamps[n-1])
		grossPnl := calcPnl(pos.direction, pos.entryPrice, lastClose)
		trades = append(trades, Trade{
			Symbol:       symbol,
			Direction:    string(pos.direction),
			EntryPrice:   pos.entryPrice,
			EntryTime:    pos.entryTime,
			EntryTimeMs:  pos.entryTimeMs,
			ExitPrice:    lastClose,
			ExitTime:     lastTs,
			ExitTimeMs:   data.Timestamps[n-1],
			PnlPct:       grossPnl - roundTripCost,
			ExitReason:   string(ExitReasonEndOfPeriod),
			HoldingHours: holdingHours(pos.entryTime, lastTs),
		})
	}

	summary := computeSummary(trades, start, end, opt.PositionPct)
	return &Result{Summary: *summary, Trades: trades}, nil
}

// --------------------------------------------------------------------------
// Internal types
// --------------------------------------------------------------------------

// position tracks an open position.
type position struct {
	direction   types.Direction
	entryPrice  float64
	entryTime   string
	entryTimeMs float64
	stop        float64
	target      float64
}

// --------------------------------------------------------------------------
// OHLCV fetching with CCXT-style pagination
// --------------------------------------------------------------------------

func (r *BacktestRunner) fetchOHLCV(ctx context.Context, symbol, start, end, timeframe string) (*types.OHLCVArrays, error) {
	startMs := parseDate(start)
	endMs := parseDate(end)

	var all []types.Candle
	cursor := endMs

	// ponytail: OKX history-candles paginates backward via `after`; ccxt-style forward
	// since loops do not work. Walk from end→start, then sort ascending.
	for {
		candles, err := r.exch.FetchOHLCV(ctx, symbol, timeframe, 300, cursor)
		if err != nil {
			return nil, fmt.Errorf("fetch OHLCV: %w", err)
		}
		if len(candles) == 0 {
			break
		}

		for _, c := range candles {
			tsMs := c.Timestamp * 1000
			if tsMs >= startMs && tsMs <= endMs {
				all = append(all, c)
			}
		}

		oldest := candles[0].Timestamp * 1000
		for _, c := range candles[1:] {
			if ts := c.Timestamp * 1000; ts < oldest {
				oldest = ts
			}
		}
		if oldest <= startMs {
			break
		}
		if oldest >= cursor {
			break
		}
		cursor = oldest
	}

	if len(all) == 0 {
		return nil, fmt.Errorf("no data returned for %s %s→%s", symbol, start, end)
	}

	// Sort chronological (oldest first)
	sort.Slice(all, func(i, j int) bool { return all[i].Timestamp < all[j].Timestamp })

	return candlesToArrays(all), nil
}

// FetchBtc1d fetches BTC/USDT daily data for cycle detection, identical in
// role to BacktestRunner.fetch_btc_cycle_data in Python.
func (r *BacktestRunner) FetchBtc1d(ctx context.Context, start, end string) (*types.OHLCVArrays, error) {
	return r.fetchOHLCV(ctx, "BTC/USDT", start, end, "1d")
}

// --------------------------------------------------------------------------
// Cycle / entry helpers
// --------------------------------------------------------------------------

// computeCycle wraps cycle detection on price/volume slices.
func (r *BacktestRunner) computeCycle(closes, volumes []float64) types.MarketCycle {
	// ponytail: minimal call — CycleDetector defaults match Python defaults
	return r.cycleDetector.DetectPhase(closes, volumes, 0, 0, 0)
}

// cycleSupports checks whether the market phase supports the given direction.
func cycleSupports(phase types.MarketPhase, dir types.Direction) bool {
	switch phase {
	case types.MarketPhaseSpring:
		return dir == types.DirectionLong
	case types.MarketPhaseSummer:
		return dir == types.DirectionLong
	case types.MarketPhaseWinter:
		return dir == types.DirectionShort
	default: // AUTUMN — both
		return true
	}
}

// detectBreakout checks if the current bar triggers a box breakout with volume
// confirmation. Returns the direction or empty string.
func detectBreakout(box *types.BoxPattern, barHigh, barLow float64, volumes []float64, barIdx int) types.Direction {
	volStart := barIdx - 19
	if volStart < 0 {
		volStart = 0
	}
	avgVol := mean(volumes[volStart : barIdx+1])

	if barHigh > box.High*1.005 && volumes[barIdx] > avgVol*1.5 {
		return types.DirectionLong
	}
	if barLow < box.Low*0.995 && volumes[barIdx] > avgVol*1.5 {
		return types.DirectionShort
	}
	return ""
}

// calcEntryPrice computes the entry price, stop-loss, and take-profit based
// on the box boundaries and direction.
func calcEntryPrice(boxHigh, boxLow float64, dir types.Direction) (entry, stop, target float64) {
	if dir == types.DirectionLong {
		entry = boxHigh * 1.005
		stop = boxLow * 0.995
		target = entry + max(boxHigh-boxLow, entry*0.01)
	} else {
		entry = boxLow * 0.995
		stop = boxHigh * 1.005
		target = entry - max(boxHigh-boxLow, entry*0.01)
	}
	return
}

// --------------------------------------------------------------------------
// Exit helpers
// --------------------------------------------------------------------------

// checkExit tests a position against the current bar. Returns a completed
// Trade if stop or target is hit, otherwise nil.
func checkExit(pos *position, barHigh, barLow float64, currentTs string, currentTsMs, roundTripCost float64) *Trade {
	entryPrice := pos.entryPrice
	dir := pos.direction

	var exitPrice float64
	var reason ExitReason

	if dir == types.DirectionLong {
		if barLow <= pos.stop {
			exitPrice = pos.stop
			reason = ExitReasonStopLoss
		} else if barHigh >= pos.target {
			exitPrice = pos.target
			reason = ExitReasonTakeProfit
		} else {
			return nil
		}
	} else {
		if barHigh >= pos.stop {
			exitPrice = pos.stop
			reason = ExitReasonStopLoss
		} else if barLow <= pos.target {
			exitPrice = pos.target
			reason = ExitReasonTakeProfit
		} else {
			return nil
		}
	}

	grossPnl := calcPnl(dir, entryPrice, exitPrice)
	return &Trade{
		Direction:    string(dir),
		EntryPrice:   entryPrice,
		EntryTime:    pos.entryTime,
		EntryTimeMs:  pos.entryTimeMs,
		ExitPrice:    exitPrice,
		ExitTime:     currentTs,
		ExitTimeMs:   currentTsMs,
		PnlPct:       grossPnl - roundTripCost,
		ExitReason:   string(reason),
		HoldingHours: holdingHours(pos.entryTime, currentTs),
	}
}

// calcPnl computes gross P&L percentage before costs.
func calcPnl(dir types.Direction, entry, exitPrice float64) float64 {
	if entry == 0 {
		return 0
	}
	if dir == types.DirectionLong {
		return (exitPrice - entry) / entry * 100
	}
	return (entry - exitPrice) / entry * 100
}

// --------------------------------------------------------------------------
// Summary statistics
// --------------------------------------------------------------------------

// computeSummary produces aggregate statistics from a trade list.
func computeSummary(trades []Trade, start, end string, positionPct float64) *Summary {
	s := &Summary{PositionPct: positionPct}
	n := len(trades)
	if n == 0 {
		return s
	}

	s.TotalTrades = n

	var wins, losses []float64
	pnlList := make([]float64, n)
	totalEquity := 1.0
	peak := 1.0
	maxDD := 0.0
	scale := positionPct / 100.0

	var totalHolding float64
	holdingCount := 0

	for i, t := range trades {
		pnlList[i] = t.PnlPct
		if t.PnlPct > 0 {
			wins = append(wins, t.PnlPct)
		} else {
			losses = append(losses, t.PnlPct)
		}

		// Equity curve (position-scaled)
		totalEquity *= 1.0 + t.PnlPct*scale/100.0
		if totalEquity > peak {
			peak = totalEquity
		}
		dd := (peak - totalEquity) / peak * 100
		if dd > maxDD {
			maxDD = dd
		}

		if t.HoldingHours > 0 {
			totalHolding += t.HoldingHours
			holdingCount++
		}
	}

	s.WinningTrades = len(wins)
	s.LosingTrades = len(losses)
	s.WinRatePct = round(float64(len(wins))/float64(n)*100, 2)
	s.AvgPnlPct = round(mean(pnlList), 4)
	s.TotalPnlPct = round((totalEquity-1)*100, 4)
	s.MaxDrawdownPct = round(maxDD, 4)

	if len(wins) > 0 {
		s.AvgWinPnlPct = round(mean(wins), 4)
	}
	if len(losses) > 0 {
		s.AvgLossPnlPct = round(mean(losses), 4)
	}

	// Profit factor
	sumWins := sum(wins)
	sumLosses := abs(sum(losses))
	if sumLosses > 0 {
		s.ProfitFactor = round(sumWins/sumLosses, 4)
	} else {
		s.ProfitFactor = math.Inf(1)
	}

	// Period length in years
	periodYears := 1.0
	if t1, err := time.Parse("2006-01-02", start[:10]); err == nil {
		if t2, err := time.Parse("2006-01-02", end[:10]); err == nil {
			dy := t2.Sub(t1).Seconds() / (365.25 * 86400)
			if dy > 0 {
				periodYears = dy
			}
		}
	}

	// Sharpe ratio (annualized, position-scaled)
	scaledPnl := make([]float64, n)
	for i, v := range pnlList {
		scaledPnl[i] = v * scale
	}
	tradesPerYear := float64(n) / periodYears
	meanRet := stat.Mean(scaledPnl, nil)
	stdRet := stat.StdDev(scaledPnl, nil)
	if stdRet > 0 {
		s.SharpeRatio = round(meanRet/stdRet*math.Sqrt(tradesPerYear), 4)
	}

	// Calmar ratio
	if maxDD > 0 {
		s.CalmarRatio = round(abs(s.TotalPnlPct)/maxDD, 4)
	}

	// Average holding hours
	if holdingCount > 0 {
		s.AvgHoldingHours = round(totalHolding/float64(holdingCount), 2)
	}

	return s
}

// --------------------------------------------------------------------------
// Utility helpers (replacing numpy/scipy)
// --------------------------------------------------------------------------

func tsToISO(tsMs float64) string {
	return time.Unix(int64(tsMs)/1000, 0).UTC().Format(time.RFC3339)
}

// parseDate parses "YYYY-MM-DD" and returns the millisecond Unix timestamp
// at midnight UTC.
func parseDate(dateStr string) int64 {
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}

func holdingHours(entryISO, exitISO string) float64 {
	t1, err1 := time.Parse(time.RFC3339, entryISO)
	t2, err2 := time.Parse(time.RFC3339, exitISO)
	if err1 != nil || err2 != nil {
		return 0
	}
	return round(t2.Sub(t1).Hours(), 2)
}

func candlesToArrays(candles []types.Candle) *types.OHLCVArrays {
	n := len(candles)
	out := &types.OHLCVArrays{
		Timestamps: make([]float64, n),
		Opens:      make([]float64, n),
		Highs:      make([]float64, n),
		Lows:       make([]float64, n),
		Closes:     make([]float64, n),
		Volumes:    make([]float64, n),
	}
	for i, c := range candles {
		out.Timestamps[i] = float64(c.Timestamp * 1000) // Candle stores seconds → ms
		out.Opens[i] = c.Open
		out.Highs[i] = c.High
		out.Lows[i] = c.Low
		out.Closes[i] = c.Close
		out.Volumes[i] = c.Volume
	}
	return out
}

func mean(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	return stat.Mean(v, nil)
}

func sum(v []float64) float64 {
	var s float64
	for _, x := range v {
		s += x
	}
	return s
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func round(x float64, prec int) float64 {
	pow := math.Pow(10, float64(prec))
	return math.Round(x*pow) / pow
}
