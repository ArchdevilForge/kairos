package deskapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ArchdevilForge/kairos/internal/cache"
	"github.com/ArchdevilForge/kairos/internal/deskapi"
	"github.com/ArchdevilForge/kairos/internal/exchange"
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
				{"symbol":"NVDAUSDT","contractType":"LinearPerpetual","status":"Trading","quoteCoin":"USDT","settleCoin":"USDT","launchTime":"1700000000000","symbolType":"stock"}
			]}}`
		case "/v5/market/tickers":
			body = `{"retCode":0,"result":{"list":[
				{"symbol":"BTCUSDT","turnover24h":"5000000000"},
				{"symbol":"NVDAUSDT","turnover24h":"120000000"}
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

	// 1. NVDA stock perpetual bars succeed with 200.
	var nvdaBars deskapi.MarketBarsResponse
	if code := getJSON("/api/v1/market/bars?symbol=NVDA/USDT:USDT&tf=15m&limit=10", &nvdaBars); code != http.StatusOK || len(nvdaBars.Bars) != 2 {
		t.Fatalf("expected 200 + 2 NVDA bars, got code=%d len=%d", code, len(nvdaBars.Bars))
	}

	// 2. Truly unknown symbol still returns 400.
	if code := getJSON("/api/v1/market/bars?symbol=FAKECOIN/USDT:USDT&tf=15m", nil); code != http.StatusBadRequest {
		t.Fatalf("expected 400 for FAKECOIN, got %d", code)
	}

	// 3. Bootstrap exposes NVDA in available_symbols and enriched open position + UTA account.
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
}
