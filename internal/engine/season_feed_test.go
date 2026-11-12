package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ArchdevilForge/kairos/internal/config"
	"github.com/ArchdevilForge/kairos/internal/exchange"
	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
)

type funcOHLCVFetcher func(ctx context.Context, symbol, timeframe string, limit int, beforeMs int64) ([]types.Candle, error)

func (fn funcOHLCVFetcher) FetchOHLCV(ctx context.Context, symbol, timeframe string, limit int, beforeMs int64) ([]types.Candle, error) {
	return fn(ctx, symbol, timeframe, limit, beforeMs)
}

func TestBuildSeasonUniverse_AlwaysUnionsBTCAndDedups(t *testing.T) {
	u1 := buildSeasonUniverse([]string{"SOL/USDT:USDT", "ETH/USDT:USDT", "SOL/USDT:USDT", "  "})
	want1 := []string{"BTC/USDT:USDT", "ETH/USDT:USDT", "SOL/USDT:USDT"}
	if strings.Join(u1, ",") != strings.Join(want1, ",") {
		t.Fatalf("got %v, want %v", u1, want1)
	}

	u2 := buildSeasonUniverse(nil)
	if len(u2) != 1 || u2[0] != "BTC/USDT:USDT" {
		t.Fatalf("empty tracked must still retain BTC, got %v", u2)
	}
}

func TestSeasonFeed_WindowRules(t *testing.T) {
	baseOpen := int64(1_710_000_000) // aligned to 300s
	lastClosedIdx := 23
	lastClosedOpen := baseOpen + int64(lastClosedIdx*300)
	lastClosedCloseTS := lastClosedOpen + 300 // 1_710_007_200

	// Helper: nClosed closed bars ending at lastClosedOpen + 1 forming tail bar with sentinel values.
	makeRaw5m := func(nClosed int, latestOpen int64, descending bool) []types.Candle {
		firstOpen := latestOpen - int64((nClosed-1)*300)
		out := make([]types.Candle, 0, nClosed+1)
		for i := 0; i < nClosed; i++ {
			openTS := firstOpen + int64(i*300)
			out = append(out, types.Candle{
				Timestamp: openTS,
				Open:      100.0,
				High:      101.0,
				Low:       99.0,
				Close:     100.5,
				Volume:    500,
				QuoteVol:  50_250,
			})
		}
		// Append forming (unclosed) tail bar with extreme sentinel values that MUST be dropped.
		out = append(out, types.Candle{
			Timestamp: latestOpen + 300,
			Open:      9999.0,
			High:      9999.0,
			Low:       9999.0,
			Close:     9999.0,
			Volume:    999_999,
			QuoteVol:  99_999_999,
		})
		if descending {
			for l, r := 0, len(out)-1; l < r; l, r = l+1, r-1 {
				out[l], out[r] = out[r], out[l]
			}
		}
		return out
	}

	t.Run("drops_forming_bar_requires_24_closed_and_bounds_at_24", func(t *testing.T) {
		feed := newSeasonFeed(nil)
		evalTime := time.Unix(lastClosedCloseTS+120, 0) // 2m after latest close (<= 10m)
		feed.setNowFunc(func() time.Time { return evalTime })

		fetcher := funcOHLCVFetcher(func(_ context.Context, symbol, _ string, _ int, _ int64) ([]types.Candle, error) {
			switch symbol {
			case "BTC/USDT:USDT":
				return makeRaw5m(24, lastClosedOpen, false), nil // 24 closed + 1 forming -> valid
			case "ETH/USDT:USDT":
				return makeRaw5m(35, lastClosedOpen, true), nil // 35 closed descending + 1 forming -> bounded to 24 ascending
			case "SOL/USDT:USDT":
				return makeRaw5m(24, lastClosedOpen, false), nil // valid
			case "XRP/USDT:USDT":
				return makeRaw5m(24, lastClosedOpen, false), nil // valid
			case "SHORT/USDT:USDT":
				return makeRaw5m(23, lastClosedOpen, false), nil // only 23 closed after dropping forming -> excluded
			case "ERR/USDT:USDT":
				return nil, errors.New("network timeout") // fail-silent -> excluded
			default:
				return nil, nil
			}
		})

		tracked := []string{"ETH/USDT:USDT", "SOL/USDT:USDT", "XRP/USDT:USDT", "SHORT/USDT:USDT", "ERR/USDT:USDT"}
		feed.poll(context.Background(), fetcher, tracked)

		snap := feed.snapshot(buildSeasonUniverse(tracked))
		if len(snap) != 4 {
			t.Fatalf("expected 4 valid symbols (BTC, ETH, SOL, XRP), got %d: %v", len(snap), snap)
		}
		if _, exists := snap["SHORT/USDT:USDT"]; exists {
			t.Fatal("symbol with only 23 closed bars must be excluded")
		}
		if _, exists := snap["ERR/USDT:USDT"]; exists {
			t.Fatal("failed symbol must be excluded")
		}

		for _, sym := range []string{"BTC/USDT:USDT", "ETH/USDT:USDT", "SOL/USDT:USDT", "XRP/USDT:USDT"} {
			bars := snap[sym]
			if len(bars) != 24 {
				t.Fatalf("%s expected exactly 24 bars, got %d", sym, len(bars))
			}
			if bars[len(bars)-1].CloseTS != lastClosedCloseTS {
				t.Fatalf("%s latest CloseTS=%d, want %d (forming bar must be dropped)",
					sym, bars[len(bars)-1].CloseTS, lastClosedCloseTS)
			}
			for i, b := range bars {
				if b.QuoteVol >= 90_000_000 || b.Close >= 9000 {
					t.Fatalf("%s bar[%d] leaked sentinel forming candle: %+v", sym, i, b)
				}
				if i > 0 && b.CloseTS <= bars[i-1].CloseTS {
					t.Fatalf("%s bars not strictly ascending at %d: %d <= %d", sym, i, b.CloseTS, bars[i-1].CloseTS)
				}
			}
		}

		// Departed symbols must have their windows reclaimed on pruneUniverse.
		if got := feed.windowCount(); got != 5 { // BTC, ETH, SOL, XRP, SHORT (ERR had 0 bars)
			t.Fatalf("windowCount before prune = %d, want 5", got)
		}
		reduced := buildSeasonUniverse([]string{"ETH/USDT:USDT", "SOL/USDT:USDT", "XRP/USDT:USDT"})
		feed.pruneUniverse(reduced)
		if got := feed.windowCount(); got != 4 {
			t.Fatalf("windowCount after prune = %d, want 4", got)
		}
	})

	t.Run("staleness_10m_boundary_and_min_4_valid_symbols_floor", func(t *testing.T) {
		feed := newSeasonFeed(nil)
		// Stale symbol's latest closed bar is 1 bar (300s) behind the fresh ones.
		staleLatestOpen := lastClosedOpen - 300 // CloseTS = lastClosedCloseTS - 300

		fetcher := funcOHLCVFetcher(func(_ context.Context, symbol, _ string, _ int, _ int64) ([]types.Candle, error) {
			if symbol == "XRP/USDT:USDT" {
				return makeRaw5m(24, staleLatestOpen, false), nil
			}
			return makeRaw5m(24, lastClosedOpen, false), nil
		})

		tracked := []string{"ETH/USDT:USDT", "SOL/USDT:USDT", "XRP/USDT:USDT"} // + BTC = 4 total
		feed.poll(context.Background(), fetcher, tracked)

		// At stale CloseTS + 600s (exact 10m boundary): XRP is still valid -> 4 symbols -> non-nil map.
		atBoundary := time.Unix((staleLatestOpen+300)+600, 0)
		snapOK := feed.snapshotAt(atBoundary, buildSeasonUniverse(tracked))
		if len(snapOK) != 4 {
			t.Fatalf("at exact 600s boundary expected 4 valid symbols, got %d", len(snapOK))
		}

		// At stale CloseTS + 601s (> 10m): XRP is excluded as stale -> only 3 valid (< 4) -> nil map!
		pastBoundary := time.Unix((staleLatestOpen+300)+601, 0)
		snapNil := feed.snapshotAt(pastBoundary, buildSeasonUniverse(tracked))
		if snapNil != nil {
			t.Fatalf("when stale exclusion drops valid count to 3 (<4), snapshot must be nil, got %d syms", len(snapNil))
		}
	})
}

func TestSeasonFeed_ConcurrentPollSnapshotPrune_RaceFree(t *testing.T) {
	feed := newSeasonFeed(nil)
	baseOpen := int64(1_710_000_000)
	lastClosedOpen := baseOpen + int64(23*300)
	feed.setNowFunc(func() time.Time { return time.Unix(lastClosedOpen+360, 0) })

	fetcher := funcOHLCVFetcher(func(_ context.Context, _ string, _ string, _ int, _ int64) ([]types.Candle, error) {
		out := make([]types.Candle, 25)
		for i := range out {
			out[i] = types.Candle{
				Timestamp: baseOpen + int64(i*300),
				Open:      100, High: 101, Low: 99, Close: 100.2,
				Volume: 100, QuoteVol: 10_020,
			}
		}
		return out, nil
	})

	symsA := []string{"ETH/USDT:USDT", "SOL/USDT:USDT", "XRP/USDT:USDT", "DOGE/USDT:USDT"}
	symsB := []string{"ETH/USDT:USDT", "SOL/USDT:USDT", "ADA/USDT:USDT"}

	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()

	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; ctx.Err() == nil; i++ {
			if i%2 == 0 {
				feed.poll(ctx, fetcher, symsA)
			} else {
				feed.poll(ctx, fetcher, symsB)
			}
		}
	}()
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			_ = feed.snapshot(buildSeasonUniverse(symsA))
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; ctx.Err() == nil; i++ {
			if i%2 == 0 {
				feed.pruneUniverse(buildSeasonUniverse(symsA))
			} else {
				feed.pruneUniverse(buildSeasonUniverse(symsB))
			}
		}
	}()
	wg.Wait()
}

// seasonE2EExchange serves deterministic higher-TF uptrend candles alongside
// configurable cross-sectional 5m QuoteVol series for end-to-end Pipeline tests.
type seasonE2EExchange struct {
	mu          sync.RWMutex
	tickers     map[string]*types.Ticker
	candles5m   map[string][]types.Candle
	lastCloseTS int64
}

func (m *seasonE2EExchange) Name() string { return "mock" }

func (m *seasonE2EExchange) SubscribeTickers(ctx context.Context, _ []string, _ chan<- types.Ticker) error {
	<-ctx.Done()
	return ctx.Err()
}

func (m *seasonE2EExchange) FetchTickers(context.Context) (map[string]*types.Ticker, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]*types.Ticker, len(m.tickers))
	for k, v := range m.tickers {
		out[k] = v
	}
	return out, nil
}

func (m *seasonE2EExchange) FetchTicker(context.Context, string) (*types.Ticker, error) {
	return nil, nil
}

func (m *seasonE2EExchange) FetchOHLCV(_ context.Context, symbol, timeframe string, limit int, _ int64) ([]types.Candle, error) {
	if timeframe == "5m" {
		m.mu.RLock()
		c := m.candles5m[symbol]
		m.mu.RUnlock()
		if len(c) == 0 {
			return nil, errors.New("missing 5m series for " + symbol)
		}
		if limit > 0 && len(c) > limit {
			c = c[len(c)-limit:]
		}
		cp := make([]types.Candle, len(c))
		copy(cp, c)
		return cp, nil
	}

	// Higher timeframes (15m, 1h, 4h, 1d): clean macro UP trend so leader_pullback_v1
	// context gates pass identically across all cross-sectional 5m regimes.
	if limit < 60 {
		limit = 60
	}
	stepSec := int64(3600)
	switch timeframe {
	case "15m":
		stepSec = 900
	case "1h":
		stepSec = 3600
	case "4h":
		stepSec = 14400
	case "1d":
		stepSec = 86400
	}
	endOpen := m.lastCloseTS - stepSec
	startOpen := endOpen - int64(limit-1)*stepSec
	out := make([]types.Candle, limit)
	px := 100.0
	for i := 0; i < limit; i++ {
		px += 0.9
		if i%7 == 0 {
			px -= 0.2
		}
		if symbol != "BTC/USDT:USDT" {
			px += 0.05
		}
		out[i] = types.Candle{
			Timestamp: startOpen + int64(i)*stepSec,
			Open:      px * 0.999,
			High:      px * 1.002,
			Low:       px * 0.998,
			Close:     px,
			Volume:    50_000,
			QuoteVol:  px * 50_000,
		}
	}
	return out, nil
}

func (m *seasonE2EExchange) Close() error { return nil }

// buildCandidatePullback5m constructs 54 closed 5m bars ending at lastClosedOpen
// (with a post-pulse higher-low pullback + restart on the final 3 closed bars)
// plus 1 unclosed forming tail bar at lastClosedOpen+300.
func buildCandidatePullback5m(lastClosedOpen int64) []types.Candle {
	var pattern []types.Candle
	px := 100.0
	mk := func(c, h, l float64) types.Candle {
		if h < c {
			h = c
		}
		if l > c {
			l = c
		}
		return types.Candle{Open: c - 0.05, Close: c, High: h, Low: l, Volume: 25_000, QuoteVol: 100_000}
	}
	for len(pattern) < 28 {
		pattern = append(pattern, mk(100.0, 100.2, 99.8))
	}
	for i := 0; i < 10; i++ {
		px += 0.1
		pattern = append(pattern, mk(px, px+0.3, px-0.3))
	}
	trough := px
	for i := 0; i < 8; i++ {
		px += 1.2
		pattern = append(pattern, mk(px, px+0.4, px-0.2))
	}
	for i := 0; i < 5; i++ {
		px -= 0.6
		pattern = append(pattern, mk(px, px+0.3, px-0.3))
	}
	if px <= trough {
		px = trough + 1.0
		pattern[len(pattern)-1] = mk(px, px+0.2, trough+0.5)
	}
	for i := 0; i < 3; i++ {
		px += 0.8
		pattern = append(pattern, mk(px, px+0.3, px-0.2))
	}
	// pattern has 54 closed bars; assign open timestamps ending at lastClosedOpen.
	firstOpen := lastClosedOpen - int64((len(pattern)-1)*300)
	for i := range pattern {
		pattern[i].Timestamp = firstOpen + int64(i*300)
	}
	// Append 1 unclosed forming tail bar.
	pattern = append(pattern, types.Candle{
		Timestamp: lastClosedOpen + 300,
		Open:      px, High: px + 50, Low: px - 50, Close: px + 40,
		Volume: 999_999, QuoteVol: 99_999_999,
	})
	return pattern
}

// buildPeerCrossSection5m builds 60 closed 5m bars + 1 forming tail bar for peer altcoins:
//   - winter=true: negative 5m returns (-0.25/bar) and contracting QuoteVol over the 24-bar tail
//     (first 18 of 24 at 140k, final 6 at 30k). Across 6 universe symbols (BTC + SOL up at 100k,
//     4 peers ETH/XRP/DOGE/ADA down):
//     bFast = 2/6 = 0.3333 < 0.46 (SpringBreadthMin),
//     bSlow = 2/6 = 0.3333 < 0.52 (AutumnSlowBreadthMin),
//     vRatio = (2*100k + 4*30k) / (2*100k + 4*112.5k) = 320k/650k = 0.4923 < 0.85 (WinterVolMax)
//     -> independently derived classifier output = WINTER at every step.
//   - winter=false (summer): positive 5m returns (+0.30/bar > 0.05% noise floor) and
//     expanding QuoteVol over the 24-bar tail (first 18 of 24 at 80k, final 6 at 160k).
//     Across 6 universe symbols (all 6 up):
//     bFast = 6/6 = 1.00 >= 0.58 (SummerBreadthMin),
//     vRatio = (2*100k + 4*160k) / (2*100k + 4*100k) = 840k/600k = 1.40 >= 0.98 (SummerVolMin)
//     across >= 2 consecutive 5m closes -> independently derived classifier output = SUMMER.
func buildPeerCrossSection5m(lastClosedOpen int64, winter bool) []types.Candle {
	const nClosed = 60
	tailStart := nClosed - 24
	firstOpen := lastClosedOpen - int64((nClosed-1)*300)
	out := make([]types.Candle, 0, nClosed+1)
	px := 120.0
	for i := 0; i < nClosed; i++ {
		openPx := px
		var qVol float64
		if winter {
			px -= 0.25
			if i < nClosed-6 {
				qVol = 140_000
			} else {
				qVol = 30_000
			}
		} else {
			px += 0.30
			if i < tailStart || i < nClosed-6 {
				qVol = 80_000
			} else {
				qVol = 160_000
			}
		}
		hi := openPx
		lo := px
		if px > openPx {
			hi, lo = px, openPx
		}
		out = append(out, types.Candle{
			Timestamp: firstOpen + int64(i*300),
			Open:      openPx,
			High:      hi + 0.05,
			Low:       lo - 0.05,
			Close:     px,
			Volume:    1000,
			QuoteVol:  qVol,
		})
	}
	// Append 1 unclosed forming tail bar that must be dropped.
	out = append(out, types.Candle{
		Timestamp: lastClosedOpen + 300,
		Open:      px, High: px + 100, Low: px - 50, Close: px + 90,
		Volume: 888_888, QuoteVol: 88_888_888,
	})
	return out
}

func TestPipeline_EndToEnd_SeasonFeedToTicketAndJournal(t *testing.T) {
	lastClosedOpen := int64(1_710_030_000)
	lastClosedCloseTS := lastClosedOpen + 300
	// Pulse occurred right before the in-session pullback low (~8 bars before restart).
	pulseTS := lastClosedOpen - int64(8*300)

	type scenarioResult struct {
		ticket  types.DecisionTicket
		outcome storage.CounterfactualOutcome
		rawJSON string
	}

	runScenario := func(
		t *testing.T,
		eventID string,
		tracked []string,
		peerWinter bool,
		evalTime time.Time,
	) scenarioResult {
		t.Helper()
		dir := t.TempDir()
		cfg, err := config.LoadString("")
		if err != nil {
			t.Fatal(err)
		}
		cfg.Exchanges.Primary = "mock"
		cfg.DataManager.Exchanges = []string{"mock"}
		cfg.Storage.DatabasePath = filepath.Join(dir, "kairos.db")
		cfg.Opportunity.Enabled = true
		cfg.Opportunity.ShadowMode = true
		cfg.Opportunity.AssumeSpreadOK = true

		p := NewPipeline(cfg, nil)
		t.Cleanup(p.Close)

		candles5m := map[string][]types.Candle{
			"BTC/USDT:USDT":  buildCandidatePullback5m(lastClosedOpen),
			"SOL/USDT:USDT":  buildCandidatePullback5m(lastClosedOpen),
			"ETH/USDT:USDT":  buildPeerCrossSection5m(lastClosedOpen, peerWinter),
			"XRP/USDT:USDT":  buildPeerCrossSection5m(lastClosedOpen, peerWinter),
			"DOGE/USDT:USDT": buildPeerCrossSection5m(lastClosedOpen, peerWinter),
			"ADA/USDT:USDT":  buildPeerCrossSection5m(lastClosedOpen, peerWinter),
		}
		mock := &seasonE2EExchange{
			candles5m:   candles5m,
			lastCloseTS: lastClosedCloseTS,
		}
		p.exchanges["mock"] = mock
		p.setSymbols("mock", tracked)
		p.seasonFeed.setNowFunc(func() time.Time { return evalTime })

		// 1. Refresh cross-sectional feed from live exchange + tracked universe.
		p.pollSeasonFeed(context.Background())

		// 2. Trigger live pipeline enrichment -> WatchAndEnrich -> BuildTicket -> Journal.
		evt := types.AnomalyEvent{
			EventType: "market_impulse",
			EventID:   eventID,
			Symbol:    "MARKET",
			Timestamp: float64(pulseTS),
			Data: map[string]any{
				"direction":             "up",
				"state_to":              "IMPULSE_UP",
				"median_return_60s_pct": 1.0,
				"leaders_detail": []any{
					map[string]any{"symbol": "SOL/USDT:USDT", "return_pct": 5.0, "relative_pct": 4.0},
				},
			},
		}
		p.enrichOpportunityAsync(evt)

		j, err := storage.NewJournal(cfg.Storage)
		if err != nil {
			t.Fatal(err)
		}
		var tickets []types.DecisionTicket
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			tickets, err = j.ListTickets("")
			if err == nil && len(tickets) > 0 {
				break
			}
			time.Sleep(15 * time.Millisecond)
		}
		if len(tickets) != 1 {
			t.Fatalf("expected 1 ticket for %s, got %d (err=%v)", eventID, len(tickets), err)
		}
		// Wait briefly for outcome seed in enrichOpportunityAsync to settle.
		var out storage.CounterfactualOutcome
		var ok bool
		for time.Now().Before(deadline) {
			out, ok, err = j.GetOutcome(tickets[0].ID)
			if err == nil && ok {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !ok {
			t.Fatalf("expected seeded outcome for ticket %s", tickets[0].ID)
		}
		rawBytes, err := os.ReadFile(j.Path())
		if err != nil {
			t.Fatal(err)
		}
		return scenarioResult{
			ticket:  tickets[0],
			outcome: out,
			rawJSON: string(rawBytes),
		}
	}

	freshEval := time.Unix(lastClosedCloseTS+90, 0) // 90s after close (well within 10m)
	fullTracked := []string{"SOL/USDT:USDT", "ETH/USDT:USDT", "XRP/USDT:USDT", "DOGE/USDT:USDT", "ADA/USDT:USDT"}

	// Case 1: Absent cross-section (<4 symbols: SOL + ETH + BTC = 3 < 4).
	absent := runScenario(t, "e2e-absent", []string{"SOL/USDT:USDT", "ETH/USDT:USDT"}, true, freshEval)
	if absent.ticket.SeasonRegime != "" || absent.ticket.WinterDefenseHint != "" {
		t.Fatalf("absent cross-section must leave fields empty, got SeasonRegime=%q WinterDefenseHint=%q",
			absent.ticket.SeasonRegime, absent.ticket.WinterDefenseHint)
	}
	if absent.outcome.SeasonRegime != "" || absent.outcome.WinterDefenseHint != "" {
		t.Fatalf("absent outcome must leave fields empty, got SeasonRegime=%q WinterDefenseHint=%q",
			absent.outcome.SeasonRegime, absent.outcome.WinterDefenseHint)
	}
	if strings.Contains(absent.rawJSON, `"season_regime"`) || strings.Contains(absent.rawJSON, `"winter_defense_hint"`) {
		t.Fatalf("absent journal JSON must omit shadow fields via omitempty, got:\n%s", absent.rawJSON)
	}

	// Case 2: Genuine WINTER cross-section (5 tracked + BTC = 6 symbols, 4/6 negative + contracting QuoteVol).
	winter := runScenario(t, "e2e-winter", fullTracked, true, freshEval)
	if winter.ticket.SeasonRegime != types.SeasonRegimeWinter {
		t.Fatalf("winter ticket SeasonRegime=%q, want %q", winter.ticket.SeasonRegime, types.SeasonRegimeWinter)
	}
	if winter.ticket.WinterDefenseHint != types.DefaultWinterDefenseHint {
		t.Fatalf("winter ticket WinterDefenseHint=%q, want %q", winter.ticket.WinterDefenseHint, types.DefaultWinterDefenseHint)
	}
	if winter.outcome.SeasonRegime != types.SeasonRegimeWinter || winter.outcome.WinterDefenseHint != types.DefaultWinterDefenseHint {
		t.Fatalf("winter outcome SeasonRegime=%q WinterDefenseHint=%q", winter.outcome.SeasonRegime, winter.outcome.WinterDefenseHint)
	}
	// Red Line E: zero change to RiskPlan sizing/stops/leverage!
	if winter.ticket.RiskPlan != absent.ticket.RiskPlan {
		t.Fatalf("shadow-only violation: RiskPlan changed between absent (%+v) and WINTER (%+v)",
			absent.ticket.RiskPlan, winter.ticket.RiskPlan)
	}

	// Case 3: Genuine SUMMER cross-section (6/6 positive returns + expanding QuoteVol).
	summer := runScenario(t, "e2e-summer", fullTracked, false, freshEval)
	if summer.ticket.SeasonRegime != types.SeasonRegimeSummer {
		t.Fatalf("summer ticket SeasonRegime=%q, want %q", summer.ticket.SeasonRegime, types.SeasonRegimeSummer)
	}
	if summer.ticket.WinterDefenseHint != "" {
		t.Fatalf("summer ticket must omit WinterDefenseHint, got %q", summer.ticket.WinterDefenseHint)
	}
	if strings.Contains(summer.rawJSON, `"winter_defense_hint"`) {
		t.Fatalf("summer journal JSON must omit winter_defense_hint, got:\n%s", summer.rawJSON)
	}
	if summer.ticket.RiskPlan != absent.ticket.RiskPlan {
		t.Fatalf("shadow-only violation: RiskPlan changed between absent (%+v) and SUMMER (%+v)",
			absent.ticket.RiskPlan, summer.ticket.RiskPlan)
	}

	// Case 4: Stale cross-section (>10m after latest closed 5m bar) -> fields omitted.
	staleEval := time.Unix(lastClosedCloseTS+601, 0)
	stale := runScenario(t, "e2e-stale", fullTracked, true, staleEval)
	if stale.ticket.SeasonRegime != "" || stale.ticket.WinterDefenseHint != "" {
		t.Fatalf("stale (>10m) cross-section must omit fields, got SeasonRegime=%q WinterDefenseHint=%q",
			stale.ticket.SeasonRegime, stale.ticket.WinterDefenseHint)
	}
}

func TestPipeline_Start_RunsSeasonFeedAndPrunesOnRefresh(t *testing.T) {
	old := exchangeNew
	defer func() { exchangeNew = old }()

	baseOpen := int64(1_710_030_000)
	lastClosedOpen := baseOpen + int64(23*300)
	lastClosedCloseTS := lastClosedOpen + 300

	mock := newRefreshMock("ETH/USDT:USDT", "SOL/USDT:USDT", "XRP/USDT:USDT", "DOGE/USDT:USDT")
	exchangeNew = func(string, exchange.Endpoints) (exchange.Exchange, error) {
		return &ohlcvRefreshMock{
			refreshMockExchange: mock,
			candles5m:           buildPeerCrossSection5m(lastClosedOpen, true),
		}, nil
	}

	cfg := refreshTestConfig(t)
	cfg.Storage.DatabasePath = filepath.Join(t.TempDir(), "kairos.db")
	cfg.Opportunity.Enabled = true
	cfg.Opportunity.ShadowMode = true

	p := NewPipeline(cfg, nil)
	p.refreshInterval = 25 * time.Millisecond
	p.seasonPollInterval = 20 * time.Millisecond
	p.seasonFeed.setNowFunc(func() time.Time { return time.Unix(lastClosedCloseTS+60, 0) })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Start(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if snap := p.seasonBarsSnapshot(); len(snap) == 5 { // 4 tracked + BTC
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if snap := p.seasonBarsSnapshot(); len(snap) != 5 {
		cancel()
		<-done
		t.Fatalf("expected 5 symbols in live season snapshot, got %d", len(snap))
	}

	// Shrink tracked universe to 2 symbols ("ETH", "SOL"); union with BTC = 3 (<4) -> nil snapshot & window count = 3.
	mock.setUniverse("ETH/USDT:USDT", "SOL/USDT:USDT")
	for time.Now().Before(deadline) {
		if p.seasonFeed.windowCount() == 3 && p.seasonBarsSnapshot() == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if wc := p.seasonFeed.windowCount(); wc != 3 {
		cancel()
		<-done
		t.Fatalf("expected departed symbol windows to be pruned to 3 (BTC+ETH+SOL), got %d", wc)
	}
	if snap := p.seasonBarsSnapshot(); snap != nil {
		cancel()
		<-done
		t.Fatalf("expected nil snapshot when universe shrinks to 3 (<4), got %d", len(snap))
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pipeline did not stop")
	}
	p.Close()
}

type ohlcvRefreshMock struct {
	*refreshMockExchange
	candles5m []types.Candle
}

func (m *ohlcvRefreshMock) FetchOHLCV(_ context.Context, _ string, timeframe string, _ int, _ int64) ([]types.Candle, error) {
	if timeframe == "5m" {
		cp := make([]types.Candle, len(m.candles5m))
		copy(cp, m.candles5m)
		return cp, nil
	}
	return nil, nil
}
