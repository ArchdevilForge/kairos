package exchange

import (
	"context"
	"fmt"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// Exchange interface that all exchange adapters implement.
//
// FetchOHLCV cursor contract: beforeMs (Unix milliseconds) is an exclusive
// backward cursor — the call returns up to `limit` of the most recent candles
// strictly older than beforeMs, sorted ascending. beforeMs=0 means "latest".
// All adapters implement the same semantics so callers (backtest pagination)
// can walk from end to start without exchange-specific branches.
type Exchange interface {
	Name() string
	SubscribeTickers(ctx context.Context, symbols []string, tickerCh chan<- types.Ticker) error
	FetchTickers(ctx context.Context) (map[string]*types.Ticker, error)
	FetchOHLCV(ctx context.Context, symbol, timeframe string, limit int, beforeMs int64) ([]types.Candle, error)
	Close() error
}

// FundingEnricher is implemented by exchanges that fetch funding rates lazily.
type FundingEnricher interface {
	EnrichFunding(ctx context.Context, tickers map[string]*types.Ticker, symbols []string)
}

// KlineSubscriber is implemented by exchanges that stream real-time candlesticks.
type KlineSubscriber interface {
	SubscribeKline(ctx context.Context, symbol, tf string, onKline func(candle types.Candle, closed bool)) error
}

// Endpoints carries per-exchange WebSocket endpoint overrides from config
// (config: exchanges.wsEndpoints). An empty field keeps the adapter default, so
// exchanges can migrate endpoints without a code change.
type Endpoints struct {
	OKX string
}

// New creates an exchange by name ("okx", "binance", "bybit") with default endpoints.
func New(name string) (Exchange, error) { return NewWithEndpoints(name, Endpoints{}) }

// NewWithEndpoints creates an exchange by name, applying WS endpoint overrides.
func NewWithEndpoints(name string, eps Endpoints) (Exchange, error) {
	switch name {
	case "okx":
		return newOKXWithEndpoint(eps.OKX), nil
	case "binance":
		return newBinance(), nil
	case "bybit":
		return newBybit(), nil
	default:
		return nil, fmt.Errorf("unknown exchange: %s", name)
	}
}
