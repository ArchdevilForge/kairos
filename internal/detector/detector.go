package detector

import (
	"log/slog"
	"sync"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// BaseDetector provides shared fields (cooldown, logger, event channel).
// There is deliberately no umbrella Detector interface: the pipeline wires
// concrete detectors directly, and a catch-all interface only forced no-op
// methods (and a fake price=0 on metrics events) without compile-time value.
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
