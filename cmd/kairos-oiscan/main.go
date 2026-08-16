// Command kairos-oiscan scans the whole futures market for open-interest
// anomalies and writes kairos-bus compatible events as JSONL.
//
// Two-stage pipeline (research: docs/research/2026-08-09-单机币合约启动确认.md):
//
//	Stage 1 discover: CoinGlass /api/futures/v2/coins/markets OI-change
//	                  leaderboard (one call per page, 200/page) → candidates
//	                  filtered by OI dollar value and h1 OI change.
//	Stage 2 confirm:  for each candidate pull Binance official data
//	                  (openInterestHist 5m, klines 1h, topLongShortPositionRatio)
//	                  → quadrant classification → launch_confirmed.
//
// Output events (kairos-bus Event schema):
//
//	oi_surge        (LOW)   candidate passed stage-1 filters
//	launch_confirmed (HIGH) OI up + price up + top-trader long ratio above
//	                        threshold (single-coin launch confirmation)
//	launch_fading    (HIGH) OI falling off after a confirmed launch
//
// Events are appended to <out>/YYYY-MM-DD.jsonl; kairos-bus gates, dedups,
// cools down and pushes them. First version is shadow-friendly: run --once
// and inspect the JSONL before wiring the bus.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ArchdevilForge/kairos/internal/data"
)

// ---------------------------------------------------------------------------
// CoinGlass market row (subset of /api/futures/v2/coins/markets fields)
// ---------------------------------------------------------------------------

type marketRow struct {
	Symbol             string  `json:"symbol"`
	H1OIChangePercent  float64 `json:"h1OIChangePercent"`
	H4OIChangePercent  float64 `json:"h4OIChangePercent"`
	OIChangePercent    float64 `json:"oichangePercent"` // ~24h window
	OpenInterest       float64 `json:"openInterest"`    // USD (All-exchange aggregate)
	OpenInterestAmount float64 `json:"openInterestAmount"`
	VolUsd             float64 `json:"volUsd"`
	AvgFundingRate     float64 `json:"avgFundingRate"`
	Price              float64 `json:"price"`
	PriceChangePercent float64 `json:"priceChangePercent"` // 24h
}

func (r marketRow) valid() bool {
	return r.Symbol != "" && r.OpenInterest > 0
}

// ---------------------------------------------------------------------------
// Binance confirm data
// ---------------------------------------------------------------------------

type binanceConfirm struct {
	OI5mTrendPct    float64 `json:"oi_5m_trend_pct"`     // latest 30m vs prior 30m
	PriceH1Pct      float64 `json:"price_h1_pct"`        // 1h price change
	TopLongPct      float64 `json:"top_trader_long_pct"` // top trader position long %
	FundingRate     float64 `json:"funding_rate"`
	OIUsd           float64 `json:"oi_usd"` // Binance official OI in USD
	Quadrant        string  `json:"quadrant"`
	QuadrantExplain string  `json:"quadrant_explain"`
}

const (
	fapiBase      = "https://fapi.binance.com"
	marketsPath   = "/api/futures/v2/coins/markets"
	pageSize      = 200
	maxPages      = 8 // 200/page → covers ~1600 symbols
	defaultOutDir = "data/inbound/futures"
)

// ---------------------------------------------------------------------------
// config
// ---------------------------------------------------------------------------

type config struct {
	once        bool
	interval    time.Duration
	outDir      string
	minOIUsd    float64
	minH1OI     float64 // absolute h1 OI change %, stage-1 filter
	maxCand     int
	minTopLong  float64 // top-trader long % for launch_confirmed
	blacklist   map[string]bool
	httpTimeout time.Duration
}

func parseFlags() config {
	var (
		once       = flag.Bool("once", false, "run a single scan and exit")
		interval   = flag.Duration("interval", 10*time.Minute, "scan interval in watch mode")
		outDir     = flag.String("out", defaultOutDir, "output dir for JSONL events (relative to kairos root)")
		minOIUsd   = flag.Float64("min-oi-usd", 5e6, "minimum OI dollar value (CoinGlass All-exchange aggregate) to consider")
		minH1OI    = flag.Float64("min-h1oi", 10.0, "minimum absolute 1h OI change percent for stage-1 candidates")
		maxCand    = flag.Int("max-candidates", 20, "max candidates confirmed per scan")
		minTopLong = flag.Float64("min-top-long", 55.0, "top-trader long %% required for launch_confirmed")
		bl         = flag.String("blacklist", "LUNA/USDT:USDT", "comma-separated symbols to skip")
		timeout    = flag.Duration("http-timeout", 20*time.Second, "per-request timeout")
	)
	flag.Parse()
	cfg := config{
		once: *once, interval: *interval, outDir: *outDir,
		minOIUsd: *minOIUsd, minH1OI: *minH1OI, maxCand: *maxCand,
		minTopLong: *minTopLong, httpTimeout: *timeout,
		blacklist: make(map[string]bool),
	}
	for _, s := range strings.Split(*bl, ",") {
		if s = strings.TrimSpace(s); s != "" {
			cfg.blacklist[strings.ToUpper(s)] = true
		}
	}
	return cfg
}

// ---------------------------------------------------------------------------
// CoinGlass stage 1: full-market OI-change leaderboard
// ---------------------------------------------------------------------------

func fetchMarkets(ctx context.Context, cfg config) ([]marketRow, error) {
	var rows []marketRow
	for page := 1; page <= maxPages; page++ {
		params := map[string]string{
			"pageSize": strconv.Itoa(pageSize),
			"pageNum":  strconv.Itoa(page),
			"sort":     "oiChange",
			"order":    "desc",
		}
		raw, err := data.FetchCoinGlassEndpoint(ctx, marketsPath, params, cfg.httpTimeout)
		if err != nil {
			return nil, fmt.Errorf("coinglass markets page %d: %w", page, err)
		}
		list, ok := raw.([]any)
		if !ok {
			return nil, fmt.Errorf("coinglass markets page %d: unexpected type %T", page, raw)
		}
		if len(list) == 0 {
			break // no more pages
		}
		for _, item := range list {
			b, err := json.Marshal(item)
			if err != nil {
				continue
			}
			var r marketRow
			if err := json.Unmarshal(b, &r); err != nil || !r.valid() {
				continue
			}
			rows = append(rows, r)
		}
		if len(list) < pageSize {
			break
		}
	}
	return rows, nil
}

// ---------------------------------------------------------------------------
// Binance stage 2: per-candidate official confirmation
// ---------------------------------------------------------------------------

type fapiClient struct{ hc *http.Client }

func (c *fapiClient) get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	u := fapiBase + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return nil, fmt.Errorf("%s %s: HTTP %d %s", path, params.Encode(), resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return io.ReadAll(resp.Body)
}

// confirmSymbol pulls Binance official data and classifies the quadrant.
func (c *fapiClient) confirmSymbol(ctx context.Context, cfg config, symbol string) (binanceConfirm, error) {
	var bc binanceConfirm

	// 1h price change from klines (two 1h candles).
	kl, err := c.get(ctx, "/fapi/v1/klines", url.Values{
		"symbol": {symbol}, "interval": {"1h"}, "limit": {"2"},
	})
	if err != nil {
		return bc, err
	}
	var klines [][]any
	if err := json.Unmarshal(kl, &klines); err != nil || len(klines) < 2 {
		return bc, fmt.Errorf("klines parse: %w", err)
	}
	open0, _ := strconv.ParseFloat(fmt.Sprint(klines[0][1]), 64)
	close1, _ := strconv.ParseFloat(fmt.Sprint(klines[1][4]), 64)
	if open0 > 0 {
		bc.PriceH1Pct = (close1/open0 - 1) * 100
	}

	// OI 5m history: latest 6 bars vs previous 6 bars.
	oiRaw, err := c.get(ctx, "/futures/data/openInterestHist", url.Values{
		"symbol": {symbol}, "period": {"5m"}, "limit": {"12"},
	})
	if err != nil {
		return bc, err
	}
	var oiHist []struct {
		SumOpenInterest      string `json:"sumOpenInterest"`
		SumOpenInterestValue string `json:"sumOpenInterestValue"`
	}
	if err := json.Unmarshal(oiRaw, &oiHist); err != nil || len(oiHist) < 12 {
		return bc, fmt.Errorf("oi history parse: %w", err)
	}
	var recent, prior float64
	for i, h := range oiHist {
		v, _ := strconv.ParseFloat(h.SumOpenInterest, 64)
		if i >= len(oiHist)-6 {
			recent += v
		} else {
			prior += v
		}
	}
	if prior > 0 {
		bc.OI5mTrendPct = (recent/prior - 1) * 100
		if v, _ := strconv.ParseFloat(oiHist[len(oiHist)-1].SumOpenInterestValue, 64); v > 0 {
			bc.OIUsd = v
		}
	}

	// Top trader long/short position ratio.
	tt, err := c.get(ctx, "/futures/data/topLongShortPositionRatio", url.Values{
		"symbol": {symbol}, "period": {"15m"}, "limit": {"1"},
	})
	if err == nil {
		var rows []struct {
			LongAccount string `json:"longAccount"`
		}
		if json.Unmarshal(tt, &rows) == nil && len(rows) > 0 {
			bc.TopLongPct, _ = strconv.ParseFloat(rows[0].LongAccount, 64)
			bc.TopLongPct *= 100
		}
	} // ratio endpoint is optional — missing data keeps quadrant partial

	// Funding rate.
	fr, err := c.get(ctx, "/fapi/v1/premiumIndex", url.Values{"symbol": {symbol}})
	if err == nil {
		var p struct {
			LastFundingRate string `json:"lastFundingRate"`
		}
		if json.Unmarshal(fr, &p) == nil {
			bc.FundingRate, _ = strconv.ParseFloat(p.LastFundingRate, 64)
			bc.FundingRate *= 100
		}
	}

	bc.Quadrant, bc.QuadrantExplain = classifyQuadrant(bc.OI5mTrendPct, bc.PriceH1Pct)
	return bc, nil
}

// classifyQuadrant maps (OI trend, price trend) to the four OI×price states.
func classifyQuadrant(oiPct, pricePct float64) (string, string) {
	oiUp := oiPct > 1.0
	priceUp := pricePct > 0.5
	switch {
	case oiUp && priceUp:
		return "new_long", "OI↑+价↑: 新多头进场(启动候选)"
	case oiUp && !priceUp:
		return "new_short", "OI↑+价↓: 空头进场/对倒(危险)"
	case !oiUp && priceUp:
		return "short_cover", "OI↓+价↑: 空头回补(逼空尾巴)"
	default:
		return "long_exit", "OI↓+价↓: 多头平仓(fading)"
	}
}

// ---------------------------------------------------------------------------
// kairos-bus Event writer
// ---------------------------------------------------------------------------

type event struct {
	Ts            string `json:"ts"`
	Floor         string `json:"floor"`
	EventType     string `json:"event_type"`
	Severity      string `json:"severity"`
	Key           string `json:"key"`
	Symbol        string `json:"symbol"`
	Title         string `json:"title"`
	Message       string `json:"message"`
	Data          any    `json:"data"`
	SchemaVersion string `json:"schema_version,omitempty"`
	StrategyID    string `json:"strategy_id,omitempty"`
	ExperimentID  string `json:"experiment_id,omitempty"`
	Mode          string `json:"mode,omitempty"`
	Venue         string `json:"venue,omitempty"`
	Direction     string `json:"direction,omitempty"`
	ParentEventID string `json:"parent_event_id,omitempty"`
}

func directionOf(pricePct float64) string {
	switch {
	case pricePct > 0.5:
		return "up"
	case pricePct < -0.5:
		return "down"
	default:
		return "neutral"
	}
}

func writeEvents(outDir string, evs []event) (int, error) {
	if len(evs) == 0 {
		return 0, nil
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return 0, err
	}
	name := filepath.Join(outDir, time.Now().UTC().Format("2006-01-02")+".jsonl")
	f, err := os.OpenFile(name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	n := 0
	for _, ev := range evs {
		b, err := json.Marshal(ev)
		if err != nil {
			continue
		}
		if _, err := f.Write(append(b, '\n')); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// ---------------------------------------------------------------------------
// scan pipeline
// ---------------------------------------------------------------------------

func scanOnce(ctx context.Context, cfg config) error {
	log := slog.Default()
	rows, err := fetchMarkets(ctx, cfg)
	if err != nil {
		return err
	}
	log.Info("full-market OI leaderboard", "symbols", len(rows))

	// Stage 1: filter candidates.
	var cands []marketRow
	for _, r := range rows {
		sym := strings.ToUpper(r.Symbol)
		if cfg.blacklist[sym] {
			continue
		}
		if r.OpenInterest < cfg.minOIUsd {
			continue
		}
		if abs(r.H1OIChangePercent) < cfg.minH1OI {
			continue
		}
		cands = append(cands, r)
	}
	// Rank by |h1 OI change| descending.
	sortRowsByH1OI(cands)
	if len(cands) > cfg.maxCand {
		cands = cands[:cfg.maxCand]
	}
	log.Info("stage-1 candidates", "count", len(cands))

	// Stage 2: confirm each candidate on Binance official data.
	cli := &fapiClient{hc: &http.Client{Timeout: cfg.httpTimeout}}
	evs := make([]event, 0, len(cands)*2)
	roundKey := time.Now().UTC().Format("20060102T1504")
	for _, r := range cands {
		sym := strings.ToUpper(r.Symbol) + "USDT"
		bc, err := cli.confirmSymbol(ctx, cfg, sym)
		if err != nil {
			log.Warn("confirm failed", "symbol", sym, "error", err)
			continue
		}
		oiSurge := event{
			Ts: time.Now().UTC().Format("2006-01-02T15:04:05Z"), Floor: "futures",
			EventType: "oi_surge", Severity: "LOW",
			SchemaVersion: "kairos.event.v1",
			Key:           sym + "-" + roundKey,
			Symbol:        sym,
			StrategyID:    "oi_launch_v1",
			ExperimentID:  "exp-" + roundKey[:8],
			Mode:          "shadow",
			Venue:         "binance",
			Direction:     directionOf(bc.PriceH1Pct),
			Title:         fmt.Sprintf("全市场 OI 异动: %s h1OI %+.1f%%", r.Symbol, r.H1OIChangePercent),
			Message: fmt.Sprintf("h1OI %+.1f%% h4OI %+.1f%% OI24h %+.1f%% | OI $%.0fM | quadrant=%s",
				r.H1OIChangePercent, r.H4OIChangePercent, r.OIChangePercent,
				r.OpenInterest/1e6, bc.Quadrant),
			Data: map[string]any{"market": r, "confirm": bc},
		}
		evs = append(evs, oiSurge)

		// Launch confirmation: OI up + price up + top traders long.
		if bc.Quadrant == "new_long" && bc.TopLongPct >= cfg.minTopLong {
			evs = append(evs, event{
				Ts: time.Now().UTC().Format("2006-01-02T15:04:05Z"), Floor: "futures",
				EventType: "launch_confirmed", Severity: "HIGH",
				SchemaVersion: "kairos.event.v1",
				StrategyID:    "oi_launch_v1",
				ExperimentID:  "exp-" + roundKey[:8],
				Mode:          "shadow",
				Venue:         "binance",
				Direction:     directionOf(bc.PriceH1Pct),
				ParentEventID: sym + "-" + roundKey + "-oi_surge",
				Key:           sym + "-" + roundKey,
				Symbol:        sym,
				Title:         fmt.Sprintf("合约启动确认: %s (大户多头 %.0f%%)", r.Symbol, bc.TopLongPct),
				Message: fmt.Sprintf("%s | OI5m %+.1f%% 价1h %+.1f%% 大户多 %.1f%% funding %.4f%%",
					bc.QuadrantExplain, bc.OI5mTrendPct, bc.PriceH1Pct, bc.TopLongPct, bc.FundingRate),
				Data: map[string]any{"market": r, "confirm": bc},
			})
		}
		// Fading: OI falling after confirmed volume — signal exits, not entries.
		if bc.Quadrant == "long_exit" && abs(r.H1OIChangePercent) >= cfg.minH1OI {
			evs = append(evs, event{
				Ts: time.Now().UTC().Format("2006-01-02T15:04:05Z"), Floor: "futures",
				EventType: "launch_fading", Severity: "HIGH",
				SchemaVersion: "kairos.event.v1",
				StrategyID:    "oi_launch_v1",
				ExperimentID:  "exp-" + roundKey[:8],
				Mode:          "shadow",
				Venue:         "binance",
				Direction:     directionOf(bc.PriceH1Pct),
				ParentEventID: sym + "-" + roundKey + "-oi_surge",
				Key:           sym + "-" + roundKey,
				Symbol:        sym,
				Title:         fmt.Sprintf("启动衰减: %s OI 回落", r.Symbol),
				Message: fmt.Sprintf("%s | OI5m %+.1f%% 价1h %+.1f%%",
					bc.QuadrantExplain, bc.OI5mTrendPct, bc.PriceH1Pct),
				Data: map[string]any{"market": r, "confirm": bc},
			})
		}
	}

	n, err := writeEvents(cfg.outDir, evs)
	if err != nil {
		return err
	}
	log.Info("scan done", "events", n, "out", cfg.outDir)
	for _, ev := range evs {
		if ev.EventType != "oi_surge" {
			log.Info("event", "type", ev.EventType, "symbol", ev.Symbol, "title", ev.Title)
		}
	}
	return nil
}

func sortRowsByH1OI(rows []marketRow) {
	// insertion sort is fine for ≤ a few hundred candidates; keeps deps zero.
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && abs(rows[j-1].H1OIChangePercent) < abs(rows[j].H1OIChangePercent); j-- {
			rows[j-1], rows[j] = rows[j], rows[j-1]
		}
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func main() {
	cfg := parseFlags()
	ctx := context.Background()
	if cfg.once {
		if err := scanOnce(ctx, cfg); err != nil {
			slog.Error("scan failed", "error", err)
			os.Exit(1)
		}
		return
	}
	slog.Info("kairos-oiscan watch mode", "interval", cfg.interval)
	for {
		start := time.Now()
		if err := scanOnce(ctx, cfg); err != nil {
			slog.Error("scan failed; retrying next interval", "error", err)
		}
		time.Sleep(time.Until(start.Add(cfg.interval)))
	}
}
