// Package evaluation tracks counterfactual outcomes and simple EV attribution.
package evaluation

import (
	"math"

	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
)

// HorizonBars maps named horizons to bar counts on the series timeframe.
type HorizonBars struct {
	M5  int
	M15 int
	H1  int
	H4  int
}

// DefaultHorizons assumes 5m bars.
func DefaultHorizons() HorizonBars {
	return HorizonBars{M5: 1, M15: 3, H1: 12, H4: 48}
}

// PathInput is a closed-bar price path for one ticket.
type PathInput struct {
	TicketID  string
	SessionID string
	Symbol    string
	Direction types.CycleDirection
	Decision  types.HumanDecision

	// Closes are forward path starting at entry bar (index 0 = entry close).
	Closes []float64
	Entry  float64
	Stop   float64
	Target float64 // 0 = no target
}

// ComputeOutcome walks the path for MFE/MAE/horizon returns and stop/target order.
func ComputeOutcome(in PathInput, hz HorizonBars) storage.CounterfactualOutcome {
	o := storage.CounterfactualOutcome{
		SchemaVersion: storage.CounterfactualSchemaVersion,
		TicketID:      in.TicketID,
		SessionID:     in.SessionID,
		Symbol:        in.Symbol,
		Direction:     in.Direction,
		Decision:      in.Decision,
	}
	if in.Entry <= 0 || len(in.Closes) == 0 {
		return o
	}
	sign := 1.0
	if in.Direction == types.CycleDirectionDown {
		sign = -1
	}

	mfe, mae := 0.0, 0.0
	stopHit, targetHit := false, false
	stopFirst, targetFirst := false, false
	maxR := 0.0
	risk := math.Abs(in.Entry - in.Stop)
	if risk <= 0 {
		risk = in.Entry * 0.01 // ponytail: 1% fallback if stop missing
	}

	for i, px := range in.Closes {
		pnlPct := sign * (px - in.Entry) / in.Entry * 100
		if pnlPct > mfe {
			mfe = pnlPct
		}
		if pnlPct < mae {
			mae = pnlPct
		}
		rMultiple := sign * (px - in.Entry) / risk
		if rMultiple > maxR {
			maxR = rMultiple
		}
		if !stopHit && in.Stop > 0 {
			if in.Direction == types.CycleDirectionUp && px <= in.Stop {
				stopHit = true
				if !targetHit {
					stopFirst = true
				}
			}
			if in.Direction == types.CycleDirectionDown && px >= in.Stop {
				stopHit = true
				if !targetHit {
					stopFirst = true
				}
			}
		}
		if !targetHit && in.Target > 0 {
			if in.Direction == types.CycleDirectionUp && px >= in.Target {
				targetHit = true
				if !stopHit {
					targetFirst = true
				}
			}
			if in.Direction == types.CycleDirectionDown && px <= in.Target {
				targetHit = true
				if !stopHit {
					targetFirst = true
				}
			}
		}
		_ = i
	}

	setRet := func(bars int) *float64 {
		if bars <= 0 || len(in.Closes) <= bars {
			if bars > 0 && len(in.Closes) > 0 {
				// use last available
				bars = len(in.Closes) - 1
			} else {
				return nil
			}
		}
		px := in.Closes[bars]
		v := sign * (px - in.Entry) / in.Entry * 100
		v = math.Round(v*10000) / 10000
		return &v
	}

	o.Return5m = setRet(hz.M5)
	o.Return15m = setRet(hz.M15)
	o.Return1h = setRet(hz.H1)
	o.Return4h = setRet(hz.H4)
	o.MFE = math.Round(mfe*10000) / 10000
	o.MAE = math.Round(mae*10000) / 10000
	o.StopHitFirst = stopFirst
	o.TargetHitFirst = targetFirst
	o.MaxRealizableR = math.Round(maxR*100) / 100
	return o
}
