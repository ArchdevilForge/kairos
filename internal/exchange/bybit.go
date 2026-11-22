package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
	"github.com/coder/websocket"
)

const (
	bybitRESTEndpoint           = "https://api.bybit.com"
	DefaultBybitWSEndpoint      = "wss://stream.bybit.com/v5/public/linear"
	defaultBybitPingInterval    = 20 * time.Second
	bybitMaxSubscribeArgsPerMsg = 10
	bybitMaxSubscribeArgsChars  = 21000
)

var (
	bybitWSEndpoint = DefaultBybitWSEndpoint
	bybitRESTBase   = bybitRESTEndpoint

	bybitTestMu            sync.RWMutex
	bybitTransportOverride http.RoundTripper
)

var sharedBybitTransport = func() *http.Transport {
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		cloned := base.Clone()
		cloned.Proxy = proxyFromConfigOrEnv
		cloned.MaxIdleConns = 64
		cloned.MaxIdleConnsPerHost = 16
		cloned.IdleConnTimeout = 90 * time.Second
		return cloned
	}
	return &http.Transport{
		Proxy:               proxyFromConfigOrEnv,
		MaxIdleConns:        64,
		MaxIdleConnsPerHost: 16,
		IdleConnTimeout:     90 * time.Second,
	}
}()

func pooledBybitClientTransport() http.RoundTripper {
	bybitTestMu.RLock()
	rt := bybitTransportOverride
	bybitTestMu.RUnlock()
	if rt != nil {
		return rt
	}
	return sharedBybitTransport
}

func newBybitPooledHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &http.Client{
		Transport: pooledBybitClientTransport(),
		Timeout:   timeout,
	}
}

func currentBybitWSEndpoint() string {
	bybitTestMu.RLock()
	ep := bybitWSEndpoint
	bybitTestMu.RUnlock()
	return ep
}

func currentBybitRESTEndpoint() string {
	bybitTestMu.RLock()
	ep := bybitRESTBase
	bybitTestMu.RUnlock()
	return ep
}

// SetBybitTestHooks overrides the pooled Bybit HTTP transport and WebSocket endpoint
// and resets in-memory Bybit discovery/universe caches for deterministic offline tests.
func SetBybitTestHooks(rt http.RoundTripper, wsEndpoint string) func() {
	bybitTestMu.Lock()
	prevRT := bybitTransportOverride
	prevWS := bybitWSEndpoint
	bybitTransportOverride = rt
	if wsEndpoint != "" {
		bybitWSEndpoint = wsEndpoint
	}
	bybitTestMu.Unlock()
	resetBybitTestCaches()

	return func() {
		bybitTestMu.Lock()
		bybitTransportOverride = prevRT
		bybitWSEndpoint = prevWS
		bybitTestMu.Unlock()
		resetBybitTestCaches()
	}
}

func resetBybitTestCaches() {
	bybitCryptoCache.mu.Lock()
	bybitCryptoCache.fetchedAt = time.Time{}
	bybitCryptoCache.allow = nil
	bybitCryptoCache.sorted = nil
	bybitCryptoCache.mu.Unlock()

	bybitTradableCache.mu.Lock()
	bybitTradableCache.fetchedAt = time.Time{}
	bybitTradableCache.allow = nil
	bybitTradableCache.sorted = nil
	bybitTradableCache.mu.Unlock()

	bybitUniverseInfoCache.mu.Lock()
	bybitUniverseInfoCache.fetchedAt = time.Time{}
	bybitUniverseInfoCache.meta = nil
	bybitUniverseInfoCache.mu.Unlock()

	bybitSpotCache.mu.Lock()
	bybitSpotCache.fetchedAt = time.Time{}
	bybitSpotCache.bases = nil
	bybitSpotCache.mu.Unlock()
}

type bybitExchange struct {
	httpCli      *http.Client
	restEndpoint string
	wsEndpoint   string
	pingInterval time.Duration
	feed         feedState
	ws           wsLifecycle
}

func newBybit() *bybitExchange { return newBybitWithEndpoint("") }

// newBybitWithEndpoint builds a Bybit adapter with an optional WS endpoint
// override; an empty endpoint keeps DefaultBybitWSEndpoint.
func newBybitWithEndpoint(wsEndpoint string) *bybitExchange {
	return newBybitWithEndpoints("", wsEndpoint)
}

func newBybitWithEndpoints(restURL, wsURL string) *bybitExchange {
	rest := strings.TrimSpace(restURL)
	if rest == "" {
		rest = currentBybitRESTEndpoint()
	}
	rest = strings.TrimRight(rest, "/")

	ws := strings.TrimSpace(wsURL)
	if ws == "" {
		ws = currentBybitWSEndpoint()
	}
	return &bybitExchange{
		httpCli:      newBybitPooledHTTPClient(15 * time.Second),
		restEndpoint: rest,
		wsEndpoint:   ws,
		pingInterval: defaultBybitPingInterval,
	}
}

func (b *bybitExchange) effectiveWSEndpoint() string {
	if ep := strings.TrimSpace(b.wsEndpoint); ep != "" {
		return ep
	}
	return currentBybitWSEndpoint()
}

func (b *bybitExchange) effectiveRESTEndpoint() string {
	if ep := strings.TrimSpace(b.restEndpoint); ep != "" {
		return strings.TrimRight(ep, "/")
	}
	return strings.TrimRight(currentBybitRESTEndpoint(), "/")
}

func (b *bybitExchange) effectivePingInterval() time.Duration {
	if b.pingInterval > 0 {
		return b.pingInterval
	}
	return defaultBybitPingInterval
}

func (b *bybitExchange) Name() string { return "bybit" }

// FeedStats reports ticker-feed liveness for the silent-feed watchdog.
func (b *bybitExchange) FeedStats() FeedStats { return b.feed.stats() }

type bybitWSConn struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
}

func (c *bybitWSConn) writeText(ctx context.Context, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.Write(ctx, websocket.MessageText, payload)
}

func (c *bybitWSConn) writeJSON(ctx context.Context, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.writeText(ctx, raw)
}

func startBybitPingLoop(ctx context.Context, wconn *bybitWSConn, interval time.Duration) func() {
	if interval <= 0 {
		interval = defaultBybitPingInterval
	}
	pingCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-pingCtx.Done():
				return
			case <-ticker.C:
				if err := wconn.writeText(pingCtx, []byte(`{"op":"ping"}`)); err != nil {
					return
				}
			}
		}
	}()
	return func() {
		cancel()
		wg.Wait()
	}
}

type bybitWSControl struct {
	Op      string `json:"op"`
	Success *bool  `json:"success"`
	RetMsg  string `json:"ret_msg"`
}

// handleBybitControlFrame processes Bybit V5 WS control frames (pong, ping, subscribe ack).
// Returns (true, err) when msg was a control frame and requires no further data parsing.
func handleBybitControlFrame(ctx context.Context, wconn *bybitWSConn, msg []byte) (bool, error) {
	var ctrl bybitWSControl
	if err := json.Unmarshal(msg, &ctrl); err != nil || ctrl.Op == "" {
		return false, nil
	}
	switch ctrl.Op {
	case "pong":
		return true, nil
	case "ping":
		// Bybit responds to client {"op":"ping"} with op:"ping", ret_msg:"pong", success:true.
		if strings.EqualFold(strings.TrimSpace(ctrl.RetMsg), "pong") || ctrl.Success != nil {
			return true, nil
		}
		_ = wconn.writeText(ctx, []byte(`{"op":"pong"}`))
		return true, nil
	case "subscribe":
		if ctrl.Success != nil && !*ctrl.Success {
			return true, fmt.Errorf("bybit ws subscribe failed: %s", strings.TrimSpace(ctrl.RetMsg))
		}
		return true, nil
	default:
		return false, nil
	}
}

func chunkBybitSubscribeArgs(args []string, maxCount, maxChars int) [][]string {
	if len(args) == 0 {
		return nil
	}
	if maxCount <= 0 {
		maxCount = bybitMaxSubscribeArgsPerMsg
	}
	if maxChars <= 0 {
		maxChars = bybitMaxSubscribeArgsChars
	}
	var chunks [][]string
	var cur []string
	curChars := 0
	for _, arg := range args {
		// Reserve 3 chars per item for JSON quotes and comma separator ("...",).
		itemCost := len(arg) + 3
		if len(cur) > 0 && (len(cur) >= maxCount || curChars+itemCost > maxChars) {
			chunks = append(chunks, cur)
			cur = nil
			curChars = 0
		}
		cur = append(cur, arg)
		curChars += itemCost
	}
	if len(cur) > 0 {
		chunks = append(chunks, cur)
	}
	return chunks
}

func (b *bybitExchange) SubscribeTickers(ctx context.Context, symbols []string, tickerCh chan<- types.Ticker) error {
	ctx, err := b.ws.begin(ctx, "bybit")
	if err != nil {
		return err
	}

	args := make([]string, 0, len(symbols))
	lookup := make(map[string]string, len(symbols))
	totalChars := 0
	for _, sym := range symbols {
		raw := strings.ToUpper(bybitSymbol(sym))
		if raw == "" {
			continue
		}
		topic := "tickers." + raw
		if totalChars+len(topic) > bybitMaxSubscribeArgsChars {
			break
		}
		totalChars += len(topic)
		args = append(args, topic)
		lookup[raw] = sym
	}
	if len(args) == 0 {
		return fmt.Errorf("bybit: no symbols to subscribe")
	}

	chunks := chunkBybitSubscribeArgs(args, bybitMaxSubscribeArgsPerMsg, bybitMaxSubscribeArgsChars)
	ep := b.effectiveWSEndpoint()
	pingInterval := b.effectivePingInterval()
	backoff := wsInitialBackoff
	flog := newFeedLog("bybit")

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		conn, _, err := websocket.Dial(ctx, ep, newWSDialOptions())
		if err != nil {
			flog.Warn("dial", "ws dial failed, retrying",
				"endpoint", ep, "symbols", len(args), "error", err, "backoff", backoff)
			if err := ctxSleep(ctx, backoff); err != nil {
				return err
			}
			backoff = nextWSBackoff(backoff)
			continue
		}
		flog.Info("ws connected", "endpoint", ep, "symbols", len(args))

		wconn := &bybitWSConn{conn: conn}
		subFailed := false
		for _, chunk := range chunks {
			sub := map[string]any{"op": "subscribe", "args": chunk}
			if err := wconn.writeJSON(ctx, sub); err != nil {
				flog.Warn("subscribe", "ws subscribe failed, retrying",
					"endpoint", ep, "symbols", len(args), "error", err, "backoff", backoff)
				subFailed = true
				break
			}
		}
		if subFailed {
			b.feed.markDisconnected()
			_ = conn.Close(websocket.StatusNormalClosure, "")
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err := ctxSleep(ctx, backoff); err != nil {
				return err
			}
			backoff = nextWSBackoff(backoff)
			continue
		}

		flog.Info("ws subscribed", "endpoint", ep, "symbols", len(args), "chunks", len(chunks))
		b.feed.markSubscribed(time.Now())

		stopPing := startBybitPingLoop(ctx, wconn, pingInterval)
		gotData, err := b.readTickers(ctx, wconn, tickerCh, lookup)
		stopPing()
		b.feed.markDisconnected()
		_ = conn.CloseNow()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if gotData {
			backoff = wsInitialBackoff
		}
		if err != nil {
			flog.Warn("read", "ws read ended, reconnecting",
				"endpoint", ep, "symbols", len(args), "error", err, "backoff", backoff)
		}
		if err := ctxSleep(ctx, backoff); err != nil {
			return err
		}
		backoff = nextWSBackoff(backoff)
	}
}

func (b *bybitExchange) FetchTickers(ctx context.Context) (map[string]*types.Ticker, error) {
	url := b.effectiveRESTEndpoint() + "/v5/market/tickers?category=linear"
	body, err := b.get(ctx, url)
	if err != nil {
		return nil, err
	}
	var resp struct {
		RetCode int `json:"retCode"`
		Result  struct {
			List []struct {
				Symbol            string `json:"symbol"`
				LastPrice         string `json:"lastPrice"`
				Turnover24h       string `json:"turnover24h"`
				Price24hPcnt      string `json:"price24hPcnt"`
				FundingRate       string `json:"fundingRate"`
				OpenInterest      string `json:"openInterest"`
				OpenInterestValue string `json:"openInterestValue"`
			} `json:"list"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("bybit fetch tickers: decode: %w", err)
	}
	if resp.RetCode != 0 {
		return nil, fmt.Errorf("bybit fetch tickers: retCode=%d", resp.RetCode)
	}

	allow, allowErr := b.cryptoPerpAllowlist(ctx)
	result := make(map[string]*types.Ticker, len(resp.Result.List))
	for _, row := range resp.Result.List {
		sym := toCanonicalBybit(row.Symbol)
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
		if v := parseFloat(row.Turnover24h); v > 0 {
			t.QuoteVolume = &v
		}
		if v := parseFloatPtr(row.Price24hPcnt); v != nil {
			pct := *v * 100
			t.ChangePct = &pct
		}
		if v := parseFloatPtr(row.OpenInterest); v != nil {
			t.OpenInterest = v
		}
		if v := parseFloatPtr(row.FundingRate); v != nil {
			t.FundingRate = v
		}
		if v := parseFloatPtr(row.OpenInterestValue); v != nil {
			t.Info["openInterestValue"] = *v
		}
		result[sym] = t
	}
	return result, nil
}

func (b *bybitExchange) cryptoPerpAllowlist(ctx context.Context) (map[string]bool, error) {
	restBase := b.effectiveRESTEndpoint()
	useGlobalCache := restBase == strings.TrimRight(currentBybitRESTEndpoint(), "/")

	if useGlobalCache {
		bybitCryptoCache.mu.Lock()
		if len(bybitCryptoCache.allow) > 0 && time.Since(bybitCryptoCache.fetchedAt) < cryptoCacheTTL {
			cp := make(map[string]bool, len(bybitCryptoCache.allow))
			for k, v := range bybitCryptoCache.allow {
				cp[k] = v
			}
			bybitCryptoCache.mu.Unlock()
			return cp, nil
		}
		bybitCryptoCache.mu.Unlock()
	}

	meta, err := fetchBybitPerpMetaFrom(ctx, b.httpCli, restBase)
	if err != nil {
		return nil, err
	}
	allow := make(map[string]bool, len(meta))
	for sym := range meta {
		allow[sym] = true
	}

	if useGlobalCache {
		now := time.Now()
		bybitUniverseInfoCache.mu.Lock()
		bybitUniverseInfoCache.meta = meta
		bybitUniverseInfoCache.fetchedAt = now
		bybitUniverseInfoCache.mu.Unlock()

		bybitCryptoCache.mu.Lock()
		bybitCryptoCache.allow = allow
		bybitCryptoCache.fetchedAt = now
		bybitCryptoCache.mu.Unlock()
	}

	cp := make(map[string]bool, len(allow))
	for k, v := range allow {
		cp[k] = v
	}
	return cp, nil
}

func (b *bybitExchange) FetchOHLCV(ctx context.Context, symbol, timeframe string, limit int, beforeMs int64) ([]types.Candle, error) {
	if limit <= 0 {
		limit = 200
	} else if limit > 1000 {
		limit = 1000
	}
	url := fmt.Sprintf("%s/v5/market/kline?category=linear&symbol=%s&interval=%s&limit=%d",
		b.effectiveRESTEndpoint(), bybitSymbol(symbol), mapBybitTimeframe(timeframe), limit)
	if beforeMs > 0 {
		// `end` is inclusive; -1 makes the shared cursor contract exclusive.
		// With only `end` set Bybit returns the most recent bars up to it.
		url += fmt.Sprintf("&end=%d", beforeMs-1)
	}

	body, err := b.get(ctx, url)
	if err != nil {
		return nil, err
	}
	var resp struct {
		RetCode int `json:"retCode"`
		Result  struct {
			List [][]string `json:"list"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("bybit fetch ohlcv: decode: %w", err)
	}
	if resp.RetCode != 0 {
		return nil, fmt.Errorf("bybit fetch ohlcv: retCode=%d", resp.RetCode)
	}

	candles := make([]types.Candle, 0, len(resp.Result.List))
	for _, row := range resp.Result.List {
		if len(row) < 6 {
			continue
		}
		ts := int64(parseFloat(row[0]))
		// Bybit /v5/market/kline?category=linear: [startTime, open, high, low, close, volume, turnover].
		// row[6] is turnover in quote coin (USDT for linear USDT perpetuals).
		var quoteVol float64
		if len(row) > 6 {
			quoteVol = parseFloat(row[6])
		}
		candles = append(candles, types.Candle{
			Timestamp: ts / 1000,
			Open:      parseFloat(row[1]),
			High:      parseFloat(row[2]),
			Low:       parseFloat(row[3]),
			Close:     parseFloat(row[4]),
			Volume:    parseFloat(row[5]),
			QuoteVol:  quoteVol,
		})
	}
	sortCandlesAscending(candles)
	return candles, nil
}

func (b *bybitExchange) Close() error {
	return b.ws.close()
}

func (b *bybitExchange) get(ctx context.Context, url string) ([]byte, error) {
	body, err := doHTTPGet(ctx, b.httpCli, url)
	if err != nil {
		return nil, fmt.Errorf("bybit request: %w", err)
	}
	return body, nil
}

func (b *bybitExchange) readTickers(ctx context.Context, wconn *bybitWSConn, tickerCh chan<- types.Ticker, lookup map[string]string) (bool, error) {
	stateBySym := make(map[string]*types.Ticker, len(lookup))
	gotData := false

	for {
		_, msg, err := wconn.conn.Read(ctx)
		if err != nil {
			return gotData, err
		}
		handled, ctrlErr := handleBybitControlFrame(ctx, wconn, msg)
		if ctrlErr != nil {
			return gotData, ctrlErr
		}
		if handled {
			continue
		}

		var raw struct {
			Topic string         `json:"topic"`
			Type  string         `json:"type"`
			Data  map[string]any `json:"data"`
		}
		if err := json.Unmarshal(msg, &raw); err != nil {
			continue
		}
		if raw.Data == nil || !strings.HasPrefix(raw.Topic, "tickers.") {
			continue
		}

		symbolRaw, _ := raw.Data["symbol"].(string)
		if symbolRaw == "" {
			symbolRaw = strings.TrimPrefix(raw.Topic, "tickers.")
		}
		sym := lookup[strings.ToUpper(symbolRaw)]
		if sym == "" {
			sym = toCanonicalBybit(symbolRaw)
		}
		if sym == "" {
			continue
		}

		b.feed.markMessage(time.Now())
		gotData = true

		// Bybit V5 tickers push an initial "snapshot" followed by sparse "delta"
		// updates carrying only changed fields. Merge into per-symbol state so
		// unchanged fields survive delta frames.
		var cur *types.Ticker
		if strings.EqualFold(raw.Type, "snapshot") || stateBySym[sym] == nil {
			cur = &types.Ticker{Symbol: sym, Info: map[string]any{"instType": "SWAP"}}
			stateBySym[sym] = cur
		} else {
			cur = stateBySym[sym]
		}
		mergeBybitTickerData(cur, raw.Data)

		out := cloneTicker(cur)
		select {
		case tickerCh <- out:
		default:
		}
	}
}

func bybitDataField(data map[string]any, key string) (string, bool) {
	rawVal, ok := data[key]
	if !ok || rawVal == nil {
		return "", false
	}
	s := strings.TrimSpace(fmt.Sprint(rawVal))
	if s == "" {
		return "", false
	}
	return s, true
}

func mergeBybitTickerData(dst *types.Ticker, data map[string]any) {
	if s, ok := bybitDataField(data, "lastPrice"); ok {
		if v := parseFloat(s); v > 0 {
			dst.LastPrice = &v
		}
	}
	if s, ok := bybitDataField(data, "turnover24h"); ok {
		if v := parseFloat(s); v > 0 {
			dst.QuoteVolume = &v
		}
	}
	if s, ok := bybitDataField(data, "price24hPcnt"); ok {
		if v := parseFloatPtr(s); v != nil {
			pct := *v * 100
			dst.ChangePct = &pct
		}
	}
	if s, ok := bybitDataField(data, "openInterest"); ok {
		if v := parseFloatPtr(s); v != nil {
			dst.OpenInterest = v
		}
	}
	if s, ok := bybitDataField(data, "fundingRate"); ok {
		if v := parseFloatPtr(s); v != nil {
			dst.FundingRate = v
		}
	}
	if s, ok := bybitDataField(data, "openInterestValue"); ok {
		if v := parseFloatPtr(s); v != nil {
			if dst.Info == nil {
				dst.Info = map[string]any{"instType": "SWAP"}
			}
			dst.Info["openInterestValue"] = *v
		}
	}
}

func cloneFloatPtr(src *float64) *float64 {
	if src == nil {
		return nil
	}
	v := *src
	return &v
}

func cloneTicker(src *types.Ticker) types.Ticker {
	if src == nil {
		return types.Ticker{}
	}
	var infoCopy map[string]any
	if len(src.Info) > 0 {
		infoCopy = make(map[string]any, len(src.Info))
		for k, v := range src.Info {
			infoCopy[k] = v
		}
	}
	return types.Ticker{
		Symbol:       src.Symbol,
		LastPrice:    cloneFloatPtr(src.LastPrice),
		QuoteVolume:  cloneFloatPtr(src.QuoteVolume),
		ChangePct:    cloneFloatPtr(src.ChangePct),
		OpenInterest: cloneFloatPtr(src.OpenInterest),
		FundingRate:  cloneFloatPtr(src.FundingRate),
		Info:         infoCopy,
	}
}

func mapBybitTimeframe(tf string) string {
	switch tf {
	case "1m":
		return "1"
	case "3m":
		return "3"
	case "5m":
		return "5"
	case "15m":
		return "15"
	case "30m":
		return "30"
	case "1h":
		return "60"
	case "2h":
		return "120"
	case "4h":
		return "240"
	case "6h":
		return "360"
	case "12h":
		return "720"
	case "1d":
		return "D"
	case "1w":
		return "W"
	case "1M":
		return "M"
	default:
		return "240"
	}
}
