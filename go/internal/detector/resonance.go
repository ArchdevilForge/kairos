package detector

import (
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// ResonanceEvent is a multi-dimension resonance-scored alert.
type ResonanceEvent struct {
	Symbol         string                       `json:"symbol" yaml:"symbol"`
	SignalScore    float64                      `json:"signal_score" yaml:"signal_score"`
	DimensionCount int                          `json:"dimension_count" yaml:"dimension_count"`
	Dimensions     map[string]types.AnomalyEvent `json:"dimensions" yaml:"dimensions"`
	Timestamp      float64                      `json:"timestamp" yaml:"timestamp"`
}

// ToAlertEvent converts the ResonanceEvent to a standard AnomalyEvent.
func (r *ResonanceEvent) ToAlertEvent() types.AnomalyEvent {
	dimKeys := make([]string, 0, len(r.Dimensions))
	for k := range r.Dimensions {
		dimKeys = append(dimKeys, k)
	}
	data := map[string]any{
		"signal_score":    r.SignalScore,
		"dimension_count": r.DimensionCount,
		"dimensions":      dimKeys,
	}
	for dim, ev := range r.Dimensions {
		data[dim+"_data"] = ev.Data
	}
	return types.AnomalyEvent{
		Symbol:    r.Symbol,
		EventType: "resonance",
		Severity:  types.SeverityHigh,
		Data:      data,
		Timestamp: r.Timestamp,
	}
}

// ResonanceScorer aggregates AnomalyEvents from multiple detectors.
// When multiple dimensions fire for the same symbol within a window,
// it computes a Signal Quality Score (0-100) and emits a ResonanceEvent.
//
// Score components (port from Python _signal_quality_score):
//   - Extremity (0-40): max z-score across active dimensions
//   - Resonance (0-30): 2 dimensions → 10, 3 → 20, 4+ → 30
//   - Direction (0-20): all point same way
//   - Context (0-10): funding + OI combo
//
// Push if score ≥ minScore (default 55).
// Window: configurable (default 300s)
// Cooldown: configurable (default 600s)
// Minimum dimensions: configurable (default 2)
// Minimum score: configurable (default 55)
type ResonanceScorer struct {
	enabled         bool
	windowSeconds   float64
	minDimensions   int
	minScore        float64
	cooldownSeconds float64

	mu           sync.RWMutex
	windows      map[string]map[string]types.AnomalyEvent // symbol -> event_type -> event
	lastEmission map[string]float64                       // symbol -> timestamp
	callbacks    []func(ResonanceEvent)
	logger       *slog.Logger
	events       chan ResonanceEvent
}

// NewResonanceScorer creates a ResonanceScorer from config.
func NewResonanceScorer(cfg types.ResonanceScorerConfig) *ResonanceScorer {
	rs := &ResonanceScorer{
		enabled:         cfg.Enabled,
		windowSeconds:   300,
		minDimensions:   2,
		minScore:        55,
		cooldownSeconds: 600,
		windows:         make(map[string]map[string]types.AnomalyEvent),
		lastEmission:    make(map[string]float64),
		logger:          slog.Default().With("detector", "resonance"),
		events:          make(chan ResonanceEvent, 64),
	}
	if cfg.WindowSeconds > 0 {
		rs.windowSeconds = float64(cfg.WindowSeconds)
	}
	if cfg.MinDimensions > 0 {
		rs.minDimensions = cfg.MinDimensions
	}
	if cfg.MinScore > 0 {
		rs.minScore = cfg.MinScore
	}
	if cfg.CooldownSeconds > 0 {
		rs.cooldownSeconds = float64(cfg.CooldownSeconds)
	}
	return rs
}

// OnEvent processes an incoming AnomalyEvent from any detector.
func (rs *ResonanceScorer) OnEvent(event types.AnomalyEvent) {
	if !rs.enabled || event.EventType == "resonance" {
		return
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()

	now := event.Timestamp
	if now <= 0 {
		now = float64(time.Now().UnixMilli()) / 1000
	}

	rs.pruneWindows(now)

	symbol := event.Symbol
	if rs.windows[symbol] == nil {
		rs.windows[symbol] = make(map[string]types.AnomalyEvent)
	}
	existing, ok := rs.windows[symbol][event.EventType]
	if ok && extremityZ(existing.Data) >= extremityZ(event.Data) {
		return
	}
	rs.windows[symbol][event.EventType] = event
	rs.evaluate(symbol, now)
}

// SetCallback registers a callback for ResonanceEvents.
func (rs *ResonanceScorer) SetCallback(cb func(ResonanceEvent)) {
	rs.callbacks = append(rs.callbacks, cb)
}

// Events returns the read-only resonance event channel.
func (rs *ResonanceScorer) Events() <-chan ResonanceEvent {
	return rs.events
}

// pruneWindows removes symbols where all events are older than the window.
func (rs *ResonanceScorer) pruneWindows(now float64) {
	cutoff := now - rs.windowSeconds
	for sym, dims := range rs.windows {
		allExpired := true
		for _, e := range dims {
			if e.Timestamp >= cutoff {
				allExpired = false
				break
			}
		}
		if allExpired {
			delete(rs.windows, sym)
		}
	}
}

// evaluate checks whether the symbol's window meets criteria and emits if so.
func (rs *ResonanceScorer) evaluate(symbol string, now float64) {
	dims := rs.windows[symbol]
	if len(dims) < rs.minDimensions {
		return
	}
	if now-rs.lastEmission[symbol] < rs.cooldownSeconds {
		return
	}

	score := signalQualityScore(dims)
	if score < rs.minScore {
		return
	}

	dimCopy := make(map[string]types.AnomalyEvent, len(dims))
	for k, v := range dims {
		dimCopy[k] = v
	}
	rEvent := ResonanceEvent{
		Symbol:         symbol,
		SignalScore:    math.Round(score),
		DimensionCount: len(dims),
		Dimensions:     dimCopy,
		Timestamp:      now,
	}
	rs.lastEmission[symbol] = now

	// Channel delivery (non-blocking).
	select {
	case rs.events <- rEvent:
	default:
		rs.logger.Warn("resonance event channel full, dropping event")
	}

	// Callback delivery.
	for _, cb := range rs.callbacks {
		func(cb func(ResonanceEvent), ev ResonanceEvent) {
			defer func() {
				if r := recover(); r != nil {
					rs.logger.Error("resonance callback panic", "recover", r)
				}
			}()
			cb(ev)
		}(cb, rEvent)
	}
}

// Reset clears all windows and emission history.
func (rs *ResonanceScorer) Reset() {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	clear(rs.windows)
	clear(rs.lastEmission)
}

// UpdateConfig hot-reloads configuration at runtime.
func (rs *ResonanceScorer) UpdateConfig(cfg types.ResonanceScorerConfig) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.enabled = cfg.Enabled
	if cfg.WindowSeconds > 0 {
		rs.windowSeconds = float64(cfg.WindowSeconds)
	}
	if cfg.MinDimensions > 0 {
		rs.minDimensions = cfg.MinDimensions
	}
	if cfg.MinScore > 0 {
		rs.minScore = cfg.MinScore
	}
	if cfg.CooldownSeconds > 0 {
		rs.cooldownSeconds = float64(cfg.CooldownSeconds)
	}
}

// ── Signal Quality Score ────────────────────────────────────────────────

// signalQualityScore computes the composite Signal Quality Score (0-100).
func signalQualityScore(dims map[string]types.AnomalyEvent) float64 {
	// 1. Extremity (0-40): max z-score equivalent across dimensions.
	maxZ := 0.0
	for _, e := range dims {
		z := extremityZ(e.Data)
		if z > maxZ {
			maxZ = z
		}
	}

	var extremity float64
	switch {
	case maxZ >= 5.0:
		extremity = 40
	case maxZ >= 4.0:
		extremity = 30
	case maxZ >= 3.0:
		extremity = 20
	case maxZ >= 2.5:
		extremity = 10
	default:
		maxSev := 1
		for _, e := range dims {
			if s := sevScore(string(e.Severity)); s > maxSev {
				maxSev = s
			}
		}
		extremity = float64(maxSev-1) * 10
	}

	// 2. Resonance (0-30): 2→10, 3→20, 4+→30.
	n := len(dims)
	resonance := float64(min(n, 4)*10 - 10)

	// 3. Direction (0-20): consensus among non-neutral dimensions.
	directions := make([]int, 0, n)
	for _, e := range dims {
		directions = append(directions, directionBias(e.EventType, e.Data))
	}
	nonNeutral := make([]int, 0, len(directions))
	for _, d := range directions {
		if d != 0 {
			nonNeutral = append(nonNeutral, d)
		}
	}

	var directionScore float64
	if len(nonNeutral) >= 2 && allPositive(nonNeutral) {
		directionScore = 20
	} else if len(nonNeutral) >= 2 && allNegative(nonNeutral) {
		directionScore = 20
	} else if len(nonNeutral) >= 2 {
		_, hasFunding := dims["funding_rate_anomaly"]
		_, hasLS := dims["long_short_ratio"]
		_, hasLiq := dims["liquidation"]
		if hasFunding && hasLS {
			fDir := directionBias("funding_rate_anomaly", dims["funding_rate_anomaly"].Data)
			lsDir := directionBias("long_short_ratio", dims["long_short_ratio"].Data)
			if fDir != 0 && lsDir != 0 && fDir == lsDir {
				directionScore = 10
			} else {
				directionScore = 0
			}
		} else if hasLiq {
			directionScore = 10
		} else {
			directionScore = 0
		}
	} else {
		directionScore = 10
	}

	// 4. Context bonus (0-10): funding + OI = crowded trade confirmation.
	ctxBonus := 0.0
	_, hasFunding := dims["funding_rate_anomaly"]
	_, hasOI := dims["open_interest_change"]
	if hasFunding && hasOI {
		ctxBonus = 5
	}

	total := extremity + resonance + directionScore + ctxBonus
	if total > 100 {
		total = 100
	}
	return total
}

// extremityZ maps data fields to z-score equivalents.
func extremityZ(data map[string]any) float64 {
	// Direct zscore field.
	if z, ok := getFloat(data, "zscore"); ok && z > 0 {
		return math.Abs(z)
	}

	// Funding rate.
	if rate, ok := getFloat(data, "funding_rate"); ok {
		absRate := math.Abs(rate)
		switch {
		case absRate >= 0.002:
			return 6.0
		case absRate >= 0.001:
			return 4.0
		case absRate >= 0.0005:
			return 2.5
		}
	}

	// Liquidation total in millions.
	if millions, ok := getFloat(data, "total_liquidation_millions"); ok {
		switch {
		case millions >= 20:
			return 6.0
		case millions >= 10:
			return 4.0
		case millions >= 5:
			return 3.0
		}
	}

	// Open interest change % (port from Python: also reads change_pct).
	if oi, ok := getFloat(data, "change_pct"); ok {
		absOI := math.Abs(oi)
		switch {
		case absOI >= 30:
			return 5.0
		case absOI >= 15:
			return 3.0
		}
	}

	// Velocity change % (port from Python: same field, different thresholds).
	if vel, ok := getFloat(data, "change_pct"); ok {
		absVel := math.Abs(vel)
		switch {
		case absVel >= 3:
			return 4.0
		case absVel >= 1.5:
			return 2.5
		}
	}

	// Volume spike ratio.
	if ratio, ok := getFloat(data, "ratio"); ok {
		switch {
		case ratio >= 15:
			return 5.0
		case ratio >= 10:
			return 3.0
		}
	}

	// L/S ratio deviation from 1.
	if lsRatio, ok := getFloat(data, "ls_ratio"); ok && math.Abs(lsRatio-1) > 3 {
		return 5.0
	}

	// Long rate.
	if longRate, ok := getFloat(data, "long_rate"); ok {
		switch {
		case longRate >= 85:
			return 4.0
		case longRate >= 80:
			return 2.5
		}
	}

	return 0.0
}

// directionBias determines bullish (1) / bearish (-1) / neutral (0) per event type.
func directionBias(eventType string, data map[string]any) int {
	switch eventType {
	case "price_velocity":
		if pct, ok := getFloat(data, "change_pct"); ok {
			if pct > 0 {
				return 1
			}
			return -1
		}
		return 0
	case "volume_spike":
		return 0
	case "open_interest_change":
		return 0
	case "funding_rate_anomaly":
		if rate, ok := getFloat(data, "funding_rate"); ok {
			if rate > 0 {
				return -1 // positive funding = bearish
			}
			return 1 // negative funding = bullish
		}
		return 0
	case "long_short_ratio":
		if longRate, ok := getFloat(data, "long_rate"); ok {
			switch {
			case longRate > 60:
				return -1
			case longRate < 40:
				return 1
			default:
				return 0
			}
		}
		return 0
	case "liquidation":
		if longPct, ok := getFloat(data, "long_liquidation_pct"); ok {
			switch {
			case longPct > 60:
				return -1
			case longPct < 40:
				return 1
			default:
				return 0
			}
		}
		return 0
	}
	return 0
}

// ── Helpers ─────────────────────────────────────────────────────────────

// getFloat extracts a float64 value from a map.
func getFloat(data map[string]any, key string) (float64, bool) {
	v, ok := data[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	case uint:
		return float64(n), true
	case int32:
		return float64(n), true
	case uint32:
		return float64(n), true
	case float32:
		return float64(n), true
	case uint8:
		return float64(n), true
	case int8:
		return float64(n), true
	case bool:
		if n {
			return 1, true
		}
		return 0, true
	case string:
		return 0, false // Python try/except would return 0, but we return not-ok
	default:
		return 0, false
	}
}

func allPositive(vals []int) bool {
	for _, v := range vals {
		if v <= 0 {
			return false
		}
	}
	return true
}

func allNegative(vals []int) bool {
	for _, v := range vals {
		if v >= 0 {
			return false
		}
	}
	return true
}

func sevScore(s string) int {
	switch s {
	case "LOW":
		return 1
	case "MEDIUM":
		return 2
	case "HIGH":
		return 3
	default:
		return 1
	}
}
