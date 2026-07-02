package utils

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNewBlacklist_LoadsFile(t *testing.T) {
	home := t.TempDir()
	cfgDir := filepath.Join(home, ".config", "kairos")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "blacklist.txt"), []byte(" btc/usdt:usdt \n\nDOGE/USDT:USDT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	b := NewBlacklist()
	if !b.IsBlocked("BTC/USDT:USDT") || !b.IsBlocked("DOGE/USDT:USDT") {
		t.Fatal("expected blocked symbols from file")
	}
	if b.IsBlocked("ETH/USDT:USDT") {
		t.Fatal("unexpected block")
	}
}

func TestExtractLastPrice_NestedInfo(t *testing.T) {
	ticker := map[string]any{
		"info": map[string]any{"markPx": "123.45"},
	}
	v := ExtractLastPrice(ticker)
	if v == nil || *v != 123.45 {
		t.Fatalf("nested markPx: %v", v)
	}
}

func TestExtractQuoteVolume_BaseTimesPrice(t *testing.T) {
	ticker := map[string]any{
		"baseVolume": 10.0,
		"last":       2.5,
	}
	if got := ExtractQuoteVolume(ticker); got != 25.0 {
		t.Fatalf("base*price: got %v", got)
	}

	nested := map[string]any{
		"info": map[string]any{"volUsd24h": "1000"},
	}
	if ExtractQuoteVolume(nested) != 1000 {
		t.Fatal("nested volUsd24h")
	}
}

func TestFirstFloat_JSONNumber(t *testing.T) {
	m := map[string]any{"n": json.Number("9.5")}
	v := FirstFloat(m, []string{"n"})
	if v == nil || *v != 9.5 {
		t.Fatalf("json.Number: %v", v)
	}
}

func TestLooksLikeUSDTPerpetual(t *testing.T) {
	if !LooksLikeUSDTPerpetual("BTC/USDT:USDT", nil) {
		t.Fatal("canonical symbol")
	}
	info := map[string]any{"info": map[string]any{"instType": "SWAP"}}
	if !LooksLikeUSDTPerpetual("BTCUSDT", info) {
		t.Fatal("swap info")
	}
	perp := map[string]any{"info": map[string]any{"contractType": "PERPETUAL"}}
	if !LooksLikeUSDTPerpetual("ETH", perp) {
		t.Fatal("perpetual contract")
	}
	if LooksLikeUSDTPerpetual("ETH", map[string]any{}) {
		t.Fatal("no info should be false")
	}
}
