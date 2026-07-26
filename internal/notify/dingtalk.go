package notify

import (
	"bytes"
	"context"
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
func (d *DingTalkClient) SendText(ctx context.Context, text string) error {
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
	return d.sendMarkdown(ctx, title, text)
}

// SendEvent formats an AlertEvent as DingTalk markdown and sends it.
func (d *DingTalkClient) SendEvent(ctx context.Context, event types.AlertEvent) error {
	title, body := formatDingTalkEvent(event)
	return d.sendMarkdown(ctx, title, body)
}

func (d *DingTalkClient) sendMarkdown(ctx context.Context, title, text string) error {
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
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

// dingTalkSign returns base64(hmac_sha256(secret, timestamp + "\n" + secret)).
// The DingTalk contract is that the *query parameter* is URL-encoded exactly
// once; signedURL builds the query via url.Values.Encode, which performs that
// encoding, so the raw base64 value must be returned here (escaping it again
// would double-encode and the server would reject the signature).
func dingTalkSign(timestamp, secret string) (string, error) {
	stringToSign := timestamp + "\n" + secret
	mac := hmac.New(sha256.New, []byte(secret))
	if _, err := mac.Write([]byte(stringToSign)); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
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
	b.WriteString("**非指令** " + manualFooter + "\n\n")
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
	dims := resonanceDimensions(data)
	dimCount := len(dims)
	if n := anyInt(data, "dimension_count"); n > 0 {
		dimCount = n
	}
	score := anyFloat(data, "signal_score")
	title := fmt.Sprintf("[%s] %s 信号质量=%.0f", severity, symbol, score)
	var b strings.Builder
	b.WriteString(fmt.Sprintf("### %s\n\n", title))
	b.WriteString("**非指令** " + manualFooter + "\n\n")
	b.WriteString(fmt.Sprintf("**维度**: %d个 | %s UTC\n\n", dimCount, ts))
	for _, dimStr := range dims {
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
	zsText := optionalZ(data)
	reason := fmt.Sprint(data["reason"])
	if reason == "<nil>" {
		reason = "?"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("### %s\n\n", title))
	b.WriteString("**非指令** " + manualFooter + "\n\n")
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
	zsText := optionalZ(data)
	reason := fmt.Sprint(data["reason"])
	if reason == "<nil>" {
		reason = "?"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("### %s\n\n", title))
	b.WriteString("**非指令** " + manualFooter + "\n\n")
	b.WriteString(fmt.Sprintf("**多/空**: %s%% / %s%% (比=%s)%s | %s UTC\n\n",
		formatField(data, "long_rate", "?"), formatField(data, "short_rate", "?"),
		formatField(data, "ls_ratio", "?"), zsText, ts))
	b.WriteString(fmt.Sprintf("**原因**: %s\n", reason))
	return title, strings.TrimRight(b.String(), "\n")
}

func formatDingTalkMarketPulse(event types.AlertEvent, severity, ts string) (string, string) {
	v := parseMarketPulse(event)

	var b strings.Builder
	b.WriteString(fmt.Sprintf("### %s\n\n", v.Title))
	b.WriteString("**非指令** " + manualFooter + "\n\n")
	if v.From != "" && v.To != "" {
		b.WriteString(fmt.Sprintf("状态：%s → %s\n\n", v.From, v.To))
	}
	switch event.Event {
	case "market_trend":
		b.WriteString(fmt.Sprintf("%s：%+.2f%%\n\n", v.TrendLabel, v.Med300))
		b.WriteString(fmt.Sprintf("%s：%.0f%%\n\n", v.BreadthLabel, v.Breadth*100))
	default:
		b.WriteString(fmt.Sprintf("%s：%+.2f%%\n\n", v.Med60Label, v.Med60))
		b.WriteString(fmt.Sprintf("广度：%.0f%%（%d / %d）\n\n", v.Breadth*100, v.Count, v.Valid))
		if v.MedZ != 0 {
			b.WriteString(fmt.Sprintf("标准化强度：%+.2fσ\n\n", v.MedZ))
		}
	}
	if v.BTC != nil {
		b.WriteString(fmt.Sprintf("BTC：%+.2f%%\n\n", *v.BTC))
	}
	if v.ETH != nil {
		b.WriteString(fmt.Sprintf("ETH：%+.2f%%\n\n", *v.ETH))
	}
	if len(v.Movers) > 0 {
		b.WriteString(v.MoversHead + "：\n\n")
		for _, s := range v.Movers {
			b.WriteString(fmt.Sprintf("- %s\n", shortSymbol(s)))
		}
		b.WriteString("\n")
	}
	if v.Conclusion != "" {
		b.WriteString(v.Conclusion + "\n\n")
	}
	b.WriteString(fmt.Sprintf("%s UTC | [%s]\n", ts, severity))
	return v.Title, strings.TrimRight(b.String(), "\n")
}
