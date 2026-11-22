package exchange

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ArchdevilForge/kairos/internal/cache"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBybitTradablePerpIncludesStocksWhileCryptoOnlyExcludes(t *testing.T) {
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body string
		switch req.URL.Path {
		case "/v5/market/instruments-info":
			body = `{"retCode":0,"result":{"nextPageCursor":"","list":[
				{"symbol":"BTCUSDT","contractType":"LinearPerpetual","status":"Trading","quoteCoin":"USDT","settleCoin":"USDT","launchTime":"1600000000000","symbolType":""},
				{"symbol":"NVDAUSDT","contractType":"LinearPerpetual","status":"Trading","quoteCoin":"USDT","settleCoin":"USDT","launchTime":"1700000000000","symbolType":"stock"},
				{"symbol":"QQQUSDT","contractType":"LinearPerpetual","status":"Trading","quoteCoin":"USDT","settleCoin":"USDT","launchTime":"1700000000000","symbolType":"ETF"}
			]}}`
		case "/v5/market/tickers":
			body = `{"retCode":0,"result":{"list":[
				{"symbol":"BTCUSDT","turnover24h":"5000000000"},
				{"symbol":"NVDAUSDT","turnover24h":"120000000"},
				{"symbol":"QQQUSDT","turnover24h":"50000000"}
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

	cleanup := SetBybitTestHooks(rt, "")
	defer cleanup()

	ctx := context.Background()
	cryptoList, cryptoAllow, err := FetchCryptoPerpSymbols(ctx, "bybit")
	if err != nil {
		t.Fatalf("FetchCryptoPerpSymbols: %v", err)
	}
	if !cryptoAllow["BTC/USDT:USDT"] || cryptoAllow["NVDA/USDT:USDT"] || cryptoAllow["QQQ/USDT:USDT"] {
		t.Fatalf("crypto allowlist should exclude stocks/ETFs: list=%v allow=%v", cryptoList, cryptoAllow)
	}

	tradableList, tradableAllow, err := FetchTradablePerpSymbols(ctx, "bybit")
	if err != nil {
		t.Fatalf("FetchTradablePerpSymbols: %v", err)
	}
	if !tradableAllow["BTC/USDT:USDT"] || !tradableAllow["NVDA/USDT:USDT"] || !tradableAllow["QQQ/USDT:USDT"] {
		t.Fatalf("tradable allowlist should include crypto + stock + ETF: list=%v allow=%v", tradableList, tradableAllow)
	}
	if len(tradableList) != 3 || tradableList[0] != "BTC/USDT:USDT" || tradableList[1] != "NVDA/USDT:USDT" {
		t.Fatalf("expected turnover-sorted tradable list, got %v", tradableList)
	}
}

func TestAggregateTradesComputesFIFORealizedPnLWhenExecPnLOmitted(t *testing.T) {
	fills := []cache.TradeFill{
		{
			Exchange:        "bybit",
			FillID:          "bybit:NVDA/USDT:USDT:exec1",
			OrderID:         "ord-open",
			Symbol:          "NVDA/USDT:USDT",
			Side:            "BUY",
			PositionSide:    "BOTH",
			Price:           128.03,
			Qty:             0.78,
			QuoteQty:        99.8634,
			Commission:      0.027462435,
			CommissionAsset: "USDT",
			RealizedPnL:     0, // Bybit execution/list omits execPnl
			TimeMs:          1776000000000,
		},
		{
			Exchange:        "bybit",
			FillID:          "bybit:NVDA/USDT:USDT:exec2",
			OrderID:         "ord-close",
			Symbol:          "NVDA/USDT:USDT",
			Side:            "SELL",
			PositionSide:    "BOTH",
			Price:           130.08,
			Qty:             0.78,
			QuoteQty:        101.4624,
			Commission:      0.027902165,
			CommissionAsset: "USDT",
			RealizedPnL:     0,
			TimeMs:          1776003600000,
		},
	}

	trades, _ := AggregateTrades(AggregateTradesInput{
		Exchange: "bybit",
		Fills:    fills,
		ClosedHints: []ClosedTradeHint{
			{
				Symbol:      "NVDA/USDT:USDT",
				OrderID:     "ord-close",
				Leverage:    10,
				UpdatedAtMs: 1776003600000,
			},
		},
		DefaultMarginMode: "cross",
	})

	if len(trades) != 1 {
		t.Fatalf("expected 1 closed trade, got %d", len(trades))
	}
	tr := trades[0]
	wantGross := (130.08 - 128.03) * 0.78
	wantNet := wantGross - (0.027462435 + 0.027902165)
	if math.Abs(tr.RealizedPnL-wantGross) > 1e-6 {
		t.Fatalf("RealizedPnL = %.8f, want %.8f", tr.RealizedPnL, wantGross)
	}
	if math.Abs(tr.NetPnL-wantNet) > 1e-6 {
		t.Fatalf("NetPnL = %.8f, want %.8f", tr.NetPnL, wantNet)
	}
	if tr.Leverage != 10 {
		t.Fatalf("Leverage = %v, want 10", tr.Leverage)
	}
	wantMargin := (128.03 * 0.78) / 10.0
	if math.Abs(tr.Margin-wantMargin) > 1e-6 {
		t.Fatalf("Margin = %.6f, want %.6f", tr.Margin, wantMargin)
	}
}

func TestBybitIncrementalSyncPreservesLeverageAndEnrichedPositionFields(t *testing.T) {
	var syncPhase atomic.Int32
	syncPhase.Store(1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		phase := syncPhase.Load()
		switch r.URL.Path {
		case "/v5/account/info":
			_, _ = w.Write([]byte(`{"retCode":0,"result":{"unifiedMarginStatus":4,"marginMode":"REGULAR_MARGIN"}}`))
		case "/v5/account/wallet-balance":
			_, _ = w.Write([]byte(`{"retCode":0,"result":{"list":[{"accountType":"UNIFIED","totalEquity":"469.86","totalWalletBalance":"470.81","totalAvailableBalance":"413.42","totalInitialMargin":"56.44","totalMarginBalance":"469.86","totalPerpUPL":"-0.98","coin":[{"coin":"USDT","walletBalance":"471.49","unrealisedPnl":"-0.98","equity":"470.51"}]}]}}`))
		case "/v5/position/list":
			_, _ = w.Write([]byte(`{"retCode":0,"result":{"nextPageCursor":"","list":[{"symbol":"ETHUSDT","side":"Sell","size":"0.33","avgPrice":"1699.66","markPrice":"1702.63","unrealisedPnl":"-0.98","liqPrice":"3119.72","leverage":"10","tradeMode":0,"positionIM":"56.53","positionBalance":"0","positionValue":"561.87","stopLoss":"1731.95","takeProfit":"1605","breakEvenPrice":"1697.79","curRealisedPnl":"-0.31","positionIdx":0,"updatedTime":"1776007200000"}]}}`))
		case "/v5/execution/list":
			if phase == 1 {
				_, _ = w.Write([]byte(`{"retCode":0,"result":{"nextPageCursor":"","list":[
					{"execId":"nv1","orderId":"o-nv-1","symbol":"NVDAUSDT","side":"Buy","execPrice":"128.03","execQty":"0.78","execValue":"99.8634","execFee":"0.0274624","feeCurrency":"USDT","execType":"Trade","execTime":"1776000000000"},
					{"execId":"nv2","orderId":"o-nv-2","symbol":"NVDAUSDT","side":"Sell","execPrice":"130.08","execQty":"0.78","execValue":"101.4624","execFee":"0.0279022","feeCurrency":"USDT","execType":"Trade","execTime":"1776003600000"}
				]}}`))
			} else {
				_, _ = w.Write([]byte(`{"retCode":0,"result":{"nextPageCursor":"","list":[
					{"execId":"eth1","orderId":"o-eth-1","symbol":"ETHUSDT","side":"Sell","execPrice":"1699.66","execQty":"0.33","execValue":"560.8878","execFee":"0.308488","feeCurrency":"USDT","execType":"Trade","execTime":"1776007200000"}
				]}}`))
			}
		case "/v5/position/closed-pnl":
			if phase == 1 {
				_, _ = w.Write([]byte(`{"retCode":0,"result":{"nextPageCursor":"","list":[
					{"symbol":"NVDAUSDT","orderId":"o-nv-2","side":"Buy","qty":"0.78","avgEntryPrice":"128.03","avgExitPrice":"130.08","closedSize":"0.78","closedPnl":"1.5436354","leverage":"10","updatedTime":"1776003600000"}
				]}}`))
			} else {
				// Incremental window no longer covers older closed NVDA trade!
				_, _ = w.Write([]byte(`{"retCode":0,"result":{"nextPageCursor":"","list":[]}}`))
			}
		case "/v5/account/transaction-log":
			_, _ = w.Write([]byte(`{"retCode":0,"result":{"nextPageCursor":"","list":[]}}`))
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"retCode": 0, "result": map[string]any{}})
		}
	}))
	defer srv.Close()

	store, err := cache.Open(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	cli := &BybitAccountClient{
		apiKey:    "test-key",
		apiSecret: "test-secret",
		baseURL:   srv.URL,
		httpCli:   srv.Client(),
	}

	ctx := context.Background()
	if _, err := cli.SyncToCache(ctx, store, nil, nil); err != nil {
		t.Fatalf("sync phase 1: %v", err)
	}

	// Phase 2: new open ETH position arrives, incremental closed-pnl returns empty.
	syncPhase.Store(2)
	if _, err := cli.SyncToCache(ctx, store, nil, nil); err != nil {
		t.Fatalf("sync phase 2: %v", err)
	}

	trades, err := store.ListSyncedTradesForExchange(ctx, "bybit", "NVDA/USDT:USDT")
	if err != nil || len(trades) != 1 {
		t.Fatalf("expected 1 NVDA trade, got err=%v len=%d", err, len(trades))
	}
	if trades[0].Leverage != 10 {
		t.Fatalf("incremental sync wiped historical NVDA leverage: got %v, want 10", trades[0].Leverage)
	}
	if trades[0].NetPnL <= 1.5 {
		t.Fatalf("expected positive NVDA net PnL (~1.54), got %.6f", trades[0].NetPnL)
	}

	positions, err := store.ListOpenPositionsForExchange(ctx, "bybit")
	if err != nil || len(positions) != 1 {
		t.Fatalf("expected 1 open ETH position, got err=%v len=%d", err, len(positions))
	}
	p := positions[0]
	if p.StopLoss != 1731.95 || p.TakeProfit != 1605 || p.PositionValue != 561.87 || p.BreakEvenPrice != 1697.79 {
		t.Fatalf("missing enriched Bybit position fields: %+v", p)
	}

	meta, err := store.GetAllExchangeMeta(ctx, "bybit")
	if err != nil {
		t.Fatal(err)
	}
	if meta["total_equity_usd"] == "" || meta["available_balance_usd"] == "" || meta["initial_margin_usd"] == "" {
		t.Fatalf("expected UTA wallet metrics persisted in account_meta, got %+v", meta)
	}
}
