package utils

import (
	"fmt"
	"strings"
)

// NormalizeSymbol normalizes a CCXT-style symbol to the canonical
// "BASE/USDT:USDT" format.  Ported from src/kairos/scanner.py
// _normalize_symbol.
func NormalizeSymbol(symbol string) (string, error) {
	value := strings.TrimSpace(symbol)
	value = strings.ToUpper(value)
	if value == "" {
		return "", fmt.Errorf("symbol is required")
	}
	// Already canonical: "BTC/USDT:USDT"
	if strings.HasSuffix(value, ":USDT") && strings.Contains(value, "/USDT:") {
		return value, nil
	}
	// "BTC/USDT" → "BTC/USDT:USDT"
	if strings.HasSuffix(value, "/USDT") {
		return value + ":USDT", nil
	}
	// "BTCUSDT" (no / or :) → "BTC/USDT:USDT"
	if strings.HasSuffix(value, "USDT") && !strings.Contains(value, "/") && !strings.Contains(value, ":") {
		base := value[:len(value)-len("USDT")]
		if base == "" {
			return "", fmt.Errorf("invalid USDT symbol: %s", symbol)
		}
		return base + "/USDT:USDT", nil
	}
	return "", fmt.Errorf("unsupported symbol format: %s", symbol)
}

// NormalizeCoinSymbol extracts the coin base name from an exchange symbol
// for CoinGlass API calls.  Ported from src/kairos/data/coinglass_client.py
// normalize_coin_symbol.
func NormalizeCoinSymbol(symbol string) string {
	value := strings.ToUpper(strings.TrimSpace(symbol))
	if value == "" {
		return ""
	}
	// Strip after ":"
	if idx := strings.Index(value, ":"); idx >= 0 {
		value = value[:idx]
	}
	// Strip separator and what follows
	for _, sep := range []string{"/", "-", "_"} {
		if idx := strings.Index(value, sep); idx >= 0 {
			value = value[:idx]
			break
		}
	}
	// Strip known suffixes
	for _, suffix := range []string{"USDT", "USDC", "USD", "PERP"} {
		if strings.HasSuffix(value, suffix) && len(value) > len(suffix) {
			return value[:len(value)-len(suffix)]
		}
	}
	return value
}

// LooksLikeUSDTPerpetual checks whether a raw symbol (and its ticker info)
// indicates a USDT-margined perpetual swap.  Ported from
// src/kairos/scanner.py _looks_like_usdt_perpetual.
func LooksLikeUSDTPerpetual(symbol string, ticker map[string]any) bool {
	// Canonical format: "BASE/USDT:USDT"
	if strings.Contains(symbol, "/USDT:USDT") && strings.HasSuffix(symbol, ":USDT") {
		return true
	}
	info, ok := ticker["info"].(map[string]any)
	if !ok {
		return false
	}
	instType := strings.ToUpper(fmt.Sprint(info["instType"]))
	contractType := strings.ToUpper(fmt.Sprint(info["contractType"]))
	return instType == "SWAP" || instType == "PERPETUAL" ||
		contractType == "SWAP" || contractType == "PERPETUAL"
}
