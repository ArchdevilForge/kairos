package exchange

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
)

func newHTTPClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second}
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

func parseOKXCandleRows(rows [][]string) []types.Candle {
	candles := make([]types.Candle, 0, len(rows))
	for _, row := range rows {
		if len(row) < 6 {
			continue
		}
		ts, _ := strconv.ParseInt(row[0], 10, 64)
		candles = append(candles, types.Candle{
			Timestamp: ts / 1000,
			Open:      parseFloat(row[1]),
			High:      parseFloat(row[2]),
			Low:       parseFloat(row[3]),
			Close:     parseFloat(row[4]),
			Volume:    parseFloat(row[5]),
		})
	}
	return candles
}

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
		candles = append(candles, types.Candle{
			Timestamp: ts / 1000,
			Open:      parseFloat(fmt.Sprint(row[1])),
			High:      parseFloat(fmt.Sprint(row[2])),
			Low:       parseFloat(fmt.Sprint(row[3])),
			Close:     parseFloat(fmt.Sprint(row[4])),
			Volume:    parseFloat(fmt.Sprint(row[5])),
		})
	}
	return candles, nil
}
