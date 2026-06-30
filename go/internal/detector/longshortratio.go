package detector

import (
	"context"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
	"github.com/ArchdevilForge/kairos/internal/utils"
)

// LongShortRatioDetector monitors CoinGlass long/short ratio snapshots and
// emits anomalies on 3 trigger modes:
//  1. Absolute: long_rate ≥ absThreshold (80%) or ≤ (100 - absThreshold)
//  2. Velocity:  per-interval shift ≥ velocityThresholdPct (15%)
//  3. Z-score:   rolling deviation ≥ zscoreThreshold (2.5)
//
// Ported from src/kairos/detectors/long_short_ratio.py.
type LongShortRatioDetector struct {
	BaseDetector

	enabled              bool
	absThreshold         float64 // long_rate %, default 80
	zscoreThreshold      float64
	zscoreWindow         int
	velocityThresholdPct float64
	minNotifyInterval    time.Duration

	// symbol -> RollingZScore for long_rate %
	zscore map[string]*utils.RollingZScore
	zsMu   sync.RWMutex
	// symbol -> (timestamp, longRate, shortRate)
	last   map[string]lsSnapshot
	lastMu sync.RWMutex
}

type lsSnapshot struct {
	ts        float64
	longRate  float64
	shortRate float64
}

// NewLongShortRatioDetector creates a detector from config.
func NewLongShortRatioDetector(cfg types.LongShortRatioConfig) *LongShortRatioDetector {
	d := &LongShortRatioDetector{
		BaseDetector: BaseDetector{
			Logger:    slog.Default().With("detector", "long_short_ratio"),
			events:    make(chan types.AnomalyEvent, 64),
			Cooldowns: make(map[string]time.Time),
		},
		enabled:              cfg.Enabled,
		absThreshold:         cfg.AbsThreshold,
		zscoreThreshold:      cfg.ZscoreThreshold,
		zscoreWindow:         cfg.ZscoreWindow,
		velocityThresholdPct: cfg.VelocityThresholdPct,
		minNotifyInterval:    parseDuration(cfg.MinNotifyInterval),
		zscore:               make(map[string]*utils.RollingZScore),
		last:                 make(map[string]lsSnapshot),
	}
	if d.absThreshold <= 0 {
		d.absThreshold = 80.0
	}
	if d.zscoreThreshold <= 0 {
		d.zscoreThreshold = 2.5
	}
	if d.zscoreWindow <= 0 {
		d.zscoreWindow = 48
	}
	if d.velocityThresholdPct <= 0 {
		d.velocityThresholdPct = 15.0
	}
	if d.minNotifyInterval <= 0 {
		d.minNotifyInterval = 30 * time.Minute
	}
	return d
}

func (d *LongShortRatioDetector) Name() string { return "long_short_ratio" }

func (d *LongShortRatioDetector) OnTicker(_ context.Context, _ types.Ticker)                      {}
func (d *LongShortRatioDetector) OnMetricsUpdate(_ context.Context, _ string, _ float64, _ float64) {}
func (d *LongShortRatioDetector) OnLiquidationSnapshot(_ string, _, _, _ float64)                  {}

// OnLSSnapshot processes one long/short ratio snapshot.
func (d *LongShortRatioDetector) OnLSSnapshot(symbol string, longRate, shortRate float64) {
	if !d.enabled {
		return
	}
	if longRate < 0 || longRate > 100 {
		return
	}
	now := time.Now()
	ts := float64(now.UnixMilli()) / 1000

	// Ensure z-score tracker exists.
	d.zsMu.Lock()
	zt, ok := d.zscore[symbol]
	if !ok {
		zt = utils.NewRollingZScore(d.zscoreWindow)
		d.zscore[symbol] = zt
	}
	d.zsMu.Unlock()

	d.lastMu.RLock()
	prev, hasPrev := d.last[symbol]
	d.lastMu.RUnlock()

	// 1. Absolute threshold.
	absLow := 100.0 - d.absThreshold
	if longRate >= d.absThreshold || longRate <= absLow {
		label := "long_excessive"
		if longRate <= absLow {
			label = "short_excessive"
		}
		key := symbol + "__ls__" + label
		d.cdMu.Lock()
		if canNotify(d.Cooldowns, key, now, d.minNotifyInterval) {
			d.cdMu.Unlock()
			d.emitLS(symbol, ts, longRate, shortRate, label, longRate, 0, now)
		} else {
			d.cdMu.Unlock()
		}
		// Still record the value for z-score history (without scoring).
		zt.Add(longRate)
		d.lastMu.Lock()
		d.last[symbol] = lsSnapshot{ts: ts, longRate: longRate, shortRate: shortRate}
		d.lastMu.Unlock()
		return
	}

	// 2. Velocity check (rapid shift vs previous poll).
	if hasPrev && prev.longRate > 0 {
		shiftPct := math.Abs(longRate-prev.longRate) / prev.longRate * 100
		if shiftPct >= d.velocityThresholdPct {
			key := symbol + "__ls__ls_velocity"
			d.cdMu.Lock()
			if canNotify(d.Cooldowns, key, now, d.minNotifyInterval) {
				d.cdMu.Unlock()
				d.emitLS(symbol, ts, longRate, shortRate, "ls_velocity", shiftPct, 0, now)
			} else {
				d.cdMu.Unlock()
			}
		}
	}

	// 3. Z-score against own history.
	zs := zt.Add(longRate)
	if zs != 0 && d.zscoreThreshold > 0 && math.Abs(zs) >= d.zscoreThreshold {
		key := symbol + "__ls__ls_zscore"
		d.cdMu.Lock()
		if canNotify(d.Cooldowns, key, now, d.minNotifyInterval) {
			d.cdMu.Unlock()
			d.emitLS(symbol, ts, longRate, shortRate, "ls_zscore", longRate, zs, now)
		} else {
			d.cdMu.Unlock()
		}
	}

	d.lastMu.Lock()
	d.last[symbol] = lsSnapshot{ts: ts, longRate: longRate, shortRate: shortRate}
	d.lastMu.Unlock()
}

func (d *LongShortRatioDetector) Events() <-chan types.AnomalyEvent { return d.events }

func (d *LongShortRatioDetector) Reset() {
	d.zsMu.Lock()
	clear(d.zscore)
	d.zsMu.Unlock()
	d.lastMu.Lock()
	clear(d.last)
	d.lastMu.Unlock()
	d.cdMu.Lock()
	clear(d.Cooldowns)
	d.cdMu.Unlock()
}

// UpdateConfig hot-reloads configuration at runtime.
func (d *LongShortRatioDetector) UpdateConfig(cfg types.LongShortRatioConfig) {
	d.BaseDetector.mu.Lock()
	defer d.BaseDetector.mu.Unlock()
	d.enabled = cfg.Enabled
	if cfg.AbsThreshold > 0 {
		d.absThreshold = cfg.AbsThreshold
	}
	if cfg.ZscoreThreshold > 0 {
		d.zscoreThreshold = cfg.ZscoreThreshold
	}
	if cfg.ZscoreWindow > 0 {
		d.zscoreWindow = cfg.ZscoreWindow
	}
	if cfg.VelocityThresholdPct > 0 {
		d.velocityThresholdPct = cfg.VelocityThresholdPct
	}
	if cfg.MinNotifyInterval != "" {
		d.minNotifyInterval = parseDuration(cfg.MinNotifyInterval)
	}
}

func (d *LongShortRatioDetector) emitLS(symbol string, ts, longRate, shortRate float64,
	reason string, value, zs float64, nowTime time.Time) {

	absVal := math.Abs(value)
	var severity types.Severity
	if absVal >= 80.0 || (zs != 0 && math.Abs(zs) >= 4.0) {
		severity = types.SeverityHigh
	} else if absVal >= 60.0 || (zs != 0 && math.Abs(zs) >= 3.0) {
		severity = types.SeverityMedium
	} else {
		severity = types.SeverityLow
	}

	lsRatio := 0.0
	if shortRate > 0 {
		lsRatio = math.Round(longRate/shortRate*10000) / 10000
	}

	data := map[string]any{
		"long_rate":             math.Round(longRate*100) / 100,
		"short_rate":            math.Round(shortRate*100) / 100,
		"ls_ratio":              lsRatio,
		"reason":                reason,
		"trigger_value":         math.Round(value*10000) / 10000,
		"zscore":                math.Round(zs*10000) / 10000,
		"threshold_abs":         d.absThreshold,
		"threshold_zscore":      d.zscoreThreshold,
		"threshold_velocity_pct": d.velocityThresholdPct,
	}
	if zs == 0 {
		data["zscore"] = nil
	}

	evt := NewEvent(symbol, "long_short_ratio", string(severity), data)
	evt.Timestamp = ts
	select {
	case d.events <- evt:
	default:
		d.Logger.Warn("long_short_ratio event channel full, dropping event")
	}
}
