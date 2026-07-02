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
	bybitRESTEndpoint = "https://api.bybit.com"
	bybitWSEndpoint   = "wss://stream.bybit.com/v5/public/linear"
)

type bybitExchange struct {
	httpCli *http.Client
	mu      sync.Mutex
	cancel  context.CancelFunc
	closed  bool
}

func newBybit() *bybitExchange {
	return &bybitExchange{httpCli: newHTTPClient()}
}

func (b *bybitExchange) Name() string { return "bybit" }

func (b *bybitExchange) SubscribeTickers(ctx context.Context, symbols []string, tickerCh chan<- types.Ticker) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return fmt.Errorf("bybit: exchange closed")
	}
	ctx, cancel := context.WithCancel(ctx)
	b.cancel = cancel
	b.mu.Unlock()

	args := make([]string, 0, len(symbols))
	lookup := make(map[string]string, len(symbols))
	for _, sym := range symbols {
		raw := strings.ToUpper(bybitSymbol(sym))
		args = append(args, "tickers."+raw)
		lookup[raw] = sym
	}
	if len(args) == 0 {
		return fmt.Errorf("bybit: no symbols to subscribe")
	}

	backoff := time.Second
	maxBackoff := 30 * time.Second
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		conn, _, err := websocket.Dial(ctx, bybitWSEndpoint, nil)
		if err != nil {
			time.Sleep(backoff)
			backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
			continue
		}
		backoff = time.Second

		sub := map[string]any{"op": "subscribe", "args": args}
		raw, _ := json.Marshal(sub)
		if err := conn.Write(ctx, websocket.MessageText, raw); err != nil {
			_ = conn.Close(websocket.StatusNormalClosure, "")
			continue
		}
		if err := b.readTickers(ctx, conn, tickerCh, lookup); err != nil {
			_ = conn.Close(websocket.StatusNormalClosure, "")
			continue
		}
	}
}

func (b *bybitExchange) FetchTickers(ctx context.Context) (map[string]*types.Ticker, error) {
	url := bybitRESTEndpoint + "/v5/market/tickers?category=linear"
	body, err := b.get(ctx, url)
	if err != nil {
		return nil, err
	}
	var resp struct {
		RetCode int `json:"retCode"`
		Result  struct {
			List []struct {
				Symbol        string `json:"symbol"`
				LastPrice     string `json:"lastPrice"`
				Turnover24h   string `json:"turnover24h"`
				Price24hPcnt  string `json:"price24hPcnt"`
				FundingRate   string `json:"fundingRate"`
				OpenInterest  string `json:"openInterest"`
			} `json:"list"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("bybit fetch tickers: decode: %w", err)
	}
	if resp.RetCode != 0 {
		return nil, fmt.Errorf("bybit fetch tickers: retCode=%d", resp.RetCode)
	}

	result := make(map[string]*types.Ticker, len(resp.Result.List))
	for _, row := range resp.Result.List {
		sym := toCanonicalBybit(row.Symbol)
		if sym == "" {
			continue
		}
		t := &types.Ticker{Symbol: sym, Info: map[string]any{"instType": "SWAP"}}
		if v := parseFloat(row.LastPrice); v > 0 {
			t.LastPrice = &v
		}
		if v := parseFloat(row.Turnover24h); v > 0 {
			t.QuoteVolume = &v
		}
		if v := parseFloat(row.Price24hPcnt); v != 0 {
			pct := v * 100
			t.ChangePct = &pct
		}
		if v := parseFloat(row.OpenInterest); v > 0 {
			t.OpenInterest = &v
		}
		if v := parseFloat(row.FundingRate); v != 0 {
			t.FundingRate = &v
		}
		result[sym] = t
	}
	return result, nil
}

func (b *bybitExchange) FetchTicker(ctx context.Context, symbol string) (*types.Ticker, error) {
	url := fmt.Sprintf("%s/v5/market/tickers?category=linear&symbol=%s", bybitRESTEndpoint, bybitSymbol(symbol))
	body, err := b.get(ctx, url)
	if err != nil {
		return nil, err
	}
	var resp struct {
		RetCode int `json:"retCode"`
		Result  struct {
			List []struct {
				Symbol       string `json:"symbol"`
				LastPrice    string `json:"lastPrice"`
				Turnover24h  string `json:"turnover24h"`
				Price24hPcnt string `json:"price24hPcnt"`
				FundingRate  string `json:"fundingRate"`
				OpenInterest string `json:"openInterest"`
			} `json:"list"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("bybit fetch ticker: decode: %w", err)
	}
	if resp.RetCode != 0 || len(resp.Result.List) == 0 {
		return nil, fmt.Errorf("bybit fetch ticker: empty")
	}
	row := resp.Result.List[0]
	sym := toCanonicalBybit(row.Symbol)
	t := &types.Ticker{Symbol: sym, Info: map[string]any{"instType": "SWAP"}}
	if v := parseFloat(row.LastPrice); v > 0 {
		t.LastPrice = &v
	}
	if v := parseFloat(row.Turnover24h); v > 0 {
		t.QuoteVolume = &v
	}
	if v := parseFloat(row.Price24hPcnt); v != 0 {
		pct := v * 100
		t.ChangePct = &pct
	}
	if v := parseFloat(row.OpenInterest); v > 0 {
		t.OpenInterest = &v
	}
	if v := parseFloat(row.FundingRate); v != 0 {
		t.FundingRate = &v
	}
	return t, nil
}

func (b *bybitExchange) FetchOHLCV(ctx context.Context, symbol, timeframe string, limit int, sinceMs int64) ([]types.Candle, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	url := fmt.Sprintf("%s/v5/market/kline?category=linear&symbol=%s&interval=%s&limit=%d",
		bybitRESTEndpoint, bybitSymbol(symbol), mapBybitTimeframe(timeframe), limit)
	if sinceMs > 0 {
		url += fmt.Sprintf("&start=%d", sinceMs)
	}

	body, err := b.get(ctx, url)
	if err != nil {
		return nil, err
	}
	var resp struct {
		RetCode int `json:"retCode"`
		Result  struct {
			List [][]string `json:"list"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("bybit fetch ohlcv: decode: %w", err)
	}
	if resp.RetCode != 0 {
		return nil, fmt.Errorf("bybit fetch ohlcv: retCode=%d", resp.RetCode)
	}

	candles := make([]types.Candle, 0, len(resp.Result.List))
	for _, row := range resp.Result.List {
		if len(row) < 6 {
			continue
		}
		ts := int64(parseFloat(row[0]))
		candles = append(candles, types.Candle{
			Timestamp: ts / 1000,
			Open:      parseFloat(row[1]),
			High:      parseFloat(row[2]),
			Low:       parseFloat(row[3]),
			Close:     parseFloat(row[4]),
			Volume:    parseFloat(row[5]),
		})
	}
	sortCandlesAscending(candles)
	return candles, nil
}

func (b *bybitExchange) Close() error {
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

func (b *bybitExchange) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("bybit request: %w", err)
	}
	resp, err := b.httpCli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bybit request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return io.ReadAll(resp.Body)
}

func (b *bybitExchange) readTickers(ctx context.Context, conn *websocket.Conn, tickerCh chan<- types.Ticker, lookup map[string]string) error {
	for {
		_, msg, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		var raw struct {
			Op   string `json:"op"`
			Topic string `json:"topic"`
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(msg, &raw); err != nil {
			continue
		}
		if raw.Op == "ping" {
			_ = conn.Write(ctx, websocket.MessageText, []byte(`{"op":"pong"}`))
			continue
		}
		if raw.Data == nil || !strings.Contains(raw.Topic, "tickers") {
			continue
		}
		symbolRaw, _ := raw.Data["symbol"].(string)
		sym := lookup[strings.ToUpper(symbolRaw)]
		if sym == "" {
			sym = toCanonicalBybit(symbolRaw)
		}
		t := types.Ticker{Symbol: sym}
		if v := parseFloat(fmt.Sprint(raw.Data["lastPrice"])); v > 0 {
			t.LastPrice = &v
		}
		if v := parseFloat(fmt.Sprint(raw.Data["turnover24h"])); v > 0 {
			t.QuoteVolume = &v
		}
		select {
		case tickerCh <- t:
		default:
		}
	}
}

func mapBybitTimeframe(tf string) string {
	switch tf {
	case "1m":
		return "1"
	case "3m":
		return "3"
	case "5m":
		return "5"
	case "15m":
		return "15"
	case "30m":
		return "30"
	case "1h":
		return "60"
	case "2h":
		return "120"
	case "4h":
		return "240"
	case "6h":
		return "360"
	case "12h":
		return "720"
	case "1d":
		return "D"
	case "1w":
		return "W"
	case "1M":
		return "M"
	default:
		return "240"
	}
}
