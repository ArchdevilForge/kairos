package exchange

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The shared FetchOHLCV contract: beforeMs is an exclusive backward cursor.
// Each adapter must translate it to its native query parameter.

func TestBinanceOHLCVCursor_UsesExclusiveEndTime(t *testing.T) {
	var gotQuery map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "klines") {
			gotQuery = r.URL.Query()
			_, _ = w.Write([]byte(`[[1704067200000,"1","2","0.5","1.5","10"]]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	b := newBinance()
	b.httpCli = testHTTPClient(srv)
	if _, err := b.FetchOHLCV(context.Background(), "BTC/USDT:USDT", "4h", 10, 1704153600000); err != nil {
		t.Fatal(err)
	}
	if got := gotQuery["endTime"]; len(got) != 1 || got[0] != "1704153599999" {
		t.Fatalf("binance must send exclusive endTime=beforeMs-1, got %v", gotQuery)
	}
	if len(gotQuery["startTime"]) != 0 {
		t.Fatalf("binance must not send startTime for backward pagination, got %v", gotQuery)
	}
}

func TestBybitOHLCVCursor_UsesExclusiveEnd(t *testing.T) {
	var gotQuery map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "kline") {
			gotQuery = r.URL.Query()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"retCode": 0,
				"result":  map[string]any{"list": [][]string{{"1704067200000", "1", "2", "0.5", "1.5", "10"}}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	b := newBybit()
	b.httpCli = testHTTPClient(srv)
	if _, err := b.FetchOHLCV(context.Background(), "BTC/USDT:USDT", "4h", 10, 1704153600000); err != nil {
		t.Fatal(err)
	}
	if got := gotQuery["end"]; len(got) != 1 || got[0] != "1704153599999" {
		t.Fatalf("bybit must send exclusive end=beforeMs-1, got %v", gotQuery)
	}
	if len(gotQuery["start"]) != 0 {
		t.Fatalf("bybit must not send start for backward pagination, got %v", gotQuery)
	}
}

func TestOKXOHLCVCursor_UsesAfterOnHistoryPath(t *testing.T) {
	var gotPath string
	var gotQuery map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "candles") {
			gotPath = r.URL.Path
			gotQuery = r.URL.Query()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": "0",
				"data": [][]string{{"1704067200000", "1", "2", "0.5", "1.5", "10"}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	o := newOKX()
	o.httpCli = testHTTPClient(srv)
	if _, err := o.FetchOHLCV(context.Background(), "BTC/USDT:USDT", "4h", 10, 1704153600000); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotPath, "history-candles") {
		t.Fatalf("okx must use history-candles for cursor pagination, got %s", gotPath)
	}
	if got := gotQuery["after"]; len(got) != 1 || got[0] != "1704153600000" {
		t.Fatalf("okx must send after=beforeMs (natively exclusive), got %v", gotQuery)
	}
}

// Real zero values must survive as present optional metrics.

func TestBinanceTickers_PreserveZeroChangeAndFunding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "ticker/24hr"):
			_ = json.NewEncoder(w).Encode([]map[string]string{
				{"symbol": "BTCUSDT", "lastPrice": "65000", "quoteVolume": "1000000", "priceChangePercent": "0.000"},
			})
		case strings.Contains(r.URL.Path, "premiumIndex"):
			_ = json.NewEncoder(w).Encode([]map[string]string{
				{"symbol": "BTCUSDT", "lastFundingRate": "0"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	b := newBinance()
	b.httpCli = testHTTPClient(srv)
	tickers, err := b.FetchTickers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	tk := tickers["BTC/USDT:USDT"]
	if tk == nil {
		t.Fatal("missing ticker")
	}
	if tk.ChangePct == nil || *tk.ChangePct != 0 {
		t.Fatalf("flat 0%% change must be present, got %v", tk.ChangePct)
	}
	if tk.FundingRate == nil || *tk.FundingRate != 0 {
		t.Fatalf("real 0 funding must be present, got %v", tk.FundingRate)
	}
}

func TestBybitTickers_PreserveZeroOptionalMetrics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"retCode": 0,
			"result": map[string]any{"list": []map[string]string{{
				"symbol": "BTCUSDT", "lastPrice": "65000", "turnover24h": "1000000",
				"price24hPcnt": "0", "fundingRate": "0", "openInterest": "0",
			}}},
		})
	}))
	defer srv.Close()

	b := newBybit()
	b.httpCli = testHTTPClient(srv)
	tickers, err := b.FetchTickers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	tk := tickers["BTC/USDT:USDT"]
	if tk == nil {
		t.Fatal("missing ticker")
	}
	if tk.ChangePct == nil || tk.FundingRate == nil || tk.OpenInterest == nil {
		t.Fatalf("zero optional metrics must stay present: %+v", tk)
	}
	if tk.FundingRate != nil && *tk.FundingRate != 0 {
		t.Fatalf("funding: %v", *tk.FundingRate)
	}
}

func TestTickers_AbsentOptionalMetricsStayNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"retCode": 0,
			"result": map[string]any{"list": []map[string]string{{
				"symbol": "BTCUSDT", "lastPrice": "65000", "turnover24h": "1000000",
			}}},
		})
	}))
	defer srv.Close()

	b := newBybit()
	b.httpCli = testHTTPClient(srv)
	tickers, err := b.FetchTickers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	tk := tickers["BTC/USDT:USDT"]
	if tk == nil {
		t.Fatal("missing ticker")
	}
	if tk.ChangePct != nil || tk.FundingRate != nil || tk.OpenInterest != nil {
		t.Fatalf("absent metrics must stay nil: %+v", tk)
	}
}
