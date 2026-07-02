package scanner

import (
	"context"
	"testing"

	"github.com/ArchdevilForge/kairos/internal/data"
	"github.com/ArchdevilForge/kairos/internal/exchange"
	"github.com/ArchdevilForge/kairos/internal/types"
)

type mockScanExchange struct {
	tickers      map[string]*types.Ticker
	fundingCalls int
}

func (m *mockScanExchange) Name() string { return "mock" }

func (m *mockScanExchange) SubscribeTickers(context.Context, []string, chan<- types.Ticker) error {
	return nil
}

func (m *mockScanExchange) FetchTickers(context.Context) (map[string]*types.Ticker, error) {
	return m.tickers, nil
}

func (m *mockScanExchange) FetchTicker(context.Context, string) (*types.Ticker, error) {
	return nil, nil
}

func (m *mockScanExchange) FetchOHLCV(context.Context, string, string, int, int64) ([]types.Candle, error) {
	return syntheticOHLCV(60), nil
}

func (m *mockScanExchange) Close() error { return nil }

func (m *mockScanExchange) EnrichFunding(_ context.Context, _ map[string]*types.Ticker, symbols []string) {
	m.fundingCalls = len(symbols)
}

func syntheticOHLCV(n int) []types.Candle {
	out := make([]types.Candle, n)
	for i := range out {
		out[i] = types.Candle{
			Timestamp: int64(1_700_000_000 + i*3600),
			Open:      100,
			High:      101,
			Low:       99,
			Close:     100.5,
			Volume:    1000,
		}
	}
	return out
}

func testScannerConfig() *types.Config {
	return &types.Config{
		Scanner: types.ScannerConfig{
			UniverseSize:      2,
			CandidateLimit:    2,
			DeepAnalysisLimit: 1,
			Timeframes:        []string{"1d", "4h", "15m"},
		},
		Scoring: types.ScoringConfig{
			CandidateWeights: map[string]float64{
				"quoteVolume": 4.0,
				"rsiHotness":  1.0,
			},
			MinimumLiquidityQuoteVolume: 1,
		},
		Exchanges: types.ExchangesConfig{Primary: "mock"},
	}
}

func ptrF(v float64) *float64 { return &v }

func TestScoreCandidate_RSIHotness(t *testing.T) {
	s := NewMarketScanner(testScannerConfig())
	rsiMap := map[string]data.CoinRSI{
		"BTC": {RSI4h: 78},
		"ETH": {RSI4h: 48},
	}
	hot := s.scoreCandidate("BTC/USDT:USDT", "mock", &types.Ticker{
		QuoteVolume: ptrF(50_000_000),
		ChangePct:   ptrF(3),
	}, nil, 0, rsiMap)
	neutral := s.scoreCandidate("ETH/USDT:USDT", "mock", &types.Ticker{
		QuoteVolume: ptrF(50_000_000),
		ChangePct:   ptrF(3),
	}, nil, 0, rsiMap)
	if hot.CandidateScore <= neutral.CandidateScore {
		t.Fatalf("hot rsi should outrank neutral: hot=%v neutral=%v", hot.CandidateScore, neutral.CandidateScore)
	}
}

func TestDiscoverCandidates_FundingEnrichTopN(t *testing.T) {
	mock := &mockScanExchange{
		tickers: map[string]*types.Ticker{
			"AAA/USDT:USDT": {QuoteVolume: ptrF(300), Info: map[string]any{"instType": "SWAP"}},
			"BBB/USDT:USDT": {QuoteVolume: ptrF(200), Info: map[string]any{"instType": "SWAP"}},
			"CCC/USDT:USDT": {QuoteVolume: ptrF(100), Info: map[string]any{"instType": "SWAP"}},
		},
	}
	s := NewMarketScanner(testScannerConfig())
	cands, universe, _ := s.discoverCandidates(context.Background(), mock, "mock", nil)
	if universe != 3 || len(cands) != 2 {
		t.Fatalf("universe=%d candidates=%d", universe, len(cands))
	}
	if mock.fundingCalls != 2 {
		t.Fatalf("expected funding enrich for top 2, got %d", mock.fundingCalls)
	}
}

func TestScanMarket_MockExchange(t *testing.T) {
	mock := &mockScanExchange{
		tickers: map[string]*types.Ticker{
			"BTC/USDT:USDT": {QuoteVolume: ptrF(500), ChangePct: ptrF(1), Info: map[string]any{"instType": "SWAP"}},
			"ETH/USDT:USDT": {QuoteVolume: ptrF(400), ChangePct: ptrF(2), Info: map[string]any{"instType": "SWAP"}},
		},
	}
	s := NewMarketScanner(testScannerConfig())
	s.exchangeFactory = func(string) (exchange.Exchange, error) { return mock, nil }
	s.rsiLoader = func(context.Context) (map[string]data.CoinRSI, string) {
		return map[string]data.CoinRSI{"BTC": {RSI4h: 72}, "ETH": {RSI4h: 40}}, ""
	}

	env := s.ScanMarket(context.Background(), "mock")
	if !env.Success {
		t.Fatalf("scan failed: %v", env.Errors)
	}
	cands, ok := env.Data["candidates"].([]map[string]any)
	if !ok || len(cands) == 0 {
		t.Fatalf("expected candidates in envelope, data=%v", env.Data)
	}
}
