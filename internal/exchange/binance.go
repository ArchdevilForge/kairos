package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
	"github.com/coder/websocket"
)

const (
	binanceRESTEndpoint = "https://fapi.binance.com"
	binanceWSEndpoint   = "wss://fstream.binance.com/ws"
)

type binanceExchange struct {
	httpCli *http.Client
	mu      sync.Mutex
	cancel  context.CancelFunc
	closed  bool
}

func newBinance() *binanceExchange {
	return &binanceExchange{httpCli: newHTTPClient()}
}

func (b *binanceExchange) Name() string { return "binance" }

func (b *binanceExchange) SubscribeTickers(ctx context.Context, symbols []string, tickerCh chan<- types.Ticker) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return fmt.Errorf("binance: exchange closed")
	}
	ctx, cancel := context.WithCancel(ctx)
	b.cancel = cancel
	b.mu.Unlock()

	streams := make([]string, 0, len(symbols))
	lookup := make(map[string]string, len(symbols))
	for _, sym := range symbols {
		raw := strings.ToLower(binanceSymbol(sym))
		streams = append(streams, raw+"@ticker")
		lookup[strings.ToUpper(raw)] = sym
	}
	if len(streams) == 0 {
		return fmt.Errorf("binance: no symbols to subscribe")
	}

	uri := binanceWSEndpoint + "/" + strings.Join(streams, "/")
	backoff := time.Second
	maxBackoff := 30 * time.Second

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		conn, _, err := websocket.Dial(ctx, uri, nil)
		if err != nil {
			time.Sleep(backoff)
			backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
			continue
		}
		backoff = time.Second
		if err := b.readTickers(ctx, conn, tickerCh, lookup); err != nil {
			_ = conn.Close(websocket.StatusNormalClosure, "")
			continue
		}
	}
}

func (b *binanceExchange) FetchTickers(ctx context.Context) (map[string]*types.Ticker, error) {
	body, err := b.get(ctx, binanceRESTEndpoint+"/fapi/v1/ticker/24hr")
	if err != nil {
		return nil, err
	}
	var rows []struct {
		Symbol             string `json:"symbol"`
		LastPrice          string `json:"lastPrice"`
		QuoteVolume        string `json:"quoteVolume"`
		PriceChangePercent string `json:"priceChangePercent"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("binance fetch tickers: decode: %w", err)
	}

	result := make(map[string]*types.Ticker, len(rows))
	for _, row := range rows {
		sym := toCanonicalBinance(row.Symbol)
		if sym == "" {
			continue
		}
		t := &types.Ticker{Symbol: sym, Info: map[string]any{"instType": "SWAP"}}
		if v := parseFloat(row.LastPrice); v > 0 {
			t.LastPrice = &v
		}
		if v := parseFloat(row.QuoteVolume); v > 0 {
			t.QuoteVolume = &v
		}
		if v := parseFloat(row.PriceChangePercent); v != 0 {
			t.ChangePct = &v
		}
		result[sym] = t
	}

	if err := b.mergePremiumIndex(ctx, result); err != nil {
		return result, nil // funding optional
	}
	return result, nil
}

func (b *binanceExchange) FetchTicker(ctx context.Context, symbol string) (*types.Ticker, error) {
	url := fmt.Sprintf("%s/fapi/v1/ticker/24hr?symbol=%s", binanceRESTEndpoint, binanceSymbol(symbol))
	body, err := b.get(ctx, url)
	if err != nil {
		return nil, err
	}
	var row struct {
		Symbol             string `json:"symbol"`
		LastPrice          string `json:"lastPrice"`
		QuoteVolume        string `json:"quoteVolume"`
		PriceChangePercent string `json:"priceChangePercent"`
	}
	if err := json.Unmarshal(body, &row); err != nil {
		return nil, fmt.Errorf("binance fetch ticker: decode: %w", err)
	}
	sym := toCanonicalBinance(row.Symbol)
	t := &types.Ticker{Symbol: sym, Info: map[string]any{"instType": "SWAP"}}
	if v := parseFloat(row.LastPrice); v > 0 {
		t.LastPrice = &v
	}
	if v := parseFloat(row.QuoteVolume); v > 0 {
		t.QuoteVolume = &v
	}
	if v := parseFloat(row.PriceChangePercent); v != 0 {
		t.ChangePct = &v
	}
	one := map[string]*types.Ticker{sym: t}
	_ = b.mergePremiumIndex(ctx, one)
	return t, nil
}

func (b *binanceExchange) FetchOHLCV(ctx context.Context, symbol, timeframe string, limit int, sinceMs int64) ([]types.Candle, error) {
	if limit <= 0 || limit > 1500 {
		limit = 500
	}
	url := fmt.Sprintf("%s/fapi/v1/klines?symbol=%s&interval=%s&limit=%d",
		binanceRESTEndpoint, binanceSymbol(symbol), mapBinanceTimeframe(timeframe), limit)
	if sinceMs > 0 {
		url += fmt.Sprintf("&startTime=%d", sinceMs)
	}

	body, err := b.get(ctx, url)
	if err != nil {
		return nil, err
	}
	return parseBinanceKlines(body)
}

func (b *binanceExchange) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	if b.cancel != nil {
		b.cancel()
	}
	return nil
}

func (b *binanceExchange) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("binance request: %w", err)
	}
	resp, err := b.httpCli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("binance request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("binance read: %w", err)
	}
	return body, nil
}

func (b *binanceExchange) mergePremiumIndex(ctx context.Context, tickers map[string]*types.Ticker) error {
	body, err := b.get(ctx, binanceRESTEndpoint+"/fapi/v1/premiumIndex")
	if err != nil {
		return err
	}
	var rows []struct {
		Symbol          string `json:"symbol"`
		LastFundingRate string `json:"lastFundingRate"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return err
	}
	for _, row := range rows {
		sym := toCanonicalBinance(row.Symbol)
		t, ok := tickers[sym]
		if !ok || t == nil {
			continue
		}
		if v := parseFloat(row.LastFundingRate); v != 0 {
			t.FundingRate = &v
		}
	}
	return nil
}

func (b *binanceExchange) readTickers(ctx context.Context, conn *websocket.Conn, tickerCh chan<- types.Ticker, lookup map[string]string) error {
	for {
		_, msg, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		var data struct {
			EventType string `json:"e"`
			Symbol    string `json:"s"`
			LastPrice string `json:"c"`
			QuoteVol  string `json:"q"`
			ChangePct string `json:"P"`
		}
		if err := json.Unmarshal(msg, &data); err != nil {
			continue
		}
		if data.Symbol == "" {
			continue
		}
		sym := lookup[strings.ToUpper(data.Symbol)]
		if sym == "" {
			sym = toCanonicalBinance(data.Symbol)
		}
		t := types.Ticker{Symbol: sym}
		if v := parseFloat(data.LastPrice); v > 0 {
			t.LastPrice = &v
		}
		if v := parseFloat(data.QuoteVol); v > 0 {
			t.QuoteVolume = &v
		}
		if v := parseFloat(data.ChangePct); v != 0 {
			t.ChangePct = &v
		}
		select {
		case tickerCh <- t:
		default:
		}
	}
}

func mapBinanceTimeframe(tf string) string {
	switch tf {
	case "1m", "3m", "5m", "15m", "30m", "1h", "2h", "4h", "6h", "8h", "12h", "1d", "3d", "1w", "1M":
		return tf
	default:
		return "4h"
	}
}
