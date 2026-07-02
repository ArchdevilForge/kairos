package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ArchdevilForge/kairos/internal/config"
	"github.com/ArchdevilForge/kairos/internal/detector"
	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestPassesAlertPolicy_AllTypes(t *testing.T) {
	cfg, err := config.LoadString(`
alertPolicy:
  enabled: true
  allowedEventTypes: [price_velocity, volume_spike]
  minSeverity: MEDIUM
  minPriceChangePct: 1.0
  minVolumeRatio: 3.0
  minOpenInterestChangePct: 4.0
  minFundingRateAbs: 0.0005
  minFundingRateChangeAbs: 0.0003
`)
	if err != nil {
		t.Fatal(err)
	}
	p := NewPipeline(cfg, nil)

	low := types.AnomalyEvent{EventType: "price_velocity", Severity: types.SeverityLow, Data: map[string]any{"change_pct": 2.0}}
	if p.passesAlertPolicy(low) {
		t.Fatal("severity too low")
	}
	disallowed := types.AnomalyEvent{EventType: "open_interest_change", Severity: types.SeverityHigh, Data: map[string]any{"change_pct": 10.0}}
	if p.passesAlertPolicy(disallowed) {
		t.Fatal("event type not allowed")
	}
	pv := types.AnomalyEvent{EventType: "price_velocity", Severity: types.SeverityHigh, Data: map[string]any{"change_pct": 0.5}}
	if p.passesAlertPolicy(pv) {
		t.Fatal("price change too small")
	}
	vs := types.AnomalyEvent{EventType: "volume_spike", Severity: types.SeverityHigh, Data: map[string]any{"ratio": 2.0}}
	if p.passesAlertPolicy(vs) {
		t.Fatal("volume ratio too small")
	}
	oi := types.AnomalyEvent{EventType: "open_interest_change", Severity: types.SeverityHigh, Data: map[string]any{"change_pct": 2.0}}
	p.allowedEventTypes = nil
	if p.passesAlertPolicy(oi) {
		t.Fatal("oi change too small")
	}
	fr := types.AnomalyEvent{EventType: "funding_rate_anomaly", Severity: types.SeverityHigh, Data: map[string]any{"funding_rate": 0.0001, "change_abs": 0.0001}}
	if p.passesAlertPolicy(fr) {
		t.Fatal("funding too small")
	}
	ok := types.AnomalyEvent{EventType: "price_velocity", Severity: types.SeverityHigh, Data: map[string]any{"change_pct": 2.0}}
	if !p.passesAlertPolicy(ok) {
		t.Fatal("should pass")
	}
}

func TestBuildAlertAndCondition(t *testing.T) {
	p := NewPipeline(&types.Config{}, nil)
	evt := types.AnomalyEvent{
		EventType: "price_velocity",
		Symbol:    "BTC/USDT:USDT",
		Severity:  types.SeverityHigh,
		Data: map[string]any{
			"price_to": 65000, "change_pct": 1.5,
			"window_seconds": 30, "threshold": 0.5,
		},
	}
	alert := p.buildAlert(evt)
	if alert.Price != 65000 || alert.Condition == "" {
		t.Fatalf("buildAlert: %+v", alert)
	}

	for _, tc := range []struct {
		typ  string
		data map[string]any
	}{
		{"volume_spike", map[string]any{"ratio": 4, "window_minutes": 10}},
		{"funding_rate_anomaly", map[string]any{"funding_rate": 0.001, "change_abs": 0.0005, "reason": "spike"}},
		{"unknown_type", map[string]any{}},
	} {
		cond := buildCondition(types.AnomalyEvent{EventType: tc.typ, Data: tc.data})
		if cond == "" {
			t.Fatalf("empty condition for %s", tc.typ)
		}
	}
}

func TestFloatFromMap_AllTypes(t *testing.T) {
	m := map[string]any{
		"f64": float64(1.5), "i": 2, "i64": int64(3), "u64": uint64(4),
		"u": uint(5), "i32": int32(6), "u32": uint32(7), "f32": float32(8),
		"u8": uint8(9), "i8": int8(10), "t": true, "bad": "x",
	}
	for _, key := range []string{"f64", "i", "i64", "u64", "u", "i32", "u32", "f32", "u8", "i8", "t"} {
		if _, ok := floatFromMap(m, key); !ok {
			t.Fatalf("missing %s", key)
		}
	}
	if v, ok := floatFromMap(m, "t"); !ok || v != 1 {
		t.Fatalf("bool true: %v %v", v, ok)
	}
	if _, ok := floatFromMap(m, "bad"); ok {
		t.Fatal("bad type")
	}
}

func TestExtractAvgLongRate(t *testing.T) {
	payload := []any{
		map[string]any{
			"list": []any{
				map[string]any{"longVolUsd": 700.0, "shortVolUsd": 300.0},
			},
		},
	}
	rate := extractAvgLongRate(payload)
	if rate == nil || *rate != 70 {
		t.Fatalf("rate: %v", rate)
	}
	if extractAvgLongRate("not-list") != nil {
		t.Fatal("invalid payload")
	}
}

func TestTotalSymbols(t *testing.T) {
	p := NewPipeline(&types.Config{}, nil)
	p.symbolsByExchange = map[string][]string{"okx": {"A", "B"}, "binance": {"C"}}
	if p.totalSymbols() != 3 {
		t.Fatalf("total: %d", p.totalSymbols())
	}
}

func TestDiscoverSymbols_MockExchange(t *testing.T) {
	vol := 1_000_000.0
	ex := &mockPipelineExchange{tickers: map[string]*types.Ticker{
		"BTC/USDT:USDT": {Symbol: "BTC/USDT:USDT", QuoteVolume: &vol},
		"ETH/USDT:USDT": {Symbol: "ETH/USDT:USDT", QuoteVolume: &vol},
		"SPOT/USDT":     {Symbol: "SPOT/USDT", QuoteVolume: &vol},
	}}
	p := NewPipeline(&types.Config{}, nil)

	symbols, err := p.discoverSymbols(context.Background(), ex, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(symbols) != 2 {
		t.Fatalf("symbols: %v", symbols)
	}
}

func TestDeliverEvent_BlacklistAndDedup(t *testing.T) {
	home := t.TempDir()
	cfgDir := filepath.Join(home, ".config", "kairos")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "blacklist.txt"), []byte("BTC/USDT:USDT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	p := NewPipeline(&types.Config{}, nil)
	p.deliverEvent(context.Background(), types.AnomalyEvent{Symbol: "BTC/USDT:USDT", EventType: "price_velocity"})

	p.dedupWindowSeconds = 3600
	p.symbolCooldownSeconds = 3600
	evt := types.AnomalyEvent{
		Symbol: "ETH/USDT:USDT", EventType: "price_velocity",
		Severity: types.SeverityHigh,
		Data:     map[string]any{"change_pct": 2.0, "price": 100},
	}
	p.deliverEvent(context.Background(), evt)
	p.deliverEvent(context.Background(), evt)
}

func TestMergeChannelsAndEventAggregator(t *testing.T) {
	p := NewPipeline(&types.Config{ResonanceScorer: types.ResonanceScorerConfig{Enabled: true}}, nil)
	p.resonanceScorer = detector.NewResonanceScorer(p.cfg.ResonanceScorer)

	ch1 := make(chan types.AnomalyEvent, 1)
	ch2 := make(chan types.AnomalyEvent, 1)
	ch1 <- types.AnomalyEvent{Symbol: "A", EventType: "price_velocity", Timestamp: float64(time.Now().Unix())}
	ch2 <- types.AnomalyEvent{Symbol: "B", EventType: "volume_spike", Timestamp: float64(time.Now().Unix())}
	close(ch1)
	close(ch2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	merged := p.mergeChannels(ctx, []<-chan types.AnomalyEvent{ch1, ch2})
	count := 0
	for range merged {
		count++
	}
	if count != 2 {
		t.Fatalf("merged count: %d", count)
	}

	delivery := make(chan types.AnomalyEvent, 2)
	src := make(chan types.AnomalyEvent, 1)
	src <- types.AnomalyEvent{Symbol: "BTC/USDT:USDT", EventType: "price_velocity", Timestamp: float64(time.Now().Unix()), Data: map[string]any{"change_pct": 2.0, "zscore": 3.0}}
	close(src)
	go p.eventAggregator(ctx, []<-chan types.AnomalyEvent{src}, delivery)
	select {
	case <-delivery:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for delivery")
	}
}

func TestConsumeTickersAndPollMetrics(t *testing.T) {
	cfg, _ := config.LoadString(`
priceVelocity:
  enabled: true
volumeSpike:
  enabled: true
futuresMetrics:
  enabled: true
  pollIntervalSeconds: 3600
`)
	p := NewPipeline(cfg, nil)
	vol := 100.0
	oi := 1000.0
	fr := 0.0001
	ex := &mockPipelineExchange{tickers: map[string]*types.Ticker{
		"BTC/USDT:USDT": {Symbol: "BTC/USDT:USDT", QuoteVolume: &vol, OpenInterest: &oi, FundingRate: &fr, LastPrice: &vol},
	}}
	es := &exchangeState{name: "okx", ex: ex, symbols: []string{"BTC/USDT:USDT"}, tickerCh: make(chan types.Ticker, 1)}
	p.registerDetectors(es)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	es.tickerCh <- types.Ticker{Symbol: "BTC/USDT:USDT", LastPrice: &vol, QuoteVolume: &vol}
	close(es.tickerCh)
	p.consumeTickers(ctx, es)

	if err := p.pollMetrics(ctx, es); err != nil {
		t.Fatal(err)
	}
}

func TestPollLongShortAndLiquidations_NoSymbols(t *testing.T) {
	p := NewPipeline(&types.Config{
		LongShortRatio: types.LongShortRatioConfig{Enabled: true},
		Liquidation:    types.LiquidationConfig{Enabled: true},
	}, nil)
	p.longShortDet = p.newLongShortDetector()
	p.liqDet = p.newLiquidationDetector()
	p.pollLongShort(context.Background())
	p.pollLiquidations(context.Background())
}

func TestRegisterDetectors_Disabled(t *testing.T) {
	p := NewPipeline(&types.Config{}, nil)
	es := &exchangeState{name: "okx"}
	p.registerDetectors(es)
	if es.velocity != nil || es.spike != nil || es.metrics != nil {
		t.Fatal("detectors should stay nil when disabled")
	}
}

type mockPipelineExchange struct {
	tickers map[string]*types.Ticker
}

func (m *mockPipelineExchange) Name() string { return "mock" }
func (m *mockPipelineExchange) SubscribeTickers(context.Context, []string, chan<- types.Ticker) error {
	return nil
}
func (m *mockPipelineExchange) FetchTickers(context.Context) (map[string]*types.Ticker, error) {
	return m.tickers, nil
}
func (m *mockPipelineExchange) FetchTicker(context.Context, string) (*types.Ticker, error) {
	return nil, nil
}
func (m *mockPipelineExchange) FetchOHLCV(context.Context, string, string, int, int64) ([]types.Candle, error) {
	return nil, nil
}
func (m *mockPipelineExchange) Close() error { return nil }
