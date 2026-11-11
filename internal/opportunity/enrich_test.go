package opportunity

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ArchdevilForge/kairos/internal/cycle"
	"github.com/ArchdevilForge/kairos/internal/types"
)

type mockOHLCV struct{}

func (m mockOHLCV) FetchOHLCV(_ context.Context, symbol, timeframe string, limit int, _ int64) ([]types.Candle, error) {
	if timeframe == "5m" {
		c := synthLongPullback()
		// pad front so len matches limit-ish
		for len(c) < 50 {
			c = append([]types.Candle{bar(100, 100.2, 99.8)}, c...)
		}
		for i := range c {
			c[i].Timestamp = int64(1_700_000_000 + i*300)
			c[i].Volume = 50_000
		}
		return c, nil
	}
	if limit < 50 {
		limit = 50
	}
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
			Timestamp: int64(1_700_000_000 + i*3600),
			Open:      px * 0.999, High: px * 1.002, Low: px * 0.998, Close: px, Volume: 50_000,
		}
	}
	return out, nil
}

func TestEnrichAndEvaluate_ProducesTickets(t *testing.T) {
	j := testJournal(t)
	s := NewService(j, DefaultConfig())
	evt := types.AnomalyEvent{
		EventType: "market_impulse", EventID: "enrich-1", Timestamp: 1_700_000_000,
		Data: map[string]any{
			"direction": "up", "state_to": "IMPULSE_UP", "median_return_60s_pct": 1.0,
			"leaders_detail": []any{
				map[string]any{"symbol": "SOL/USDT:USDT", "return_pct": 5.0, "relative_pct": 4.0},
			},
		},
	}
	if _, err := s.HandlePulseEvent(evt); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultEnrichConfig()
	cfg.AssumeSpreadOK = true
	cfg.MinQuoteVol = 1
	// unit fixture is historical path; allow pullback before synthetic pulse ts
	cfg.RequireInSessionPullback = false
	res, err := s.EnrichAndEvaluate(context.Background(), EnrichRequest{
		Event: evt, Fetcher: mockOHLCV{}, Config: cfg, Equity: 10_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tickets) == 0 {
		for _, m := range res.Matches {
			t.Logf("fail=%v", m.HardFailures)
		}
		t.Fatalf("want tickets, matches=%d", len(res.Matches))
	}
	tk := res.Tickets[0]
	if tk.CreatedAt == 0 || tk.SignalAt == 0 {
		t.Fatalf("timestamps missing: %+v", tk)
	}
	if len(tk.Invalidations) == 0 || tk.RiskPlan.StopPrice == 0 {
		t.Fatalf("plan incomplete: %+v", tk)
	}
}

func TestEnrichAndEvaluate_SpreadFailClosed(t *testing.T) {
	s := NewService(testJournal(t), DefaultConfig())
	evt := types.AnomalyEvent{
		EventType: "market_impulse", EventID: "sp-1", Timestamp: 1,
		Data: map[string]any{
			"direction": "up",
			"leaders_detail": []any{
				map[string]any{"symbol": "SOL/USDT:USDT", "return_pct": 4.0, "relative_pct": 3.0},
			},
		},
	}
	cfg := DefaultEnrichConfig()
	cfg.AssumeSpreadOK = false
	cfg.MinQuoteVol = 1
	res, err := s.EnrichAndEvaluate(context.Background(), EnrichRequest{
		Event: evt, Fetcher: mockOHLCV{}, Config: cfg,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tickets) != 0 {
		t.Fatal("unmeasured spread must not mint tickets")
	}
}

func TestEnrichAndEvaluate_FetchFailClosed(t *testing.T) {
	s := NewService(testJournal(t), DefaultConfig())
	fail := ohlcvFunc(func(ctx context.Context, symbol, timeframe string, limit int, beforeMs int64) ([]types.Candle, error) {
		return nil, fmt.Errorf("boom")
	})
	_, err := s.EnrichAndEvaluate(context.Background(), EnrichRequest{
		Event: types.AnomalyEvent{
			EventType: "market_impulse", EventID: "fail-1", Timestamp: 1,
			Data: map[string]any{
				"direction": "up",
				"leaders_detail": []any{
					map[string]any{"symbol": "SOL/USDT:USDT", "return_pct": 3.0, "relative_pct": 2.0},
				},
			},
		},
		Fetcher: fail,
	})
	if err == nil {
		t.Fatal("market fetch fail should error")
	}
}

func TestWatchAndEnrich_RefreshesUniverseBars5mSnapshot(t *testing.T) {
	j := testJournal(t)
	cfgSvc := DefaultConfig()
	cfgSvc.SessionTTLSeconds = 60
	s := NewService(j, cfgSvc)

	baseOpen := int64(1_700_000_000)
	// Build 54 closed 5m bars + 1 forming bar; pulse sits right before pullback low.
	build5m := func(withRestart bool) []types.Candle {
		c := synthLongPullback()
		for len(c) < 54 {
			c = append([]types.Candle{bar(100, 100.2, 99.8)}, c...)
		}
		if !withRestart {
			// Continue stepping down on the final 3 bars so attempt #1 sits on the low (no restart yet).
			px := c[len(c)-4].Low
			for i := len(c) - 3; i < len(c); i++ {
				px -= 0.4
				c[i] = bar(px, px+0.1, px-0.1)
			}
		}
		// Append 1 unclosed forming bar (dropped by enrich.go:173).
		c = append(c, bar(120, 121, 119))
		for i := range c {
			c[i].Timestamp = baseOpen + int64(i*300)
			c[i].Volume = 50_000
		}
		return c
	}

	lastClosedOpen := baseOpen + int64(53*300)
	pulseTS := lastClosedOpen - int64(8*300)

	snapCalls := 0
	fetcher := ohlcvFunc(func(ctx context.Context, symbol, timeframe string, limit int, beforeMs int64) ([]types.Candle, error) {
		if timeframe == "5m" {
			return build5m(snapCalls >= 2), nil
		}
		return (mockOHLCV{}).FetchOHLCV(ctx, symbol, timeframe, limit, beforeMs)
	})

	evt := types.AnomalyEvent{
		EventType: "market_impulse", EventID: "watch-snap-1", Timestamp: float64(pulseTS),
		Data: map[string]any{
			"direction": "up", "state_to": "IMPULSE_UP", "median_return_60s_pct": 1.0,
			"leaders_detail": []any{
				map[string]any{"symbol": "SOL/USDT:USDT", "return_pct": 5.0, "relative_pct": 4.0},
			},
		},
	}

	snapFn := func() map[string][]cycle.SeasonBar5m {
		snapCalls++
		if snapCalls == 1 {
			// Cross-section not ready on initial attempt.
			return nil
		}
		syms := []string{"BTC/USDT:USDT", "ETH/USDT:USDT", "SOL/USDT:USDT", "XRP/USDT:USDT"}
		out := make(map[string][]cycle.SeasonBar5m, len(syms))
		startClose := lastClosedOpen + 300 - int64(23*300)
		for _, sym := range syms {
			bars := make([]cycle.SeasonBar5m, 24)
			for i := range bars {
				bars[i] = cycle.SeasonBar5m{
					CloseTS:  startClose + int64(i*300),
					Open:     100.0,
					High:     100.1,
					Low:      99.5,
					Close:    99.6, // negative returns across all 4 symbols -> WINTER
					QuoteVol: 40_000,
				}
			}
			out[sym] = bars
		}
		return out
	}

	ecfg := DefaultEnrichConfig()
	ecfg.AssumeSpreadOK = true
	ecfg.MinQuoteVol = 1
	ecfg.WatchInterval = 15 * time.Millisecond

	s.WatchAndEnrich(context.Background(), EnrichRequest{
		Event:                  evt,
		Fetcher:                fetcher,
		Config:                 ecfg,
		UniverseBars5mSnapshot: snapFn,
	})

	if snapCalls < 2 {
		t.Fatalf("expected UniverseBars5mSnapshot to be called across retries, got %d", snapCalls)
	}
	tickets, err := j.ListTickets("")
	if err != nil || len(tickets) != 1 {
		t.Fatalf("tickets=%d err=%v", len(tickets), err)
	}
	if tickets[0].SeasonRegime != types.SeasonRegimeWinter || tickets[0].WinterDefenseHint != types.DefaultWinterDefenseHint {
		t.Fatalf("ticket got SeasonRegime=%q WinterDefenseHint=%q, want WINTER + default hint",
			tickets[0].SeasonRegime, tickets[0].WinterDefenseHint)
	}
}
