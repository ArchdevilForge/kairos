package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ArchdevilForge/kairos/internal/config"
	"github.com/ArchdevilForge/kairos/internal/exchange"
	"github.com/ArchdevilForge/kairos/internal/types"
)

// refreshMockExchange serves a mutable ticker set and records every WS
// subscription's symbol list.
type refreshMockExchange struct {
	mu      sync.Mutex
	tickers map[string]*types.Ticker
	subs    [][]string
}

func newRefreshMock(symbols ...string) *refreshMockExchange {
	m := &refreshMockExchange{tickers: map[string]*types.Ticker{}}
	m.setUniverse(symbols...)
	return m
}

func (m *refreshMockExchange) setUniverse(symbols ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tickers = map[string]*types.Ticker{}
	vol := 5_000_000.0
	for _, s := range symbols {
		v := vol
		m.tickers[s] = &types.Ticker{Symbol: s, QuoteVolume: &v}
		vol -= 1000 // keep deterministic volume ordering
	}
}

func (m *refreshMockExchange) subscriptions() [][]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([][]string, len(m.subs))
	copy(out, m.subs)
	return out
}

func (m *refreshMockExchange) Name() string { return "mock" }

func (m *refreshMockExchange) SubscribeTickers(ctx context.Context, symbols []string, _ chan<- types.Ticker) error {
	m.mu.Lock()
	cp := make([]string, len(symbols))
	copy(cp, symbols)
	m.subs = append(m.subs, cp)
	m.mu.Unlock()
	<-ctx.Done()
	return ctx.Err()
}

func (m *refreshMockExchange) FetchTickers(context.Context) (map[string]*types.Ticker, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]*types.Ticker, len(m.tickers))
	for k, v := range m.tickers {
		out[k] = v
	}
	return out, nil
}

func (m *refreshMockExchange) FetchTicker(context.Context, string) (*types.Ticker, error) {
	return nil, nil
}

func (m *refreshMockExchange) FetchOHLCV(context.Context, string, string, int, int64) ([]types.Candle, error) {
	return nil, nil
}

func (m *refreshMockExchange) Close() error { return nil }

func refreshTestConfig(t *testing.T) *types.Config {
	t.Helper()
	cfg, err := config.LoadString(`
dataManager:
  topSymbols: 5
  refreshIntervalHours: 999
marketPulse:
  enabled: true
  shadowMode: true
`)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Exchanges.Primary = "mock"
	cfg.DataManager.Exchanges = []string{"mock"}
	return cfg
}

// After a refresh detects a universe change, the WS feed must be
// resubscribed with the new list and MarketPulse must follow.
func TestRefresh_ResubscribesWSWithNewUniverse(t *testing.T) {
	old := exchangeNew
	defer func() { exchangeNew = old }()
	mock := newRefreshMock("AAA/USDT:USDT", "BBB/USDT:USDT")
	exchangeNew = func(name string) (exchange.Exchange, error) { return mock, nil }

	cfg := refreshTestConfig(t)
	p := NewPipeline(cfg, nil)
	p.refreshInterval = 25 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Start(ctx) }()

	waitFor := func(cond func() bool, msg string) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if cond() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("timeout waiting for %s", msg)
	}

	waitFor(func() bool { return len(mock.subscriptions()) >= 1 }, "initial subscription")

	// Change the universe: drop BBB, add CCC.
	mock.setUniverse("AAA/USDT:USDT", "CCC/USDT:USDT")

	waitFor(func() bool { return len(mock.subscriptions()) >= 2 }, "resubscription after refresh")

	subs := mock.subscriptions()
	last := subs[len(subs)-1]
	found := map[string]bool{}
	for _, s := range last {
		found[s] = true
	}
	if !found["CCC/USDT:USDT"] || found["BBB/USDT:USDT"] {
		t.Fatalf("resubscription must use the new universe, got %v", last)
	}

	// The published snapshot must match too.
	snap := p.symbolsSnapshot("mock")
	snapSet := map[string]bool{}
	for _, s := range snap {
		snapSet[s] = true
	}
	if !snapSet["CCC/USDT:USDT"] || snapSet["BBB/USDT:USDT"] {
		t.Fatalf("snapshot not updated: %v", snap)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("pipeline did not stop")
	}
	p.Close()
}

// Concurrent refresh writes and poller reads must be race-free (run under
// -race). This exercises the exact overlap that used to crash: refreshLoop
// writing symbolsByExchange while CoinGlass pollers iterate it.
func TestRefresh_ConcurrentSnapshotReadsAreRaceFree(t *testing.T) {
	old := exchangeNew
	defer func() { exchangeNew = old }()
	mock := newRefreshMock("AAA/USDT:USDT", "BBB/USDT:USDT")
	exchangeNew = func(name string) (exchange.Exchange, error) { return mock, nil }

	cfg := refreshTestConfig(t)
	p := NewPipeline(cfg, nil)
	p.refreshInterval = 5 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Start(ctx) }()

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = p.coinglassSymbols()
					_ = p.allSymbolsSnapshot()
					_ = p.totalSymbols()
				}
			}
		}()
	}

	// Keep mutating the universe so refreshes publish new snapshots.
	for i := 0; i < 20; i++ {
		if i%2 == 0 {
			mock.setUniverse("AAA/USDT:USDT", "CCC/USDT:USDT")
		} else {
			mock.setUniverse("AAA/USDT:USDT", "BBB/USDT:USDT")
		}
		time.Sleep(5 * time.Millisecond)
	}

	close(stop)
	wg.Wait()
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("pipeline did not stop")
	}
	p.Close()
}
