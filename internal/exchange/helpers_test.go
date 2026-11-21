package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ArchdevilForge/kairos/internal/data"
	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestParseFloat(t *testing.T) {
	if parseFloat("1.5") != 1.5 {
		t.Fatal("parseFloat 1.5")
	}
	if parseFloat("") != 0 {
		t.Fatal("parseFloat empty")
	}
	if parseFloat("bad") != 0 {
		t.Fatal("parseFloat bad")
	}
}

func TestBinanceSymbol(t *testing.T) {
	if got := binanceSymbol("BTC/USDT:USDT"); got != "BTCUSDT" {
		t.Fatalf("got %q", got)
	}
}

func TestToCanonicalBinance(t *testing.T) {
	if got := toCanonicalBinance("ETHUSDT"); got != "ETH/USDT:USDT" {
		t.Fatalf("got %q", got)
	}
	if got := toCanonicalBinance("INVALID"); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestExchangeNew_Unknown(t *testing.T) {
	_, err := New("unknown")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestExchangeNew_Known(t *testing.T) {
	for _, name := range []string{"okx", "binance", "bybit"} {
		ex, err := New(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if ex.Name() != name {
			t.Fatalf("name: got %q", ex.Name())
		}
		_ = ex.Close()
	}
}

func TestToCanonicalBybit(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"BTCUSDT", "BTC/USDT:USDT"},
		{"btcusdt", "BTC/USDT:USDT"},
		{"1000PEPEUSDT", "1000PEPE/USDT:USDT"},
		{"BTCUSDT-27DEC24", ""},
		{"BTC-27DEC24", ""},
		{"ETH-PERP", ""},
		{"BTCUSDC", ""},
		{"USDT", ""},
		{"BTC/USDT", ""},
	}
	for _, tc := range cases {
		if got := toCanonicalBybit(tc.in); got != tc.want {
			t.Fatalf("toCanonicalBybit(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestChunkBybitSubscribeArgs(t *testing.T) {
	args := make([]string, 25)
	for i := range args {
		args[i] = fmt.Sprintf("tickers.SYM%02dUSDT", i)
	}
	chunks := chunkBybitSubscribeArgs(args, bybitMaxSubscribeArgsPerMsg, bybitMaxSubscribeArgsChars)
	if len(chunks) != 3 || len(chunks[0]) != 10 || len(chunks[1]) != 10 || len(chunks[2]) != 5 {
		t.Fatalf("unexpected count chunks: %v", chunks)
	}

	charChunks := chunkBybitSubscribeArgs([]string{"tickers.AAAAUSDT", "tickers.BBBBUSDT", "tickers.CCCCUSDT"}, 10, 35)
	if len(charChunks) != 3 {
		t.Fatalf("expected 3 char-bounded chunks, got %v", charChunks)
	}
}

func TestBybitFetchOHLCV_LimitClamp(t *testing.T) {
	var limits []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limits = append(limits, r.URL.Query().Get("limit"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"retCode": 0,
			"result": map[string]any{
				"list": [][]string{
					{"1704067500000", "2", "3", "1", "2.5", "20", "50"},
					{"1704067200000", "1", "2", "0.5", "1.5", "10", "15"},
				},
			},
		})
	}))
	defer srv.Close()

	b := newBybitWithEndpoints(srv.URL, "")
	for _, lim := range []int{0, -5, 1500, 500} {
		candles, err := b.FetchOHLCV(context.Background(), "BTC/USDT:USDT", "5m", lim, 0)
		if err != nil {
			t.Fatalf("FetchOHLCV(limit=%d): %v", lim, err)
		}
		if len(candles) != 2 || candles[0].Timestamp != 1704067200 || candles[0].QuoteVol != 15 {
			t.Fatalf("unexpected candles: %+v", candles)
		}
	}
	want := []string{"200", "200", "1000", "500"}
	for i, w := range want {
		if limits[i] != w {
			t.Fatalf("limit[%d] = %q, want %q (all=%v)", i, limits[i], w, limits)
		}
	}
}

func TestBybitFetchTickers_AllowlistAndFallback(t *testing.T) {
	var failInst atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/v5/market/instruments-info"):
			if failInst.Load() {
				http.Error(w, "upstream down", http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"retCode": 0,
				"result": map[string]any{
					"list": []map[string]any{
						{"symbol": "BTCUSDT", "contractType": "LinearPerpetual", "status": "Trading", "quoteCoin": "USDT", "settleCoin": "USDT", "symbolType": ""},
						{"symbol": "1000PEPEUSDT", "contractType": "LinearPerpetual", "status": "Trading", "quoteCoin": "USDT", "settleCoin": "USDT", "symbolType": "innovation"},
						{"symbol": "EURUSDUSDT", "contractType": "LinearPerpetual", "status": "Trading", "quoteCoin": "USDT", "settleCoin": "USDT", "symbolType": "forex"},
						{"symbol": "AAPLUSDT", "contractType": "LinearPerpetual", "status": "Trading", "quoteCoin": "USDT", "settleCoin": "USDT", "symbolType": "stock"},
						{"symbol": "PREUSDT", "contractType": "LinearPerpetual", "status": "Trading", "quoteCoin": "USDT", "settleCoin": "USDT", "isPreListing": true},
					},
				},
			})
		case strings.Contains(r.URL.Path, "/v5/market/tickers"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"retCode": 0,
				"result": map[string]any{
					"list": []map[string]string{
						{"symbol": "BTCUSDT", "lastPrice": "65000", "turnover24h": "100000000", "price24hPcnt": "0.02", "openInterest": "1000", "openInterestValue": "65000000", "fundingRate": "0.0001"},
						{"symbol": "1000PEPEUSDT", "lastPrice": "0.01", "turnover24h": "25000000", "openInterest": "1000000", "openInterestValue": "10000"},
						{"symbol": "EURUSDUSDT", "lastPrice": "1.1", "turnover24h": "50000000"},
						{"symbol": "AAPLUSDT", "lastPrice": "200", "turnover24h": "50000000"},
						{"symbol": "PREUSDT", "lastPrice": "5", "turnover24h": "50000000"},
						{"symbol": "BTC-27DEC24", "lastPrice": "66000", "turnover24h": "50000000"},
						{"symbol": "BTCUSDC", "lastPrice": "65000", "turnover24h": "50000000"},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	b := newBybitWithEndpoints(srv.URL, "")
	tickers, err := b.FetchTickers(context.Background())
	if err != nil {
		t.Fatalf("FetchTickers: %v", err)
	}
	if len(tickers) != 2 || tickers["BTC/USDT:USDT"] == nil || tickers["1000PEPE/USDT:USDT"] == nil {
		t.Fatalf("expected only crypto USDT linear perps, got %+v", tickers)
	}
	if oiVal, ok := tickers["BTC/USDT:USDT"].Info["openInterestValue"].(float64); !ok || oiVal != 65000000 {
		t.Fatalf("expected Info[openInterestValue]=65000000, got %+v", tickers["BTC/USDT:USDT"].Info)
	}

	// Fallback when instruments-info fails: keeps non-dated USDT contracts and skips - / USDC.
	failInst.Store(true)
	fallbackTickers, err := b.FetchTickers(context.Background())
	if err != nil {
		t.Fatalf("FetchTickers fallback: %v", err)
	}
	if fallbackTickers["BTC/USDT:USDT"] == nil || fallbackTickers["1000PEPE/USDT:USDT"] == nil {
		t.Fatalf("fallback missing crypto tickers: %+v", fallbackTickers)
	}
	if fallbackTickers["BTC-27DEC24/USDT:USDT"] != nil || fallbackTickers["BTC/USDC:USDC"] != nil {
		t.Fatalf("fallback must reject dated/USDC contracts: %+v", fallbackTickers)
	}
}

func TestFetchBybitCryptoPerpSymbols_PaginationAndDispatch(t *testing.T) {
	var seenCursors []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/v5/market/instruments-info"):
			rawCursor := r.URL.Query().Get("cursor")
			seenCursors = append(seenCursors, rawCursor)
			if rawCursor == "" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"retCode": 0,
					"result": map[string]any{
						"nextPageCursor": "page%3A2",
						"list": []map[string]any{
							{"symbol": "BTCUSDT", "contractType": "LinearPerpetual", "status": "Trading", "quoteCoin": "USDT", "settleCoin": "USDT", "launchTime": "1584230400000"},
							{"symbol": "XAUUSDT", "contractType": "LinearPerpetual", "status": "Trading", "quoteCoin": "USDT", "settleCoin": "USDT", "symbolType": "commodity"},
							{"symbol": "NVDAXUSDT", "contractType": "LinearPerpetual", "status": "Trading", "quoteCoin": "USDT", "settleCoin": "USDT", "symbolType": "xstocks"},
							{"symbol": "SPYUSDT", "contractType": "LinearPerpetual", "status": "Trading", "quoteCoin": "USDT", "settleCoin": "USDT", "symbolType": "ETF"},
						},
					},
				})
				return
			}
			if rawCursor != "page:2" {
				t.Errorf("expected unescaped cursor page:2 (no double encoding), got %q", rawCursor)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"retCode": 0,
				"result": map[string]any{
					"nextPageCursor": "",
					"list": []map[string]any{
						{"symbol": "ETHUSDT", "contractType": "LinearPerpetual", "status": "Trading", "quoteCoin": "USDT", "settleCoin": "USDT", "launchTime": "1584230400000"},
						{"symbol": "SOLUSDT", "contractType": "LinearPerpetual", "status": "Trading", "quoteCoin": "USDT", "settleCoin": "USDT", "symbolType": "normal"},
						{"symbol": "MSFTUSDT", "contractType": "LinearPerpetual", "status": "Trading", "quoteCoin": "USDT", "settleCoin": "USDT", "symbolType": "mstocks"},
					},
				},
			})
		case strings.Contains(r.URL.Path, "/v5/market/tickers"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"retCode": 0,
				"result": map[string]any{
					"list": []map[string]string{
						{"symbol": "BTCUSDT", "turnover24h": "200000000"},
						{"symbol": "ETHUSDT", "turnover24h": "500000000"},
						{"symbol": "SOLUSDT", "turnover24h": "100000000"},
					},
				},
			})
		case strings.Contains(r.URL.Path, "/fapi/v1/exchangeInfo"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"symbols": []map[string]string{
					{"symbol": "BNBUSDT", "status": "TRADING", "contractType": "PERPETUAL", "quoteAsset": "USDT", "underlyingType": "COIN"},
				},
			})
		case strings.Contains(r.URL.Path, "/fapi/v1/ticker/24hr"):
			_ = json.NewEncoder(w).Encode([]map[string]string{
				{"symbol": "BNBUSDT", "quoteVolume": "90000000"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	rt := testHTTPClient(srv).Transport
	defer SetBybitTestHooks(rt, "")()
	defer SetBinanceTestHooks(rt, "")()

	syms, allow, err := FetchCryptoPerpSymbols(context.Background(), "")
	if err != nil {
		t.Fatalf("FetchCryptoPerpSymbols(''): %v", err)
	}
	if len(seenCursors) != 2 || seenCursors[1] != "page:2" {
		t.Fatalf("expected 2 paginated calls with unescaped cursor, got %v", seenCursors)
	}
	wantOrder := []string{"ETH/USDT:USDT", "BTC/USDT:USDT", "SOL/USDT:USDT"}
	if len(syms) != len(wantOrder) {
		t.Fatalf("got %v, want %v", syms, wantOrder)
	}
	for i, w := range wantOrder {
		if syms[i] != w || !allow[w] {
			t.Fatalf("syms[%d] = %q, want %q", i, syms[i], w)
		}
	}

	// Empty/unknown defaults to Bybit; "binance" dispatches to Binance.
	if bybitSyms, _, err := FetchCryptoPerpSymbols(context.Background(), "unknown"); err != nil || len(bybitSyms) != 3 {
		t.Fatalf("unknown exchange should default to bybit, got %v err=%v", bybitSyms, err)
	}
	binSyms, _, err := FetchCryptoPerpSymbols(context.Background(), "binance")
	if err != nil || len(binSyms) != 1 || binSyms[0] != "BNB/USDT:USDT" {
		t.Fatalf("binance dispatch failed: %v err=%v", binSyms, err)
	}
}

func TestFilterBybitTop100Universe(t *testing.T) {
	nowMs := time.Now().UnixMilli()
	oldLaunch := fmt.Sprintf("%d", nowMs-90*24*3600*1000)
	newLaunch := fmt.Sprintf("%d", nowMs-5*24*3600*1000)

	var spotRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/v5/market/instruments-info") {
			cat := r.URL.Query().Get("category")
			if cat == "spot" {
				spotRawQuery = r.URL.RawQuery
				_ = json.NewEncoder(w).Encode(map[string]any{
					"retCode": 0,
					"result": map[string]any{
						"list": []map[string]string{
							{"symbol": "BTCUSDT", "baseCoin": "BTC", "quoteCoin": "USDT", "status": "Trading"},
							{"symbol": "ETHUSDT", "baseCoin": "ETH", "quoteCoin": "USDT", "status": "Trading"},
							{"symbol": "SOLUSDT", "baseCoin": "SOL", "quoteCoin": "USDT", "status": "Trading"},
							{"symbol": "PEPEUSDT", "baseCoin": "PEPE", "quoteCoin": "USDT", "status": "Trading"},
							{"symbol": "XRPUSDT", "baseCoin": "XRP", "quoteCoin": "USDT", "status": "Trading"},
							{"symbol": "DOGEUSDT", "baseCoin": "DOGE", "quoteCoin": "USDT", "status": "Trading"},
							{"symbol": "CHURNUSDT", "baseCoin": "CHURN", "quoteCoin": "USDT", "status": "Trading"},
							{"symbol": "NEWUSDT", "baseCoin": "NEW", "quoteCoin": "USDT", "status": "Trading"},
						},
					},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"retCode": 0,
				"result": map[string]any{
					"list": []map[string]any{
						{"symbol": "BTCUSDT", "contractType": "LinearPerpetual", "status": "Trading", "quoteCoin": "USDT", "settleCoin": "USDT", "launchTime": oldLaunch},
						{"symbol": "ETHUSDT", "contractType": "LinearPerpetual", "status": "Trading", "quoteCoin": "USDT", "settleCoin": "USDT", "launchTime": oldLaunch},
						{"symbol": "SOLUSDT", "contractType": "LinearPerpetual", "status": "Trading", "quoteCoin": "USDT", "settleCoin": "USDT", "launchTime": oldLaunch},
						{"symbol": "1000PEPEUSDT", "contractType": "LinearPerpetual", "status": "Trading", "quoteCoin": "USDT", "settleCoin": "USDT", "launchTime": oldLaunch},
						{"symbol": "XRPUSDT", "contractType": "LinearPerpetual", "status": "Trading", "quoteCoin": "USDT", "settleCoin": "USDT", "launchTime": oldLaunch},
						{"symbol": "DOGEUSDT", "contractType": "LinearPerpetual", "status": "Trading", "quoteCoin": "USDT", "settleCoin": "USDT", "launchTime": oldLaunch},
						{"symbol": "NOSPOTUSDT", "contractType": "LinearPerpetual", "status": "Trading", "quoteCoin": "USDT", "settleCoin": "USDT", "launchTime": oldLaunch},
						{"symbol": "NEWUSDT", "contractType": "LinearPerpetual", "status": "Trading", "quoteCoin": "USDT", "settleCoin": "USDT", "launchTime": newLaunch},
						{"symbol": "CHURNUSDT", "contractType": "LinearPerpetual", "status": "Trading", "quoteCoin": "USDT", "settleCoin": "USDT", "launchTime": oldLaunch},
					},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	defer SetBybitTestHooks(testHTTPClient(srv).Transport, "")()

	ptr := func(v float64) *float64 { return &v }
	tickers := map[string]*types.Ticker{
		"BTC/USDT:USDT":      {Symbol: "BTC/USDT:USDT", LastPrice: ptr(65000), QuoteVolume: ptr(500_000_000)},
		"ETH/USDT:USDT":      {Symbol: "ETH/USDT:USDT", LastPrice: ptr(3000), QuoteVolume: ptr(300_000_000)},
		"SOL/USDT:USDT":      {Symbol: "SOL/USDT:USDT", LastPrice: ptr(150), QuoteVolume: ptr(200_000_000), Info: map[string]any{"openInterestValue": 100_000_000.0}},
		"1000PEPE/USDT:USDT": {Symbol: "1000PEPE/USDT:USDT", LastPrice: ptr(0.01), QuoteVolume: ptr(80_000_000), OpenInterest: ptr(3_000_000_000)}, // fallback OI*price=30M
		"XRP/USDT:USDT":      {Symbol: "XRP/USDT:USDT", LastPrice: ptr(0.6), QuoteVolume: ptr(12_000_000), Info: map[string]any{"openInterestValue": 12_000_000.0}},
		"DOGE/USDT:USDT":     {Symbol: "DOGE/USDT:USDT", LastPrice: ptr(0.2), QuoteVolume: ptr(60_000_000), Info: map[string]any{"openInterestValue": 40_000_000.0}},
		"NOSPOT/USDT:USDT":   {Symbol: "NOSPOT/USDT:USDT", LastPrice: ptr(2.0), QuoteVolume: ptr(50_000_000), Info: map[string]any{"openInterestValue": 25_000_000.0}},
		"NEW/USDT:USDT":      {Symbol: "NEW/USDT:USDT", LastPrice: ptr(1.0), QuoteVolume: ptr(50_000_000), Info: map[string]any{"openInterestValue": 25_000_000.0}},
		"CHURN/USDT:USDT":    {Symbol: "CHURN/USDT:USDT", LastPrice: ptr(1.0), QuoteVolume: ptr(500_000_000), Info: map[string]any{"openInterestValue": 20_000_000.0}}, // Vol/OI=25 > 20
	}
	mcap := map[string]data.MarketCapEntry{
		"BTC":    {Rank: 1, CapUSD: 1_300_000_000_000},
		"ETH":    {Rank: 2, CapUSD: 350_000_000_000},
		"SOL":    {Rank: 5, CapUSD: 70_000_000_000},
		"XRP":    {Rank: 7, CapUSD: 35_000_000_000},
		"DOGE":   {Rank: 9, CapUSD: 25_000_000_000},
		"PEPE":   {Rank: 25, CapUSD: 4_000_000_000},
		"NOSPOT": {Rank: 30, CapUSD: 2_000_000_000},
		"NEW":    {Rank: 31, CapUSD: 2_000_000_000},
		"CHURN":  {Rank: 32, CapUSD: 2_000_000_000},
	}

	got, ok := FilterBybitTop100Universe(
		context.Background(),
		tickers,
		mcap,
		[]string{"XRP/USDT:USDT"}, // incumbent gets 0.75x threshold (11.25M) so 12M passes
		10,
		UniverseFilterConfig{RequireSpotPair: true},
	)
	if !ok {
		t.Fatal("expected FilterBybitTop100Universe to apply")
	}
	if spotRawQuery != "category=spot" {
		t.Fatalf("spot query must omit limit/cursor, got %q", spotRawQuery)
	}
	if len(got) != 6 || got[0] != "BTC/USDT:USDT" || got[1] != "ETH/USDT:USDT" {
		t.Fatalf("unexpected filtered universe: %v", got)
	}
	gotSet := map[string]bool{}
	for _, s := range got {
		gotSet[s] = true
	}
	for _, want := range []string{"SOL/USDT:USDT", "1000PEPE/USDT:USDT", "XRP/USDT:USDT", "DOGE/USDT:USDT"} {
		if !gotSet[want] {
			t.Fatalf("expected %s in filtered universe %v", want, got)
		}
	}
	for _, reject := range []string{"NOSPOT/USDT:USDT", "NEW/USDT:USDT", "CHURN/USDT:USDT"} {
		if gotSet[reject] {
			t.Fatalf("unexpected %s in filtered universe %v", reject, got)
		}
	}

	// Fewer than 5 passing symbols -> returns (nil, false).
	if small, smallOK := FilterBybitTop100Universe(context.Background(), tickers, mcap, nil, 3, UniverseFilterConfig{}); smallOK || small != nil {
		t.Fatalf("expected (nil, false) when topN < 5, got (%v, %v)", small, smallOK)
	}
	// Verify url package import is exercised if needed:
	if _, err := url.Parse(srv.URL); err != nil {
		t.Fatal(err)
	}
}
