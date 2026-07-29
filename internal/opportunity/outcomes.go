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
	Interval time.Duration
	// MaxAge: skip tickets older than this since CreatedAt/SignalAt.
	MaxAge   time.Duration
	BarLimit int
	Timeout  time.Duration
	// CostPct round-trip fee+slip as fraction (e.g. 0.001 = 10bps).
	CostPct float64
}

// DefaultOutcomeTrackConfig returns stage-1 defaults.
func DefaultOutcomeTrackConfig() OutcomeTrackConfig {
	return OutcomeTrackConfig{
		Interval: 2 * time.Minute,
		MaxAge:   6 * time.Hour,
		BarLimit: 120,
		Timeout:  20 * time.Second,
		CostPct:  0.001, // ~10 bps round-trip placeholder; override from real fees
	}
}

// TrackOutcomes refreshes counterfactual rows using bars at/after ticket signal time.
func (s *Service) TrackOutcomes(ctx context.Context, fetch OHLCVFetcher, cfg OutcomeTrackConfig) (updated int, err error) {
	if s == nil || s.journal == nil || fetch == nil {
		return 0, nil
	}
	if cfg.BarLimit <= 0 {
		cfg.BarLimit = 120
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
	now := time.Now()
	nowUnix := now.Unix()
	hz := evaluation.DefaultHorizons()

	for _, t := range tickets {
		if t.ID == "" || t.Symbol == "" {
			continue
		}
		startUnix := t.EntryTriggeredAt
		if startUnix <= 0 {
			startUnix = t.SignalAt
		}
		if startUnix <= 0 {
			startUnix = t.CreatedAt
		}
		if startUnix <= 0 {
			s.log.Debug("outcome skip: no ticket timestamp", "ticket", t.ID)
			continue
		}
		age := now.Sub(time.Unix(startUnix, 0))
		if age < 0 {
			age = 0
		}
		if age > cfg.MaxAge {
			continue // MaxAge enforced
		}

		// per-ticket timeout slice of parent ctx
		fetchCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
		candles, err := fetch.FetchOHLCV(fetchCtx, t.Symbol, "5m", cfg.BarLimit, 0)
		cancel()
		if err != nil || len(candles) < 2 {
			continue
		}
		// closed only
		if len(candles) > 2 {
			candles = candles[:len(candles)-1]
		}

		// Forward path STRICTLY after trigger bar close (no same-bar lookahead).
		var bars []evaluation.Bar
		for _, c := range candles {
			ts := c.Timestamp
			if ts > 1_000_000_000_000 {
				ts = ts / 1000
			}
			if ts > startUnix {
				bars = append(bars, evaluation.Bar{
					TS: ts, Open: c.Open, High: c.High, Low: c.Low, Close: c.Close,
				})
			}
		}
		if len(bars) == 0 {
			// no forward bars yet
			continue
		}

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

		entry := t.RiskPlan.EntryPrice
		stop := t.RiskPlan.StopPrice
		if entry <= 0 {
			entry = bars[0].Close
		}
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
			TicketID:     t.ID,
			SessionID:    t.SessionID,
			Symbol:       t.Symbol,
			Direction:    t.Direction,
			Decision:     dec,
			Bars:         bars,
			Entry:        entry,
			Stop:         stop,
			Target:       target,
			CostPct:      cfg.CostPct,
			TimeExitBars: hz.H1, // fixed 1h mechanical horizon on 5m
		}, hz)
		o.AsOfUnix = nowUnix

		if prev, ok, _ := s.journal.GetOutcome(t.ID); ok {
			if prev.MFE == o.MFE && prev.MAE == o.MAE && prev.MechanicalR == o.MechanicalR &&
				prev.NetR == o.NetR && prev.Complete == o.Complete {
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

// RunOutcomeLoop blocks until ctx done.
func (s *Service) RunOutcomeLoop(ctx context.Context, fetch OHLCVFetcher, cfg OutcomeTrackConfig) {
	if s == nil || fetch == nil {
		return
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 2 * time.Minute
	}
	t := time.NewTicker(cfg.Interval)
	defer t.Stop()
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

var _ = storage.CounterfactualSchemaVersion
