package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ArchdevilForge/kairos/internal/types"
	"github.com/coder/websocket"
)

var (
	_ KlineSubscriber = (*binanceExchange)(nil)
	_ KlineSubscriber = (*bybitExchange)(nil)
)

// SubscribeKline subscribes to Bybit V5 public linear kline stream
// (wss://stream.bybit.com/v5/public/linear, topic kline.<interval>.<SYM>)
// for a single symbol and timeframe until ctx is cancelled or the adapter closes.
func (b *bybitExchange) SubscribeKline(ctx context.Context, symbol, tf string, onKline func(types.Candle, bool)) error {
	if b.ws.isClosed() {
		return fmt.Errorf("bybit: exchange closed")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	rawSym := strings.ToUpper(bybitSymbol(symbol))
	if rawSym == "" {
		return fmt.Errorf("bybit kline ws: empty symbol")
	}

	topic := "kline." + mapBybitTimeframe(tf) + "." + rawSym
	ep := b.effectiveWSEndpoint()
	pingInterval := b.effectivePingInterval()
	backoff := wsInitialBackoff
	flog := newFeedLog("bybit-kline")

	for {
		if b.ws.isClosed() {
			return fmt.Errorf("bybit: exchange closed")
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		conn, _, err := websocket.Dial(ctx, ep, newWSDialOptions())
		if err != nil {
			flog.Warn("dial", "kline ws dial failed, retrying",
				"endpoint", ep, "symbol", symbol, "tf", tf, "error", err, "backoff", backoff)
			if err := ctxSleep(ctx, backoff); err != nil {
				return err
			}
			backoff = nextWSBackoff(backoff)
			continue
		}

		wconn := &bybitWSConn{conn: conn}
		sub := map[string]any{"op": "subscribe", "args": []string{topic}}
		if err := wconn.writeJSON(ctx, sub); err != nil {
			_ = conn.Close(websocket.StatusNormalClosure, "")
			if ctx.Err() != nil {
				return ctx.Err()
			}
			flog.Warn("subscribe", "kline ws subscribe failed, retrying",
				"endpoint", ep, "symbol", symbol, "tf", tf, "error", err, "backoff", backoff)
			if sleepErr := ctxSleep(ctx, backoff); sleepErr != nil {
				return sleepErr
			}
			backoff = nextWSBackoff(backoff)
			continue
		}

		stopPing := startBybitPingLoop(ctx, wconn, pingInterval)
		gotData, err := readBybitKlineLoop(ctx, wconn, topic, onKline)
		stopPing()
		_ = conn.CloseNow()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if gotData {
			backoff = wsInitialBackoff
		}
		if err != nil {
			flog.Warn("read", "kline ws read ended, reconnecting",
				"endpoint", ep, "symbol", symbol, "tf", tf, "error", err, "backoff", backoff)
		}
		if sleepErr := ctxSleep(ctx, backoff); sleepErr != nil {
			return sleepErr
		}
		backoff = nextWSBackoff(backoff)
	}
}

// SubscribeKlineForExchange subscribes to live kline updates on the named exchange
// ("binance" or "bybit", defaulting to "bybit" when empty/unknown).
func SubscribeKlineForExchange(ctx context.Context, exchangeName, symbol, tf string, onKline func(types.Candle, bool)) error {
	switch strings.ToLower(strings.TrimSpace(exchangeName)) {
	case "binance":
		return SubscribeKline(ctx, symbol, tf, onKline)
	default:
		return newBybit().SubscribeKline(ctx, symbol, tf, onKline)
	}
}

func readBybitKlineLoop(ctx context.Context, wconn *bybitWSConn, wantTopic string, onKline func(types.Candle, bool)) (bool, error) {
	gotData := false
	for {
		_, msg, err := wconn.conn.Read(ctx)
		if err != nil {
			return gotData, err
		}
		handled, ctrlErr := handleBybitControlFrame(ctx, wconn, msg)
		if ctrlErr != nil {
			return gotData, ctrlErr
		}
		if handled {
			continue
		}

		candles, confirms, ok := parseBybitKlineWSMessage(msg, wantTopic)
		if !ok {
			continue
		}
		gotData = true
		if onKline != nil {
			for i, c := range candles {
				onKline(c, confirms[i])
			}
		}
	}
}

func parseBybitKlineWSMessage(msg []byte, wantTopic string) ([]types.Candle, []bool, bool) {
	var payload struct {
		Topic string `json:"topic"`
		Data  []struct {
			Start    int64  `json:"start"`
			Open     string `json:"open"`
			High     string `json:"high"`
			Low      string `json:"low"`
			Close    string `json:"close"`
			Volume   string `json:"volume"`
			Turnover string `json:"turnover"`
			Confirm  bool   `json:"confirm"`
		} `json:"data"`
	}
	if err := json.Unmarshal(msg, &payload); err != nil {
		return nil, nil, false
	}
	if !strings.HasPrefix(payload.Topic, "kline.") {
		return nil, nil, false
	}
	if wantTopic != "" && payload.Topic != wantTopic {
		return nil, nil, false
	}
	if len(payload.Data) == 0 {
		return nil, nil, false
	}

	candles := make([]types.Candle, 0, len(payload.Data))
	confirms := make([]bool, 0, len(payload.Data))
	for _, item := range payload.Data {
		if item.Start <= 0 {
			continue
		}
		candles = append(candles, types.Candle{
			Timestamp: item.Start / 1000,
			Open:      parseFloat(item.Open),
			High:      parseFloat(item.High),
			Low:       parseFloat(item.Low),
			Close:     parseFloat(item.Close),
			Volume:    parseFloat(item.Volume),
			QuoteVol:  parseFloat(item.Turnover),
		})
		confirms = append(confirms, item.Confirm)
	}
	if len(candles) == 0 {
		return nil, nil, false
	}
	return candles, confirms, true
}
