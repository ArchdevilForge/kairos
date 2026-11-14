package exchange

import (
	"context"
	"encoding/json"
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
