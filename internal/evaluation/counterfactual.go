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

// PathInput is a forward path for one ticket.
// Bars must start STRICTLY AFTER the trigger bar (no same-bar lookahead).
type PathInput struct {
	TicketID  string
	SessionID string
	Symbol    string
	Direction types.CycleDirection
	Decision  types.HumanDecision

	Bars   []Bar
	Entry  float64
	Stop   float64
	Target float64

	// CostR estimated round-trip in R units. Prefer CostPct/stop for honesty.
	CostR float64
	// CostPct round-trip fee+slip as fraction (e.g. 0.001 = 10 bps). If >0 and
	// stop distance known, overrides CostR.
	CostPct float64

	// TimeExitBars: mechanical time stop (default 12 = 1h on 5m). 0 → DefaultHorizons.H1.
	TimeExitBars int
}

// ComputeOutcome walks high/low path after entry bar.
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
	costR := in.CostR
	if in.CostPct > 0 {
		stopPct := risk / in.Entry
		if stopPct > 0 {
			costR = in.CostPct / stopPct
		}
	}
	timeExit := in.TimeExitBars
	if timeExit <= 0 {
		timeExit = hz.H1
	}

	o.PathStartUnix = in.Bars[0].TS
	o.PathEndUnix = in.Bars[len(in.Bars)-1].TS

	mfe, mae := 0.0, 0.0
	stopFirst, targetFirst := false, false
	maxR := 0.0
	mechanicalR := 0.0
	exited := false
	exitIdx := -1

	limit := len(in.Bars)
	if timeExit < limit {
		// still walk full for MFE diagnosis, but mechanical time-exit at timeExit
	}

	for i, b := range in.Bars {
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

		if !exited {
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
			if hitStop && hitTarget {
				stopFirst, exited, exitIdx = true, true, i
				mechanicalR = -1
			} else if hitStop {
				stopFirst, exited, exitIdx = true, true, i
				mechanicalR = -1
			} else if hitTarget {
				targetFirst, exited, exitIdx = true, true, i
				mechanicalR = sign * (in.Target - in.Entry) / risk
			} else if i+1 >= timeExit {
				// fixed horizon time exit at this bar close
				exited, exitIdx = true, i
				mechanicalR = sign * (b.Close - in.Entry) / risk
			}
		}
	}
	if !exited {
		// path shorter than time exit — not finalized
		last := in.Bars[len(in.Bars)-1]
		mechanicalR = sign * (last.Close - in.Entry) / risk
		exitIdx = len(in.Bars) - 1
	}

	setRet := func(bars int) *float64 {
		// honest: need strictly more than `bars` index available (bars is count from 0)
		if bars < 0 || len(in.Bars) <= bars {
			return nil
		}
		px := in.Bars[bars].Close
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
	o.NetR = math.Round((mechanicalR-costR)*100) / 100

	o.Complete5m = len(in.Bars) > hz.M5
	o.Complete15m = len(in.Bars) > hz.M15
	o.Complete1h = len(in.Bars) > hz.H1
	o.Complete4h = len(in.Bars) > hz.H4
	// Finalized: hit stop/target OR reached fixed time-exit bars
	o.Finalized = stopFirst || targetFirst || (exitIdx+1 >= timeExit && len(in.Bars) >= timeExit)
	// Complete (legacy field): finalized for selection alpha
	o.Complete = o.Finalized
	_ = types.DirectionLong // keep? remove
	return o
}
