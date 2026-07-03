package data

import (
	"os"
	"testing"
	"time"
)

func TestParseMarketCapRankMap(t *testing.T) {
	payload := []any{
		map[string]any{"code": "BTC", "marketCap": "1700000000000"},
		map[string]any{"code": "DOGE", "marketCap": 25000000000.0},
	}
	m, err := ParseMarketCapRankMap(payload)
	if err != nil {
		t.Fatal(err)
	}
	if m["BTC"] != 1_700_000_000_000 {
		t.Fatalf("BTC: %v", m["BTC"])
	}
	if m["DOGE"] != 25_000_000_000 {
		t.Fatalf("DOGE: %v", m["DOGE"])
	}
}

func TestFetchMarketCapMap_Live(t *testing.T) {
	if os.Getenv("KAIROS_LIVE_COINGLASS") == "" {
		t.Skip("set KAIROS_LIVE_COINGLASS=1 to probe live CoinGlass")
	}
	m, err := FetchMarketCapMap(20 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if m["BTC"] <= 0 {
		t.Fatalf("missing BTC cap: %v", m["BTC"])
	}
	t.Logf("BTC cap=%.0f entries=%d", m["BTC"], len(m))
}
