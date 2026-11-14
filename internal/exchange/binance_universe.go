package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ArchdevilForge/kairos/internal/data"
	"github.com/ArchdevilForge/kairos/internal/types"
	"github.com/ArchdevilForge/kairos/internal/utils"
)

const (
	binanceSpotExchangeInfoURL = "https://api.binance.com/api/v3/exchangeInfo?permissions=SPOT"
	binanceExchangeInfoTimeout = 15 * time.Second
	binanceSpotInfoTimeout     = 5 * time.Second
	binanceOIBatchTimeout      = 10 * time.Second
	binanceOICacheTTL          = 30 * time.Minute
	binanceOIMaxWorkers        = 12
	minFilteredUniverseSize    = 5
)

// UniverseFilterConfig controls Top-100 MarketCap and anti-manipulation filtering
// for Binance USDT-M perpetual discovery.
type UniverseFilterConfig struct {
	MaxMarketCapRank int     // default 100
	MinListingDays   int     // default 30
	MinOIUSD         float64 // default 15_000_000
	MaxOIToMCapRatio float64 // default 0.50
	MinQuoteVol24h   float64 // default 15_000_000
	MaxVolToOIRatio  float64 // default 20.0 (35.0 for rank <= 20)
	RequireSpotPair  bool    // default true (gracefully skipped if spot API unreachable)
}

type binancePerpMeta struct {
	onboardDateMs int64
}

var binanceUniverseInfoCache = struct {
	mu        sync.Mutex
	fetchedAt time.Time
	meta      map[string]binancePerpMeta
}{}

var binanceSpotCache = struct {
	mu        sync.Mutex
	fetchedAt time.Time
	bases     map[string]bool
}{}

type cachedOIEntry struct {
	oiQty     float64
	fetchedAt time.Time
}

var binanceOICache = struct {
	mu      sync.Mutex
	entries map[string]cachedOIEntry
}{
	entries: make(map[string]cachedOIEntry),
}

type universeCandidate struct {
	symbol      string
	quoteVol    float64
	lastPrice   float64
	oiUSD       float64
	cap         data.MarketCapEntry
	isIncumbent bool
	preOI       *float64
}

func normalizeUniverseFilterConfig(cfg UniverseFilterConfig) UniverseFilterConfig {
	if cfg.MaxMarketCapRank <= 0 {
		cfg.MaxMarketCapRank = 100
	}
	if cfg.MinListingDays <= 0 {
		cfg.MinListingDays = 30
	}
	if cfg.MinOIUSD <= 0 {
		cfg.MinOIUSD = 15_000_000
	}
	if cfg.MaxOIToMCapRatio <= 0 {
		cfg.MaxOIToMCapRatio = 0.50
	}
	if cfg.MinQuoteVol24h <= 0 {
		cfg.MinQuoteVol24h = 15_000_000
	}
	if cfg.MaxVolToOIRatio <= 0 {
		cfg.MaxVolToOIRatio = 20.0
	}
	return cfg
}

// FilterBinanceTop100Universe filters Binance USDT-M tickers using MarketCap rank,
// listing age, optional spot pair presence, 24h quote volume, Open Interest floor,
// OI/MarketCap cap, and Vol/OI churn cap. Returns (nil, false) when live metadata
// is unavailable or fewer than 5 symbols pass so callers can safely fall back.
func FilterBinanceTop100Universe(
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

	cli := newPooledHTTPClient(binanceExchangeInfoTimeout)
	perpMeta, ok := loadBinancePerpMeta(ctx, cli)
	if !ok {
		return nil, false
	}
	populateBinanceCryptoSortedFromTickers(perpMeta, tickers)

	var spotBases map[string]bool
	var spotLoaded bool
	if cfg.RequireSpotPair {
		spotBases, spotLoaded = loadBinanceSpotBases(ctx, cli)
	}

	incumbentSet := make(map[string]bool, len(incumbents))
	for _, sym := range incumbents {
		incumbentSet[sym] = true
	}

	nowMs := time.Now().UnixMilli()
	minAgeMs := int64(cfg.MinListingDays) * 24 * int64(time.Hour/time.Millisecond)

	var hasBTC, hasETH bool
	candidates := make([]universeCandidate, 0, len(tickers))

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
		if meta.onboardDateMs <= 0 || nowMs-meta.onboardDateMs < minAgeMs {
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

		candidates = append(candidates, universeCandidate{
			symbol:      sym,
			quoteVol:    quoteVol,
			lastPrice:   tickerLastPrice(t),
			cap:         capEntry,
			isIncumbent: incumbentSet[sym],
			preOI:       t.OpenInterest,
		})
	}

	fetchBinanceOpenInterestBatch(ctx, cli, candidates)

	passing := make([]universeCandidate, 0, len(candidates))
	for _, c := range candidates {
		if c.oiUSD > 0 {
			effMinOI := cfg.MinOIUSD
			if c.isIncumbent {
				effMinOI *= 0.75
			}
			if c.oiUSD < effMinOI {
				continue
			}
			if c.cap.CapUSD <= 0 || c.oiUSD/c.cap.CapUSD > cfg.MaxOIToMCapRatio {
				continue
			}
			maxVolOI := cfg.MaxVolToOIRatio
			if c.cap.Rank <= 20 && maxVolOI < 35.0 {
				maxVolOI = 35.0
			}
			if c.quoteVol/c.oiUSD > maxVolOI {
				continue
			}
		}
		passing = append(passing, c)
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

func universeCandidateScore(oiUSD, quoteVol float64) float64 {
	if oiUSD > 0 {
		return 0.5*math.Log1p(oiUSD) + 0.5*math.Log1p(quoteVol)
	}
	return math.Log1p(quoteVol)
}

func loadBinancePerpMeta(ctx context.Context, cli *http.Client) (map[string]binancePerpMeta, bool) {
	binanceUniverseInfoCache.mu.Lock()
	if len(binanceUniverseInfoCache.meta) > 0 && time.Since(binanceUniverseInfoCache.fetchedAt) < cryptoCacheTTL {
		cached := binanceUniverseInfoCache.meta
		binanceUniverseInfoCache.mu.Unlock()
		return cached, true
	}
	binanceUniverseInfoCache.mu.Unlock()

	reqCtx, cancel := context.WithTimeout(ctx, binanceExchangeInfoTimeout)
	defer cancel()

	var payload struct {
		Symbols []struct {
			Symbol         string `json:"symbol"`
			Status         string `json:"status"`
			ContractType   string `json:"contractType"`
			QuoteAsset     string `json:"quoteAsset"`
			UnderlyingType string `json:"underlyingType"`
			OnboardDate    int64  `json:"onboardDate"`
		} `json:"symbols"`
	}
	if err := httpGetJSON(reqCtx, cli, binanceRESTEndpoint+"/fapi/v1/exchangeInfo", &payload); err != nil {
		return nil, false
	}

	meta := make(map[string]binancePerpMeta, len(payload.Symbols))
	allow := make(map[string]bool, len(payload.Symbols))
	for _, s := range payload.Symbols {
		if s.Status != "TRADING" || s.ContractType != "PERPETUAL" || s.QuoteAsset != "USDT" {
			continue
		}
		if s.UnderlyingType != "" && s.UnderlyingType != "COIN" {
			continue
		}
		canon := toCanonicalBinance(s.Symbol)
		if canon == "" {
			continue
		}
		meta[canon] = binancePerpMeta{onboardDateMs: s.OnboardDate}
		allow[canon] = true
	}
	if len(meta) == 0 {
		return nil, false
	}

	now := time.Now()
	binanceUniverseInfoCache.mu.Lock()
	binanceUniverseInfoCache.meta = meta
	binanceUniverseInfoCache.fetchedAt = now
	binanceUniverseInfoCache.mu.Unlock()

	binanceCryptoCache.mu.Lock()
	if len(binanceCryptoCache.allow) == 0 || time.Since(binanceCryptoCache.fetchedAt) >= cryptoCacheTTL {
		sorted := make([]string, 0, len(allow))
		for sym := range allow {
			sorted = append(sorted, sym)
		}
		sort.Strings(sorted)
		binanceCryptoCache.allow = allow
		if len(binanceCryptoCache.sorted) == 0 {
			binanceCryptoCache.sorted = sorted
		}
		binanceCryptoCache.fetchedAt = now
	}
	binanceCryptoCache.mu.Unlock()

	return meta, true
}

func populateBinanceCryptoSortedFromTickers(perpMeta map[string]binancePerpMeta, tickers map[string]*types.Ticker) {
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
	binanceCryptoCache.mu.Lock()
	binanceCryptoCache.sorted = sorted
	if len(binanceCryptoCache.allow) == 0 {
		allow := make(map[string]bool, len(perpMeta))
		for sym := range perpMeta {
			allow[sym] = true
		}
		binanceCryptoCache.allow = allow
	}
	if binanceCryptoCache.fetchedAt.IsZero() {
		binanceCryptoCache.fetchedAt = time.Now()
	}
	binanceCryptoCache.mu.Unlock()
}

func loadBinanceSpotBases(ctx context.Context, cli *http.Client) (map[string]bool, bool) {
	binanceSpotCache.mu.Lock()
	if len(binanceSpotCache.bases) > 0 && time.Since(binanceSpotCache.fetchedAt) < cryptoCacheTTL {
		cached := binanceSpotCache.bases
		binanceSpotCache.mu.Unlock()
		return cached, true
	}
	binanceSpotCache.mu.Unlock()

	reqCtx, cancel := context.WithTimeout(ctx, binanceSpotInfoTimeout)
	defer cancel()

	var payload struct {
		Symbols []struct {
			Symbol     string `json:"symbol"`
			Status     string `json:"status"`
			BaseAsset  string `json:"baseAsset"`
			QuoteAsset string `json:"quoteAsset"`
		} `json:"symbols"`
	}
	if err := httpGetJSON(reqCtx, cli, binanceSpotExchangeInfoURL, &payload); err != nil {
		return nil, false
	}

	bases := make(map[string]bool, len(payload.Symbols))
	for _, s := range payload.Symbols {
		if s.Status != "TRADING" || s.QuoteAsset != "USDT" {
			continue
		}
		base := data.CanonicalMarketCapBase(s.BaseAsset)
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

	binanceSpotCache.mu.Lock()
	binanceSpotCache.bases = bases
	binanceSpotCache.fetchedAt = time.Now()
	binanceSpotCache.mu.Unlock()

	return bases, true
}

func fetchBinanceOpenInterestBatch(ctx context.Context, cli *http.Client, candidates []universeCandidate) {
	if len(candidates) == 0 {
		return
	}

	oiCtx, cancel := context.WithTimeout(ctx, binanceOIBatchTimeout)
	defer cancel()

	workers := binanceOIMaxWorkers
	if len(candidates) < workers {
		workers = len(candidates)
	}

	var next atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				idx := int(next.Add(1) - 1)
				if idx >= len(candidates) || oiCtx.Err() != nil {
					return
				}
				c := &candidates[idx]
				if c.lastPrice <= 0 {
					continue
				}
				binanceOICache.mu.Lock()
				cachedEntry, hasCached := binanceOICache.entries[c.symbol]
				binanceOICache.mu.Unlock()
				if hasCached && cachedEntry.oiQty > 0 && time.Since(cachedEntry.fetchedAt) < binanceOICacheTTL {
					c.oiUSD = cachedEntry.oiQty * c.lastPrice
					continue
				}
				rawSym := url.QueryEscape(binanceSymbol(c.symbol))
				target := binanceRESTEndpoint + "/fapi/v1/openInterest?symbol=" + rawSym
				var resp struct {
					OpenInterest string `json:"openInterest"`
				}
				if err := httpGetJSON(oiCtx, cli, target, &resp); err == nil {
					if qty := parseFloat(resp.OpenInterest); qty > 0 {
						binanceOICache.mu.Lock()
						binanceOICache.entries[c.symbol] = cachedOIEntry{
							oiQty:     qty,
							fetchedAt: time.Now(),
						}
						binanceOICache.mu.Unlock()
						c.oiUSD = qty * c.lastPrice
						continue
					}
				}
				if c.preOI != nil && *c.preOI > 0 {
					c.oiUSD = *c.preOI * c.lastPrice
				}
			}
		}()
	}
	wg.Wait()
}

func tickerQuoteVolume(t *types.Ticker) float64 {
	if t == nil {
		return 0
	}
	if t.QuoteVolume != nil && *t.QuoteVolume > 0 {
		return *t.QuoteVolume
	}
	if len(t.Info) > 0 {
		return utils.ExtractQuoteVolume(t.Info)
	}
	return 0
}

func tickerLastPrice(t *types.Ticker) float64 {
	if t == nil {
		return 0
	}
	if t.LastPrice != nil && *t.LastPrice > 0 {
		return *t.LastPrice
	}
	if len(t.Info) > 0 {
		if p := utils.ExtractLastPrice(t.Info); p != nil && *p > 0 {
			return *p
		}
	}
	return 0
}

func httpGetJSON(ctx context.Context, cli *http.Client, targetURL string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return err
	}
	resp, err := cli.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, dst)
}
