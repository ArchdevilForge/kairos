package backtest

import (
	"context"
	"testing"

	"github.com/ArchdevilForge/kairos/internal/types"
)

type stubExchange struct {
	pages map[int64][]types.Candle
	calls []int64
}

func (s *stubExchange) Name() string { return "stub" }

func (s *stubExchange) SubscribeTickers(context.Context, []string, chan<- types.Ticker) error {
	return nil
}

func (s *stubExchange) FetchTickers(context.Context) (map[string]*types.Ticker, error) {
	return nil, nil
}

func (s *stubExchange) FetchTicker(context.Context, string) (*types.Ticker, error) {
	return nil, nil
}

func (s *stubExchange) Close() error { return nil }

func (s *stubExchange) FetchOHLCV(_ context.Context, _ string, _ string, _ int, sinceMs int64) ([]types.Candle, error) {
	s.calls = append(s.calls, sinceMs)
	batch, ok := s.pages[sinceMs]
	if !ok {
		return nil, nil
	}
	return batch, nil
}

func TestFetchOHLCV_BackwardPagination(t *testing.T) {
	endMs := parseDate("2024-03-01")
	startMs := parseDate("2024-01-01")
	midTs := startMs/1000 + 86400*20 // in-range bar between start and end

	ex := &stubExchange{
		pages: map[int64][]types.Candle{
			endMs: {
				{Timestamp: endMs / 1000, Close: 3},
				{Timestamp: midTs, Close: 2.5},
			},
			midTs * 1000: {
				{Timestamp: midTs, Close: 2.5},
				{Timestamp: startMs / 1000, Close: 1},
			},
		},
	}

	runner := New(ex)
	data, err := runner.fetchOHLCV(context.Background(), "BTC/USDT", "2024-01-01", "2024-03-01", "4h")
	if err != nil {
		t.Fatal(err)
	}
	if data == nil || len(data.Closes) < 3 {
		t.Fatalf("expected >=3 bars, got %d (%v) calls=%v keys=%v cursorKey=%d", len(data.Closes), data.Closes, ex.calls, endMs, midTs*1000)
	}
	if data.Closes[0] != 1 || data.Closes[len(data.Closes)-1] != 3 {
		t.Fatalf("closes: %v", data.Closes)
	}
}
