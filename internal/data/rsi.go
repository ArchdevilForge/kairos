package data

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// CoinRSI holds CoinGlass spot RSI snapshots for one base coin.
type CoinRSI struct {
	RSI15m float64
	RSI1h  float64
	RSI4h  float64
}

// FetchSpotRSIMap loads CoinGlass /api/spot/rsi/list (best-effort optional context).
func FetchSpotRSIMap(timeout time.Duration) (map[string]CoinRSI, error) {
	raw, err := FetchCoinGlassEndpoint("/api/spot/rsi/list", map[string]string{
		"pageSize": "500",
		"pageNum":  "1",
	}, timeout)
	if err != nil {
		return nil, err
	}
	return ParseSpotRSIMap(raw)
}

// ParseSpotRSIMap normalizes decrypted CoinGlass RSI list payloads.
func ParseSpotRSIMap(payload any) (map[string]CoinRSI, error) {
	list, err := extractRSIList(payload)
	if err != nil {
		return nil, err
	}
	out := make(map[string]CoinRSI, len(list))
	for _, row := range list {
		sym := strings.ToUpper(strings.TrimSpace(fmt.Sprint(row["symbol"])))
		if sym == "" {
			continue
		}
		out[sym] = CoinRSI{
			RSI15m: floatFromAny(row["rsi15m"]),
			RSI1h:  floatFromAny(row["rsi1h"]),
			RSI4h:  floatFromAny(row["rsi4h"]),
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("coinglass rsi: empty list")
	}
	return out, nil
}

func extractRSIList(payload any) ([]map[string]any, error) {
	switch v := payload.(type) {
	case map[string]any:
		if raw, ok := v["list"].([]any); ok {
			return rowsToMaps(raw), nil
		}
		return nil, fmt.Errorf("coinglass rsi: missing list")
	case []any:
		return rowsToMaps(v), nil
	default:
		return nil, fmt.Errorf("coinglass rsi: unexpected payload type %T", payload)
	}
}

func rowsToMaps(rows []any) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, item := range rows {
		if row, ok := item.(map[string]any); ok {
			out = append(out, row)
		}
	}
	return out
}

func floatFromAny(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f
	default:
		if v == nil {
			return 0
		}
		f, _ := strconv.ParseFloat(fmt.Sprint(v), 64)
		return f
	}
}
