package opportunity

import (
	"context"
	"fmt"
	"testing"

	"github.com/ArchdevilForge/kairos/internal/types"
)

type mockOHLCV struct {
	up bool
}

func (m mockOHLCV) FetchOHLCV(_ context.Context, symbol, timeframe string, limit int, _ int64) ([]types.Candle, error) {
	if limit < 50 {
		limit = 50
	}
	n := limit
	out := make([]types.Candle, n)
	px := 100.0
	for i := 0; i < n; i++ {
		if m.up {
			px += 0.8
			if i%7 == 0 {
				px -= 0.15
			}
		} else {
			px -= 0.8
			if i%7 == 0 {
				px += 0.15
			}
		}
		// slight symbol variance
		if symbol != "BTC/USDT:USDT" {
			if m.up {
				px += 0.05
			} else {
				px -= 0.05
			}
		}
		_ = timeframe
		out[i] = types.Candle{
			Timestamp: int64(1_700_000_000 + i*300),
			Open:      px * 0.999,
			High:      px * 1.002,
			Low:       px * 0.998,
			Close:     px,
			Volume:    1000 + float64(i),
		}
	}
	return out, nil
}

func TestEnrichAndEvaluate_ProducesTickets(t *testing.T) {
	j := testJournal(t)
	s := NewService(j, DefaultConfig())
	evt := types.AnomalyEvent{
		EventType: "market_impulse",
		EventID:   "enrich-1",
		Timestamp: 1_700_000_000,
		Data: map[string]any{
			"direction":             "up",
			"state_to":              "IMPULSE_UP",
			"median_return_60s_pct": 1.0,
			"leaders_detail": []any{
				map[string]any{"symbol": "SOL/USDT:USDT", "return_pct": 5.0, "relative_pct": 4.0},
				map[string]any{"symbol": "ETH/USDT:USDT", "return_pct": 3.0, "relative_pct": 2.0},
			},
		},
	}
	// pulse session first (as pipeline does)
	if _, err := s.HandlePulseEvent(evt); err != nil {
		t.Fatal(err)
	}
	res, err := s.EnrichAndEvaluate(context.Background(), EnrichRequest{
		Event:   evt,
		Fetcher: mockOHLCV{up: true},
		Config:  DefaultEnrichConfig(),
		Equity:  10_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tickets) == 0 {
		// debug why
		t.Fatalf("want tickets, matches=%d ranked=%d session=%s market_align=%s primary=%s",
			len(res.Matches), len(res.Ranked), res.Session.ID,
			res.Session.MarketCycle.Alignment, res.Session.MarketCycle.PrimaryDirection)
	}
	if len(res.Tickets) > 3 {
		t.Fatalf("max 3 tickets, got %d", len(res.Tickets))
	}
	for _, tk := range res.Tickets {
		if len(tk.Invalidations) == 0 {
			t.Fatalf("ticket missing invalidation: %+v", tk)
		}
		if tk.RiskPlan.StopPrice == 0 || tk.RiskPlan.EntryPrice == 0 {
			t.Fatalf("missing entry/stop: %+v", tk.RiskPlan)
		}
	}
	// idempotent session id
	if res.Session.ID != "sess-enrich-1" {
		t.Fatalf("session id=%s", res.Session.ID)
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

type ohlcvFunc func(ctx context.Context, symbol, timeframe string, limit int, beforeMs int64) ([]types.Candle, error)

func (f ohlcvFunc) FetchOHLCV(ctx context.Context, symbol, timeframe string, limit int, beforeMs int64) ([]types.Candle, error) {
	return f(ctx, symbol, timeframe, limit, beforeMs)
}

func TestTriggerPlan_LongStopBelowEntry(t *testing.T) {
	c := make([]types.Candle, 12)
	px := 100.0
	for i := range c {
		px += 1
		c[i] = types.Candle{Close: px, High: px + 1, Low: px - 2, Open: px}
	}
	entry, stop, inv, ok := triggerPlan(types.CycleDirectionUp, c)
	if !ok || stop >= entry || len(inv) == 0 {
		t.Fatalf("entry=%v stop=%v inv=%v ok=%v", entry, stop, inv, ok)
	}
}
