package detector

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// Detector is the interface all anomaly detectors implement.
type Detector interface {
	Name() string
	OnTicker(ctx context.Context, ticker types.Ticker)
	OnMetricsUpdate(ctx context.Context, symbol string, oi float64, fundingRate float64)
	OnLSSnapshot(symbol string, longRate, shortRate float64)
	OnLiquidationSnapshot(symbol string, totalUSD, longUSD, shortUSD float64)
	Events() <-chan types.AnomalyEvent
	Reset()
}

// BaseDetector provides shared fields (cooldown, logger, event channel).
type BaseDetector struct {
	mu        sync.RWMutex
	Logger    *slog.Logger
	events    chan types.AnomalyEvent
	Cooldowns map[string]time.Time
	cdMu      sync.Mutex
}

// NewEvent creates an AnomalyEvent with proper fields.
func NewEvent(symbol, eventType, severity string, data map[string]any) types.AnomalyEvent {
	return types.AnomalyEvent{
		Symbol:    symbol,
		EventType: eventType,
		Severity:  types.Severity(severity),
		Data:      data,
		Timestamp: float64(time.Now().UnixMilli()) / 1000,
	}
}
