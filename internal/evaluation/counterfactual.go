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

// Bar is one OHLCV step for path accounting (high/low matter).
type Bar struct {
	TS    int64
	Open  float64
	High  float64
	Low   float64
	Close float64
}

// PathInput is a forward path for one ticket starting at/after signal.
type PathInput struct {
	TicketID  string
	SessionID string
	Symbol    string
	Direction types.CycleDirection
	Decision  types.HumanDecision

	Bars   []Bar
	Entry  float64
	Stop   float64
	Target float64 // 0 = no target; use time exit only

	// CostR is estimated round-trip cost in R units (fees+slippage).
	CostR float64
}

// ComputeOutcome walks high/low path for MFE/MAE, stop/target order, MechanicalR, NetR.
func ComputeOutcome(in PathInput, hz HorizonBars) storage.CounterfactualOutcome {
	o := storage.CounterfactualOutcome{
		SchemaVersion: storage.CounterfactualSchemaVersion,
		TicketID:      in.TicketID,
		SessionID:     in.SessionID,
		Symbol:        in.Symbol,
		Direction:     in.Direction,
		Decision:      in.Decision,
	}
	if in.Entry <= 0 || len(in.Bars) == 0 {
		return o
	}
	sign := 1.0
	if in.Direction == types.CycleDirectionDown {
		sign = -1
	}
	risk := math.Abs(in.Entry - in.Stop)
	if risk <= 0 {
		risk = in.Entry * 0.01
	}

	o.PathStartUnix = in.Bars[0].TS
	o.PathEndUnix = in.Bars[len(in.Bars)-1].TS

	mfe, mae := 0.0, 0.0
	stopFirst, targetFirst := false, false
	stopped, targeted := false, false
	maxR := 0.0
	mechanicalR := 0.0
	exited := false

	for i, b := range in.Bars {
		// favorable extreme this bar
		fav := b.High
		adv := b.Low
		if in.Direction == types.CycleDirectionDown {
			fav = b.Low
			adv = b.High
		}
		favPct := sign * (fav - in.Entry) / in.Entry * 100
		advPct := sign * (adv - in.Entry) / in.Entry * 100
		if favPct > mfe {
			mfe = favPct
		}
		if advPct < mae {
			mae = advPct
		}
		rMult := sign * (fav - in.Entry) / risk
		if rMult > maxR {
			maxR = rMult
		}

		if !exited && in.Stop > 0 {
			hitStop := false
			if in.Direction == types.CycleDirectionUp && b.Low <= in.Stop {
				hitStop = true
			}
			if in.Direction == types.CycleDirectionDown && b.High >= in.Stop {
				hitStop = true
			}
			hitTarget := false
			if in.Target > 0 {
				if in.Direction == types.CycleDirectionUp && b.High >= in.Target {
					hitTarget = true
				}
				if in.Direction == types.CycleDirectionDown && b.Low <= in.Target {
					hitTarget = true
				}
			}
			// same bar ambiguity: conservative — stop first if both
			if hitStop && hitTarget {
				stopFirst = true
				stopped = true
				exited = true
				mechanicalR = -1
			} else if hitStop {
				stopFirst = true
				stopped = true
				exited = true
				mechanicalR = -1
			} else if hitTarget {
				targetFirst = true
				targeted = true
				exited = true
				mechanicalR = sign * (in.Target - in.Entry) / risk
			}
		}
		_ = i
	}
	if !exited {
		// time exit at last close
		last := in.Bars[len(in.Bars)-1].Close
		mechanicalR = sign * (last - in.Entry) / risk
	}
	_ = stopped
	_ = targeted

	setRet := func(bars int) *float64 {
		if bars <= 0 || len(in.Bars) == 0 {
			return nil
		}
		idx := bars
		if idx >= len(in.Bars) {
			idx = len(in.Bars) - 1
		}
		px := in.Bars[idx].Close
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
	o.MechanicalR = math.Round(mechanicalR*100) / 100
	o.NetR = math.Round((mechanicalR-in.CostR)*100) / 100
	// complete if we have at least 5m horizon bars
	o.Complete = len(in.Bars) > hz.M5
	return o
}
