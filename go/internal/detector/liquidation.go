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

// LiquidationDetector monitors CoinGlass liquidation data and emits anomalies
// on 3 trigger modes:
//  1. Absolute: total liquidation ≥ absThresholdUsd ($1M default)
//  2. Z-score:  rolling z-score ≥ zscoreThreshold (2.5 default)
//  3. Imbalance: one side (long or short) ≥ imbalanceThreshold (80% default)
//
// Ported from src/kairos/detectors/liquidation.py.
type LiquidationDetector struct {
	BaseDetector

	enabled             bool
	absThresholdUsd     float64
	zscoreThreshold     float64
	zscoreWindow        int
	imbalanceThreshold  float64
	minNotifyInterval   time.Duration

	// symbol -> RollingZScore for liquidation USD totals
	zscore map[string]*utils.RollingZScore
	zsMu   sync.RWMutex
	// symbol -> (timestamp, total, long, short)
	last map[string]liqSnapshot
	lastMu sync.RWMutex
}

type liqSnapshot struct {
	ts        float64
	totalUSD  float64
	longUSD   float64
	shortUSD  float64
}

// NewLiquidationDetector creates a detector from config.
func NewLiquidationDetector(cfg types.LiquidationConfig) *LiquidationDetector {
	d := &LiquidationDetector{
		BaseDetector: BaseDetector{
			Logger:    slog.Default().With("detector", "liquidation"),
			events:    make(chan types.AnomalyEvent, 64),
			Cooldowns: make(map[string]time.Time),
		},
		enabled:            cfg.Enabled,
		absThresholdUsd:    cfg.AbsThresholdUsd,
		zscoreThreshold:    cfg.ZscoreThreshold,
		zscoreWindow:       cfg.ZscoreWindow,
		imbalanceThreshold: cfg.ImbalanceThreshold,
		minNotifyInterval:  parseDuration(cfg.MinNotifyInterval),
		zscore:             make(map[string]*utils.RollingZScore),
		last:               make(map[string]liqSnapshot),
	}
	if d.absThresholdUsd <= 0 {
		d.absThresholdUsd = 1_000_000
	}
	if d.zscoreThreshold <= 0 {
		d.zscoreThreshold = 2.5
	}
	if d.zscoreWindow <= 0 {
		d.zscoreWindow = 48
	}
	if d.imbalanceThreshold <= 0 {
		d.imbalanceThreshold = 0.80
	}
	if d.minNotifyInterval <= 0 {
		d.minNotifyInterval = 30 * time.Minute
	}
	return d
}

func (d *LiquidationDetector) Name() string { return "liquidation" }

func (d *LiquidationDetector) OnTicker(_ context.Context, _ types.Ticker)        {}
func (d *LiquidationDetector) OnMetricsUpdate(_ context.Context, _ string, _ float64, _ float64) {}

func (d *LiquidationDetector) OnLSSnapshot(_ string, _, _ float64) {}

// OnLiquidationSnapshot processes one liquidation data snapshot.
func (d *LiquidationDetector) OnLiquidationSnapshot(symbol string, totalUSD, longUSD, shortUSD float64) {
	if !d.enabled || totalUSD <= 0 {
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
	if totalUSD >= d.absThresholdUsd {
		key := symbol + "__liq__absolute"
		d.cdMu.Lock()
		if canNotify(d.Cooldowns, key, now, d.minNotifyInterval) {
			d.cdMu.Unlock()
			d.emitLiquidation(symbol, ts, totalUSD, longUSD, shortUSD, "liq_absolute", totalUSD, 0, now)
		} else {
			d.cdMu.Unlock()
		}
	}

	// 2. Z-score spike.
	zs := zt.Add(totalUSD)
	if zs != 0 && d.zscoreThreshold > 0 && math.Abs(zs) >= d.zscoreThreshold {
		key := symbol + "__liq__zscore"
		d.cdMu.Lock()
		if canNotify(d.Cooldowns, key, now, d.minNotifyInterval) {
			d.cdMu.Unlock()
			totalPrev := 0.0
			if hasPrev {
				totalPrev = prev.totalUSD
			}
			d.emitLiquidation(symbol, ts, totalUSD, longUSD, shortUSD, "liq_zscore", totalPrev, zs, now)
		} else {
			d.cdMu.Unlock()
		}
	}

	// 3. Imbalance (one side dominating).
	if totalUSD > 0 {
		longRatio := longUSD / totalUSD
		shortRatio := shortUSD / totalUSD
		if longRatio >= d.imbalanceThreshold {
			key := symbol + "__liq__imbalance"
			d.cdMu.Lock()
			if canNotify(d.Cooldowns, key, now, d.minNotifyInterval) {
				d.cdMu.Unlock()
				d.emitLiquidation(symbol, ts, totalUSD, longUSD, shortUSD, "liq_long_dominated", longRatio, 0, now)
			} else {
				d.cdMu.Unlock()
			}
		} else if shortRatio >= d.imbalanceThreshold {
			key := symbol + "__liq__imbalance"
			d.cdMu.Lock()
			if canNotify(d.Cooldowns, key, now, d.minNotifyInterval) {
				d.cdMu.Unlock()
				d.emitLiquidation(symbol, ts, totalUSD, longUSD, shortUSD, "liq_short_dominated", shortRatio, 0, now)
			} else {
				d.cdMu.Unlock()
			}
		}
	}

	// Record snapshot.
	d.lastMu.Lock()
	d.last[symbol] = liqSnapshot{ts: ts, totalUSD: totalUSD, longUSD: longUSD, shortUSD: shortUSD}
	d.lastMu.Unlock()
}

// Implements the Detector interface — Go type-check trick.
var _ Detector = (*LiquidationDetector)(nil)

func (d *LiquidationDetector) Events() <-chan types.AnomalyEvent { return d.events }

func (d *LiquidationDetector) Reset() {
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
func (d *LiquidationDetector) UpdateConfig(cfg types.LiquidationConfig) {
	d.BaseDetector.mu.Lock()
	defer d.BaseDetector.mu.Unlock()
	d.enabled = cfg.Enabled
	if cfg.AbsThresholdUsd > 0 {
		d.absThresholdUsd = cfg.AbsThresholdUsd
	}
	if cfg.ZscoreThreshold > 0 {
		d.zscoreThreshold = cfg.ZscoreThreshold
	}
	if cfg.ZscoreWindow > 0 {
		d.zscoreWindow = cfg.ZscoreWindow
	}
	if cfg.ImbalanceThreshold > 0 {
		d.imbalanceThreshold = cfg.ImbalanceThreshold
	}
	if cfg.MinNotifyInterval != "" {
		d.minNotifyInterval = parseDuration(cfg.MinNotifyInterval)
	}
}

func (d *LiquidationDetector) emitLiquidation(symbol string, ts, totalUSD, longUSD, shortUSD float64,
	reason string, triggerValue, zs float64, nowTime time.Time) {

	totalM := totalUSD / 1_000_000

	var severity types.Severity
	if totalUSD >= d.absThresholdUsd*5 || (zs != 0 && math.Abs(zs) >= 4.0) {
		severity = types.SeverityHigh
	} else if totalUSD >= d.absThresholdUsd*2 || (zs != 0 && math.Abs(zs) >= 3.2) {
		severity = types.SeverityMedium
	} else {
		severity = types.SeverityLow
	}

	longPct := 50.0
	shortPct := 50.0
	if totalUSD > 0 {
		longPct = math.Round(longUSD/totalUSD*1000) / 10
		shortPct = math.Round(shortUSD/totalUSD*1000) / 10
	}

	data := map[string]any{
		"total_liquidation_usd":     math.Round(totalUSD*100) / 100,
		"total_liquidation_millions": math.Round(totalM*100) / 100,
		"long_liquidation_usd":      math.Round(longUSD*100) / 100,
		"short_liquidation_usd":     math.Round(shortUSD*100) / 100,
		"long_liquidation_pct":      longPct,
		"short_liquidation_pct":     shortPct,
		"reason":                    reason,
		"trigger_value":             math.Round(triggerValue*10000) / 10000,
		"zscore":                    math.Round(zs*10000) / 10000,
		"threshold_abs_usd":         d.absThresholdUsd,
		"threshold_zscore":          d.zscoreThreshold,
		"threshold_imbalance":       d.imbalanceThreshold,
	}
	if zs == 0 {
		data["zscore"] = nil
	}

	evt := NewEvent(symbol, "liquidation", string(severity), data)
	evt.Timestamp = ts
	select {
	case d.events <- evt:
	default:
		d.Logger.Warn("liquidation event channel full, dropping event")
	}
}
