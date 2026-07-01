package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestHintStore_RecordAndBoost(t *testing.T) {
	dir := t.TempDir()
	cfg := types.StorageConfig{
		DatabasePath:            filepath.Join(dir, "kairos.db"),
		WatchHintRetentionHours: 24,
		WatchHintScoreBoost:     1.0,
	}
	store, err := NewHintStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Record("ETH/USDT:USDT", "volume_spike", "okx"); err != nil {
		t.Fatal(err)
	}
	boosts := store.ActiveBoosts()
	if boosts["ETH/USDT:USDT"] != 1.0 {
		t.Fatalf("boost: %v", boosts)
	}
}

func TestHintStore_ExpiredHintIgnored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "watch-hints.jsonl")
	old := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339Nano)
	if err := os.WriteFile(path, []byte(`{"symbol":"BTC/USDT:USDT","event_type":"price_velocity","exchange":"okx","at":"`+old+`"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := &HintStore{path: path, retention: 24 * time.Hour, boost: 0.5}
	if len(store.ActiveBoosts()) != 0 {
		t.Fatal("expected expired hint ignored")
	}
}
