package opportunity

import (
	"testing"

	"github.com/ArchdevilForge/kairos/internal/ranker"
	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestRankInputsFromPulse_Detail(t *testing.T) {
	evt := types.AnomalyEvent{
		Data: map[string]any{
			"median_return_60s_pct": 1.0,
			"btc_return_pct":        0.5,
			"leaders_detail": []any{
				map[string]any{"symbol": "SOL/USDT:USDT", "return_pct": 4.0, "relative_pct": 3.0},
				map[string]any{"symbol": "ETH/USDT:USDT", "return_pct": 2.0, "relative_pct": 1.0},
			},
			"laggards_detail": []any{
				map[string]any{"symbol": "AAA/USDT:USDT", "return_pct": -2.0, "relative_pct": -3.0},
			},
		},
	}
	in := RankInputsFromPulse(evt)
	if len(in) != 3 {
		t.Fatalf("inputs=%d", len(in))
	}
	longs := ranker.RankLong(in, ranker.SoftConfig())
	if len(longs) == 0 || longs[0].Symbol != "SOL/USDT:USDT" {
		t.Fatalf("leader=%v", longs)
	}
	if longs[0].LiquidityOK {
		t.Fatal("pulse soft rank must not claim liquidity ok")
	}
}

func TestRankInputsFromPulse_NameFallback(t *testing.T) {
	evt := types.AnomalyEvent{
		Data: map[string]any{
			"leaders":  []string{"A/USDT:USDT", "B/USDT:USDT"},
			"laggards": []string{"Z/USDT:USDT"},
		},
	}
	in := RankInputsFromPulse(evt)
	if len(in) != 3 {
		t.Fatalf("inputs=%d", len(in))
	}
}

func TestHandlePulseEvent_SavesCandidates(t *testing.T) {
	j := testJournal(t)
	s := NewService(j, DefaultConfig())
	evt := types.AnomalyEvent{
		EventType: "market_impulse", EventID: "rank-e1", Timestamp: 1,
		Data: map[string]any{
			"direction": "up", "state_to": "IMPULSE_UP",
			"leaders_detail": []any{
				map[string]any{"symbol": "SOL/USDT:USDT", "return_pct": 5.0, "relative_pct": 4.0},
			},
		},
	}
	sess, err := s.HandlePulseEvent(evt)
	if err != nil || sess == nil {
		t.Fatal(err)
	}
	c, ok, err := j.GetCandidates(sess.ID)
	if err != nil || !ok || len(c.Candidates) < 1 {
		t.Fatalf("candidates ok=%v n=%d err=%v", ok, len(c.Candidates), err)
	}
}
