package data

import (
	"os"
	"testing"
	"time"
)

func TestFetchSpotRSIMap_LiveGo(t *testing.T) {
	if os.Getenv("KAIROS_LIVE") == "" {
		t.Skip("set KAIROS_LIVE=1 to run")
	}
	m, err := FetchSpotRSIMap(15 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) < 100 {
		t.Fatalf("expected large rsi map, got %d", len(m))
	}
	if m["BTC"].RSI4h <= 0 {
		t.Fatalf("missing BTC rsi4h: %+v", m["BTC"])
	}
}
