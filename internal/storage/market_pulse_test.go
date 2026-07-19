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
}
