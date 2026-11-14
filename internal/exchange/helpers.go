package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
)

const (
	wsInitialBackoff = time.Second
	wsMaxBackoff     = 30 * time.Second
)

func newHTTPClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second}
}

func doHTTPGet(ctx context.Context, cli *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	return body, nil
}

type wsLifecycle struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	closed bool
}

func (w *wsLifecycle) begin(ctx context.Context, name string) (context.Context, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil, fmt.Errorf("%s: exchange closed", name)
	}
	ctx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	return ctx, nil
}

func (w *wsLifecycle) close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	if w.cancel != nil {
		w.cancel()
	}
	return nil
}

func nextWSBackoff(cur time.Duration) time.Duration {
	if cur <= 0 {
		return wsInitialBackoff
	}
	next := cur * 2
	if next > wsMaxBackoff {
		return wsMaxBackoff
	}
	return next
}

func parseFloat(s string) float64 {
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

// parseFloatPtr distinguishes "field absent/unparseable" (nil) from a real
// value, including a real zero. Use it for optional metrics (change pct,
// funding rate, open interest) where 0 is meaningful.
func parseFloatPtr(s string) *float64 {
	if s == "" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &v
}

// ctxSleep waits for d or until ctx is done, returning ctx.Err() when
// cancelled. Used by WS reconnect backoff so shutdown is not delayed.
func ctxSleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// binanceSymbol converts "BTC/USDT:USDT" → "BTCUSDT".
func binanceSymbol(symbol string) string {
	s := strings.TrimSpace(symbol)
	if idx := strings.Index(s, "/"); idx >= 0 {
		base := s[:idx]
		return base + "USDT"
	}
	return strings.ReplaceAll(s, "/", "")
}

// bybitSymbol converts "BTC/USDT:USDT" → "BTCUSDT".
func bybitSymbol(symbol string) string {
	return binanceSymbol(symbol)
}

// toCanonicalBinance converts "BTCUSDT" → "BTC/USDT:USDT".
func toCanonicalBinance(raw string) string {
	raw = strings.ToUpper(strings.TrimSpace(raw))
	if !strings.HasSuffix(raw, "USDT") {
		return ""
	}
	base := strings.TrimSuffix(raw, "USDT")
	if base == "" {
		return ""
	}
	return base + "/USDT:USDT"
}

// toCanonicalBybit converts "BTCUSDT" → "BTC/USDT:USDT".
func toCanonicalBybit(raw string) string {
	return toCanonicalBinance(raw)
}

func sortCandlesAscending(candles []types.Candle) {
	sort.Slice(candles, func(i, j int) bool {
		return candles[i].Timestamp < candles[j].Timestamp
	})
}

// parseOKXCandleRows decodes OKX /api/v5/market/candles (and history-candles)
// string arrays: [tsMs, o, h, l, c, vol, volCcy, volCcyQuote, confirm].
// row[5] stays mapped to Volume; row[7] (volCcyQuote) populates QuoteVol when present.
func parseOKXCandleRows(rows [][]string) []types.Candle {
	candles := make([]types.Candle, 0, len(rows))
	for _, row := range rows {
		if len(row) < 6 {
			continue
		}
		ts, _ := strconv.ParseInt(row[0], 10, 64)
		var quoteVol float64
		if len(row) > 7 {
			quoteVol = parseFloat(row[7])
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
	return candles
}

// parseBinanceKlines decodes Binance USDS-M Futures /fapi/v1/klines arrays:
// [openTimeMs, open, high, low, close, volume, closeTimeMs, quoteAssetVolume, ...].
// row[5] stays mapped to Volume; row[7] (quote asset volume in USDT) populates
// QuoteVol when present (left 0 on short test rows).
func parseBinanceKlines(body []byte) ([]types.Candle, error) {
	if len(body) > 0 && body[0] == '{' {
		var errObj struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		if err := json.Unmarshal(body, &errObj); err == nil && errObj.Code != 0 {
			return nil, fmt.Errorf("binance fetch ohlcv: %s", errObj.Msg)
		}
	}
	var rows [][]any
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("binance fetch ohlcv: decode: %w", err)
	}
	candles := make([]types.Candle, 0, len(rows))
	for _, row := range rows {
		if len(row) < 6 {
			continue
		}
		ts := int64(parseFloat(fmt.Sprint(row[0])))
		var quoteVol float64
		if len(row) > 7 {
			quoteVol = parseFloat(fmt.Sprint(row[7]))
		}
		candles = append(candles, types.Candle{
			Timestamp: ts / 1000,
			Open:      parseFloat(fmt.Sprint(row[1])),
			High:      parseFloat(fmt.Sprint(row[2])),
			Low:       parseFloat(fmt.Sprint(row[3])),
			Close:     parseFloat(fmt.Sprint(row[4])),
			Volume:    parseFloat(fmt.Sprint(row[5])),
			QuoteVol:  quoteVol,
		})
	}
	return candles, nil
}
