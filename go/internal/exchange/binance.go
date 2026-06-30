package exchange

import (
	"context"
	"fmt"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// Binance exchange — stub for Phase B.
type binanceExchange struct{}

func newBinance() *binanceExchange { return &binanceExchange{} }

func (b *binanceExchange) Name() string { return "binance" }

func (b *binanceExchange) SubscribeTickers(ctx context.Context, symbols []string, tickerCh chan<- types.Ticker) error {
	return fmt.Errorf("binance: not implemented (Phase B)")
}

func (b *binanceExchange) FetchTickers(ctx context.Context) (map[string]*types.Ticker, error) {
	return nil, fmt.Errorf("binance: not implemented (Phase B)")
}

func (b *binanceExchange) FetchOHLCV(ctx context.Context, symbol, timeframe string, limit int, sinceMs int64) ([]types.Candle, error) {
	return nil, fmt.Errorf("binance: not implemented (Phase B)")
}

func (b *binanceExchange) Close() error { return nil }
