package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
	"github.com/coder/websocket"
)

// ---------------------------------------------------------------------------
// OKX exchange — WebSocket ticker subscription + REST market data
// ---------------------------------------------------------------------------

const (
	okxWSEndpoint   = "wss://ws.okx.com:8443/ws/v5/public"
	okxRESTEndpoint = "https://www.okx.com"
)

type okxExchange struct {
	name    string
	httpCli *http.Client
	mu      sync.Mutex
	cancel  context.CancelFunc
	closed  bool
}

func newOKX() *okxExchange {
	return &okxExchange{
		name:    "okx",
		httpCli: newHTTPClient(),
	}
}

// ---------------------------------------------------------------------------
// Exchange interface
// ---------------------------------------------------------------------------

func (o *okxExchange) Name() string { return o.name }

func (o *okxExchange) SubscribeTickers(ctx context.Context, symbols []string, tickerCh chan<- types.Ticker) error {
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return fmt.Errorf("okx: exchange closed")
	}
	ctx, cancel := context.WithCancel(ctx)
	o.cancel = cancel
	o.mu.Unlock()

	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second

	// Build subscription args once — symbol conversion is stable.
	type subArg struct {
		Channel string `json:"channel"`
		InstID  string `json:"instId"`
	}
	args := make([]subArg, 0, len(symbols))
	for _, sym := range symbols {
		args = append(args, subArg{Channel: "tickers", InstID: normalizeSymbol(sym)})
	}

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		conn, _, err := websocket.Dial(ctx, okxWSEndpoint, nil)
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			time.Sleep(backoff)
			backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
			continue
		}
		backoff = 1 * time.Second // reset on successful connect

		// Subscribe
		sub := map[string]any{"op": "subscribe", "args": args}
		raw, _ := json.Marshal(sub)
		if err := conn.Write(ctx, websocket.MessageText, raw); err != nil {
			_ = conn.Close(websocket.StatusNormalClosure, "")
			continue
		}

		// Read loop
		if err := o.readTickers(ctx, conn, tickerCh); err != nil {
			_ = conn.Close(websocket.StatusNormalClosure, "")
			continue
		}
	}
}

func (o *okxExchange) FetchTickers(ctx context.Context) (map[string]*types.Ticker, error) {
	url := okxRESTEndpoint + "/api/v5/market/tickers?instType=SWAP"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("okx fetch tickers: %w", err)
	}

	resp, err := o.httpCli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("okx fetch tickers: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("okx fetch tickers: read: %w", err)
	}

	var okxResp struct {
		Code string            `json:"code"`
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &okxResp); err != nil {
		return nil, fmt.Errorf("okx fetch tickers: decode: %w", err)
	}
	if okxResp.Code != "0" {
		return nil, fmt.Errorf("okx fetch tickers: code=%s", okxResp.Code)
	}

	result := make(map[string]*types.Ticker, len(okxResp.Data))
	for _, raw := range okxResp.Data {
		var item struct {
			InstID   string `json:"instId"`
			Last     string `json:"last"`
			Vol24h   string `json:"vol24h"`
			VolCcy24 string `json:"volCcy24h"`
			Open24h  string `json:"open24h"`
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			continue
		}
		sym := denormalizeSymbol(item.InstID)
		if sym == "" {
			continue
		}
		t := &types.Ticker{Symbol: sym, Info: make(map[string]any)}

		if v := parseFloat(item.Last); v > 0 {
			t.LastPrice = &v
		}
		if v := parseFloat(item.VolCcy24); v > 0 {
			t.QuoteVolume = &v
		}
		if o, l := parseFloat(item.Open24h), parseFloat(item.Last); o > 0 && l > 0 {
			pct := (l - o) / o * 100
			t.ChangePct = &pct
		}
		result[sym] = t
	}
	enrichOKXMetrics(ctx, o.httpCli, result)
	return result, nil
}

func (o *okxExchange) FetchOHLCV(ctx context.Context, symbol, timeframe string, limit int, sinceMs int64) ([]types.Candle, error) {
	instID := normalizeSymbol(symbol)
	bar := mapTimeframe(timeframe)
	if limit <= 0 || limit > 300 {
		limit = 100 // OKX max is 300
	}

	url := fmt.Sprintf("%s/api/v5/market/candles?instId=%s&bar=%s&limit=%d",
		okxRESTEndpoint, instID, bar, limit)
	if sinceMs > 0 {
		url += fmt.Sprintf("&after=%d", sinceMs)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("okx fetch ohlcv: %w", err)
	}

	resp, err := o.httpCli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("okx fetch ohlcv: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("okx fetch ohlcv: read: %w", err)
	}

	var okxResp struct {
		Code string     `json:"code"`
		Data [][]string `json:"data"`
	}
	if err := json.Unmarshal(body, &okxResp); err != nil {
		return nil, fmt.Errorf("okx fetch ohlcv: decode: %w", err)
	}
	if okxResp.Code != "0" {
		return nil, fmt.Errorf("okx fetch ohlcv: code=%s", okxResp.Code)
	}

	candles := make([]types.Candle, 0, len(okxResp.Data))
	for _, row := range okxResp.Data {
		// row: [ts, o, h, l, c, vol, volCcy, volCcyQuote, confirm]
		if len(row) < 6 {
			continue
		}
		ts, _ := strconv.ParseInt(row[0], 10, 64)
		candles = append(candles, types.Candle{
			Timestamp: ts / 1000, // ms → seconds
			Open:      parseFloat(row[1]),
			High:      parseFloat(row[2]),
			Low:       parseFloat(row[3]),
			Close:     parseFloat(row[4]),
			Volume:    parseFloat(row[5]),
		})
	}
	return candles, nil
}

func (o *okxExchange) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return nil
	}
	o.closed = true
	if o.cancel != nil {
		o.cancel()
	}
	return nil
}

// ---------------------------------------------------------------------------
// WS read loop
// ---------------------------------------------------------------------------

func (o *okxExchange) readTickers(ctx context.Context, conn *websocket.Conn, tickerCh chan<- types.Ticker) error {
	for {
		_, msg, err := conn.Read(ctx)
		if err != nil {
			return err
		}

		var raw struct {
			Event string          `json:"event"`
			Data  []json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(msg, &raw); err != nil {
			continue
		}

		// Application-level ping from OKX
		if raw.Event == "ping" {
			_ = conn.Write(ctx, websocket.MessageText, []byte(`{"event":"pong"}`))
			continue
		}

		for _, item := range raw.Data {
			var tick struct {
				InstID   string `json:"instId"`
				Last     string `json:"last"`
				Vol24h   string `json:"vol24h"`
				VolCcy24 string `json:"volCcy24h"`
				Open24h  string `json:"open24h"`
				Ts       string `json:"ts"`
			}
			if err := json.Unmarshal(item, &tick); err != nil {
				continue
			}

			sym := denormalizeSymbol(tick.InstID)
			if sym == "" {
				continue
			}

			t := types.Ticker{Symbol: sym}
			if v := parseFloat(tick.Last); v > 0 {
				t.LastPrice = &v
			}
			if v := parseFloat(tick.VolCcy24); v > 0 {
				t.QuoteVolume = &v
			}
			if o, l := parseFloat(tick.Open24h), parseFloat(tick.Last); o > 0 && l > 0 {
				pct := (l - o) / o * 100
				t.ChangePct = &pct
			}

			select {
			case tickerCh <- t:
			default:
				// ponytail: drop if nobody reading — caller's job to size buffer
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Symbol normalization helpers
// ---------------------------------------------------------------------------

// normalizeSymbol converts "BTC/USDT:USDT" → "BTC-USDT-SWAP".
func normalizeSymbol(symbol string) string {
	// Already in OKX format
	if strings.HasSuffix(symbol, "-USDT-SWAP") {
		return symbol
	}
	// CCXT-style "BTC/USDT:USDT"
	if idx := strings.Index(symbol, "/"); idx >= 0 {
		base := symbol[:idx]
		return base + "-USDT-SWAP"
	}
	return symbol
}

// denormalizeSymbol converts "BTC-USDT-SWAP" → "BTC/USDT:USDT".
func denormalizeSymbol(instID string) string {
	if !strings.HasSuffix(instID, "-SWAP") {
		return ""
	}
	base := strings.TrimSuffix(instID, "-USDT-SWAP")
	if base == instID || base == "" {
		return ""
	}
	return base + "/USDT:USDT"
}

// ---------------------------------------------------------------------------
// Timeframe mapping helpers
// ---------------------------------------------------------------------------

// mapTimeframe converts a canonical timeframe ("1m", "4h", "1d") to OKX bar format.
func mapTimeframe(tf string) string {
	switch tf {
	case "1m", "3m", "5m", "15m", "30m":
		return tf
	case "1h":
		return "1H"
	case "2h":
		return "2H"
	case "4h":
		return "4H"
	case "6h":
		return "6H"
	case "12h":
		return "12H"
	case "1d":
		return "1D"
	case "1w":
		return "1W"
	case "1M":
		return "1M"
	default:
		return "1H"
	}
}

func enrichOKXMetrics(ctx context.Context, cli *http.Client, tickers map[string]*types.Ticker) {
	body, err := okxGET(ctx, cli, okxRESTEndpoint+"/api/v5/public/open-interest?instType=SWAP")
	if err != nil {
		return
	}
	var resp struct {
		Code string `json:"code"`
		Data []struct {
			InstID string `json:"instId"`
			OI     string `json:"oi"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &resp) != nil || resp.Code != "0" {
		return
	}
	for _, row := range resp.Data {
		sym := denormalizeSymbol(row.InstID)
		t, ok := tickers[sym]
		if !ok || t == nil {
			continue
		}
		if v := parseFloat(row.OI); v > 0 {
			t.OpenInterest = &v
		}
	}

	for sym, t := range tickers {
		if t == nil {
			continue
		}
		instID := normalizeSymbol(sym)
		fBody, err := okxGET(ctx, cli, okxRESTEndpoint+"/api/v5/public/funding-rate?instId="+instID)
		if err != nil {
			continue
		}
		var fResp struct {
			Code string `json:"code"`
			Data []struct {
				FundingRate string `json:"fundingRate"`
			} `json:"data"`
		}
		if json.Unmarshal(fBody, &fResp) != nil || fResp.Code != "0" || len(fResp.Data) == 0 {
			continue
		}
		if v := parseFloat(fResp.Data[0].FundingRate); v != 0 {
			t.FundingRate = &v
		}
	}
}

func okxGET(ctx context.Context, cli *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return io.ReadAll(resp.Body)
}
