package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
	"github.com/coder/websocket"
)

const (
	binanceRESTEndpoint = "https://fapi.binance.com"
	cryptoCacheTTL      = time.Hour
)

var binanceWSEndpoint = "wss://fstream.binance.com/market/ws"

var binanceCryptoCache = struct {
	mu        sync.Mutex
	fetchedAt time.Time
	allow     map[string]bool
	sorted    []string
}{}

var sharedBinanceTransport = func() *http.Transport {
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		cloned := base.Clone()
		cloned.MaxIdleConns = 64
		cloned.MaxIdleConnsPerHost = 16
		cloned.IdleConnTimeout = 90 * time.Second
		return cloned
	}
	return &http.Transport{
		MaxIdleConns:        64,
		MaxIdleConnsPerHost: 16,
		IdleConnTimeout:     90 * time.Second,
	}
}()

var (
	binanceTestMu            sync.RWMutex
	binanceTransportOverride http.RoundTripper
)

func pooledClientTransport() http.RoundTripper {
	binanceTestMu.RLock()
	rt := binanceTransportOverride
	binanceTestMu.RUnlock()
	if rt != nil {
		return rt
	}
	return sharedBinanceTransport
}

// SetBinanceTestHooks overrides the pooled Binance HTTP transport and WebSocket endpoint
// and resets in-memory Binance discovery/OI caches for deterministic offline tests.
func SetBinanceTestHooks(rt http.RoundTripper, wsEndpoint string) func() {
	binanceTestMu.Lock()
	prevRT := binanceTransportOverride
	prevWS := binanceWSEndpoint
	binanceTransportOverride = rt
	if wsEndpoint != "" {
		binanceWSEndpoint = wsEndpoint
	}
	binanceTestMu.Unlock()
	resetBinanceTestCaches()

	return func() {
		binanceTestMu.Lock()
		binanceTransportOverride = prevRT
		binanceWSEndpoint = prevWS
		binanceTestMu.Unlock()
		resetBinanceTestCaches()
	}
}

func resetBinanceTestCaches() {
	binanceCryptoCache.mu.Lock()
	binanceCryptoCache.fetchedAt = time.Time{}
	binanceCryptoCache.allow = nil
	binanceCryptoCache.sorted = nil
	binanceCryptoCache.mu.Unlock()

	binanceUniverseInfoCache.mu.Lock()
	binanceUniverseInfoCache.fetchedAt = time.Time{}
	binanceUniverseInfoCache.meta = nil
	binanceUniverseInfoCache.mu.Unlock()

	binanceSpotCache.mu.Lock()
	binanceSpotCache.fetchedAt = time.Time{}
	binanceSpotCache.bases = nil
	binanceSpotCache.mu.Unlock()

	binanceOICache.mu.Lock()
	binanceOICache.entries = make(map[string]cachedOIEntry)
	binanceOICache.mu.Unlock()
}

func newPooledHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &http.Client{
		Transport: pooledClientTransport(),
		Timeout:   timeout,
	}
}

type binanceExchange struct {
	httpCli *http.Client
	feed    feedState
	ws      wsLifecycle
}

func newBinance() *binanceExchange {
	return &binanceExchange{httpCli: newPooledHTTPClient(15 * time.Second)}
}

func (b *binanceExchange) Name() string { return "binance" }

// FeedStats reports ticker-feed liveness for the silent-feed watchdog.
func (b *binanceExchange) FeedStats() FeedStats { return b.feed.stats() }

func (b *binanceExchange) SubscribeTickers(ctx context.Context, symbols []string, tickerCh chan<- types.Ticker) error {
	ctx, err := b.ws.begin(ctx, "binance")
	if err != nil {
		return err
	}

	streams := make([]string, 0, len(symbols))
	lookup := make(map[string]string, len(symbols))
	for _, sym := range symbols {
		raw := strings.ToLower(binanceSymbol(sym))
		streams = append(streams, raw+"@ticker")
		lookup[strings.ToUpper(raw)] = sym
	}
	if len(streams) == 0 {
		return fmt.Errorf("binance: no symbols to subscribe")
	}

	uri := binanceWSEndpoint + "/" + strings.Join(streams, "/")
	backoff := wsInitialBackoff

	flog := newFeedLog("binance")
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		conn, _, err := websocket.Dial(ctx, uri, nil)
		if err != nil {
			flog.Warn("dial", "ws dial failed, retrying",
				"endpoint", binanceWSEndpoint, "symbols", len(streams), "error", err, "backoff", backoff)
			if err := ctxSleep(ctx, backoff); err != nil {
				return err
			}
			backoff = nextWSBackoff(backoff)
			continue
		}
		backoff = wsInitialBackoff
		flog.Info("ws connected", "endpoint", binanceWSEndpoint, "symbols", len(streams))
		// Binance carries the subscription in the dial URL, so "subscribed" is
		// reached as soon as the combined stream socket is up.
		flog.Info("ws subscribed", "endpoint", binanceWSEndpoint, "symbols", len(streams), "mode", "streams_in_url")
		b.feed.markSubscribed(time.Now())
		if err := b.readTickers(ctx, conn, tickerCh, lookup); err != nil {
			b.feed.markDisconnected()
			_ = conn.Close(websocket.StatusNormalClosure, "")
			if ctx.Err() != nil {
				return ctx.Err()
			}
			flog.Warn("read", "ws read ended, reconnecting",
				"endpoint", binanceWSEndpoint, "symbols", len(streams), "error", err, "backoff", time.Duration(0))
			continue
		}
	}
}

func (b *binanceExchange) FetchTickers(ctx context.Context) (map[string]*types.Ticker, error) {
	body, err := b.get(ctx, binanceRESTEndpoint+"/fapi/v1/ticker/24hr")
	if err != nil {
		return nil, err
	}
	var rows []struct {
		Symbol             string `json:"symbol"`
		LastPrice          string `json:"lastPrice"`
		QuoteVolume        string `json:"quoteVolume"`
		PriceChangePercent string `json:"priceChangePercent"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("binance fetch tickers: decode: %w", err)
	}

	result := make(map[string]*types.Ticker, len(rows))
	allow, allowErr := b.cryptoPerpAllowlist(ctx)
	for _, row := range rows {
		sym := toCanonicalBinance(row.Symbol)
		if sym == "" {
			continue
		}
		if allowErr == nil && len(allow) > 0 && !allow[sym] {
			continue
		}
		t := &types.Ticker{Symbol: sym, Info: map[string]any{"instType": "SWAP"}}
		if v := parseFloat(row.LastPrice); v > 0 {
			t.LastPrice = &v
		}
		if v := parseFloat(row.QuoteVolume); v > 0 {
			t.QuoteVolume = &v
		}
		if v := parseFloatPtr(row.PriceChangePercent); v != nil {
			t.ChangePct = v
		}
		result[sym] = t
	}

	if err := b.mergePremiumIndex(ctx, result); err != nil {
		return result, nil // funding optional
	}
	return result, nil
}

func (b *binanceExchange) FetchOHLCV(ctx context.Context, symbol, timeframe string, limit int, beforeMs int64) ([]types.Candle, error) {
	if limit <= 0 || limit > 1500 {
		limit = 500
	}
	url := fmt.Sprintf("%s/fapi/v1/klines?symbol=%s&interval=%s&limit=%d",
		binanceRESTEndpoint, binanceSymbol(symbol), mapBinanceTimeframe(timeframe), limit)
	if beforeMs > 0 {
		// endTime is inclusive; -1 makes the shared cursor contract exclusive.
		// Without startTime Binance returns the most recent bars up to endTime.
		url += fmt.Sprintf("&endTime=%d", beforeMs-1)
	}

	body, err := b.get(ctx, url)
	if err != nil {
		return nil, err
	}
	return parseBinanceKlines(body)
}

func (b *binanceExchange) Close() error {
	return b.ws.close()
}

func (b *binanceExchange) get(ctx context.Context, url string) ([]byte, error) {
	body, err := doHTTPGet(ctx, b.httpCli, url)
	if err != nil {
		return nil, fmt.Errorf("binance request: %w", err)
	}
	return body, nil
}

func (b *binanceExchange) mergePremiumIndex(ctx context.Context, tickers map[string]*types.Ticker) error {
	body, err := b.get(ctx, binanceRESTEndpoint+"/fapi/v1/premiumIndex")
	if err != nil {
		return err
	}
	var rows []struct {
		Symbol          string `json:"symbol"`
		LastFundingRate string `json:"lastFundingRate"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return err
	}
	for _, row := range rows {
		sym := toCanonicalBinance(row.Symbol)
		t, ok := tickers[sym]
		if !ok || t == nil {
			continue
		}
		if v := parseFloatPtr(row.LastFundingRate); v != nil {
			t.FundingRate = v
		}
	}
	return nil
}

func (b *binanceExchange) readTickers(ctx context.Context, conn *websocket.Conn, tickerCh chan<- types.Ticker, lookup map[string]string) error {
	for {
		_, msg, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		b.feed.markMessage(time.Now())
		// Decode into a case-sensitive map because Binance 24hrTicker carries
		// colliding keys (e/E, p/P, q/Q) under Go encoding/json struct matching.
		var m map[string]any
		if err := json.Unmarshal(msg, &m); err != nil {
			continue
		}
		rawSym, _ := m["s"].(string)
		if rawSym == "" {
			continue
		}
		sym := lookup[strings.ToUpper(rawSym)]
		if sym == "" {
			sym = toCanonicalBinance(rawSym)
		}
		t := types.Ticker{Symbol: sym}
		if lastStr, _ := m["c"].(string); lastStr != "" {
			if v := parseFloat(lastStr); v > 0 {
				t.LastPrice = &v
			}
		}
		if qvStr, _ := m["q"].(string); qvStr != "" {
			if v := parseFloat(qvStr); v > 0 {
				t.QuoteVolume = &v
			}
		}
		if pctStr, _ := m["P"].(string); pctStr != "" {
			if v := parseFloatPtr(pctStr); v != nil {
				t.ChangePct = v
			}
		}
		select {
		case tickerCh <- t:
		default:
		}
	}
}

func mapBinanceTimeframe(tf string) string {
	switch tf {
	case "1m", "3m", "5m", "15m", "30m", "1h", "2h", "4h", "6h", "8h", "12h", "1d", "3d", "1w", "1M":
		return tf
	default:
		return "4h"
	}
}

func (b *binanceExchange) cryptoPerpAllowlist(ctx context.Context) (map[string]bool, error) {
	binanceCryptoCache.mu.Lock()
	if len(binanceCryptoCache.allow) > 0 && time.Since(binanceCryptoCache.fetchedAt) < cryptoCacheTTL {
		cp := make(map[string]bool, len(binanceCryptoCache.allow))
		for k, v := range binanceCryptoCache.allow {
			cp[k] = v
		}
		binanceCryptoCache.mu.Unlock()
		return cp, nil
	}
	binanceCryptoCache.mu.Unlock()

	allow, err := fetchCryptoAllowlistFrom(ctx, b.httpCli, binanceRESTEndpoint)
	if err != nil {
		return nil, err
	}
	binanceCryptoCache.mu.Lock()
	binanceCryptoCache.allow = allow
	binanceCryptoCache.fetchedAt = time.Now()
	binanceCryptoCache.mu.Unlock()

	cp := make(map[string]bool, len(allow))
	for k, v := range allow {
		cp[k] = v
	}
	return cp, nil
}

func fetchCryptoAllowlistFrom(ctx context.Context, cli *http.Client, restBase string) (map[string]bool, error) {
	body, err := doHTTPGet(ctx, cli, strings.TrimRight(restBase, "/")+"/fapi/v1/exchangeInfo")
	if err != nil {
		return nil, err
	}
	var payload struct {
		Symbols []struct {
			Symbol         string `json:"symbol"`
			Status         string `json:"status"`
			ContractType   string `json:"contractType"`
			QuoteAsset     string `json:"quoteAsset"`
			UnderlyingType string `json:"underlyingType"`
		} `json:"symbols"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if len(payload.Symbols) == 0 {
		return nil, fmt.Errorf("binance exchangeInfo: empty symbols")
	}
	allow := make(map[string]bool, len(payload.Symbols))
	for _, s := range payload.Symbols {
		if s.Status != "TRADING" || s.ContractType != "PERPETUAL" || s.QuoteAsset != "USDT" {
			continue
		}
		// Reject TradFi equities/commodities/ETFs (underlyingType=EQUITY/COMMODITY).
		if s.UnderlyingType != "" && s.UnderlyingType != "COIN" {
			continue
		}
		if canon := toCanonicalBinance(s.Symbol); canon != "" {
			allow[canon] = true
		}
	}
	if len(allow) == 0 {
		return nil, fmt.Errorf("binance exchangeInfo: zero crypto perpetuals")
	}
	return allow, nil
}

// FetchBinanceCryptoPerpSymbols returns active Binance USDT-M crypto perpetual symbols
// sorted by 24h quote volume descending, along with an O(1) allowlist map (cached 1h).
func FetchBinanceCryptoPerpSymbols(ctx context.Context) ([]string, map[string]bool, error) {
	var cachedAllow map[string]bool
	binanceCryptoCache.mu.Lock()
	if len(binanceCryptoCache.allow) > 0 && time.Since(binanceCryptoCache.fetchedAt) < cryptoCacheTTL {
		allowCopy := make(map[string]bool, len(binanceCryptoCache.allow))
		for k, v := range binanceCryptoCache.allow {
			allowCopy[k] = v
		}
		if len(binanceCryptoCache.sorted) > 0 {
			sortedCopy := append([]string(nil), binanceCryptoCache.sorted...)
			binanceCryptoCache.mu.Unlock()
			return sortedCopy, allowCopy, nil
		}
		cachedAllow = allowCopy
	}
	binanceCryptoCache.mu.Unlock()

	cli := newPooledHTTPClient(15 * time.Second)
	allow := cachedAllow
	if len(allow) == 0 {
		var err error
		allow, err = fetchCryptoAllowlistFrom(ctx, cli, binanceRESTEndpoint)
		if err != nil {
			return nil, nil, err
		}
	}

	vols := make(map[string]float64, len(allow))
	if body, tErr := doHTTPGet(ctx, cli, binanceRESTEndpoint+"/fapi/v1/ticker/24hr"); tErr == nil {
		var rows []struct {
			Symbol      string `json:"symbol"`
			QuoteVolume string `json:"quoteVolume"`
		}
		if json.Unmarshal(body, &rows) == nil {
			for _, r := range rows {
				if canon := toCanonicalBinance(r.Symbol); canon != "" && allow[canon] {
					vols[canon] = parseFloat(r.QuoteVolume)
				}
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

	binanceCryptoCache.mu.Lock()
	binanceCryptoCache.allow = allow
	binanceCryptoCache.sorted = sorted
	binanceCryptoCache.fetchedAt = time.Now()
	binanceCryptoCache.mu.Unlock()

	allowCopy := make(map[string]bool, len(allow))
	for k, v := range allow {
		allowCopy[k] = v
	}
	return append([]string(nil), sorted...), allowCopy, nil
}
