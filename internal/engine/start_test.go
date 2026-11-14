package engine

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ArchdevilForge/kairos/internal/cache"
	"github.com/ArchdevilForge/kairos/internal/config"
	"github.com/ArchdevilForge/kairos/internal/exchange"
	"github.com/ArchdevilForge/kairos/internal/types"
)

type startMockExchange struct {
	tickers map[string]*types.Ticker
	candles []types.Candle
}

func (m *startMockExchange) Name() string { return "mock" }
func (m *startMockExchange) SubscribeTickers(ctx context.Context, _ []string, _ chan<- types.Ticker) error {
	<-ctx.Done()
	return ctx.Err()
}
func (m *startMockExchange) FetchTickers(context.Context) (map[string]*types.Ticker, error) {
	return m.tickers, nil
}
func (m *startMockExchange) FetchOHLCV(context.Context, string, string, int, int64) ([]types.Candle, error) {
	return m.candles, nil
}
func (m *startMockExchange) Close() error { return nil }

func TestPipeline_Start_MockExchange(t *testing.T) {
	old := exchangeNew
	defer func() { exchangeNew = old }()

	vol := 2_000_000.0
	mock := &startMockExchange{tickers: map[string]*types.Ticker{
		"BTC/USDT:USDT": {Symbol: "BTC/USDT:USDT", QuoteVolume: &vol},
	}}
	exchangeNew = func(name string, eps exchange.Endpoints) (exchange.Exchange, error) {
		if name == "mock" {
			return mock, nil
		}
		return exchange.NewWithEndpoints(name, eps)
	}

	cfg, err := config.LoadString(`
dataManager:
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
	// "mock" is not a real adapter, so it is injected after validation.
	cfg.Exchanges.Primary = "mock"
	cfg.DataManager.Exchanges = []string{"mock"}

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
	cfg, err := config.LoadString(`resonanceScorer:
  enabled: true`)
	if err != nil {
		t.Fatal(err)
	}
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

func TestPipeline_DeskHelpers_And_CatchUpAndBackfill(t *testing.T) {
	tmp := t.TempDir()
	now := time.Now().Unix()
	var bars []types.Candle
	for i := 0; i < 45; i++ {
		ts := now - int64(45-i)*300
		bars = append(bars, types.Candle{
			Timestamp: ts,
			Open:      100 + float64(i)*0.1,
			High:      101 + float64(i)*0.1,
			Low:       99 + float64(i)*0.1,
			Close:     100.5 + float64(i)*0.1,
			Volume:    1000,
			QuoteVol:  100000,
		})
	}
	mock := &startMockExchange{candles: bars}

	cfg := &types.Config{}
	cfg.Exchanges.Primary = "mock"
	cfg.DataManager.Exchanges = []string{"mock"}
	cfg.Storage.DatabasePath = filepath.Join(tmp, "kairos.db")
	cfg.Storage.WatchHintsPath = filepath.Join(tmp, "hints.jsonl")
	cfg.Notice.Enabled = true

	p := NewPipeline(cfg, nil)
	p.exchanges["mock"] = mock
	p.symbolsByExchange["mock"] = []string{"BTC/USDT:USDT", "ETH/USDT:USDT"}

	if err := p.EnsureDeskStorage(); err != nil {
		t.Fatal(err)
	}
	store, err := cache.OpenFromStorageConfig(cfg.Storage)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	p.SetCacheStore(store)

	var (
		gotTickers int
		gotPulses  int
		gotAnom    int
		gotNotices []types.Notice
	)
	p.SetDesktopObserver(DesktopObserver{
		OnTicker: func(_ string, _ types.Ticker) { gotTickers++ },
		OnPulse:  func(_ MarketPulseView) { gotPulses++ },
		OnAnomaly: func(_ types.AnomalyEvent) {
			gotAnom++
		},
		OnNotice: func(n types.Notice) {
			gotNotices = append(gotNotices, n)
		},
	})

	if p.PrimaryExchange() != "mock" || p.PrimaryExchangeClient() != mock {
		t.Fatalf("primary exchange mismatch: %q %v", p.PrimaryExchange(), p.PrimaryExchangeClient())
	}
	if len(p.TrackedSymbols()) != 2 || p.Journal() == nil || p.NoticeJournal() == nil {
		t.Fatalf("expected tracked symbols & journals initialized")
	}

	b, ml, tz := 80.0, 8.0, "UTC"
	p.UpdateRiskGateConfig(&b, &ml, &tz)
	if got := p.Config().RiskGate; got.TradingBankrollUSD != 80 || got.MaxLossUSD != 8 || got.Timezone != "UTC" {
		t.Fatalf("UpdateRiskGateConfig failed: %+v", got)
	}

	px := 65000.0
	p.emitDesktopTicker("mock", types.Ticker{Symbol: "BTC/USDT:USDT", LastPrice: &px})
	p.emitDesktopPulse()
	p.recordAndEmitAnomaly(types.AnomalyEvent{
		Symbol:    "BTC/USDT:USDT",
		EventType: "price_velocity",
		Severity:  types.SeverityHigh,
		Timestamp: float64(now),
	})
	if gotTickers != 1 || gotPulses != 1 || gotAnom != 1 {
		t.Fatalf("observer counts: ticker=%d pulse=%d anom=%d", gotTickers, gotPulses, gotAnom)
	}
	if rec := p.RecentAnomalies(5); len(rec) != 1 || rec[0].Symbol != "BTC/USDT:USDT" {
		t.Fatalf("RecentAnomalies: %+v", rec)
	}

	// seedSeenNotices + emitNewNotices
	_ = p.NoticeJournal().SaveNotice(types.Notice{
		SchemaVersion:  types.NoticeSchemaVersion,
		NoticeID:       "nt-old",
		Symbol:         "BTC/USDT:USDT",
		DeliveryStatus: types.NoticeStatusSent,
		CreatedAt:      time.Now().Add(-time.Minute),
	})
	p.seedSeenNotices()
	_ = p.NoticeJournal().SaveNotice(types.Notice{
		SchemaVersion:  types.NoticeSchemaVersion,
		NoticeID:       "nt-suppressed",
		Symbol:         "BTC/USDT:USDT",
		DeliveryStatus: types.NoticeStatusSuppressed,
		CreatedAt:      time.Now(),
	})
	_ = p.NoticeJournal().SaveNotice(types.Notice{
		SchemaVersion:  types.NoticeSchemaVersion,
		NoticeID:       "nt-new",
		Symbol:         "BTC/USDT:USDT",
		DeliveryStatus: types.NoticeStatusSent,
		CreatedAt:      time.Now(),
	})
	p.emitNewNotices()
	if len(gotNotices) != 1 || gotNotices[0].NoticeID != "nt-new" {
		t.Fatalf("expected only nt-new emitted, got %+v", gotNotices)
	}

	// CatchUpAndBackfill populates klines for open position + active ticket + BTC.
	_ = store.ReplaceOpenPositions(context.Background(), []cache.OpenPosition{
		{Symbol: "ETH/USDT:USDT", PositionSide: "BOTH", MarginMode: "isolated", Leverage: 5, Qty: 1},
	})
	_ = p.Journal().SaveTicket(types.DecisionTicket{
		SchemaVersion: types.DecisionTicketSchemaVersion,
		ID:            "tkt-backfill-1",
		Symbol:        "SOL/USDT:USDT",
		Status:        types.TicketStatusOpen,
	})
	if err := p.CatchUpAndBackfill(context.Background()); err != nil {
		t.Fatal(err)
	}
	klines, err := store.GetKlines(context.Background(), "BTC/USDT:USDT", "5m", 100, 0)
	if err != nil || len(klines) == 0 {
		t.Fatalf("expected backfilled BTC klines, got len=%d err=%v", len(klines), err)
	}

	// Notice hook & policy helpers
	p.initNoticeService()
	p.deliverNotice(context.Background(), types.AnomalyEvent{
		Symbol:    "BTC/USDT:USDT",
		EventType: "volume_spike",
		Exchange:  "mock",
		Severity:  types.SeverityHigh,
		Timestamp: float64(now),
	})
	p.recordWatchHint("BTC/USDT:USDT", "volume_spike", "mock")
	if p.isMarketPulseActive() {
		t.Fatal("expected inactive market pulse with no detector")
	}
	_ = p.passesMarketEventPolicy(types.AnomalyEvent{Symbol: "MARKET", EventType: "market_impulse", Severity: types.SeverityHigh})
	_ = p.SeasonRegime()
	_ = p.MarketPulseView()
	_ = p.shouldDeliverNotice(types.AnomalyEvent{Symbol: "BTC/USDT:USDT", EventType: "volume_spike"})
	_ = fetchClosedCandles(context.Background(), mock, "BTC/USDT:USDT", "4h", 32)
}
