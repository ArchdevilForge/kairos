package detector

import (
	"context"
	"testing"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestVolumeSpikeDetector_Emits(t *testing.T) {
	d := NewVolumeSpikeDetector(types.VolumeSpikeConfig{
		Enabled:           true,
		Multiplier:        2.0,
		WindowMinutes:     10,
		MinNotifyInterval: "0s",
	})
	d.minNotifyInterval = 0
	d.windowMinutes = 10
	d.minHistorySeconds = 600

	now := float64(time.Now().UnixMilli()) / 1000
	var points []volPoint
	baseVol := 1000.0
	for i := 0; i < 620; i++ {
		ts := now - float64(620-i)*60
		points = append(points, volPoint{ts: ts, vol: baseVol + float64(i)*5})
	}
	lastVol := points[len(points)-1].vol
	points = append(points, volPoint{ts: now, vol: lastVol + 50000})
	d.volumeHistory["BTC/USDT:USDT"] = points
	d.lastPrice["BTC/USDT:USDT"] = 100

	d.checkSpike("BTC/USDT:USDT", now)
	select {
	case evt := <-d.Events():
		if evt.EventType != "volume_spike" {
			t.Fatalf("event: %+v", evt)
		}
	default:
		t.Fatal("expected volume_spike event")
	}
}

func TestPriceVelocityDetector_Emits(t *testing.T) {
	d := NewPriceVelocityDetector(types.PriceVelocityConfig{
		Enabled: true,
		Windows: []types.PriceWindow{{Seconds: 30, Threshold: 0.5}},
		CooldownSeconds: 0,
	})
	d.cooldown = 0

	now := time.Now()
	ts := float64(now.UnixMilli()) / 1000
	d.priceHistory["BTC/USDT:USDT"] = []pricePoint{
		{ts: ts - 90, price: 100},
		{ts: ts - 60, price: 100},
		{ts: ts - 45, price: 100},
		{ts: ts - 5, price: 100},
		{ts: ts, price: 102},
	}
	d.checkVelocity("BTC/USDT:USDT", 102, ts, now)

	select {
	case evt := <-d.Events():
		if evt.EventType != "price_velocity" {
			t.Fatalf("event: %+v", evt)
		}
	default:
		t.Fatal("expected price_velocity event")
	}
}

func TestVolumeSpikeDetector_UpdateConfigAndReset(t *testing.T) {
	d := NewVolumeSpikeDetector(types.VolumeSpikeConfig{Enabled: true})
	d.OnTicker(context.Background(), types.Ticker{
		Symbol: "ETH/USDT:USDT", QuoteVolume: ptrFloat(1), LastPrice: ptrFloat(2),
	})
	d.Reset()
	d.UpdateConfig(types.VolumeSpikeConfig{
		Enabled: true, Multiplier: 4, WindowMinutes: 5, MinHistorySeconds: 120, MinNotifyInterval: "30s",
	})
	if d.multiplier != 4 || d.windowMinutes != 5 {
		t.Fatalf("config: mult=%v win=%v", d.multiplier, d.windowMinutes)
	}
}

func TestResonanceEvent_ToAlertEventAndScorerHooks(t *testing.T) {
	re := ResonanceEvent{
		Symbol: "BTC/USDT:USDT", SignalScore: 80, DimensionCount: 2,
		Dimensions: map[string]types.AnomalyEvent{
			"price_velocity": {Data: map[string]any{"change_pct": 2.0}},
		},
		Timestamp: 123,
	}
	evt := re.ToAlertEvent()
	if evt.EventType != "resonance" || evt.Data["price_velocity_data"] == nil {
		t.Fatalf("ToAlertEvent: %+v", evt)
	}

	rs := NewResonanceScorer(types.ResonanceScorerConfig{Enabled: true})
	got := false
	rs.SetCallback(func(re ResonanceEvent) { got = true })
	rs.Reset()
	rs.UpdateConfig(types.ResonanceScorerConfig{Enabled: true, MinScore: 40, MinDimensions: 1, CooldownSeconds: 0, WindowSeconds: 300})
	if rs.minScore != 40 {
		t.Fatalf("UpdateConfig: %v", rs.minScore)
	}
	_ = got
}

func TestParseDuration(t *testing.T) {
	if parseDuration("30s") != 30*time.Second {
		t.Fatal("30s")
	}
	if parseDuration("120") != 120*time.Second {
		t.Fatal("plain seconds")
	}
	if parseDuration("") != 0 {
		t.Fatal("empty")
	}
}

func ptrFloat(v float64) *float64 { return &v }
