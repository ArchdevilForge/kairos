package utils

import (
	"encoding/json"
	"strconv"
)

// AsFloat converts numeric values and json.Number to float64, returning 0 for
// strings, booleans, nil, and unsupported types.
func AsFloat(v any) float64 {
	f, _ := numericFloat(v)
	return f
}

// FloatFromMap returns the numeric value at key in m as float64, or 0 when m
// is nil, key is absent, or the value is non-numeric (including strings).
func FloatFromMap(m map[string]any, key string) float64 {
	if m == nil {
		return 0
	}
	return AsFloat(m[key])
}

func numericFloat(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case int32:
		return float64(val), true
	case int16:
		return float64(val), true
	case int8:
		return float64(val), true
	case uint:
		return float64(val), true
	case uint64:
		return float64(val), true
	case uint32:
		return float64(val), true
	case uint16:
		return float64(val), true
	case uint8:
		return float64(val), true
	case json.Number:
		f, err := val.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

// FirstFloat returns the value of the first key in keys that can be parsed as
// a float64, or nil if none match.
func FirstFloat(m map[string]any, keys []string) *float64 {
	for _, key := range keys {
		v, ok := m[key]
		if !ok || v == nil || v == "" {
			continue
		}
		if f, ok := numericFloat(v); ok {
			return &f
		}
		if val, ok := v.(string); ok {
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
