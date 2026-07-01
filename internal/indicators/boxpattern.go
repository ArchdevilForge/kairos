package indicators

import (
	"math"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// BoxDetectorConfig holds configuration for BoxDetector.
type BoxDetectorConfig struct {
	MinBars              int
	MaxBars              int
	TouchThresholdPct    float64
	ConvergenceThreshold float64
	MinVolumeDeclinePct  float64
}

// DefaultBoxDetectorConfig returns default configuration matching the Python defaults.
func DefaultBoxDetectorConfig() BoxDetectorConfig {
	return BoxDetectorConfig{
		MinBars:              10,
		MaxBars:              100,
		TouchThresholdPct:    0.3,
		ConvergenceThreshold: 0.7,
		MinVolumeDeclinePct:  0.3,
	}
}

// BoxDetector detects box patterns in OHLCV data.
// Ported from kairos.analysis.box_pattern.BoxDetector.
type BoxDetector struct {
	config BoxDetectorConfig
}

// NewBoxDetector creates a BoxDetector with the given config.
func NewBoxDetector(config BoxDetectorConfig) *BoxDetector {
	return &BoxDetector{config: config}
}

// Detect finds box patterns in OHLCV slices using a sliding window.
// Returns a slice of box patterns, or nil if none found.
func (d *BoxDetector) Detect(
	symbol, timeframe string,
	highs, lows, closes, volumes, timestamps []float64,
) []types.BoxPattern {
	if len(highs) < d.config.MinBars {
		return nil
	}

	var boxes []types.BoxPattern
	i := 0
	for i < len(highs)-d.config.MinBars {
		box := d.tryDetectBox(symbol, timeframe,
			highs[i:], lows[i:], closes[i:], volumes[i:], timestamps[i:])

		if box != nil && box.Status != types.BoxStatusInvalid {
			boxes = append(boxes, *box)
			// Skip ahead past this box
			boxBars := d.config.MinBars
			if len(timestamps) > 1 {
				barDuration := timestamps[1] - timestamps[0]
				if barDuration > 0 {
					bars := int((box.EndTime - box.StartTime) / barDuration)
					if bars > boxBars {
						boxBars = bars
					}
				}
			}
			i += boxBars
		} else {
			i++
		}
	}

	if len(boxes) == 0 {
		return nil
	}
	return boxes
}

// tryDetectBox attempts to detect a single box pattern starting from the beginning of the data.
func (d *BoxDetector) tryDetectBox(
	symbol, timeframe string,
	highs, lows, closes, volumes, timestamps []float64,
) *types.BoxPattern {
	if len(highs) < d.config.MinBars {
		return nil
	}

	// Find initial high and low from the lookback window
	initialHigh := maxVal(highs[:d.config.MinBars])
	initialLow := minVal(lows[:d.config.MinBars])

	// Box height must be reasonable (not too tight, not too wide)
	heightPct := (initialHigh - initialLow) / initialLow * 100
	if initialLow <= 0 {
		heightPct = 0
	}
	if heightPct < 1 || heightPct > 15 {
		return nil
	}

	// Extend box while price stays within bounds
	boxHigh := initialHigh
	boxLow := initialLow
	touchHigh := 0
	touchLow := 0
	boxEnd := d.config.MinBars

	limit := len(highs)
	if limit > d.config.MaxBars {
		limit = d.config.MaxBars
	}

	for i := d.config.MinBars; i < limit; i++ {
		// Break if price breaks above or below box with threshold tolerance
		if highs[i] > boxHigh*(1+d.config.TouchThresholdPct/100) {
			break
		}
		if lows[i] < boxLow*(1-d.config.TouchThresholdPct/100) {
			break
		}

		// Count touches at high boundary
		if boxHigh > 0 && math.Abs(highs[i]-boxHigh)/boxHigh*100 < d.config.TouchThresholdPct {
			touchHigh++
		}
		// Count touches at low boundary
		if boxLow > 0 && math.Abs(lows[i]-boxLow)/boxLow*100 < d.config.TouchThresholdPct {
			touchLow++
		}

		boxEnd = i
	}

	// Check if we have enough bars
	if boxEnd < d.config.MinBars {
		return nil
	}

	// Check for second tests (二次探顶/底)
	secondTestHigh := touchHigh >= 2
	secondTestLow := touchLow >= 2

	// Calculate convergence — range getting tighter
	startRecent := boxEnd - 5
	if startRecent < 0 {
		startRecent = 0
	}
	recentRange := maxVal(highs[startRecent:boxEnd]) - minVal(lows[startRecent:boxEnd])
	initialRange := boxHigh - boxLow
	convergence := 0.0
	if initialRange > 0 {
		convergence = 1 - (recentRange / initialRange)
	}

	// Check volume decline: recent volume vs early volume
	earlyVol := meanVal(volumes[:d.config.MinBars])
	recentVol := meanVal(volumes[startRecent:boxEnd])
	volumeDeclining := earlyVol > 0 && recentVol < earlyVol*(1-d.config.MinVolumeDeclinePct)

	// Determine status
	status := types.BoxStatusForming
	if convergence > d.config.ConvergenceThreshold && (secondTestHigh || secondTestLow) {
		status = types.BoxStatusConverging
	}

	return &types.BoxPattern{
		Symbol:          symbol,
		Timeframe:       timeframe,
		High:            boxHigh,
		Low:             boxLow,
		StartTime:       timestamps[0],
		EndTime:         timestamps[boxEnd],
		Status:          status,
		TouchHigh:       touchHigh,
		TouchLow:        touchLow,
		SecondTestHigh:  secondTestHigh,
		SecondTestLow:   secondTestLow,
		ConvergencePct:  convergence,
		VolumeDeclining: volumeDeclining,
	}
}

// CheckBreakout checks if a box has broken out given the current price and volume.
// Returns the (possibly modified) box. Volume must be at least 1.5x average to confirm.
func (d *BoxDetector) CheckBreakout(box *types.BoxPattern, currentPrice, currentVolume, avgVolume float64) *types.BoxPattern {
	if box.Status == types.BoxStatusBreakoutUp || box.Status == types.BoxStatusBreakoutDown {
		return box
	}

	// Upward breakout: 0.5% above box high with volume confirmation
	if currentPrice > box.High*1.005 && currentVolume > avgVolume*1.5 {
		box.Status = types.BoxStatusBreakoutUp
		box.BreakoutPrice = &currentPrice
		inf := math.Inf(1)
		box.BreakoutTime = &inf
		return box
	}

	// Downward breakout: 0.5% below box low with volume confirmation
	if currentPrice < box.Low*0.995 && currentVolume > avgVolume*1.5 {
		box.Status = types.BoxStatusBreakoutDown
		box.BreakoutPrice = &currentPrice
		inf := math.Inf(1)
		box.BreakoutTime = &inf
		return box
	}

	return box
}

// --- helpers (replacing numpy vector operations) ---

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
