package data

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestFetchMarketCapMap_LiveGo(t *testing.T) {
	if os.Getenv("KAIROS_LIVE") == "" {
		t.Skip("set KAIROS_LIVE=1 to run")
	}
	m, err := FetchMarketCapMap(context.Background(), 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) < 100 {
		t.Fatalf("expected large market cap map, got %d", len(m))
	}
	if m["BTC"] <= 0 {
		t.Fatalf("missing BTC market cap: %v", m["BTC"])
	}
}
