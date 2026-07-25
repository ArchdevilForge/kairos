package notify

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// DingTalkClient sends markdown alerts to a DingTalk custom-robot webhook.
type DingTalkClient struct {
	webhookURL string
	secret     string
	httpClient *http.Client
}

// NewDingTalkClient builds a client. webhookURL is required; secret enables 加签.
func NewDingTalkClient(webhookURL, secret string) (*DingTalkClient, error) {
	webhookURL = strings.TrimSpace(webhookURL)
	if webhookURL == "" {
		return nil, fmt.Errorf("DINGTALK_WEBHOOK_URL is required")
	}
	if _, err := url.ParseRequestURI(webhookURL); err != nil {
		return nil, fmt.Errorf("invalid DINGTALK_WEBHOOK_URL: %w", err)
	}
	return &DingTalkClient{
		webhookURL: webhookURL,
		secret:     strings.TrimSpace(secret),
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// IsConfigured reports whether a webhook is present.
func (d *DingTalkClient) IsConfigured() bool {
	return d != nil && d.webhookURL != ""
}

// SendText posts a markdown message (title truncated from first line).
func (d *DingTalkClient) SendText(text string) error {
	if !d.IsConfigured() {
		return fmt.Errorf("dingtalk not configured")
	}
	title := "kairos"
	if line, _, ok := strings.Cut(text, "\n"); ok && strings.TrimSpace(line) != "" {
		title = strings.TrimSpace(line)
	} else if strings.TrimSpace(text) != "" {
		title = strings.TrimSpace(text)
	}
	if len([]rune(title)) > 64 {
		title = string([]rune(title)[:64])
	}
	return d.sendMarkdown(title, text)
}

// SendEvent formats an AlertEvent as DingTalk markdown and sends it.
func (d *DingTalkClient) SendEvent(event types.AlertEvent) error {
	title, body := formatDingTalkEvent(event)
	return d.sendMarkdown(title, body)
}

func (d *DingTalkClient) sendMarkdown(title, text string) error {
	payload := map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": title,
			"text":  text,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	endpoint, err := d.signedURL()
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("dingtalk http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("dingtalk decode: %w body=%s", err, strings.TrimSpace(string(raw)))
	}
	if result.ErrCode != 0 {
		return fmt.Errorf("dingtalk errcode=%d errmsg=%s", result.ErrCode, result.ErrMsg)
	}
	return nil
}

// signedURL appends timestamp+sign when secret is set (官方加签).
func (d *DingTalkClient) signedURL() (string, error) {
	if d.secret == "" {
		return d.webhookURL, nil
	}
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	sign, err := dingTalkSign(ts, d.secret)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(d.webhookURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("timestamp", ts)
	q.Set("sign", sign)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// dingTalkSign = url_encode(base64(hmac_sha256(secret, timestamp + "\n" + secret)))
func dingTalkSign(timestamp, secret string) (string, error) {
	stringToSign := timestamp + "\n" + secret
	mac := hmac.New(sha256.New, []byte(secret))
	if _, err := mac.Write([]byte(stringToSign)); err != nil {
		return "", err
	}
	return url.QueryEscape(base64.StdEncoding.EncodeToString(mac.Sum(nil))), nil
}

// ---------------------------------------------------------------------------
// Markdown formatters — same fields as Telegram HTML, DingTalk markup.
// ---------------------------------------------------------------------------

func formatDingTalkEvent(event types.AlertEvent) (title, body string) {
	symbol := strings.ReplaceAll(strings.ReplaceAll(event.Symbol, "/USDT:USDT", ""), "/USDT", "")
	eventName := eventNameZh(event.Event)
	severity := severityZh(string(event.Severity))
	ts := formatTimestamp(event.Timestamp)

	switch event.Event {
	case "resonance":
		return formatDingTalkResonance(event, symbol, severity, ts)
	case "liquidation":
		return formatDingTalkLiquidation(event, symbol, severity, ts)
	case "long_short_ratio":
		return formatDingTalkLongShort(event, symbol, severity, ts)
	case "market_impulse", "market_trend", "market_stress", "market_decay":
		return formatDingTalkMarketPulse(event, severity, ts)
	}

	title = fmt.Sprintf("[%s] %s %s", severity, symbol, eventName)
	var b strings.Builder
	b.WriteString(fmt.Sprintf("### %s\n\n", title))
	b.WriteString("**非指令** 仅供人工判断\n\n")
	b.WriteString(fmt.Sprintf("**价/变**: %.2f / %+.2f%% | %s UTC\n\n", event.Price, event.ChangePct, ts))
	b.WriteString(fmt.Sprintf("**触发**: %s\n", event.Condition))
	if event.Exchange != "" {
		b.WriteString(fmt.Sprintf("\n**交易所**: %s\n", event.Exchange))
	}
	return title, strings.TrimRight(b.String(), "\n")
}

func formatDingTalkResonance(event types.AlertEvent, symbol, severity, ts string) (string, string) {
	data := event.Data
	if data == nil {
		data = map[string]any{}
	}
	dims, _ := data["dimensions"].([]any)
	dimCount := len(dims)
	if n, ok := data["dimension_count"].(int); ok && n > 0 {
		dimCount = n
	}
	score, _ := data["signal_score"].(float64)
	title := fmt.Sprintf("[%s] %s 信号质量=%.0f", severity, symbol, score)
	var b strings.Builder
	b.WriteString(fmt.Sprintf("### %s\n\n", title))
	b.WriteString("**非指令** 仅供人工判断\n\n")
	b.WriteString(fmt.Sprintf("**维度**: %d个 | %s UTC\n\n", dimCount, ts))
	for _, dim := range dims {
		dimStr, _ := dim.(string)
		dimZh := eventNameZh(dimStr)
		dimData, _ := data[dimStr+"_data"].(map[string]any)
		if dimData == nil {
			dimData = map[string]any{}
		}
		switch dimStr {
		case "price_velocity":
			b.WriteString(fmt.Sprintf("- %s: %s%% / %ss\n", dimZh, formatField(dimData, "change_pct", "?"), formatField(dimData, "window_seconds", "?")))
		case "volume_spike":
			b.WriteString(fmt.Sprintf("- %s: %sx\n", dimZh, formatField(dimData, "ratio", "?")))
		case "open_interest_change":
			b.WriteString(fmt.Sprintf("- %s: %s%%\n", dimZh, formatField(dimData, "change_pct", "?")))
		case "funding_rate_anomaly":
			b.WriteString(fmt.Sprintf("- %s: %s\n", dimZh, formatField(dimData, "funding_rate", "?")))
		case "long_short_ratio":
			b.WriteString(fmt.Sprintf("- %s: 多%s%% / 空%s%%\n", dimZh, formatField(dimData, "long_rate", "?"), formatField(dimData, "short_rate", "?")))
		case "liquidation":
			b.WriteString(fmt.Sprintf("- %s: $%sM\n", dimZh, formatField(dimData, "total_liquidation_millions", "?")))
		default:
			b.WriteString(fmt.Sprintf("- %s\n", dimZh))
		}
	}
	return title, strings.TrimRight(b.String(), "\n")
}

func formatDingTalkLiquidation(event types.AlertEvent, symbol, severity, ts string) (string, string) {
	data := event.Data
	if data == nil {
		data = map[string]any{}
	}
	title := fmt.Sprintf("[%s] %s 爆仓异动", severity, symbol)
	zsText := ""
	if _, ok := data["zscore"]; ok {
		zsText = fmt.Sprintf(" | Z=%s", formatField(data, "zscore", "?"))
	}
	reason := fmt.Sprint(data["reason"])
	if reason == "<nil>" {
		reason = "?"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("### %s\n\n", title))
	b.WriteString("**非指令** 仅供人工判断\n\n")
	b.WriteString(fmt.Sprintf("**金额**: $%sM%s | %s UTC\n\n", formatField(data, "total_liquidation_millions", "0"), zsText, ts))
	b.WriteString(fmt.Sprintf("**多/空**: %s%% / %s%%\n\n", formatField(data, "long_liquidation_pct", "50"), formatField(data, "short_liquidation_pct", "50")))
	b.WriteString(fmt.Sprintf("**原因**: %s\n", reason))
	return title, strings.TrimRight(b.String(), "\n")
}

func formatDingTalkLongShort(event types.AlertEvent, symbol, severity, ts string) (string, string) {
	data := event.Data
	if data == nil {
		data = map[string]any{}
	}
	title := fmt.Sprintf("[%s] %s 多空比异动", severity, symbol)
	zsText := ""
	if _, ok := data["zscore"]; ok {
		zsText = fmt.Sprintf(" | Z=%s", formatField(data, "zscore", "?"))
	}
	reason := fmt.Sprint(data["reason"])
	if reason == "<nil>" {
		reason = "?"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("### %s\n\n", title))
	b.WriteString("**非指令** 仅供人工判断\n\n")
	b.WriteString(fmt.Sprintf("**多/空**: %s%% / %s%% (比=%s)%s | %s UTC\n\n",
		formatField(data, "long_rate", "?"), formatField(data, "short_rate", "?"),
		formatField(data, "ls_ratio", "?"), zsText, ts))
	b.WriteString(fmt.Sprintf("**原因**: %s\n", reason))
	return title, strings.TrimRight(b.String(), "\n")
}

func formatDingTalkMarketPulse(event types.AlertEvent, severity, ts string) (string, string) {
	data := event.Data
	if data == nil {
		data = map[string]any{}
	}
	dir := fmt.Sprint(data["direction"])
	if dir == "<nil>" {
		dir = ""
	}
	from := fmt.Sprint(data["state_from"])
	to := fmt.Sprint(data["state_to"])
	med60 := anyFloat(data, "median_return_60s_pct")
	med300 := anyFloat(data, "median_return_300s_pct")
	breadth := anyFloat(data, "breadth")
	adv := anyInt(data, "advancers")
	valid := anyInt(data, "valid_symbols")
	medZ := anyFloat(data, "median_z_60s")
	btc := anyFloatPtr(data, "btc_return_pct")
	eth := anyFloatPtr(data, "eth_return_pct")

	title := marketPulseTitle(event.Event, dir)
	var b strings.Builder
	b.WriteString(fmt.Sprintf("### %s\n\n", title))
	b.WriteString("**非指令** 仅供人工判断，不自动交易。\n\n")
	if from != "<nil>" && to != "<nil>" && from != "" && to != "" {
		b.WriteString(fmt.Sprintf("状态：%s → %s\n\n", from, to))
	}
	switch event.Event {
	case "market_trend":
		b.WriteString(fmt.Sprintf("5分钟市场中位涨幅：%+.2f%%\n\n", med300))
		b.WriteString(fmt.Sprintf("上涨广度：%.0f%%\n\n", breadth*100))
	default:
		label := "1分钟市场中位涨幅"
		if dir == "down" {
			label = "1分钟市场中位跌幅"
		}
		b.WriteString(fmt.Sprintf("%s：%+.2f%%\n\n", label, med60))
		b.WriteString(fmt.Sprintf("广度：%.0f%%（%d / %d）\n\n", breadth*100, adv, valid))
		if medZ != 0 {
			b.WriteString(fmt.Sprintf("标准化强度：%+.2fσ\n\n", medZ))
		}
	}
	if btc != nil {
		b.WriteString(fmt.Sprintf("BTC：%+.2f%%\n\n", *btc))
	}
	if eth != nil {
		b.WriteString(fmt.Sprintf("ETH：%+.2f%%\n\n", *eth))
	}
	leaders := anyStringSlice(data, "leaders")
	if len(leaders) > 0 {
		head := "领涨"
		if dir == "down" {
			head = "领跌"
		}
		if event.Event == "market_trend" {
			head = "强于市场"
		}
		b.WriteString(head + "：\n\n")
		for _, s := range leaders {
			b.WriteString(fmt.Sprintf("- %s\n", shortSymbol(s)))
		}
		b.WriteString("\n")
	}
	switch event.Event {
	case "market_impulse":
		b.WriteString("结论：市场出现同步异动，值得打开盘面观察。\n\n")
	case "market_trend":
		b.WriteString("结论：趋势确认，建议打开盘面观察。\n\n")
	case "market_stress":
		b.WriteString("注意：市场出现系统性快速波动。\n\n")
	case "market_decay":
		b.WriteString("结论：趋势广度衰减，继续盯盘价值下降。\n\n")
	}
	b.WriteString(fmt.Sprintf("%s UTC | [%s]\n", ts, severity))
	return title, strings.TrimRight(b.String(), "\n")
}
