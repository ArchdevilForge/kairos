package data

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

type marketCapRewriteTransport struct {
	base *url.URL
	orig http.RoundTripper
}

func (t *marketCapRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = t.base.Scheme
	clone.URL.Host = t.base.Host
	return t.orig.RoundTrip(clone)
}

func withMockDefaultTransport(t *testing.T, srv *httptest.Server) {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	orig := http.DefaultTransport
	http.DefaultTransport = &marketCapRewriteTransport{base: u, orig: orig}
	t.Cleanup(func() {
		http.DefaultTransport = orig
	})
}

func TestCanonicalMarketCapBase(t *testing.T) {
	cases := map[string]string{
		"":                     "",
		"   ":                  "",
		"BTC/USDT:USDT":        "BTC",
		"1000PEPE/USDT:USDT":   "PEPE",
		"1000000MOGUSDT":       "MOG",
		"1MBABYDOGE/USDT:USDT": "BABYDOGE",
		"eth-usdt":             "ETH",
		"sol_usdt":             "SOL",
		"10004":                "10004",
	}
	for in, want := range cases {
		if got := CanonicalMarketCapBase(in); got != want {
			t.Fatalf("CanonicalMarketCapBase(%q) = %q, want %q", in, got, want)
		}
	}
}

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

func TestParseMarketCapRankEntries_FiltersStablecoinsAndSorts(t *testing.T) {
	payload := map[string]any{
		"list": []any{
			map[string]any{"code": "USDT", "marketCap": 140_000_000_000.0},
			map[string]any{"code": "USDC", "marketCap": 60_000_000_000.0},
			map[string]any{"code": "BTC", "market_cap_usd": 1_500_000_000_000.0},
			map[string]any{"symbol": "BTC", "market_cap": int64(1_800_000_000_000)},
			map[string]any{"code": "ETH", "marketCap": float32(400_000_000_000)},
			map[string]any{"code": "SOL", "marketCap": int(100_000_000_000)},
			map[string]any{"code": "AVAX", "marketCap": "100000000000"},
			map[string]any{"code": "BAD", "marketCap": nil},
		},
	}
	entries, err := ParseMarketCapRankEntries(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := entries["USDT"]; ok {
		t.Fatal("USDT should be excluded")
	}
	if _, ok := entries["USDC"]; ok {
		t.Fatal("USDC should be excluded")
	}
	if entries["BTC"].Rank != 1 || entries["BTC"].CapUSD != 1_800_000_000_000 {
		t.Fatalf("BTC entry: %+v", entries["BTC"])
	}
	if entries["ETH"].Rank != 2 {
		t.Fatalf("ETH rank: %+v", entries["ETH"])
	}
	if entries["AVAX"].Rank != 3 || entries["SOL"].Rank != 4 {
		t.Fatalf("tie-break rank expected AVAX=3 SOL=4, got AVAX=%d SOL=%d", entries["AVAX"].Rank, entries["SOL"].Rank)
	}

	if _, err := ParseMarketCapRankEntries(map[string]any{"other": 1}); err == nil {
		t.Fatal("expected missing list error")
	}
	if _, err := ParseMarketCapRankEntries("bad"); err == nil {
		t.Fatal("expected unexpected payload type error")
	}
	if _, err := ParseMarketCapRankEntries([]any{map[string]any{"code": "USDT", "marketCap": 100}}); err == nil {
		t.Fatal("expected empty list error when only stablecoins exist")
	}
}

func TestFetchMarketCapRankEntries_CoinGeckoPrimary(t *testing.T) {
	t.Setenv(envCoinGlassUsePy, "0")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/api/v3/coins/markets") {
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"symbol": "usdt", "market_cap": 140_000_000_000},
				{"symbol": "btc", "market_cap": 1_700_000_000_000},
				{"symbol": "btc", "market_cap": 1_900_000_000_000},
				{"symbol": "eth", "market_cap": 400_000_000_000},
				{"symbol": "1000pepe", "market_cap": 8_000_000_000},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	withMockDefaultTransport(t, srv)

	entries, err := FetchMarketCapRankEntries(context.Background(), 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if entries["BTC"].Rank != 1 || entries["BTC"].CapUSD != 1_900_000_000_000 {
		t.Fatalf("BTC = %+v", entries["BTC"])
	}
	if entries["ETH"].Rank != 2 || entries["PEPE"].Rank != 3 {
		t.Fatalf("entries = %+v", entries)
	}
	if _, ok := entries["USDT"]; ok {
		t.Fatal("stablecoin USDT must be excluded")
	}
}

func TestFetchMarketCapRankEntries_CoinGlassFallback(t *testing.T) {
	t.Setenv(envCoinGlassUsePy, "0")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/api/v3/coins/markets"):
			http.Error(w, "rate limited", http.StatusTooManyRequests)
		case strings.Contains(r.URL.Path, "/api/marketCapRank"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": "0",
				"data": []map[string]any{
					{"code": "USDC", "marketCap": 60_000_000_000},
					{"code": "BTC", "marketCap": "1750000000000"},
					{"code": "ETH", "marketCap": 380_000_000_000},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	withMockDefaultTransport(t, srv)

	entries, err := FetchMarketCapRankEntries(context.Background(), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if entries["BTC"].Rank != 1 || entries["ETH"].Rank != 2 {
		t.Fatalf("fallback entries = %+v", entries)
	}

	m, err := FetchMarketCapMap(context.Background(), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if m["BTC"] != 1_750_000_000_000 {
		t.Fatalf("FetchMarketCapMap BTC = %v", m["BTC"])
	}
}

func TestFetchMarketCapRankEntries_FailurePaths(t *testing.T) {
	t.Setenv(envCoinGlassUsePy, "0")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	withMockDefaultTransport(t, srv)

	if _, err := FetchMarketCapRankEntries(context.Background(), 0); err == nil {
		t.Fatal("expected error when both CoinGecko and CoinGlass fail")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := FetchMarketCapRankEntries(ctx, time.Second); err == nil {
		t.Fatal("expected cancelled error")
	}
}

func TestFetchMarketCapMap_Live(t *testing.T) {
	if os.Getenv("KAIROS_LIVE_COINGLASS") == "" {
		t.Skip("set KAIROS_LIVE_COINGLASS=1 to probe live CoinGlass")
	}
	m, err := FetchMarketCapMap(context.Background(), 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if m["BTC"] <= 0 {
		t.Fatalf("missing BTC cap: %v", m["BTC"])
	}
	t.Logf("BTC cap=%.0f entries=%d", m["BTC"], len(m))
}
