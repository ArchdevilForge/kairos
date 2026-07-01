package detector

import (
	"context"
	"testing"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestNewPriceVelocityDetector_Defaults(t *testing.T) {
	d := NewPriceVelocityDetector(types.PriceVelocityConfig{Enabled: true})
	if !d.enabled {
		t.Fatal("expected enabled")
	}
	if len(d.windows) != 3 {
		t.Fatalf("windows: got %d", len(d.windows))
	}
	if d.cooldown != 60*time.Second {
		t.Fatalf("cooldown: %v", d.cooldown)
	}
}

func TestPriceVelocityDetector_OnTickerNilPrice(t *testing.T) {
	d := NewPriceVelocityDetector(types.PriceVelocityConfig{Enabled: true})
	d.OnTicker(context.Background(), types.Ticker{Symbol: "BTC/USDT:USDT"})
	select {
	case <-d.Events():
		t.Fatal("unexpected event")
	default:
	}
}

func TestFuturesMetricsDetector_OpenInterestChange(t *testing.T) {
	d := NewFuturesMetricsDetector(types.FuturesMetricsConfig{
		Enabled: true,
		OpenInterest: types.OIConfig{
			Enabled:          true,
			MinChangePct:     5.0,
			MinNotifyInterval: "0s",
		},
	})
	ctx := context.Background()
	d.OnMetricsUpdate(ctx, "BTC/USDT:USDT", 1000, 0)
	d.OnMetricsUpdate(ctx, "BTC/USDT:USDT", 1100, 0)

	select {
	case evt := <-d.Events():
		if evt.EventType != "open_interest_change" {
			t.Fatalf("event type: %q", evt.EventType)
		}
	case <-time.After(time.Second):
		t.Fatal("expected open_interest_change event")
	}
}

func TestFuturesMetricsDetector_FundingRateAnomaly(t *testing.T) {
	d := NewFuturesMetricsDetector(types.FuturesMetricsConfig{
		Enabled: true,
		FundingRate: types.FundingRateConfig{
			Enabled:           true,
			AbsRateThreshold:  0.0005,
			MinChangeAbs:      0.0003,
			MinNotifyInterval: "0s",
		},
	})
	ctx := context.Background()
	d.OnMetricsUpdate(ctx, "ETH/USDT:USDT", 0, 0.0001)
	d.OnMetricsUpdate(ctx, "ETH/USDT:USDT", 0, 0.001)

	select {
	case evt := <-d.Events():
		if evt.EventType != "funding_rate_anomaly" {
			t.Fatalf("event type: %q", evt.EventType)
		}
	case <-time.After(time.Second):
		t.Fatal("expected funding_rate_anomaly event")
	}
}

func TestLongShortRatioDetector_EmitsOnExtreme(t *testing.T) {
	d := NewLongShortRatioDetector(types.LongShortRatioConfig{
		Enabled:           true,
		AbsThreshold:      80,
		MinNotifyInterval: "0s",
	})
	d.OnLSSnapshot("BTC/USDT:USDT", 85, 15)
	select {
	case evt := <-d.Events():
		if evt.EventType != "long_short_ratio" {
			t.Fatalf("event type: %q", evt.EventType)
		}
	case <-time.After(time.Second):
		t.Fatal("expected long_short_ratio event")
	}
}

func TestLiquidationDetector_EmitsOnThreshold(t *testing.T) {
	d := NewLiquidationDetector(types.LiquidationConfig{
		Enabled:           true,
		AbsThresholdUsd:   500_000,
		MinNotifyInterval: "0s",
	})
	d.OnLiquidationSnapshot("BTC/USDT:USDT", 2_000_000, 1_500_000, 500_000)
	select {
	case evt := <-d.Events():
		if evt.EventType != "liquidation" {
			t.Fatalf("event type: %q", evt.EventType)
		}
	case <-time.After(time.Second):
		t.Fatal("expected liquidation event")
	}
}

func TestResonanceScorer_MultiDimension(t *testing.T) {
	rs := NewResonanceScorer(types.ResonanceScorerConfig{
		Enabled:         true,
		MinScore:        55,
		MinDimensions:   2,
		WindowSeconds:   300,
		CooldownSeconds: 0,
	})
	now := float64(time.Now().UnixMilli()) / 1000
	rs.OnEvent(types.AnomalyEvent{
		Symbol: "BTC/USDT:USDT", EventType: "price_velocity",
		Severity: types.SeverityHigh, Timestamp: now,
		Data: map[string]any{"change_pct": 2.5, "zscore": 5.0},
	})
	rs.OnEvent(types.AnomalyEvent{
		Symbol: "BTC/USDT:USDT", EventType: "volume_spike",
		Severity: types.SeverityHigh, Timestamp: now,
		Data: map[string]any{"volume_ratio": 8.0, "zscore": 5.5},
	})
	select {
	case re := <-rs.Events():
		if re.DimensionCount < 2 {
			t.Fatalf("dimensions: %d", re.DimensionCount)
		}
		if re.SignalScore < 55 {
			t.Fatalf("score: %v", re.SignalScore)
		}
	case <-time.After(time.Second):
		t.Fatal("expected resonance event")
	}
}

func TestNewEvent(t *testing.T) {
	evt := NewEvent("BTC/USDT:USDT", "price_velocity", "HIGH", map[string]any{"x": 1})
	if evt.Symbol != "BTC/USDT:USDT" || evt.EventType != "price_velocity" {
		t.Fatalf("unexpected event: %+v", evt)
	}
	if evt.Timestamp <= 0 {
		t.Fatal("timestamp missing")
	}
}
