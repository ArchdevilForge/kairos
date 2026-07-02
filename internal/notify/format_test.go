package notify

import (
	"strings"
	"testing"

	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestFormatResonance_AllDimensions(t *testing.T) {
	text := formatResonance(types.AlertEvent{
		Data: map[string]any{
			"signal_score":    88.0,
			"dimension_count": 6,
			"dimensions": []any{
				"price_velocity", "volume_spike", "open_interest_change",
				"funding_rate_anomaly", "long_short_ratio", "liquidation", "unknown_dim",
			},
			"price_velocity_data":       map[string]any{"change_pct": 1.2, "window_seconds": 30},
			"volume_spike_data":         map[string]any{"ratio": 4.5},
			"open_interest_change_data": map[string]any{"change_pct": 6.0},
			"funding_rate_anomaly_data": map[string]any{"funding_rate": 0.001},
			"long_short_ratio_data":     map[string]any{"long_rate": 60, "short_rate": 40},
			"liquidation_data":          map[string]any{"total_liquidation_millions": 12.3},
		},
	}, "BTC", "高", "12:34")
	for _, want := range []string{"信号质量=88", "价格异动", "成交量异动", "持仓量异动", "资金费率异动", "多空比异动", "爆仓异动", "unknown_dim"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
}

func TestFormatLiquidationAndLongShort(t *testing.T) {
	liq := formatLiquidation(types.AlertEvent{
		Data: map[string]any{
			"total_liquidation_millions": 5.5,
			"long_liquidation_pct":       70,
			"short_liquidation_pct":      30,
			"reason":                     "zscore spike",
			"zscore":                     3.2,
		},
	}, "ETH", "中", "01:02")
	if !strings.Contains(liq, "$5.5M") || !strings.Contains(liq, "Z=3.2") {
		t.Fatalf("liquidation: %s", liq)
	}

	ls := formatLongShort(types.AlertEvent{
		Data: map[string]any{
			"long_rate": 55, "short_rate": 45, "ls_ratio": 1.22,
			"reason": "extreme", "zscore": 2.1,
		},
	}, "SOL", "低", "23:59")
	if !strings.Contains(ls, "55") || !strings.Contains(ls, "比=1.22") {
		t.Fatalf("long/short: %s", ls)
	}
}

func TestFormatTimestampAndField(t *testing.T) {
	if got := formatTimestamp("2026-06-27T01:02:03+00:00"); got != "01:02" {
		t.Fatalf("iso ts: %q", got)
	}
	if got := formatTimestamp("1234567890"); got != "12345" {
		t.Fatalf("short fallback: %q", got)
	}
	m := map[string]any{"x": nil}
	if formatField(m, "x", "?") != "?" || formatField(m, "missing", "-") != "-" {
		t.Fatal("formatField fallback")
	}
	if formatField(map[string]any{"n": 42}, "n", "?") != "42" {
		t.Fatal("formatField value")
	}
}

func TestEventNameAndSeverityZh(t *testing.T) {
	if eventNameZh("resonance") != "多维度共振" {
		t.Fatal("eventNameZh")
	}
	if eventNameZh("custom_event") != "custom_event" {
		t.Fatal("unknown event")
	}
	if severityZh("HIGH") != "高" || severityZh("UNKNOWN") != "UNKNOWN" {
		t.Fatal("severityZh")
	}
}

func TestFormatResonance_NilData(t *testing.T) {
	text := formatResonance(types.AlertEvent{}, "BTC", "高", "00:00")
	if !strings.Contains(text, "信号质量=0") {
		t.Fatalf("nil data: %s", text)
	}
}

func TestFormatLiquidation_NilReason(t *testing.T) {
	text := formatLiquidation(types.AlertEvent{Data: map[string]any{}}, "BTC", "高", "00:00")
	if strings.Contains(text, "<nil>") {
		t.Fatalf("nil zscore must not leak HTML: %s", text)
	}
	text = formatLiquidation(types.AlertEvent{Data: map[string]any{"zscore": nil}}, "BTC", "高", "00:00")
	if strings.Contains(text, "<nil>") {
		t.Fatalf("nil zscore field: %s", text)
	}
	if !strings.Contains(text, "原因") {
		t.Fatalf("nil reason: %s", text)
	}
}
