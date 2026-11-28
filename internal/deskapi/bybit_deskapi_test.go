package deskapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ArchdevilForge/kairos/internal/cache"
	"github.com/ArchdevilForge/kairos/internal/deskapi"
	"github.com/ArchdevilForge/kairos/internal/exchange"
	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/coder/websocket"
)

type fnRT func(*http.Request) (*http.Response, error)

func (f fnRT) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBybitStockPerpBarsBootstrapAndStatsBySymbol(t *testing.T) {
	rt := fnRT(func(req *http.Request) (*http.Response, error) {
		var body string
		switch req.URL.Path {
		case "/v5/market/instruments-info":
			body = `{"retCode":0,"result":{"nextPageCursor":"","list":[
				{"symbol":"BTCUSDT","contractType":"LinearPerpetual","status":"Trading","quoteCoin":"USDT","settleCoin":"USDT","launchTime":"1600000000000","symbolType":""},
				{"symbol":"NVDAUSDT","contractType":"LinearPerpetual","status":"Trading","quoteCoin":"USDT","settleCoin":"USDT","launchTime":"1700000000000","symbolType":"stock","fullName":"NVIDIA","underlyingTicker":"NVDA","marketRegion":"US"},
				{"symbol":"AMDSTOCKUSDT","contractType":"LinearPerpetual","status":"Trading","quoteCoin":"USDT","settleCoin":"USDT","launchTime":"1700000000000","symbolType":"stock","fullName":"Advanced Micro Devices","underlyingTicker":"AMD","marketRegion":"US"}
			]}}`
		case "/v5/market/tickers":
			body = `{"retCode":0,"result":{"list":[
				{"symbol":"BTCUSDT","turnover24h":"5000000000"},
				{"symbol":"NVDAUSDT","turnover24h":"120000000"},
				{"symbol":"AMDSTOCKUSDT","turnover24h":"60000000"}
			]}}`
		case "/v5/market/kline":
			body = `{"retCode":0,"result":{"list":[
				["1776003600000","129.5","130.5","129.0","130.1","1200","156000"],
				["1776002700000","128.0","129.6","127.9","129.5","1100","142000"]
			]}}`
		default:
			body = `{"retCode":0,"result":{"list":[]}}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})
	cleanup := exchange.SetBybitTestHooks(rt, "")
	defer cleanup()

	store, err := cache.Open(filepath.Join(t.TempDir(), "desk-bybit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	now := time.Now().Unix()
	_ = store.ReplaceSyncedTradesForExchange(ctx, "bybit", []cache.SyncedTrade{
		{
			Exchange:     "bybit",
			ID:           "trd-bybit-nvda-1",
			Symbol:       "NVDA/USDT:USDT",
			Direction:    "long",
			PositionSide: "BOTH",
			MarginMode:   "cross",
			Leverage:     10,
			EntryPrice:   128.03,
			ExitPrice:    130.08,
			Qty:          0.78,
			EntryTimeMs:  (now - 7200) * 1000,
			ExitTimeMs:   (now - 3600) * 1000,
			RealizedPnL:  1.599,
			FeeUSD:       0.0554,
			NetPnL:       1.5436,
			Margin:       9.9863,
			Status:       "closed",
		},
	})
	_ = store.ReplaceOpenPositionsForExchange(ctx, "bybit", []cache.OpenPosition{
		{
			Exchange:       "bybit",
			Symbol:         "ETH/USDT:USDT",
			PositionSide:   "BOTH",
			MarginMode:     "cross",
			Leverage:       10,
			Qty:            -0.33,
			EntryPrice:     1699.66,
			MarkPrice:      1702.63,
			UnrealizedPnL:  -0.98,
			LiqPrice:       3119.72,
			IsolatedWallet: 56.53,
			StopLoss:       1731.95,
			TakeProfit:     1605.0,
			PositionValue:  561.87,
			BreakEvenPrice: 1697.79,
			UpdatedAt:      now,
		},
	})
	_ = store.SetExchangeMeta(ctx, "bybit", "account_type", "UNIFIED")
	_ = store.SetExchangeMeta(ctx, "bybit", "margin_mode", "cross")
	_ = store.SetExchangeMeta(ctx, "bybit", "wallet_balance_usd", "471.49")
	_ = store.SetExchangeMeta(ctx, "bybit", "total_equity_usd", "469.86")
	_ = store.SetExchangeMeta(ctx, "bybit", "available_balance_usd", "413.42")
	_ = store.SetExchangeMeta(ctx, "bybit", "initial_margin_usd", "56.44")
	_ = store.SetExchangeMeta(ctx, "bybit", "unrealized_pnl_usd", "-0.98")
	_ = store.SetExchangeMeta(ctx, "bybit", "last_sync_at", "1776007200")

	srv := deskapi.NewServer(nil, store, "tok")
	defer srv.Close()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	getJSON := func(path string, dst any) int {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+path, nil)
		req.Header.Set("Authorization", "Bearer tok")
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer func() { _ = resp.Body.Close() }()
		if dst != nil {
			_ = json.NewDecoder(resp.Body).Decode(dst)
		}
		return resp.StatusCode
	}

	// 1. NVDA stock perpetual bars succeed with 200 (canonical, fullName, and underlying ticker aliases).
	var nvdaBars deskapi.MarketBarsResponse
	if code := getJSON("/api/v1/market/bars?symbol=NVDA/USDT:USDT&tf=15m&limit=10", &nvdaBars); code != http.StatusOK || len(nvdaBars.Bars) != 2 {
		t.Fatalf("expected 200 + 2 NVDA bars, got code=%d len=%d", code, len(nvdaBars.Bars))
	}
	var nvidiaAliasBars deskapi.MarketBarsResponse
	if code := getJSON("/api/v1/market/bars?symbol=NVIDIA&tf=15m&limit=10", &nvidiaAliasBars); code != http.StatusOK || nvidiaAliasBars.Symbol != "NVDA/USDT:USDT" || len(nvidiaAliasBars.Bars) != 2 {
		t.Fatalf("expected NVIDIA alias to resolve to NVDA/USDT:USDT, got code=%d sym=%q len=%d", code, nvidiaAliasBars.Symbol, len(nvidiaAliasBars.Bars))
	}
	var amdAliasBars deskapi.MarketBarsResponse
	if code := getJSON("/api/v1/market/bars?symbol=AMD&tf=15m&limit=10", &amdAliasBars); code != http.StatusOK || amdAliasBars.Symbol != "AMDSTOCK/USDT:USDT" || len(amdAliasBars.Bars) != 2 {
		t.Fatalf("expected AMD alias to resolve to AMDSTOCK/USDT:USDT, got code=%d sym=%q len=%d", code, amdAliasBars.Symbol, len(amdAliasBars.Bars))
	}

	// 2. Truly unknown symbol still returns 400.
	if code := getJSON("/api/v1/market/bars?symbol=FAKECOIN/USDT:USDT&tf=15m", nil); code != http.StatusBadRequest {
		t.Fatalf("expected 400 for FAKECOIN, got %d", code)
	}

	// 3. Bootstrap exposes NVDA in available_symbols, symbol_catalog metadata, and enriched open position + UTA account.
	var boot deskapi.BootstrapPayload
	if code := getJSON("/api/v1/bootstrap", &boot); code != http.StatusOK {
		t.Fatalf("bootstrap code=%d", code)
	}
	foundNVDA := false
	for _, s := range boot.Status.AvailableSymbols {
		if s == "NVDA/USDT:USDT" {
			foundNVDA = true
			break
		}
	}
	if !foundNVDA {
		t.Fatalf("NVDA/USDT:USDT missing from available_symbols: %v", boot.Status.AvailableSymbols)
	}
	foundNVDACatalog := false
	for _, item := range boot.Status.SymbolCatalog {
		if item.Symbol == "NVDA/USDT:USDT" && item.FullName == "NVIDIA" && item.SymbolType == "stock" && item.UnderlyingTicker == "NVDA" {
			foundNVDACatalog = true
			break
		}
	}
	if !foundNVDACatalog {
		t.Fatalf("NVDA catalog metadata missing from bootstrap.status.symbol_catalog: %+v", boot.Status.SymbolCatalog)
	}
	if boot.Status.Account.TotalEquityUSD != 469.86 || boot.Status.Account.AccountType != "UNIFIED" {
		t.Fatalf("unexpected account status: %+v", boot.Status.Account)
	}
	if len(boot.OpenPositions) != 1 || boot.OpenPositions[0].StopLoss != 1731.95 || boot.OpenPositions[0].TakeProfit != 1605 {
		t.Fatalf("unexpected open positions: %+v", boot.OpenPositions)
	}

	// 4. Stats returns equity curve with baseline (2 points for 1 trade), BySymbol rows, and UTA reconciliation.
	var stats deskapi.StatsPayload
	if code := getJSON("/api/v1/stats", &stats); code != http.StatusOK {
		t.Fatalf("stats code=%d", code)
	}
	if len(stats.EquityCurve) != 2 {
		t.Fatalf("expected baseline + 1 closed trade = 2 equity points, got %d", len(stats.EquityCurve))
	}
	if len(stats.BySymbol) != 2 {
		t.Fatalf("expected 2 BySymbol rows (NVDA + ETH), got %+v", stats.BySymbol)
	}
	if stats.WalletReconciliation.ClosedTradesCount != 1 || stats.WalletReconciliation.OpenPositionsCount != 1 || stats.WalletReconciliation.AvgLeverage != 10 {
		t.Fatalf("unexpected reconciliation summary: %+v", stats.WalletReconciliation)
	}

	// 5. WS subscribe resolves cold-start alias (NVIDIA/USDT:USDT -> NVDA/USDT:USDT, AMD -> AMDSTOCK/USDT:USDT).
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/ws?token=tok"
	wsCtx, wsCancel := context.WithTimeout(ctx, 5*time.Second)
	defer wsCancel()
	wsConn, _, err := websocket.Dial(wsCtx, wsURL, nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer func() { _ = wsConn.Close(websocket.StatusNormalClosure, "") }()

	subPayload := `{"type":"subscribe","subs":[{"symbol":"NVIDIA/USDT:USDT","tfs":[]},{"symbol":"AMD","tfs":[]}]}`
	if err := wsConn.Write(wsCtx, websocket.MessageText, []byte(subPayload)); err != nil {
		t.Fatalf("ws write subscribe: %v", err)
	}
	_, rawAck, err := wsConn.Read(wsCtx)
	if err != nil {
		t.Fatalf("ws read ack: %v", err)
	}
	var ack struct {
		Type   string `json:"type"`
		Symbol string `json:"symbol"`
		Subs   []struct {
			Symbol string `json:"symbol"`
		} `json:"subs"`
	}
	if err := json.Unmarshal(rawAck, &ack); err != nil {
		t.Fatalf("decode ws ack: %v", err)
	}
	if ack.Type != "subscribed" || ack.Symbol != "NVDA/USDT:USDT" || len(ack.Subs) != 2 || ack.Subs[1].Symbol != "AMDSTOCK/USDT:USDT" {
		t.Fatalf("expected WS alias resolution to NVDA & AMDSTOCK, got %+v (raw=%s)", ack, string(rawAck))
	}
}

type fakeAccountClient struct {
	exName   string
	hasKeys  bool
	syncFn   func(ctx context.Context, store *cache.Store) (*exchange.AccountSyncOutcome, error)
	syncCall atomic.Int32
}

func (f *fakeAccountClient) Exchange() string { return f.exName }
func (f *fakeAccountClient) HasKeys() bool    { return f.hasKeys }
func (f *fakeAccountClient) APIKeyMasked() string {
	if !f.hasKeys {
		return ""
	}
	return "test****key"
}
func (f *fakeAccountClient) SetKeys(k, s string) { f.hasKeys = k != "" && s != "" }
func (f *fakeAccountClient) ReloadKeys()         {}
func (f *fakeAccountClient) SyncToCache(
	ctx context.Context,
	store *cache.Store,
	_ *storage.Journal,
	_ []string,
) (*exchange.AccountSyncOutcome, error) {
	f.syncCall.Add(1)
	if f.syncFn != nil {
		return f.syncFn(ctx, store)
	}
	nowSec := time.Now().Unix()
	_ = store.SetExchangeMeta(ctx, f.exName, "wallet_balance_usd", "500.00")
	_ = store.SetExchangeMeta(ctx, f.exName, "last_sync_at", "1795858000")
	return &exchange.AccountSyncOutcome{
		SyncedTrades:     2,
		SyncedFills:      4,
		OpenPositions:    1,
		WalletBalanceUSD: 500,
		LastSyncAt:       nowSec,
	}, nil
}

func TestAccountSyncStaleAwareCoalescingAndBackgroundAutoSync(t *testing.T) {
	rt := fnRT(func(req *http.Request) (*http.Response, error) {
		var body string
		switch req.URL.Path {
		case "/v5/market/instruments-info":
			body = `{"retCode":0,"result":{"nextPageCursor":"","list":[
				{"symbol":"BTCUSDT","contractType":"LinearPerpetual","status":"Trading","quoteCoin":"USDT","settleCoin":"USDT","launchTime":"1600000000000","symbolType":""},
				{"symbol":"NVDAUSDT","contractType":"LinearPerpetual","status":"Trading","quoteCoin":"USDT","settleCoin":"USDT","launchTime":"1700000000000","symbolType":"stock","fullName":"NVIDIA","underlyingTicker":"NVDA"}
			]}}`
		default:
			body = `{"retCode":0,"result":{"list":[]}}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})
	cleanup := exchange.SetBybitTestHooks(rt, "")
	defer cleanup()

	store, err := cache.Open(filepath.Join(t.TempDir(), "desk-autosync.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	srv := deskapi.NewServer(nil, store, "tok")
	defer srv.Close()

	// 1. Unconfigured client: stale-aware sync returns 200 skipped=true; forced sync returns 400.
	unconfigured := &fakeAccountClient{exName: "bybit", hasKeys: false}
	srv.SetAccountClient("bybit", unconfigured)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	postSync := func(path string) (int, deskapi.AccountSyncResponse) {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+path, nil)
		req.Header.Set("Authorization", "Bearer tok")
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		defer func() { _ = resp.Body.Close() }()
		var out deskapi.AccountSyncResponse
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}

	if code, out := postSync("/api/v1/account/sync?if_stale_sec=30"); code != http.StatusOK || !out.Ok || !out.Skipped {
		t.Fatalf("expected 200 skipped=true when unconfigured, got code=%d out=%+v", code, out)
	}
	if code, _ := postSync("/api/v1/account/sync"); code != http.StatusBadRequest {
		t.Fatalf("expected 400 for manual sync without keys, got %d", code)
	}

	// 2. Configured client: concurrent sync requests coalesce into a single upstream SyncToCache call.
	gate := make(chan struct{})
	started := make(chan struct{}, 2)
	configured := &fakeAccountClient{
		exName:  "bybit",
		hasKeys: true,
		syncFn: func(ctx context.Context, st *cache.Store) (*exchange.AccountSyncOutcome, error) {
			select {
			case started <- struct{}{}:
			default:
			}
			<-gate
			nowSec := time.Now().Unix()
			_ = st.SetExchangeMeta(ctx, "bybit", "wallet_balance_usd", "612.50")
			_ = st.SetExchangeMeta(ctx, "bybit", "last_sync_at", "1795858500")
			_ = st.SetExchangeMeta(ctx, "bybit", "sync_error", "")
			return &exchange.AccountSyncOutcome{
				SyncedTrades:     3,
				SyncedFills:      6,
				OpenPositions:    1,
				WalletBalanceUSD: 612.50,
				LastSyncAt:       nowSec,
			}, nil
		},
	}
	srv.SetAccountClient("bybit", configured)

	var wg sync.WaitGroup
	results := make([]deskapi.AccountSyncResponse, 2)
	codes := make([]int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			codes[idx], results[idx] = postSync("/api/v1/account/sync?if_stale_sec=60")
		}(i)
	}
	<-started
	time.Sleep(25 * time.Millisecond)
	close(gate)
	wg.Wait()

	if codes[0] != http.StatusOK || codes[1] != http.StatusOK {
		t.Fatalf("expected both coalesced calls to return 200, got %v", codes)
	}
	if configured.syncCall.Load() != 1 {
		t.Fatalf("expected coalesced concurrent syncs to call SyncToCache once, got %d", configured.syncCall.Load())
	}
	if results[0].SyncedTrades != 3 || results[1].SyncedTrades != 3 {
		t.Fatalf("expected both callers to receive synced_trades=3, got %+v", results)
	}

	// 3. Immediate stale-aware request skips because sync just finished; forced sync runs again.
	if code, out := postSync("/api/v1/account/sync?if_stale_sec=60"); code != http.StatusOK || !out.Skipped {
		t.Fatalf("expected fresh sync to skip with if_stale_sec=60, got code=%d out=%+v", code, out)
	}
	if configured.syncCall.Load() != 1 {
		t.Fatalf("expected skipped sync not to call SyncToCache, got %d", configured.syncCall.Load())
	}
	if code, out := postSync("/api/v1/account/sync"); code != http.StatusOK || out.Skipped {
		t.Fatalf("expected forced manual sync to run, got code=%d out=%+v", code, out)
	}
	if configured.syncCall.Load() != 2 {
		t.Fatalf("expected forced manual sync to increment call count to 2, got %d", configured.syncCall.Load())
	}

	// 4. StartBackgroundLoops triggers automatic periodic sync without manual HTTP calls.
	autoClient := &fakeAccountClient{exName: "bybit", hasKeys: true}
	srv2 := deskapi.NewServer(nil, store, "tok")
	srv2.SetAccountClient("bybit", autoClient)
	srv2.SetAutoSyncInterval(50*time.Millisecond, 5*time.Millisecond)
	srv2.StartBackgroundLoops()
	defer srv2.Close()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && autoClient.syncCall.Load() < 2 {
		time.Sleep(15 * time.Millisecond)
	}
	if autoClient.syncCall.Load() < 2 {
		t.Fatalf("expected StartBackgroundLoops to run automatic syncs (>=2), got %d", autoClient.syncCall.Load())
	}
}
