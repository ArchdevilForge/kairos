package engine

import (
	"context"
	"testing"
	"time"

	"github.com/ArchdevilForge/kairos/internal/config"
	"github.com/ArchdevilForge/kairos/internal/exchange"
	"github.com/ArchdevilForge/kairos/internal/types"
)

type startMockExchange struct {
	tickers map[string]*types.Ticker
}

func (m *startMockExchange) Name() string { return "mock" }
func (m *startMockExchange) SubscribeTickers(ctx context.Context, _ []string, _ chan<- types.Ticker) error {
	<-ctx.Done()
	return ctx.Err()
}
func (m *startMockExchange) FetchTickers(context.Context) (map[string]*types.Ticker, error) {
	return m.tickers, nil
}
func (m *startMockExchange) FetchTicker(context.Context, string) (*types.Ticker, error) {
	return nil, nil
}
func (m *startMockExchange) FetchOHLCV(context.Context, string, string, int, int64) ([]types.Candle, error) {
	return nil, nil
}
func (m *startMockExchange) Close() error { return nil }

func TestPipeline_Start_MockExchange(t *testing.T) {
	old := exchangeNew
	defer func() { exchangeNew = old }()

	vol := 2_000_000.0
	mock := &startMockExchange{tickers: map[string]*types.Ticker{
		"BTC/USDT:USDT": {Symbol: "BTC/USDT:USDT", QuoteVolume: &vol},
	}}
	exchangeNew = func(name string) (exchange.Exchange, error) {
		if name == "mock" {
			return mock, nil
		}
		return exchange.New(name)
	}

	cfg, err := config.LoadString(`
dataManager:
  exchanges: [mock]
  topSymbols: 5
  refreshIntervalHours: 999
priceVelocity:
  enabled: true
volumeSpike:
  enabled: true
futuresMetrics:
  enabled: true
  pollIntervalSeconds: 3600
longShortRatio:
  enabled: false
liquidation:
  enabled: false
resonanceScorer:
  enabled: true
  cooldownSeconds: 3600
alertPolicy:
  enabled: false
`)
	if err != nil {
		t.Fatal(err)
	}

	p := NewPipeline(cfg, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err = p.Start(ctx)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Fatalf("Start: %v", err)
	}
	p.Stop()
	p.Close()
}

func TestPipeline_StopClose(t *testing.T) {
	p := NewPipeline(&types.Config{}, nil)
	p.Stop()
	p.Close()
}

func TestRefreshLoop_Cancel(t *testing.T) {
	cfg, _ := config.LoadString(`dataManager:
  refreshIntervalHours: 999`)
	p := NewPipeline(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.refreshLoop(ctx)
}

func TestTelegramDeliverer_Cancel(t *testing.T) {
	p := NewPipeline(&types.Config{}, nil)
	ch := make(chan types.AnomalyEvent)
	close(ch)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.telegramDeliverer(ctx, ch)
}

func TestResonanceDeliverer_Cancel(t *testing.T) {
	cfg, _ := config.LoadString(`resonanceScorer:
  enabled: true`)
	p := NewPipeline(cfg, nil)
	p.resonanceScorer = nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.resonanceDeliverer(ctx)
}

func TestNewLongShortAndLiqDetector_Disabled(t *testing.T) {
	p := NewPipeline(&types.Config{}, nil)
	if p.newLongShortDetector() != nil || p.newLiquidationDetector() != nil {
		t.Fatal("expected nil when disabled")
	}
}

func TestMergeChannels_Empty(t *testing.T) {
	p := NewPipeline(&types.Config{}, nil)
	if ch := p.mergeChannels(context.Background(), nil); ch != nil {
		t.Fatal("expected nil")
	}
}
