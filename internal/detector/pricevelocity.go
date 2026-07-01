package detector

import (
	"context"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// PriceVelocityDetector monitors price change percentage over configurable
// windows and emits AnomalyEvents when any window's threshold is exceeded.
// Ported from src/kairos/detectors/price_velocity.py.
type PriceVelocityDetector struct {
	BaseDetector

	enabled   bool
	windows   []types.PriceWindow
	cooldown  time.Duration

	// symbol -> ring of (timestamp, price)
	priceHistory map[string][]pricePoint
	priceMu      sync.RWMutex
	// "symbol_windowSeconds" -> last notify time
	lastNotify map[string]time.Time
	notifyMu   sync.Mutex
}

type pricePoint struct {
	ts  float64
	price float64
}

// NewPriceVelocityDetector creates a detector from config.
func NewPriceVelocityDetector(cfg types.PriceVelocityConfig) *PriceVelocityDetector {
	d := &PriceVelocityDetector{
		BaseDetector: BaseDetector{
			Logger: slog.Default().With("detector", "price_velocity"),
			events: make(chan types.AnomalyEvent, 64),
		},
		enabled:      cfg.Enabled,
		windows:      cfg.Windows,
		cooldown:     time.Duration(cfg.CooldownSeconds) * time.Second,
		priceHistory: make(map[string][]pricePoint),
		lastNotify:   make(map[string]time.Time),
	}
	if len(d.windows) == 0 {
		d.windows = []types.PriceWindow{
			{Seconds: 30, Threshold: 0.5},
			{Seconds: 60, Threshold: 0.8},
			{Seconds: 120, Threshold: 1.2},
		}
	}
	if d.cooldown <= 0 {
		d.cooldown = 60 * time.Second
	}
	return d
}

func (d *PriceVelocityDetector) Name() string { return "price_velocity" }

// OnTicker processes a ticker update. The timestamp is taken from the
// system clock to match Python behaviour.
func (d *PriceVelocityDetector) OnTicker(_ context.Context, ticker types.Ticker) {
	if ticker.LastPrice == nil {
		return
	}
	now := time.Now()
	ts := float64(now.UnixMilli()) / 1000

	d.priceMu.Lock()
	points := d.priceHistory[ticker.Symbol]
	if points == nil {
		points = make([]pricePoint, 0, 300)
	}
	// Keep at most 300 samples (same as Python _MAX_SAMPLES).
	if len(points) >= 300 {
		points = points[1:]
	}
	points = append(points, pricePoint{ts: ts, price: *ticker.LastPrice})
	d.priceHistory[ticker.Symbol] = points
	d.priceMu.Unlock()

	d.checkVelocity(ticker.Symbol, *ticker.LastPrice, ts, now)
}

func (d *PriceVelocityDetector) OnMetricsUpdate(_ context.Context, _ string, _ float64, _ float64) {}
func (d *PriceVelocityDetector) OnLSSnapshot(_ string, _, _ float64)                              {}
func (d *PriceVelocityDetector) OnLiquidationSnapshot(_ string, _, _, _ float64)                    {}

func (d *PriceVelocityDetector) Events() <-chan types.AnomalyEvent { return d.events }
func (d *PriceVelocityDetector) Reset() {
	d.priceMu.Lock()
	clear(d.priceHistory)
	d.priceMu.Unlock()
	d.notifyMu.Lock()
	clear(d.lastNotify)
	d.notifyMu.Unlock()
}

// UpdateConfig hot-reloads configuration at runtime.
func (d *PriceVelocityDetector) UpdateConfig(cfg types.PriceVelocityConfig) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.enabled = cfg.Enabled
	if len(cfg.Windows) > 0 {
		d.windows = cfg.Windows
	}
	if cfg.CooldownSeconds > 0 {
		d.cooldown = time.Duration(cfg.CooldownSeconds) * time.Second
	}
}

func (d *PriceVelocityDetector) checkVelocity(symbol string, currentPrice float64, now float64, nowTime time.Time) {
	d.priceMu.RLock()
	points := d.priceHistory[symbol]
	d.priceMu.RUnlock()
	if len(points) < 5 {
		return
	}

	for _, win := range d.windows {
		targetTime := now - float64(win.Seconds)

		// Find the price closest to but not after target_time.
		var pastPrice float64
		found := false
		for _, p := range points {
			if p.ts <= targetTime {
				pastPrice = p.price
				found = true
			}
		}
		if !found || pastPrice <= 0 {
			continue
		}

		changePct := ((currentPrice - pastPrice) / pastPrice) * 100

		if math.Abs(changePct) < win.Threshold {
			continue
		}

		// Cooldown per symbol+window.
		key := symbol + "_" + itoa(win.Seconds)
		d.notifyMu.Lock()
		last := d.lastNotify[key]
		if nowTime.Sub(last) < d.cooldown {
			d.notifyMu.Unlock()
			continue
		}
		d.lastNotify[key] = nowTime
		d.notifyMu.Unlock()

		absChange := math.Abs(changePct)
		var severity types.Severity
		switch {
		case absChange >= win.Threshold*3:
			severity = types.SeverityHigh
		case absChange >= win.Threshold*2:
			severity = types.SeverityMedium
		default:
			severity = types.SeverityLow
		}

		evt := NewEvent(symbol, "price_velocity", string(severity), map[string]any{
			"change_pct":    round(changePct, 2),
			"window_seconds": win.Seconds,
			"threshold":      win.Threshold,
			"price_from":     round(pastPrice, 8),
			"price_to":       round(currentPrice, 8),
		})
		evt.Timestamp = now

		select {
		case d.events <- evt:
		default:
			d.Logger.Warn("price_velocity event channel full, dropping event")
		}

		// Emit only for the shortest triggering window.
		break
	}
}

// itoa is a fast int-to-string for small ints (avoids strconv import).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [12]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// round rounds f to ndigits decimal places.
func round(f float64, ndigits int) float64 {
	pow := math.Pow10(ndigits)
	return math.Round(f*pow) / pow
}
