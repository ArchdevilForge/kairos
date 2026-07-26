package notify

import (
	"context"
	"strings"
	"testing"

	"github.com/ArchdevilForge/kairos/internal/detector"
	"github.com/ArchdevilForge/kairos/internal/types"
)

// realDownMarketEvent produces a genuine detector payload by driving a
// MarketPulseDetector through a broad sell-off, so these tests exercise the
// real producer→formatter contract instead of a hand-built map.
func realDownMarketEvent(t *testing.T) types.AlertEvent {
	t.Helper()
	cfg := types.MarketPulseConfig{
		Enabled:                 true,
		ShadowMode:              false,
		SnapshotIntervalSeconds: 5,
		FreshnessSeconds:        30,
		MaxLookupGapSeconds:     15,
		WarmupSeconds:           1,
		HistoryRetentionSeconds: 900,
		MinFreshRatio:           0.5,
		MinValidSymbols:         5,
		NoiseReturnPct:          0.05,
		PrimarySymbols:          []string{"BTC/USDT:USDT"},
		Impulse: types.MarketPulseImpulseConfig{
			MinBreadth: 0.6, MinMedianReturnPct: 0.1,
			ConfirmationSamples: 1, ConfirmationWindowSamples: 1,
		},
		Trend:   types.MarketPulseTrendConfig{MinBreadth: 0.6, MinMedianReturnPct: 0.3, MinPersistSeconds: 10, ConfirmationSamples: 1, ConfirmationWindowSamples: 1},
		Stress:  types.MarketPulseStressConfig{MinBreadth: 0.95, MinMedianReturnPct: 5},
		Decay:   types.MarketPulseDecayConfig{Enabled: true, MaxBreadth: 0.3, MaxMedianReturnPct: 0.05, PersistSeconds: 30, QuietResetSeconds: 30},
		Leaders: types.MarketPulseLeadersConfig{Limit: 3},
	}
	d := detector.NewMarketPulseDetector(cfg)
	clock := 1_000_000.0
	d.SetNowFunc(func() float64 { return clock })

	syms := []string{
		"AAA/USDT:USDT", "BBB/USDT:USDT", "CCC/USDT:USDT", "DDD/USDT:USDT",
		"EEE/USDT:USDT", "FFF/USDT:USDT", "GGG/USDT:USDT", "HHH/USDT:USDT",
	}
	d.UpdateUniverse(syms)

	base := 100.0
	feed := func(mult func(i int) float64) {
		for i, s := range syms {
			p := base * mult(i)
			d.OnTicker(context.Background(), types.Ticker{Symbol: s, LastPrice: &p})
		}
	}
	feed(func(int) float64 { return 1.0 })
	clock += 60
	// Broad sell-off: most symbols -0.5%, one bleeding hardest at -1.5%,
	// one flat (the cross-sectionally strongest name).
	feed(func(i int) float64 {
		switch i {
		case 0:
			return 1.0 // strongest — must NOT appear as 领跌
		case 1:
			return 0.985 // weakest — must appear as 领跌
		default:
			return 0.995
		}
	})
	d.EvaluateAt(clock)

	var evt types.AnomalyEvent
	found := false
	for {
		select {
		case e := <-d.Events():
			if e.EventType == "market_impulse" || e.EventType == "market_stress" {
				evt = e
				found = true
			}
			continue
		default:
		}
		break
	}
	if !found {
		t.Fatalf("detector did not emit a down market event (state=%s)", d.State())
	}
	dir, _ := evt.Data["direction"].(string)
	if dir != "down" {
		t.Fatalf("expected down direction, got %q", dir)
	}

	return types.AlertEvent{
		Event:     evt.EventType,
		Symbol:    evt.Symbol,
		Severity:  evt.Severity,
		Timestamp: "2026-07-26T12:00:00Z",
		Data:      evt.Data,
	}
}

func TestDownMarketAlert_UsesRealDetectorPayload(t *testing.T) {
	alert := realDownMarketEvent(t)

	tgBody := formatEvent(alert)
	_, dtBody := formatDingTalkEvent(alert)

	for name, body := range map[string]string{"telegram": tgBody, "dingtalk": dtBody} {
		if !strings.Contains(body, "跌") {
			t.Fatalf("%s down alert lost down direction:\n%s", name, body)
		}
		// The weakest symbol must be listed as 领跌; the strongest must not.
		if !strings.Contains(body, "BBB") {
			t.Fatalf("%s down alert must list the biggest decliner (laggard):\n%s", name, body)
		}
		if strings.Contains(body, "AAA") {
			t.Fatalf("%s down alert lists the strongest symbol as a decliner:\n%s", name, body)
		}
		if !strings.Contains(body, "领跌") {
			t.Fatalf("%s down alert missing 领跌 header:\n%s", name, body)
		}
		if !strings.Contains(body, manualFooter) {
			t.Fatalf("%s alert missing manual-judgement footer:\n%s", name, body)
		}
	}

	// The breadth numerator must be the decliner count, not advancers.
	decliners := anyInt(alert.Data, "decliners")
	advancers := anyInt(alert.Data, "advancers")
	if decliners == advancers {
		t.Fatalf("test setup broken: decliners=%d advancers=%d must differ", decliners, advancers)
	}
	v := parseMarketPulse(alert)
	if v.Count != decliners {
		t.Fatalf("down breadth numerator = %d, want decliners %d", v.Count, decliners)
	}
}

func TestResonanceFormatter_AcceptsRealStringSliceDimensions(t *testing.T) {
	// Real in-process producers build []string; a JSON round-trip yields
	// []any. Both must render the per-dimension lines.
	for name, dims := range map[string]any{
		"in-process": []string{"price_velocity", "volume_spike"},
		"json-trip":  []any{"price_velocity", "volume_spike"},
	} {
		alert := types.AlertEvent{
			Event: "resonance", Symbol: "SOL/USDT:USDT", Severity: types.SeverityHigh,
			Timestamp: "2026-07-26T12:00:00Z",
			Data: map[string]any{
				"signal_score":    72.0,
				"dimension_count": 2,
				"dimensions":      dims,
				"price_velocity_data": map[string]any{
					"change_pct": 1.4, "window_seconds": 60,
				},
				"volume_spike_data": map[string]any{"ratio": 5.2},
			},
		}
		body := formatEvent(alert)
		_, dt := formatDingTalkEvent(alert)
		for _, out := range []string{body, dt} {
			if !strings.Contains(out, "价格异动") || !strings.Contains(out, "成交量异动") {
				t.Fatalf("%s dimensions not rendered:\n%s", name, out)
			}
		}
	}
}

func TestZScoreDisplay_NilAndNegative(t *testing.T) {
	// A payload without z must not render "Z=?".
	noZ := types.AlertEvent{
		Event: "liquidation", Symbol: "PEPE/USDT:USDT", Severity: types.SeverityMedium,
		Timestamp: "2026-07-26T12:00:00Z",
		Data: map[string]any{
			"total_liquidation_millions": 2.4,
			"long_liquidation_pct":       80.0,
			"short_liquidation_pct":      20.0,
			"reason":                     "liq_absolute",
		},
	}
	if body := formatEvent(noZ); strings.Contains(body, "Z=") {
		t.Fatalf("missing z must render nothing, got:\n%s", body)
	}
	withZ := noZ
	withZ.Data = map[string]any{
		"total_liquidation_millions": 2.4,
		"long_liquidation_pct":       80.0,
		"short_liquidation_pct":      20.0,
		"reason":                     "liq_zscore",
		"zscore":                     -3.1,
	}
	if body := formatEvent(withZ); !strings.Contains(body, "Z=-3.1") {
		t.Fatalf("real negative z must render, got:\n%s", body)
	}
}
