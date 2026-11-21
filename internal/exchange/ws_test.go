package exchange

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
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

func TestBybitWSEndpointDefaultAndOverride(t *testing.T) {
	const wantDefault = "wss://stream.bybit.com/v5/public/linear"
	if got := DefaultBybitWSEndpoint; got != wantDefault {
		t.Fatalf("DefaultBybitWSEndpoint = %q, want %q", got, wantDefault)
	}
	if got := newBybit().wsEndpoint; got != wantDefault {
		t.Fatalf("newBybit().wsEndpoint = %q, want %q", got, wantDefault)
	}
	for _, blank := range []string{"", "   "} {
		if got := newBybitWithEndpoint(blank).wsEndpoint; got != wantDefault {
			t.Fatalf("newBybitWithEndpoint(%q).wsEndpoint = %q, want default", blank, got)
		}
	}

	custom := "wss://stream-testnet.bybit.com/v5/public/linear"
	ex, err := NewWithEndpoints("bybit", Endpoints{Bybit: custom})
	if err != nil {
		t.Fatalf("NewWithEndpoints(bybit): %v", err)
	}
	bybitEx, ok := ex.(*bybitExchange)
	if !ok {
		t.Fatalf("NewWithEndpoints(bybit) returned %T", ex)
	}
	if bybitEx.wsEndpoint != custom {
		t.Fatalf("configured endpoint = %q, want %q", bybitEx.wsEndpoint, custom)
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
		// Wait for the subscribe frame, then push one ticker so feed liveness is
		// observable, and keep the socket open until the test ends.
		if _, _, err := conn.Read(r.Context()); err != nil {
			return
		}
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"arg":{"channel":"tickers"},"data":[{"instId":"BTC-USDT-SWAP","last":"100","volCcy24h":"1","open24h":"99"}]}`))
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

	// The watchdog needs adapter-reported liveness, not consumer guesswork.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		st := o.FeedStats()
		if st.Subscribed && !st.LastMessageAt.IsZero() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("FeedStats must report subscribed + last message, got %+v", o.FeedStats())
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

func TestFeedState_DisconnectClearsSubscription(t *testing.T) {
	var f feedState
	if st := f.stats(); st.Subscribed {
		t.Fatalf("zero feedState must not report subscribed: %+v", st)
	}
	now := time.Now()
	f.markSubscribed(now)
	f.markMessage(now.Add(time.Second))
	st := f.stats()
	if !st.Subscribed || !st.SubscribedAt.Equal(now) || st.LastMessageAt.IsZero() {
		t.Fatalf("unexpected subscribed stats: %+v", st)
	}

	f.markDisconnected()
	st = f.stats()
	if st.Subscribed || !st.SubscribedAt.IsZero() || !st.LastMessageAt.IsZero() {
		t.Fatalf("disconnect must clear subscription+message state, got %+v", st)
	}
}

func TestBinanceSubscribeKline_MockWS(t *testing.T) {
	var connNum int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

		mu.Lock()
		connNum++
		n := connNum
		mu.Unlock()

		if n == 1 {
			_ = conn.Write(r.Context(), websocket.MessageText, []byte(`not-json`))
			_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"e":"aggTrade"}`))
			_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"e":"kline","k":{"t":1704067200000,"o":"65000","h":"65100","l":"64900","c":"65050","v":"12.5","q":"812500","x":false}}`))
			_ = conn.Close(websocket.StatusNormalClosure, "reconnect")
			return
		}
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"e":"kline","k":{"t":1704067200000,"o":"65000","h":"65200","l":"64900","c":"65180","v":"20.0","q":"1303600","x":true}}`))
		<-r.Context().Done()
	}))
	defer srv.Close()

	cleanup := SetBinanceTestHooks(nil, "ws"+strings.TrimPrefix(srv.URL, "http"))
	defer cleanup()

	if err := SubscribeKline(context.Background(), "", "5m", nil); err == nil {
		t.Fatal("expected error for empty symbol")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	type klineEvt struct {
		candle types.Candle
		closed bool
	}
	ch := make(chan klineEvt, 4)
	done := make(chan error, 1)
	b := newBinance()
	go func() {
		done <- b.SubscribeKline(ctx, "BTC/USDT:USDT", "5m", func(c types.Candle, closed bool) {
			ch <- klineEvt{candle: c, closed: closed}
		})
	}()

	var got []klineEvt
	deadline := time.After(4 * time.Second)
	for len(got) < 2 {
		select {
		case evt := <-ch:
			got = append(got, evt)
		case <-deadline:
			t.Fatalf("timed out waiting for 2 kline frames, got %d", len(got))
		}
	}
	cancel()
	<-done

	if got[0].closed || got[0].candle.Timestamp != 1704067200 || got[0].candle.Close != 65050 {
		t.Fatalf("unexpected open kline: %+v", got[0])
	}
	if !got[1].closed || got[1].candle.Close != 65180 || got[1].candle.QuoteVol != 1303600 {
		t.Fatalf("unexpected closed kline: %+v", got[1])
	}
}

func TestBinanceSubscribeTickers_MockWS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"s":"BTCUSDT","c":"65432.1","q":"987654","P":"1.75"}`))
		<-r.Context().Done()
	}))
	defer srv.Close()

	cleanup := SetBinanceTestHooks(nil, "ws"+strings.TrimPrefix(srv.URL, "http"))
	defer cleanup()

	b := newBinance()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	tickerCh := make(chan types.Ticker, 4)
	done := make(chan error, 1)
	go func() {
		done <- b.SubscribeTickers(ctx, []string{"BTC/USDT:USDT"}, tickerCh)
	}()

	select {
	case tk := <-tickerCh:
		if tk.Symbol != "BTC/USDT:USDT" || tk.LastPrice == nil || *tk.LastPrice != 65432.1 {
			t.Fatalf("unexpected ticker: %+v", tk)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for mock Binance ticker")
	}
	if !b.FeedStats().Subscribed {
		t.Fatal("expected FeedStats.Subscribed == true")
	}
	cancel()
	<-done
}

func TestBybitSubscribeTickers_ChunkingHeartbeatAndDeltaMerge(t *testing.T) {
	var (
		mu          sync.Mutex
		subChunks   [][]string
		clientPings int
		pongReplies int
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

		ctx := r.Context()
		// Expect 3 chunked subscribe frames for 23 symbols (10 + 10 + 3).
		for i := 0; i < 3; i++ {
			_, raw, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var sub struct {
				Op   string   `json:"op"`
				Args []string `json:"args"`
			}
			if err := json.Unmarshal(raw, &sub); err != nil || sub.Op != "subscribe" {
				t.Errorf("expected subscribe frame, got %s (err=%v)", raw, err)
				return
			}
			mu.Lock()
			subChunks = append(subChunks, sub.Args)
			mu.Unlock()
			_ = conn.Write(ctx, websocket.MessageText, []byte(`{"success":true,"ret_msg":"","op":"subscribe"}`))
		}

		// Send initial snapshot with all tracked fields.
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{
			"topic":"tickers.BTCUSDT",
			"type":"snapshot",
			"data":{
				"symbol":"BTCUSDT",
				"lastPrice":"65000",
				"turnover24h":"10000000",
				"price24hPcnt":"0.025",
				"openInterest":"1234.5",
				"openInterestValue":"80242500",
				"fundingRate":"0.0001"
			}
		}`))

		// Send sparse delta changing only lastPrice; unchanged fields must persist.
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{
			"topic":"tickers.BTCUSDT",
			"type":"delta",
			"data":{
				"symbol":"BTCUSDT",
				"lastPrice":"65500"
			}
		}`))

		// Send an unsolicited server ping to verify {"op":"pong"} response.
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"op":"ping"}`))

		for {
			_, raw, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var msg struct {
				Op string `json:"op"`
			}
			if json.Unmarshal(raw, &msg) != nil {
				continue
			}
			switch msg.Op {
			case "ping":
				mu.Lock()
				clientPings++
				mu.Unlock()
				// Reply with Bybit's ping+ret_msg=pong shape; client must NOT echo pong back.
				_ = conn.Write(ctx, websocket.MessageText, []byte(`{"success":true,"ret_msg":"pong","op":"ping"}`))
			case "pong":
				mu.Lock()
				pongReplies++
				mu.Unlock()
			}
		}
	}))
	defer srv.Close()

	b := newBybitWithEndpoint("ws" + strings.TrimPrefix(srv.URL, "http"))
	b.pingInterval = 25 * time.Millisecond

	symbols := make([]string, 23)
	symbols[0] = "BTC/USDT:USDT"
	for i := 1; i < 23; i++ {
		symbols[i] = fmt.Sprintf("COIN%02d/USDT:USDT", i)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	tickerCh := make(chan types.Ticker, 8)
	done := make(chan error, 1)
	go func() {
		done <- b.SubscribeTickers(ctx, symbols, tickerCh)
	}()

	var got []types.Ticker
	deadline := time.After(3 * time.Second)
	for len(got) < 2 {
		select {
		case tk := <-tickerCh:
			got = append(got, tk)
		case <-deadline:
			t.Fatalf("timed out waiting for 2 ticker frames, got %d", len(got))
		}
	}

	// Wait briefly for at least one client heartbeat ping and one pong reply to server ping.
	waitDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(waitDeadline) {
		mu.Lock()
		pings, pongs := clientPings, pongReplies
		mu.Unlock()
		if pings >= 1 && pongs == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	if len(subChunks) != 3 || len(subChunks[0]) != 10 || len(subChunks[1]) != 10 || len(subChunks[2]) != 3 {
		t.Fatalf("expected 3 subscribe chunks [10, 10, 3], got %v", subChunks)
	}
	if clientPings < 1 {
		t.Fatalf("expected >=1 client heartbeat ping, got %d", clientPings)
	}
	if pongReplies != 1 {
		t.Fatalf("expected exactly 1 pong reply (no echo on Bybit pong ack), got %d", pongReplies)
	}

	snap, delta := got[0], got[1]
	if snap.Symbol != "BTC/USDT:USDT" || snap.LastPrice == nil || *snap.LastPrice != 65000 {
		t.Fatalf("unexpected snapshot ticker: %+v", snap)
	}
	if delta.LastPrice == nil || *delta.LastPrice != 65500 {
		t.Fatalf("delta LastPrice = %v, want 65500", delta.LastPrice)
	}
	if delta.QuoteVolume == nil || *delta.QuoteVolume != 10000000 {
		t.Fatalf("delta erased QuoteVolume: %v", delta.QuoteVolume)
	}
	if delta.ChangePct == nil || *delta.ChangePct != 2.5 {
		t.Fatalf("delta erased ChangePct: %v", delta.ChangePct)
	}
	if delta.OpenInterest == nil || *delta.OpenInterest != 1234.5 {
		t.Fatalf("delta erased OpenInterest: %v", delta.OpenInterest)
	}
	if delta.FundingRate == nil || *delta.FundingRate != 0.0001 {
		t.Fatalf("delta erased FundingRate: %v", delta.FundingRate)
	}
	if snap.LastPrice == delta.LastPrice || snap.QuoteVolume == delta.QuoteVolume {
		t.Fatal("emitted ticker pointers must be cloned, not aliased")
	}
	if *snap.LastPrice != 65000 {
		t.Fatalf("delta mutated earlier snapshot LastPrice to %v", *snap.LastPrice)
	}
}

func TestBybitSubscribeTickers_SubscribeRejectedReconnects(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

		if _, _, err := conn.Read(r.Context()); err != nil {
			return
		}
		if attempts.Add(1) == 1 {
			_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"success":false,"ret_msg":"handler not found","op":"subscribe"}`))
			<-r.Context().Done()
			return
		}
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"success":true,"ret_msg":"","op":"subscribe"}`))
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"topic":"tickers.BTCUSDT","type":"snapshot","data":{"symbol":"BTCUSDT","lastPrice":"66000","turnover24h":"500000"}}`))
		<-r.Context().Done()
	}))
	defer srv.Close()

	b := newBybitWithEndpoint("ws" + strings.TrimPrefix(srv.URL, "http"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tickerCh := make(chan types.Ticker, 4)
	done := make(chan error, 1)
	go func() {
		done <- b.SubscribeTickers(ctx, []string{"BTC/USDT:USDT"}, tickerCh)
	}()

	select {
	case tk := <-tickerCh:
		if tk.LastPrice == nil || *tk.LastPrice != 66000 {
			t.Fatalf("unexpected ticker after reconnect: %+v", tk)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("timed out waiting for reconnect after subscribe rejection")
	}
	if attempts.Load() < 2 {
		t.Fatalf("expected >=2 attempts, got %d", attempts.Load())
	}
	cancel()
	<-done
}

func TestBybitSubscribeKline_MockWS_AndDispatch(t *testing.T) {
	var connNum atomic.Int32
	var gotTopicMu sync.Mutex
	var gotTopic string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

		_, raw, err := conn.Read(r.Context())
		if err != nil {
			return
		}
		var sub struct {
			Op   string   `json:"op"`
			Args []string `json:"args"`
		}
		if json.Unmarshal(raw, &sub) == nil && len(sub.Args) > 0 {
			gotTopicMu.Lock()
			gotTopic = sub.Args[0]
			gotTopicMu.Unlock()
		}
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"success":true,"ret_msg":"","op":"subscribe"}`))

		if connNum.Add(1) == 1 {
			_ = conn.Write(r.Context(), websocket.MessageText, []byte(`not-json`))
			_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"topic":"tickers.BTCUSDT","data":{}}`))
			_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"topic":"kline.5.BTCUSDT","type":"snapshot","data":[{"start":1704067200000,"end":1704067499999,"interval":"5","open":"65000","high":"65100","low":"64900","close":"65050","volume":"12.5","turnover":"812500","confirm":false,"timestamp":1704067210000}]}`))
			_ = conn.Close(websocket.StatusNormalClosure, "reconnect")
			return
		}
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"topic":"kline.5.BTCUSDT","type":"snapshot","data":[{"start":1704067200000,"end":1704067499999,"interval":"5","open":"65000","high":"65200","low":"64900","close":"65180","volume":"20.0","turnover":"1303600","confirm":true,"timestamp":1704067500000}]}`))
		<-r.Context().Done()
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	cleanup := SetBybitTestHooks(nil, wsURL)
	defer cleanup()

	if err := SubscribeKlineForExchange(context.Background(), "bybit", "", "5m", nil); err == nil {
		t.Fatal("expected error for empty symbol")
	}

	closedBybit := newBybit()
	_ = closedBybit.Close()
	if err := closedBybit.SubscribeKline(context.Background(), "BTC/USDT:USDT", "5m", nil); err == nil {
		t.Fatal("expected error on closed adapter")
	}

	// Verify "" defaults to Bybit via SubscribeKlineForExchange.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	type klineEvt struct {
		candle types.Candle
		closed bool
	}
	ch := make(chan klineEvt, 4)
	done := make(chan error, 1)
	go func() {
		done <- SubscribeKlineForExchange(ctx, "", "BTC/USDT:USDT", "5m", func(c types.Candle, closed bool) {
			ch <- klineEvt{candle: c, closed: closed}
		})
	}()

	var got []klineEvt
	deadline := time.After(4 * time.Second)
	for len(got) < 2 {
		select {
		case evt := <-ch:
			got = append(got, evt)
		case <-deadline:
			t.Fatalf("timed out waiting for 2 Bybit kline frames, got %d", len(got))
		}
	}
	cancel()
	<-done

	gotTopicMu.Lock()
	topic := gotTopic
	gotTopicMu.Unlock()
	if topic != "kline.5.BTCUSDT" {
		t.Fatalf("subscribed topic = %q, want kline.5.BTCUSDT", topic)
	}
	if got[0].closed || got[0].candle.Timestamp != 1704067200 || got[0].candle.Close != 65050 || got[0].candle.QuoteVol != 812500 {
		t.Fatalf("unexpected open Bybit kline: %+v", got[0])
	}
	if !got[1].closed || got[1].candle.Close != 65180 || got[1].candle.QuoteVol != 1303600 {
		t.Fatalf("unexpected closed Bybit kline: %+v", got[1])
	}

	// Verify SubscribeKlineForExchange("binance", ...) dispatches to Binance WS.
	binSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"e":"kline","k":{"t":1704067800000,"o":"100","h":"105","l":"99","c":"104","v":"5","q":"520","x":true}}`))
		<-r.Context().Done()
	}))
	defer binSrv.Close()
	binCleanup := SetBinanceTestHooks(nil, "ws"+strings.TrimPrefix(binSrv.URL, "http"))
	defer binCleanup()

	ctx2, cancel2 := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel2()
	binCh := make(chan klineEvt, 1)
	binDone := make(chan error, 1)
	go func() {
		binDone <- SubscribeKlineForExchange(ctx2, "binance", "ETH/USDT:USDT", "5m", func(c types.Candle, closed bool) {
			binCh <- klineEvt{candle: c, closed: closed}
		})
	}()
	select {
	case evt := <-binCh:
		if !evt.closed || evt.candle.Timestamp != 1704067800 || evt.candle.Close != 104 {
			t.Fatalf("unexpected dispatched Binance kline: %+v", evt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for dispatched Binance kline")
	}
	cancel2()
	<-binDone
}
