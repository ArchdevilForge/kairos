package exchange

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type hostRewriteTransport struct {
	base *url.URL
}

func (t *hostRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = t.base.Scheme
	req.URL.Host = t.base.Host
	return http.DefaultTransport.RoundTrip(req)
}

func testHTTPClient(srv *httptest.Server) *http.Client {
	u, _ := url.Parse(srv.URL)
	return &http.Client{Transport: &hostRewriteTransport{base: u}, Timeout: newHTTPClient().Timeout}
}

func TestBinanceFetchTickersAndOHLCV_Mock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "ticker/24hr"):
			if r.URL.Query().Get("symbol") != "" {
				_ = json.NewEncoder(w).Encode(map[string]string{
					"symbol": "BTCUSDT", "lastPrice": "65000", "quoteVolume": "1000000", "priceChangePercent": "1.5",
				})
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]string{
				{"symbol": "BTCUSDT", "lastPrice": "65000", "quoteVolume": "1000000", "priceChangePercent": "1.5"},
			})
		case strings.Contains(r.URL.Path, "premiumIndex"):
			_ = json.NewEncoder(w).Encode([]map[string]string{
				{"symbol": "BTCUSDT", "lastFundingRate": "0.0001"},
			})
		case strings.Contains(r.URL.Path, "klines"):
			_, _ = w.Write([]byte(`[[1704067200000,"1","2","0.5","1.5","10"]]`))
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
	if tickers["BTC/USDT:USDT"] == nil || tickers["BTC/USDT:USDT"].FundingRate == nil {
		t.Fatalf("tickers: %+v", tickers["BTC/USDT:USDT"])
	}

	one, err := b.FetchTicker(context.Background(), "BTC/USDT:USDT")
	if err != nil || one.LastPrice == nil {
		t.Fatalf("fetch ticker: %+v %v", one, err)
	}

	candles, err := b.FetchOHLCV(context.Background(), "BTC/USDT", "4h", 10, 0)
	if err != nil || len(candles) != 1 {
		t.Fatalf("ohlcv: %+v %v", candles, err)
	}
}

func TestOKXFetchTickersAndOHLCV_Mock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/market/tickers"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": "0",
				"data": []map[string]string{
					{"instId": "BTC-USDT-SWAP", "last": "65000", "volCcy24h": "5000000", "open24h": "64000"},
				},
			})
		case strings.Contains(r.URL.Path, "open-interest"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": "0",
				"data": []map[string]string{{"instId": "BTC-USDT-SWAP", "oi": "12345"}},
			})
		case strings.Contains(r.URL.Path, "/market/candles"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": "0",
				"data": [][]string{{"1704067200000", "1", "2", "0.5", "1.5", "10"}},
			})
		case strings.Contains(r.URL.Path, "/market/ticker"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": "0",
				"data": []map[string]string{
					{"instId": "BTC-USDT-SWAP", "last": "65000", "volCcy24h": "5000000", "open24h": "64000"},
				},
			})
		case strings.Contains(r.URL.Path, "funding-rate"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": "0",
				"data": []map[string]string{{"fundingRate": "0.0002"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	o := newOKX()
	o.httpCli = testHTTPClient(srv)

	tickers, err := o.FetchTickers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tickers["BTC/USDT:USDT"] == nil || tickers["BTC/USDT:USDT"].OpenInterest == nil {
		t.Fatalf("okx tickers: %+v", tickers["BTC/USDT:USDT"])
	}

	candles, err := o.FetchOHLCV(context.Background(), "BTC/USDT:USDT", "4h", 10, 0)
	if err != nil || len(candles) != 1 {
		t.Fatalf("okx ohlcv: %v %+v", err, candles)
	}

	tk, err := o.FetchTicker(context.Background(), "BTC/USDT:USDT")
	if err != nil || tk.FundingRate == nil {
		t.Fatalf("okx ticker: %+v %v", tk, err)
	}
}

func TestBybitFetchTickersAndOHLCV_Mock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/v5/market/tickers"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"retCode": 0,
				"result": map[string]any{
					"list": []map[string]string{
						{"symbol": "BTCUSDT", "lastPrice": "65000", "turnover24h": "900000", "price24hPcnt": "0.015"},
					},
				},
			})
		case strings.Contains(r.URL.Path, "/v5/market/kline"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"retCode": 0,
				"result": map[string]any{
					"list": [][]string{{"1704067200000", "1", "2", "0.5", "1.5", "10", "0"}},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	b := newBybit()
	b.httpCli = testHTTPClient(srv)

	tickers, err := b.FetchTickers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tickers["BTC/USDT:USDT"] == nil {
		t.Fatalf("bybit tickers: %+v", tickers)
	}

	candles, err := b.FetchOHLCV(context.Background(), "BTC/USDT:USDT", "4h", 10, 0)
	if err != nil || len(candles) != 1 {
		t.Fatalf("bybit ohlcv: %v %+v", err, candles)
	}
}

func TestBinanceClose_Idempotent(t *testing.T) {
	b := newBinance()
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal("second close")
	}
}

func TestMapBinanceTimeframe(t *testing.T) {
	if mapBinanceTimeframe("unknown") != "4h" {
		t.Fatal("default tf")
	}
}
