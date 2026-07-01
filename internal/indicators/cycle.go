package indicators

import (
	"math"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// CycleDetectorConfig holds configuration thresholds for market cycle detection.
type CycleDetectorConfig struct {
	SpringBtcChangeMin      float64
	SummerBtcChangeMin      float64
	AutumnBtcChangeMax      float64
	WinterBtcChangeMax      float64
	HighVolatilityThreshold float64
	LowVolatilityThreshold  float64
	HighFundingThreshold    float64
	LowFundingThreshold     float64
}

// DefaultCycleDetectorConfig returns default configuration matching the Python defaults.
func DefaultCycleDetectorConfig() CycleDetectorConfig {
	return CycleDetectorConfig{
		SpringBtcChangeMin:      10,
		SummerBtcChangeMin:      30,
		AutumnBtcChangeMax:      50,
		WinterBtcChangeMax:      -10,
		HighVolatilityThreshold: 5,
		LowVolatilityThreshold:  2,
		HighFundingThreshold:    0.05,
		LowFundingThreshold:     -0.01,
	}
}

// CycleDetector detects market cycle phase from BTC price/volume data.
// Ported from kairos.analysis.cycle.CycleDetector.
type CycleDetector struct {
	config CycleDetectorConfig
}

// NewCycleDetector creates a CycleDetector with the given config.
func NewCycleDetector(config CycleDetectorConfig) *CycleDetector {
	return &CycleDetector{config: config}
}

// DetectPhase detects the current market phase from BTC data.
func (d *CycleDetector) DetectPhase(
	btcPrices []float64,
	btcVolumes []float64,
	altcoinCorrelation float64,
	avgFundingRate float64,
	totalMarketCapChange30d float64,
) types.MarketCycle {
	if len(btcPrices) < 30 {
		return d.defaultCycle()
	}

	btcChange7d := d.calculateChange(btcPrices, 7)
	btcChange30d := d.calculateChange(btcPrices, 30)
	volatility := d.calculateVolatility(btcPrices)
	volumeTrend := d.calculateVolumeTrend(btcVolumes)
	btcTrend := d.determineTrend(btcPrices)

	phase, confidence := d.determinePhase(
		btcChange7d, btcChange30d,
		volatility, volumeTrend,
		altcoinCorrelation, avgFundingRate,
		totalMarketCapChange30d,
	)

	return types.MarketCycle{
		Phase:               phase,
		Confidence:          confidence,
		BtcTrend:            btcTrend,
		BtcChange30D:        btcChange30d,
		BtcChange7D:         btcChange7d,
		Volatility:          volatility,
		VolumeTrend:         volumeTrend,
		AltcoinCorrelation:  altcoinCorrelation,
		FundingRatesAvg:     avgFundingRate,
		MarketCapChange30D:  totalMarketCapChange30d,
	}
}

// calculateChange computes the percentage price change over the last N data points.
func (d *CycleDetector) calculateChange(prices []float64, days int) float64 {
	if len(prices) < days {
		return 0
	}
	oldPrice := prices[len(prices)-days]
	if oldPrice == 0 {
		return 0
	}
	return (prices[len(prices)-1] - oldPrice) / oldPrice * 100
}

// calculateVolatility computes annualized volatility from daily returns (ATR-like).
func (d *CycleDetector) calculateVolatility(prices []float64, period ...int) float64 {
	p := 14
	if len(period) > 0 {
		p = period[0]
	}
	if len(prices) < p+1 {
		return 0
	}

	// Daily returns: (prices[i] - prices[i-1]) / prices[i-1]
	returns := make([]float64, len(prices)-1)
	for i := 1; i < len(prices); i++ {
		if prices[i-1] != 0 {
			returns[i-1] = (prices[i] - prices[i-1]) / prices[i-1]
		}
	}

	// Use only the last p returns
	recentReturns := returns[len(returns)-p:]
	return stdDevPop(recentReturns) * 100 * math.Sqrt(365)
}

// calculateVolumeTrend determines whether volume is increasing, decreasing, or stable.
func (d *CycleDetector) calculateVolumeTrend(volumes []float64, period ...int) string {
	p := 7
	if len(period) > 0 {
		p = period[0]
	}
	if len(volumes) < p*2 {
		return "stable"
	}

	recentAvg := meanVal(volumes[len(volumes)-p:])
	priorAvg := meanVal(volumes[len(volumes)-p*2 : len(volumes)-p])

	switch {
	case recentAvg > priorAvg*1.2:
		return "increasing"
	case recentAvg < priorAvg*0.8:
		return "decreasing"
	default:
		return "stable"
	}
}

// determineTrend determines price trend (up/down/sideways) via linear regression slope.
func (d *CycleDetector) determineTrend(prices []float64, period ...int) string {
	p := 20
	if len(period) > 0 {
		p = period[0]
	}
	if len(prices) < p {
		return "sideways"
	}

	recent := prices[len(prices)-p:]
	slope := linearSlope(recent)

	switch {
	case slope > recent[len(recent)-1]*0.001:
		return "up"
	case slope < -recent[len(recent)-1]*0.001:
		return "down"
	default:
		return "sideways"
	}
}

// determinePhase scores each phase and returns the best match with confidence.
func (d *CycleDetector) determinePhase(
	btc7d, btc30d, volatility float64,
	volumeTrend string,
	altcoinCorr, fundingRate, mcapChange30d float64,
) (types.MarketPhase, float64) {
	scores := map[types.MarketPhase]int{
		types.MarketPhaseSpring: 0,
		types.MarketPhaseSummer: 0,
		types.MarketPhaseAutumn: 0,
		types.MarketPhaseWinter: 0,
	}

	// BTC 30-day change signals
	switch {
	case btc30d > d.config.SummerBtcChangeMin:
		scores[types.MarketPhaseSummer] += 2
		scores[types.MarketPhaseAutumn] += 1
	case btc30d > d.config.SpringBtcChangeMin:
		scores[types.MarketPhaseSpring] += 2
		scores[types.MarketPhaseSummer] += 1
	case btc30d < d.config.WinterBtcChangeMax:
		scores[types.MarketPhaseWinter] += 2
	default:
		scores[types.MarketPhaseAutumn] += 1
		scores[types.MarketPhaseWinter] += 1
	}

	// Recent momentum (7d)
	switch {
	case btc7d > 10:
		scores[types.MarketPhaseSummer] += 1
	case btc7d > 5:
		scores[types.MarketPhaseSpring] += 1
	case btc7d < -10:
		scores[types.MarketPhaseWinter] += 2
	}

	// Volatility signals
	switch {
	case volatility > d.config.HighVolatilityThreshold:
		scores[types.MarketPhaseSummer] += 1
		scores[types.MarketPhaseWinter] += 1
	case volatility < d.config.LowVolatilityThreshold:
		scores[types.MarketPhaseAutumn] += 1
	}

	// Volume trend
	switch volumeTrend {
	case "increasing":
		scores[types.MarketPhaseSpring] += 1
		scores[types.MarketPhaseSummer] += 1
	case "decreasing":
		scores[types.MarketPhaseAutumn] += 1
		scores[types.MarketPhaseWinter] += 1
	}

	// Funding rates (high positive = overheated)
	switch {
	case fundingRate > d.config.HighFundingThreshold:
		scores[types.MarketPhaseSummer] += 1
		scores[types.MarketPhaseAutumn] += 1
	case fundingRate < d.config.LowFundingThreshold:
		scores[types.MarketPhaseWinter] += 1
	}

	// Altcoin correlation (high = still in trend, low = rotation/补涨)
	switch {
	case altcoinCorr > 0.8:
		scores[types.MarketPhaseSpring] += 1
		scores[types.MarketPhaseSummer] += 1
	case altcoinCorr < 0.5:
		scores[types.MarketPhaseAutumn] += 1
	}

	// Find phase with highest score
	best := types.MarketPhaseWinter
	bestScore := -1
	total := 0
	for phase, score := range scores {
		total += score
		if score > bestScore {
			bestScore = score
			best = phase
		}
	}

	confidence := 0.0
	if total > 0 {
		confidence = math.Round(float64(bestScore)/float64(total)*100) / 100
	}

	return best, confidence
}

// defaultCycle returns a default winter cycle when insufficient data.
func (d *CycleDetector) defaultCycle() types.MarketCycle {
	return types.MarketCycle{
		Phase:      types.MarketPhaseWinter,
		Confidence: 0.5,
		BtcTrend:   "sideways",
	}
}

// --- helpers (replacing numpy operations) ---

// stdDevPop computes population standard deviation (matching numpy.std default).
func stdDevPop(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	m := meanVal(v)
	var sumSq float64
	for _, x := range v {
		d := x - m
		sumSq += d * d
	}
	return math.Sqrt(sumSq / float64(len(v)))
}

// linearSlope computes the slope of a simple linear regression y = a + b*x,
// where x = [0, 1, 2, ..., len(v)-1] and y = v.
// Replaces np.polyfit(range(n), v, 1)[0].
func linearSlope(v []float64) float64 {
	n := float64(len(v))
	if n < 2 {
		return 0
	}
	var sumX, sumY, sumXY, sumX2 float64
	for i, y := range v {
		x := float64(i)
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}
	denom := n*sumX2 - sumX*sumX
	if denom == 0 {
		return 0
	}
	return (n*sumXY - sumX*sumY) / denom
}
