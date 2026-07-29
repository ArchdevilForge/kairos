package opportunity

import (
	"context"
	"testing"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
)

type ohlcvFunc func(ctx context.Context, symbol, timeframe string, limit int, beforeMs int64) ([]types.Candle, error)

func (f ohlcvFunc) FetchOHLCV(ctx context.Context, symbol, timeframe string, limit int, beforeMs int64) ([]types.Candle, error) {
	return f(ctx, symbol, timeframe, limit, beforeMs)
}

func TestForwardBars_StartsAtNextBarOpen(t *testing.T) {
	triggerClose := int64(1000) // trigger bar 700–1000 closed at 1000
	candles := []types.Candle{
		{Timestamp: 700, Open: 99, High: 100, Low: 98, Close: 100},   // trigger bar open — exclude
		{Timestamp: 1000, Open: 100, High: 102, Low: 99, Close: 101}, // first post-entry — include
		{Timestamp: 1300, Open: 101, High: 103, Low: 100, Close: 102},
	}
	bars := forwardBars(candles, triggerClose)
	if len(bars) != 2 {
		t.Fatalf("want 2 forward bars, got %d", len(bars))
	}
	if bars[0].TS != triggerClose {
		t.Fatalf("first forward bar open must equal trigger close: %d", bars[0].TS)
	}
	if bars[0].High != 102 {
		t.Fatalf("first bar high=%v", bars[0].High)
	}
}

func TestTrackOutcomes_UsesSignalTimeAndHighLow(t *testing.T) {
	j := testJournal(t)
	s := NewService(j, DefaultConfig())
	signal := time.Now().Add(-30 * time.Minute).Unix()
	_ = j.SaveTicket(types.DecisionTicket{
		ID: "t-out-1", SessionID: "s1", Symbol: "SOL/USDT:USDT",
		Direction: types.CycleDirectionUp, Status: types.TicketStatusAccepted,
		RiskPlan:  types.RiskPlan{EntryPrice: 100, StopPrice: 97},
		CreatedAt: signal, SignalAt: signal, EntryTriggeredAt: signal,
	})
	_ = j.SaveDecision(types.DecisionRecord{TicketID: "t-out-1", Decision: types.DecisionAccepted})

	fetch := ohlcvFunc(func(ctx context.Context, symbol, timeframe string, limit int, beforeMs int64) ([]types.Candle, error) {
		// include bars before signal (should be filtered) and after
		var out []types.Candle
		for i := 0; i < 10; i++ {
			ts := signal - 600 + int64(i*300) // some before
			px := 100.0 + float64(i)*0.5
			out = append(out, types.Candle{
				Timestamp: ts, Open: px, Close: px, High: px + 1, Low: px - 1, Volume: 1,
			})
		}
		return out, nil
	})

	n, err := s.TrackOutcomes(context.Background(), fetch, DefaultOutcomeTrackConfig())
	if err != nil || n != 1 {
		t.Fatalf("updated=%d err=%v", n, err)
	}
	o, ok, err := j.GetOutcome("t-out-1")
	if err != nil || !ok {
		t.Fatal(err)
	}
	if o.PathStartUnix < signal {
		t.Fatalf("path started before signal: %d < %d", o.PathStartUnix, signal)
	}
	if o.MFE <= 0 {
		t.Fatalf("mfe from highs should be >0: %+v", o)
	}
}

func TestTrackOutcomes_MaxAge(t *testing.T) {
	j := testJournal(t)
	s := NewService(j, DefaultConfig())
	old := time.Now().Add(-48 * time.Hour).Unix()
	_ = j.SaveTicket(types.DecisionTicket{
		ID: "old", Symbol: "X/USDT:USDT", Direction: types.CycleDirectionUp,
		CreatedAt: old, SignalAt: old, RiskPlan: types.RiskPlan{EntryPrice: 1, StopPrice: 0.9},
	})
	fetch := ohlcvFunc(func(context.Context, string, string, int, int64) ([]types.Candle, error) {
		return []types.Candle{{Timestamp: time.Now().Unix(), Close: 1, High: 1.1, Low: 0.9}}, nil
	})
	n, err := s.TrackOutcomes(context.Background(), fetch, DefaultOutcomeTrackConfig())
	if err != nil || n != 0 {
		t.Fatalf("max age should skip, n=%d err=%v", n, err)
	}
}

func TestRunOutcomeLoop_Cancels(t *testing.T) {
	s := NewService(testJournal(t), DefaultConfig())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.RunOutcomeLoop(ctx, ohlcvFunc(func(context.Context, string, string, int, int64) ([]types.Candle, error) {
			return nil, nil
		}), OutcomeTrackConfig{Interval: 50 * time.Millisecond, Timeout: time.Second, MaxAge: time.Hour})
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not stop")
	}
}
