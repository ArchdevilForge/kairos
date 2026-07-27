package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestMarketPulseStore_Record(t *testing.T) {
	dir := t.TempDir()
	cfg := types.StorageConfig{DatabasePath: filepath.Join(dir, "kairos.db")}
	s, err := NewMarketPulseStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	err = s.Record(types.AnomalyEvent{
		Symbol:    "MARKET",
		EventType: "market_impulse",
		Timestamp: 1,
		Data: map[string]any{
			"direction":             "up",
			"state_from":            "QUIET",
			"state_to":              "IMPULSE_UP",
			"median_return_60s_pct": 0.23,
			"breadth":               0.76,
			"valid_symbols":         29,
			"fresh_ratio":           0.97,
			"shadow_mode":           true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	line := string(b)
	if !strings.Contains(line, "market_impulse") || !strings.Contains(line, "IMPULSE_UP") {
		t.Fatalf("payload: %s", line)
	}
}

func TestMarketPulseStore_NilSafe(t *testing.T) {
	var s *MarketPulseStore
	if err := s.Record(types.AnomalyEvent{}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordOutcome(types.AnomalyEvent{}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordSnapshot(types.MarketSnapshot{Timestamp: 1}, "QUIET"); err != nil {
		t.Fatal(err)
	}
}

func TestMarketPulseStore_RecordSnapshotThrottle(t *testing.T) {
	dir := t.TempDir()
	cfg := types.StorageConfig{DatabasePath: filepath.Join(dir, "kairos.db")}
	s, err := NewMarketPulseStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	snap := types.MarketSnapshot{
		Timestamp:       1000,
		DataOK:          true,
		ValidSymbols:    20,
		MedianReturn60s: 0.12,
		UpBreadth60s:    0.6,
	}
	if err := s.RecordSnapshot(snap, "QUIET"); err != nil {
		t.Fatal(err)
	}
	// Within 60s window — must not write a second line.
	snap.Timestamp = 1030
	snap.MedianReturn60s = 0.99
	if err := s.RecordSnapshot(snap, "QUIET"); err != nil {
		t.Fatal(err)
	}
	snap.Timestamp = 1061
	if err := s.RecordSnapshot(snap, "IMPULSE_UP"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(s.SnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 snapshot lines, got %d: %s", len(lines), b)
	}
	if !strings.Contains(lines[1], "IMPULSE_UP") {
		t.Fatalf("second line: %s", lines[1])
	}
}

func TestMarketPulseStore_RecordOutcome(t *testing.T) {
	dir := t.TempDir()
	cfg := types.StorageConfig{DatabasePath: filepath.Join(dir, "kairos.db")}
	s, err := NewMarketPulseStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	err = s.RecordOutcome(types.AnomalyEvent{
		Symbol:    "MARKET",
		EventType: "market_outcome",
		Timestamp: 1000,
		Data: map[string]any{
			"source_event":      "market_impulse",
			"direction":         "up",
			"event_ts":          100.0,
			"median_return_1m":  0.10,
			"median_return_3m":  0.20,
			"median_return_5m":  0.30,
			"median_return_15m": 0.50,
			"mfe":               0.55,
			"mae":               0.05,
			"max_breadth":       0.80,
			"trend_duration_s":  400.0,
			"reversed":          false,
			"impulse_precision": true,
			"trend_precision":   true,
			"shadow_mode":       true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(s.OutcomePath())
	if err != nil {
		t.Fatal(err)
	}
	line := string(b)
	if !strings.Contains(line, "market_impulse") || !strings.Contains(line, "impulse_precision") {
		t.Fatalf("payload: %s", line)
	}
}
