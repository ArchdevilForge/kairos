package data

import "testing"

func TestParseSpotRSIMap_List(t *testing.T) {
	payload := map[string]any{
		"list": []any{
			map[string]any{"symbol": "BTC", "rsi15m": "72.1", "rsi1h": 68.0, "rsi4h": 75.5},
			map[string]any{"symbol": "ETH", "rsi4h": 28.0},
		},
	}
	got, err := ParseSpotRSIMap(payload)
	if err != nil {
		t.Fatal(err)
	}
	if got["BTC"].RSI4h != 75.5 || got["ETH"].RSI4h != 28.0 {
		t.Fatalf("got %+v", got)
	}
}

func TestParseSpotRSIMap_Empty(t *testing.T) {
	_, err := ParseSpotRSIMap(map[string]any{"list": []any{}})
	if err == nil {
		t.Fatal("expected error for empty list")
	}
}

func TestRSIHotnessScore(t *testing.T) {
	if got := RSIHotnessScore(75, 1.0); got <= 0 {
		t.Fatalf("expected hot score, got %v", got)
	}
	if got := RSIHotnessScore(50, 1.0); got != 0 {
		t.Fatalf("neutral rsi should score 0, got %v", got)
	}
	if got := RSIHotnessScore(0, 1.0); got != 0 {
		t.Fatalf("missing rsi should score 0, got %v", got)
	}
}
