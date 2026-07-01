package backtest

import (
	"testing"

	"github.com/ArchdevilForge/kairos/internal/scanner"
	"github.com/ArchdevilForge/kairos/internal/types"
)

// TestCycleStrategyGate documents that winter long candidates are withheld in scanner scoring.
func TestCycleStrategyGate(t *testing.T) {
	s := scanner.NewMarketScanner(&types.Config{})
	state, w := s.ApplyStrategyActionGate(string(types.ActionStateTradeCandidate), types.DirectionLong, "winter", "down")
	if state != string(types.ActionStatePrepare) {
		t.Fatalf("winter long gate: %s", state)
	}
	if len(w) == 0 {
		t.Fatal("expected warning")
	}
}
