package data

import "math"

// RSIHotnessScore returns candidate attention points for extreme RSI (4h).
// Neutral zone (45–55) scores 0; oversold/overbought extremes approach maxWeight.
func RSIHotnessScore(rsi4h, maxWeight float64) float64 {
	if rsi4h <= 0 || maxWeight <= 0 {
		return 0
	}
	extreme := math.Max(rsi4h-55, 45-rsi4h)
	if extreme <= 0 {
		return 0
	}
	return math.Min(maxWeight, extreme/25*maxWeight)
}
