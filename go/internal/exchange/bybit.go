package exchange

import (
	"context"
	"fmt"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// Bybit exchange — stub for Phase B.
type bybitExchange struct{}

func newBybit() *bybitExchange { return &bybitExchange{} }

func (b *bybitExchange) Name() string { return "bybit" }

func (b *bybitExchange) SubscribeTickers(ctx context.Context, symbols []string, tickerCh chan<- types.Ticker) error {
	return fmt.Errorf("bybit: not implemented (Phase B)")
}

func (b *bybitExchange) FetchTickers(ctx context.Context) (map[string]*types.Ticker, error) {
	return nil, fmt.Errorf("bybit: not implemented (Phase B)")
}

func (b *bybitExchange) FetchOHLCV(ctx context.Context, symbol, timeframe string, limit int, sinceMs int64) ([]types.Candle, error) {
	return nil, fmt.Errorf("bybit: not implemented (Phase B)")
}

func (b *bybitExchange) Close() error { return nil }
