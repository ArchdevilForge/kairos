package cycle

import "math"

func sma(v []float64, period int) float64 {
	if period <= 0 || len(v) < period {
		return 0
	}
	return mean(v[len(v)-period:])
}

func mean(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	var s float64
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

func stdPop(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	m := mean(v)
	var sumSq float64
	for _, x := range v {
		d := x - m
		sumSq += d * d
	}
	return math.Sqrt(sumSq / float64(len(v)))
}

// slope of y over x=0..n-1
func slope(v []float64) float64 {
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

func pctChange(v []float64, lookback int) float64 {
	if lookback <= 0 || len(v) <= lookback {
		return 0
	}
	old := v[len(v)-1-lookback]
	if old == 0 {
		return 0
	}
	return (v[len(v)-1] - old) / old * 100
}

func maxOf(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	m := v[0]
	for _, x := range v[1:] {
		if x > m {
			m = x
		}
	}
	return m
}

func minOf(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	m := v[0]
	for _, x := range v[1:] {
		if x < m {
			m = x
		}
	}
	return m
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

// mirrorCloses turns an up series into a down series around the first price.
// Used for long/short symmetry tests.
func mirrorCloses(closes []float64) []float64 {
	if len(closes) == 0 {
		return nil
	}
	out := make([]float64, len(closes))
	base := closes[0]
	for i, c := range closes {
		out[i] = base - (c - base)
	}
	return out
}

func mirrorHL(highs, lows []float64) (mirroredHighs, mirroredLows []float64) {
	// After price mirror, high/low swap relative to base path:
	// new_high[i] = base - (low[i]-base), new_low[i] = base - (high[i]-base)
	if len(highs) == 0 || len(lows) == 0 || len(highs) != len(lows) {
		return nil, nil
	}
	base := (highs[0] + lows[0]) / 2
	mh := make([]float64, len(highs))
	ml := make([]float64, len(lows))
	for i := range highs {
		mh[i] = base - (lows[i] - base)
		ml[i] = base - (highs[i] - base)
		if mh[i] < ml[i] {
			mh[i], ml[i] = ml[i], mh[i]
		}
	}
	return mh, ml
}
