package exchange

import (
	"net/http"
	"strconv"
	"strings"
	"time"
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
