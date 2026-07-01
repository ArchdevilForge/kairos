package scanner

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/ArchdevilForge/kairos/internal/indicators"
	"github.com/ArchdevilForge/kairos/internal/types"
)

// ---------------------------------------------------------------------------
// Signal envelope builder
// ---------------------------------------------------------------------------

func makeSignalEnvelope(success bool, data map[string]any, symbol string, score map[string]any, reasons, warnings, errors []string) *types.SignalEnvelope {
	if reasons == nil {
		reasons = []string{}
	}
	if warnings == nil {
		warnings = []string{}
	}
	if errors == nil {
		errors = []string{}
	}
	var symPtr *string
	if symbol != "" {
		symPtr = &symbol
	}
	return &types.SignalEnvelope{
		Success:       success,
		SchemaVersion: "1.0",
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		Symbol:        symPtr,
		Data:          data,
		Score:         score,
		Reasons:       reasons,
		Warnings:      warnings,
		Errors:        errors,
	}
}

// DetectSupportAtBoxLow checks 15m double-bottom / non-lower-low near box support.
func DetectSupportAtBoxLow(ohlcv *types.OHLCVArrays, boxLow float64) (triggered, near bool) {
	if ohlcv == nil || boxLow <= 0 || len(ohlcv.Lows) < 12 {
		return false, false
	}
	lastLow := ohlcv.Lows[len(ohlcv.Lows)-1]
	near = lastLow <= boxLow*1.01 && lastLow >= boxLow*0.985
	if !near {
		return false, false
	}
	triggered = detectDoubleBottom(ohlcv.Lows, ohlcv.Closes)
	return triggered, true
}

func detectDoubleBottom(lows, closes []float64) bool {
	if len(lows) < 12 || len(closes) < 12 {
		return false
	}
	n := len(lows)
	window := lows[n-12 : n]
	firstLow := minVal(window[:6])
	secondLow := minVal(window[6:])
	if secondLow < firstLow*0.995 {
		return false
	}
	return closes[n-1] > closes[n-2]
}

func cycleDetectorConfig(cfg types.ScoringConfig) indicators.CycleDetectorConfig {
	d := indicators.DefaultCycleDetectorConfig()
	y := cfg.CycleDetector
	if y.SpringBtcChangeMin != 0 {
		d.SpringBtcChangeMin = y.SpringBtcChangeMin
	}
	if y.SummerBtcChangeMin != 0 {
		d.SummerBtcChangeMin = y.SummerBtcChangeMin
	}
	if y.AutumnBtcChangeMax != 0 {
		d.AutumnBtcChangeMax = y.AutumnBtcChangeMax
	}
	if y.WinterBtcChangeMax != 0 {
		d.WinterBtcChangeMax = y.WinterBtcChangeMax
	}
	if y.HighVolatilityThreshold != 0 {
		d.HighVolatilityThreshold = y.HighVolatilityThreshold
	}
	if y.LowVolatilityThreshold != 0 {
		d.LowVolatilityThreshold = y.LowVolatilityThreshold
	}
	if y.HighFundingThreshold != 0 {
		d.HighFundingThreshold = y.HighFundingThreshold
	}
	if y.LowFundingThreshold != 0 {
		d.LowFundingThreshold = y.LowFundingThreshold
	}
	return d
}

// ---------------------------------------------------------------------------
// Trend / Volume helpers  (ported from _trend, _volume_confirmed)
// ---------------------------------------------------------------------------

// Trend determines the price trend of OHLCV data.
func Trend(ohlcv *types.OHLCVArrays) string {
	closes := ohlcv.Closes
	if len(closes) < 20 {
		return "sideways"
	}
	recentMean := meanVal(closes[len(closes)-20:])
	current := closes[len(closes)-1]
	switch {
	case current > recentMean*1.005:
		return "up"
	case current < recentMean*0.995:
		return "down"
	default:
		return "sideways"
	}
}

// VolumeConfirmed checks whether the most recent candle volume is ≥ 1.2× the
// prior 19-candle mean volume.
func VolumeConfirmed(ohlcv *types.OHLCVArrays) bool {
	volumes := ohlcv.Volumes
	if len(volumes) < 20 {
		return false
	}
	recent := volumes[len(volumes)-1]
	baseline := meanVal(volumes[len(volumes)-20 : len(volumes)-1])
	return baseline > 0 && recent >= baseline*1.2
}

// ---------------------------------------------------------------------------
// OHLCV conversion  (ported from _ohlcv_to_arrays)
// ---------------------------------------------------------------------------

// OHLCVToArrays converts a slice of Candle to an OHLCVArrays struct.
func OHLCVToArrays(candles []types.Candle) *types.OHLCVArrays {
	if len(candles) == 0 {
		return nil
	}
	n := len(candles)
	o := &types.OHLCVArrays{
		Timestamps: make([]float64, n),
		Opens:      make([]float64, n),
		Highs:      make([]float64, n),
		Lows:       make([]float64, n),
		Closes:     make([]float64, n),
		Volumes:    make([]float64, n),
	}
	for i, c := range candles {
		o.Timestamps[i] = float64(c.Timestamp)
		o.Opens[i] = c.Open
		o.Highs[i] = c.High
		o.Lows[i] = c.Low
		o.Closes[i] = c.Close
		o.Volumes[i] = c.Volume
	}
	return o
}

// ---------------------------------------------------------------------------
// MarketCycle → map  (ported from _cycle_to_dict)
// ---------------------------------------------------------------------------

// CycleToDict converts a MarketCycle struct to a map for JSON output.
func CycleToDict(cycle types.MarketCycle) map[string]any {
	return map[string]any{
		"phase":                string(cycle.Phase),
		"confidence":           cycle.Confidence,
		"btc_trend":            cycle.BtcTrend,
		"btc_change_30d":       cycle.BtcChange30D,
		"btc_change_7d":        cycle.BtcChange7D,
		"volatility":           cycle.Volatility,
		"volume_trend":         cycle.VolumeTrend,
		"altcoin_correlation":  cycle.AltcoinCorrelation,
		"funding_rates_avg":    cycle.FundingRatesAvg,
	}
}

// ---------------------------------------------------------------------------
// Dedup  (ported from _dedupe_strings)
// ---------------------------------------------------------------------------

// DedupeStrings returns a new slice preserving order with duplicates removed.
func DedupeStrings(v []string) []string {
	seen := make(map[string]struct{}, len(v))
	result := make([]string, 0, len(v))
	for _, s := range v {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		result = append(result, s)
	}
	return result
}

// ---------------------------------------------------------------------------
// Fingerprint  (ported from _fingerprint)
// ---------------------------------------------------------------------------

func fingerprint(symbol, direction, setupType string, structure map[string]any, risk types.RiskBounds) string {
	payload := fmt.Sprintf("%s|%s|%s|%s|%.8f|%.8f|", symbol, direction, setupType,
		getMapString(structure, "timeframe"),
		getMapFloat64(structure, "high"),
		getMapFloat64(structure, "low"))

	// Entry zone
	for i, v := range risk.EntryZone {
		if i > 0 {
			payload += ","
		}
		payload += fmt.Sprintf("%v", v)
	}
	payload += "|"
	if risk.StructuralStop != nil {
		payload += fmt.Sprintf("%v", *risk.StructuralStop)
	}
	payload += "|"
	for i, v := range risk.Targets {
		if i > 0 {
			payload += ","
		}
		payload += fmt.Sprintf("%v", v)
	}
	h := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(h[:12]) // 24 hex chars
}

// ---------------------------------------------------------------------------
// Numeric slice helpers (small — avoid dependency on external math libs)
// ---------------------------------------------------------------------------

func meanVal(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	var sum float64
	for _, x := range v {
		sum += x
	}
	return sum / float64(len(v))
}

func maxVal(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	m := v[0]
	for _, x := range v {
		if x > m {
			m = x
		}
	}
	return m
}

func minVal(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	m := v[0]
	for _, x := range v {
		if x < m {
			m = x
		}
	}
	return m
}

// ---------------------------------------------------------------------------
// Config getter helpers
// ---------------------------------------------------------------------------

func getWeight(m map[string]float64, key string, def float64) float64 {
	if m == nil {
		return def
	}
	if v, ok := m[key]; ok {
		return v
	}
	return def
}

func getMapString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getMapFloat64(m map[string]any, key string) float64 {
	if m == nil {
		return 0
	}
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case float64:
			return val
		case int:
			return float64(val)
		}
	}
	return 0
}

func getMapBool(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}
