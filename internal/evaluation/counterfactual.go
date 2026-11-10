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

	// CostR estimated round-trip in R units. Prefer CostPct/stop or UseFullFriction for honesty.
	CostR float64
	// CostPct round-trip fee+slip as fraction (e.g. 0.001 = 10 bps). If >0 and
	// stop distance known, overrides CostR.
	CostPct float64

	// Work Item 12: Canonical full-friction parameters.
	// When UseFullFriction is true (or Friction != nil), NetR uses ComputeFullFrictionNetR
	// (numerator nets out both-side fees + dynamic slippage + accumulated funding;
	// denominator is pre-entry initial 1R including slippage buffer).
	UseFullFriction    bool
	WaveClusterID      string
	NotionalUSD        float64
	ATR1mEntry         float64
	ADV5mEntryUSD      float64
	RealizedFeeUSD     float64
	RealizedFundingUSD float64
	HasRealizedFunding bool
	FundingSchedule    map[int64]float64
	FrictionParams     *FrictionParams
	BarPeriodSec       int64

	// TimeExitBars: mechanical time stop (default 12 = 1h on 5m). 0 → DefaultHorizons.H1.
	TimeExitBars int
}

// ComputeOutcome walks high/low path after entry bar.
func ComputeOutcome(in PathInput, hz HorizonBars) storage.CounterfactualOutcome {
	o := storage.CounterfactualOutcome{
		SchemaVersion: storage.CounterfactualSchemaVersion,
		TicketID:      in.TicketID,
		SessionID:     in.SessionID,
		WaveClusterID: in.WaveClusterID,
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
	exitPx := in.Bars[len(in.Bars)-1].Close

	// Walk all bars for path MFE/MAE; mechanical exit still stops at timeExit/stop/target.
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
				exitPx = in.Stop
			} else if hitStop {
				stopFirst, exited, exitIdx = true, true, i
				mechanicalR = -1
				exitPx = in.Stop
			} else if hitTarget {
				targetFirst, exited, exitIdx = true, true, i
				mechanicalR = sign * (in.Target - in.Entry) / risk
				exitPx = in.Target
			} else if i+1 >= timeExit {
				// fixed horizon time exit at this bar close
				exited, exitIdx = true, i
				mechanicalR = sign * (b.Close - in.Entry) / risk
				exitPx = b.Close
			}
		}
	}
	if !exited {
		// path shorter than time exit — not finalized
		last := in.Bars[len(in.Bars)-1]
		mechanicalR = sign * (last.Close - in.Entry) / risk
		exitIdx = len(in.Bars) - 1
		exitPx = last.Close
	}

	// Horizons are bar *counts*; path[0] is first bar AFTER trigger close.
	// ReturnNm uses Bars[count-1]; need len >= count.
	setRet := func(count int) *float64 {
		if count <= 0 || len(in.Bars) < count {
			return nil
		}
		px := in.Bars[count-1].Close
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

	useFull := in.UseFullFriction ||
		in.FrictionParams != nil ||
		in.HasRealizedFunding ||
		in.RealizedFundingUSD != 0 ||
		in.RealizedFeeUSD > 0 ||
		len(in.FundingSchedule) > 0

	if useFull {
		barPeriod := in.BarPeriodSec
		if barPeriod <= 0 {
			if len(in.Bars) >= 2 && in.Bars[1].TS > in.Bars[0].TS {
				barPeriod = in.Bars[1].TS - in.Bars[0].TS
			} else {
				barPeriod = 300
			}
		}
		exitTS := o.PathEndUnix
		if exitIdx >= 0 && exitIdx < len(in.Bars) {
			exitTS = in.Bars[exitIdx].TS + barPeriod
		}
		ff := ComputeFullFrictionNetR(FullFrictionInput{
			Symbol:             in.Symbol,
			Direction:          in.Direction,
			EntryPrice:         in.Entry,
			ExitPrice:          exitPx,
			StopPrice:          in.Stop,
			EntryUnix:          o.PathStartUnix,
			ExitUnix:           exitTS,
			NotionalUSD:        in.NotionalUSD,
			IsStopExit:         stopFirst,
			ATR1mOpen:          in.ATR1mEntry,
			ADV5mOpenUSD:       in.ADV5mEntryUSD,
			ATR1mClose:         in.ATR1mEntry,
			ADV5mCloseUSD:      in.ADV5mEntryUSD,
			RealizedFeeUSD:     in.RealizedFeeUSD,
			RealizedFundingUSD: in.RealizedFundingUSD,
			HasRealizedFunding: in.HasRealizedFunding,
			FundingSchedule:    in.FundingSchedule,
			Params:             in.FrictionParams,
		})
		o.NetR = round4(ff.NetR)
		o.RealizedFeeUSD = round4(ff.FeeUSD)
		o.RealizedSlippageUSD = round4(ff.SlippageUSD)
		o.RealizedFundingUSD = round4(ff.FundingUSD)
		o.Initial1RPct = round4(ff.Initial1RPct * 100.0)
		o.ExitPrice = exitPx

		waveRes := SimulateHoldingPolicy(PolicySimInput{
			SignalID:        in.TicketID,
			WaveClusterID:   in.WaveClusterID,
			Symbol:          in.Symbol,
			Direction:       in.Direction,
			EntryUnix:       o.PathStartUnix,
			EntryPrice:      in.Entry,
			StopPrice:       in.Stop,
			ATR1mEntry:      in.ATR1mEntry,
			ADV5mEntryUSD:   in.ADV5mEntryUSD,
			NotionalUSD:     in.NotionalUSD,
			Bars:            in.Bars,
			BarPeriodSec:    barPeriod,
			Policy:          PolicyWave,
			KBE:             DefaultWaveKBE,
			FundingSchedule: in.FundingSchedule,
			Params:          in.FrictionParams,
		})
		pw := round4(waveRes.NetR())
		o.PolicyWaveR = &pw
	} else {
		o.NetR = math.Round((mechanicalR-costR)*100) / 100
	}

	o.Complete5m = len(in.Bars) >= hz.M5
	o.Complete15m = len(in.Bars) >= hz.M15
	o.Complete1h = len(in.Bars) >= hz.H1
	o.Complete4h = len(in.Bars) >= hz.H4
	// Finalized: hit stop/target OR reached fixed time-exit bar count
	o.Finalized = stopFirst || targetFirst || (len(in.Bars) >= timeExit && (exitIdx+1 >= timeExit || exitIdx < 0))
	if !stopFirst && !targetFirst && len(in.Bars) >= timeExit {
		o.Finalized = true
	}
	o.Complete = o.Finalized
	return o
}
