package exchange

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
	"github.com/coder/websocket"
)

func TestBinanceSubscribeTickers_ClosedAndCancelled(t *testing.T) {
	b := newBinance()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.SubscribeTickers(ctx, []string{"BTC/USDT:USDT"}, make(chan types.Ticker)); err == nil {
		t.Fatal("expected ctx error")
	}
	_ = b.Close()
	if err := b.SubscribeTickers(context.Background(), []string{"BTC/USDT:USDT"}, make(chan types.Ticker)); err == nil {
		t.Fatal("expected closed error")
	}
	if err := b.SubscribeTickers(context.Background(), nil, make(chan types.Ticker)); err == nil {
		t.Fatal("expected no symbols error")
	}
}

func TestOKXSubscribeTickers_ClosedAndCancelled(t *testing.T) {
	o := newOKX()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := o.SubscribeTickers(ctx, []string{"BTC/USDT:USDT"}, make(chan types.Ticker)); err == nil {
		t.Fatal("expected ctx error")
	}
	_ = o.Close()
	if err := o.SubscribeTickers(context.Background(), []string{"BTC/USDT:USDT"}, make(chan types.Ticker)); err == nil {
		t.Fatal("expected closed error")
	}
}

func TestBybitSubscribeTickers_ClosedAndCancelled(t *testing.T) {
	b := newBybit()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.SubscribeTickers(ctx, []string{"BTC/USDT:USDT"}, make(chan types.Ticker)); err == nil {
		t.Fatal("expected ctx error")
	}
	_ = b.Close()
	if err := b.SubscribeTickers(context.Background(), []string{"BTC/USDT:USDT"}, make(chan types.Ticker)); err == nil {
		t.Fatal("expected closed error")
	}
}

// OKX retired WS port 8443 (notice code 64010); the default must be 443 and
// the endpoint must be overridable from config.
func TestOKXWSEndpointDefaultAndOverride(t *testing.T) {
	const wantDefault = "wss://ws.okx.com:443/ws/v5/public"
	if got := DefaultOKXWSEndpoint; got != wantDefault {
		t.Fatalf("DefaultOKXWSEndpoint = %q, want %q", got, wantDefault)
	}
	if got := newOKX().wsEndpoint; got != wantDefault {
		t.Fatalf("newOKX().wsEndpoint = %q, want %q", got, wantDefault)
	}
	for _, blank := range []string{"", "   "} {
		if got := newOKXWithEndpoint(blank).wsEndpoint; got != wantDefault {
			t.Fatalf("newOKXWithEndpoint(%q).wsEndpoint = %q, want default", blank, got)
		}
	}

	custom := "wss://ws.example.test:443/ws/v5/public"
	ex, err := NewWithEndpoints("okx", Endpoints{OKX: custom})
	if err != nil {
		t.Fatalf("NewWithEndpoints: %v", err)
	}
	okxEx, ok := ex.(*okxExchange)
	if !ok {
		t.Fatalf("NewWithEndpoints(okx) returned %T", ex)
	}
	if okxEx.wsEndpoint != custom {
		t.Fatalf("configured endpoint = %q, want %q", okxEx.wsEndpoint, custom)
	}

	// Unconfigured adapters keep their own defaults.
	if ex, err := NewWithEndpoints("binance", Endpoints{OKX: custom}); err != nil {
		t.Fatalf("NewWithEndpoints(binance): %v", err)
	} else if _, ok := ex.(*binanceExchange); !ok {
		t.Fatalf("NewWithEndpoints(binance) returned %T", ex)
	}
}

// syncLogBuffer captures slog output for feed-logging assertions.
type syncLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *syncLogBuffer) count(substr string) int {
	return strings.Count(b.String(), substr)
}

func captureSlog(t *testing.T) *syncLogBuffer {
	t.Helper()
	buf := &syncLogBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

func waitForLog(t *testing.T, buf *syncLogBuffer, want string, count int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if buf.count(want) >= count {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d occurrence(s) of %q in log:\n%s", count, want, buf.String())
}

// A flapping feed must not flood the log, but the swallowed repeats still surface.
func TestFeedLog_WarnThrottlesRepeats(t *testing.T) {
	buf := captureSlog(t)
	fl := newFeedLog("okx")
	now := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	fl.now = func() time.Time { return now }
	fl.minGap = time.Minute

	for i := 0; i < 3; i++ {
		fl.Warn("dial", "ws dial failed, retrying", "attempt", i)
	}
	if got := buf.count("ws dial failed"); got != 1 {
		t.Fatalf("expected 1 throttled WARN, got %d:\n%s", got, buf.String())
	}

	now = now.Add(2 * time.Minute)
	fl.Warn("dial", "ws dial failed, retrying", "attempt", 9)
	if got := buf.count("ws dial failed"); got != 2 {
		t.Fatalf("expected a second WARN after minGap, got %d:\n%s", got, buf.String())
	}
	if !strings.Contains(buf.String(), "suppressed_repeats=2") {
		t.Fatalf("swallowed repeats must be reported, got:\n%s", buf.String())
	}
}

// Connect + subscribe must each produce an INFO line carrying the symbol count.
func TestOKXSubscribeTickers_LogsConnectAndSubscribe(t *testing.T) {
	buf := captureSlog(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
		// Wait for the subscribe frame, then keep the socket open until the test ends.
		if _, _, err := conn.Read(r.Context()); err != nil {
			return
		}
		<-r.Context().Done()
	}))
	defer srv.Close()

	o := newOKXWithEndpoint("ws" + strings.TrimPrefix(srv.URL, "http"))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() {
		_ = o.SubscribeTickers(ctx, []string{"BTC/USDT:USDT", "ETH/USDT:USDT"}, make(chan types.Ticker, 8))
	}()

	waitForLog(t, buf, "ws connected", 1, 2*time.Second)
	waitForLog(t, buf, "ws subscribed", 1, 2*time.Second)
	if !strings.Contains(buf.String(), "symbols=2") {
		t.Fatalf("feed logs must carry the symbol count, got:\n%s", buf.String())
	}
}

// A dial failure must be WARN-logged with the error and the backoff, not swallowed.
func TestOKXSubscribeTickers_LogsDialFailure(t *testing.T) {
	buf := captureSlog(t)
	o := newOKXWithEndpoint("ws://127.0.0.1:1/ws/v5/public")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		_ = o.SubscribeTickers(ctx, []string{"BTC/USDT:USDT"}, make(chan types.Ticker, 4))
		close(done)
	}()

	waitForLog(t, buf, "ws dial failed", 1, 1500*time.Millisecond)
	for _, want := range []string{"level=WARN", "error=", "backoff="} {
		if !strings.Contains(buf.String(), want) {
			t.Fatalf("dial WARN must contain %q, got:\n%s", want, buf.String())
		}
	}
	cancel()
	<-done
}

func TestOKXEnrichFunding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "funding-rate") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": "0",
				"data": []map[string]string{{"fundingRate": "0.0003"}},
			})
		}
	}))
	defer srv.Close()
	o := newOKX()
	o.httpCli = testHTTPClient(srv)
	vol := 1.0
	tickers := map[string]*types.Ticker{
		"BTC/USDT:USDT": {Symbol: "BTC/USDT:USDT", LastPrice: &vol},
	}
	o.EnrichFunding(context.Background(), tickers, []string{"BTC/USDT:USDT"})
	if tickers["BTC/USDT:USDT"].FundingRate == nil {
		t.Fatal("expected funding rate")
	}
}

func TestMapTimeframeAndDenormalize(t *testing.T) {
	if mapTimeframe("2h") != "2H" {
		t.Fatal("mapTimeframe")
	}
	if denormalizeSymbol("BTC-USDT-SWAP") != "BTC/USDT:USDT" {
		t.Fatal("denormalize")
	}
	if normalizeSymbol("BTC/USDT") == "" {
		t.Fatal("normalize")
	}
}
