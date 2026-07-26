package data

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// FetchMarketCapMap loads CoinGlass global market-cap rank (one REST call at subscribe).
func FetchMarketCapMap(ctx context.Context, timeout time.Duration) (map[string]float64, error) {
	raw, err := FetchCoinGlassEndpoint(ctx, "/api/marketCapRank", map[string]string{
		"pageSize": "500",
	}, timeout)
	if err != nil {
		return nil, err
	}
	return ParseMarketCapRankMap(raw)
}

// ParseMarketCapRankMap normalizes /api/marketCapRank payloads.
// Keys are upper-case coin codes (BTC, ETH, …).
func ParseMarketCapRankMap(payload any) (map[string]float64, error) {
	list, err := extractMarketCapList(payload)
	if err != nil {
		return nil, err
	}
	out := make(map[string]float64, len(list))
	for _, row := range list {
		code := strings.ToUpper(strings.TrimSpace(fmt.Sprint(firstString(row, "code", "symbol"))))
		if code == "" {
			continue
		}
		cap := floatFromAny(row["marketCap"])
		if cap <= 0 {
			cap = floatFromAny(row["market_cap_usd"])
		}
		if cap <= 0 {
			continue
		}
		out[code] = cap
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("coinglass market cap rank: empty list")
	}
	return out, nil
}

func firstString(row map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := row[k]; ok && v != nil {
			return fmt.Sprint(v)
		}
	}
	return ""
}

func extractMarketCapList(payload any) ([]map[string]any, error) {
	switch v := payload.(type) {
	case []any:
		return rowsToMaps(v), nil
	case map[string]any:
		if raw, ok := v["list"].([]any); ok {
			return rowsToMaps(raw), nil
		}
		if raw, ok := v["data"].([]any); ok {
			return rowsToMaps(raw), nil
		}
		return nil, fmt.Errorf("coinglass market cap: missing list")
	default:
		return nil, fmt.Errorf("coinglass market cap: unexpected payload type %T", payload)
	}
}
