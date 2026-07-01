package utils

import (
	"math"
	"testing"
)

func TestRollingZScore_InsufficientSamples(t *testing.T) {
	z := NewRollingZScore(5)
	if got := z.Add(1.0); got != 0 {
		t.Fatalf("first sample z-score: got %v want 0", got)
	}
}

func TestRollingZScore_KnownWindow(t *testing.T) {
	z := NewRollingZScore(4)
	for _, v := range []float64{10, 11, 9, 10} {
		z.Add(v)
	}
	score := z.Add(20)
	if score <= 0 {
		t.Fatalf("expected positive z-score for spike, got %v", score)
	}
}

func TestRollingZScore_Reset(t *testing.T) {
	z := NewRollingZScore(3)
	z.Add(1)
	z.Add(2)
	z.Reset()
	if got := z.Add(5); got != 0 {
		t.Fatalf("after reset first z-score should be 0, got %v", got)
	}
}

func TestNormalizeSymbol(t *testing.T) {
	cases := []struct {
		in, want string
		wantErr  bool
	}{
		{"BTC/USDT:USDT", "BTC/USDT:USDT", false},
		{"BTC/USDT", "BTC/USDT:USDT", false},
		{"btcusdt", "BTC/USDT:USDT", false},
		{"", "", true},
		{"BTC/USD", "", true},
	}
	for _, tc := range cases {
		got, err := NormalizeSymbol(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("%q: expected error", tc.in)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("%q: got (%q, %v)", tc.in, got, err)
		}
	}
}

func TestNormalizeCoinSymbol(t *testing.T) {
	if got := NormalizeCoinSymbol("ETH/USDT:USDT"); got != "ETH" {
		t.Fatalf("got %q", got)
	}
	if got := NormalizeCoinSymbol("BTC-USDT-SWAP"); got != "BTC" {
		t.Fatalf("got %q", got)
	}
}

func TestFirstFloat(t *testing.T) {
	m := map[string]any{"quoteVolume": "123.45", "x": 1}
	v := FirstFloat(m, []string{"missing", "quoteVolume"})
	if v == nil || math.Abs(*v-123.45) > 1e-9 {
		t.Fatalf("got %v", v)
	}
}

func TestExtractQuoteVolume(t *testing.T) {
	ticker := map[string]any{"quoteVolume": 1_000_000.0}
	if got := ExtractQuoteVolume(ticker); got != 1_000_000 {
		t.Fatalf("got %v", got)
	}
}

func TestBlacklist_IsBlocked(t *testing.T) {
	b := &Blacklist{blocked: map[string]struct{}{"BTC/USDT:USDT": {}}}
	if !b.IsBlocked("btc/usdt:usdt") {
		t.Fatal("expected blocked")
	}
	if b.IsBlocked("ETH/USDT:USDT") {
		t.Fatal("expected not blocked")
	}
}
