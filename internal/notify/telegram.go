package notify

import (
	"context"
	"fmt"
	"html"
	"strings"

	"github.com/ArchdevilForge/kairos/internal/types"
	"github.com/go-telegram/bot"
)

// TelegramClient wraps the go-telegram/bot library for sending HTML-formatted
// market alerts to a configured Telegram chat.
type TelegramClient struct {
	b      *bot.Bot
	chatID int64
}

// NewTelegramClient creates a client.  Returns an error when the token is
// empty or the bot fails to initialise.
func NewTelegramClient(token string, chatID int64) (*TelegramClient, error) {
	if token == "" {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}
	if chatID == 0 {
		return nil, fmt.Errorf("TELEGRAM_CHAT_ID is required")
	}
	b, err := bot.New(token)
	if err != nil {
		return nil, fmt.Errorf("telegram bot init: %w", err)
	}
	return &TelegramClient{b: b, chatID: chatID}, nil
}

// IsConfigured reports whether the client has both a token and chat ID.
func (t *TelegramClient) IsConfigured() bool {
	return t.b != nil && t.chatID != 0
}

// SendText sends an HTML message to the configured chat. The caller context
// bounds the request so pipeline shutdown can cancel in-flight sends.
func (t *TelegramClient) SendText(ctx context.Context, text string) error {
	if !t.IsConfigured() {
		return fmt.Errorf("telegram not configured")
	}
	_, err := t.b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    t.chatID,
		Text:      text,
		ParseMode: "HTML",
	})
	return err
}

// SendEvent formats an AlertEvent and sends it.
func (t *TelegramClient) SendEvent(ctx context.Context, event types.AlertEvent) error {
	return t.SendText(ctx, formatEvent(event))
}

// ---------------------------------------------------------------------------
// Formatting helpers — ported from src/kairos/telegram.py
// ---------------------------------------------------------------------------

func formatEvent(event types.AlertEvent) string {
	symbol := html.EscapeString(
		strings.ReplaceAll(
			strings.ReplaceAll(event.Symbol, "/USDT:USDT", ""),
			"/USDT", "",
		),
	)
	eventName := html.EscapeString(eventNameZh(event.Event))
	severity := html.EscapeString(severityZh(string(event.Severity)))
	ts := formatTimestamp(event.Timestamp)

	switch event.Event {
	case "resonance":
		return formatResonance(event, symbol, severity, ts)
	case "liquidation":
		return formatLiquidation(event, symbol, severity, ts)
	case "long_short_ratio":
		return formatLongShort(event, symbol, severity, ts)
	case "market_impulse", "market_trend", "market_stress", "market_decay":
		return formatMarketPulse(event, severity, ts)
	case "market_data_stale", "market_data_recovered":
		return formatDataHealth(event, ts)
	}

	condition := html.EscapeString(event.Condition)
	var b strings.Builder
	b.WriteString(fmt.Sprintf("<b>[%s] %s %s</b>\n", severity, symbol, eventName))
	b.WriteString("<b>非指令</b> " + manualFooter + "\n")
	b.WriteString(fmt.Sprintf("<b>价/变</b>: %.2f / %+.2f%% | %s UTC\n", event.Price, event.ChangePct, ts))
	b.WriteString(fmt.Sprintf("<b>触发</b>: %s\n", condition))
	if event.Exchange != "" {
		b.WriteString(fmt.Sprintf("<b>交易所</b>: %s\n", html.EscapeString(event.Exchange)))
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatResonance(event types.AlertEvent, symbol, severity, ts string) string {
	data := event.Data
	if data == nil {
		data = make(map[string]any)
	}
	dims := resonanceDimensions(data)
	dimCount := len(dims)
	if n := anyInt(data, "dimension_count"); n > 0 {
		dimCount = n
	}
	score := anyFloat(data, "signal_score")

	var b strings.Builder
	b.WriteString(fmt.Sprintf("<b>[%s] %s 信号质量=%.0f</b>\n", severity, symbol, score))
	b.WriteString("<b>非指令</b> " + manualFooter + "\n")
	b.WriteString(fmt.Sprintf("<b>维度</b>: %d个 | %s UTC\n", dimCount, ts))

	for _, dimStr := range dims {
		dimZh := eventNameZh(dimStr)
		dimData, _ := data[dimStr+"_data"].(map[string]any)
		if dimData == nil {
			dimData = make(map[string]any)
		}

		switch dimStr {
		case "price_velocity":
			pct := formatField(dimData, "change_pct", "?")
			ws := formatField(dimData, "window_seconds", "?")
			b.WriteString(fmt.Sprintf("  ▸ %s: %s%% / %ss\n", dimZh, pct, ws))
		case "volume_spike":
			ratio := formatField(dimData, "ratio", "?")
			b.WriteString(fmt.Sprintf("  ▸ %s: %sx\n", dimZh, ratio))
		case "open_interest_change":
			pct := formatField(dimData, "change_pct", "?")
			b.WriteString(fmt.Sprintf("  ▸ %s: %s%%\n", dimZh, pct))
		case "funding_rate_anomaly":
			rate := formatField(dimData, "funding_rate", "?")
			b.WriteString(fmt.Sprintf("  ▸ %s: %s\n", dimZh, rate))
		case "long_short_ratio":
			longR := formatField(dimData, "long_rate", "?")
			shortR := formatField(dimData, "short_rate", "?")
			b.WriteString(fmt.Sprintf("  ▸ %s: 多%s%% / 空%s%%\n", dimZh, longR, shortR))
		case "liquidation":
			total := formatField(dimData, "total_liquidation_millions", "?")
			b.WriteString(fmt.Sprintf("  ▸ %s: $%sM\n", dimZh, total))
		default:
			b.WriteString(fmt.Sprintf("  ▸ %s\n", dimZh))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatLiquidation(event types.AlertEvent, symbol, severity, ts string) string {
	data := event.Data
	if data == nil {
		data = make(map[string]any)
	}
	total := formatField(data, "total_liquidation_millions", "0")
	longPct := formatField(data, "long_liquidation_pct", "50")
	shortPct := formatField(data, "short_liquidation_pct", "50")
	reason := fmt.Sprint(data["reason"])
	if reason == "<nil>" {
		reason = "?"
	}
	zsText := optionalZ(data)

	var b strings.Builder
	b.WriteString(fmt.Sprintf("<b>[%s] %s 爆仓异动</b>\n", severity, symbol))
	b.WriteString("<b>非指令</b> " + manualFooter + "\n")
	b.WriteString(fmt.Sprintf("<b>金额</b>: $%sM%s | %s UTC\n", total, zsText, ts))
	b.WriteString(fmt.Sprintf("<b>多/空</b>: %s%% / %s%%\n", longPct, shortPct))
	b.WriteString(fmt.Sprintf("<b>原因</b>: %s\n", html.EscapeString(reason)))
	return strings.TrimRight(b.String(), "\n")
}

func formatLongShort(event types.AlertEvent, symbol, severity, ts string) string {
	data := event.Data
	if data == nil {
		data = make(map[string]any)
	}
	longR := formatField(data, "long_rate", "?")
	shortR := formatField(data, "short_rate", "?")
	ratio := formatField(data, "ls_ratio", "?")
	reason := fmt.Sprint(data["reason"])
	if reason == "<nil>" {
		reason = "?"
	}
	zsText := optionalZ(data)

	var b strings.Builder
	b.WriteString(fmt.Sprintf("<b>[%s] %s 多空比异动</b>\n", severity, symbol))
	b.WriteString("<b>非指令</b> " + manualFooter + "\n")
	b.WriteString(fmt.Sprintf("<b>多/空</b>: %s%% / %s%% (比=%s)%s | %s UTC\n", longR, shortR, ratio, zsText, ts))
	b.WriteString(fmt.Sprintf("<b>原因</b>: %s\n", html.EscapeString(reason)))
	return strings.TrimRight(b.String(), "\n")
}

// ---------------------------------------------------------------------------
// Small helpers
// ---------------------------------------------------------------------------

func formatMarketPulse(event types.AlertEvent, severity, ts string) string {
	v := parseMarketPulse(event)

	var b strings.Builder
	b.WriteString(fmt.Sprintf("<b>%s</b>\n", html.EscapeString(v.Title)))
	b.WriteString("<b>非指令</b> " + manualFooter + "\n")
	if v.From != "" && v.To != "" {
		b.WriteString(fmt.Sprintf("状态：%s → %s\n", html.EscapeString(v.From), html.EscapeString(v.To)))
	}

	switch event.Event {
	case "market_trend":
		b.WriteString(fmt.Sprintf("%s：%+.2f%%\n", v.TrendLabel, v.Med300))
		b.WriteString(fmt.Sprintf("%s：%.0f%%\n", v.BreadthLabel, v.Breadth*100))
	default:
		b.WriteString(fmt.Sprintf("%s：%+.2f%%\n", v.Med60Label, v.Med60))
		b.WriteString(fmt.Sprintf("广度：%.0f%%（%d / %d）\n", v.Breadth*100, v.Count, v.Valid))
		if v.MedZ != 0 {
			b.WriteString(fmt.Sprintf("标准化强度：%+.2fσ\n", v.MedZ))
		}
	}
	// Only mention coverage when it is degraded; a full-coverage reading needs
	// no caveat and the extra line would just add noise.
	if v.Universe > 0 && v.Coverage > 0 && v.Coverage < 0.99 {
		b.WriteString(fmt.Sprintf("样本覆盖：%d / %d（%.0f%%）\n", v.Valid, v.Universe, v.Coverage*100))
	}
	if v.BTC != nil {
		b.WriteString(fmt.Sprintf("BTC：%+.2f%%\n", *v.BTC))
	}
	if v.ETH != nil {
		b.WriteString(fmt.Sprintf("ETH：%+.2f%%\n", *v.ETH))
	}

	if len(v.Movers) > 0 {
		b.WriteString(v.MoversHead + "：\n")
		for _, s := range v.Movers {
			b.WriteString(fmt.Sprintf("%s\n", html.EscapeString(shortSymbol(s))))
		}
	}

	if v.Conclusion != "" {
		b.WriteString(v.Conclusion + "\n")
	}
	b.WriteString(fmt.Sprintf("%s UTC | [%s]\n", ts, html.EscapeString(severity)))
	return strings.TrimRight(b.String(), "\n")
}

// formatDataHealth renders a detector-health notice. This is an operational
// message about the system, not a market signal, so it carries no trading copy.
func formatDataHealth(event types.AlertEvent, ts string) string {
	data := event.Data
	if data == nil {
		data = map[string]any{}
	}
	mins := int(anyFloat(data, "outage_seconds") / 60)
	valid := anyInt(data, "valid_symbols")
	universe := anyInt(data, "universe_size")
	coverage := anyFloat(data, "coverage") * 100

	var b strings.Builder
	if event.Event == "market_data_recovered" {
		b.WriteString("<b>✅ 大盘检测器数据已恢复</b>\n")
		b.WriteString(fmt.Sprintf("中断时长：约 %d 分钟\n", mins))
		b.WriteString(fmt.Sprintf("当前样本：%d / %d（覆盖率 %.0f%%）\n", valid, universe, coverage))
	} else {
		b.WriteString("<b>⚠️ 大盘检测器数据不足</b>\n")
		b.WriteString(fmt.Sprintf("已持续：约 %d 分钟，期间不会产生大盘告警\n", mins))
		b.WriteString(fmt.Sprintf("原因：%s\n", html.EscapeString(formatField(data, "gate_reason", "unknown"))))
		b.WriteString(fmt.Sprintf("样本：%d / %d（覆盖率 %.0f%%，要求 ≥%d 且 ≥%.0f%%）\n",
			valid, universe, coverage,
			anyInt(data, "min_valid"), anyFloat(data, "min_fresh_ratio")*100))
	}
	b.WriteString(fmt.Sprintf("%s UTC\n", ts))
	return strings.TrimRight(b.String(), "\n")
}

func marketPulseTitle(eventType, dir string) string {
	switch eventType {
	case "market_impulse":
		if dir == "down" {
			return "🌊 市场向下启动"
		}
		return "🌊 市场向上启动"
	case "market_trend":
		if dir == "down" {
			return "📉 市场下跌趋势确认"
		}
		return "📈 市场上涨趋势确认"
	case "market_stress":
		if dir == "down" {
			return "🚨 市场快速下跌"
		}
		return "🚨 市场快速上涨"
	case "market_decay":
		return "💤 市场趋势衰减"
	default:
		return "市场异动"
	}
}

func shortSymbol(sym string) string {
	s := strings.ReplaceAll(sym, "/USDT:USDT", "")
	s = strings.ReplaceAll(s, "/USDT", "")
	return s
}

func anyFloat(m map[string]any, key string) float64 {
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}

func anyFloatPtr(m map[string]any, key string) *float64 {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	f := anyFloat(m, key)
	return &f
}

func anyInt(m map[string]any, key string) int {
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

func anyStringSlice(m map[string]any, key string) []string {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	switch xs := v.(type) {
	case []string:
		return xs
	case []any:
		out := make([]string, 0, len(xs))
		for _, x := range xs {
			out = append(out, fmt.Sprint(x))
		}
		return out
	default:
		return nil
	}
}

func eventNameZh(event string) string {
	m := map[string]string{
		"price_velocity":        "价格异动",
		"volume_spike":          "成交量异动",
		"open_interest_change":  "持仓量异动",
		"funding_rate_anomaly":  "资金费率异动",
		"long_short_ratio":      "多空比异动",
		"liquidation":           "爆仓异动",
		"resonance":             "多维度共振",
		"market_impulse":        "市场启动",
		"market_trend":          "市场趋势确认",
		"market_stress":         "市场快速波动",
		"market_decay":          "市场趋势衰减",
		"market_data_stale":     "检测器数据不足",
		"market_data_recovered": "检测器数据恢复",
	}
	if v, ok := m[event]; ok {
		return v
	}
	return event
}

func severityZh(severity string) string {
	m := map[string]string{"LOW": "低", "MEDIUM": "中", "HIGH": "高"}
	if v, ok := m[severity]; ok {
		return v
	}
	return severity
}

// formatTimestamp extracts HH:MM from an ISO 8601 timestamp.
func formatTimestamp(ts string) string {
	if idx := strings.Index(ts, "T"); idx >= 0 {
		t := ts[idx+1:]
		if len(t) > 5 {
			t = t[:5]
		}
		return t
	}
	if len(ts) > 5 {
		return ts[:5]
	}
	return ts
}

// formatField returns the string representation of a key in a map, or
// fallback if missing / nil.
func formatField(m map[string]any, key, fallback string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return fallback
	}
	return fmt.Sprint(v)
}
