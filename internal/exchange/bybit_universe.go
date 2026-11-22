package exchange

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ArchdevilForge/kairos/internal/data"
	"github.com/ArchdevilForge/kairos/internal/types"
	"github.com/ArchdevilForge/kairos/internal/utils"
)

const (
	bybitExchangeInfoTimeout = 15 * time.Second
	bybitSpotInfoTimeout     = 5 * time.Second
	bybitMaxInstrumentPages  = 50
)

type bybitPerpMeta struct {
	launchTimeMs int64
}

var bybitCryptoCache = struct {
	mu        sync.Mutex
	fetchedAt time.Time
	allow     map[string]bool
	sorted    []string
}{}

var bybitTradableCache = struct {
	mu        sync.Mutex
	fetchedAt time.Time
	allow     map[string]bool
	sorted    []string
}{}

var bybitUniverseInfoCache = struct {
	mu        sync.Mutex
	fetchedAt time.Time
	meta      map[string]bybitPerpMeta
}{}

var bybitSpotCache = struct {
	mu        sync.Mutex
	fetchedAt time.Time
	bases     map[string]bool
}{}

// FetchCryptoPerpSymbols returns active USDT crypto perpetual symbols for the
// given exchange ("bybit" or "binance", defaulting to "bybit" when empty/unknown).
func FetchCryptoPerpSymbols(ctx context.Context, exchangeName string) ([]string, map[string]bool, error) {
	switch strings.ToLower(strings.TrimSpace(exchangeName)) {
	case "binance":
		return FetchBinanceCryptoPerpSymbols(ctx)
	default:
		return FetchBybitCryptoPerpSymbols(ctx)
	}
}

// FetchTradablePerpSymbols returns all active USDT linear perpetual symbols
// supported for charting, search, and trade review on the given exchange.
// On Bybit this includes crypto, stock, ETF, commodity, and forex linear perpetuals,
// while MarketPulse / Top-100 discovery continues to use FetchCryptoPerpSymbols.
func FetchTradablePerpSymbols(ctx context.Context, exchangeName string) ([]string, map[string]bool, error) {
	switch strings.ToLower(strings.TrimSpace(exchangeName)) {
	case "binance":
		return FetchBinanceCryptoPerpSymbols(ctx)
	default:
		return FetchBybitTradablePerpSymbols(ctx)
	}
}

// FetchBybitTradablePerpSymbols returns all active Bybit USDT linear perpetual
// symbols (including crypto, stock, ETF, commodity, and forex perpetuals) sorted
// by 24h quote turnover descending, along with an O(1) allowlist map (cached 1h).
func FetchBybitTradablePerpSymbols(ctx context.Context) ([]string, map[string]bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	bybitTradableCache.mu.Lock()
	if len(bybitTradableCache.allow) > 0 && time.Since(bybitTradableCache.fetchedAt) < cryptoCacheTTL {
		allowCopy := make(map[string]bool, len(bybitTradableCache.allow))
		for k, v := range bybitTradableCache.allow {
			allowCopy[k] = v
		}
		sortedCopy := append([]string(nil), bybitTradableCache.sorted...)
		bybitTradableCache.mu.Unlock()
		return sortedCopy, allowCopy, nil
	}
	bybitTradableCache.mu.Unlock()

	cli := newBybitPooledHTTPClient(bybitExchangeInfoTimeout)
	restBase := strings.TrimRight(currentBybitRESTEndpoint(), "/")

	meta, err := fetchBybitPerpMetaFiltered(ctx, cli, restBase, false)
	if err != nil {
		return nil, nil, err
	}
	allow := make(map[string]bool, len(meta))
	for sym := range meta {
		allow[sym] = true
	}

	vols := make(map[string]float64, len(allow))
	var tickerResp struct {
		RetCode int `json:"retCode"`
		Result  struct {
			List []struct {
				Symbol      string `json:"symbol"`
				Turnover24h string `json:"turnover24h"`
			} `json:"list"`
		} `json:"result"`
	}
	if err := httpGetJSON(ctx, cli, restBase+"/v5/market/tickers?category=linear", &tickerResp); err == nil && tickerResp.RetCode == 0 {
		for _, r := range tickerResp.Result.List {
			if canon := toCanonicalBybit(r.Symbol); canon != "" && allow[canon] {
				vols[canon] = parseFloat(r.Turnover24h)
			}
		}
	}

	sorted := make([]string, 0, len(allow))
	for sym := range allow {
		sorted = append(sorted, sym)
	}
	sort.Slice(sorted, func(i, j int) bool {
		vi, vj := vols[sorted[i]], vols[sorted[j]]
		if vi != vj {
			return vi > vj
		}
		return sorted[i] < sorted[j]
	})

	bybitTradableCache.mu.Lock()
	bybitTradableCache.allow = allow
	bybitTradableCache.sorted = sorted
	bybitTradableCache.fetchedAt = time.Now()
	bybitTradableCache.mu.Unlock()

	allowCopy := make(map[string]bool, len(allow))
	for k, v := range allow {
		allowCopy[k] = v
	}
	return append([]string(nil), sorted...), allowCopy, nil
}

// FetchBybitCryptoPerpSymbols returns active Bybit USDT linear crypto perpetual
// symbols sorted by 24h quote turnover descending, along with an O(1) allowlist
// map (cached 1h).
func FetchBybitCryptoPerpSymbols(ctx context.Context) ([]string, map[string]bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var cachedAllow map[string]bool
	bybitCryptoCache.mu.Lock()
	if len(bybitCryptoCache.allow) > 0 && time.Since(bybitCryptoCache.fetchedAt) < cryptoCacheTTL {
		allowCopy := make(map[string]bool, len(bybitCryptoCache.allow))
		for k, v := range bybitCryptoCache.allow {
			allowCopy[k] = v
		}
		if len(bybitCryptoCache.sorted) > 0 {
			sortedCopy := append([]string(nil), bybitCryptoCache.sorted...)
			bybitCryptoCache.mu.Unlock()
			return sortedCopy, allowCopy, nil
		}
		cachedAllow = allowCopy
	}
	bybitCryptoCache.mu.Unlock()

	cli := newBybitPooledHTTPClient(bybitExchangeInfoTimeout)
	restBase := strings.TrimRight(currentBybitRESTEndpoint(), "/")

	allow := cachedAllow
	if len(allow) == 0 {
		meta, err := fetchBybitPerpMetaFrom(ctx, cli, restBase)
		if err != nil {
			return nil, nil, err
		}
		allow = make(map[string]bool, len(meta))
		for sym := range meta {
			allow[sym] = true
		}
		now := time.Now()
		bybitUniverseInfoCache.mu.Lock()
		bybitUniverseInfoCache.meta = meta
		bybitUniverseInfoCache.fetchedAt = now
		bybitUniverseInfoCache.mu.Unlock()
	}

	vols := make(map[string]float64, len(allow))
	var tickerResp struct {
		RetCode int `json:"retCode"`
		Result  struct {
			List []struct {
				Symbol      string `json:"symbol"`
				Turnover24h string `json:"turnover24h"`
			} `json:"list"`
		} `json:"result"`
	}
	if err := httpGetJSON(ctx, cli, restBase+"/v5/market/tickers?category=linear", &tickerResp); err == nil && tickerResp.RetCode == 0 {
		for _, r := range tickerResp.Result.List {
			if canon := toCanonicalBybit(r.Symbol); canon != "" && allow[canon] {
				vols[canon] = parseFloat(r.Turnover24h)
			}
		}
	}

	sorted := make([]string, 0, len(allow))
	for sym := range allow {
		sorted = append(sorted, sym)
	}
	sort.Slice(sorted, func(i, j int) bool {
		vi, vj := vols[sorted[i]], vols[sorted[j]]
		if vi != vj {
			return vi > vj
		}
		return sorted[i] < sorted[j]
	})

	bybitCryptoCache.mu.Lock()
	bybitCryptoCache.allow = allow
	bybitCryptoCache.sorted = sorted
	bybitCryptoCache.fetchedAt = time.Now()
	bybitCryptoCache.mu.Unlock()

	allowCopy := make(map[string]bool, len(allow))
	for k, v := range allow {
		allowCopy[k] = v
	}
	return append([]string(nil), sorted...), allowCopy, nil
}

// FilterBybitTop100Universe filters Bybit USDT linear tickers using MarketCap rank,
// listing age, optional spot pair presence, 24h quote turnover, Open Interest floor,
// OI/MarketCap cap, and Vol/OI churn cap. Returns (nil, false) when live metadata
// is unavailable or fewer than 5 symbols pass so callers can safely fall back.
func FilterBybitTop100Universe(
	ctx context.Context,
	tickers map[string]*types.Ticker,
	mcapEntries map[string]data.MarketCapEntry,
	incumbents []string,
	topN int,
	cfg UniverseFilterConfig,
) ([]string, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	cfg = normalizeUniverseFilterConfig(cfg)
	if topN <= 0 || len(tickers) == 0 || len(mcapEntries) == 0 {
		return nil, false
	}

	cli := newBybitPooledHTTPClient(bybitExchangeInfoTimeout)
	perpMeta, ok := loadBybitPerpMeta(ctx, cli)
	if !ok {
		return nil, false
	}
	populateBybitCryptoSortedFromTickers(perpMeta, tickers)

	var spotBases map[string]bool
	var spotLoaded bool
	if cfg.RequireSpotPair {
		spotBases, spotLoaded = loadBybitSpotBases(ctx, cli)
	}

	incumbentSet := make(map[string]bool, len(incumbents))
	for _, sym := range incumbents {
		incumbentSet[sym] = true
	}

	nowMs := time.Now().UnixMilli()
	minAgeMs := int64(cfg.MinListingDays) * 24 * int64(time.Hour/time.Millisecond)

	var hasBTC, hasETH bool
	passing := make([]universeCandidate, 0, len(tickers))

	for sym, t := range tickers {
		if t == nil {
			continue
		}
		meta, listed := perpMeta[sym]
		if !listed {
			continue
		}
		if sym == "BTC/USDT:USDT" {
			hasBTC = true
			continue
		}
		if sym == "ETH/USDT:USDT" {
			hasETH = true
			continue
		}

		base := data.CanonicalMarketCapBase(sym)
		capEntry, hasCap := mcapEntries[base]
		if !hasCap || capEntry.Rank <= 0 || capEntry.Rank > cfg.MaxMarketCapRank || capEntry.CapUSD <= 0 {
			continue
		}
		if meta.launchTimeMs <= 0 || nowMs-meta.launchTimeMs < minAgeMs {
			continue
		}
		if cfg.RequireSpotPair && spotLoaded && !spotBases[base] {
			continue
		}

		quoteVol := tickerQuoteVolume(t)
		effMinVol := cfg.MinQuoteVol24h
		if incumbentSet[sym] {
			effMinVol *= 0.75
		}
		if quoteVol < effMinVol {
			continue
		}

		lastPrice := tickerLastPrice(t)
		oiUSD := bybitTickerOIUSD(t, lastPrice)
		if oiUSD > 0 {
			effMinOI := cfg.MinOIUSD
			if incumbentSet[sym] {
				effMinOI *= 0.75
			}
			if oiUSD < effMinOI {
				continue
			}
			if capEntry.CapUSD <= 0 || oiUSD/capEntry.CapUSD > cfg.MaxOIToMCapRatio {
				continue
			}
			maxVolOI := cfg.MaxVolToOIRatio
			if capEntry.Rank <= 20 && maxVolOI < 35.0 {
				maxVolOI = 35.0
			}
			if quoteVol/oiUSD > maxVolOI {
				continue
			}
		}

		passing = append(passing, universeCandidate{
			symbol:      sym,
			quoteVol:    quoteVol,
			lastPrice:   lastPrice,
			oiUSD:       oiUSD,
			cap:         capEntry,
			isIncumbent: incumbentSet[sym],
			preOI:       t.OpenInterest,
		})
	}

	sort.Slice(passing, func(i, j int) bool {
		si := universeCandidateScore(passing[i].oiUSD, passing[i].quoteVol)
		sj := universeCandidateScore(passing[j].oiUSD, passing[j].quoteVol)
		if si != sj {
			return si > sj
		}
		if passing[i].quoteVol != passing[j].quoteVol {
			return passing[i].quoteVol > passing[j].quoteVol
		}
		return passing[i].symbol < passing[j].symbol
	})

	out := make([]string, 0, topN)
	if hasBTC && len(out) < topN {
		out = append(out, "BTC/USDT:USDT")
	}
	if hasETH && len(out) < topN {
		out = append(out, "ETH/USDT:USDT")
	}
	for _, c := range passing {
		if len(out) >= topN {
			break
		}
		out = append(out, c.symbol)
	}
	if len(out) < minFilteredUniverseSize {
		return nil, false
	}
	return out, true
}

func bybitTickerOIUSD(t *types.Ticker, lastPrice float64) float64 {
	if t == nil {
		return 0
	}
	if len(t.Info) > 0 {
		if v := utils.FirstFloat(t.Info, []string{"openInterestValue"}); v != nil && *v > 0 {
			return *v
		}
	}
	if t.OpenInterest != nil && *t.OpenInterest > 0 && lastPrice > 0 {
		return *t.OpenInterest * lastPrice
	}
	return 0
}

func loadBybitPerpMeta(ctx context.Context, cli *http.Client) (map[string]bybitPerpMeta, bool) {
	bybitUniverseInfoCache.mu.Lock()
	if len(bybitUniverseInfoCache.meta) > 0 && time.Since(bybitUniverseInfoCache.fetchedAt) < cryptoCacheTTL {
		cached := bybitUniverseInfoCache.meta
		bybitUniverseInfoCache.mu.Unlock()
		return cached, true
	}
	bybitUniverseInfoCache.mu.Unlock()

	reqCtx, cancel := context.WithTimeout(ctx, bybitExchangeInfoTimeout)
	defer cancel()

	restBase := strings.TrimRight(currentBybitRESTEndpoint(), "/")
	meta, err := fetchBybitPerpMetaFrom(reqCtx, cli, restBase)
	if err != nil {
		return nil, false
	}

	allow := make(map[string]bool, len(meta))
	for sym := range meta {
		allow[sym] = true
	}

	now := time.Now()
	bybitUniverseInfoCache.mu.Lock()
	bybitUniverseInfoCache.meta = meta
	bybitUniverseInfoCache.fetchedAt = now
	bybitUniverseInfoCache.mu.Unlock()

	bybitCryptoCache.mu.Lock()
	if len(bybitCryptoCache.allow) == 0 || time.Since(bybitCryptoCache.fetchedAt) >= cryptoCacheTTL {
		sorted := make([]string, 0, len(allow))
		for sym := range allow {
			sorted = append(sorted, sym)
		}
		sort.Strings(sorted)
		bybitCryptoCache.allow = allow
		if len(bybitCryptoCache.sorted) == 0 {
			bybitCryptoCache.sorted = sorted
		}
		bybitCryptoCache.fetchedAt = now
	}
	bybitCryptoCache.mu.Unlock()

	return meta, true
}

func fetchBybitPerpMetaFrom(ctx context.Context, cli *http.Client, restBase string) (map[string]bybitPerpMeta, error) {
	return fetchBybitPerpMetaFiltered(ctx, cli, restBase, true)
}

func fetchBybitPerpMetaFiltered(ctx context.Context, cli *http.Client, restBase string, cryptoOnly bool) (map[string]bybitPerpMeta, error) {
	base := strings.TrimRight(restBase, "/")
	meta := make(map[string]bybitPerpMeta)
	seenCursors := make(map[string]bool)
	cursor := ""

	for page := 0; page < bybitMaxInstrumentPages; page++ {
		q := url.Values{
			"category": {"linear"},
			"limit":    {"1000"},
		}
		if cursor != "" {
			if unescaped, err := url.PathUnescape(cursor); err == nil {
				q.Set("cursor", unescaped)
			} else {
				q.Set("cursor", cursor)
			}
		}
		target := base + "/v5/market/instruments-info?" + q.Encode()

		var payload struct {
			RetCode int    `json:"retCode"`
			RetMsg  string `json:"retMsg"`
			Result  struct {
				NextPageCursor string `json:"nextPageCursor"`
				List           []struct {
					Symbol       string `json:"symbol"`
					ContractType string `json:"contractType"`
					Status       string `json:"status"`
					QuoteCoin    string `json:"quoteCoin"`
					SettleCoin   string `json:"settleCoin"`
					LaunchTime   string `json:"launchTime"`
					SymbolType   string `json:"symbolType"`
					IsPreListing bool   `json:"isPreListing"`
				} `json:"list"`
			} `json:"result"`
		}
		if err := httpGetJSON(ctx, cli, target, &payload); err != nil {
			return nil, err
		}
		if payload.RetCode != 0 {
			return nil, fmt.Errorf("bybit instruments-info: retCode=%d msg=%s", payload.RetCode, payload.RetMsg)
		}

		for _, inst := range payload.Result.List {
			if inst.Status != "Trading" || inst.ContractType != "LinearPerpetual" ||
				inst.QuoteCoin != "USDT" || inst.SettleCoin != "USDT" || inst.IsPreListing {
				continue
			}
			if cryptoOnly && !isBybitCryptoSymbolType(inst.SymbolType) {
				continue
			}
			canon := toCanonicalBybit(inst.Symbol)
			if canon == "" {
				continue
			}
			launchMs, _ := strconv.ParseInt(strings.TrimSpace(inst.LaunchTime), 10, 64)
			meta[canon] = bybitPerpMeta{launchTimeMs: launchMs}
		}

		next := strings.TrimSpace(payload.Result.NextPageCursor)
		if next == "" || seenCursors[next] {
			break
		}
		seenCursors[next] = true
		cursor = next
	}

	if len(meta) == 0 {
		return nil, fmt.Errorf("bybit instruments-info: zero linear perpetuals")
	}
	return meta, nil
}

func isBybitCryptoSymbolType(symbolType string) bool {
	switch strings.ToLower(strings.TrimSpace(symbolType)) {
	case "forex", "stock", "stocks", "commodity", "commodities", "etf", "xstocks", "mstocks", "index", "tradfi", "equity":
		return false
	default:
		return true
	}
}

func populateBybitCryptoSortedFromTickers(perpMeta map[string]bybitPerpMeta, tickers map[string]*types.Ticker) {
	if len(perpMeta) == 0 || len(tickers) == 0 {
		return
	}
	sorted := make([]string, 0, len(perpMeta))
	for sym := range perpMeta {
		sorted = append(sorted, sym)
	}
	sort.Slice(sorted, func(i, j int) bool {
		vi := tickerQuoteVolume(tickers[sorted[i]])
		vj := tickerQuoteVolume(tickers[sorted[j]])
		if vi != vj {
			return vi > vj
		}
		return sorted[i] < sorted[j]
	})
	bybitCryptoCache.mu.Lock()
	bybitCryptoCache.sorted = sorted
	if len(bybitCryptoCache.allow) == 0 {
		allow := make(map[string]bool, len(perpMeta))
		for sym := range perpMeta {
			allow[sym] = true
		}
		bybitCryptoCache.allow = allow
	}
	if bybitCryptoCache.fetchedAt.IsZero() {
		bybitCryptoCache.fetchedAt = time.Now()
	}
	bybitCryptoCache.mu.Unlock()
}

func loadBybitSpotBases(ctx context.Context, cli *http.Client) (map[string]bool, bool) {
	bybitSpotCache.mu.Lock()
	if len(bybitSpotCache.bases) > 0 && time.Since(bybitSpotCache.fetchedAt) < cryptoCacheTTL {
		cached := bybitSpotCache.bases
		bybitSpotCache.mu.Unlock()
		return cached, true
	}
	bybitSpotCache.mu.Unlock()

	reqCtx, cancel := context.WithTimeout(ctx, bybitSpotInfoTimeout)
	defer cancel()

	// Bybit V5 spot instruments-info does not accept limit or cursor pagination.
	target := strings.TrimRight(currentBybitRESTEndpoint(), "/") + "/v5/market/instruments-info?category=spot"
	var payload struct {
		RetCode int `json:"retCode"`
		Result  struct {
			List []struct {
				Symbol    string `json:"symbol"`
				Status    string `json:"status"`
				BaseCoin  string `json:"baseCoin"`
				QuoteCoin string `json:"quoteCoin"`
			} `json:"list"`
		} `json:"result"`
	}
	if err := httpGetJSON(reqCtx, cli, target, &payload); err != nil || payload.RetCode != 0 {
		return nil, false
	}

	bases := make(map[string]bool, len(payload.Result.List))
	for _, s := range payload.Result.List {
		if s.Status != "Trading" || s.QuoteCoin != "USDT" {
			continue
		}
		base := data.CanonicalMarketCapBase(s.BaseCoin)
		if base == "" {
			base = data.CanonicalMarketCapBase(s.Symbol)
		}
		if base != "" {
			bases[base] = true
		}
	}
	if len(bases) == 0 {
		return nil, false
	}

	bybitSpotCache.mu.Lock()
	bybitSpotCache.bases = bases
	bybitSpotCache.fetchedAt = time.Now()
	bybitSpotCache.mu.Unlock()

	return bases, true
}
