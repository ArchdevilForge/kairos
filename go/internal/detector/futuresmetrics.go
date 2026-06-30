package detector

import (
	"context"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// FuturesMetricsDetector monitors open interest changes and funding rate
// anomalies.  Ported from src/kairos/detectors/futures_metrics.py.
type FuturesMetricsDetector struct {
	BaseDetector

	enabled             bool
	oiEnabled           bool
	oiMinChangePct      float64
	oiMinNotifyInterval time.Duration
	fundingEnabled      bool
	fundingAbsThreshold float64
	fundingMinChangeAbs float64
	fundingMinNotify    time.Duration

	// symbol -> last observed value
	lastOI      map[string]float64
	lastFunding map[string]float64
	oiMu        sync.RWMutex
	fundingMu   sync.RWMutex
}

// NewFuturesMetricsDetector creates a detector from config.
func NewFuturesMetricsDetector(cfg types.FuturesMetricsConfig) *FuturesMetricsDetector {
	d := &FuturesMetricsDetector{
		BaseDetector: BaseDetector{
			Logger:    slog.Default().With("detector", "futures_metrics"),
			events:    make(chan types.AnomalyEvent, 64),
			Cooldowns: make(map[string]time.Time),
		},
		enabled:             cfg.Enabled,
		oiEnabled:           cfg.OpenInterest.Enabled,
		oiMinChangePct:      cfg.OpenInterest.MinChangePct,
		oiMinNotifyInterval: parseDuration(cfg.OpenInterest.MinNotifyInterval),
		fundingEnabled:      cfg.FundingRate.Enabled,
		fundingAbsThreshold: cfg.FundingRate.AbsRateThreshold,
		fundingMinChangeAbs: cfg.FundingRate.MinChangeAbs,
		fundingMinNotify:    parseDuration(cfg.FundingRate.MinNotifyInterval),
		lastOI:              make(map[string]float64),
		lastFunding:         make(map[string]float64),
	}
	if d.oiMinChangePct <= 0 {
		d.oiMinChangePct = 5.0
	}
	if d.oiMinNotifyInterval <= 0 {
		d.oiMinNotifyInterval = 30 * time.Minute
	}
	if d.fundingAbsThreshold <= 0 {
		d.fundingAbsThreshold = 0.0005
	}
	if d.fundingMinChangeAbs <= 0 {
		d.fundingMinChangeAbs = 0.0003
	}
	if d.fundingMinNotify <= 0 {
		d.fundingMinNotify = 30 * time.Minute
	}
	return d
}

func (d *FuturesMetricsDetector) Name() string { return "futures_metrics" }

// OnTicker is unused — FuturesMetricsDetector receives data via OnMetricsUpdate.
func (d *FuturesMetricsDetector) OnTicker(_ context.Context, _ types.Ticker) {}

// OnMetricsUpdate processes an OI/funding-rate snapshot.  The Python version
// also receives price (set to 0 here since the Go interface omits it).
func (d *FuturesMetricsDetector) OnMetricsUpdate(_ context.Context, symbol string, oi float64, fundingRate float64) {
	if !d.enabled {
		return
	}
	now := time.Now()
	ts := float64(now.UnixMilli()) / 1000

	d.checkOpenInterest(symbol, ts, oi, now)
	d.checkFundingRate(symbol, ts, fundingRate, now)
}

func (d *FuturesMetricsDetector) OnLSSnapshot(_ string, _, _ float64)              {}
func (d *FuturesMetricsDetector) OnLiquidationSnapshot(_ string, _, _, _ float64) {}

func (d *FuturesMetricsDetector) Events() <-chan types.AnomalyEvent { return d.events }

func (d *FuturesMetricsDetector) Reset() {
	d.oiMu.Lock()
	clear(d.lastOI)
	d.oiMu.Unlock()
	d.fundingMu.Lock()
	clear(d.lastFunding)
	d.fundingMu.Unlock()
	d.cdMu.Lock()
	clear(d.Cooldowns)
	d.cdMu.Unlock()
}

// UpdateConfig hot-reloads configuration at runtime.
func (d *FuturesMetricsDetector) UpdateConfig(cfg types.FuturesMetricsConfig) {
	d.BaseDetector.mu.Lock()
	defer d.BaseDetector.mu.Unlock()
	d.enabled = cfg.Enabled
	d.oiEnabled = cfg.OpenInterest.Enabled
	if cfg.OpenInterest.MinChangePct > 0 {
		d.oiMinChangePct = cfg.OpenInterest.MinChangePct
	}
	if cfg.OpenInterest.MinNotifyInterval != "" {
		d.oiMinNotifyInterval = parseDuration(cfg.OpenInterest.MinNotifyInterval)
	}
	d.fundingEnabled = cfg.FundingRate.Enabled
	if cfg.FundingRate.AbsRateThreshold > 0 {
		d.fundingAbsThreshold = cfg.FundingRate.AbsRateThreshold
	}
	if cfg.FundingRate.MinChangeAbs > 0 {
		d.fundingMinChangeAbs = cfg.FundingRate.MinChangeAbs
	}
	if cfg.FundingRate.MinNotifyInterval != "" {
		d.fundingMinNotify = parseDuration(cfg.FundingRate.MinNotifyInterval)
	}
}

// ---------------------------------------------------------------------------
// Open interest
// ---------------------------------------------------------------------------

func (d *FuturesMetricsDetector) checkOpenInterest(symbol string, now float64, oi float64, nowTime time.Time) {
	d.oiMu.RLock()
	oiEnabled := d.oiEnabled
	d.oiMu.RUnlock()
	if !oiEnabled || oi <= 0 {
		return
	}

	d.oiMu.Lock()
	previous, has := d.lastOI[symbol]
	d.lastOI[symbol] = oi
	d.oiMu.Unlock()
	if !has || previous <= 0 {
		return
	}

	changePct := ((oi - previous) / previous) * 100
	if math.Abs(changePct) < d.oiMinChangePct {
		return
	}

	key := symbol + "_open_interest_change"
	d.cdMu.Lock()
	if !canNotify(d.Cooldowns, key, nowTime, d.oiMinNotifyInterval) {
		d.cdMu.Unlock()
		return
	}
	d.cdMu.Unlock()

	absChange := math.Abs(changePct)
	var severity types.Severity
	if absChange >= d.oiMinChangePct*2 {
		severity = types.SeverityHigh
	} else {
		severity = types.SeverityMedium
	}

	evt := NewEvent(symbol, "open_interest_change", string(severity), map[string]any{
		"price":               0.0,
		"open_interest":       round(oi, 8),
		"previous_open_interest": round(previous, 8),
		"change_pct":          round(changePct, 2),
		"threshold_pct":       d.oiMinChangePct,
	})
	evt.Timestamp = now
	d.emit(evt)
}

// ---------------------------------------------------------------------------
// Funding rate
// ---------------------------------------------------------------------------

func (d *FuturesMetricsDetector) checkFundingRate(symbol string, now float64, fundingRate float64, nowTime time.Time) {
	d.fundingMu.RLock()
	fundingEnabled := d.fundingEnabled
	d.fundingMu.RUnlock()
	if !fundingEnabled {
		return
	}

	d.fundingMu.Lock()
	previous, has := d.lastFunding[symbol]
	d.lastFunding[symbol] = fundingRate
	d.fundingMu.Unlock()

	changeAbs := 0.0
	if has {
		changeAbs = math.Abs(fundingRate - previous)
	}
	isExtreme := math.Abs(fundingRate) >= d.fundingAbsThreshold
	isShift := has && changeAbs >= d.fundingMinChangeAbs
	if !isExtreme && !isShift {
		return
	}

	key := symbol + "_funding_rate_anomaly"
	d.cdMu.Lock()
	if !canNotify(d.Cooldowns, key, nowTime, d.fundingMinNotify) {
		d.cdMu.Unlock()
		return
	}
	d.cdMu.Unlock()

	reason := ""
	if isExtreme && isShift {
		reason = "extreme+shift"
	} else if isExtreme {
		reason = "extreme"
	} else {
		reason = "shift"
	}

	var severity types.Severity
	if math.Abs(fundingRate) >= d.fundingAbsThreshold*2 {
		severity = types.SeverityHigh
	} else {
		severity = types.SeverityMedium
	}

	evt := NewEvent(symbol, "funding_rate_anomaly", string(severity), map[string]any{
		"price":                0.0,
		"funding_rate":        fundingRate,
		"previous_funding_rate": previous,
		"change_abs":          changeAbs,
		"abs_threshold":       d.fundingAbsThreshold,
		"change_threshold":    d.fundingMinChangeAbs,
		"reason":              reason,
	})
	evt.Timestamp = now
	d.emit(evt)
}

// emit tries to send an event to the channel without blocking.
func (d *FuturesMetricsDetector) emit(evt types.AnomalyEvent) {
	select {
	case d.events <- evt:
	default:
		d.Logger.Warn("futures_metrics event channel full, dropping event")
	}
}

// canNotify checks the cooldown map and returns true if the event should
// fire.  If it returns true, the key has been updated to nowTime. Caller
// must hold cdMu.
func canNotify(m map[string]time.Time, key string, now time.Time, interval time.Duration) bool {
	last, ok := m[key]
	if !ok || now.Sub(last) >= interval {
		m[key] = now
		return true
	}
	return false
}
