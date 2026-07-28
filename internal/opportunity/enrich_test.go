package opportunity

import (
	"context"
	"fmt"
	"testing"

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
