package exchange

import (
	"testing"

	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestSortCandlesAscending(t *testing.T) {
	candles := []types.Candle{
		{Timestamp: 300, Close: 3},
		{Timestamp: 100, Close: 1},
		{Timestamp: 200, Close: 2},
	}
	sortCandlesAscending(candles)
	if candles[0].Timestamp != 100 || candles[2].Timestamp != 300 {
		t.Fatalf("order: %+v", candles)
	}
}

func TestParseOKXCandleRows(t *testing.T) {
	rows := [][]string{
		{"1704067200000", "1", "2", "0.5", "1.5", "10"},
	}
	candles := parseOKXCandleRows(rows)
	if len(candles) != 1 || candles[0].Timestamp != 1704067200 {
		t.Fatalf("got %+v", candles)
	}
}

func TestParseBinanceKlines_ErrorObject(t *testing.T) {
	body := []byte(`{"code":0,"msg":"Service unavailable from a restricted location"}`)
	_, err := parseBinanceKlines(body)
	if err == nil {
		t.Fatal("expected error for binance error payload")
	}
}

func TestParseBinanceKlines_Array(t *testing.T) {
	body := []byte(`[[1704067200000,"1","2","0.5","1.5","10"]]`)
	candles, err := parseBinanceKlines(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(candles) != 1 || candles[0].Close != 1.5 {
		t.Fatalf("got %+v", candles)
	}
}
