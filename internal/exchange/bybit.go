package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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
	feed    feedState
	ws      wsLifecycle
}

func newBybit() *bybitExchange {
	return &bybitExchange{httpCli: newHTTPClient()}
}

func (b *bybitExchange) Name() string { return "bybit" }

// FeedStats reports ticker-feed liveness for the silent-feed watchdog.
func (b *bybitExchange) FeedStats() FeedStats { return b.feed.stats() }

func (b *bybitExchange) SubscribeTickers(ctx context.Context, symbols []string, tickerCh chan<- types.Ticker) error {
	ctx, err := b.ws.begin(ctx, "bybit")
	if err != nil {
		return err
	}

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

	backoff := wsInitialBackoff
	flog := newFeedLog("bybit")
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		conn, _, err := websocket.Dial(ctx, bybitWSEndpoint, nil)
		if err != nil {
			flog.Warn("dial", "ws dial failed, retrying",
				"endpoint", bybitWSEndpoint, "symbols", len(args), "error", err, "backoff", backoff)
			if err := ctxSleep(ctx, backoff); err != nil {
				return err
			}
			backoff = nextWSBackoff(backoff)
			continue
		}
		backoff = wsInitialBackoff
		flog.Info("ws connected", "endpoint", bybitWSEndpoint, "symbols", len(args))

		sub := map[string]any{"op": "subscribe", "args": args}
		raw, _ := json.Marshal(sub)
		if err := conn.Write(ctx, websocket.MessageText, raw); err != nil {
			flog.Warn("subscribe", "ws subscribe failed, retrying",
				"endpoint", bybitWSEndpoint, "symbols", len(args), "error", err, "backoff", time.Duration(0))
			b.feed.markDisconnected()
			_ = conn.Close(websocket.StatusNormalClosure, "")
			continue
		}
		flog.Info("ws subscribed", "endpoint", bybitWSEndpoint, "symbols", len(args))
		b.feed.markSubscribed(time.Now())
		if err := b.readTickers(ctx, conn, tickerCh, lookup); err != nil {
			b.feed.markDisconnected()
			_ = conn.Close(websocket.StatusNormalClosure, "")
			if ctx.Err() != nil {
				return ctx.Err()
			}
			flog.Warn("read", "ws read ended, reconnecting",
				"endpoint", bybitWSEndpoint, "symbols", len(args), "error", err, "backoff", time.Duration(0))
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
		if v := parseFloatPtr(row.Price24hPcnt); v != nil {
			pct := *v * 100
			t.ChangePct = &pct
		}
		if v := parseFloatPtr(row.OpenInterest); v != nil {
			t.OpenInterest = v
		}
		if v := parseFloatPtr(row.FundingRate); v != nil {
			t.FundingRate = v
		}
		result[sym] = t
	}
	return result, nil
}

func (b *bybitExchange) FetchOHLCV(ctx context.Context, symbol, timeframe string, limit int, beforeMs int64) ([]types.Candle, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	url := fmt.Sprintf("%s/v5/market/kline?category=linear&symbol=%s&interval=%s&limit=%d",
		bybitRESTEndpoint, bybitSymbol(symbol), mapBybitTimeframe(timeframe), limit)
	if beforeMs > 0 {
		// `end` is inclusive; -1 makes the shared cursor contract exclusive.
		// With only `end` set Bybit returns the most recent bars up to it.
		url += fmt.Sprintf("&end=%d", beforeMs-1)
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
		// Bybit /v5/market/kline?category=linear: [startTime, open, high, low, close, volume, turnover].
		// row[6] is turnover in quote coin (USDT for linear USDT perpetuals).
		var quoteVol float64
		if len(row) > 6 {
			quoteVol = parseFloat(row[6])
		}
		candles = append(candles, types.Candle{
			Timestamp: ts / 1000,
			Open:      parseFloat(row[1]),
			High:      parseFloat(row[2]),
			Low:       parseFloat(row[3]),
			Close:     parseFloat(row[4]),
			Volume:    parseFloat(row[5]),
			QuoteVol:  quoteVol,
		})
	}
	sortCandlesAscending(candles)
	return candles, nil
}

func (b *bybitExchange) Close() error {
	return b.ws.close()
}

func (b *bybitExchange) get(ctx context.Context, url string) ([]byte, error) {
	body, err := doHTTPGet(ctx, b.httpCli, url)
	if err != nil {
		return nil, fmt.Errorf("bybit request: %w", err)
	}
	return body, nil
}

func (b *bybitExchange) readTickers(ctx context.Context, conn *websocket.Conn, tickerCh chan<- types.Ticker, lookup map[string]string) error {
	for {
		_, msg, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		b.feed.markMessage(time.Now())
		var raw struct {
			Op    string         `json:"op"`
			Topic string         `json:"topic"`
			Data  map[string]any `json:"data"`
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
