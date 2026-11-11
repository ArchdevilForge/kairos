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
		// 6-col legacy fixture: QuoteVol stays 0, Volume = row[5].
		{"1704067200000", "1", "2", "0.5", "1.5", "10"},
		// Full 9-col OKX payload: [ts, o, h, l, c, vol, volCcy, volCcyQuote, confirm].
		// row[5]="420" -> Volume, row[6]="4.2" (volCcy) skipped, row[7]="273840.75" -> QuoteVol.
		{"1704067500000", "65000", "65300", "64950", "65200", "420", "4.2", "273840.75", "1"},
		// Malformed short row (<6) ignored.
		{"1704067800000", "1", "2"},
	}
	candles := parseOKXCandleRows(rows)
	if len(candles) != 2 {
		t.Fatalf("want 2 candles, got %d: %+v", len(candles), candles)
	}
	if candles[0].Timestamp != 1704067200 || candles[0].Volume != 10 || candles[0].QuoteVol != 0 {
		t.Fatalf("short row mismatch: %+v", candles[0])
	}
	if candles[1].Timestamp != 1704067500 ||
		candles[1].Volume != 420 ||
		candles[1].QuoteVol != 273840.75 ||
		candles[1].Close != 65200 {
		t.Fatalf("full OKX row[7]->QuoteVol mismatch: %+v", candles[1])
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
	body := []byte(`[
		[1704067200000,"1","2","0.5","1.5","10"],
		[1704067500000,"100","102","99","101","50",1704067799999,"5045.25",120,"25","2520.0","0"]
	]`)
	candles, err := parseBinanceKlines(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(candles) != 2 || candles[0].Close != 1.5 || candles[0].QuoteVol != 0 {
		t.Fatalf("first candle mismatch: %+v", candles)
	}
	if candles[1].Volume != 50 || candles[1].QuoteVol != 5045.25 {
		t.Fatalf("binance row[7] quoteAssetVolume mismatch: %+v", candles[1])
	}
}
