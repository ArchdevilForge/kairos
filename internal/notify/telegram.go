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

// SendText sends an HTML message to the configured chat.
func (t *TelegramClient) SendText(text string) error {
	if !t.IsConfigured() {
		return fmt.Errorf("telegram not configured")
	}
	_, err := t.b.SendMessage(context.Background(), &bot.SendMessageParams{
		ChatID:    t.chatID,
		Text:      text,
		ParseMode: "HTML",
	})
	return err
}

// SendEvent formats an AlertEvent and sends it.
func (t *TelegramClient) SendEvent(event types.AlertEvent) error {
	return t.SendText(formatEvent(event))
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
	}

	condition := html.EscapeString(event.Condition)
	var b strings.Builder
	b.WriteString(fmt.Sprintf("<b>[%s] %s %s</b>\n", severity, symbol, eventName))
	b.WriteString("<b>非指令</b> 仅供人工判断\n")
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
	dims, _ := data["dimensions"].([]any)
	dimCount := len(dims)
	if n, ok := data["dimension_count"].(int); ok && n > 0 {
		dimCount = n
	}
	score, _ := data["signal_score"].(float64)

	var b strings.Builder
	b.WriteString(fmt.Sprintf("<b>[%s] %s 信号质量=%.0f</b>\n", severity, symbol, score))
	b.WriteString("<b>非指令</b> 仅供人工判断\n")
	b.WriteString(fmt.Sprintf("<b>维度</b>: %d个 | %s UTC\n", dimCount, ts))

	for _, dim := range dims {
		dimStr, _ := dim.(string)
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
	zsText := ""
	if zs, ok := data["zscore"]; ok {
		zsText = fmt.Sprintf(" | Z=%v", zs)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("<b>[%s] %s 爆仓异动</b>\n", severity, symbol))
	b.WriteString("<b>非指令</b> 仅供人工判断\n")
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
	zsText := ""
	if zs, ok := data["zscore"]; ok {
		zsText = fmt.Sprintf(" | Z=%v", zs)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("<b>[%s] %s 多空比异动</b>\n", severity, symbol))
	b.WriteString("<b>非指令</b> 仅供人工判断\n")
	b.WriteString(fmt.Sprintf("<b>多/空</b>: %s%% / %s%% (比=%s)%s | %s UTC\n", longR, shortR, ratio, zsText, ts))
	b.WriteString(fmt.Sprintf("<b>原因</b>: %s\n", html.EscapeString(reason)))
	return strings.TrimRight(b.String(), "\n")
}

// ---------------------------------------------------------------------------
// Small helpers
// ---------------------------------------------------------------------------

func eventNameZh(event string) string {
	m := map[string]string{
		"price_velocity":       "价格异动",
		"volume_spike":         "成交量异动",
		"open_interest_change": "持仓量异动",
		"funding_rate_anomaly": "资金费率异动",
		"long_short_ratio":     "多空比异动",
		"liquidation":          "爆仓异动",
		"resonance":            "多维度共振",
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
