package deskapi

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ArchdevilForge/kairos/internal/cache"
	"github.com/ArchdevilForge/kairos/internal/cycle"
	"github.com/ArchdevilForge/kairos/internal/decision"
	"github.com/ArchdevilForge/kairos/internal/engine"
	"github.com/ArchdevilForge/kairos/internal/exchange"
	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
)

// Server exposes the local loopback HTTP & WebSocket API for Kairos Desk.
type Server struct {
	pipeline     *engine.Pipeline
	cache        *cache.Store
	acctClient   *exchange.BinanceAccountClient
	restFallback exchange.Exchange
	token        string

	wsHub *wsHub

	bgCtx    context.Context
	bgCancel context.CancelFunc
	bgWG     sync.WaitGroup

	cycleCacheMu sync.Mutex
	cycleCache   map[string]cachedCycle
	cycleWarming map[string]bool

	barsFetchMu sync.Mutex
	barsFetchAt map[string]time.Time
	barsWarming map[string]bool
}

type cachedCycle struct {
	cm types.CycleMap
	at time.Time
}

// NewServer builds a deskapi Server wired to pipeline, derived cache, and bearer auth token.
func NewServer(pipeline *engine.Pipeline, cacheStore *cache.Store, token string) *Server {
	if pipeline != nil && cacheStore != nil {
		restoreNonSecretSettings(context.Background(), cacheStore, pipeline)
	}

	bgCtx, bgCancel := context.WithCancel(context.Background())
	fb, _ := exchange.New("binance")
	s := &Server{
		pipeline:     pipeline,
		cache:        cacheStore,
		acctClient:   exchange.NewBinanceAccountClient(),
		restFallback: fb,
		token:        token,
		bgCtx:        bgCtx,
		bgCancel:     bgCancel,
		cycleCache:   make(map[string]cachedCycle),
		cycleWarming: make(map[string]bool),
		barsFetchAt:  make(map[string]time.Time),
		barsWarming:  make(map[string]bool),
	}
	s.wsHub = newWSHub(s)
	return s
}

func restoreNonSecretSettings(ctx context.Context, cs *cache.Store, p *engine.Pipeline) {
	meta, err := cs.GetAllAccountMeta(ctx)
	if err != nil || len(meta) == 0 {
		return
	}
	var bankrollPtr, maxLossPtr *float64
	var tzPtr *string
	if v, ok := meta["setting_bankroll_usd"]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			bankrollPtr = &f
		}
	}
	if v, ok := meta["setting_max_loss_usd"]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			maxLossPtr = &f
		}
	}
	if v, ok := meta["setting_timezone"]; ok && strings.TrimSpace(v) != "" {
		tz := strings.TrimSpace(v)
		tzPtr = &tz
	}
	if bankrollPtr != nil || maxLossPtr != nil || tzPtr != nil {
		p.UpdateRiskGateConfig(bankrollPtr, maxLossPtr, tzPtr)
	}
}

// Observer returns the DesktopObserver callbacks to register on engine.Pipeline.
func (s *Server) Observer() engine.DesktopObserver {
	return s.wsHub.observer()
}

// Close shuts down background WebSocket upstream subscriptions.
func (s *Server) Close() {
	if s.bgCancel != nil {
		s.bgCancel()
	}
	if s.wsHub != nil {
		s.wsHub.close()
	}
	s.bgWG.Wait()
	if s.restFallback != nil {
		_ = s.restFallback.Close()
	}
}

// Handler returns the HTTP handler with CORS and Bearer/query token enforcement.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("GET /api/v1/bootstrap", s.handleBootstrap)
	mux.HandleFunc("GET /api/v1/market/bars", s.handleMarketBars)
	mux.HandleFunc("GET /api/v1/replay/state", s.handleReplayState)
	mux.HandleFunc("GET /api/v1/drawings", s.handleListDrawings)
	mux.HandleFunc("POST /api/v1/drawings", s.handleCreateDrawing)
	mux.HandleFunc("PUT /api/v1/drawings/{id}", s.handleUpdateDrawing)
	mux.HandleFunc("DELETE /api/v1/drawings/{id}", s.handleDeleteDrawing)
	mux.HandleFunc("DELETE /api/v1/drawings", s.handleDeleteDrawingsForSymbol)
	mux.HandleFunc("POST /api/v1/account/sync", s.handleAccountSync)
	mux.HandleFunc("GET /api/v1/stats", s.handleStats)
	mux.HandleFunc("PUT /api/v1/settings", s.handleUpdateSettings)
	mux.HandleFunc("POST /api/v1/system/wake", s.handleSystemWake)
	mux.HandleFunc("GET /api/v1/ws", s.handleWS)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.URL.Path != "/api/v1/health" && strings.HasPrefix(r.URL.Path, "/api/v1/") {
			if !s.isAuthorized(r) {
				writeError(w, http.StatusUnauthorized, "unauthorized", "UNAUTHORIZED")
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) isAuthorized(r *http.Request) bool {
	if s.token == "" {
		return true
	}
	got := ""
	if auth := strings.TrimSpace(r.Header.Get("Authorization")); auth != "" {
		if len(auth) > 7 && strings.EqualFold(auth[:7], "Bearer ") {
			got = strings.TrimSpace(auth[7:])
		}
	}
	if got == "" {
		got = r.URL.Query().Get("token")
	}
	if len(got) != len(s.token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) == 1
}

// -----------------------------------------------------------------------------
// Health & Bootstrap
// -----------------------------------------------------------------------------

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
		"ts": time.Now().Unix(),
	})
}

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := time.Now()
	cfg := s.activeConfig()
	gcfg := decision.BehaviorGateConfigFrom(cfg.RiskGate)
	loc := decision.ResolveLocation(gcfg.Timezone)

	symbols := s.trackedSymbols(ctx)
	exName := "binance"
	var pulseView engine.MarketPulseView
	var anomalies []types.AnomalyEvent
	if s.pipeline != nil {
		if p := s.pipeline.PrimaryExchange(); p != "" {
			exName = p
		}
		pulseView = s.pipeline.MarketPulseView()
		anomalies = s.pipeline.RecentAnomalies(200)
	} else {
		pulseView.State = types.MarketStateQuiet
	}
	availCtx, availCancel := context.WithTimeout(ctx, 4*time.Second)
	availableSymbols, cryptoAllow, availErr := exchange.FetchBinanceCryptoPerpSymbols(availCtx)
	availCancel()
	if availErr == nil && len(cryptoAllow) > 0 {
		filtered := make([]string, 0, len(symbols))
		for _, sym := range symbols {
			if cryptoAllow[sym] {
				filtered = append(filtered, sym)
			}
		}
		if len(filtered) > 0 {
			symbols = filtered
		}
	} else {
		availableSymbols = symbols
	}
	if anomalies == nil {
		anomalies = []types.AnomalyEvent{}
	}

	var notices []types.Notice
	if nj := s.noticeJournal(); nj != nil {
		if list, err := nj.ListNotices(); err == nil {
			notices = reverseNoticesLimit(list, 200, s.pipeline)
		}
	}
	if notices == nil {
		notices = []types.Notice{}
	}

	syncedTrades := []cache.SyncedTrade{}
	openPositions := []cache.OpenPosition{}
	allFills := []cache.TradeFill{}
	if s.cache != nil {
		if st, err := s.cache.ListSyncedTrades(ctx, ""); err == nil {
			syncedTrades = st
		}
		if op, err := s.cache.ListOpenPositions(ctx); err == nil {
			openPositions = op
		}
		if fl, err := s.cache.ListTradeFills(ctx, "", 0); err == nil {
			allFills = fl
		}
	}

	bundles, gateHealth := s.buildTicketBundlesAndGateHealth(now, gcfg, syncedTrades, allFills)

	// Unplanned trades = synced trades with no linked_ticket_id.
	unplanned := make([]cache.SyncedTrade, 0)
	fillsByTrade := groupFillsByTrade(allFills)
	for _, st := range syncedTrades {
		if st.LinkedTicketID != "" {
			continue
		}
		stCopy := st
		stCopy.Fills = fillsByTrade[st.ID]
		unplanned = append(unplanned, stCopy)
	}

	// Enrich open positions with linked ticket.
	openTradeTicketByKey := make(map[string]string)
	for _, st := range syncedTrades {
		if st.Status == "open" && st.LinkedTicketID != "" {
			openTradeTicketByKey[st.Symbol+"|"+st.PositionSide] = st.LinkedTicketID
		}
	}
	for i := range openPositions {
		pos := &openPositions[i]
		pos.LinkedTicketID = openTradeTicketByKey[pos.Symbol+"|"+pos.PositionSide]
	}

	var mktCycle *types.CycleMap
	if cm, ok := s.getNonBlockingCycle(ctx, "BTC/USDT:USDT", now.Unix()); ok {
		mktCycle = &cm
	}

	pulseDir := ""
	switch pulseView.State {
	case types.MarketStateImpulseUp, types.MarketStateTrendingUp, types.MarketStateStressUp:
		pulseDir = "up"
	case types.MarketStateImpulseDown, types.MarketStateTrendingDown, types.MarketStateStressDown:
		pulseDir = "down"
	}

	snapCopy := pulseView.Snapshot
	payload := BootstrapPayload{
		Status: DeskStatus{
			Exchange:         exName,
			Symbols:          symbols,
			AvailableSymbols: availableSymbols,
			Timezone:         loc.String(),
			InLiveWindow:     gcfg.InLiveWindow(now),
			WindowStartHour:  gcfg.WindowStartHour,
			WindowEndHour:    gcfg.WindowEndHour,
			Pulse: PulseStatus{
				State:           pulseView.State,
				Direction:       pulseDir,
				UpdatedAt:       pulseView.UpdatedAt,
				DataOK:          pulseView.Snapshot.DataOK,
				MedianReturn60s: pulseView.Snapshot.MedianReturn60s,
				MedianReturn300: pulseView.Snapshot.MedianReturn300s,
				MedianZ60s:      pulseView.Snapshot.MedianZ60s,
				UpBreadth60s:    pulseView.Snapshot.UpBreadth60s,
				DownBreadth60s:  pulseView.Snapshot.DownBreadth60s,
				Snapshot:        &snapCopy,
			},
			SeasonRegime:      pulseView.SeasonRegime,
			WinterDefenseHint: types.WinterDefenseHintFor(pulseView.SeasonRegime),
			MarketCycle:       mktCycle,
			GateHealth:        gateHealth,
			RiskConfig: RiskConfigView{
				TradingBankrollUSD: gcfg.TradingBankrollUSD,
				MaxLossUSD:         gcfg.MaxLossUSD,
				MaxMajorLeverage:   gcfg.MaxMajorLeverage,
				MaxAltLeverage:     gcfg.MaxAltLeverage,
				CooldownMinutes:    gcfg.CooldownMinutes,
			},
			Account: s.accountStatus(ctx),
		},
		Tickets:         bundles,
		UnplannedTrades: unplanned,
		OpenPositions:   openPositions,
		Notices:         notices,
		Anomalies:       anomalies,
	}
	writeJSON(w, http.StatusOK, payload)
}

// -----------------------------------------------------------------------------
// Drawings
// -----------------------------------------------------------------------------

func (s *Server) handleListDrawings(w http.ResponseWriter, r *http.Request) {
	if s.cache == nil {
		writeJSON(w, http.StatusOK, []cache.ChartDrawing{})
		return
	}
	sym := canonicalSymbol(r.URL.Query().Get("symbol"))
	list, err := s.cache.ListDrawings(r.Context(), sym, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "CACHE_ERROR")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateDrawing(w http.ResponseWriter, r *http.Request) {
	if s.cache == nil {
		writeError(w, http.StatusServiceUnavailable, "cache store unavailable", "CACHE_UNAVAILABLE")
		return
	}
	var req CreateDrawingRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "INVALID_JSON")
		return
	}
	req.Symbol = canonicalSymbol(req.Symbol)
	if req.Symbol == "" {
		writeError(w, http.StatusBadRequest, "symbol is required", "INVALID_SYMBOL")
		return
	}
	switch req.Kind {
	case "hline", "ruler", "trend", "ray", "region", "fib":
	default:
		writeError(w, http.StatusBadRequest, "kind must be hline|ruler|trend|ray|region|fib", "INVALID_KIND")
		return
	}
	item, err := s.cache.CreateDrawing(r.Context(), cache.ChartDrawing{
		Symbol: req.Symbol,
		Kind:   req.Kind,
		Points: req.Points,
		Label:  req.Label,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "CACHE_ERROR")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleUpdateDrawing(w http.ResponseWriter, r *http.Request) {
	if s.cache == nil {
		writeError(w, http.StatusServiceUnavailable, "cache store unavailable", "CACHE_UNAVAILABLE")
		return
	}
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid drawing id", "INVALID_ID")
		return
	}
	var req UpdateDrawingRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "INVALID_JSON")
		return
	}
	item, err := s.cache.UpdateDrawing(r.Context(), id, req.Points, req.Label)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "drawing not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error(), "CACHE_ERROR")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleDeleteDrawing(w http.ResponseWriter, r *http.Request) {
	if s.cache == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid drawing id", "INVALID_ID")
		return
	}
	if err := s.cache.DeleteDrawing(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "CACHE_ERROR")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteDrawingsForSymbol(w http.ResponseWriter, r *http.Request) {
	if s.cache == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	sym := canonicalSymbol(r.URL.Query().Get("symbol"))
	if sym == "" {
		writeError(w, http.StatusBadRequest, "symbol query param required", "INVALID_SYMBOL")
		return
	}
	if err := s.cache.DeleteDrawingsForSymbol(r.Context(), sym); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "CACHE_ERROR")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// -----------------------------------------------------------------------------
// Account sync, Settings & System Wake
// -----------------------------------------------------------------------------

func (s *Server) handleAccountSync(w http.ResponseWriter, r *http.Request) {
	if s.cache == nil {
		writeError(w, http.StatusServiceUnavailable, "cache store unavailable", "CACHE_UNAVAILABLE")
		return
	}
	if !s.acctClient.HasKeys() {
		writeError(w, http.StatusBadRequest, "binance read-only API credentials not configured", "NO_API_KEYS")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	out, err := s.acctClient.SyncToCache(ctx, s.cache, s.journal(), s.trackedSymbols(ctx))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error(), "BINANCE_SYNC_FAILED")
		return
	}
	acct := s.accountStatus(ctx)
	if positions, err := s.cache.ListOpenPositions(ctx); err == nil {
		s.wsHub.broadcastAccountUpdated(acct, positions)
	} else {
		s.wsHub.broadcastAccountUpdated(acct, nil)
	}
	writeJSON(w, http.StatusOK, AccountSyncResponse{
		Ok:            true,
		SyncedTrades:  out.SyncedTrades,
		SyncedFills:   out.SyncedFills,
		OpenPositions: out.OpenPositions,
		Account:       acct,
	})
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req UpdateSettingsRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "INVALID_JSON")
		return
	}
	if s.pipeline != nil {
		s.pipeline.UpdateRiskGateConfig(req.TradingBankrollUSD, req.MaxLossUSD, req.Timezone)
	}
	if s.cache != nil {
		if req.TradingBankrollUSD != nil && *req.TradingBankrollUSD > 0 {
			_ = s.cache.SetAccountMeta(r.Context(), "setting_bankroll_usd", strconv.FormatFloat(*req.TradingBankrollUSD, 'f', 4, 64))
		}
		if req.MaxLossUSD != nil && *req.MaxLossUSD > 0 {
			_ = s.cache.SetAccountMeta(r.Context(), "setting_max_loss_usd", strconv.FormatFloat(*req.MaxLossUSD, 'f', 4, 64))
		}
		if req.Timezone != nil && strings.TrimSpace(*req.Timezone) != "" {
			_ = s.cache.SetAccountMeta(r.Context(), "setting_timezone", strings.TrimSpace(*req.Timezone))
		}
	}

	var key, sec string
	if req.BinanceReadonlyAPIKey != nil {
		key = strings.TrimSpace(*req.BinanceReadonlyAPIKey)
	}
	if req.BinanceReadonlyAPISecret != nil {
		sec = strings.TrimSpace(*req.BinanceReadonlyAPISecret)
	}
	if key != "" && sec == "" {
		if fields := strings.Fields(key); len(fields) == 2 {
			key, sec = fields[0], fields[1]
		}
	}
	if !s.acctClient.HasKeys() && (key == "") != (sec == "") {
		writeError(w, http.StatusBadRequest, "请同时填写完整的币安只读 API Key 和 API Secret", "INCOMPLETE_API_KEYS")
		return
	}
	if key != "" || sec != "" {
		if err := exchange.SaveBinanceReadonlyKeys(key, sec); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error(), "SECRETS_SAVE_FAILED")
			return
		}
		s.acctClient.SetKeys(key, sec)
		if s.cache != nil {
			_ = s.cache.SetAccountMeta(r.Context(), "sync_error", "")
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleSystemWake(w http.ResponseWriter, r *http.Request) {
	if s.pipeline != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		_ = s.pipeline.CatchUpAndBackfill(ctx)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// -----------------------------------------------------------------------------
// Shared Helpers
// -----------------------------------------------------------------------------

func (s *Server) activeConfig() types.Config {
	if s.pipeline != nil {
		return s.pipeline.Config()
	}
	var cfg types.Config
	cfg.RiskGate.Enabled = true
	cfg.RiskGate.CooldownMinutes = 45
	cfg.RiskGate.WindowStartHour = 18
	cfg.RiskGate.WindowEndHour = 1
	cfg.RiskGate.Timezone = "Asia/Shanghai"
	cfg.RiskGate.TradingBankrollUSD = 40
	cfg.RiskGate.MaxLossUSD = 4
	cfg.RiskGate.MaxMajorLeverage = 10
	cfg.RiskGate.MaxAltLeverage = 5
	cfg.RiskGate.TrancheBaseUSD = 10000
	return cfg
}

func (s *Server) journal() *storage.Journal {
	if s.pipeline != nil {
		return s.pipeline.Journal()
	}
	return nil
}

func (s *Server) noticeJournal() *storage.NoticeJournal {
	if s.pipeline != nil {
		return s.pipeline.NoticeJournal()
	}
	return nil
}

func (s *Server) ohlcvFetcher() exchange.Exchange {
	if s.pipeline != nil {
		if ex := s.pipeline.PrimaryExchangeClient(); ex != nil {
			return ex
		}
	}
	return s.restFallback
}

func (s *Server) trackedSymbols(ctx context.Context) []string {
	var syms []string
	if s.pipeline != nil {
		syms = s.pipeline.TrackedSymbols()
	}
	if len(syms) == 0 && s.cache != nil {
		syms, _ = s.cache.ListCachedSymbols(ctx)
	}
	if len(syms) == 0 {
		syms = []string{"BTC/USDT:USDT", "ETH/USDT:USDT", "SOL/USDT:USDT"}
	}
	return syms
}

func (s *Server) accountStatus(ctx context.Context) AccountStatusView {
	view := AccountStatusView{
		Configured:   s.acctClient.HasKeys(),
		APIKeyMasked: s.acctClient.APIKeyMasked(),
	}
	if s.cache == nil {
		return view
	}
	meta, err := s.cache.GetAllAccountMeta(ctx)
	if err != nil {
		return view
	}
	if v, ok := meta["wallet_balance_usd"]; ok {
		view.WalletBalanceUSD, _ = strconv.ParseFloat(v, 64)
	}
	if v, ok := meta["unrealized_pnl_usd"]; ok {
		view.UnrealizedPnLUSD, _ = strconv.ParseFloat(v, 64)
	}
	if v, ok := meta["last_sync_at"]; ok {
		view.LastSyncAt, _ = strconv.ParseInt(v, 10, 64)
	}
	view.SyncError = meta["sync_error"]
	return view
}

func (s *Server) getNonBlockingCycle(ctx context.Context, symbol string, nowUnix int64) (types.CycleMap, bool) {
	s.cycleCacheMu.Lock()
	cc, hasStale := s.cycleCache[symbol]
	if hasStale && time.Since(cc.at) < 3*time.Minute {
		s.cycleCacheMu.Unlock()
		return cc.cm, true
	}
	s.cycleCacheMu.Unlock()

	if s.cache != nil {
		if cm, ok := s.computeCycleMap(ctx, symbol, nowUnix, true); ok {
			s.cycleCacheMu.Lock()
			s.cycleCache[symbol] = cachedCycle{cm: cm, at: time.Now()}
			s.cycleCacheMu.Unlock()
			return cm, true
		}
	}

	s.cycleCacheMu.Lock()
	if !s.cycleWarming[symbol] && s.bgCtx.Err() == nil {
		s.cycleWarming[symbol] = true
		s.bgWG.Add(1)
		go func(sym string) {
			defer s.bgWG.Done()
			bgCtx, cancel := context.WithTimeout(s.bgCtx, 15*time.Second)
			defer cancel()
			cm, ok := s.computeCycleMap(bgCtx, sym, time.Now().Unix(), false)
			s.cycleCacheMu.Lock()
			delete(s.cycleWarming, sym)
			if ok && s.bgCtx.Err() == nil {
				s.cycleCache[sym] = cachedCycle{cm: cm, at: time.Now()}
			}
			s.cycleCacheMu.Unlock()
		}(symbol)
	}
	s.cycleCacheMu.Unlock()

	if hasStale {
		return cc.cm, true
	}
	return types.CycleMap{}, false
}

func (s *Server) computeCycleMap(ctx context.Context, symbol string, asOfUnix int64, strictCacheOnly bool) (types.CycleMap, bool) {
	tfs := []string{"1d", "4h", "15m", "5m"}
	var series []cycle.Series
	var maxClose int64
	for _, tf := range tfs {
		candles, err := s.loadClosedCandles(ctx, symbol, tf, 90, asOfUnix, strictCacheOnly)
		if err != nil || len(candles) < 40 {
			continue
		}
		closes := make([]float64, len(candles))
		highs := make([]float64, len(candles))
		lows := make([]float64, len(candles))
		vols := make([]float64, len(candles))
		for i, c := range candles {
			closes[i], highs[i], lows[i], vols[i] = c.Close, c.High, c.Low, c.Volume
		}
		openTS := candles[len(candles)-1].Timestamp
		closeTS := openTS + tfDurationSec(tf)
		if closeTS > maxClose {
			maxClose = closeTS
		}
		series = append(series, cycle.Series{
			Timeframe:        tf,
			Role:             roleForTimeframe(tf),
			LastBarUnix:      openTS,
			LastBarCloseUnix: closeTS,
			Closes:           closes,
			Highs:            highs,
			Lows:             lows,
			Volumes:          vols,
		})
	}
	if len(series) == 0 {
		return types.CycleMap{}, false
	}
	if maxClose == 0 {
		maxClose = asOfUnix
	}
	return cycle.MapStateless(symbol, maxClose, "", series), true
}

func roleForTimeframe(tf string) types.TimeframeRole {
	switch tf {
	case "1d", "4h":
		return types.TimeframeRoleContext
	case "1h", "15m":
		return types.TimeframeRoleSetup
	case "5m", "1m":
		return types.TimeframeRoleTrigger
	default:
		return types.TimeframeRoleSetup
	}
}

func reverseNoticesLimit(list []types.Notice, limit int, p *engine.Pipeline) []types.Notice {
	n := len(list)
	if n == 0 {
		return []types.Notice{}
	}
	if limit <= 0 || limit > n {
		limit = n
	}
	out := make([]types.Notice, 0, limit)
	for i := n - 1; i >= 0 && len(out) < limit; i-- {
		nt := list[i]
		if nt.DeliveryStatus == types.NoticeStatusSuppressed {
			continue
		}
		if p != nil && !p.IsVettedSymbol(nt.Symbol) {
			continue
		}
		out = append(out, nt)
	}
	return out
}

func canonicalSymbol(raw string) string {
	s := strings.ToUpper(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	if strings.Contains(s, "/USDT:USDT") {
		return s
	}
	if strings.HasSuffix(s, "-USDT-SWAP") {
		return strings.TrimSuffix(s, "-USDT-SWAP") + "/USDT:USDT"
	}
	if strings.HasSuffix(s, "-USDT") {
		return strings.TrimSuffix(s, "-USDT") + "/USDT:USDT"
	}
	if strings.HasSuffix(s, "/USDT") {
		return s + ":USDT"
	}
	if !strings.ContainsAny(s, "/-:") && strings.HasSuffix(s, "USDT") && len(s) > 4 {
		return strings.TrimSuffix(s, "USDT") + "/USDT:USDT"
	}
	return s
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, msg, code string) {
	body := map[string]string{"error": msg}
	if code != "" {
		body["code"] = code
	}
	writeJSON(w, status, body)
}
