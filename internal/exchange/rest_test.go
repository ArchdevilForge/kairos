package exchange

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ArchdevilForge/kairos/internal/cache"
	"github.com/ArchdevilForge/kairos/internal/data"
	"github.com/ArchdevilForge/kairos/internal/decision"
	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
	_ "modernc.org/sqlite"
)

type hostRewriteTransport struct {
	base *url.URL
}

func (t *hostRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = t.base.Scheme
	clone.URL.Host = t.base.Host
	return http.DefaultTransport.RoundTrip(clone)
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

	candles, err := b.FetchOHLCV(context.Background(), "BTC/USDT", "4h", 10, 0)
	if err != nil || len(candles) != 1 {
		t.Fatalf("ohlcv: %+v %v", candles, err)
	}
}

func TestOKXFetchTickersAndOHLCV_Mock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/public/instruments"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": "0",
				"data": []map[string]string{
					{"instId": "BTC-USDT-SWAP", "instCategory": "1", "state": "live", "settleCcy": "USDT"},
					{"instId": "AAPL-USDT-SWAP", "instCategory": "3", "state": "live", "settleCcy": "USDT"},
					{"instId": "XAU-USDT-SWAP", "instCategory": "4", "state": "live", "settleCcy": "USDT"},
				},
			})
		case strings.Contains(r.URL.Path, "/market/tickers"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": "0",
				"data": []map[string]string{
					{"instId": "BTC-USDT-SWAP", "last": "65000", "volCcy24h": "5000000", "open24h": "64000"},
					{"instId": "AAPL-USDT-SWAP", "last": "200", "volCcy24h": "999999999", "open24h": "190"},
					{"instId": "XAU-USDT-SWAP", "last": "2000", "volCcy24h": "999999999", "open24h": "1900"},
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
				"data": [][]string{{"1704067200000", "1", "2", "0.5", "1.5", "10", "1.0", "15234.5", "1"}},
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
	if _, ok := tickers["AAPL/USDT:USDT"]; ok {
		t.Fatal("stock perpetual should be filtered out")
	}
	if _, ok := tickers["XAU/USDT:USDT"]; ok {
		t.Fatal("commodity perpetual should be filtered out")
	}
	wantVol := 65000.0 * 5_000_000
	if got := *tickers["BTC/USDT:USDT"].QuoteVolume; got != wantVol {
		t.Fatalf("quote volume USDT: got %v want %v", got, wantVol)
	}

	candles, err := o.FetchOHLCV(context.Background(), "BTC/USDT:USDT", "4h", 10, 0)
	if err != nil || len(candles) != 1 || candles[0].Volume != 10 || candles[0].QuoteVol != 15234.5 {
		t.Fatalf("okx ohlcv: %v %+v", err, candles)
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
					"list": [][]string{{"1704067200000", "1", "2", "0.5", "1.5", "10", "15.25"}},
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
	if err != nil || len(candles) != 1 || candles[0].Volume != 10 || candles[0].QuoteVol != 15.25 {
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

func TestFetchBinanceCryptoPerpSymbols_Mock(t *testing.T) {
	var infoCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/fapi/v1/exchangeInfo"):
			infoCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"symbols": []map[string]any{
					{"symbol": "BTCUSDT", "status": "TRADING", "contractType": "PERPETUAL", "quoteAsset": "USDT", "underlyingType": "COIN"},
					{"symbol": "ETHUSDT", "status": "TRADING", "contractType": "PERPETUAL", "quoteAsset": "USDT", "underlyingType": "COIN"},
					{"symbol": "SOLUSDT", "status": "TRADING", "contractType": "PERPETUAL", "quoteAsset": "USDT", "underlyingType": ""},
					{"symbol": "AAPLUSDT", "status": "TRADING", "contractType": "PERPETUAL", "quoteAsset": "USDT", "underlyingType": "EQUITY"},
					{"symbol": "OLDUSDT", "status": "BREAK", "contractType": "PERPETUAL", "quoteAsset": "USDT", "underlyingType": "COIN"},
				},
			})
		case strings.Contains(r.URL.Path, "/fapi/v1/ticker/24hr"):
			_ = json.NewEncoder(w).Encode([]map[string]string{
				{"symbol": "ETHUSDT", "quoteVolume": "800000000"},
				{"symbol": "BTCUSDT", "quoteVolume": "1500000000"},
				{"symbol": "SOLUSDT", "quoteVolume": "300000000"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cleanup := SetBinanceTestHooks(testHTTPClient(srv).Transport, "")
	defer cleanup()

	syms, allow, err := FetchBinanceCryptoPerpSymbols(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(syms) != 3 || syms[0] != "BTC/USDT:USDT" || syms[1] != "ETH/USDT:USDT" || syms[2] != "SOL/USDT:USDT" {
		t.Fatalf("unexpected sorted symbols: %v", syms)
	}
	if !allow["BTC/USDT:USDT"] || allow["AAPL/USDT:USDT"] || allow["OLD/USDT:USDT"] {
		t.Fatalf("unexpected allowlist: %+v", allow)
	}

	// Second call must hit the 1h in-memory cache without another exchangeInfo call.
	syms2, _, err := FetchBinanceCryptoPerpSymbols(context.Background())
	if err != nil || len(syms2) != 3 {
		t.Fatalf("cached fetch: %v %v", syms2, err)
	}
	if infoCalls.Load() != 1 {
		t.Fatalf("expected 1 exchangeInfo call (cached), got %d", infoCalls.Load())
	}
}

func TestFilterBinanceTop100Universe_Mock(t *testing.T) {
	oldOnboard := time.Now().Add(-90 * 24 * time.Hour).UnixMilli()
	newOnboard := time.Now().Add(-5 * 24 * time.Hour).UnixMilli()
	var exchangeInfoCalls, oiCalls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/fapi/v1/exchangeInfo"):
			exchangeInfoCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"symbols": []map[string]any{
					{"symbol": "BTCUSDT", "status": "TRADING", "contractType": "PERPETUAL", "quoteAsset": "USDT", "underlyingType": "COIN", "onboardDate": oldOnboard},
					{"symbol": "ETHUSDT", "status": "TRADING", "contractType": "PERPETUAL", "quoteAsset": "USDT", "underlyingType": "COIN", "onboardDate": oldOnboard},
					{"symbol": "SOLUSDT", "status": "TRADING", "contractType": "PERPETUAL", "quoteAsset": "USDT", "underlyingType": "COIN", "onboardDate": oldOnboard},
					{"symbol": "BNBUSDT", "status": "TRADING", "contractType": "PERPETUAL", "quoteAsset": "USDT", "underlyingType": "COIN", "onboardDate": oldOnboard},
					{"symbol": "XRPUSDT", "status": "TRADING", "contractType": "PERPETUAL", "quoteAsset": "USDT", "underlyingType": "COIN", "onboardDate": oldOnboard},
					{"symbol": "DOGEUSDT", "status": "TRADING", "contractType": "PERPETUAL", "quoteAsset": "USDT", "underlyingType": "COIN", "onboardDate": oldOnboard},
					{"symbol": "NEWCOINUSDT", "status": "TRADING", "contractType": "PERPETUAL", "quoteAsset": "USDT", "underlyingType": "COIN", "onboardDate": newOnboard},
					{"symbol": "NOSPOTUSDT", "status": "TRADING", "contractType": "PERPETUAL", "quoteAsset": "USDT", "underlyingType": "COIN", "onboardDate": oldOnboard},
					{"symbol": "CHURNUSDT", "status": "TRADING", "contractType": "PERPETUAL", "quoteAsset": "USDT", "underlyingType": "COIN", "onboardDate": oldOnboard},
					{"symbol": "HIGHMCAPUSDT", "status": "TRADING", "contractType": "PERPETUAL", "quoteAsset": "USDT", "underlyingType": "COIN", "onboardDate": oldOnboard},
					{"symbol": "LOWRANKUSDT", "status": "TRADING", "contractType": "PERPETUAL", "quoteAsset": "USDT", "underlyingType": "COIN", "onboardDate": oldOnboard},
					{"symbol": "INCUMBENTUSDT", "status": "TRADING", "contractType": "PERPETUAL", "quoteAsset": "USDT", "underlyingType": "COIN", "onboardDate": oldOnboard},
				},
			})
		case strings.Contains(r.URL.Path, "/api/v3/exchangeInfo"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"symbols": []map[string]any{
					{"symbol": "BTCUSDT", "status": "TRADING", "baseAsset": "BTC", "quoteAsset": "USDT"},
					{"symbol": "ETHUSDT", "status": "TRADING", "baseAsset": "ETH", "quoteAsset": "USDT"},
					{"symbol": "SOLUSDT", "status": "TRADING", "baseAsset": "SOL", "quoteAsset": "USDT"},
					{"symbol": "BNBUSDT", "status": "TRADING", "baseAsset": "BNB", "quoteAsset": "USDT"},
					{"symbol": "XRPUSDT", "status": "TRADING", "baseAsset": "XRP", "quoteAsset": "USDT"},
					{"symbol": "DOGEUSDT", "status": "TRADING", "baseAsset": "DOGE", "quoteAsset": "USDT"},
					{"symbol": "CHURNUSDT", "status": "TRADING", "baseAsset": "CHURN", "quoteAsset": "USDT"},
					{"symbol": "HIGHMCAPUSDT", "status": "TRADING", "baseAsset": "HIGHMCAP", "quoteAsset": "USDT"},
					{"symbol": "INCUMBENTUSDT", "status": "TRADING", "baseAsset": "INCUMBENT", "quoteAsset": "USDT"},
				},
			})
		case strings.Contains(r.URL.Path, "/fapi/v1/openInterest"):
			oiCalls.Add(1)
			sym := r.URL.Query().Get("symbol")
			oi := "500000"
			switch sym {
			case "CHURNUSDT":
				oi = "20000" // 20k * 100 = $2M < MinOIUSD
			case "HIGHMCAPUSDT":
				oi = "5000000" // $500M > 0.5 * $600M MCap
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"symbol": sym, "openInterest": oi})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cleanup := SetBinanceTestHooks(testHTTPClient(srv).Transport, "")
	defer cleanup()

	mkTicker := func(sym string, px, qv float64) *types.Ticker {
		p, v := px, qv
		return &types.Ticker{Symbol: sym, LastPrice: &p, QuoteVolume: &v}
	}
	tickers := map[string]*types.Ticker{
		"BTC/USDT:USDT":       mkTicker("BTC/USDT:USDT", 65000, 1_500_000_000),
		"ETH/USDT:USDT":       mkTicker("ETH/USDT:USDT", 3500, 900_000_000),
		"SOL/USDT:USDT":       mkTicker("SOL/USDT:USDT", 100, 300_000_000),
		"BNB/USDT:USDT":       mkTicker("BNB/USDT:USDT", 100, 200_000_000),
		"XRP/USDT:USDT":       mkTicker("XRP/USDT:USDT", 100, 180_000_000),
		"DOGE/USDT:USDT":      mkTicker("DOGE/USDT:USDT", 100, 160_000_000),
		"NEWCOIN/USDT:USDT":   mkTicker("NEWCOIN/USDT:USDT", 100, 150_000_000),
		"NOSPOT/USDT:USDT":    mkTicker("NOSPOT/USDT:USDT", 100, 150_000_000),
		"CHURN/USDT:USDT":     mkTicker("CHURN/USDT:USDT", 100, 150_000_000),
		"HIGHMCAP/USDT:USDT":  mkTicker("HIGHMCAP/USDT:USDT", 100, 150_000_000),
		"LOWRANK/USDT:USDT":   mkTicker("LOWRANK/USDT:USDT", 100, 150_000_000),
		"INCUMBENT/USDT:USDT": mkTicker("INCUMBENT/USDT:USDT", 100, 12_000_000),
	}
	mcap := map[string]data.MarketCapEntry{
		"BTC":       {CapUSD: 1_800_000_000_000, Rank: 1},
		"ETH":       {CapUSD: 400_000_000_000, Rank: 2},
		"SOL":       {CapUSD: 80_000_000_000, Rank: 5},
		"BNB":       {CapUSD: 90_000_000_000, Rank: 4},
		"XRP":       {CapUSD: 120_000_000_000, Rank: 3},
		"DOGE":      {CapUSD: 25_000_000_000, Rank: 8},
		"NEWCOIN":   {CapUSD: 5_000_000_000, Rank: 30},
		"NOSPOT":    {CapUSD: 5_000_000_000, Rank: 31},
		"CHURN":     {CapUSD: 5_000_000_000, Rank: 32},
		"HIGHMCAP":  {CapUSD: 600_000_000, Rank: 50},
		"LOWRANK":   {CapUSD: 100_000_000, Rank: 150},
		"INCUMBENT": {CapUSD: 2_000_000_000, Rank: 45},
	}

	got, ok := FilterBinanceTop100Universe(
		context.Background(),
		tickers,
		mcap,
		[]string{"INCUMBENT/USDT:USDT"},
		10,
		UniverseFilterConfig{RequireSpotPair: true},
	)
	if !ok {
		t.Fatal("expected universe filter to succeed")
	}
	if len(got) != 7 {
		t.Fatalf("expected 7 filtered symbols, got %d: %v", len(got), got)
	}
	if got[0] != "BTC/USDT:USDT" || got[1] != "ETH/USDT:USDT" {
		t.Fatalf("expected BTC, ETH first, got %v", got)
	}
	joined := strings.Join(got, ",")
	for _, bad := range []string{"NEWCOIN", "NOSPOT", "CHURN", "HIGHMCAP", "LOWRANK"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("unexpected symbol %s in %v", bad, got)
		}
	}
	if !strings.Contains(joined, "INCUMBENT/USDT:USDT") {
		t.Fatalf("expected incumbent to pass relaxed threshold, got %v", got)
	}

	// Second call reuses both perp meta cache and 30m OI cache.
	oiBefore := oiCalls.Load()
	got2, ok2 := FilterBinanceTop100Universe(context.Background(), tickers, mcap, nil, 6, UniverseFilterConfig{RequireSpotPair: true})
	if !ok2 || len(got2) != 6 {
		t.Fatalf("second filter call: ok=%v got=%v", ok2, got2)
	}
	if exchangeInfoCalls.Load() != 1 || oiCalls.Load() != oiBefore {
		t.Fatalf("expected cached meta & OI, info=%d oi_delta=%d", exchangeInfoCalls.Load(), oiCalls.Load()-oiBefore)
	}

	// Fewer than 5 passing symbols triggers safe fallback (nil, false).
	if res, ok := FilterBinanceTop100Universe(context.Background(), tickers, mcap, nil, 3, UniverseFilterConfig{}); ok || res != nil {
		t.Fatalf("expected fallback for topN=3, got %v %v", res, ok)
	}
	if res, ok := FilterBinanceTop100Universe(context.Background(), nil, mcap, nil, 10, UniverseFilterConfig{}); ok || res != nil {
		t.Fatalf("expected fallback for empty tickers")
	}
}

func TestBinanceReadonlyKeys_SaveLoadMask(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(envBinanceReadonlyKey, "")
	t.Setenv(envBinanceReadonlySecret, "")

	p, err := SecretsFilePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("OTHER_KEY='keep_me'\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := SaveBinanceReadonlyKeys("dummy_api_key_123456", "dummy_api_sec_abcdef"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 perms, got %o", info.Mode().Perm())
	}
	raw, err := os.ReadFile(p)
	if err != nil || !strings.Contains(string(raw), "OTHER_KEY=keep_me") {
		t.Fatalf("expected unrelated key preserved, got %q err=%v", string(raw), err)
	}

	k, s := LoadBinanceReadonlyKeys()
	if k != "dummy_api_key_123456" || s != "dummy_api_sec_abcdef" {
		t.Fatalf("loaded keys mismatch: %q %q", k, s)
	}

	t.Setenv(envBinanceReadonlyKey, "env_override_key_9999")
	k2, _ := LoadBinanceReadonlyKeys()
	if k2 != "env_override_key_9999" {
		t.Fatalf("expected env override, got %q", k2)
	}

	if MaskAPIKey("") != "" || MaskAPIKey("short") != "****" || MaskAPIKey("dummy_api_key_123456") != "dumm****3456" {
		t.Fatalf("unexpected MaskAPIKey results")
	}
}

func TestBinanceAccountClient_SyncToCache_Mock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(envBinanceReadonlyKey, "dummy_acct_key_0001")
	t.Setenv(envBinanceReadonlySecret, "dummy_acct_sec_0001")

	var tradesCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/fapi/v1/time"):
			_ = json.NewEncoder(w).Encode(map[string]int64{"serverTime": time.Now().UnixMilli()})
		case strings.Contains(r.URL.Path, "/fapi/v2/account"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"totalWalletBalance":         "100.0",
				"totalUnrealizedProfit":      "2.5",
				"totalMarginBalance":         "102.5",
				"availableBalance":           "80.0",
				"totalPositionInitialMargin": "20.0",
				"assets": []map[string]string{
					{"asset": "USDT", "walletBalance": "105.0", "unrealizedProfit": "3.25"},
				},
			})
		case strings.Contains(r.URL.Path, "/fapi/v2/positionRisk"):
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"symbol":           "BTCUSDT",
					"positionAmt":      "0.01",
					"entryPrice":       "65000",
					"markPrice":        "65325",
					"unRealizedProfit": "3.25",
					"liquidationPrice": "59000",
					"leverage":         "10",
					"marginType":       "isolated",
					"positionSide":     "BOTH",
					"isolatedWallet":   "65.0",
					"updateTime":       time.Now().UnixMilli(),
				},
				{
					"symbol":      "ETHUSDT",
					"positionAmt": "0",
				},
			})
		case strings.Contains(r.URL.Path, "/fapi/v1/income"):
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"symbol":     "BTCUSDT",
					"incomeType": "FUNDING_FEE",
					"income":     "-0.15",
					"asset":      "USDT",
					"time":       time.Now().Add(-30 * time.Minute).UnixMilli(),
					"tranId":     10001,
				},
			})
		case strings.Contains(r.URL.Path, "/fapi/v1/userTrades"):
			callN := tradesCalls.Add(1)
			if callN == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"code": -1021, "msg": "Timestamp outside recvWindow"})
				return
			}
			if r.URL.Query().Get("fromId") != "" {
				_ = json.NewEncoder(w).Encode([]map[string]any{})
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"id":              5001,
					"orderId":         9001,
					"symbol":          "BTCUSDT",
					"side":            "BUY",
					"positionSide":    "BOTH",
					"price":           "65000",
					"qty":             "0.01",
					"quoteQty":        "650",
					"commission":      "0.001",
					"commissionAsset": "BNB",
					"realizedPnl":     "0",
					"time":            time.Now().Add(-time.Hour).UnixMilli(),
				},
			})
		case strings.Contains(r.URL.Path, "/fapi/v1/ticker/price"):
			_ = json.NewEncoder(w).Encode(map[string]string{"symbol": "BNBUSDT", "price": "600"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cleanup := SetBinanceTestHooks(testHTTPClient(srv).Transport, "")
	defer cleanup()

	tmp := t.TempDir()
	store, err := cache.Open(filepath.Join(tmp, "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	journal, err := storage.NewJournal(types.StorageConfig{DatabasePath: filepath.Join(tmp, "kairos.db")})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	_ = journal.SaveTicket(types.DecisionTicket{
		SchemaVersion: types.DecisionTicketSchemaVersion,
		ID:            "tkt-btc-1",
		Symbol:        "BTC/USDT:USDT",
		Direction:     types.CycleDirectionUp,
		Status:        types.TicketStatusAccepted,
		CreatedAt:     now - 7200,
		SignalAt:      now - 7200,
		RiskPlan:      types.RiskPlan{EntryPrice: 65000, StopPrice: 64000},
	})

	cli := NewBinanceAccountClient()
	if !cli.HasKeys() || cli.APIKeyMasked() != "dumm****0001" {
		t.Fatalf("unexpected key state: has=%v masked=%q", cli.HasKeys(), cli.APIKeyMasked())
	}
	cli.SetKeys("dummy_acct_key_0001", "dummy_acct_sec_0001")
	cli.ReloadKeys()

	out, err := cli.SyncToCache(context.Background(), store, journal, []string{"BTC/USDT:USDT"})
	if err != nil {
		t.Fatal(err)
	}
	if out.SyncedTrades != 1 || out.SyncedFills != 1 || out.OpenPositions != 1 || out.WalletBalanceUSD != 105.0 {
		t.Fatalf("unexpected sync outcome: %+v", out)
	}

	// Incremental follow-up sync exercises maxIDBySym -> fromId path.
	out2, err := cli.SyncToCache(context.Background(), store, journal, nil)
	if err != nil || out2.SyncedFills != 1 {
		t.Fatalf("incremental sync: out=%+v err=%v", out2, err)
	}
}

func TestAggregateBinanceTrades_And_Windows(t *testing.T) {
	baseMs := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC).UnixMilli()
	rawFills := []cache.TradeFill{
		{FillID: "BTC/USDT:USDT:1", Symbol: "BTC/USDT:USDT", Side: "BUY", PositionSide: "BOTH", Price: 100, Qty: 2, Commission: 0.01, CommissionAsset: "BNB", TimeMs: baseMs},
		{FillID: "BTC/USDT:USDT:2", Symbol: "BTC/USDT:USDT", Side: "BUY", PositionSide: "BOTH", Price: 110, Qty: 1, Commission: 0.2, CommissionAsset: "USDT", TimeMs: baseMs + 1000},
		// Flip 3 long -> 1 short.
		{FillID: "BTC/USDT:USDT:3", Symbol: "BTC/USDT:USDT", Side: "SELL", PositionSide: "BOTH", Price: 120, Qty: 4, Commission: 0.4, CommissionAsset: "USDT", RealizedPnL: 50, TimeMs: baseMs + 60_000},
		// Close short.
		{FillID: "BTC/USDT:USDT:4", Symbol: "BTC/USDT:USDT", Side: "BUY", PositionSide: "BOTH", Price: 115, Qty: 1, Commission: 0.1, CommissionAsset: "USDT", RealizedPnL: 5, TimeMs: baseMs + 120_000},
		// Hedge mode SHORT open still open.
		{FillID: "ETH/USDT:USDT:5", Symbol: "ETH/USDT:USDT", Side: "SELL", PositionSide: "SHORT", Price: 3000, Qty: 1, Commission: 0.5, CommissionAsset: "USDT", TimeMs: baseMs + 200_000},
	}
	incomes := []cache.IncomeRecord{
		{TranID: "inc-1", Symbol: "BTC/USDT:USDT", IncomeType: "FUNDING_FEE", Income: -0.5, Asset: "USDT", TimeMs: baseMs + 30_000},
	}
	posRisks := []BinancePositionRisk{
		{Symbol: "BTCUSDT", PositionSide: "BOTH", Leverage: "10", MarginType: "isolated"},
		{Symbol: "ETHUSDT", PositionSide: "SHORT", Leverage: "5", MarginType: "cross", PositionAmt: "-1", LiquidationPrice: "3600"},
	}
	tickets := []types.DecisionTicket{
		{
			ID:        "tkt-iso-long",
			Symbol:    "BTC/USDT:USDT",
			Direction: types.CycleDirectionUp,
			Status:    types.TicketStatusClosed,
			CreatedAt: baseMs / 1000,
			RiskPlan:  types.RiskPlan{EntryPrice: 100, StopPrice: 95},
		},
	}
	maxLoss := 15.0
	decisions := map[string]types.DecisionRecord{
		"tkt-iso-long": {
			TicketID:          "tkt-iso-long",
			Decision:          types.DecisionAccepted,
			DecidedAt:         baseMs / 1000,
			ExitReason:        types.ExitReasonTargetPartial,
			PlannedMaxLossUSD: &maxLoss,
		},
	}

	trades, linkedFills := AggregateBinanceTrades(rawFills, incomes, posRisks, tickets, decisions, 500.0)
	if len(trades) != 3 || len(linkedFills) != 6 {
		t.Fatalf("expected 3 trades and 6 linked fills (with flip split), got trades=%d fills=%d", len(trades), len(linkedFills))
	}

	var matchedClosed *cache.SyncedTrade
	for i := range trades {
		if trades[i].LinkedTicketID == "tkt-iso-long" {
			matchedClosed = &trades[i]
		}
	}
	if matchedClosed == nil || matchedClosed.FundingUSD != -0.5 || matchedClosed.GateCode == "" {
		t.Fatalf("expected matched closed trade with funding & gate code, got %+v", matchedClosed)
	}
	if _, lf := AggregateBinanceTrades(nil, nil, nil, nil, nil, 0); lf != nil {
		t.Fatal("expected nil for empty fills")
	}

	// buildCovering7DayWindows: > 8 distinct 7-day spans capped to 8, plus open window.
	var ts []int64
	for i := 0; i < 10; i++ {
		ts = append(ts, baseMs+int64(i)*8*24*3600*1000)
	}
	wins := buildCovering7DayWindows(ts, true, baseMs+90*24*3600*1000)
	if len(wins) != 8 {
		t.Fatalf("expected 8 capped windows, got %d", len(wins))
	}
	emptyWins := buildCovering7DayWindows(nil, false, baseMs)
	if len(emptyWins) != 1 {
		t.Fatalf("expected 1 default window for empty timestamps, got %d", len(emptyWins))
	}

	_ = decision.MarginModeIsolated
	_ = strconv.Itoa(len(trades))
}

func verifyBybitRequestSignature(t *testing.T, r *http.Request, apiKey, apiSecret string) {
	t.Helper()
	if r.Method != http.MethodGet {
		t.Fatalf("Bybit client MUST send only GET requests, got %s %s", r.Method, r.URL.Path)
	}
	gotKey := r.Header.Get("X-BAPI-API-KEY")
	gotTS := r.Header.Get("X-BAPI-TIMESTAMP")
	gotRecv := r.Header.Get("X-BAPI-RECV-WINDOW")
	gotType := r.Header.Get("X-BAPI-SIGN-TYPE")
	gotSign := r.Header.Get("X-BAPI-SIGN")

	if gotKey != apiKey || gotType != "2" || gotRecv == "" || gotTS == "" {
		t.Fatalf("missing required V5 headers: key=%q ts=%q recv=%q type=%q", gotKey, gotTS, gotRecv, gotType)
	}
	prehash := gotTS + gotKey + gotRecv + r.URL.RawQuery
	mac := hmac.New(sha256.New, []byte(apiSecret))
	_, _ = mac.Write([]byte(prehash))
	wantSign := hex.EncodeToString(mac.Sum(nil))
	if gotSign != wantSign {
		t.Fatalf("signature mismatch for %s?%s: got=%s want=%s (prehash=%q)", r.URL.Path, r.URL.RawQuery, gotSign, wantSign, prehash)
	}
}

func TestAccountClientFactory_And_BybitReadonlyKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(envBinanceReadonlyKey, "")
	t.Setenv(envBinanceReadonlySecret, "")
	t.Setenv(envBybitReadonlyKey, "")
	t.Setenv(envBybitReadonlySecret, "")

	if err := SaveBinanceReadonlyKeys("bnc_key_11112222", "bnc_sec_33334444"); err != nil {
		t.Fatal(err)
	}
	if err := SaveBybitReadonlyKeys("byb_key_55556666", "byb_sec_77778888"); err != nil {
		t.Fatal(err)
	}

	bk, bs := LoadBinanceReadonlyKeys()
	yk, ys := LoadBybitReadonlyKeys()
	if bk != "bnc_key_11112222" || bs != "bnc_sec_33334444" {
		t.Fatalf("binance keys altered after saving bybit keys: %q %q", bk, bs)
	}
	if yk != "byb_key_55556666" || ys != "byb_sec_77778888" {
		t.Fatalf("bybit keys mismatch: %q %q", yk, ys)
	}

	p, err := SecretsFilePath()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("expected secrets.env 0600, got mode=%v err=%v", info.Mode().Perm(), err)
	}

	cDefault := NewAccountClient("")
	cBybit := NewAccountClient("bybit")
	cBinance := NewAccountClient("binance")
	if cDefault.Exchange() != "bybit" || cBybit.Exchange() != "bybit" || cBinance.Exchange() != "binance" {
		t.Fatalf("unexpected factory exchanges: default=%q bybit=%q binance=%q", cDefault.Exchange(), cBybit.Exchange(), cBinance.Exchange())
	}

	// Ensure credentials are never serialized in JSON.
	rawJSON, err := json.Marshal(cBybit)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rawJSON), "byb_key") || strings.Contains(string(rawJSON), "byb_sec") {
		t.Fatalf("credentials leaked in json.Marshal: %s", string(rawJSON))
	}
}

func TestBybitAccountClient_SigningTimeSyncCursorAndErrors(t *testing.T) {
	const (
		apiKey    = "byb_test_key_0001"
		apiSecret = "byb_test_sec_0001"
	)
	var timeSyncCalled atomic.Int32
	var infoCalls atomic.Int32
	var posPages atomic.Int32

	// Server time shifted +12s to force 10002 timestamp retry on initial call.
	serverNowMs := time.Now().Add(12 * time.Second).UnixMilli()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v5/market/time" {
			timeSyncCalled.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"retCode": 0,
				"retMsg":  "OK",
				"time":    serverNowMs,
				"result": map[string]string{
					"timeSecond": strconv.FormatInt(serverNowMs/1000, 10),
					"timeNano":   strconv.FormatInt(serverNowMs*1_000_000, 10),
				},
			})
			return
		}

		verifyBybitRequestSignature(t, r, apiKey, apiSecret)

		switch r.URL.Path {
		case "/v5/account/info":
			n := infoCalls.Add(1)
			if n == 1 {
				// Bybit returns HTTP 200 with retCode 10002 on clock drift.
				_ = json.NewEncoder(w).Encode(map[string]any{
					"retCode": 10002,
					"retMsg":  "invalid request, please check your server timestamp or recv_window param",
					"result":  map[string]any{},
				})
				return
			}
			tsGot, _ := strconv.ParseInt(r.Header.Get("X-BAPI-TIMESTAMP"), 10, 64)
			if math.Abs(float64(tsGot-serverNowMs)) > 3000 {
				t.Fatalf("expected timestamp synced near serverNowMs=%d, got=%d", serverNowMs, tsGot)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"retCode": 0,
				"retMsg":  "OK",
				"result": map[string]any{
					"unifiedMarginStatus": 5,
					"marginMode":          "ISOLATED_MARGIN",
				},
			})
		case "/v5/position/list":
			p := posPages.Add(1)
			if p == 1 {
				if r.URL.Query().Get("cursor") != "" {
					t.Fatalf("expected empty cursor on page 1, got %q", r.URL.Query().Get("cursor"))
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"retCode": 0,
					"result": map[string]any{
						// Includes both percent-encoded colons/commas AND base64-style '+' (%2B).
						"nextPageCursor": "132766%3A2%2Cabc%2Bdef%3D",
						"list": []map[string]any{
							{
								"symbol":          "BTCUSDT",
								"side":            "Buy",
								"size":            "0.25",
								"avgPrice":        "65000",
								"markPrice":       "66000",
								"unrealisedPnl":   "250",
								"liqPrice":        "59000",
								"leverage":        "10",
								"tradeMode":       0,
								"positionIM":      "1625",
								"positionBalance": "1600",
								"positionIdx":     0,
								"updatedTime":     "1700000100000",
							},
						},
					},
				})
				return
			}
			// Verify cursor was NOT double-encoded (%253A) and '+' was preserved (not turned into space!).
			if strings.Contains(r.URL.RawQuery, "%253A") || strings.Contains(r.URL.RawQuery, "abc+def") {
				t.Fatalf("cursor double-encoded or space-corrupted in RawQuery: %s", r.URL.RawQuery)
			}
			if gotCur := r.URL.Query().Get("cursor"); gotCur != "132766:2,abc+def=" {
				t.Fatalf("unexpected decoded cursor on page 2: got=%q want=%q (raw=%s)", gotCur, "132766:2,abc+def=", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"retCode": 0,
				"result": map[string]any{
					"nextPageCursor": "",
					"list": []map[string]any{
						{
							"symbol":          "ETHUSDT",
							"side":            "Sell",
							"size":            "2.0",
							"avgPrice":        "3000",
							"markPrice":       "2950",
							"unrealisedPnl":   "100",
							"liqPrice":        "3300",
							"leverage":        "5",
							"tradeMode":       0,
							"positionIM":      "",
							"positionBalance": "1200",
							"positionIdx":     2,
							"updatedTime":     "1700000200000",
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cli := NewBybitAccountClient()
	cli.SetKeys(apiKey, apiSecret)
	cli.baseURL = srv.URL

	ctx := context.Background()
	info, err := cli.GetAccountInfo(ctx)
	if err != nil {
		t.Fatalf("GetAccountInfo after 10002 retry failed: %v", err)
	}
	if timeSyncCalled.Load() != 1 {
		t.Fatalf("expected 1 /v5/market/time call, got %d", timeSyncCalled.Load())
	}
	if !info.IsUTA() || info.ResolvedMarginMode() != decision.MarginModeIsolated {
		t.Fatalf("unexpected account info: %+v", info)
	}

	positions, err := cli.GetPositions(ctx)
	if err != nil {
		t.Fatalf("GetPositions failed: %v", err)
	}
	if len(positions) != 2 {
		t.Fatalf("expected 2 paginated positions, got %d", len(positions))
	}
	if positions[0].PositionSide != "BOTH" || positions[0].Qty != 0.25 || positions[0].IsolatedWallet != 1625 {
		t.Fatalf("unexpected BTC position: %+v", positions[0])
	}
	if positions[1].PositionSide != "SHORT" || positions[1].Qty != -2.0 || positions[1].IsolatedWallet != 1200 {
		t.Fatalf("unexpected ETH hedge short position: %+v", positions[1])
	}

	// Verify actionable error formatting for 10003, 10004, 10005, 10006.
	for _, tc := range []struct {
		code    int
		wantSub string
	}{
		{10003, "invalid API key"},
		{10004, "signature verification failed"},
		{10005, "permission denied"},
		{10006, "rate limit exceeded"},
	} {
		errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Bapi-Limit-Reset-Timestamp", "1700000999000")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"retCode": tc.code,
				"retMsg":  "mock error",
			})
		}))
		errCli := NewBybitAccountClient()
		errCli.SetKeys(apiKey, apiSecret)
		errCli.baseURL = errSrv.URL
		_, gotErr := errCli.GetWalletBalance(ctx)
		errSrv.Close()
		if gotErr == nil || !strings.Contains(gotErr.Error(), tc.wantSub) {
			t.Fatalf("code %d expected substring %q, got %v", tc.code, tc.wantSub, gotErr)
		}
	}
}

func TestBybitAccountClient_ClassicFallback(t *testing.T) {
	const (
		apiKey    = "byb_classic_key_1"
		apiSecret = "byb_classic_sec_1"
	)
	nowMs := time.Now().UnixMilli()
	var sawContractWallet atomic.Bool
	var sawFundingExecList atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		verifyBybitRequestSignature(t, r, apiKey, apiSecret)
		switch r.URL.Path {
		case "/v5/account/info":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"retCode": 0,
				"result": map[string]any{
					"unifiedMarginStatus": 1,
					"marginMode":          "",
				},
			})
		case "/v5/account/wallet-balance":
			acctType := r.URL.Query().Get("accountType")
			if acctType == "UNIFIED" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"retCode": 10001,
					"retMsg":  "accountType only supports CONTRACT",
				})
				return
			}
			if acctType == "CONTRACT" {
				sawContractWallet.Store(true)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"retCode": 0,
				"result": map[string]any{
					"list": []map[string]any{
						{
							"accountType": "CONTRACT",
							"coin": []map[string]string{
								{"coin": "USDT", "walletBalance": "420.5", "unrealisedPnl": "-10.25", "availableToWithdraw": "300"},
							},
						},
					},
				},
			})
		case "/v5/position/list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"retCode": 0,
				"result": map[string]any{
					"list": []map[string]any{
						{
							"symbol":        "BTCUSDT",
							"side":          "Buy",
							"size":          "0.1",
							"avgPrice":      "60000",
							"markPrice":     "59900",
							"unrealisedPnl": "-10.25",
							"liqPrice":      "54000",
							"leverage":      "10",
							"tradeMode":     1, // Classic isolated
							"positionIM":    "600",
							"positionIdx":   0,
							"updatedTime":   strconv.FormatInt(nowMs, 10),
						},
					},
				},
			})
		case "/v5/execution/list":
			if r.URL.Query().Get("execType") == "Funding" {
				sawFundingExecList.Store(true)
				endMs, _ := strconv.ParseInt(r.URL.Query().Get("endTime"), 10, 64)
				if endMs >= nowMs-1000 {
					_ = json.NewEncoder(w).Encode(map[string]any{
						"retCode": 0,
						"result": map[string]any{
							"list": []map[string]any{
								{
									"execId":      "fund-classic-1",
									"symbol":      "BTCUSDT",
									"execFee":     "0.35", // paid 0.35 -> Income = -0.35
									"feeCurrency": "USDT",
									"execType":    "Funding",
									"execTime":    strconv.FormatInt(nowMs-60_000, 10),
								},
							},
						},
					})
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"retCode": 0, "result": map[string]any{"list": []any{}}})
				return
			}
			endMs, _ := strconv.ParseInt(r.URL.Query().Get("endTime"), 10, 64)
			if endMs >= nowMs-1000 {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"retCode": 0,
					"result": map[string]any{
						"list": []map[string]any{
							{
								"execId":      "exec-classic-open-1",
								"orderId":     "ord-classic-1",
								"symbol":      "BTCUSDT",
								"side":        "Buy",
								"execPrice":   "60000",
								"execQty":     "0.1",
								"execValue":   "6000",
								"execFee":     "1.2",
								"feeCurrency": "USDT",
								"execPnl":     "0",
								"closedSize":  "0",
								"execType":    "Trade",
								"execTime":    strconv.FormatInt(nowMs-120_000, 10),
							},
						},
					},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"retCode": 0, "result": map[string]any{"list": []any{}}})
		case "/v5/position/closed-pnl":
			_ = json.NewEncoder(w).Encode(map[string]any{"retCode": 0, "result": map[string]any{"list": []any{}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	store, err := cache.Open(filepath.Join(t.TempDir(), "classic.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	cli := NewBybitAccountClient()
	cli.SetKeys(apiKey, apiSecret)
	cli.baseURL = srv.URL

	// Standalone GetWalletBalance triggers 10001 -> CONTRACT fallback too.
	wb, err := cli.GetWalletBalance(context.Background())
	if err != nil || !sawContractWallet.Load() || wb.WalletBalanceUSD != 420.5 || wb.UnrealizedPnLUSD != -10.25 {
		t.Fatalf("GetWalletBalance classic fallback failed: wb=%+v err=%v", wb, err)
	}

	out, err := cli.SyncToCache(context.Background(), store, nil, nil)
	if err != nil {
		t.Fatalf("Classic SyncToCache failed: %v", err)
	}
	if !sawFundingExecList.Load() {
		t.Fatal("expected Classic SyncToCache to query execution/list?execType=Funding")
	}
	if out.OpenPositions != 1 || out.SyncedTrades != 1 || out.WalletBalanceUSD != 420.5 {
		t.Fatalf("unexpected Classic sync outcome: %+v", out)
	}
	pos, err := store.ListOpenPositionsForExchange(context.Background(), "bybit")
	if err != nil || len(pos) != 1 || pos[0].MarginMode != decision.MarginModeIsolated {
		t.Fatalf("expected Classic tradeMode=1 -> isolated, got %+v err=%v", pos, err)
	}
	trades, err := store.ListSyncedTradesForExchange(context.Background(), "bybit", "")
	if err != nil || len(trades) != 1 || math.Abs(trades[0].FundingUSD-(-0.35)) > 1e-9 {
		t.Fatalf("expected funding -0.35 (sign-flipped from execFee=0.35), got %+v err=%v", trades, err)
	}
}

func TestBybitAccountClient_SyncToCache_EndToEnd_And_Coexistence(t *testing.T) {
	const (
		apiKey    = "byb_uta_key_0001"
		apiSecret = "byb_uta_sec_0001"
	)

	// 1. Seed a legacy Binance desktop-cache.db with OLD schema (pre-migration).
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "desktop-cache.db")
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	legacyStmts := []string{
		`CREATE TABLE klines (
			symbol TEXT NOT NULL, tf TEXT NOT NULL, time_sec INTEGER NOT NULL,
			open REAL NOT NULL, high REAL NOT NULL, low REAL NOT NULL, close REAL NOT NULL,
			volume REAL NOT NULL, quote_vol REAL NOT NULL DEFAULT 0, source TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (symbol, tf, time_sec)
		);`,
		`CREATE TABLE trade_fills (
			fill_id TEXT PRIMARY KEY, order_id TEXT NOT NULL, symbol TEXT NOT NULL,
			side TEXT NOT NULL, position_side TEXT NOT NULL, price REAL NOT NULL,
			qty REAL NOT NULL, quote_qty REAL NOT NULL, commission REAL NOT NULL,
			commission_asset TEXT NOT NULL, realized_pnl REAL NOT NULL,
			time_ms INTEGER NOT NULL, trade_id TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE synced_trades (
			id TEXT PRIMARY KEY, symbol TEXT NOT NULL, direction TEXT NOT NULL,
			position_side TEXT NOT NULL, margin_mode TEXT NOT NULL, leverage REAL NOT NULL DEFAULT 1,
			entry_price REAL NOT NULL, exit_price REAL NOT NULL, qty REAL NOT NULL,
			entry_time_ms INTEGER NOT NULL, exit_time_ms INTEGER NOT NULL,
			realized_pnl REAL NOT NULL, fee_usd REAL NOT NULL, funding_usd REAL NOT NULL,
			net_pnl REAL NOT NULL, margin REAL NOT NULL, liq_price REAL NOT NULL DEFAULT 0,
			status TEXT NOT NULL, linked_ticket_id TEXT NOT NULL DEFAULT '', gate_code TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE open_positions (
			symbol TEXT NOT NULL, position_side TEXT NOT NULL, margin_mode TEXT NOT NULL,
			leverage REAL NOT NULL, qty REAL NOT NULL, entry_price REAL NOT NULL,
			mark_price REAL NOT NULL, unrealized_pnl REAL NOT NULL, liq_price REAL NOT NULL,
			isolated_wallet REAL NOT NULL, updated_at INTEGER NOT NULL,
			PRIMARY KEY (symbol, position_side)
		);`,
		`CREATE TABLE account_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);`,
		`CREATE TABLE account_income (
			tran_id TEXT PRIMARY KEY, symbol TEXT NOT NULL, income_type TEXT NOT NULL,
			income REAL NOT NULL, asset TEXT NOT NULL, time_ms INTEGER NOT NULL
		);`,
		`INSERT INTO klines (symbol, tf, time_sec, open, high, low, close, volume, quote_vol, source)
		 VALUES ('BTC/USDT:USDT', '5m', 1700000000, 50000, 50100, 49900, 50050, 10, 500000, '');`,
		`INSERT INTO trade_fills VALUES ('BTC/USDT:USDT:99', '9901', 'BTC/USDT:USDT', 'BUY', 'BOTH', 50000, 0.1, 5000, 2.0, 'USDT', 0, 1700000000000, 'trd-btcusdt-both-1700000000000-99');`,
		`INSERT INTO synced_trades VALUES ('trd-btcusdt-both-1700000000000-99', 'BTC/USDT:USDT', 'long', 'BOTH', 'isolated', 10, 50000, 0, 0.1, 1700000000000, 0, 0, 2.0, 0, -2.0, 500, 45000, 'open', '', 'GATE_NO_VALID_TICKET');`,
		`INSERT INTO open_positions VALUES ('BTC/USDT:USDT', 'BOTH', 'isolated', 10, 0.1, 50000, 50100, 10, 45000, 500, 1700000000);`,
		`INSERT INTO account_income VALUES ('999_FUNDING_FEE_1700000100000', 'BTC/USDT:USDT', 'FUNDING_FEE', -0.25, 'USDT', 1700000100000);`,
		`INSERT INTO account_meta VALUES ('wallet_balance_usd', '888.5'), ('unrealized_pnl_usd', '10.0'), ('last_sync_at', '1700000000'), ('sync_error', ''), ('setting_bankroll_usd', '2500');`,
	}
	for _, stmt := range legacyStmts {
		if _, err := rawDB.Exec(stmt); err != nil {
			t.Fatalf("seed legacy sqlite %q: %v", stmt, err)
		}
	}
	_ = rawDB.Close()

	// Open with cache.Open -> must migrate non-destructively AND be idempotent on reopen!
	store, err := cache.Open(dbPath)
	if err != nil {
		t.Fatalf("cache.Open migration failed: %v", err)
	}
	_ = store.Close()
	store, err = cache.Open(dbPath)
	if err != nil {
		t.Fatalf("cache.Open idempotent reopen failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()

	// Verify klines migrated '' -> 'binance' and coexists with 'bybit' at same timestamp.
	if err := store.UpsertKline(ctx, "BTC/USDT:USDT", "5m", "bybit", types.Candle{
		Timestamp: 1700000000, Open: 50010, High: 50120, Low: 49910, Close: 50080, Volume: 12, QuoteVol: 600000,
	}); err != nil {
		t.Fatalf("UpsertKline bybit at same timestamp failed: %v", err)
	}
	bncKlines, err := store.GetKlinesForSource(ctx, "BTC/USDT:USDT", "5m", "binance", 10, 0)
	if err != nil || len(bncKlines) != 1 || bncKlines[0].Close != 50050 {
		t.Fatalf("expected migrated binance kline, got %+v err=%v", bncKlines, err)
	}
	bybKlines, err := store.GetKlinesForSource(ctx, "BTC/USDT:USDT", "5m", "bybit", 10, 0)
	if err != nil || len(bybKlines) != 1 || bybKlines[0].Close != 50080 {
		t.Fatalf("expected bybit kline, got %+v err=%v", bybKlines, err)
	}
	if _, err := store.GetKlinesUpToForSource(ctx, "BTC/USDT:USDT", "5m", "bybit", 1700000000, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetKlinesRangeForSource(ctx, "BTC/USDT:USDT", "5m", "binance", 1699999900, 1700000100); err != nil {
		t.Fatal(err)
	}
	if syms, err := store.ListCachedSymbolsForSource(ctx, "bybit"); err != nil || len(syms) != 1 {
		t.Fatalf("ListCachedSymbolsForSource: %v err=%v", syms, err)
	}

	// Verify seeded binance:* meta and legacy fallback.
	if v, err := store.GetExchangeMeta(ctx, "binance", "wallet_balance_usd"); err != nil || v != "888.5" {
		t.Fatalf("expected seeded binance wallet_balance_usd=888.5, got %q err=%v", v, err)
	}
	if setting, err := store.GetAccountMeta(ctx, "setting_bankroll_usd"); err != nil || setting != "2500" {
		t.Fatalf("expected global setting preserved, got %q err=%v", setting, err)
	}

	// 2. Prepare DecisionJournal with an accepted ticket matching the Bybit BTC trade.
	now := time.Now()
	nowMs := now.UnixMilli()
	entryMs := nowMs - 2*3600*1000
	fundingMs := nowMs - 3600*1000
	exitMs := nowMs - 10*60*1000

	journal, err := storage.NewJournal(types.StorageConfig{DatabasePath: filepath.Join(tmp, "kairos.db")})
	if err != nil {
		t.Fatal(err)
	}
	maxLoss := 20.0
	if err := journal.SaveTicket(types.DecisionTicket{
		SchemaVersion: types.DecisionTicketSchemaVersion,
		ID:            "tkt-bybit-btc-1",
		Symbol:        "BTC/USDT:USDT",
		Direction:     types.CycleDirectionUp,
		Status:        types.TicketStatusClosed,
		ExitReason:    types.ExitReasonTargetPartial,
		CreatedAt:     entryMs / 1000,
		SignalAt:      entryMs / 1000,
		RiskPlan:      types.RiskPlan{EntryPrice: 65000, StopPrice: 64500},
	}); err != nil {
		t.Fatal(err)
	}
	if err := journal.SaveDecision(types.DecisionRecord{
		TicketID:          "tkt-bybit-btc-1",
		Decision:          types.DecisionAccepted,
		DecidedAt:         entryMs / 1000,
		ExitReason:        types.ExitReasonTargetPartial,
		PlannedMaxLossUSD: &maxLoss,
	}); err != nil {
		t.Fatal(err)
	}

	// 3. Mock Bybit V5 UTA account endpoints.
	var execChunks atomic.Int32
	var txLogChunks atomic.Int32
	var seenDatedContract atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		verifyBybitRequestSignature(t, r, apiKey, apiSecret)

		switch r.URL.Path {
		case "/v5/account/info":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"retCode": 0,
				"result": map[string]any{
					"unifiedMarginStatus": 4,
					"marginMode":          "ISOLATED_MARGIN",
				},
			})
		case "/v5/account/wallet-balance":
			if r.URL.Query().Get("accountType") != "UNIFIED" {
				t.Fatalf("expected UNIFIED wallet-balance query, got %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"retCode": 0,
				"result": map[string]any{
					"list": []map[string]any{
						{
							"accountType":        "UNIFIED",
							"totalWalletBalance": "1500.0",
							"totalPerpUPL":       "12.0",
							"coin": []map[string]string{
								{"coin": "BTC", "walletBalance": "0.01"},
								{"coin": "USDT", "walletBalance": "1234.56", "unrealisedPnl": "18.75"},
							},
						},
					},
				},
			})
		case "/v5/position/list":
			sym := r.URL.Query().Get("symbol")
			if sym == "ETHUSDT" {
				// Zero-size hedge mode metadata row for historical ETH fills!
				_ = json.NewEncoder(w).Encode(map[string]any{
					"retCode": 0,
					"result": map[string]any{
						"list": []map[string]any{
							{"symbol": "ETHUSDT", "size": "0", "positionIdx": 1, "tradeMode": 0, "leverage": "5"},
							{"symbol": "ETHUSDT", "size": "0", "positionIdx": 2, "tradeMode": 0, "leverage": "5"},
						},
					},
				})
				return
			}
			// settleCoin=USDT returns open One-Way BTCUSDT (with tradeMode=0 despite UTA ISOLATED_MARGIN!).
			_ = json.NewEncoder(w).Encode(map[string]any{
				"retCode": 0,
				"result": map[string]any{
					"list": []map[string]any{
						{
							"symbol":        "BTCUSDT",
							"side":          "Buy",
							"size":          "0.05",
							"avgPrice":      "66000",
							"markPrice":     "66375",
							"unrealisedPnl": "18.75",
							"liqPrice":      "60000",
							"leverage":      "10",
							"tradeMode":     0, // deprecated on UTA; must resolve to "isolated" via account/info!
							"positionIM":    "330",
							"positionIdx":   0,
							"updatedTime":   strconv.FormatInt(nowMs, 10),
						},
					},
				},
			})
		case "/v5/execution/list":
			execChunks.Add(1)
			st, _ := strconv.ParseInt(r.URL.Query().Get("startTime"), 10, 64)
			et, _ := strconv.ParseInt(r.URL.Query().Get("endTime"), 10, 64)
			if st <= 0 || et <= 0 || et-st > 7*24*3600*1000 {
				t.Fatalf("invalid execution/list 7-day chunk window: start=%d end=%d diff=%d", st, et, et-st)
			}
			if et < nowMs-1000 {
				_ = json.NewEncoder(w).Encode(map[string]any{"retCode": 0, "result": map[string]any{"list": []any{}}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"retCode": 0,
				"result": map[string]any{
					"list": []map[string]any{
						// 1. BTC One-Way trade: open 0.1 long @ 65000 (maker rebate -0.25!), closed with 0.1 sell @ 66000 (+100 gross execPnl, fee +0.50).
						{
							"execId": "byb-exec-1", "orderId": "ord-btc-open", "symbol": "BTCUSDT",
							"side": "Buy", "execPrice": "65000", "execQty": "0.1", "execValue": "6500",
							"execFee": "-0.25", "feeCurrency": "USDT", "execPnl": "0", "closedSize": "0",
							"execType": "Trade", "execTime": strconv.FormatInt(entryMs, 10),
						},
						// Funding row in execution/list MUST be excluded!
						{
							"execId": "byb-exec-funding-ignore", "orderId": "", "symbol": "BTCUSDT",
							"side": "Buy", "execPrice": "65500", "execQty": "0.1", "execValue": "6550",
							"execFee": "9.99", "feeCurrency": "USDT", "execPnl": "0", "closedSize": "0",
							"execType": "Funding", "execTime": strconv.FormatInt(fundingMs, 10),
						},
						{
							"execId": "byb-exec-2", "orderId": "ord-btc-close", "symbol": "BTCUSDT",
							"side": "Sell", "execPrice": "66000", "execQty": "0.1", "execValue": "6600",
							"execFee": "0.50", "feeCurrency": "USDT", "execPnl": "100", "closedSize": "0.1",
							"execType": "AdlTrade", "execTime": strconv.FormatInt(exitMs, 10),
						},
						// 2. BTC current open position fill (0.05 long @ 66000).
						{
							"execId": "byb-exec-3", "orderId": "ord-btc-open2", "symbol": "BTCUSDT",
							"side": "Buy", "execPrice": "66000", "execQty": "0.05", "execValue": "3300",
							"execFee": "0.33", "feeCurrency": "USDT", "execPnl": "0", "closedSize": "0",
							"execType": "Trade", "execTime": strconv.FormatInt(nowMs-60_000, 10),
						},
						// 3. ETH Hedge mode Short open & close -> must map to "SHORT".
						{
							"execId": "byb-exec-eth-1", "orderId": "ord-eth-open", "symbol": "ETHUSDT",
							"side": "Sell", "execPrice": "3000", "execQty": "1", "execValue": "3000",
							"execFee": "0.6", "feeCurrency": "USDT", "execPnl": "0", "closedSize": "0",
							"execType": "Trade", "execTime": strconv.FormatInt(entryMs+10_000, 10),
						},
						{
							"execId": "byb-exec-eth-2", "orderId": "ord-eth-close", "symbol": "ETHUSDT",
							"side": "Buy", "execPrice": "2900", "execQty": "1", "execValue": "2900",
							"execFee": "0.5", "feeCurrency": "USDT", "execPnl": "100", "closedSize": "1",
							"execType": "Trade", "execTime": strconv.FormatInt(exitMs+10_000, 10),
						},
						// 4. Dated delivery contract must be filtered out.
						{
							"execId": "byb-exec-dated", "orderId": "ord-dated", "symbol": "BTC-27DEC26",
							"side": "Buy", "execPrice": "65000", "execQty": "1", "execValue": "65000",
							"execFee": "1", "feeCurrency": "USDT", "execPnl": "0", "closedSize": "0",
							"execType": "Trade", "execTime": strconv.FormatInt(entryMs, 10),
						},
					},
				},
			})
		case "/v5/position/closed-pnl":
			endMs, _ := strconv.ParseInt(r.URL.Query().Get("endTime"), 10, 64)
			if endMs < nowMs-1000 {
				_ = json.NewEncoder(w).Encode(map[string]any{"retCode": 0, "result": map[string]any{"list": []any{}}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"retCode": 0,
				"result": map[string]any{
					"list": []map[string]any{
						{
							"symbol": "ETHUSDT", "orderId": "ord-eth-close", "side": "Buy",
							"qty": "1", "closedSize": "1", "closedPnl": "98.9", "leverage": "8",
							"createdTime": strconv.FormatInt(exitMs, 10), "updatedTime": strconv.FormatInt(exitMs+10_000, 10),
						},
					},
				},
			})
		case "/v5/account/transaction-log":
			txLogChunks.Add(1)
			st, _ := strconv.ParseInt(r.URL.Query().Get("startTime"), 10, 64)
			et, _ := strconv.ParseInt(r.URL.Query().Get("endTime"), 10, 64)
			if st <= 0 || et <= 0 || et-st > 7*24*3600*1000 {
				t.Fatalf("transaction-log MUST include explicit <=7d startTime/endTime, got start=%d end=%d", st, et)
			}
			if et < nowMs-1000 {
				_ = json.NewEncoder(w).Encode(map[string]any{"retCode": 0, "result": map[string]any{"list": []any{}}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"retCode": 0,
				"result": map[string]any{
					"list": []map[string]any{
						{
							"id": "tx-fund-btc-1", "symbol": "BTCUSDT", "type": "SETTLEMENT",
							"funding": "-1.25", "currency": "USDT", "transactionTime": strconv.FormatInt(fundingMs, 10),
						},
						{
							"id": "tx-trade-ignore", "symbol": "BTCUSDT", "type": "TRADE",
							"funding": "0", "fee": "0.50", "currency": "USDT", "transactionTime": strconv.FormatInt(exitMs, 10),
						},
					},
				},
			})
		default:
			if strings.Contains(r.URL.RawQuery, "BTC-27DEC26") {
				seenDatedContract.Store(true)
			}
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cli := NewBybitAccountClient()
	cli.SetKeys(apiKey, apiSecret)
	cli.baseURL = srv.URL

	out, err := cli.SyncToCache(ctx, store, journal, nil)
	if err != nil {
		t.Fatalf("Bybit SyncToCache failed: %v", err)
	}
	if execChunks.Load() < 13 || txLogChunks.Load() < 13 {
		t.Fatalf("expected 90d initial sync to split into >=13 7-day chunks, got exec=%d tx=%d", execChunks.Load(), txLogChunks.Load())
	}
	if seenDatedContract.Load() {
		t.Fatal("dated contract should have been filtered before position lookup")
	}
	if out.SyncedTrades != 3 || out.SyncedFills != 5 || out.OpenPositions != 1 || out.WalletBalanceUSD != 1234.56 || out.UnrealizedPnLUSD != 18.75 {
		t.Fatalf("unexpected Bybit SyncToCache outcome: %+v", out)
	}

	// Verify Bybit closed BTC trade: GROSS RealizedPnL=100, FeeUSD=-0.25+0.50=0.25, FundingUSD=-1.25 => NetPnL=98.50, matched to ticket!
	bybTrades, err := store.ListSyncedTradesForExchange(ctx, "bybit", "")
	if err != nil || len(bybTrades) != 3 {
		t.Fatalf("expected 3 bybit synced trades, got %+v err=%v", bybTrades, err)
	}
	var btcClosed, ethClosed, btcOpen *cache.SyncedTrade
	for i := range bybTrades {
		tr := &bybTrades[i]
		if tr.Symbol == "BTC/USDT:USDT" && tr.Status == "closed" {
			btcClosed = tr
		} else if tr.Symbol == "ETH/USDT:USDT" && tr.Status == "closed" {
			ethClosed = tr
		} else if tr.Symbol == "BTC/USDT:USDT" && tr.Status == "open" {
			btcOpen = tr
		}
	}
	if btcClosed == nil || btcClosed.LinkedTicketID != "tkt-bybit-btc-1" || btcClosed.MarginMode != decision.MarginModeIsolated {
		t.Fatalf("expected matched isolated closed BTC trade, got %+v", btcClosed)
	}
	if math.Abs(btcClosed.RealizedPnL-100) > 1e-9 || math.Abs(btcClosed.FeeUSD-0.25) > 1e-9 ||
		math.Abs(btcClosed.FundingUSD-(-1.25)) > 1e-9 || math.Abs(btcClosed.NetPnL-98.5) > 1e-9 {
		t.Fatalf("unexpected BTC PnL math: %+v", btcClosed)
	}
	if btcClosed.GateCode == decision.GateCrossMargin || btcClosed.GateCode == decision.GateNoValidTicket {
		t.Fatalf("unexpected violation gate code on valid isolated ticket trade: %q", btcClosed.GateCode)
	}
	if ethClosed == nil || ethClosed.PositionSide != "SHORT" || ethClosed.Direction != "short" || ethClosed.Leverage != 8 || ethClosed.GateCode != decision.GateNoValidTicket {
		t.Fatalf("expected ETH hedge short with closed-pnl leverage=8 and GATE_NO_VALID_TICKET, got %+v", ethClosed)
	}
	if btcOpen == nil || btcOpen.MarginMode != decision.MarginModeIsolated || btcOpen.Margin != 330 {
		t.Fatalf("expected open BTC trade isolated margin=330, got %+v", btcOpen)
	}

	// Verify open_positions coexistence: BOTH Binance and Bybit BTC/USDT:USDT BOTH exist!
	bybPos, err := store.ListOpenPositionsForExchange(ctx, "bybit")
	if err != nil || len(bybPos) != 1 || bybPos[0].MarginMode != decision.MarginModeIsolated {
		t.Fatalf("expected 1 isolated bybit open position, got %+v err=%v", bybPos, err)
	}
	bncPos, err := store.ListOpenPositionsForExchange(ctx, "binance")
	if err != nil || len(bncPos) != 1 || bncPos[0].EntryPrice != 50000 {
		t.Fatalf("pre-existing binance open position damaged: %+v err=%v", bncPos, err)
	}
	allPos, err := store.ListOpenPositions(ctx)
	if err != nil || len(allPos) != 2 {
		t.Fatalf("expected 2 total open positions across binance+bybit, got %d err=%v", len(allPos), err)
	}

	// Verify pre-existing Binance fills, synced trades, incomes, and meta are untouched!
	bncFills, err := store.ListTradeFillsForExchange(ctx, "binance", "", 0)
	if err != nil || len(bncFills) != 1 || bncFills[0].FillID != "BTC/USDT:USDT:99" {
		t.Fatalf("pre-existing binance fills damaged: %+v err=%v", bncFills, err)
	}
	bncTrades, err := store.ListSyncedTradesForExchange(ctx, "binance", "")
	if err != nil || len(bncTrades) != 1 || bncTrades[0].EntryPrice != 50000 {
		t.Fatalf("pre-existing binance synced trade damaged: %+v err=%v", bncTrades, err)
	}
	bncInc, err := store.ListIncomeRecordsForExchange(ctx, "binance", "")
	if err != nil || len(bncInc) != 1 || bncInc[0].Income != -0.25 {
		t.Fatalf("pre-existing binance income damaged: %+v err=%v", bncInc, err)
	}

	bybMeta, err := store.GetAllExchangeMeta(ctx, "bybit")
	if err != nil || bybMeta["wallet_balance_usd"] != "1234.560000" {
		t.Fatalf("unexpected bybit meta: %+v err=%v", bybMeta, err)
	}
	bncMeta, err := store.GetAllExchangeMeta(ctx, "binance")
	if err != nil || bncMeta["wallet_balance_usd"] != "888.5" {
		t.Fatalf("pre-existing binance meta damaged: %+v err=%v", bncMeta, err)
	}

	// 4. Incremental sync uses cached latest fill/income time -> <= 2 chunks instead of 13!
	execChunks.Store(0)
	txLogChunks.Store(0)
	out2, err := cli.SyncToCache(ctx, store, journal, nil)
	if err != nil {
		t.Fatalf("incremental Bybit SyncToCache failed: %v", err)
	}
	if execChunks.Load() != 1 || txLogChunks.Load() != 1 || out2.SyncedFills != 5 {
		t.Fatalf("expected 1 incremental chunk each and 5 fills preserved, got exec=%d tx=%d out=%+v", execChunks.Load(), txLogChunks.Load(), out2)
	}

	_ = fmt.Sprintf("verified %d bybit trades", len(bybTrades))
}

func TestBybitAccountClient_UTACrossMargin_And_OneWayFlip(t *testing.T) {
	const (
		apiKey    = "byb_cross_key_1"
		apiSecret = "byb_cross_sec_1"
	)
	nowMs := time.Now().UnixMilli()
	entryMs := nowMs - 3600*1000
	flipMs := nowMs - 1800*1000

	tmp := t.TempDir()
	store, err := cache.Open(filepath.Join(tmp, "cross.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	journal, err := storage.NewJournal(types.StorageConfig{DatabasePath: filepath.Join(tmp, "kairos.db")})
	if err != nil {
		t.Fatal(err)
	}
	_ = journal.SaveTicket(types.DecisionTicket{
		SchemaVersion: types.DecisionTicketSchemaVersion,
		ID:            "tkt-cross-1",
		Symbol:        "SOL/USDT:USDT",
		Direction:     types.CycleDirectionUp,
		Status:        types.TicketStatusAccepted,
		CreatedAt:     entryMs / 1000,
		SignalAt:      entryMs / 1000,
		RiskPlan:      types.RiskPlan{EntryPrice: 100, StopPrice: 95},
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		verifyBybitRequestSignature(t, r, apiKey, apiSecret)
		switch r.URL.Path {
		case "/v5/account/info":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"retCode": 0,
				"result": map[string]any{
					"unifiedMarginStatus": 3,
					"marginMode":          "REGULAR_MARGIN",
				},
			})
		case "/v5/account/wallet-balance":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"retCode": 0,
				"result": map[string]any{
					"list": []map[string]any{
						{"accountType": "UNIFIED", "totalWalletBalance": "500", "totalPerpUPL": "0"},
					},
				},
			})
		case "/v5/position/list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"retCode": 0,
				"result": map[string]any{
					"list": []map[string]any{
						{"symbol": "SOLUSDT", "side": "Sell", "size": "1", "avgPrice": "110", "markPrice": "109", "unrealisedPnl": "1", "leverage": "5", "positionIdx": 0, "updatedTime": strconv.FormatInt(nowMs, 10)},
					},
				},
			})
		case "/v5/execution/list":
			et, _ := strconv.ParseInt(r.URL.Query().Get("endTime"), 10, 64)
			if et < nowMs-1000 {
				_ = json.NewEncoder(w).Encode(map[string]any{"retCode": 0, "result": map[string]any{"list": []any{}}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"retCode": 0,
				"result": map[string]any{
					"list": []map[string]any{
						{
							"execId": "sol-1", "orderId": "ord-sol-1", "symbol": "SOLUSDT",
							"side": "Buy", "execPrice": "100", "execQty": "2", "execValue": "200",
							"execFee": "0.1", "feeCurrency": "USDT", "execPnl": "0", "closedSize": "0",
							"execType": "Trade", "execTime": strconv.FormatInt(entryMs, 10),
						},
						// One-Way flip: Sell 3 when 2 long is open -> closes 2 long + opens 1 short!
						{
							"execId": "sol-2", "orderId": "ord-sol-2", "symbol": "SOLUSDT",
							"side": "Sell", "execPrice": "110", "execQty": "3", "execValue": "330",
							"execFee": "0.15", "feeCurrency": "USDT", "execPnl": "20", "closedSize": "2",
							"execType": "Trade", "execTime": strconv.FormatInt(flipMs, 10),
						},
					},
				},
			})
		case "/v5/position/closed-pnl", "/v5/account/transaction-log":
			_ = json.NewEncoder(w).Encode(map[string]any{"retCode": 0, "result": map[string]any{"list": []any{}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cli := NewBybitAccountClient()
	cli.SetKeys(apiKey, apiSecret)
	cli.baseURL = srv.URL

	out, err := cli.SyncToCache(context.Background(), store, journal, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.SyncedTrades != 2 || out.SyncedFills != 3 || out.OpenPositions != 1 {
		t.Fatalf("unexpected One-Way flip sync outcome: %+v", out)
	}
	trades, err := store.ListSyncedTradesForExchange(context.Background(), "bybit", "SOL/USDT:USDT")
	if err != nil || len(trades) != 2 {
		t.Fatalf("expected 2 SOL trades (closed long + open short), got %+v err=%v", trades, err)
	}
	var closedLong *cache.SyncedTrade
	for i := range trades {
		if trades[i].Status == "closed" {
			closedLong = &trades[i]
		}
	}
	if closedLong == nil || closedLong.MarginMode != "cross" || closedLong.GateCode != decision.GateCrossMargin || closedLong.LinkedTicketID != "tkt-cross-1" {
		t.Fatalf("expected UTA REGULAR_MARGIN -> cross and GATE_CROSS_MARGIN, got %+v", closedLong)
	}
}
