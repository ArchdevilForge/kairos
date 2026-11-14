package deskapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ArchdevilForge/kairos/internal/cache"
	"github.com/ArchdevilForge/kairos/internal/engine"
	"github.com/ArchdevilForge/kairos/internal/exchange"
	"github.com/ArchdevilForge/kairos/internal/types"
	"github.com/coder/websocket"
)

const tickerThrottleIntervalMs = 500

type wsRawJSON []byte

type klineSubKey struct {
	symbol string
	tf     string
}

type upstreamKlineSub struct {
	refs   int
	cancel context.CancelFunc
}

type wsHub struct {
	server       *Server
	ctx          context.Context
	cancel       context.CancelFunc
	clientCount  atomic.Int32
	mu           sync.Mutex
	closed       bool
	clients      map[*wsClient]struct{}
	klineSubs    map[klineSubKey]*upstreamKlineSub
	lastTickerAt map[string]int64
	wg           sync.WaitGroup
}

func newWSHub(srv *Server) *wsHub {
	ctx, cancel := context.WithCancel(context.Background())
	return &wsHub{
		server:       srv,
		ctx:          ctx,
		cancel:       cancel,
		clients:      make(map[*wsClient]struct{}),
		klineSubs:    make(map[klineSubKey]*upstreamKlineSub),
		lastTickerAt: make(map[string]int64),
	}
}

func (h *wsHub) hasNoClients() bool {
	return h == nil || h.clientCount.Load() == 0
}

func (h *wsHub) close() {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.closed = true
	h.cancel()
	for _, sub := range h.klineSubs {
		sub.cancel()
	}
	h.klineSubs = make(map[klineSubKey]*upstreamKlineSub)
	for c := range h.clients {
		_ = c.conn.Close(websocket.StatusGoingAway, "server shutdown")
	}
	h.mu.Unlock()
	h.wg.Wait()
}

func (h *wsHub) observer() engine.DesktopObserver {
	return engine.DesktopObserver{
		OnTicker: func(_ string, t types.Ticker) {
			if h.hasNoClients() || t.LastPrice == nil {
				return
			}
			nowMs := time.Now().UnixMilli()
			h.mu.Lock()
			if lastMs, ok := h.lastTickerAt[t.Symbol]; ok && nowMs-lastMs < tickerThrottleIntervalMs {
				h.mu.Unlock()
				return
			}
			var msg wsTickerMsg
			sent := false
			for c := range h.clients {
				if !c.wantsSymbol(t.Symbol) {
					continue
				}
				if !sent {
					msg = wsTickerMsg{
						Type:           "ticker",
						Symbol:         t.Symbol,
						TS:             nowMs / 1000,
						Price:          *t.LastPrice,
						Change24hPct:   t.ChangePct,
						QuoteVolume24h: t.QuoteVolume,
						OpenInterest:   t.OpenInterest,
						FundingRate:    t.FundingRate,
					}
					sent = true
					h.lastTickerAt[t.Symbol] = nowMs
				}
				c.enqueueTicker(msg)
			}
			h.mu.Unlock()
		},
		OnPulse: func(view engine.MarketPulseView) {
			if h.hasNoClients() {
				return
			}
			dir := ""
			switch view.State {
			case types.MarketStateImpulseUp, types.MarketStateTrendingUp, types.MarketStateStressUp:
				dir = "up"
			case types.MarketStateImpulseDown, types.MarketStateTrendingDown, types.MarketStateStressDown:
				dir = "down"
			}
			snap := view.Snapshot
			msg := wsPulseMsg{
				Type: "pulse",
				Pulse: PulseStatus{
					State:           view.State,
					Direction:       dir,
					UpdatedAt:       view.UpdatedAt,
					DataOK:          view.Snapshot.DataOK,
					MedianReturn60s: view.Snapshot.MedianReturn60s,
					MedianReturn300: view.Snapshot.MedianReturn300s,
					MedianZ60s:      view.Snapshot.MedianZ60s,
					UpBreadth60s:    view.Snapshot.UpBreadth60s,
					DownBreadth60s:  view.Snapshot.DownBreadth60s,
					Snapshot:        &snap,
				},
				SeasonRegime: view.SeasonRegime,
			}
			b, err := json.Marshal(msg)
			if err != nil {
				return
			}
			raw := wsRawJSON(b)
			h.mu.Lock()
			for c := range h.clients {
				c.enqueuePulse(raw)
			}
			h.mu.Unlock()
		},
		OnAnomaly: func(evt types.AnomalyEvent) {
			h.broadcastDiscrete(map[string]any{
				"type":    "anomaly",
				"anomaly": evt,
			})
		},
		OnNotice: func(n types.Notice) {
			h.broadcastDiscrete(map[string]any{
				"type":   "notice",
				"notice": n,
			})
		},
	}
}

func (h *wsHub) broadcastAccountUpdated(acct AccountStatusView, positions []cache.OpenPosition) {
	if h.hasNoClients() {
		return
	}
	h.broadcastDiscrete(map[string]any{
		"type":           "account_updated",
		"account":        acct,
		"open_positions": positions,
	})
}

func (h *wsHub) broadcastDiscrete(payload any) {
	if h.hasNoClients() {
		return
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	raw := wsRawJSON(b)
	h.mu.Lock()
	for c := range h.clients {
		c.enqueueDiscreteRaw(raw)
	}
	h.mu.Unlock()
}

func (h *wsHub) onUpstreamKline(sym, tf string, candle types.Candle, closed bool) {
	if h.server != nil && h.server.cache != nil && closed {
		ctx, cancel := context.WithTimeout(h.ctx, 2*time.Second)
		_ = h.server.cache.UpsertKline(ctx, sym, tf, "binance", candle)
		cancel()
	}
	if h.hasNoClients() {
		return
	}
	msg := wsKlineMsg{
		Type:   "kline",
		Symbol: sym,
		TF:     tf,
		Bar: Bar{
			Time:     candle.Timestamp,
			Open:     candle.Open,
			High:     candle.High,
			Low:      candle.Low,
			Close:    candle.Close,
			Volume:   candle.Volume,
			QuoteVol: candle.QuoteVol,
		},
		Closed: closed,
	}
	key := klineSubKey{symbol: sym, tf: tf}
	h.mu.Lock()
	for c := range h.clients {
		if c.wantsKline(sym, tf) {
			c.enqueueKline(key, msg)
		}
	}
	h.mu.Unlock()
}

func (h *wsHub) addClient(c *wsClient) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.clientCount.Store(int32(len(h.clients)))
	h.mu.Unlock()
}

func (h *wsHub) removeClient(c *wsClient) {
	c.subMu.Lock()
	oldKlines := c.subKlines
	c.subSymbols = nil
	c.subKlines = nil
	c.subMu.Unlock()

	h.mu.Lock()
	delete(h.clients, c)
	h.clientCount.Store(int32(len(h.clients)))
	for key := range oldKlines {
		h.releaseKlineLocked(key)
	}
	h.mu.Unlock()
}

type wsSubSpec struct {
	Symbol string   `json:"symbol"`
	TFs    []string `json:"tfs"`
}

func (h *wsHub) setClientSubscriptions(c *wsClient, subs []wsSubSpec) []wsSubSpec {
	newSyms := make(map[string]bool, len(subs))
	newKlines := make(map[klineSubKey]bool, len(subs)*2)
	applied := make([]wsSubSpec, 0, len(subs))

	for _, item := range subs {
		sym := canonicalSymbol(item.Symbol)
		if sym == "MARKET" {
			sym = "BTC/USDT:USDT"
		}
		if sym == "" {
			continue
		}
		newSyms[sym] = true
		normTFs := make([]string, 0, len(item.TFs))
		for _, rawTF := range item.TFs {
			tf := normalizeTF(rawTF)
			key := klineSubKey{symbol: sym, tf: tf}
			if !newKlines[key] {
				newKlines[key] = true
				normTFs = append(normTFs, tf)
			}
		}
		applied = append(applied, wsSubSpec{Symbol: sym, TFs: normTFs})
	}

	c.subMu.Lock()
	oldKlines := c.subKlines
	c.subSymbols = newSyms
	c.subKlines = newKlines
	c.subMu.Unlock()

	h.mu.Lock()
	for key := range newKlines {
		if !oldKlines[key] {
			h.acquireKlineLocked(key)
		}
	}
	for key := range oldKlines {
		if !newKlines[key] {
			h.releaseKlineLocked(key)
		}
	}
	h.mu.Unlock()
	return applied
}

func (h *wsHub) acquireKlineLocked(key klineSubKey) {
	if h.closed {
		return
	}
	if sub, ok := h.klineSubs[key]; ok {
		sub.refs++
		return
	}
	subCtx, cancel := context.WithCancel(h.ctx)
	h.klineSubs[key] = &upstreamKlineSub{refs: 1, cancel: cancel}

	sym, tf := key.symbol, key.tf
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		cb := func(candle types.Candle, closed bool) {
			h.onUpstreamKline(sym, tf, candle, closed)
		}
		if h.server != nil && h.server.pipeline != nil {
			if ex := h.server.pipeline.PrimaryExchangeClient(); ex != nil {
				if ks, ok := ex.(exchange.KlineSubscriber); ok {
					_ = ks.SubscribeKline(subCtx, sym, tf, cb)
					return
				}
			}
		}
		_ = exchange.SubscribeKline(subCtx, sym, tf, cb)
	}()
}

func (h *wsHub) releaseKlineLocked(key klineSubKey) {
	sub, ok := h.klineSubs[key]
	if !ok {
		return
	}
	sub.refs--
	if sub.refs <= 0 {
		sub.cancel()
		delete(h.klineSubs, key)
	}
}

// -----------------------------------------------------------------------------
// WebSocket handler & 16ms coalescing client
// -----------------------------------------------------------------------------

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	client := &wsClient{
		hub:     s.wsHub,
		conn:    conn,
		tickers: make(map[string]wsTickerMsg),
		klines:  make(map[klineSubKey]wsKlineMsg),
		done:    make(chan struct{}),
	}
	s.wsHub.addClient(client)
	defer s.wsHub.removeClient(client)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go client.flushLoop(ctx)

	for {
		_, msg, err := conn.Read(ctx)
		if err != nil {
			close(client.done)
			return
		}
		var req struct {
			Type   string      `json:"type"`
			Symbol string      `json:"symbol"`
			TFs    []string    `json:"tfs"`
			Subs   []wsSubSpec `json:"subs"`
		}
		if err := json.Unmarshal(msg, &req); err != nil {
			client.enqueueDiscrete(map[string]any{
				"type":    "error",
				"message": "invalid JSON subscribe payload",
			})
			continue
		}
		if req.Type == "subscribe" {
			specs := req.Subs
			if len(specs) == 0 && strings.TrimSpace(req.Symbol) != "" {
				specs = []wsSubSpec{{Symbol: req.Symbol, TFs: req.TFs}}
			}
			applied := s.wsHub.setClientSubscriptions(client, specs)
			firstSym := ""
			var firstTFs []string
			if len(applied) > 0 {
				firstSym = applied[0].Symbol
				firstTFs = applied[0].TFs
			}
			client.enqueueDiscrete(map[string]any{
				"type":   "subscribed",
				"symbol": firstSym,
				"tfs":    firstTFs,
				"subs":   applied,
			})
		}
	}
}

type wsTickerMsg struct {
	Type           string   `json:"type"`
	Symbol         string   `json:"symbol"`
	TS             int64    `json:"ts"`
	Price          float64  `json:"price"`
	Change24hPct   *float64 `json:"change_24h_pct,omitempty"`
	QuoteVolume24h *float64 `json:"quote_volume_24h,omitempty"`
	OpenInterest   *float64 `json:"open_interest,omitempty"`
	FundingRate    *float64 `json:"funding_rate,omitempty"`
}

type wsKlineMsg struct {
	Type   string `json:"type"`
	Symbol string `json:"symbol"`
	TF     string `json:"tf"`
	Bar    Bar    `json:"bar"`
	Closed bool   `json:"closed"`
}

type wsPulseMsg struct {
	Type         string             `json:"type"`
	Pulse        PulseStatus        `json:"pulse"`
	SeasonRegime types.SeasonRegime `json:"season_regime,omitempty"`
}

type wsClient struct {
	hub  *wsHub
	conn *websocket.Conn

	subMu      sync.RWMutex
	subSymbols map[string]bool
	subKlines  map[klineSubKey]bool

	bufMu    sync.Mutex
	tickers  map[string]wsTickerMsg
	klines   map[klineSubKey]wsKlineMsg
	pulse    wsRawJSON
	discrete []wsRawJSON

	done chan struct{}
}

func (c *wsClient) wantsSymbol(sym string) bool {
	c.subMu.RLock()
	match := c.subSymbols[sym]
	c.subMu.RUnlock()
	return match
}

func (c *wsClient) wantsKline(sym, tf string) bool {
	c.subMu.RLock()
	match := c.subKlines[klineSubKey{symbol: sym, tf: tf}]
	c.subMu.RUnlock()
	return match
}

func (c *wsClient) enqueueTicker(msg wsTickerMsg) {
	c.bufMu.Lock()
	c.tickers[msg.Symbol] = msg
	c.bufMu.Unlock()
}

func (c *wsClient) enqueueKline(key klineSubKey, msg wsKlineMsg) {
	c.bufMu.Lock()
	if prev, ok := c.klines[key]; ok && prev.Closed && !msg.Closed && prev.Bar.Time == msg.Bar.Time {
		msg.Closed = true
	}
	c.klines[key] = msg
	c.bufMu.Unlock()
}

func (c *wsClient) enqueuePulse(raw wsRawJSON) {
	c.bufMu.Lock()
	c.pulse = raw
	c.bufMu.Unlock()
}

func (c *wsClient) enqueueDiscrete(msg any) {
	b, err := json.Marshal(msg)
	if err != nil {
		return
	}
	c.enqueueDiscreteRaw(wsRawJSON(b))
}

func (c *wsClient) enqueueDiscreteRaw(raw wsRawJSON) {
	c.bufMu.Lock()
	const maxDiscreteQueue = 256
	if len(c.discrete) >= maxDiscreteQueue {
		c.discrete = c.discrete[1:]
	}
	c.discrete = append(c.discrete, raw)
	c.bufMu.Unlock()
}

func (c *wsClient) flushLoop(ctx context.Context) {
	ticker := time.NewTicker(16 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case <-ticker.C:
			c.bufMu.Lock()
			if len(c.tickers) == 0 && len(c.klines) == 0 && len(c.pulse) == 0 && len(c.discrete) == 0 {
				c.bufMu.Unlock()
				continue
			}
			var tickers map[string]wsTickerMsg
			if len(c.tickers) > 0 {
				tickers = c.tickers
				c.tickers = make(map[string]wsTickerMsg)
			}
			var klines map[klineSubKey]wsKlineMsg
			if len(c.klines) > 0 {
				klines = c.klines
				c.klines = make(map[klineSubKey]wsKlineMsg)
			}
			pulse := c.pulse
			discrete := c.discrete
			c.pulse = nil
			c.discrete = nil
			c.bufMu.Unlock()

			for _, item := range discrete {
				if !c.writeRaw(ctx, item) {
					return
				}
			}
			if len(pulse) > 0 {
				if !c.writeRaw(ctx, pulse) {
					return
				}
			}
			for _, tk := range tickers {
				if !c.writeJSON(ctx, tk) {
					return
				}
			}
			for _, kl := range klines {
				if !c.writeJSON(ctx, kl) {
					return
				}
			}
		}
	}
}

func (c *wsClient) writeJSON(ctx context.Context, payload any) bool {
	b, err := json.Marshal(payload)
	if err != nil {
		return true
	}
	return c.writeRaw(ctx, b)
}

func (c *wsClient) writeRaw(ctx context.Context, b []byte) bool {
	wCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	err := c.conn.Write(wCtx, websocket.MessageText, b)
	cancel()
	if err != nil {
		_ = c.conn.Close(websocket.StatusNormalClosure, "write error")
		return false
	}
	return true
}
