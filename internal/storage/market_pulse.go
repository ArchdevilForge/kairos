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

// MarketPulseStore is an append-only JSONL log of market pulse events.
type MarketPulseStore struct {
	path string
	mu   sync.Mutex
}

// NewMarketPulseStore opens or creates the market pulse event log.
func NewMarketPulseStore(cfg types.StorageConfig) (*MarketPulseStore, error) {
	dir := filepath.Dir(expandPath(cfg.DatabasePath))
	path := filepath.Join(dir, "market-pulse-events.jsonl")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("market pulse store mkdir: %w", err)
	}
	return &MarketPulseStore{path: path}, nil
}

// Path returns the JSONL file path.
func (s *MarketPulseStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
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
