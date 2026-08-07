package cycle

import "github.com/ArchdevilForge/kairos/internal/types"

// ClassifyOHLCV is a one-shot helper for CLI/debug (no hysteresis).
func ClassifyOHLCV(tf string, role types.TimeframeRole, closes, highs, lows, volumes []float64) types.CycleNode {
	return classifyRaw(Series{
		Timeframe: tf,
		Role:      role,
		Closes:    closes,
		Highs:     highs,
		Lows:      lows,
		Volumes:   volumes,
	})
}

// MapFromOHLCV builds a CycleMap without hysteresis (CLI/debug).
// Production enrichment must use Service.Map so transition policy sticks.
func MapFromOHLCV(symbol string, asOf int64, legacy types.MarketPhase, series []Series) types.CycleMap {
	return MapStateless(symbol, asOf, legacy, series)
}
