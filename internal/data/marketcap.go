package data

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const coinGeckoMarketsURL = "https://api.coingecko.com/api/v3/coins/markets?vs_currency=usd&order=market_cap_desc&per_page=150&page=1&sparkline=false"

// MarketCapEntry holds a coin's USD market cap and 1-based tradable crypto rank
// (excluding stablecoins from rank slots).
type MarketCapEntry struct {
	CapUSD float64
	Rank   int
}

var stablecoinBases = map[string]bool{
	"USDT":  true,
	"USDC":  true,
	"FDUSD": true,
	"USDE":  true,
	"DAI":   true,
	"TUSD":  true,
	"BUSD":  true,
	"USD1":  true,
	"PYUSD": true,
	"USDD":  true,
	"RLUSD": true,
	"USDS":  true,
	"USDG":  true,
	"EURC":  true,
}

// CanonicalMarketCapBase strips /USDT:USDT, USDT suffix, and 1000/1000000/1M prefixes.
func CanonicalMarketCapBase(symbol string) string {
	s := strings.ToUpper(strings.TrimSpace(symbol))
	if s == "" {
		return ""
	}
	if idx := strings.IndexAny(s, "/:"); idx >= 0 {
		s = s[:idx]
	}
	if idx := strings.IndexAny(s, "-_"); idx >= 0 {
		s = s[:idx]
	}
	if len(s) > len("USDT") && strings.HasSuffix(s, "USDT") {
		s = s[:len(s)-len("USDT")]
	}
	for _, prefix := range []string{"1000000", "1000", "1M"} {
		if len(s) > len(prefix) && strings.HasPrefix(s, prefix) {
			next := s[len(prefix)]
			if next >= 'A' && next <= 'Z' {
				s = s[len(prefix):]
				break
			}
		}
	}
	return s
}

// FetchMarketCapRankEntries returns upper-case base coin -> MarketCapEntry (1-based tradable crypto rank,
// excluding stablecoins USDT/USDC/FDUSD/USDE/DAI/TUSD/BUSD/USD1/PYUSD/USDD/RLUSD from rank slots),
// trying CoinGecko /api/v3/coins/markets (pure crypto) first and falling back to CoinGlass /api/marketCapRank.
func FetchMarketCapRankEntries(ctx context.Context, timeout time.Duration) (map[string]MarketCapEntry, error) {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	geckoTimeout := timeout
	if geckoTimeout >= 8*time.Second {
		geckoTimeout = timeout / 2
	}

	geckoEntries, geckoErr := fetchCoinGeckoMarketCapEntries(ctx, geckoTimeout)
	if geckoErr == nil && len(geckoEntries) > 0 {
		return geckoEntries, nil
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	raw, cgErr := FetchCoinGlassEndpoint(ctx, "/api/marketCapRank", map[string]string{
		"pageSize": "500",
	}, timeout)
	if cgErr == nil {
		if cgEntries, err := ParseMarketCapRankEntries(raw); err == nil && len(cgEntries) > 0 {
			return cgEntries, nil
		} else if err != nil {
			cgErr = err
		}
	}

	if geckoErr != nil {
		return nil, geckoErr
	}
	return nil, cgErr
}

// ParseMarketCapRankEntries normalizes market-cap payloads into 1-based non-stablecoin ranks.
func ParseMarketCapRankEntries(payload any) (map[string]MarketCapEntry, error) {
	list, err := extractMarketCapList(payload)
	if err != nil {
		return nil, err
	}
	bestCaps := make(map[string]float64, len(list))
	for _, row := range list {
		code := CanonicalMarketCapBase(firstString(row, "code", "symbol"))
		if code == "" || stablecoinBases[code] {
			continue
		}
		capUSD := floatFromAny(row["marketCap"])
		if capUSD <= 0 {
			capUSD = floatFromAny(row["market_cap_usd"])
		}
		if capUSD <= 0 {
			capUSD = floatFromAny(row["market_cap"])
		}
		if capUSD <= 0 {
			continue
		}
		if capUSD > bestCaps[code] {
			bestCaps[code] = capUSD
		}
	}
	out := buildRankedEntries(bestCaps)
	if len(out) == 0 {
		return nil, fmt.Errorf("coinglass market cap rank: empty list")
	}
	return out, nil
}

func fetchCoinGeckoMarketCapEntries(ctx context.Context, timeout time.Duration) (map[string]MarketCapEntry, error) {
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, coinGeckoMarketsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "kairos/1.0")

	cli := &http.Client{Timeout: timeout}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("coingecko markets: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("coingecko markets: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("coingecko markets read: %w", err)
	}

	var rows []struct {
		Symbol    string  `json:"symbol"`
		MarketCap float64 `json:"market_cap"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("coingecko markets decode: %w", err)
	}

	bestCaps := make(map[string]float64, len(rows))
	for _, r := range rows {
		code := CanonicalMarketCapBase(r.Symbol)
		if code == "" || stablecoinBases[code] || r.MarketCap <= 0 {
			continue
		}
		if r.MarketCap > bestCaps[code] {
			bestCaps[code] = r.MarketCap
		}
	}
	out := buildRankedEntries(bestCaps)
	if len(out) == 0 {
		return nil, fmt.Errorf("coingecko markets: empty list")
	}
	return out, nil
}

func buildRankedEntries(caps map[string]float64) map[string]MarketCapEntry {
	type coinCap struct {
		code string
		cap  float64
	}
	list := make([]coinCap, 0, len(caps))
	for code, capUSD := range caps {
		if capUSD <= 0 || stablecoinBases[code] {
			continue
		}
		list = append(list, coinCap{code: code, cap: capUSD})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].cap != list[j].cap {
			return list[i].cap > list[j].cap
		}
		return list[i].code < list[j].code
	})
	out := make(map[string]MarketCapEntry, len(list))
	for i, item := range list {
		out[item.code] = MarketCapEntry{
			CapUSD: item.cap,
			Rank:   i + 1,
		}
	}
	return out
}

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
			if s := strings.TrimSpace(fmt.Sprint(v)); s != "" {
				return s
			}
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
