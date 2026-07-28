package opportunity

import (
	"context"
	"testing"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestTrackOutcomes_FillsMFE(t *testing.T) {
	j := testJournal(t)
	s := NewService(j, DefaultConfig())
	_ = j.SaveTicket(types.DecisionTicket{
		ID: "t-out-1", SessionID: "s1", Symbol: "SOL/USDT:USDT",
		Direction: types.CycleDirectionUp, Status: types.TicketStatusAccepted,
		RiskPlan: types.RiskPlan{EntryPrice: 100, StopPrice: 97},
	})
	_ = j.SaveDecision(types.DecisionRecord{TicketID: "t-out-1", Decision: types.DecisionAccepted})

	// path: dip then rally
	fetch := ohlcvFunc(func(ctx context.Context, symbol, timeframe string, limit int, beforeMs int64) ([]types.Candle, error) {
		px := []float64{100, 99, 98, 101, 104, 106, 105}
		out := make([]types.Candle, len(px))
		for i, p := range px {
			out[i] = types.Candle{Close: p, High: p * 1.001, Low: p * 0.999, Open: p, Volume: 1}
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
	if o.MFE <= 0 || o.MaxRealizableR <= 0 {
		t.Fatalf("outcome=%+v", o)
	}
	if o.Decision != types.DecisionAccepted {
		t.Fatalf("decision=%s", o.Decision)
	}
	// second pass unchanged → 0 updates
	n, err = s.TrackOutcomes(context.Background(), fetch, DefaultOutcomeTrackConfig())
	if err != nil || n != 0 {
		t.Fatalf("idempotent updated=%d err=%v", n, err)
	}
}

func TestRunOutcomeLoop_Cancels(t *testing.T) {
	s := NewService(testJournal(t), DefaultConfig())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.RunOutcomeLoop(ctx, ohlcvFunc(func(context.Context, string, string, int, int64) ([]types.Candle, error) {
			return nil, nil
		}), OutcomeTrackConfig{Interval: 50 * time.Millisecond, Timeout: time.Second})
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
