package utils_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ArchdevilForge/kairos/internal/cache"
	"github.com/ArchdevilForge/kairos/internal/decision"
	"github.com/ArchdevilForge/kairos/internal/deskapi"
	"github.com/ArchdevilForge/kairos/internal/engine"
	"github.com/ArchdevilForge/kairos/internal/exchange"
	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
	"github.com/ArchdevilForge/kairos/internal/utils"
	"github.com/coder/websocket"
)

func TestNewBlacklist_LoadsFile(t *testing.T) {
	home := t.TempDir()
	cfgDir := filepath.Join(home, ".config", "kairos")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "blacklist.txt"), []byte(" btc/usdt:usdt \n\nDOGE/USDT:USDT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	b := utils.NewBlacklist()
	if !b.IsBlocked("BTC/USDT:USDT") || !b.IsBlocked("DOGE/USDT:USDT") {
		t.Fatal("expected blocked symbols from file")
	}
	if b.IsBlocked("ETH/USDT:USDT") {
		t.Fatal("unexpected block")
	}
}

func TestExtractLastPrice_NestedInfo(t *testing.T) {
	ticker := map[string]any{
		"info": map[string]any{"markPx": "123.45"},
	}
	v := utils.ExtractLastPrice(ticker)
	if v == nil || *v != 123.45 {
		t.Fatalf("nested markPx: %v", v)
	}
}

func TestExtractQuoteVolume_BaseTimesPrice(t *testing.T) {
	ticker := map[string]any{
		"baseVolume": 10.0,
		"last":       2.5,
	}
	if got := utils.ExtractQuoteVolume(ticker); got != 25.0 {
		t.Fatalf("base*price: got %v", got)
	}

	nested := map[string]any{
		"info": map[string]any{"volUsd24h": "1000"},
	}
	if utils.ExtractQuoteVolume(nested) != 1000 {
		t.Fatal("nested volUsd24h")
	}
}

func TestFirstFloat_JSONNumber(t *testing.T) {
	m := map[string]any{"n": json.Number("9.5")}
	v := utils.FirstFloat(m, []string{"n"})
	if v == nil || *v != 9.5 {
		t.Fatalf("json.Number: %v", v)
	}
}

func TestLooksLikeUSDTPerpetual(t *testing.T) {
	if !utils.LooksLikeUSDTPerpetual("BTC/USDT:USDT", nil) {
		t.Fatal("canonical symbol")
	}
	info := map[string]any{"info": map[string]any{"instType": "SWAP"}}
	if !utils.LooksLikeUSDTPerpetual("BTCUSDT", info) {
		t.Fatal("swap info")
	}
	perp := map[string]any{"info": map[string]any{"contractType": "PERPETUAL"}}
	if !utils.LooksLikeUSDTPerpetual("ETH", perp) {
		t.Fatal("perpetual contract")
	}
	if utils.LooksLikeUSDTPerpetual("ETH", map[string]any{}) {
		t.Fatal("no info should be false")
	}
}

func TestCacheStore_Comprehensive(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	cfg := types.StorageConfig{DatabasePath: filepath.Join(tmp, "kairos.db")}
	if got := cache.DBPath(cfg); !strings.HasSuffix(got, "desktop-cache.db") {
		t.Fatalf("DBPath = %q", got)
	}

	store, err := cache.OpenFromStorageConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if store.Path() == "" {
		t.Fatal("expected non-empty store path")
	}

	// Nil store guards.
	var nilStore *cache.Store
	if nilStore.Path() != "" || nilStore.Close() != nil {
		t.Fatal("nil store Path/Close failed")
	}
	if err := nilStore.UpsertKlines(ctx, "BTC/USDT:USDT", "5m", "binance", []types.Candle{{Timestamp: 100}}); err != nil {
		t.Fatal(err)
	}
	if _, err := nilStore.CreateDrawing(ctx, cache.ChartDrawing{}); err == nil {
		t.Fatal("expected error creating drawing on nil store")
	}

	// Klines CRUD + queries.
	err = store.UpsertKlines(ctx, "BTC/USDT:USDT", "5m", "binance", []types.Candle{
		{Timestamp: 1700001000, Open: 100, High: 105, Low: 99, Close: 104, Volume: 10, QuoteVol: 1040},
		{Timestamp: 1700001300, Open: 104, High: 108, Low: 103, Close: 107, Volume: 12, QuoteVol: 1284},
		{Timestamp: 1700001600, Open: 107, High: 110, Low: 106, Close: 109, Volume: 15, QuoteVol: 1635},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertKline(ctx, "BTC/USDT:USDT", "5m", "binance", types.Candle{Timestamp: 1700001900_000, Open: 109, High: 112, Low: 108, Close: 111, Volume: 20, QuoteVol: 2220}); err != nil {
		t.Fatal(err)
	}
	kl, err := store.GetKlines(ctx, "BTC/USDT:USDT", "5m", 10, 0)
	if err != nil || len(kl) != 4 || kl[3].Timestamp != 1700001900 {
		t.Fatalf("GetKlines: len=%d kl=%+v err=%v", len(kl), kl, err)
	}
	klBefore, err := store.GetKlines(ctx, "BTC/USDT:USDT", "5m", 10, 1700001600)
	if err != nil || len(klBefore) != 2 {
		t.Fatalf("GetKlines before: len=%d err=%v", len(klBefore), err)
	}
	klUpTo, err := store.GetKlinesUpTo(ctx, "BTC/USDT:USDT", "5m", 1700001600, 10)
	if err != nil || len(klUpTo) != 3 {
		t.Fatalf("GetKlinesUpTo: len=%d err=%v", len(klUpTo), err)
	}
	klRange, err := store.GetKlinesRange(ctx, "BTC/USDT:USDT", "5m", 1700001300, 1700001600)
	if err != nil || len(klRange) != 2 {
		t.Fatalf("GetKlinesRange: len=%d err=%v", len(klRange), err)
	}
	latestTS, err := store.LatestKlineTime(ctx, "BTC/USDT:USDT", "5m")
	if err != nil || latestTS != 1700001900 {
		t.Fatalf("LatestKlineTime = %d err=%v", latestTS, err)
	}
	if missingTS, err := store.LatestKlineTime(ctx, "NONE/USDT:USDT", "5m"); err != nil || missingTS != 0 {
		t.Fatalf("missing LatestKlineTime = %d err=%v", missingTS, err)
	}
	syms, err := store.ListCachedSymbols(ctx)
	if err != nil || len(syms) != 1 || syms[0] != "BTC/USDT:USDT" {
		t.Fatalf("ListCachedSymbols = %v err=%v", syms, err)
	}

	// Fills, Income, SyncedTrades, OpenPositions.
	fills := []cache.TradeFill{
		{FillID: "f1", OrderID: "o1", Symbol: "BTC/USDT:USDT", Side: "BUY", PositionSide: "BOTH", Price: 100, Qty: 1, QuoteQty: 100, Commission: 0.1, CommissionAsset: "USDT", TimeMs: 1000_000, TradeID: "tr1"},
		{FillID: "f2", OrderID: "o2", Symbol: "BTC/USDT:USDT", Side: "SELL", PositionSide: "BOTH", Price: 110, Qty: 1, QuoteQty: 110, Commission: 0.1, CommissionAsset: "USDT", RealizedPnL: 10, TimeMs: 2000_000, TradeID: "tr1"},
	}
	if err := store.UpsertTradeFills(ctx, fills); err != nil {
		t.Fatal(err)
	}
	if got, err := store.ListTradeFills(ctx, "BTC/USDT:USDT", 1500_000); err != nil || len(got) != 1 {
		t.Fatalf("ListTradeFills filtered: %d %v", len(got), err)
	}
	if got, err := store.ListTradeFillsByTradeID(ctx, "tr1"); err != nil || len(got) != 2 {
		t.Fatalf("ListTradeFillsByTradeID: %d %v", len(got), err)
	}
	if ms, err := store.LatestFillTimeMs(ctx, "BTC/USDT:USDT"); err != nil || ms != 2000_000 {
		t.Fatalf("LatestFillTimeMs: %d %v", ms, err)
	}
	if ms, err := store.LatestFillTimeMs(ctx, ""); err != nil || ms != 2000_000 {
		t.Fatalf("LatestFillTimeMs overall: %d %v", ms, err)
	}

	if err := store.UpsertIncomeRecords(ctx, []cache.IncomeRecord{
		{TranID: "inc1", Symbol: "BTC/USDT:USDT", IncomeType: "FUNDING_FEE", Income: -0.25, Asset: "USDT", TimeMs: 1500_000},
	}); err != nil {
		t.Fatal(err)
	}
	if inc, err := store.ListIncomeRecords(ctx, "FUNDING_FEE"); err != nil || len(inc) != 1 {
		t.Fatalf("ListIncomeRecords: %d %v", len(inc), err)
	}
	if incAll, err := store.ListIncomeRecords(ctx, ""); err != nil || len(incAll) != 1 {
		t.Fatalf("ListIncomeRecords all: %d %v", len(incAll), err)
	}
	if ms, err := store.LatestIncomeTimeMs(ctx); err != nil || ms != 1500_000 {
		t.Fatalf("LatestIncomeTimeMs: %d %v", ms, err)
	}

	trades := []cache.SyncedTrade{
		{ID: "tr1", Symbol: "BTC/USDT:USDT", Direction: "long", PositionSide: "BOTH", MarginMode: "isolated", Leverage: 10, EntryPrice: 100, ExitPrice: 110, Qty: 1, EntryTimeMs: 1000_000, ExitTimeMs: 2000_000, RealizedPnL: 10, FeeUSD: 0.2, FundingUSD: -0.25, NetPnL: 9.55, Margin: 10, Status: "closed"},
	}
	if err := store.ReplaceAllSyncedTrades(ctx, trades, fills); err != nil {
		t.Fatal(err)
	}
	if st, err := store.ListSyncedTrades(ctx, "BTC/USDT:USDT"); err != nil || len(st) != 1 || st[0].NetPnL != 9.55 {
		t.Fatalf("ListSyncedTrades: %+v err=%v", st, err)
	}

	if err := store.ReplaceOpenPositions(ctx, []cache.OpenPosition{
		{Symbol: "ETH/USDT:USDT", PositionSide: "BOTH", MarginMode: "cross", Leverage: 5, Qty: 1, EntryPrice: 3000, MarkPrice: 3050, UnrealizedPnL: 50, UpdatedAt: 2000},
	}); err != nil {
		t.Fatal(err)
	}
	if op, err := store.ListOpenPositions(ctx); err != nil || len(op) != 1 {
		t.Fatalf("ListOpenPositions: %+v err=%v", op, err)
	}

	// Drawings CRUD + zero-lookahead filter.
	d1, err := store.CreateDrawing(ctx, cache.ChartDrawing{
		Symbol: "BTC/USDT:USDT",
		Kind:   "hline",
		Points: []cache.ChartDrawingPoint{{Time: 1000, Price: 64000}},
		Label:  "support",
	})
	if err != nil || d1.ID <= 0 {
		t.Fatalf("CreateDrawing d1: %+v err=%v", d1, err)
	}
	d2, err := store.CreateDrawing(ctx, cache.ChartDrawing{
		Symbol: "BTC/USDT:USDT",
		Kind:   "ruler",
		Points: []cache.ChartDrawingPoint{{Time: 1500, Price: 64000}, {Time: 2500, Price: 66000}},
	})
	if err != nil || d2.ID <= 0 {
		t.Fatalf("CreateDrawing d2: %+v err=%v", d2, err)
	}
	newLbl := "key-level"
	upd, err := store.UpdateDrawing(ctx, d1.ID, []cache.ChartDrawingPoint{{Time: 1000, Price: 64100}}, &newLbl)
	if err != nil || upd.Label != "key-level" {
		t.Fatalf("UpdateDrawing with label: %+v err=%v", upd, err)
	}
	updNoLbl, err := store.UpdateDrawing(ctx, d1.ID, []cache.ChartDrawingPoint{{Time: 1000, Price: 64200}}, nil)
	if err != nil || updNoLbl.Label != "key-level" {
		t.Fatalf("UpdateDrawing without label: %+v err=%v", updNoLbl, err)
	}
	if _, err := store.UpdateDrawing(ctx, 99999, nil, nil); err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
	dlFiltered, err := store.ListDrawings(ctx, "BTC/USDT:USDT", 2000)
	if err != nil || len(dlFiltered) != 1 || dlFiltered[0].ID != d1.ID {
		t.Fatalf("ListDrawings zero-lookahead failed: %+v err=%v", dlFiltered, err)
	}
	if err := store.DeleteDrawing(ctx, d1.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteDrawingsForSymbol(ctx, "BTC/USDT:USDT"); err != nil {
		t.Fatal(err)
	}
	if remaining, err := store.ListDrawings(ctx, "", 0); err != nil || len(remaining) != 0 {
		t.Fatalf("expected 0 drawings remaining, got %d err=%v", len(remaining), err)
	}

	// Account meta CRUD.
	if err := store.SetAccountMeta(ctx, "wallet_balance_usd", "123.45"); err != nil {
		t.Fatal(err)
	}
	if v, err := store.GetAccountMeta(ctx, "wallet_balance_usd"); err != nil || v != "123.45" {
		t.Fatalf("GetAccountMeta = %q err=%v", v, err)
	}
	if v, err := store.GetAccountMeta(ctx, "nonexistent"); err != nil || v != "" {
		t.Fatalf("missing GetAccountMeta = %q err=%v", v, err)
	}
	if all, err := store.GetAllAccountMeta(ctx); err != nil || all["wallet_balance_usd"] != "123.45" {
		t.Fatalf("GetAllAccountMeta = %+v err=%v", all, err)
	}
}

type mockRewriteRT struct {
	base *url.URL
}

func (t *mockRewriteRT) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = t.base.Scheme
	clone.URL.Host = t.base.Host
	return http.DefaultTransport.RoundTrip(clone)
}

func doReq(t *testing.T, method, targetURL, token string, body any) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, targetURL, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestDeskAPIServer_HTTP_And_WS_Integration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("BINANCE_READONLY_API_KEY", "")
	t.Setenv("BINANCE_READONLY_API_SECRET", "")

	now := time.Now().Unix()
	var klineCallCount atomic.Int32
	var wsConnMu sync.Mutex
	wsPushed := make(chan struct{}, 2)

	// Unified local mock Binance REST + WS server.
	binanceMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

			wsConnMu.Lock()
			tOpenMs := (now / 300) * 300 * 1000
			frame1, _ := json.Marshal(map[string]any{
				"e": "kline",
				"k": map[string]any{
					"t": tOpenMs, "o": "65000", "h": "65150", "l": "64950", "c": "65120", "v": "10", "q": "651200", "x": false,
				},
			})
			frame2, _ := json.Marshal(map[string]any{
				"e": "kline",
				"k": map[string]any{
					"t": tOpenMs, "o": "65000", "h": "65200", "l": "64950", "c": "65180", "v": "15", "q": "977700", "x": true,
				},
			})
			wsConnMu.Unlock()

			_ = conn.Write(r.Context(), websocket.MessageText, frame1)
			_ = conn.Write(r.Context(), websocket.MessageText, frame2)
			select {
			case wsPushed <- struct{}{}:
			default:
			}
			<-r.Context().Done()
			return
		}

		switch {
		case strings.Contains(r.URL.Path, "/fapi/v1/exchangeInfo"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"symbols": []map[string]any{
					{"symbol": "BTCUSDT", "status": "TRADING", "contractType": "PERPETUAL", "quoteAsset": "USDT", "underlyingType": "COIN"},
					{"symbol": "ETHUSDT", "status": "TRADING", "contractType": "PERPETUAL", "quoteAsset": "USDT", "underlyingType": "COIN"},
				},
			})
		case strings.Contains(r.URL.Path, "/fapi/v1/ticker/24hr"):
			_ = json.NewEncoder(w).Encode([]map[string]string{
				{"symbol": "BTCUSDT", "quoteVolume": "2000000000"},
				{"symbol": "ETHUSDT", "quoteVolume": "900000000"},
			})
		case strings.Contains(r.URL.Path, "/fapi/v1/klines"):
			klineCallCount.Add(1)
			baseTS := ((now / 300) * 300) - 300*4
			var rows [][]any
			for i := 0; i < 5; i++ {
				tsMs := (baseTS + int64(i)*300) * 1000
				rows = append(rows, []any{tsMs, "65000", "65100", "64900", "65050", "10", tsMs + 299999, "650500"})
			}
			_ = json.NewEncoder(w).Encode(rows)
		case strings.Contains(r.URL.Path, "/fapi/v2/account"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"totalWalletBalance":    "55.0",
				"totalUnrealizedProfit": "1.5",
				"assets":                []map[string]string{{"asset": "USDT", "walletBalance": "55.0", "unrealizedProfit": "1.5"}},
			})
		case strings.Contains(r.URL.Path, "/fapi/v2/positionRisk"):
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"symbol": "BTCUSDT", "positionAmt": "0.01", "entryPrice": "65000", "markPrice": "65150", "unRealizedProfit": "1.5", "liquidationPrice": "59000", "leverage": "10", "marginType": "isolated", "positionSide": "BOTH", "isolatedWallet": "65", "updateTime": now * 1000},
			})
		case strings.Contains(r.URL.Path, "/fapi/v1/income"):
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		case strings.Contains(r.URL.Path, "/fapi/v1/userTrades"):
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 7001, "orderId": 8001, "symbol": "BTCUSDT", "side": "BUY", "positionSide": "BOTH", "price": "65000", "qty": "0.01", "quoteQty": "650", "commission": "0.1", "commissionAsset": "USDT", "realizedPnl": "0", "time": (now - 600) * 1000},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer binanceMock.Close()

	mockURL, _ := url.Parse(binanceMock.URL)
	wsMockBase := "ws" + strings.TrimPrefix(binanceMock.URL, "http") + "/market/ws"
	restoreBinance := exchange.SetBinanceTestHooks(&mockRewriteRT{base: mockURL}, wsMockBase)
	defer restoreBinance()

	tmp := t.TempDir()
	cfg := types.Config{}
	cfg.Exchanges.Primary = "binance"
	cfg.DataManager.Exchanges = []string{"binance"}
	cfg.Storage.DatabasePath = filepath.Join(tmp, "kairos.db")
	cfg.Storage.WatchHintsPath = filepath.Join(tmp, "hints.jsonl")
	cfg.RiskGate.Enabled = true
	cfg.RiskGate.TradingBankrollUSD = 50
	cfg.RiskGate.MaxLossUSD = 5
	cfg.RiskGate.MaxMajorLeverage = 10
	cfg.RiskGate.MaxAltLeverage = 5
	cfg.RiskGate.CooldownMinutes = 45
	cfg.RiskGate.WindowStartHour = 18
	cfg.RiskGate.WindowEndHour = 1
	cfg.RiskGate.Timezone = "Asia/Shanghai"
	cfg.Notice.Enabled = true

	p := engine.NewPipeline(&cfg, nil)
	if err := p.EnsureDeskStorage(); err != nil {
		t.Fatal(err)
	}
	store, err := cache.OpenFromStorageConfig(cfg.Storage)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	p.SetCacheStore(store)

	// Seed account_meta settings to cover restoreNonSecretSettings in NewServer.
	ctx := context.Background()
	_ = store.SetAccountMeta(ctx, "setting_bankroll_usd", "60.0")
	_ = store.SetAccountMeta(ctx, "setting_max_loss_usd", "6.0")
	_ = store.SetAccountMeta(ctx, "setting_timezone", "Asia/Shanghai")

	// Seed >= 50 closed bars across 1d, 4h, 15m, 5m and 1m bars for cycle + replay synthesis.
	seedTF := func(tf string, sec int64, count int) {
		var cs []types.Candle
		start := ((now / sec) * sec) - int64(count)*sec
		for i := 0; i < count; i++ {
			px := 64000 + float64(i)*10
			cs = append(cs, types.Candle{
				Timestamp: start + int64(i)*sec,
				Open:      px,
				High:      px + 20,
				Low:       px - 15,
				Close:     px + 8,
				Volume:    100 + float64(i),
				QuoteVol:  (100 + float64(i)) * px,
			})
		}
		if err := store.UpsertKlines(ctx, "BTC/USDT:USDT", tf, "binance", cs); err != nil {
			t.Fatal(err)
		}
	}
	seedTF("1d", 86400, 50)
	seedTF("4h", 14400, 50)
	seedTF("15m", 900, 50)
	seedTF("5m", 300, 60)
	seedTF("1m", 60, 120)

	// Seed authoritative journal tickets, decisions, outcomes, annotations, notices.
	j := p.Journal()
	_ = j.SaveTicket(types.DecisionTicket{
		SchemaVersion:    types.DecisionTicketSchemaVersion,
		ID:               "tkt-btc-1",
		SessionID:        "sess-1",
		Symbol:           "BTC/USDT:USDT",
		PlaybookID:       types.PlaybookLeaderPullbackV1,
		Direction:        types.CycleDirectionUp,
		Grade:            types.TicketGradeA,
		TradeClass:       types.TradeClassAlignedLong,
		Status:           types.TicketStatusClosed,
		CreatedAt:        now - 3600,
		SignalAt:         now - 3600,
		EntryTriggeredAt: now - 3500,
		ClosedAt:         now - 1800,
		ExitReason:       types.ExitReasonProtectiveStop,
		ExitPrice:        64500,
		RiskPlan:         types.RiskPlan{EntryPrice: 65000, StopPrice: 64500},
	})
	maxLoss := 5.0
	stopPx := 64500.0
	_ = j.SaveDecision(types.DecisionRecord{
		SchemaVersion:     types.DecisionRecordSchemaVersion,
		TicketID:          "tkt-btc-1",
		Decision:          types.DecisionAccepted,
		ReasonCodes:       []string{types.ReasonStructureGood},
		PlannedStop:       &stopPx,
		PlannedMaxLossUSD: &maxLoss,
		DecidedAt:         now - 3550,
	})
	_ = j.SaveOutcome(storage.CounterfactualOutcome{
		SchemaVersion:     storage.CounterfactualSchemaVersion,
		TicketID:          "tkt-btc-1",
		SessionID:         "sess-1",
		Symbol:            "BTC/USDT:USDT",
		Direction:         types.CycleDirectionUp,
		Decision:          types.DecisionAccepted,
		NetR:              -1.0,
		MechanicalR:       -1.0,
		ExitReason:        types.ExitReasonProtectiveStop,
		TouchedPresetStop: true,
		PathStartUnix:     now - 3500,
		PathEndUnix:       now - 1800,
		Finalized:         true,
		Complete:          true,
		AsOfUnix:          now - 1800,
	})
	_ = j.SaveAnnotation(storage.GateAnnotation{
		TicketID: "tkt-btc-1",
		Codes:    []string{decision.GateNoValidTicket},
		Override: false,
		At:       now - 3550,
	})
	// Second ticket on SOL to exercise cooldown / lock in GateHealth.
	_ = j.SaveTicket(types.DecisionTicket{
		SchemaVersion: types.DecisionTicketSchemaVersion,
		ID:            "tkt-sol-1",
		SessionID:     "sess-2",
		Symbol:        "SOL/USDT:USDT",
		Direction:     types.CycleDirectionUp,
		Status:        types.TicketStatusClosed,
		CreatedAt:     now - 600,
		SignalAt:      now - 600,
		ClosedAt:      now - 300,
	})
	_ = j.SaveDecision(types.DecisionRecord{
		SchemaVersion: types.DecisionRecordSchemaVersion,
		TicketID:      "tkt-sol-1",
		Decision:      types.DecisionAccepted,
		ReasonCodes:   []string{types.ReasonStructureGood},
		DecidedAt:     now - 590,
	})
	_ = j.SaveOutcome(storage.CounterfactualOutcome{
		SchemaVersion: storage.CounterfactualSchemaVersion,
		TicketID:      "tkt-sol-1",
		Symbol:        "SOL/USDT:USDT",
		Decision:      types.DecisionAccepted,
		NetR:          -0.8,
		PathStartUnix: now - 580,
		PathEndUnix:   now - 300,
		Finalized:     true,
		Complete:      true,
		AsOfUnix:      now - 300,
	})

	_ = p.NoticeJournal().SaveNotice(types.Notice{
		SchemaVersion:  types.NoticeSchemaVersion,
		NoticeID:       "nt-btc-1",
		Symbol:         "BTC/USDT:USDT",
		DeliveryStatus: types.NoticeStatusSent,
		CreatedAt:      time.Unix(now-1200, 0),
	})

	// Seed trades, fills, open positions in SQLite cache.
	_ = store.ReplaceAllSyncedTrades(ctx, []cache.SyncedTrade{
		{ID: "tr-linked", Symbol: "BTC/USDT:USDT", Direction: "long", PositionSide: "BOTH", MarginMode: "isolated", Leverage: 10, EntryPrice: 65000, ExitPrice: 65500, Qty: 0.01, EntryTimeMs: (now - 3500) * 1000, ExitTimeMs: (now - 1800) * 1000, RealizedPnL: 5, FeeUSD: 0.2, NetPnL: 4.8, Status: "closed", LinkedTicketID: "tkt-btc-1"},
		{ID: "tr-unplanned", Symbol: "BTC/USDT:USDT", Direction: "long", PositionSide: "BOTH", MarginMode: "cross", Leverage: 10, EntryPrice: 64000, ExitPrice: 63800, Qty: 0.01, EntryTimeMs: (now - 900) * 1000, ExitTimeMs: (now - 700) * 1000, RealizedPnL: -2, FeeUSD: 0.1, NetPnL: -2.1, Status: "closed", GateCode: decision.GateNoValidTicket},
		{ID: "tr-open", Symbol: "BTC/USDT:USDT", Direction: "long", PositionSide: "BOTH", MarginMode: "isolated", Leverage: 10, EntryPrice: 65000, Qty: 0.01, EntryTimeMs: (now - 400) * 1000, Status: "open", LinkedTicketID: "tkt-btc-1"},
	}, []cache.TradeFill{
		{FillID: "f-a", OrderID: "o1", Symbol: "BTC/USDT:USDT", Side: "BUY", PositionSide: "BOTH", Price: 65000, Qty: 0.01, TimeMs: (now - 3500) * 1000, TradeID: "tr-linked"},
		{FillID: "f-b", OrderID: "o2", Symbol: "BTC/USDT:USDT", Side: "BUY", PositionSide: "BOTH", Price: 64000, Qty: 0.01, TimeMs: (now - 900) * 1000, TradeID: "tr-unplanned"},
	})
	_ = store.ReplaceOpenPositions(ctx, []cache.OpenPosition{
		{Symbol: "BTC/USDT:USDT", PositionSide: "BOTH", MarginMode: "isolated", Leverage: 10, Qty: 0.01, EntryPrice: 65000, MarkPrice: 65200, UnrealizedPnL: 2.0, UpdatedAt: now},
	})

	const token = "desk-test-secret-token"
	deskSrv := deskapi.NewServer(p, store, token)
	defer deskSrv.Close()
	obs := deskSrv.Observer()
	p.SetDesktopObserver(obs)

	// Guard: OnTicker & OnPulse with 0 WS clients must return immediately without panic.
	pxZero := 65000.0
	obs.OnTicker("binance", types.Ticker{Symbol: "BTC/USDT:USDT", LastPrice: &pxZero})
	obs.OnPulse(p.MarketPulseView())

	httpSrv := httptest.NewServer(deskSrv.Handler())
	defer httpSrv.Close()

	// 1. CORS OPTIONS & Health & Auth 401.
	optResp := doReq(t, http.MethodOptions, httpSrv.URL+"/api/v1/bootstrap", "", nil)
	_ = optResp.Body.Close()
	if optResp.StatusCode != http.StatusNoContent {
		t.Fatalf("OPTIONS status = %d", optResp.StatusCode)
	}
	healthResp := doReq(t, http.MethodGet, httpSrv.URL+"/api/v1/health", "", nil)
	_ = healthResp.Body.Close()
	if healthResp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", healthResp.StatusCode)
	}
	unauth := doReq(t, http.MethodGet, httpSrv.URL+"/api/v1/bootstrap", "", nil)
	_ = unauth.Body.Close()
	if unauth.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", unauth.StatusCode)
	}
	wrongTok := doReq(t, http.MethodGet, httpSrv.URL+"/api/v1/bootstrap", "wrong", nil)
	_ = wrongTok.Body.Close()
	if wrongTok.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong token, got %d", wrongTok.StatusCode)
	}

	// 2. Deleted endpoints must not exist (404 or 405).
	for _, dead := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/tickets/gate-preview"},
		{http.MethodPost, "/api/v1/tickets/draft"},
		{http.MethodPost, "/api/v1/tickets/t1/decide"},
		{http.MethodPost, "/api/v1/tickets/t1/close"},
		{http.MethodPut, "/api/v1/reviews/ticket/t1"},
	} {
		r := doReq(t, dead.method, httpSrv.URL+dead.path, token, map[string]any{})
		_ = r.Body.Close()
		if r.StatusCode != http.StatusNotFound && r.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("expected deleted route %s %s to return 404/405, got %d", dead.method, dead.path, r.StatusCode)
		}
	}

	// 3. Bootstrap (with ?token= query auth).
	bootResp := doReq(t, http.MethodGet, httpSrv.URL+"/api/v1/bootstrap?token="+token, "", nil)
	var boot deskapi.BootstrapPayload
	if err := json.NewDecoder(bootResp.Body).Decode(&boot); err != nil {
		t.Fatal(err)
	}
	_ = bootResp.Body.Close()
	if bootResp.StatusCode != http.StatusOK || len(boot.Tickets) != 2 || len(boot.UnplannedTrades) != 1 || len(boot.OpenPositions) != 1 {
		t.Fatalf("bootstrap mismatch: code=%d tickets=%d unplanned=%d pos=%d", bootResp.StatusCode, len(boot.Tickets), len(boot.UnplannedTrades), len(boot.OpenPositions))
	}
	if boot.Status.MarketCycle == nil || boot.Status.RiskConfig.TradingBankrollUSD != 60.0 {
		t.Fatalf("expected restored settings and market cycle: %+v", boot.Status)
	}

	// 4. Market bars: non-crypto rejection, cache hit + warm, cache miss + persist, before_ts.
	badSym := doReq(t, http.MethodGet, httpSrv.URL+"/api/v1/market/bars?symbol=AAPL/USDT:USDT&tf=5m", token, nil)
	_ = badSym.Body.Close()
	if badSym.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-crypto symbol, got %d", badSym.StatusCode)
	}

	barsHitResp := doReq(t, http.MethodGet, httpSrv.URL+"/api/v1/market/bars?symbol=BTCUSDT&tf=5m&limit=20", token, nil)
	var barsHit deskapi.MarketBarsResponse
	if err := json.NewDecoder(barsHitResp.Body).Decode(&barsHit); err != nil {
		t.Fatal(err)
	}
	_ = barsHitResp.Body.Close()
	if barsHitResp.StatusCode != http.StatusOK || len(barsHit.Bars) != 20 || barsHit.SymbolCycle == nil || len(barsHit.Metrics) != 0 {
		t.Fatalf("market/bars hit mismatch: code=%d bars=%d cycle=%v", barsHitResp.StatusCode, len(barsHit.Bars), barsHit.SymbolCycle)
	}

	// Cache miss on ETH/USDT:USDT triggers mock REST fetch + SQLite upsert.
	barsMissResp := doReq(t, http.MethodGet, httpSrv.URL+"/api/v1/market/bars?symbol=ETHUSDT&tf=5m&limit=10", token, nil)
	var barsMiss deskapi.MarketBarsResponse
	if err := json.NewDecoder(barsMissResp.Body).Decode(&barsMiss); err != nil {
		t.Fatal(err)
	}
	_ = barsMissResp.Body.Close()
	if barsMissResp.StatusCode != http.StatusOK || len(barsMiss.Bars) != 5 || klineCallCount.Load() == 0 {
		t.Fatalf("market/bars miss mismatch: code=%d len=%d klineCalls=%d", barsMissResp.StatusCode, len(barsMiss.Bars), klineCallCount.Load())
	}

	// before_ts pagination path.
	barsPageResp := doReq(t, http.MethodGet, httpSrv.URL+"/api/v1/market/bars?symbol=BTCUSDT&tf=5m&limit=5&before_ts="+strconvInt64(now-600), token, nil)
	_ = barsPageResp.Body.Close()
	if barsPageResp.StatusCode != http.StatusOK {
		t.Fatalf("before_ts status = %d", barsPageResp.StatusCode)
	}

	// 5. Drawings CRUD (`hline` & `ruler` + validation errors).
	dHline := doReq(t, http.MethodPost, httpSrv.URL+"/api/v1/drawings", token, deskapi.CreateDrawingRequest{
		Symbol: "BTCUSDT",
		Kind:   "hline",
		Points: []cache.ChartDrawingPoint{{Time: now - 3000, Price: 64500}},
		Label:  "entry-line",
	})
	var createdH cache.ChartDrawing
	_ = json.NewDecoder(dHline.Body).Decode(&createdH)
	_ = dHline.Body.Close()
	if dHline.StatusCode != http.StatusCreated || createdH.Kind != "hline" {
		t.Fatalf("create hline: status=%d item=%+v", dHline.StatusCode, createdH)
	}

	dRuler := doReq(t, http.MethodPost, httpSrv.URL+"/api/v1/drawings", token, deskapi.CreateDrawingRequest{
		Symbol: "BTCUSDT",
		Kind:   "ruler",
		Points: []cache.ChartDrawingPoint{{Time: now - 2000, Price: 64000}, {Time: now + 3600, Price: 66000}},
	})
	var createdR cache.ChartDrawing
	_ = json.NewDecoder(dRuler.Body).Decode(&createdR)
	_ = dRuler.Body.Close()
	if dRuler.StatusCode != http.StatusCreated || createdR.Kind != "ruler" {
		t.Fatalf("create ruler: status=%d item=%+v", dRuler.StatusCode, createdR)
	}

	badKind := doReq(t, http.MethodPost, httpSrv.URL+"/api/v1/drawings", token, deskapi.CreateDrawingRequest{Symbol: "BTCUSDT", Kind: "invalid"})
	_ = badKind.Body.Close()
	if badKind.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 invalid kind, got %d", badKind.StatusCode)
	}

	listD := doReq(t, http.MethodGet, httpSrv.URL+"/api/v1/drawings?symbol=BTCUSDT", token, nil)
	var drawings []cache.ChartDrawing
	_ = json.NewDecoder(listD.Body).Decode(&drawings)
	_ = listD.Body.Close()
	if len(drawings) != 2 {
		t.Fatalf("expected 2 drawings, got %d", len(drawings))
	}

	updLbl := "moved"
	updD := doReq(t, http.MethodPut, httpSrv.URL+"/api/v1/drawings/"+strconvInt64(createdH.ID), token, deskapi.UpdateDrawingRequest{
		Points: []cache.ChartDrawingPoint{{Time: now - 3000, Price: 64600}},
		Label:  &updLbl,
	})
	_ = updD.Body.Close()
	if updD.StatusCode != http.StatusOK {
		t.Fatalf("update drawing status = %d", updD.StatusCode)
	}
	missD := doReq(t, http.MethodPut, httpSrv.URL+"/api/v1/drawings/999999", token, deskapi.UpdateDrawingRequest{})
	_ = missD.Body.Close()
	if missD.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 drawing update, got %d", missD.StatusCode)
	}

	// 6. Replay state (zero-lookahead hides future ruler drawing & closed_at after cursor_ts).
	cursor := now - 2500
	repResp := doReq(t, http.MethodGet, httpSrv.URL+"/api/v1/replay/state?symbol=BTCUSDT&tfs=1m,5m,1h&cursor_ts="+strconvInt64(cursor), token, nil)
	var rep deskapi.ReplayStateResponse
	if err := json.NewDecoder(repResp.Body).Decode(&rep); err != nil {
		t.Fatal(err)
	}
	_ = repResp.Body.Close()
	if repResp.StatusCode != http.StatusOK || len(rep.Drawings) != 1 || len(rep.BarsByTF["1h"]) == 0 {
		t.Fatalf("replay state mismatch: code=%d drawings=%d 1h_bars=%d", repResp.StatusCode, len(rep.Drawings), len(rep.BarsByTF["1h"]))
	}
	if len(rep.Tickets) != 1 || rep.Tickets[0].Ticket.Status != types.TicketStatusAccepted {
		t.Fatalf("expected replay ticket rolled back to accepted at cursor_ts, got %+v", rep.Tickets)
	}

	// Delete drawings after replay test.
	delOne := doReq(t, http.MethodDelete, httpSrv.URL+"/api/v1/drawings/"+strconvInt64(createdH.ID), token, nil)
	_ = delOne.Body.Close()
	if delOne.StatusCode != http.StatusNoContent {
		t.Fatalf("delete drawing status = %d", delOne.StatusCode)
	}
	delSym := doReq(t, http.MethodDelete, httpSrv.URL+"/api/v1/drawings?symbol=BTCUSDT", token, nil)
	_ = delSym.Body.Close()
	if delSym.StatusCode != http.StatusNoContent {
		t.Fatalf("delete drawings by symbol status = %d", delSym.StatusCode)
	}

	// 7. Stats.
	statsResp := doReq(t, http.MethodGet, httpSrv.URL+"/api/v1/stats", token, nil)
	var stats deskapi.StatsPayload
	if err := json.NewDecoder(statsResp.Body).Decode(&stats); err != nil {
		t.Fatal(err)
	}
	_ = statsResp.Body.Close()
	if statsResp.StatusCode != http.StatusOK || stats.RDistribution.TotalTrades != 2 || stats.GateCompliance.UnplannedTradeCount != 1 {
		t.Fatalf("stats mismatch: code=%d rdist=%+v compliance=%+v", statsResp.StatusCode, stats.RDistribution, stats.GateCompliance)
	}
	if stats.TagBreakdown.SetupTags == nil || stats.TagBreakdown.ErrorTags == nil || stats.TagBreakdown.Grades == nil || stats.TagBreakdown.Emotions == nil {
		t.Fatalf("expected non-nil empty slices in TagBreakdown: %+v", stats.TagBreakdown)
	}

	// 8. Account sync 400 before credentials, Settings 400 & auto-split, then Account sync 200.
	syncNoKeys := doReq(t, http.MethodPost, httpSrv.URL+"/api/v1/account/sync", token, nil)
	_ = syncNoKeys.Body.Close()
	if syncNoKeys.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 NO_API_KEYS, got %d", syncNoKeys.StatusCode)
	}

	onlyKey := "dummy_only_key_1234"
	badSet := doReq(t, http.MethodPut, httpSrv.URL+"/api/v1/settings", token, deskapi.UpdateSettingsRequest{
		BinanceReadonlyAPIKey: &onlyKey,
	})
	_ = badSet.Body.Close()
	if badSet.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 INCOMPLETE_API_KEYS, got %d", badSet.StatusCode)
	}

	combined := "dummy_desk_key_1234 dummy_desk_sec_5678"
	newBankroll := 75.0
	newMaxLoss := 7.5
	newTZ := "UTC"
	okSet := doReq(t, http.MethodPut, httpSrv.URL+"/api/v1/settings", token, deskapi.UpdateSettingsRequest{
		TradingBankrollUSD:    &newBankroll,
		MaxLossUSD:            &newMaxLoss,
		Timezone:              &newTZ,
		BinanceReadonlyAPIKey: &combined,
	})
	_ = okSet.Body.Close()
	if okSet.StatusCode != http.StatusOK {
		t.Fatalf("settings auto-split status = %d", okSet.StatusCode)
	}

	// 9. System wake.
	wakeResp := doReq(t, http.MethodPost, httpSrv.URL+"/api/v1/system/wake", token, nil)
	_ = wakeResp.Body.Close()
	if wakeResp.StatusCode != http.StatusOK {
		t.Fatalf("system/wake status = %d", wakeResp.StatusCode)
	}

	// 10. WebSocket connect, subscribe, upstream kline push, ticker throttle, pulse/anomaly/notice/account_updated push!
	wsTarget := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/api/v1/ws?token=" + token
	wsCtx, wsCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer wsCancel()
	wsConn, _, err := websocket.Dial(wsCtx, wsTarget, nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer func() { _ = wsConn.Close(websocket.StatusNormalClosure, "") }()

	// Invalid JSON yields error frame.
	_ = wsConn.Write(wsCtx, websocket.MessageText, []byte(`{bad`))
	// Subscribe to BTCUSDT 5m.
	_ = wsConn.Write(wsCtx, websocket.MessageText, []byte(`{"type":"subscribe","symbol":"BTCUSDT","tfs":["5m"]}`))

	readFrame := func() map[string]any {
		_, data, err := wsConn.Read(wsCtx)
		if err != nil {
			t.Fatalf("ws read: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("ws unmarshal %q: %v", string(data), err)
		}
		return m
	}

	seen := map[string]int{}
	// Wait for error, subscribed, and kline frames from mock upstream WS.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && (seen["error"] == 0 || seen["subscribed"] == 0 || seen["kline"] == 0) {
		m := readFrame()
		tp, _ := m["type"].(string)
		seen[tp]++
	}
	if seen["error"] == 0 || seen["subscribed"] == 0 || seen["kline"] == 0 {
		t.Fatalf("missing expected initial ws frames: %+v", seen)
	}

	// Trigger account/sync -> broadcasts account_updated over WS.
	syncOK := doReq(t, http.MethodPost, httpSrv.URL+"/api/v1/account/sync", token, nil)
	var syncBody deskapi.AccountSyncResponse
	_ = json.NewDecoder(syncOK.Body).Decode(&syncBody)
	_ = syncOK.Body.Close()
	if syncOK.StatusCode != http.StatusOK || !syncBody.Ok || syncBody.Account.WalletBalanceUSD != 55.0 {
		t.Fatalf("account/sync failed: status=%d body=%+v", syncOK.StatusCode, syncBody)
	}

	// Trigger observer events: unsubscribed symbol skipped, same symbol throttled within 500ms, pulse/anomaly/notice delivered.
	px1, px2, pxOther := 65100.0, 65200.0, 100.0
	obs.OnTicker("binance", types.Ticker{Symbol: "DOGE/USDT:USDT", LastPrice: &pxOther})
	obs.OnTicker("binance", types.Ticker{Symbol: "BTC/USDT:USDT", LastPrice: &px1})
	obs.OnTicker("binance", types.Ticker{Symbol: "BTC/USDT:USDT", LastPrice: &px2}) // throttled (<500ms)
	obs.OnPulse(p.MarketPulseView())
	obs.OnAnomaly(types.AnomalyEvent{Symbol: "BTC/USDT:USDT", EventType: "price_velocity", Severity: types.SeverityHigh, Timestamp: float64(now)})
	obs.OnNotice(types.Notice{NoticeID: "nt-ws-1", Symbol: "BTC/USDT:USDT"})

	var tickerPrices []float64
	for time.Now().Before(deadline) && (seen["account_updated"] == 0 || seen["pulse"] == 0 || seen["anomaly"] == 0 || seen["notice"] == 0 || seen["ticker"] == 0) {
		m := readFrame()
		tp, _ := m["type"].(string)
		seen[tp]++
		if tp == "ticker" {
			if sym, _ := m["symbol"].(string); sym == "DOGE/USDT:USDT" {
				t.Fatal("unsubscribed DOGE ticker must not be delivered")
			}
			if pr, ok := m["price"].(float64); ok {
				tickerPrices = append(tickerPrices, pr)
			}
		}
	}
	if seen["account_updated"] == 0 || seen["pulse"] == 0 || seen["anomaly"] == 0 || seen["notice"] == 0 || seen["ticker"] == 0 {
		t.Fatalf("missing broadcast ws frames: %+v", seen)
	}
	if len(tickerPrices) != 1 || tickerPrices[0] != 65100.0 {
		t.Fatalf("expected 500ms throttle to deliver only first ticker price 65100, got %v", tickerPrices)
	}
}

func strconvInt64(v int64) string {
	return strconv.FormatInt(v, 10)
}
