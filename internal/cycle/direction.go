package cycle

import (
	"math"

	"github.com/ArchdevilForge/kairos/internal/types"
)

const (
	midPeriod  = 20
	fastPeriod = 10
	structWin  = 10
	rangeLook  = 40
	minBarsDir = 40
	slopeLook  = 5
)

// detectDirection classifies up/down/neutral from closed bars only.
func detectDirection(closes, highs, lows []float64) (types.CycleDirection, float64, []types.Evidence) {
	var ev []types.Evidence
	if len(closes) < minBarsDir || len(highs) < minBarsDir || len(lows) < minBarsDir {
		return types.CycleDirectionNeutral, 0.2, []types.Evidence{{
			Code:        "insufficient_bars",
			Description: "need >=40 closed bars for direction",
			Value:       float64(len(closes)),
		}}
	}

	price := closes[len(closes)-1]
	mid := sma(closes, midPeriod)
	fast := sma(closes, fastPeriod)
	fastSeries := make([]float64, 0, slopeLook)
	// rebuild last slopeLook fast MAs
	for i := slopeLook; i >= 1; i-- {
		end := len(closes) - i + 1
		if end < fastPeriod {
			continue
		}
		fastSeries = append(fastSeries, sma(closes[:end], fastPeriod))
	}
	fastSlope := slope(fastSeries)

	up, down := 0, 0

	// 1) price vs mid MA
	if mid > 0 {
		rel := (price - mid) / mid * 100
		if rel > 0.15 {
			up++
			ev = append(ev, types.Evidence{Code: "price_above_mid_ma", Description: "price above mid MA", Value: rel})
		} else if rel < -0.15 {
			down++
			ev = append(ev, types.Evidence{Code: "price_below_mid_ma", Description: "price below mid MA", Value: rel})
		}
	}

	// 2) fast/slow relationship + slope
	if fast > mid && fastSlope > 0 {
		up++
		ev = append(ev, types.Evidence{Code: "fast_ma_rising", Description: "fast MA above mid and rising", Value: fastSlope})
	} else if fast < mid && fastSlope < 0 {
		down++
		ev = append(ev, types.Evidence{Code: "fast_ma_falling", Description: "fast MA below mid and falling", Value: fastSlope})
	}

	// 3) swing structure over two windows
	hRecent := maxOf(highs[len(highs)-structWin:])
	hPrior := maxOf(highs[len(highs)-2*structWin : len(highs)-structWin])
	lRecent := minOf(lows[len(lows)-structWin:])
	lPrior := minOf(lows[len(lows)-2*structWin : len(lows)-structWin])
	if hRecent > hPrior && lRecent > lPrior {
		up++
		ev = append(ev, types.Evidence{Code: "higher_high_higher_low", Description: "HH+HL structure"})
	} else if hRecent < hPrior && lRecent < lPrior {
		down++
		ev = append(ev, types.Evidence{Code: "lower_high_lower_low", Description: "LH+LL structure"})
	}

	// 4) range break vs prior rangeLook bars (excluding last bar)
	if len(closes) >= rangeLook+1 {
		window := closes[len(closes)-1-rangeLook : len(closes)-1]
		rHigh, rLow := maxOf(window), minOf(window)
		if price > rHigh {
			up++
			ev = append(ev, types.Evidence{Code: "range_breakout", Description: "break above recent range", Value: price - rHigh})
		} else if price < rLow {
			down++
			ev = append(ev, types.Evidence{Code: "range_breakdown", Description: "break below recent range", Value: rLow - price})
		}
	}

	// 5) pullback continuation: higher low after dip while above mid (up), mirror down
	if len(closes) >= 15 && mid > 0 {
		recentLow := minOf(lows[len(lows)-8:])
		priorLow := minOf(lows[len(lows)-15 : len(lows)-8])
		recentHigh := maxOf(highs[len(highs)-8:])
		priorHigh := maxOf(highs[len(highs)-15 : len(highs)-8])
		if price > mid && recentLow > priorLow {
			up++
			ev = append(ev, types.Evidence{Code: "higher_low", Description: "higher low hold above mid MA"})
		}
		if price < mid && recentHigh < priorHigh {
			down++
			ev = append(ev, types.Evidence{Code: "lower_high", Description: "lower high hold below mid MA"})
		}
	}

	conf := clamp01(math.Abs(float64(up-down)) / 5.0)
	switch {
	case up >= down+2:
		return types.CycleDirectionUp, math.Max(conf, 0.4), ev
	case down >= up+2:
		return types.CycleDirectionDown, math.Max(conf, 0.4), ev
	default:
		ev = append(ev, types.Evidence{Code: "mixed_votes", Description: "direction votes not decisive", Value: float64(up - down)})
		return types.CycleDirectionNeutral, math.Max(0.3, 1-conf), ev
	}
}
