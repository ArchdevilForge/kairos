package detector

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// VolumeSpikeDetector detects abnormal volume surges by comparing recent
// per-minute deltas against a rolling baseline.
// Ported from src/kairos/detectors/volume_spike.py.
type VolumeSpikeDetector struct {
	BaseDetector

	enabled           bool
	multiplier        float64
	windowMinutes     int
	minHistorySeconds float64
	minNotifyInterval time.Duration

	// symbol -> ring of (timestamp_s, cumulative_volume)
	volumeHistory map[string][]volPoint
	volMu         sync.RWMutex
	// symbol -> last price
	lastPrice map[string]float64
	priceMu   sync.RWMutex
	// symbol -> last notify time
	lastNotify map[string]time.Time
	notifyMu   sync.Mutex
}

type volPoint struct {
	ts  float64
	vol float64
}

// NewVolumeSpikeDetector creates a detector from config.
func NewVolumeSpikeDetector(cfg types.VolumeSpikeConfig) *VolumeSpikeDetector {
	d := &VolumeSpikeDetector{
		BaseDetector: BaseDetector{
			Logger: slog.Default().With("detector", "volume_spike"),
			events: make(chan types.AnomalyEvent, 64),
		},
		enabled:           cfg.Enabled,
		multiplier:        cfg.Multiplier,
		windowMinutes:     cfg.WindowMinutes,
		minNotifyInterval: parseDuration(cfg.MinNotifyInterval),
		volumeHistory:     make(map[string][]volPoint),
		lastPrice:         make(map[string]float64),
		lastNotify:        make(map[string]time.Time),
	}
	if d.multiplier <= 0 {
		d.multiplier = 3.0
	}
	if d.windowMinutes <= 0 {
		d.windowMinutes = 10
	}
	if d.minNotifyInterval <= 0 {
		d.minNotifyInterval = 2 * time.Minute
	}
	d.minHistorySeconds = float64(d.windowMinutes * 60)
	if cfg.MinHistorySeconds > 0 {
		d.minHistorySeconds = float64(cfg.MinHistorySeconds)
	}
	return d
}

func (d *VolumeSpikeDetector) Name() string { return "volume_spike" }

// OnTicker processes a ticker update — records both the cumulative volume
// and the latest price.
func (d *VolumeSpikeDetector) OnTicker(_ context.Context, ticker types.Ticker) {
	now := float64(time.Now().UnixMilli()) / 1000

	if ticker.QuoteVolume != nil {
		d.volMu.Lock()
		points := d.volumeHistory[ticker.Symbol]
		if points == nil {
			points = make([]volPoint, 0, 600)
		}
		// Keep at most 600 samples (same as Python _MAX_SAMPLES).
		if len(points) >= 600 {
			points = points[1:]
		}
		points = append(points, volPoint{ts: now, vol: *ticker.QuoteVolume})
		d.volumeHistory[ticker.Symbol] = points
		d.volMu.Unlock()
	}

	if ticker.LastPrice != nil {
		d.priceMu.Lock()
		d.lastPrice[ticker.Symbol] = *ticker.LastPrice
		d.priceMu.Unlock()
	}

	d.checkSpike(ticker.Symbol, now)
}

func (d *VolumeSpikeDetector) OnMetricsUpdate(_ context.Context, _ string, _ float64, _ float64) {}
func (d *VolumeSpikeDetector) OnLSSnapshot(_ string, _, _ float64)                              {}
func (d *VolumeSpikeDetector) OnLiquidationSnapshot(_ string, _, _, _ float64)                    {}

func (d *VolumeSpikeDetector) Events() <-chan types.AnomalyEvent { return d.events }
func (d *VolumeSpikeDetector) Reset() {
	d.volMu.Lock()
	clear(d.volumeHistory)
	d.volMu.Unlock()
	d.priceMu.Lock()
	clear(d.lastPrice)
	d.priceMu.Unlock()
	d.notifyMu.Lock()
	clear(d.lastNotify)
	d.notifyMu.Unlock()
}

// UpdateConfig hot-reloads configuration at runtime.
func (d *VolumeSpikeDetector) UpdateConfig(cfg types.VolumeSpikeConfig) {
	d.BaseDetector.mu.Lock()
	defer d.BaseDetector.mu.Unlock()
	d.enabled = cfg.Enabled
	if cfg.Multiplier > 0 {
		d.multiplier = cfg.Multiplier
	}
	if cfg.WindowMinutes > 0 {
		d.windowMinutes = cfg.WindowMinutes
		d.minHistorySeconds = float64(cfg.WindowMinutes * 60)
	}
	if cfg.MinHistorySeconds > 0 {
		d.minHistorySeconds = float64(cfg.MinHistorySeconds)
	}
	if cfg.MinNotifyInterval != "" {
		d.minNotifyInterval = parseDuration(cfg.MinNotifyInterval)
	}
}

func (d *VolumeSpikeDetector) checkSpike(symbol string, now float64) {
	d.volMu.RLock()
	points := d.volumeHistory[symbol]
	d.volMu.RUnlock()

	if len(points) < 10 {
		return
	}
	if points[len(points)-1].ts-points[0].ts < d.minHistorySeconds {
		return
	}

	windowStart := now - float64(d.windowMinutes)*60
	recentCutoff := now - 60 // last 1 minute

	var recentDeltas, windowDeltas []float64
	for i := 1; i < len(points); i++ {
		tPrev, vPrev := points[i-1].ts, points[i-1].vol
		tCurr, vCurr := points[i].ts, points[i].vol

		delta := vCurr - vPrev
		if delta < 0 {
			continue // volume counter reset (new 24h window)
		}
		dt := tCurr - tPrev
		if dt <= 0 {
			continue
		}

		if tCurr >= recentCutoff {
			recentDeltas = append(recentDeltas, delta)
		} else if tCurr >= windowStart {
			windowDeltas = append(windowDeltas, delta)
		}
	}

	if len(recentDeltas) == 0 || len(windowDeltas) == 0 {
		return
	}

	// Compare average per-tick delta, not raw sum (same as Python).
	var recentSum, windowSum float64
	for _, v := range recentDeltas {
		recentSum += v
	}
	for _, v := range windowDeltas {
		windowSum += v
	}
	recentAvg := recentSum / float64(len(recentDeltas))
	windowAvg := windowSum / float64(len(windowDeltas))

	if windowAvg <= 0 {
		return
	}

	ratio := recentAvg / windowAvg

	nowTime := time.Now()

	d.notifyMu.Lock()
	last := d.lastNotify[symbol]
	if ratio < d.multiplier || nowTime.Sub(last) < d.minNotifyInterval {
		d.notifyMu.Unlock()
		return
	}
	d.lastNotify[symbol] = nowTime
	d.notifyMu.Unlock()

	var severity types.Severity
	switch {
	case ratio >= d.multiplier*2:
		severity = types.SeverityHigh
	case ratio >= d.multiplier*1.5:
		severity = types.SeverityMedium
	default:
		severity = types.SeverityLow
	}

	d.priceMu.RLock()
	lastPx := d.lastPrice[symbol]
	d.priceMu.RUnlock()

	evt := NewEvent(symbol, "volume_spike", string(severity), map[string]any{
		"price":          round(lastPx, 8),
		"ratio":          round(ratio, 2),
		"recent_avg":     round(recentAvg, 2),
		"window_avg":     round(windowAvg, 2),
		"window_minutes": d.windowMinutes,
	})
	evt.Timestamp = now

	select {
	case d.events <- evt:
	default:
		d.Logger.Warn("volume_spike event channel full, dropping event")
	}
}

// parseDuration converts a duration string ("30s", "2m", "1h") or plain
// number (seconds) to time.Duration.  Ported from Python _parse_seconds.
func parseDuration(value string) time.Duration {
	if value == "" {
		return 0
	}
	// Try parsing as Go duration first for standard formats.
	if d, err := time.ParseDuration(value); err == nil {
		return d
	}
	// Plain number string → seconds.
	var f float64
	if _, err := fmt.Sscanf(value, "%f", &f); err == nil {
		return time.Duration(f * float64(time.Second))
	}
	return 0
}
