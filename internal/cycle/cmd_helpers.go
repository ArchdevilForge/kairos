package cycle

import "github.com/ArchdevilForge/kairos/internal/types"

// MapFromOHLCV builds a CycleMap without hysteresis (CLI/debug).
// Production enrichment must use Service.Map so transition policy sticks.
func MapFromOHLCV(symbol string, asOf int64, legacy types.MarketPhase, series []Series) types.CycleMap {
	return MapStateless(symbol, asOf, legacy, series)
}
