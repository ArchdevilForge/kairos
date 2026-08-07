package cycle

import (
	"math"

	"github.com/ArchdevilForge/kairos/internal/types"
)

const (
	strengthLook = 20
	momLook      = 10
	minBarsPhase = 40
)

// detectPhase classifies wave phase given direction. Neutral ⇒ winter.
func detectPhase(dir types.CycleDirection, closes, volumes []float64) (types.WavePhase, float64, float64, float64, []types.Evidence) {
	var ev []types.Evidence
	if dir == types.CycleDirectionNeutral {
		ev = append(ev, types.Evidence{Code: "neutral_direction", Description: "no stable direction → winter"})
		return types.WavePhaseWinter, 0.6, 0, 0, ev
	}
	if len(closes) < minBarsPhase {
		return types.WavePhaseWinter, 0.3, 0, 0, []types.Evidence{{
			Code: "insufficient_bars", Description: "need bars for phase", Value: float64(len(closes)),
		}}
	}

	// trend strength: |pct change| scaled by recent vol
	chg := pctChange(closes, strengthLook)
	rets := make([]float64, 0, strengthLook)
	for i := len(closes) - strengthLook; i < len(closes); i++ {
		if i <= 0 || closes[i-1] == 0 {
			continue
		}
		rets = append(rets, (closes[i]-closes[i-1])/closes[i-1])
	}
	vol := stdPop(rets) * 100
	if vol < 1e-9 {
		vol = 1e-9
	}
	signedStrength := chg / vol // in "vol multiples" over window
	strength := math.Abs(signedStrength)

	// momentum change: recent half vs prior half of strengthLook
	recentChg := pctChange(closes, momLook)
	priorSlice := closes[len(closes)-strengthLook : len(closes)-momLook]
	priorChg := 0.0
	if len(priorSlice) > 1 && priorSlice[0] != 0 {
		priorChg = (priorSlice[len(priorSlice)-1] - priorSlice[0]) / priorSlice[0] * 100
	}
	// align sign with direction
	sign := 1.0
	if dir == types.CycleDirectionDown {
		sign = -1
	}
	momRecent := sign * recentChg
	momPrior := sign * priorChg
	momDelta := momRecent - momPrior

	// volume quality crude
	// thrash check: many mid-MA crosses → winter even if dir weakly voted
	mid := sma(closes, midPeriod)
	crosses := 0
	if mid > 0 && len(closes) >= 15 {
		for i := len(closes) - 14; i < len(closes); i++ {
			if i == 0 {
				continue
			}
			a, b := closes[i-1]-mid, closes[i]-mid
			if a == 0 || b == 0 {
				continue
			}
			if (a > 0) != (b > 0) {
				crosses++
			}
		}
	}
	if crosses >= 6 && strength < 1.2 {
		ev = append(ev, types.Evidence{Code: "ma_thrash", Description: "repeated mid-MA crosses", Value: float64(crosses)})
		return types.WavePhaseWinter, 0.7, strength, momDelta, ev
	}

	// direction-aligned strength must be positive enough; else autumn/winter
	aligned := sign * chg
	switch {
	case aligned > 0 && strength >= 1.5 && momDelta >= 0:
		ev = append(ev,
			types.Evidence{Code: "trend_expanding", Description: "aligned strength high and momentum rising", Value: strength},
			types.Evidence{Code: "momentum_expanding", Description: "momentum change positive", Value: momDelta},
		)
		return types.WavePhaseSummer, clamp01(0.5 + strength/6), strength, momDelta, ev
	case aligned > 0 && strength >= 0.6 && momDelta > 0 && momPrior < momRecent:
		ev = append(ev, types.Evidence{Code: "new_direction_thrust", Description: "strength building from lower base", Value: strength})
		return types.WavePhaseSpring, clamp01(0.45 + strength/5), strength, momDelta, ev
	case aligned > 0 && strength >= 0.5 && momDelta < 0:
		ev = append(ev, types.Evidence{Code: "momentum_decaying", Description: "direction holds but momentum fades", Value: momDelta})
		return types.WavePhaseAutumn, clamp01(0.4 + strength/6), strength, momDelta, ev
	case aligned > 0 && strength >= 0.8:
		// steady but not expanding → summer-lite
		ev = append(ev, types.Evidence{Code: "trend_persistent", Description: "aligned trend persists", Value: strength})
		return types.WavePhaseSummer, clamp01(0.4 + strength/6), strength, momDelta, ev
	case aligned > 0:
		ev = append(ev, types.Evidence{Code: "early_or_weak_thrust", Description: "weak aligned move", Value: strength})
		return types.WavePhaseSpring, clamp01(0.35 + strength/5), strength, momDelta, ev
	default:
		ev = append(ev, types.Evidence{Code: "direction_not_confirmed_by_change", Description: "votes vs net change disagree", Value: aligned})
		return types.WavePhaseWinter, 0.5, strength, momDelta, ev
	}
}
