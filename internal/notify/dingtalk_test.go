package notify

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestDingTalkSign(t *testing.T) {
	// Fixed vector: recompute expected with same algorithm.
	ts := "1710000000000"
	secret := "SECtest"
	got, err := dingTalkSign(ts, secret)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(ts + "\n" + secret))
	want := url.QueryEscape(base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	if got != want {
		t.Fatalf("sign=%q want=%q", got, want)
	}
}

func TestDingTalkClient_SendMarkdown(t *testing.T) {
	var gotPath string
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RawQuery
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer srv.Close()

	c, err := NewDingTalkClient(srv.URL+"?access_token=tok", "SECabc")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SendText("hello ding"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotPath, "timestamp=") || !strings.Contains(gotPath, "sign=") {
		t.Fatalf("signed query missing: %s", gotPath)
	}
	if !strings.Contains(gotBody, `"msgtype":"markdown"`) || !strings.Contains(gotBody, "hello ding") {
		t.Fatalf("body=%s", gotBody)
	}
}

func TestFormatDingTalkMarketPulse(t *testing.T) {
	title, body := formatDingTalkEvent(types.AlertEvent{
		Event:     "market_impulse",
		Symbol:    "MARKET",
		Severity:  types.SeverityHigh,
		Timestamp: "2026-07-25T12:00:00Z",
		Data: map[string]any{
			"direction":             "up",
			"state_from":            "QUIET",
			"state_to":              "IMPULSE_UP",
			"median_return_60s_pct": 0.23,
			"breadth":               0.76,
			"advancers":             22,
			"valid_symbols":         29,
			"btc_return_pct":        0.31,
			"leaders":               []string{"SOL/USDT:USDT", "DOGE/USDT:USDT"},
		},
	})
	if !strings.Contains(title, "向上启动") {
		t.Fatalf("title=%s", title)
	}
	for _, need := range []string{"非指令", "0.23", "76%", "BTC", "SOL", "打开盘面"} {
		if !strings.Contains(body, need) {
			t.Fatalf("body missing %q:\n%s", need, body)
		}
	}
	// Must not leak HTML tags from Telegram formatter.
	if strings.Contains(body, "<b>") {
		t.Fatalf("unexpected HTML: %s", body)
	}
}

func TestNewDingTalkClient_RequiresURL(t *testing.T) {
	if _, err := NewDingTalkClient("", ""); err == nil {
		t.Fatal("expected error")
	}
}
