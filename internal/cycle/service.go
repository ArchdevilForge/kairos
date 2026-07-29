package cycle

import (
	"sync"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// Service owns a long-lived Classifier so hysteresis persists across enrich calls.
type Service struct {
	mu         sync.Mutex
	classifier *Classifier
}

// NewService returns a production cycle service with default transition policy.
func NewService() *Service {
	return &Service{classifier: NewClassifier(DefaultTransitionPolicy())}
}

// Map classifies multi-TF series for one symbol, reusing hysteresis state.
// asOf should be the last closed bar unix time (not wall clock).
// legacy may be empty when unknown — never invent summer.
func (s *Service) Map(symbol string, asOf int64, legacy types.MarketPhase, series []Series) types.CycleMap {
	if s == nil {
		s = NewService()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.classifier == nil {
		s.classifier = NewClassifier(DefaultTransitionPolicy())
	}
	if asOf <= 0 {
		asOf = lastClosedUnix(series)
	}
	return s.classifier.ClassifyMap(symbol, asOf, legacy, series)
}

// MapStateless is for one-shot CLI/debug without hysteresis (explicit).
func MapStateless(symbol string, asOf int64, legacy types.MarketPhase, series []Series) types.CycleMap {
	if asOf <= 0 {
		asOf = lastClosedUnix(series)
	}
	c := NewClassifier(DefaultTransitionPolicy())
	return c.ClassifyMap(symbol, asOf, legacy, series)
}

func lastClosedUnix(series []Series) int64 {
	// Series currently carry no timestamps on closes; callers should pass asOf.
	// Keep 0 so BuildMap stores 0 rather than wall-clock lies.
	return 0
}
