package detector

import (
	"testing"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestResonanceScorer_CallbackAndPrune(t *testing.T) {
	rs := NewResonanceScorer(types.ResonanceScorerConfig{
		Enabled: true, MinScore: 30, MinDimensions: 2, CooldownSeconds: 0, WindowSeconds: 300,
	})
	var got ResonanceEvent
	rs.SetCallback(func(re ResonanceEvent) { got = re })

	now := float64(time.Now().UnixMilli()) / 1000
	events := []types.AnomalyEvent{
		{Symbol: "ETH/USDT:USDT", EventType: "price_velocity", Severity: types.SeverityHigh, Timestamp: now, Data: map[string]any{"change_pct": 3.0, "zscore": 4.0}},
		{Symbol: "ETH/USDT:USDT", EventType: "volume_spike", Severity: types.SeverityHigh, Timestamp: now, Data: map[string]any{"ratio": 8.0, "zscore": 5.0}},
		{Symbol: "ETH/USDT:USDT", EventType: "funding_rate_anomaly", Severity: types.SeverityHigh, Timestamp: now, Data: map[string]any{"funding_rate": 0.002, "zscore": 3.0}},
	}
	for _, evt := range events {
		rs.OnEvent(evt)
	}
	if got.Symbol == "" {
		t.Fatal("expected callback resonance event")
	}

	// Expired window prune path.
	rs.OnEvent(types.AnomalyEvent{
		Symbol: "SOL/USDT:USDT", EventType: "price_velocity", Timestamp: now - 400,
		Data: map[string]any{"change_pct": 1.0, "zscore": 1.0},
	})
	rs.Reset()
}

func TestResonanceScorer_DisabledIgnores(t *testing.T) {
	rs := NewResonanceScorer(types.ResonanceScorerConfig{Enabled: false})
	rs.OnEvent(types.AnomalyEvent{Symbol: "BTC/USDT:USDT", EventType: "price_velocity"})
	select {
	case <-rs.Events():
		t.Fatal("unexpected event")
	default:
	}
}

func TestResonanceScorer_KeepsHigherExtremity(t *testing.T) {
	rs := NewResonanceScorer(types.ResonanceScorerConfig{Enabled: true, MinDimensions: 1, MinScore: 0, CooldownSeconds: 0})
	now := float64(time.Now().UnixMilli()) / 1000
	rs.OnEvent(types.AnomalyEvent{
		Symbol: "BTC/USDT:USDT", EventType: "price_velocity", Timestamp: now,
		Data: map[string]any{"change_pct": 1.0, "zscore": 5.0},
	})
	rs.OnEvent(types.AnomalyEvent{
		Symbol: "BTC/USDT:USDT", EventType: "price_velocity", Timestamp: now + 1,
		Data: map[string]any{"change_pct": 1.0, "zscore": 2.0},
	})
}

func TestDirectionBiasAndHelpers(t *testing.T) {
	if directionBias("price_velocity", map[string]any{"change_pct": 2.0}) <= 0 {
		t.Fatal("long bias")
	}
	if directionBias("price_velocity", map[string]any{"change_pct": -2.0}) >= 0 {
		t.Fatal("short bias")
	}
	if !allPositive([]int{1, 2}) || allNegative([]int{1, 2}) {
		t.Fatal("allPositive")
	}
	if sevScore("HIGH") <= sevScore("LOW") {
		t.Fatal("sevScore")
	}
}
