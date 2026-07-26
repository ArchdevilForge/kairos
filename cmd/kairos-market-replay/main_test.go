package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTicks(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ticks.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadTicks_SortsByTimestampThenSymbol(t *testing.T) {
	path := writeTicks(t, `
{"ts": 1060, "symbol": "ETH/USDT:USDT", "price": 3000}
{"ts": 1000, "symbol": "BTC/USDT:USDT", "price": 60000}
{"ts": 1060, "symbol": "BTC/USDT:USDT", "price": 60100}
`)
	ticks, err := loadTicks(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(ticks) != 3 {
		t.Fatalf("ticks: %d", len(ticks))
	}
	if ticks[0].TS != 1000 || ticks[1].Symbol != "BTC/USDT:USDT" || ticks[2].Symbol != "ETH/USDT:USDT" {
		t.Fatalf("sort order wrong: %+v", ticks)
	}
}

func TestLoadTicks_SkipsInvalidRowsKeepsValid(t *testing.T) {
	path := writeTicks(t, `
{"ts": 1000, "symbol": "", "price": 100}
{"ts": 1000, "symbol": "AAA/USDT:USDT", "price": 0}
{"ts": 1000, "symbol": "BBB/USDT:USDT", "price": 5}
`)
	ticks, err := loadTicks(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(ticks) != 1 || ticks[0].Symbol != "BBB/USDT:USDT" {
		t.Fatalf("expected only the valid row, got %+v", ticks)
	}
}

func TestLoadTicks_BadJSONFailsLoudly(t *testing.T) {
	path := writeTicks(t, `{"ts": 1000, "symbol": "AAA/USDT:USDT", "price": 5}
{not-json}`)
	if _, err := loadTicks(path); err == nil {
		t.Fatal("malformed JSONL must be an error, not silently dropped")
	}
}

func TestLoadTicks_MissingFile(t *testing.T) {
	if _, err := loadTicks("/nonexistent/ticks.jsonl"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadTicks_EmptyFileYieldsNoTicks(t *testing.T) {
	path := writeTicks(t, "\n\n")
	ticks, err := loadTicks(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(ticks) != 0 {
		t.Fatalf("ticks: %+v", ticks)
	}
}

func TestUniqueSymbols(t *testing.T) {
	ticks := []tick{
		{TS: 1, Symbol: "A", Price: 1},
		{TS: 2, Symbol: "B", Price: 1},
		{TS: 3, Symbol: "A", Price: 1},
	}
	syms := uniqueSymbols(ticks)
	if len(syms) != 2 || syms[0] != "A" || syms[1] != "B" {
		t.Fatalf("syms: %v", syms)
	}
}
