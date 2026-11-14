package deskapi

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ArchdevilForge/kairos/internal/cache"
	"github.com/ArchdevilForge/kairos/internal/exchange"
	"github.com/ArchdevilForge/kairos/internal/types"
)

func (s *Server) handleMarketBars(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sym := canonicalSymbol(r.URL.Query().Get("symbol"))
	if sym == "" || sym == "MARKET" {
		sym = "BTC/USDT:USDT"
	}
	tf := normalizeTF(r.URL.Query().Get("tf"))
	limit := parseLimit(r.URL.Query().Get("limit"), 800, 1500)
	beforeTS, _ := strconv.ParseInt(r.URL.Query().Get("before_ts"), 10, 64)

	if _, allow, err := exchange.FetchBinanceCryptoPerpSymbols(ctx); err == nil && len(allow) > 0 && !allow[sym] {
		writeError(w, http.StatusBadRequest, "仅支持 Crypto 永续合约: "+sym, "NON_CRYPTO_SYMBOL")
		return
	}

	candles := s.loadBarsWithFallback(ctx, sym, tf, limit, beforeTS)
	bars := candlesToBars(candles)

	// Historical pagination scroll-back: return bars quickly without recalculating cycles/metrics.
	if beforeTS > 0 {
		writeJSON(w, http.StatusOK, MarketBarsResponse{
			Symbol:  sym,
			TF:      tf,
			Bars:    bars,
			Metrics: []exchange.MetricsPoint{},
		})
		return
	}

	nowUnix := time.Now().Unix()
	var mktCycle, symCycle *types.CycleMap
	if cm, ok := s.getNonBlockingCycle(ctx, "BTC/USDT:USDT", nowUnix); ok {
		mktCycle = &cm
	}
	if sym == "BTC/USDT:USDT" && mktCycle != nil {
		cp := *mktCycle
		symCycle = &cp
	} else if cm, ok := s.getNonBlockingCycle(ctx, sym, nowUnix); ok {
		symCycle = &cm
	}

	writeJSON(w, http.StatusOK, MarketBarsResponse{
		Symbol:      sym,
		TF:          tf,
		Bars:        bars,
		MarketCycle: mktCycle,
		SymbolCycle: symCycle,
		Box:         nil,
		Metrics:     []exchange.MetricsPoint{},
	})
}

func (s *Server) handleReplayState(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sym := canonicalSymbol(r.URL.Query().Get("symbol"))
	if sym == "" || sym == "MARKET" {
		sym = "BTC/USDT:USDT"
	}
	cursorTS, _ := strconv.ParseInt(r.URL.Query().Get("cursor_ts"), 10, 64)
	if cursorTS <= 0 {
		cursorTS = time.Now().Unix()
	}
	limit := parseLimit(r.URL.Query().Get("limit"), 300, 1500)

	rawTFs := strings.TrimSpace(r.URL.Query().Get("tfs"))
	var tfs []string
	if rawTFs == "" {
		tfs = []string{"4h", "15m", "5m"}
	} else {
		seen := make(map[string]bool)
		for _, part := range strings.Split(rawTFs, ",") {
			tf := normalizeTF(part)
			if !seen[tf] {
				seen[tf] = true
				tfs = append(tfs, tf)
			}
		}
	}
	if len(tfs) == 0 {
		tfs = []string{"4h", "15m", "5m"}
	}

	// Determine earliest 1m boundary needed to synthesize in-progress bars (or fallback closed bars).
	var maxTFSec int64 = 60
	for _, tf := range tfs {
		if sec := tfDurationSec(tf); sec > maxTFSec {
			maxTFSec = sec
		}
	}
	var all1m []types.Candle
	if s.cache != nil {
		// Load enough 1m bars up to cursorTS to cover forming higher-TF candles (and 1m fallback).
		minFrom := (cursorTS/maxTFSec)*maxTFSec - maxTFSec*2
		if minFrom < 0 {
			minFrom = 0
		}
		all1m, _ = s.cache.GetKlinesUpTo(ctx, sym, "1m", cursorTS, maxInt(limit*2, 1500))
		if len(all1m) == 0 && minFrom > 0 {
			all1m, _ = s.cache.GetKlinesRange(ctx, sym, "1m", minFrom, cursorTS)
		}
	}

	barsByTF := make(map[string][]Bar, len(tfs))
	for _, tf := range tfs {
		candles := s.replayCandlesForTF(ctx, sym, tf, cursorTS, limit, all1m)
		barsByTF[tf] = candlesToBars(candles)
	}

	drawings := []cache.ChartDrawing{}
	fills := []cache.TradeFill{}
	syncedTrades := []cache.SyncedTrade{}
	if s.cache != nil {
		if d, err := s.cache.ListDrawings(ctx, sym, cursorTS); err == nil && d != nil {
			drawings = d
		}
		if fl, err := s.cache.ListTradeFills(ctx, sym, cursorTS*1000); err == nil && fl != nil {
			fills = fl
		}
		if st, err := s.cache.ListSyncedTrades(ctx, sym); err == nil {
			syncedTrades = st
		}
	}

	anomalies := []types.AnomalyEvent{}
	if s.pipeline != nil {
		for _, a := range s.pipeline.RecentAnomalies(500) {
			if int64(a.Timestamp) > cursorTS {
				continue
			}
			if a.Symbol != "" && a.Symbol != "MARKET" && a.Symbol != sym {
				continue
			}
			anomalies = append(anomalies, a)
		}
	}

	notices := []types.Notice{}
	if nj := s.noticeJournal(); nj != nil {
		if list, err := nj.ListNotices(); err == nil {
			for _, n := range list {
				if n.Symbol != sym || n.CreatedAt.Unix() > cursorTS {
					continue
				}
				if n.DeliveryStatus == types.NoticeStatusSuppressed {
					continue
				}
				notices = append(notices, n)
			}
		}
	}

	// Strictly filter tickets & decisions/outcomes/fills <= cursorTS.
	allBundles, _ := s.buildTicketBundlesAndGateHealth(time.Unix(cursorTS, 0), decisionConfigFromServer(s), syncedTrades, fills)
	tickets := make([]TicketBundle, 0)
	for _, b := range allBundles {
		if b.Ticket.Symbol != sym || b.Ticket.CreatedAt > cursorTS {
			continue
		}
		if b.Ticket.ClosedAt > cursorTS {
			b.Ticket.ClosedAt = 0
			b.Ticket.ExitPrice = 0
			b.Ticket.ExitReason = ""
			b.Ticket.Status = types.TicketStatusAccepted
		}
		if b.Decision != nil && b.Decision.DecidedAt > cursorTS {
			b.Decision = nil
			b.Ticket.Status = types.TicketStatusOpen
		}
		if b.Outcome != nil && b.Outcome.AsOfUnix > cursorTS {
			b.Outcome = nil
		}
		if b.LinkedTrade != nil && b.LinkedTrade.EntryTimeMs > cursorTS*1000 {
			b.LinkedTrade = nil
		}
		var filteredFills []cache.TradeFill
		for _, f := range b.Fills {
			if f.TimeMs <= cursorTS*1000 {
				filteredFills = append(filteredFills, f)
			}
		}
		b.Fills = filteredFills
		tickets = append(tickets, b)
	}

	var mktCycle, symCycle *types.CycleMap
	if cm, ok := s.computeCycleMap(ctx, "BTC/USDT:USDT", cursorTS, true); ok {
		mktCycle = &cm
	}
	if sym == "BTC/USDT:USDT" && mktCycle != nil {
		cp := *mktCycle
		symCycle = &cp
	} else if cm, ok := s.computeCycleMap(ctx, sym, cursorTS, true); ok {
		symCycle = &cm
	}

	writeJSON(w, http.StatusOK, ReplayStateResponse{
		Symbol:      sym,
		CursorTS:    cursorTS,
		BarsByTF:    barsByTF,
		Drawings:    drawings,
		Anomalies:   anomalies,
		Notices:     notices,
		Tickets:     tickets,
		Fills:       fills,
		MarketCycle: mktCycle,
		SymbolCycle: symCycle,
		Box:         nil,
	})
}

func (s *Server) replayCandlesForTF(ctx context.Context, sym, tf string, cursorTS int64, limit int, all1m []types.Candle) []types.Candle {
	if tf == "1m" {
		if len(all1m) == 0 && s.cache != nil {
			all1m, _ = s.cache.GetKlinesUpTo(ctx, sym, "1m", cursorTS, limit)
		}
		if len(all1m) > limit {
			return all1m[len(all1m)-limit:]
		}
		return all1m
	}

	tfSec := tfDurationSec(tf)
	floorTS := (cursorTS / tfSec) * tfSec
	maxClosedOpen := cursorTS - tfSec

	var closed []types.Candle
	if s.cache != nil && maxClosedOpen >= 0 {
		closed, _ = s.cache.GetKlinesUpTo(ctx, sym, tf, maxClosedOpen, limit)
	}
	// If higher-TF klines were not cached (e.g. only 1m bars seeded), synthesize closed bars from 1m <= cursorTS.
	if len(closed) == 0 && len(all1m) > 0 {
		closed = synthesizeBarsFrom1m(all1m, tfSec, maxClosedOpen, cursorTS)
	}

	// Synthesize strictly in-progress bar [floorTS, cursorTS] from 1m bars <= cursorTS.
	if forming, ok := synthesizeSingleBar(all1m, floorTS, cursorTS); ok {
		closed = append(closed, forming)
	}

	if len(closed) > limit {
		closed = closed[len(closed)-limit:]
	}
	return closed
}

func synthesizeBarsFrom1m(bars1m []types.Candle, tfSec, maxOpenTS, cursorTS int64) []types.Candle {
	if len(bars1m) == 0 || tfSec <= 0 {
		return nil
	}
	buckets := make(map[int64][]types.Candle)
	var keys []int64
	for _, b := range bars1m {
		if b.Timestamp > cursorTS {
			continue
		}
		fl := (b.Timestamp / tfSec) * tfSec
		if fl > maxOpenTS {
			continue
		}
		if _, ok := buckets[fl]; !ok {
			keys = append(keys, fl)
		}
		buckets[fl] = append(buckets[fl], b)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	out := make([]types.Candle, 0, len(keys))
	for _, k := range keys {
		if c, ok := aggregateCandles(k, buckets[k]); ok {
			out = append(out, c)
		}
	}
	return out
}

func synthesizeSingleBar(bars1m []types.Candle, floorTS, cursorTS int64) (types.Candle, bool) {
	var slice []types.Candle
	for _, b := range bars1m {
		if b.Timestamp >= floorTS && b.Timestamp <= cursorTS {
			slice = append(slice, b)
		}
	}
	return aggregateCandles(floorTS, slice)
}

func aggregateCandles(openTS int64, slice []types.Candle) (types.Candle, bool) {
	if len(slice) == 0 {
		return types.Candle{}, false
	}
	agg := types.Candle{
		Timestamp: openTS,
		Open:      slice[0].Open,
		High:      slice[0].High,
		Low:       slice[0].Low,
		Close:     slice[len(slice)-1].Close,
	}
	for _, b := range slice {
		if b.High > agg.High {
			agg.High = b.High
		}
		if b.Low < agg.Low {
			agg.Low = b.Low
		}
		agg.Volume += b.Volume
		agg.QuoteVol += b.QuoteVol
	}
	return agg, true
}

func (s *Server) isBarsCacheFresh(sym, tf string, maxAge time.Duration) bool {
	key := sym + "|" + tf
	s.barsFetchMu.Lock()
	at, ok := s.barsFetchAt[key]
	s.barsFetchMu.Unlock()
	return ok && time.Since(at) < maxAge
}

func (s *Server) markBarsCacheFetched(sym, tf string) {
	key := sym + "|" + tf
	s.barsFetchMu.Lock()
	s.barsFetchAt[key] = time.Now()
	s.barsFetchMu.Unlock()
}

func (s *Server) warmBarsTailAsync(sym, tf string, fetchLim int) {
	if s.cache == nil || s.bgCtx.Err() != nil {
		return
	}
	ex := s.ohlcvFetcher()
	if ex == nil {
		return
	}
	key := sym + "|" + tf
	s.barsFetchMu.Lock()
	if s.barsWarming[key] || s.bgCtx.Err() != nil {
		s.barsFetchMu.Unlock()
		return
	}
	s.barsWarming[key] = true
	s.bgWG.Add(1)
	s.barsFetchMu.Unlock()

	go func() {
		defer s.bgWG.Done()
		bgCtx, cancel := context.WithTimeout(s.bgCtx, 2500*time.Millisecond)
		defer cancel()
		fetched, err := ex.FetchOHLCV(bgCtx, sym, tf, fetchLim, 0)
		if err == nil && len(fetched) > 0 && s.bgCtx.Err() == nil {
			_ = s.cache.UpsertKlines(bgCtx, sym, tf, ex.Name(), fetched)
			s.markBarsCacheFetched(sym, tf)
		}
		s.barsFetchMu.Lock()
		delete(s.barsWarming, key)
		s.barsFetchMu.Unlock()
	}()
}

func (s *Server) loadBarsWithFallback(ctx context.Context, sym, tf string, limit int, beforeTS int64) []types.Candle {
	var cached []types.Candle
	if s.cache != nil {
		cached, _ = s.cache.GetKlines(ctx, sym, tf, limit, beforeTS)
	}

	fetchLim := 0
	timeout := 8 * time.Second
	if len(cached) < limit {
		fetchLim = limit
	} else if beforeTS > 0 && len(cached) > 0 {
		if tfSec := tfDurationSec(tf); tfSec > 0 && beforeTS-cached[len(cached)-1].Timestamp > tfSec*2 {
			fetchLim = limit
		}
	} else if beforeTS == 0 && len(cached) >= limit {
		if s.isBarsCacheFresh(sym, tf, 10*time.Second) {
			return cached
		}
		tfSec := maxInt64(60, tfDurationSec(tf))
		barAge := time.Now().Unix() - cached[len(cached)-1].Timestamp
		if barAge < tfSec {
			s.warmBarsTailAsync(sym, tf, 2)
			return cached
		}
		fetchLim = minInt(limit, maxInt(2, int(barAge/tfSec)+2))
		timeout = 2500 * time.Millisecond
	}

	if fetchLim > 0 {
		if ex := s.ohlcvFetcher(); ex != nil {
			reqCtx, cancel := context.WithTimeout(ctx, timeout)
			var beforeMs int64
			if beforeTS > 0 {
				beforeMs = beforeTS * 1000
			}
			fetched, err := ex.FetchOHLCV(reqCtx, sym, tf, fetchLim, beforeMs)
			cancel()
			if err == nil && len(fetched) > 0 {
				if beforeTS == 0 {
					s.markBarsCacheFetched(sym, tf)
				}
				if s.cache != nil {
					_ = s.cache.UpsertKlines(ctx, sym, tf, ex.Name(), fetched)
					if reloaded, err := s.cache.GetKlines(ctx, sym, tf, limit, beforeTS); err == nil && len(reloaded) > 0 {
						return reloaded
					}
				}
				return fetched
			}
		}
	}
	return cached
}

// loadClosedCandles returns strictly closed bars with close_time <= asOfUnix.
func (s *Server) loadClosedCandles(ctx context.Context, sym, tf string, limit int, asOfUnix int64, strictCacheOnly bool) ([]types.Candle, error) {
	tfSec := tfDurationSec(tf)
	maxOpen := asOfUnix - tfSec
	if maxOpen < 0 {
		return nil, nil
	}
	if s.cache != nil {
		bars, err := s.cache.GetKlinesUpTo(ctx, sym, tf, maxOpen, limit)
		if err == nil && (len(bars) >= 40 || strictCacheOnly) {
			return bars, nil
		}
	}
	if strictCacheOnly {
		return nil, nil
	}
	bars := s.loadBarsWithFallback(ctx, sym, tf, limit+2, 0)
	var closed []types.Candle
	for _, b := range bars {
		if b.Timestamp+tfSec <= asOfUnix {
			closed = append(closed, b)
		}
	}
	if len(closed) > limit {
		closed = closed[len(closed)-limit:]
	}
	return closed, nil
}

func candlesToBars(candles []types.Candle) []Bar {
	out := make([]Bar, 0, len(candles))
	for _, c := range candles {
		out = append(out, Bar{
			Time:     c.Timestamp,
			Open:     c.Open,
			High:     c.High,
			Low:      c.Low,
			Close:    c.Close,
			Volume:   c.Volume,
			QuoteVol: c.QuoteVol,
		})
	}
	return out
}

func normalizeTF(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1m":
		return "1m"
	case "5m":
		return "5m"
	case "15m":
		return "15m"
	case "1h":
		return "1h"
	case "4h":
		return "4h"
	case "1d":
		return "1d"
	default:
		return "5m"
	}
}

func tfDurationSec(tf string) int64 {
	switch tf {
	case "1m":
		return 60
	case "5m":
		return 300
	case "15m":
		return 900
	case "1h":
		return 3600
	case "4h":
		return 14400
	case "1d":
		return 86400
	default:
		return 300
	}
}

func parseLimit(raw string, def, maxVal int) int {
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return def
	}
	if v > maxVal {
		return maxVal
	}
	return v
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
