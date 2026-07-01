package utils

import (
	"math"
	"sync"
)

// RollingZScore computes a rolling-window z-score over recent values using a
// circular buffer.  Ported from src/kairos/utils/zscore.py.
type RollingZScore struct {
	buffer    []float64
	size      int
	count     int
	head      int // next write position
	minSamples int
	mu        sync.RWMutex
}

// NewRollingZScore creates a tracker with the given window size.
// minSamples defaults to 2 if window >= 2, otherwise window.
func NewRollingZScore(window int) *RollingZScore {
	if window < 1 {
		window = 1
	}
	minSamples := 2
	if window < minSamples {
		minSamples = window
	}
	return &RollingZScore{
		buffer:     make([]float64, window),
		size:       window,
		minSamples: minSamples,
	}
}

// Add inserts value into the rolling window and returns its z-score relative
// to the existing window (the new value is *not* included in its own
// z-score).  Returns 0.0 when fewer than minSamples have been collected.
func (z *RollingZScore) Add(value float64) float64 {
	z.mu.Lock()
	defer z.mu.Unlock()

	score := z.compute(value)

	// Store the new value in the circular buffer.
	z.buffer[z.head] = value
	z.head = (z.head + 1) % z.size
	if z.count < z.size {
		z.count++
	}

	return score
}

// compute returns the z-score of value against the current window.
func (z *RollingZScore) compute(value float64) float64 {
	if z.count < z.minSamples {
		return 0.0
	}
	n := z.count
	var sum float64
	for i := range n {
		sum += z.buffer[i]
	}
	mean := sum / float64(n)

	var varsum float64
	for i := range n {
		d := z.buffer[i] - mean
		varsum += d * d
	}
	variance := varsum / float64(n-1) // sample variance
	if variance == 0 {
		return 0.0
	}
	return (value - mean) / math.Sqrt(variance)
}

// Reset clears all stored values.
func (z *RollingZScore) Reset() {
	z.mu.Lock()
	defer z.mu.Unlock()
	z.count = 0
	z.head = 0
	// Zeroing the buffer isn't strictly needed but prevents stale data leaks.
	clear(z.buffer)
}
