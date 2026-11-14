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
	binanceRESTEndpoint = "https://fapi.binance.com"
	binanceWSEndpoint   = "wss://fstream.binance.com/ws"
)

type binanceExchange struct {
	httpCli *http.Client
	feed    feedState
	ws      wsLifecycle
}

func newBinance() *binanceExchange {
	return &binanceExchange{httpCli: newHTTPClient()}
}

func (b *binanceExchange) Name() string { return "binance" }

// FeedStats reports ticker-feed liveness for the silent-feed watchdog.
func (b *binanceExchange) FeedStats() FeedStats { return b.feed.stats() }

func (b *binanceExchange) SubscribeTickers(ctx context.Context, symbols []string, tickerCh chan<- types.Ticker) error {
	ctx, err := b.ws.begin(ctx, "binance")
	if err != nil {
		return err
	}

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
	backoff := wsInitialBackoff

	flog := newFeedLog("binance")
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		conn, _, err := websocket.Dial(ctx, uri, nil)
		if err != nil {
			flog.Warn("dial", "ws dial failed, retrying",
				"endpoint", binanceWSEndpoint, "symbols", len(streams), "error", err, "backoff", backoff)
			if err := ctxSleep(ctx, backoff); err != nil {
				return err
			}
			backoff = nextWSBackoff(backoff)
			continue
		}
		backoff = wsInitialBackoff
		flog.Info("ws connected", "endpoint", binanceWSEndpoint, "symbols", len(streams))
		// Binance carries the subscription in the dial URL, so "subscribed" is
		// reached as soon as the combined stream socket is up.
		flog.Info("ws subscribed", "endpoint", binanceWSEndpoint, "symbols", len(streams), "mode", "streams_in_url")
		b.feed.markSubscribed(time.Now())
		if err := b.readTickers(ctx, conn, tickerCh, lookup); err != nil {
			b.feed.markDisconnected()
			_ = conn.Close(websocket.StatusNormalClosure, "")
			if ctx.Err() != nil {
				return ctx.Err()
			}
			flog.Warn("read", "ws read ended, reconnecting",
				"endpoint", binanceWSEndpoint, "symbols", len(streams), "error", err, "backoff", time.Duration(0))
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
		if v := parseFloatPtr(row.PriceChangePercent); v != nil {
			t.ChangePct = v
		}
		result[sym] = t
	}

	if err := b.mergePremiumIndex(ctx, result); err != nil {
		return result, nil // funding optional
	}
	return result, nil
}

func (b *binanceExchange) FetchOHLCV(ctx context.Context, symbol, timeframe string, limit int, beforeMs int64) ([]types.Candle, error) {
	if limit <= 0 || limit > 1500 {
		limit = 500
	}
	url := fmt.Sprintf("%s/fapi/v1/klines?symbol=%s&interval=%s&limit=%d",
		binanceRESTEndpoint, binanceSymbol(symbol), mapBinanceTimeframe(timeframe), limit)
	if beforeMs > 0 {
		// endTime is inclusive; -1 makes the shared cursor contract exclusive.
		// Without startTime Binance returns the most recent bars up to endTime.
		url += fmt.Sprintf("&endTime=%d", beforeMs-1)
	}

	body, err := b.get(ctx, url)
	if err != nil {
		return nil, err
	}
	return parseBinanceKlines(body)
}

func (b *binanceExchange) Close() error {
	return b.ws.close()
}

func (b *binanceExchange) get(ctx context.Context, url string) ([]byte, error) {
	body, err := doHTTPGet(ctx, b.httpCli, url)
	if err != nil {
		return nil, fmt.Errorf("binance request: %w", err)
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
		if v := parseFloatPtr(row.LastFundingRate); v != nil {
			t.FundingRate = v
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
		b.feed.markMessage(time.Now())
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
		if v := parseFloatPtr(data.ChangePct); v != nil {
			t.ChangePct = v
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
