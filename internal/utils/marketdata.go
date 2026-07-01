package utils

import (
	"encoding/json"
	"strconv"
)

// FirstFloat returns the value of the first key in keys that can be parsed as
// a float64, or nil if none match.
func FirstFloat(m map[string]any, keys []string) *float64 {
	for _, key := range keys {
		v, ok := m[key]
		if !ok || v == nil || v == "" {
			continue
		}
		switch val := v.(type) {
		case float64:
			return &val
		case int:
			f := float64(val)
			return &f
		case int64:
			f := float64(val)
			return &f
		case json.Number:
			f, err := val.Float64()
			if err != nil {
				continue
			}
			return &f
		case string:
			if val == "" {
				continue
			}
			f, err := strconv.ParseFloat(val, 64)
			if err != nil {
				continue
			}
			return &f
		}
	}
	return nil
}

// ExtractQuoteVolume returns the 24h quote/USD notional volume from a ticker
// payload (map[string]any), matching the logic in
// src/kairos/utils/market_data.py.
func ExtractQuoteVolume(ticker map[string]any) float64 {
	direct := FirstFloat(ticker, []string{"quoteVolume", "quoteVolume24h", "turnover", "turnover24h"})
	if direct != nil {
		return *direct
	}

	if info, ok := ticker["info"].(map[string]any); ok {
		quoteNotional := FirstFloat(info, []string{
			"volUsd24h", "volCcyQuote24h", "quoteVolume",
			"quoteVolume24h", "turnover", "turnover24h",
		})
		if quoteNotional != nil {
			return *quoteNotional
		}
	}

	baseVolume := FirstFloat(ticker, []string{"baseVolume", "volume"})
	if baseVolume == nil {
		if info, ok := ticker["info"].(map[string]any); ok {
			baseVolume = FirstFloat(info, []string{"vol24h", "baseVolume", "volume"})
		}
	}
	price := ExtractLastPrice(ticker)
	if baseVolume != nil && price != nil {
		return *baseVolume * *price
	}
	return 0.0
}

// ExtractLastPrice returns the best available last/close price from a ticker
// payload, matching src/kairos/utils/market_data.py.
func ExtractLastPrice(ticker map[string]any) *float64 {
	if v := FirstFloat(ticker, []string{"last", "close", "markPrice", "lastPrice"}); v != nil {
		return v
	}
	if info, ok := ticker["info"].(map[string]any); ok {
		return FirstFloat(info, []string{"last", "lastPrice", "markPx", "idxPx"})
	}
	return nil
}
