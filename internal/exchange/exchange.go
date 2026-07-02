package exchange

import (
	"context"
	"fmt"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// Exchange interface that all exchange adapters implement.
type Exchange interface {
	Name() string
	SubscribeTickers(ctx context.Context, symbols []string, tickerCh chan<- types.Ticker) error
	FetchTickers(ctx context.Context) (map[string]*types.Ticker, error)
	FetchTicker(ctx context.Context, symbol string) (*types.Ticker, error)
	FetchOHLCV(ctx context.Context, symbol, timeframe string, limit int, sinceMs int64) ([]types.Candle, error)
	Close() error
}

// FundingEnricher is implemented by exchanges that fetch funding rates lazily.
type FundingEnricher interface {
	EnrichFunding(ctx context.Context, tickers map[string]*types.Ticker, symbols []string)
}

// New creates an exchange by name ("okx", "binance", "bybit").
func New(name string) (Exchange, error) {
	switch name {
	case "okx":
		return newOKX(), nil
	case "binance":
		return newBinance(), nil
	case "bybit":
		return newBybit(), nil
	default:
		return nil, fmt.Errorf("unknown exchange: %s", name)
	}
}
