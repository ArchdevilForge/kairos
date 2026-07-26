package notify

import (
	"fmt"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// marketPulseView is the single parsed representation of a MarketPulse event
// payload. Both Telegram and DingTalk render from it, so direction-dependent
// field selection (advancers vs decliners, leaders vs laggards, 涨/跌 copy)
// lives in exactly one place and cannot drift between channels.
type marketPulseView struct {
	Direction string
	From, To  string

	Med60   float64
	Med300  float64
	Breadth float64
	MedZ    float64

	// Count is the breadth numerator matching the direction: decliners for
	// down moves, advancers otherwise.
	Count int
	Valid int

	BTC *float64
	ETH *float64

	// Movers matches the direction: laggards for down, leaders otherwise.
	Movers     []string
	MoversHead string

	Title        string
	Med60Label   string
	TrendLabel   string
	BreadthLabel string
	Conclusion   string
}

func parseMarketPulse(event types.AlertEvent) marketPulseView {
	data := event.Data
	if data == nil {
		data = map[string]any{}
	}
	v := marketPulseView{
		Med60:   anyFloat(data, "median_return_60s_pct"),
		Med300:  anyFloat(data, "median_return_300s_pct"),
		Breadth: anyFloat(data, "breadth"),
		MedZ:    anyFloat(data, "median_z_60s"),
		Valid:   anyInt(data, "valid_symbols"),
		BTC:     anyFloatPtr(data, "btc_return_pct"),
		ETH:     anyFloatPtr(data, "eth_return_pct"),
	}
	if s := fmt.Sprint(data["direction"]); s != "<nil>" {
		v.Direction = s
	}
	if s := fmt.Sprint(data["state_from"]); s != "<nil>" {
		v.From = s
	}
	if s := fmt.Sprint(data["state_to"]); s != "<nil>" {
		v.To = s
	}

	down := v.Direction == "down"
	if down {
		v.Count = anyInt(data, "decliners")
		v.Movers = anyStringSlice(data, "laggards")
		v.Med60Label = "1分钟市场中位跌幅"
		v.TrendLabel = "5分钟市场中位跌幅"
		v.BreadthLabel = "下跌广度"
		v.MoversHead = "领跌"
	} else {
		v.Count = anyInt(data, "advancers")
		v.Movers = anyStringSlice(data, "leaders")
		v.Med60Label = "1分钟市场中位涨幅"
		v.TrendLabel = "5分钟市场中位涨幅"
		v.BreadthLabel = "上涨广度"
		v.MoversHead = "领涨"
	}
	if event.Event == "market_trend" {
		if down {
			v.MoversHead = "弱于市场"
		} else {
			v.MoversHead = "强于市场"
		}
	}

	v.Title = marketPulseTitle(event.Event, v.Direction)
	switch event.Event {
	case "market_impulse":
		v.Conclusion = "结论：市场出现同步异动，值得打开盘面观察。"
	case "market_trend":
		v.Conclusion = "结论：趋势确认，建议打开盘面观察。"
	case "market_stress":
		v.Conclusion = "注意：市场出现系统性快速波动。"
	case "market_decay":
		v.Conclusion = "结论：趋势广度衰减，继续盯盘价值下降。"
	}
	return v
}

// resonanceDimensions reads the dimensions list from a resonance payload,
// accepting both the in-process []string shape and the []any shape a JSON
// round-trip produces.
func resonanceDimensions(data map[string]any) []string {
	return anyStringSlice(data, "dimensions")
}

// optionalZ returns the z-score display text (" | Z=…") only when the payload
// carries a real z value — a missing or nil z renders nothing instead of Z=?.
func optionalZ(data map[string]any) string {
	v, ok := data["zscore"]
	if !ok || v == nil {
		return ""
	}
	return fmt.Sprintf(" | Z=%s", formatField(data, "zscore", "?"))
}

// manualFooter is the exact human-control sentence required by the backend
// spec for every outbound alert.
const manualFooter = "仅供人工判断，不自动交易。"
