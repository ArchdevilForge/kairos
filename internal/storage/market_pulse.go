package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// MarketPulseRecord is one persisted market event + snapshot fields.
type MarketPulseRecord struct {
	Timestamp        float64        `json:"timestamp"`
	EventType        string         `json:"event_type"`
	Direction        string         `json:"direction"`
	StateFrom        string         `json:"state_from"`
	StateTo          string         `json:"state_to"`
	MedianReturn60s  float64        `json:"median_return_60s"`
	MedianReturn300s float64        `json:"median_return_300s"`
	MedianZ60s       float64        `json:"median_z_60s"`
	Breadth          float64        `json:"breadth"`
	ValidSymbols     int            `json:"valid_symbols"`
	FreshRatio       float64        `json:"fresh_ratio"`
	ShadowMode       bool           `json:"shadow_mode"`
	Payload          map[string]any `json:"payload"`
	RecordedAt       time.Time      `json:"recorded_at"`
}

// MarketPulseStore is an append-only JSONL log of market pulse events, outcomes,
// and periodic cross-sectional snapshots used for attention-lift calibration.
type MarketPulseStore struct {
	path         string
	outcomePath  string
	snapshotPath string
	mu           sync.Mutex
	lastSnapTS   float64 // last persisted snapshot timestamp (throttle)
}

// Dir returns the directory that anchors every storage sidecar file, so
// out-of-process tools resolve the same paths as the daemon instead of
// reimplementing the derivation.
func Dir(cfg types.StorageConfig) string {
	return filepath.Dir(expandPath(cfg.DatabasePath))
}

// MarketPulseEventsPath and MarketPulseOutcomesPath name the calibration logs.
func MarketPulseEventsPath(cfg types.StorageConfig) string {
	return filepath.Join(Dir(cfg), "market-pulse-events.jsonl")
}

func MarketPulseOutcomesPath(cfg types.StorageConfig) string {
	return filepath.Join(Dir(cfg), "market-pulse-outcomes.jsonl")
}

// MarketPulseSnapshotsPath names the 60s cross-sectional baseline log.
func MarketPulseSnapshotsPath(cfg types.StorageConfig) string {
	return filepath.Join(Dir(cfg), "market-pulse-snapshots.jsonl")
}

// snapshotMinIntervalSeconds is the floor between persisted snapshot rows.
// Detector evaluate may run every 5s; calibration only needs ~1 sample/minute.
const snapshotMinIntervalSeconds = 60

// NewMarketPulseStore opens or creates the market pulse event log.
func NewMarketPulseStore(cfg types.StorageConfig) (*MarketPulseStore, error) {
	dir := Dir(cfg)
	path := MarketPulseEventsPath(cfg)
	outcomePath := MarketPulseOutcomesPath(cfg)
	snapshotPath := MarketPulseSnapshotsPath(cfg)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("market pulse store mkdir: %w", err)
	}
	return &MarketPulseStore{path: path, outcomePath: outcomePath, snapshotPath: snapshotPath}, nil
}

// Path returns the events JSONL file path.
func (s *MarketPulseStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// OutcomePath returns the outcomes JSONL file path.
func (s *MarketPulseStore) OutcomePath() string {
	if s == nil {
		return ""
	}
	return s.outcomePath
}

// SnapshotPath returns the periodic snapshot JSONL file path.
func (s *MarketPulseStore) SnapshotPath() string {
	if s == nil {
		return ""
	}
	return s.snapshotPath
}

// Record appends one market event. Nil-safe.
func (s *MarketPulseStore) Record(evt types.AnomalyEvent) error {
	if s == nil {
		return nil
	}
	data := evt.Data
	if data == nil {
		data = map[string]any{}
	}
	rec := MarketPulseRecord{
		Timestamp:        evt.Timestamp,
		EventType:        evt.EventType,
		Direction:        fmt.Sprint(data["direction"]),
		StateFrom:        fmt.Sprint(data["state_from"]),
		StateTo:          fmt.Sprint(data["state_to"]),
		MedianReturn60s:  asFloat(data["median_return_60s_pct"]),
		MedianReturn300s: asFloat(data["median_return_300s_pct"]),
		MedianZ60s:       asFloat(data["median_z_60s"]),
		Breadth:          asFloat(data["breadth"]),
		ValidSymbols:     asInt(data["valid_symbols"]),
		FreshRatio:       asFloat(data["fresh_ratio"]),
		ShadowMode:       asBool(data["shadow_mode"]),
		Payload:          data,
		RecordedAt:       time.Now().UTC(),
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.Write(append(line, '\n'))
	return err
}

// MarketPulseOutcomeRecord is one completed post-event performance sample.
type MarketPulseOutcomeRecord struct {
	Timestamp        float64        `json:"timestamp"`
	SourceEvent      string         `json:"source_event"`
	Direction        string         `json:"direction"`
	EventTS          float64        `json:"event_ts"`
	MedianReturn1m   float64        `json:"median_return_1m"`
	MedianReturn3m   float64        `json:"median_return_3m"`
	MedianReturn5m   float64        `json:"median_return_5m"`
	MedianReturn15m  float64        `json:"median_return_15m"`
	MFE              float64        `json:"mfe"`
	MAE              float64        `json:"mae"`
	MaxBreadth       float64        `json:"max_breadth"`
	TrendDurationS   float64        `json:"trend_duration_s"`
	Reversed         bool           `json:"reversed"`
	ImpulsePrecision bool           `json:"impulse_precision"`
	TrendPrecision   bool           `json:"trend_precision"`
	ShadowMode       bool           `json:"shadow_mode"`
	Payload          map[string]any `json:"payload"`
	RecordedAt       time.Time      `json:"recorded_at"`
}

// MarketPulseSnapshotRecord is one lightweight cross-sectional sample for
// random-baseline attention lift. Keep fields small: ~1.4k rows/day.
type MarketPulseSnapshotRecord struct {
	Timestamp        float64   `json:"timestamp"`
	State            string    `json:"state"`
	DataOK           bool      `json:"data_ok"`
	ValidSymbols     int       `json:"valid_symbols"`
	ValidZSymbols    int       `json:"valid_z_symbols"`
	ZUsable          bool      `json:"z_usable"`
	FreshRatio       float64   `json:"fresh_ratio"`
	MedianReturn60s  float64   `json:"median_return_60s"`
	MedianReturn180s float64   `json:"median_return_180s"`
	MedianReturn300s float64   `json:"median_return_300s"`
	MedianZ60s       float64   `json:"median_z_60s"`
	UpBreadth60s     float64   `json:"up_breadth_60s"`
	DownBreadth60s   float64   `json:"down_breadth_60s"`
	RecordedAt       time.Time `json:"recorded_at"`
}

// RecordSnapshot appends one snapshot row, throttled to one per 60s of snap time.
// Nil-safe. Skips zero-timestamp snapshots (detector not yet evaluated).
func (s *MarketPulseStore) RecordSnapshot(snap types.MarketSnapshot, state string) error {
	if s == nil || snap.Timestamp <= 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastSnapTS > 0 && snap.Timestamp-s.lastSnapTS < snapshotMinIntervalSeconds {
		return nil
	}
	rec := MarketPulseSnapshotRecord{
		Timestamp:        snap.Timestamp,
		State:            state,
		DataOK:           snap.DataOK,
		ValidSymbols:     snap.ValidSymbols,
		ValidZSymbols:    snap.ValidZSymbols,
		ZUsable:          snap.ZUsable,
		FreshRatio:       snap.FreshRatio,
		MedianReturn60s:  snap.MedianReturn60s,
		MedianReturn180s: snap.MedianReturn180s,
		MedianReturn300s: snap.MedianReturn300s,
		MedianZ60s:       snap.MedianZ60s,
		UpBreadth60s:     snap.UpBreadth60s,
		DownBreadth60s:   snap.DownBreadth60s,
		RecordedAt:       time.Now().UTC(),
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.snapshotPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err = f.Write(append(line, '\n')); err != nil {
		return err
	}
	s.lastSnapTS = snap.Timestamp
	return nil
}

// RecordOutcome appends one completed post-event outcome. Nil-safe.
func (s *MarketPulseStore) RecordOutcome(evt types.AnomalyEvent) error {
	if s == nil {
		return nil
	}
	data := evt.Data
	if data == nil {
		data = map[string]any{}
	}
	rec := MarketPulseOutcomeRecord{
		Timestamp:        evt.Timestamp,
		SourceEvent:      fmt.Sprint(data["source_event"]),
		Direction:        fmt.Sprint(data["direction"]),
		EventTS:          asFloat(data["event_ts"]),
		MedianReturn1m:   asFloat(data["median_return_1m"]),
		MedianReturn3m:   asFloat(data["median_return_3m"]),
		MedianReturn5m:   asFloat(data["median_return_5m"]),
		MedianReturn15m:  asFloat(data["median_return_15m"]),
		MFE:              asFloat(data["mfe"]),
		MAE:              asFloat(data["mae"]),
		MaxBreadth:       asFloat(data["max_breadth"]),
		TrendDurationS:   asFloat(data["trend_duration_s"]),
		Reversed:         asBool(data["reversed"]),
		ImpulsePrecision: asBool(data["impulse_precision"]),
		TrendPrecision:   asBool(data["trend_precision"]),
		ShadowMode:       asBool(data["shadow_mode"]),
		Payload:          data,
		RecordedAt:       time.Now().UTC(),
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(s.outcomePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.Write(append(line, '\n'))
	return err
}

func asFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}

func asInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

func asBool(v any) bool {
	b, _ := v.(bool)
	return b
}
