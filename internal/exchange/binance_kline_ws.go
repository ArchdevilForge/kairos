package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/ArchdevilForge/kairos/internal/types"
	"github.com/coder/websocket"
)

// MetricsPoint represents historical futures metrics aligned to a bar timestamp (Unix seconds).
type MetricsPoint struct {
	Time         int64    `json:"time"`
	OpenInterest *float64 `json:"open_interest,omitempty"`
	OIChangePct  *float64 `json:"oi_change_pct,omitempty"`
	FundingRate  *float64 `json:"funding_rate,omitempty"`
}

// SubscribeKline subscribes to Binance USDT-M kline WebSocket stream
// wss://fstream.binance.com/ws/<sym>@kline_<tf> for a single symbol and timeframe.
func (b *binanceExchange) SubscribeKline(ctx context.Context, symbol, tf string, onKline func(types.Candle, bool)) error {
	return SubscribeKline(ctx, symbol, tf, onKline)
}

// SubscribeKline connects to wss://fstream.binance.com/ws/<sym>@kline_<tf> and
// invokes onKline for each incoming bar update until ctx is cancelled.
func SubscribeKline(ctx context.Context, symbol, tf string, onKline func(types.Candle, bool)) error {
	return subscribeBinanceKlineWithEndpoint(ctx, binanceWSEndpoint, symbol, tf, onKline)
}

func subscribeBinanceKlineWithEndpoint(ctx context.Context, wsBase, symbol, tf string, onKline func(types.Candle, bool)) error {
	rawSym := strings.ToLower(binanceSymbol(symbol))
	if rawSym == "" {
		return fmt.Errorf("binance kline ws: empty symbol")
	}
	interval := mapBinanceTimeframe(tf)
	uri := strings.TrimRight(wsBase, "/") + "/" + rawSym + "@kline_" + interval

	backoff := wsInitialBackoff
	flog := newFeedLog("binance-kline")

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		conn, _, err := websocket.Dial(ctx, uri, nil)
		if err != nil {
			flog.Warn("dial", "kline ws dial failed, retrying",
				"symbol", symbol, "tf", tf, "error", err, "backoff", backoff)
			if err := ctxSleep(ctx, backoff); err != nil {
				return err
			}
			backoff = nextWSBackoff(backoff)
			continue
		}
		backoff = wsInitialBackoff

		err = readBinanceKlineLoop(ctx, conn, onKline)
		_ = conn.Close(websocket.StatusNormalClosure, "")
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			flog.Warn("read", "kline ws read ended, reconnecting",
				"symbol", symbol, "tf", tf, "error", err, "backoff", backoff)
			if sleepErr := ctxSleep(ctx, backoff); sleepErr != nil {
				return sleepErr
			}
			backoff = nextWSBackoff(backoff)
		}
	}
}

func readBinanceKlineLoop(ctx context.Context, conn *websocket.Conn, onKline func(types.Candle, bool)) error {
	for {
		_, msg, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		candle, closed, ok := parseBinanceKlineWSMessage(msg)
		if !ok {
			continue
		}
		if onKline != nil {
			onKline(candle, closed)
		}
	}
}

func parseBinanceKlineWSMessage(msg []byte) (types.Candle, bool, bool) {
	// Decode into a case-sensitive map because Binance kline payloads carry both
	// lowercase and uppercase keys (e/E, t/T, l/L, v/V, q/Q) that collide under
	// Go encoding/json's case-insensitive struct tag matching.
	var root map[string]any
	if err := json.Unmarshal(msg, &root); err != nil {
		return types.Candle{}, false, false
	}
	evt, _ := root["e"].(string)
	if evt != "kline" {
		return types.Candle{}, false, false
	}
	k, ok := root["k"].(map[string]any)
	if !ok {
		return types.Candle{}, false, false
	}
	tNum, _ := k["t"].(float64)
	startTimeMs := int64(tNum)
	if startTimeMs <= 0 {
		return types.Candle{}, false, false
	}
	str := func(key string) string {
		s, _ := k[key].(string)
		return s
	}
	closed, _ := k["x"].(bool)
	return types.Candle{
		Timestamp: startTimeMs / 1000,
		Open:      parseFloat(str("o")),
		High:      parseFloat(str("h")),
		Low:       parseFloat(str("l")),
		Close:     parseFloat(str("c")),
		Volume:    parseFloat(str("v")),
		QuoteVol:  parseFloat(str("q")),
	}, closed, true
}

// FetchMetricsHistory fetches Binance USDT-M open interest and funding rate history
// aligned for chart sub-panes. Fails gracefully on unsupported timeframes or errors.
func FetchBinanceMetricsHistory(ctx context.Context, symbol, tf string, limit int, beforeMs int64) ([]MetricsPoint, error) {
	cli := newHTTPClient()
	return fetchBinanceMetricsHistory(ctx, cli, binanceRESTEndpoint, symbol, tf, limit, beforeMs)
}

func fetchBinanceMetricsHistory(ctx context.Context, cli *http.Client, restBase, symbol, tf string, limit int, beforeMs int64) ([]MetricsPoint, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	sym := binanceSymbol(symbol)
	if sym == "" {
		return nil, nil
	}
	period := mapBinanceOIPeriod(tf)

	byTime := make(map[int64]*MetricsPoint)
	ensure := func(ts int64) *MetricsPoint {
		if p, ok := byTime[ts]; ok {
			return p
		}
		p := &MetricsPoint{Time: ts}
		byTime[ts] = p
		return p
	}

	if period != "" {
		q := url.Values{
			"symbol": {sym},
			"period": {period},
			"limit":  {fmt.Sprintf("%d", limit)},
		}
		if beforeMs > 0 {
			q.Set("endTime", fmt.Sprintf("%d", beforeMs-1))
		}
		u := strings.TrimRight(restBase, "/") + "/futures/data/openInterestHist?" + q.Encode()
		if body, err := doHTTPGet(ctx, cli, u); err == nil {
			var rows []struct {
				SumOpenInterest      string `json:"sumOpenInterest"`
				SumOpenInterestValue string `json:"sumOpenInterestValue"`
				Timestamp            int64  `json:"timestamp"`
			}
			if json.Unmarshal(body, &rows) == nil {
				var prevOI float64
				for _, r := range rows {
					if r.Timestamp <= 0 {
						continue
					}
					ts := r.Timestamp / 1000
					oi := parseFloat(r.SumOpenInterestValue)
					if oi <= 0 {
						oi = parseFloat(r.SumOpenInterest)
					}
					if oi <= 0 {
						continue
					}
					pt := ensure(ts)
					oiVal := oi
					pt.OpenInterest = &oiVal
					if prevOI > 0 {
						chg := (oiVal - prevOI) / prevOI * 100.0
						pt.OIChangePct = &chg
					}
					prevOI = oiVal
				}
			}
		}
	}

	fq := url.Values{
		"symbol": {sym},
		"limit":  {fmt.Sprintf("%d", minInt(limit, 200))},
	}
	if beforeMs > 0 {
		fq.Set("endTime", fmt.Sprintf("%d", beforeMs-1))
	}
	fu := strings.TrimRight(restBase, "/") + "/fapi/v1/fundingRate?" + fq.Encode()
	if body, err := doHTTPGet(ctx, cli, fu); err == nil {
		var rows []struct {
			FundingTime int64  `json:"fundingTime"`
			FundingRate string `json:"fundingRate"`
		}
		if json.Unmarshal(body, &rows) == nil {
			for _, r := range rows {
				if r.FundingTime <= 0 {
					continue
				}
				ts := r.FundingTime / 1000
				if rate := parseFloatPtr(r.FundingRate); rate != nil {
					ensure(ts).FundingRate = rate
				}
			}
		}
	}

	out := make([]MetricsPoint, 0, len(byTime))
	for _, pt := range byTime {
		out = append(out, *pt)
	}
	sortMetricsPoints(out)
	return out, nil
}

func mapBinanceOIPeriod(tf string) string {
	switch tf {
	case "1m", "3m", "5m":
		return "5m"
	case "15m", "30m", "1h", "2h", "4h", "6h", "12h", "1d":
		return tf
	default:
		return "5m"
	}
}

func sortMetricsPoints(pts []MetricsPoint) {
	for i := 1; i < len(pts); i++ {
		x := pts[i]
		j := i - 1
		for j >= 0 && pts[j].Time > x.Time {
			pts[j+1] = pts[j]
			j--
		}
		pts[j+1] = x
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
