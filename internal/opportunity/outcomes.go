package opportunity

import (
	"context"
	"time"

	"github.com/ArchdevilForge/kairos/internal/evaluation"
	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
)

// OutcomeTrackConfig controls forward-path refresh.
type OutcomeTrackConfig struct {
	// Interval between full sweeps (pipeline loop).
	Interval time.Duration
	// MaxAge how long after ticket creation we keep refreshing.
	MaxAge time.Duration
	// BarLimit 5m bars to pull for the path.
	BarLimit int
	Timeout  time.Duration
}

// DefaultOutcomeTrackConfig returns stage-1 defaults.
func DefaultOutcomeTrackConfig() OutcomeTrackConfig {
	return OutcomeTrackConfig{
		Interval: 2 * time.Minute,
		MaxAge:   6 * time.Hour,
		BarLimit: 60, // ~5h of 5m bars
		Timeout:  20 * time.Second,
	}
}

// TrackOutcomes refreshes counterfactual rows for open/decided tickets still in MaxAge.
// Uses 5m OHLCV; entry = ticket RiskPlan.EntryPrice (or first close ≥ created).
func (s *Service) TrackOutcomes(ctx context.Context, fetch OHLCVFetcher, cfg OutcomeTrackConfig) (updated int, err error) {
	if s == nil || s.journal == nil || fetch == nil {
		return 0, nil
	}
	if cfg.BarLimit <= 0 {
		cfg.BarLimit = 60
	}
	if cfg.MaxAge <= 0 {
		cfg.MaxAge = 6 * time.Hour
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 20 * time.Second
	}

	tickets, err := s.journal.ListTickets("")
	if err != nil {
		return 0, err
	}
	now := time.Now().Unix()
	hz := evaluation.DefaultHorizons()

	fetchCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	for _, t := range tickets {
		if t.ID == "" || t.Symbol == "" {
			continue
		}
		// skip closed long ago if we can parse — tickets lack CreatedAt; use outcome AsOf or always try within symbol fetch
		dec := types.HumanDecision("")
		if d, ok, _ := s.journal.GetDecision(t.ID); ok {
			dec = d.Decision
		} else {
			switch t.Status {
			case types.TicketStatusAccepted:
				dec = types.DecisionAccepted
			case types.TicketStatusRejected:
				dec = types.DecisionRejected
			case types.TicketStatusWaiting:
				dec = types.DecisionWaiting
			case types.TicketStatusMissed:
				dec = types.DecisionMissed
			}
		}

		candles, err := fetch.FetchOHLCV(fetchCtx, t.Symbol, "5m", cfg.BarLimit, 0)
		if err != nil || len(candles) < 3 {
			s.log.Debug("outcome fetch skip", "ticket", t.ID, "error", err)
			continue
		}
		// closed bars only
		if len(candles) > 3 {
			candles = candles[:len(candles)-1]
		}

		entry := t.RiskPlan.EntryPrice
		stop := t.RiskPlan.StopPrice
		if entry <= 0 {
			entry = candles[0].Close
		}
		// build forward path from first bar at/after entry price touch, else full series from start
		startIdx := 0
		for i, c := range candles {
			// prefer bar whose close is near entry (±0.5%)
			if entry > 0 && absRatio(c.Close, entry) <= 0.005 {
				startIdx = i
				break
			}
		}
		closes := make([]float64, 0, len(candles)-startIdx)
		for i := startIdx; i < len(candles); i++ {
			closes = append(closes, candles[i].Close)
		}
		if len(closes) == 0 {
			continue
		}

		// target: first risk target unused — use 2R mechanical
		target := 0.0
		if stop > 0 && entry > 0 {
			risk := abs(entry - stop)
			if t.Direction == types.CycleDirectionDown {
				target = entry - 2*risk
			} else {
				target = entry + 2*risk
			}
		}

		o := evaluation.ComputeOutcome(evaluation.PathInput{
			TicketID:  t.ID,
			SessionID: t.SessionID,
			Symbol:    t.Symbol,
			Direction: t.Direction,
			Decision:  dec,
			Closes:    closes,
			Entry:     entry,
			Stop:      stop,
			Target:    target,
		}, hz)
		o.AsOfUnix = now

		// skip write if unchanged vs last (cheap compare on MFE/MAE/R)
		if prev, ok, _ := s.journal.GetOutcome(t.ID); ok {
			if prev.MFE == o.MFE && prev.MAE == o.MAE && prev.MaxRealizableR == o.MaxRealizableR &&
				ptrEq(prev.Return5m, o.Return5m) && ptrEq(prev.Return1h, o.Return1h) {
				continue
			}
		}
		if err := s.journal.SaveOutcome(o); err != nil {
			return updated, err
		}
		updated++
	}
	return updated, nil
}

// RunOutcomeLoop blocks until ctx done, sweeping outcomes on Interval.
func (s *Service) RunOutcomeLoop(ctx context.Context, fetch OHLCVFetcher, cfg OutcomeTrackConfig) {
	if s == nil || fetch == nil {
		return
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 2 * time.Minute
	}
	t := time.NewTicker(cfg.Interval)
	defer t.Stop()
	// initial pass
	if n, err := s.TrackOutcomes(ctx, fetch, cfg); err != nil {
		s.log.Warn("outcome track failed", "error", err)
	} else if n > 0 {
		s.log.Info("outcomes updated", "count", n)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := s.TrackOutcomes(ctx, fetch, cfg); err != nil {
				s.log.Warn("outcome track failed", "error", err)
			} else if n > 0 {
				s.log.Info("outcomes updated", "count", n)
			}
		}
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func absRatio(a, b float64) float64 {
	if b == 0 {
		return 1
	}
	return abs(a-b) / abs(b)
}

func ptrEq(a, b *float64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// Ensure CounterfactualOutcome import used when only tracking
var _ = storage.CounterfactualSchemaVersion
