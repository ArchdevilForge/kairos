package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
	"github.com/coder/websocket"
)

func (o *okxExchange) SubscribeKline(ctx context.Context, symbol, tf string, onKline func(types.Candle, bool)) error {
	endpoint := strings.TrimSuffix(o.wsEndpoint, "/")
	if strings.HasSuffix(endpoint, "/public") {
		endpoint = strings.TrimSuffix(endpoint, "/public") + "/business"
	}
	sub, _ := json.Marshal(map[string]any{
		"op": "subscribe",
		"args": []map[string]string{{
			"channel": "candle" + mapTimeframe(tf),
			"instId":  normalizeSymbol(symbol),
		}},
	})
	backoff := wsInitialBackoff
	flog := newFeedLog("okx-kline")
	for ctx.Err() == nil {
		conn, _, err := websocket.Dial(ctx, endpoint, newWSDialOptions())
		if err == nil {
			readCtx, stop := okxHeartbeat(ctx, conn)
			err = conn.Write(readCtx, websocket.MessageText, sub)
			if err == nil {
				err = readOKXKlines(readCtx, conn, func(c types.Candle, closed bool) {
					backoff = wsInitialBackoff
					onKline(c, closed)
				})
			}
			stop()
			_ = conn.CloseNow()
		}
		if ctx.Err() != nil {
			break
		}
		flog.Warn("stream", "kline stream ended, retrying", "symbol", symbol, "tf", tf, "error", err, "backoff", backoff)
		if err := ctxSleep(ctx, backoff); err != nil {
			return err
		}
		backoff = nextWSBackoff(backoff)
	}
	return ctx.Err()
}

func readOKXKlines(ctx context.Context, conn *websocket.Conn, onKline func(types.Candle, bool)) error {
	for {
		msg, err := readOKXMessage(ctx, conn)
		if err != nil {
			return err
		}
		var payload struct {
			Event string     `json:"event"`
			Data  [][]string `json:"data"`
		}
		if json.Unmarshal(msg, &payload) != nil {
			continue
		}
		if payload.Event == "error" {
			return fmt.Errorf("okx kline subscription: %s", msg)
		}
		for _, row := range payload.Data {
			if len(row) < 9 || (row[8] != "0" && row[8] != "1") {
				continue
			}
			candle := parseOKXCandleRows([][]string{row})[0]
			if candle.Timestamp > 0 {
				onKline(candle, row[8] == "1")
			}
		}
	}
}

// OKX expects a text ping on idle connections and replies with a text pong.
func okxHeartbeat(parent context.Context, conn *websocket.Conn) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				writeCtx, stop := context.WithTimeout(ctx, 5*time.Second)
				err := conn.Write(writeCtx, websocket.MessageText, []byte("ping"))
				stop()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	return ctx, cancel
}

func readOKXMessage(ctx context.Context, conn *websocket.Conn) ([]byte, error) {
	readCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, msg, err := conn.Read(readCtx)
	return msg, err
}
