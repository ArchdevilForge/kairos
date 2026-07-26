package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ArchdevilForge/kairos/internal/detector"
	"github.com/ArchdevilForge/kairos/internal/notify"
	"github.com/ArchdevilForge/kairos/internal/types"
)

// dingTestServer is a controllable DingTalk endpoint: fail=true → HTTP 500.
type dingTestServer struct {
	srv      *httptest.Server
	fail     atomic.Bool
	requests atomic.Int32
	lastBody atomic.Value
}

func newDingTestServer() *dingTestServer {
	d := &dingTestServer{}
	d.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.requests.Add(1)
		buf := make([]byte, 8192)
		n, _ := r.Body.Read(buf)
		d.lastBody.Store(string(buf[:n]))
		if d.fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	return d
}

func newDeliveryPipeline(t *testing.T, cfg *types.Config) (*Pipeline, *dingTestServer) {
	t.Helper()
	ding := newDingTestServer()
	t.Cleanup(ding.srv.Close)
	client, err := notify.NewDingTalkClient(ding.srv.URL+"?access_token=t", "")
	if err != nil {
		t.Fatal(err)
	}
	p := NewPipeline(cfg, nil)
	p.hints = nil // avoid writing watch-hints.jsonl into the repo during tests
	p.SetDingTalk(client)
	return p, ding
}

func highVelocityEvent(symbol string) types.AnomalyEvent {
	return annotateEvent(types.AnomalyEvent{
		Symbol:    symbol,
		EventType: "price_velocity",
		Severity:  types.SeverityHigh,
		Data:      map[string]any{"change_pct": 2.0, "price_to": 100.0, "window_seconds": 60, "threshold": 0.9},
	}, "okx")
}

// The long symbol cooldown must commit only after a successful delivery: a
// transient channel outage must not suppress the next event for 45 minutes.
func TestDeliverEvent_CooldownCommitsOnlyOnSuccess(t *testing.T) {
	p, ding := newDeliveryPipeline(t, &types.Config{})
	p.dedupWindowSeconds = 0 // isolate cooldown semantics
	p.symbolCooldownSeconds = 3600

	ctx := context.Background()

	// 1st attempt: channel down → delivered=false → no cooldown commit.
	ding.fail.Store(true)
	p.deliverEvent(ctx, highVelocityEvent("ETH/USDT:USDT"))
	if got := ding.requests.Load(); got != 1 {
		t.Fatalf("first attempt requests=%d", got)
	}

	// 2nd attempt: channel back → must NOT be cooldown-gated, must deliver.
	ding.fail.Store(false)
	p.deliverEvent(ctx, highVelocityEvent("ETH/USDT:USDT"))
	if got := ding.requests.Load(); got != 2 {
		t.Fatalf("recovered attempt must reach the channel, requests=%d", got)
	}

	// 3rd attempt: now the cooldown is committed → gated, no request.
	p.deliverEvent(ctx, highVelocityEvent("ETH/USDT:USDT"))
	if got := ding.requests.Load(); got != 2 {
		t.Fatalf("cooldown after success must gate, requests=%d", got)
	}
}

// The short dedup window commits on every attempt so a flapping channel is
// rate-limited even before any success.
func TestDeliverEvent_DedupWindowCommitsOnAttempt(t *testing.T) {
	p, ding := newDeliveryPipeline(t, &types.Config{})
	p.dedupWindowSeconds = 3600
	p.symbolCooldownSeconds = 7200

	ding.fail.Store(true)
	ctx := context.Background()
	p.deliverEvent(ctx, highVelocityEvent("ETH/USDT:USDT"))
	p.deliverEvent(ctx, highVelocityEvent("ETH/USDT:USDT"))
	if got := ding.requests.Load(); got != 1 {
		t.Fatalf("dedup window must rate-limit failing retries, requests=%d", got)
	}
}

// Market events bypass the generic per-symbol cooldown: MarketPulse has its
// own direction/state cooldowns, and an up-impulse must not silence a
// legitimate down-impulse minutes later.
func TestDeliverEvent_MarketEventsBypassSymbolCooldown(t *testing.T) {
	p, ding := newDeliveryPipeline(t, &types.Config{})
	p.dedupWindowSeconds = 0
	p.symbolCooldownSeconds = 3600

	ctx := context.Background()
	up := annotateEvent(types.AnomalyEvent{
		Symbol: "MARKET", EventType: "market_impulse", Severity: types.SeverityHigh,
		Data: map[string]any{"direction": "up", "state_from": "QUIET", "state_to": "IMPULSE_UP"},
	}, "okx")
	down := annotateEvent(types.AnomalyEvent{
		Symbol: "MARKET", EventType: "market_impulse", Severity: types.SeverityHigh,
		Data: map[string]any{"direction": "down", "state_from": "DECAY", "state_to": "IMPULSE_DOWN"},
	}, "okx")

	p.deliverEvent(ctx, up)
	p.deliverEvent(ctx, down)
	if got := ding.requests.Load(); got != 2 {
		t.Fatalf("reversal within cooldown must still deliver, requests=%d", got)
	}
}

// Resonance is delivered through the same policy gate as everything else: an
// allow-list without "resonance" must block it.
func TestResonance_GoesThroughAlertPolicy(t *testing.T) {
	blockCfg := &types.Config{
		AlertPolicy: types.AlertPolicyConfig{
			Enabled:           true,
			AllowedEventTypes: []string{"price_velocity"},
			MinSeverity:       "LOW",
		},
		ResonanceScorer: types.ResonanceScorerConfig{Enabled: true, MinScore: 10},
	}
	p, ding := newDeliveryPipeline(t, blockCfg)
	re := detector.ResonanceEvent{
		Symbol: "SOL/USDT:USDT", SignalScore: 80, DimensionCount: 2,
		Dimensions: map[string]types.AnomalyEvent{
			"price_velocity": {EventType: "price_velocity", Data: map[string]any{"change_pct": 1.2}},
			"volume_spike":   {EventType: "volume_spike", Data: map[string]any{"ratio": 4.0}},
		},
	}
	p.sendResonanceAlert(context.Background(), re)
	if got := ding.requests.Load(); got != 0 {
		t.Fatalf("resonance outside allow-list must be gated, requests=%d", got)
	}

	allowCfg := &types.Config{
		AlertPolicy: types.AlertPolicyConfig{
			Enabled:           true,
			AllowedEventTypes: []string{"resonance"},
			MinSeverity:       "LOW",
		},
		ResonanceScorer: types.ResonanceScorerConfig{Enabled: true, MinScore: 10},
	}
	p2, ding2 := newDeliveryPipeline(t, allowCfg)
	p2.sendResonanceAlert(context.Background(), re)
	if got := ding2.requests.Load(); got != 1 {
		t.Fatalf("allowed resonance must deliver, requests=%d", got)
	}
	body, _ := ding2.lastBody.Load().(string)
	if !strings.Contains(body, "信号质量") {
		t.Fatalf("resonance body: %s", body)
	}
}

// buildAlert must preserve provenance end to end.
func TestBuildAlert_CarriesTimestampExchangeEventID(t *testing.T) {
	p := NewPipeline(&types.Config{}, nil)
	evt := annotateEvent(types.AnomalyEvent{
		Symbol:    "BTC/USDT:USDT",
		EventType: "price_velocity",
		Severity:  types.SeverityHigh,
		Timestamp: 1753500000,
		Data:      map[string]any{"change_pct": 1.5, "price_to": 65000.0},
	}, "bybit")

	alert := p.buildAlert(evt)
	if alert.Exchange != "bybit" {
		t.Fatalf("exchange lost: %+v", alert)
	}
	if alert.EventID == "" || alert.EventID != evt.EventID {
		t.Fatalf("event id lost: %+v", alert)
	}
	want := time.Unix(1753500000, 0).UTC().Format(time.RFC3339)
	if alert.Timestamp != want {
		t.Fatalf("timestamp = %q, want %q", alert.Timestamp, want)
	}

	// A second annotate of the same event yields the same deterministic id.
	again := annotateEvent(types.AnomalyEvent{
		Symbol:    "BTC/USDT:USDT",
		EventType: "price_velocity",
		Severity:  types.SeverityHigh,
		Timestamp: 1753500000,
		Data:      map[string]any{"change_pct": 1.5, "price_to": 65000.0},
	}, "bybit")
	if again.EventID != evt.EventID {
		t.Fatalf("event id not deterministic: %s vs %s", again.EventID, evt.EventID)
	}
}

// MARKET-level events and outcomes must not become resonance dimensions.
func TestIsResonanceInput_ExcludesMarketAndOutcome(t *testing.T) {
	for _, typ := range []string{"market_impulse", "market_trend", "market_stress", "market_decay", "market_outcome", "resonance"} {
		if isResonanceInput(typ) {
			t.Fatalf("%s must not feed the resonance scorer", typ)
		}
	}
	for _, typ := range []string{"price_velocity", "volume_spike", "open_interest_change", "funding_rate_anomaly", "long_short_ratio", "liquidation"} {
		if !isResonanceInput(typ) {
			t.Fatalf("%s must feed the resonance scorer", typ)
		}
	}
}
