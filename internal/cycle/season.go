package cycle

import (
	"math"
	"sort"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// Default threshold and window constants ported verbatim from
// tools/research/h1_experiment.py (classify_season_from_aggregates + build_causal_season_timeline).
const (
	DefaultSummerBreadthMin     = 0.58
	DefaultSpringBreadthMin     = 0.46
	DefaultAutumnSlowBreadthMin = 0.52
	DefaultAutumnFastBreadthMin = 0.40
	DefaultBreadthMomentumFloor = -0.02
	DefaultSummerVolMin         = 0.98
	DefaultWinterVolMax         = 0.85

	DefaultSeasonFastBars        = 6    // 30m rolling breadth & volume
	DefaultSeasonSlowBars        = 24   // 2h rolling baseline breadth & volume
	DefaultSeasonNoiseRetPct     = 0.05 // 5m slice return noise floor (%)
	DefaultSeasonMinValidSymbols = 4
	DefaultSeasonHysteresisBars  = 2
)

// SeasonThresholds holds the threshold parameters for ClassifySeasonWithThresholds.
type SeasonThresholds struct {
	SummerBreadthMin     float64
	SpringBreadthMin     float64
	AutumnSlowBreadthMin float64
	SummerVolMin         float64
	WinterVolMax         float64
}

// DefaultSeasonThresholds returns the canonical threshold set from h1_experiment.py.
func DefaultSeasonThresholds() SeasonThresholds {
	return SeasonThresholds{
		SummerBreadthMin:     DefaultSummerBreadthMin,
		SpringBreadthMin:     DefaultSpringBreadthMin,
		AutumnSlowBreadthMin: DefaultAutumnSlowBreadthMin,
		SummerVolMin:         DefaultSummerVolMin,
		WinterVolMax:         DefaultWinterVolMax,
	}
}

// SeasonTimelineConfig holds window parameters for BuildCausalSeasonTimelineWithConfig.
type SeasonTimelineConfig struct {
	FastBars      int
	SlowBars      int
	NoiseRetPct   float64
	Thresholds    SeasonThresholds
	BTCSymbol     string
	MinValidSyms  int
	HysteresisLen int
}

// DefaultSeasonTimelineConfig returns the canonical timeline configuration from h1_experiment.py.
func DefaultSeasonTimelineConfig() SeasonTimelineConfig {
	return SeasonTimelineConfig{
		FastBars:      DefaultSeasonFastBars,
		SlowBars:      DefaultSeasonSlowBars,
		NoiseRetPct:   DefaultSeasonNoiseRetPct,
		Thresholds:    DefaultSeasonThresholds(),
		BTCSymbol:     "BTC/USDT:USDT",
		MinValidSyms:  DefaultSeasonMinValidSymbols,
		HysteresisLen: DefaultSeasonHysteresisBars,
	}
}

// SeasonBar1m represents a 1-minute historical bar with quote volume (matching research MinuteBar).
type SeasonBar1m struct {
	TS       int64   `json:"ts"`
	Open     float64 `json:"open"`
	High     float64 `json:"high"`
	Low      float64 `json:"low"`
	Close    float64 `json:"close"`
	QuoteVol float64 `json:"quote_vol"`
}

// SeasonBar5m represents a closed 5-minute bar indexed by CloseTS = open_ts + 300.
type SeasonBar5m struct {
	CloseTS  int64   `json:"close_ts"`
	Open     float64 `json:"open"`
	High     float64 `json:"high"`
	Low      float64 `json:"low"`
	Close    float64 `json:"close"`
	QuoteVol float64 `json:"quote_vol"`
}

const seasonBar5mSeconds int64 = 300

// SeasonBar5mFromCandle is the single canonical converter from a closed 5-minute
// types.Candle (whose Timestamp is bar open time in Unix seconds, or milliseconds
// if > 1e12) into a SeasonBar5m indexed by CloseTS = open_ts + 300, matching
// Build5mBarsFrom1m and tools/research/h1_experiment.py.
func SeasonBar5mFromCandle(c types.Candle) SeasonBar5m {
	openTS := c.Timestamp
	if openTS > 1_000_000_000_000 {
		openTS /= 1000
	}
	return SeasonBar5m{
		CloseTS:  openTS + seasonBar5mSeconds,
		Open:     c.Open,
		High:     c.High,
		Low:      c.Low,
		Close:    c.Close,
		QuoteVol: c.QuoteVol,
	}
}

// SeasonObservation is the causal market season state at 5m bar close timestamp TS.
// Ported verbatim from tools/research/h1_experiment.py SeasonObservation.
type SeasonObservation struct {
	TS              int64              `json:"ts"`
	SeasonRegime    types.SeasonRegime `json:"season_regime"`
	BreadthFast     float64            `json:"breadth_fast"`
	BreadthSlow     float64            `json:"breadth_slow"`
	BreadthMomentum float64            `json:"breadth_momentum"`
	VolumeRatio     float64            `json:"volume_ratio"`
	BTCNearHigh     bool               `json:"btc_near_high"`
	CrowdingPct     float64            `json:"crowding_pct"`
}

// ClassifySeasonFromAggregates maps (breadthFast, breadthSlow, volumeRatio) -> SeasonRegime
// using the canonical thresholds from tools/research/h1_experiment.py.
func ClassifySeasonFromAggregates(breadthFast, breadthSlow, volumeRatio float64) types.SeasonRegime {
	return ClassifySeasonWithThresholds(breadthFast, breadthSlow, volumeRatio, DefaultSeasonThresholds())
}

// ClassifySeasonWithThresholds is the deterministic causal rule mapping
// (breadthFast, breadthSlow, volumeRatio) -> SeasonRegime with explicit thresholds.
//
// Ported verbatim from tools/research/h1_experiment.py classify_season_from_aggregates:
//   - SUMMER: breadth_fast >= summer_breadth_min and volume_ratio >= summer_vol_min
//   - AUTUMN: breadth_slow >= autumn_slow_breadth_min and breadth_fast >= 0.40 and (b_mom < -0.02 or volume_ratio < summer_vol_min)
//   - SPRING: breadth_fast >= spring_breadth_min and b_mom >= -0.02 and volume_ratio >= winter_vol_max
//   - WINTER: otherwise
func ClassifySeasonWithThresholds(breadthFast, breadthSlow, volumeRatio float64, th SeasonThresholds) types.SeasonRegime {
	bMom := breadthFast - breadthSlow
	if breadthFast >= th.SummerBreadthMin && volumeRatio >= th.SummerVolMin {
		return types.SeasonRegimeSummer
	}
	if breadthSlow >= th.AutumnSlowBreadthMin &&
		breadthFast >= DefaultAutumnFastBreadthMin &&
		(bMom < DefaultBreadthMomentumFloor || volumeRatio < th.SummerVolMin) {
		return types.SeasonRegimeAutumn
	}
	if breadthFast >= th.SpringBreadthMin &&
		bMom >= DefaultBreadthMomentumFloor &&
		volumeRatio >= th.WinterVolMax {
		return types.SeasonRegimeSpring
	}
	return types.SeasonRegimeWinter
}

// Build5mBarsFrom1m aggregates sorted 1m bars into complete 5m bars (len(grp) >= 5),
// setting CloseTS = b_open + 300 and summing QuoteVol exactly as in h1_experiment.py.
func Build5mBarsFrom1m(series []SeasonBar1m) []SeasonBar5m {
	if len(series) == 0 {
		return nil
	}
	buckets := make(map[int64][]SeasonBar1m)
	var keys []int64
	for _, b := range series {
		bOpen := (b.TS / 300) * 300
		if _, exists := buckets[bOpen]; !exists {
			keys = append(keys, bOpen)
		}
		buckets[bOpen] = append(buckets[bOpen], b)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	out := make([]SeasonBar5m, 0, len(keys))
	for _, bOpen := range keys {
		grp := buckets[bOpen]
		if len(grp) < 5 {
			continue
		}
		hi := grp[0].High
		lo := grp[0].Low
		qSum := 0.0
		for _, x := range grp {
			if x.High > hi {
				hi = x.High
			}
			if x.Low < lo {
				lo = x.Low
			}
			qSum += x.QuoteVol
		}
		out = append(out, SeasonBar5m{
			CloseTS:  bOpen + 300,
			Open:     grp[0].Open,
			High:     hi,
			Low:      lo,
			Close:    grp[len(grp)-1].Close,
			QuoteVol: qSum,
		})
	}
	return out
}

// BuildCausalSeasonTimeline computes causal SeasonObservation at every 5m close timestamp t
// using ONLY bars closed <= t, with canonical parameters from tools/research/h1_experiment.py.
func BuildCausalSeasonTimeline(
	bars5mBySym map[string][]SeasonBar5m,
	fundingBySym map[string]map[int64]float64,
) map[int64]SeasonObservation {
	return BuildCausalSeasonTimelineWithConfig(bars5mBySym, fundingBySym, DefaultSeasonTimelineConfig())
}

// BuildCausalSeasonTimelineWithConfig is the configurable counterpart of BuildCausalSeasonTimeline.
// Ported verbatim from tools/research/h1_experiment.py build_causal_season_timeline.
func BuildCausalSeasonTimelineWithConfig(
	bars5mBySym map[string][]SeasonBar5m,
	fundingBySym map[string]map[int64]float64,
	cfg SeasonTimelineConfig,
) map[int64]SeasonObservation {
	if cfg.FastBars <= 0 {
		cfg.FastBars = DefaultSeasonFastBars
	}
	if cfg.SlowBars <= 0 {
		cfg.SlowBars = DefaultSeasonSlowBars
	}
	if cfg.MinValidSyms <= 0 {
		cfg.MinValidSyms = DefaultSeasonMinValidSymbols
	}
	if cfg.HysteresisLen <= 0 {
		cfg.HysteresisLen = DefaultSeasonHysteresisBars
	}
	if cfg.BTCSymbol == "" {
		cfg.BTCSymbol = "BTC/USDT:USDT"
	}
	if cfg.Thresholds == (SeasonThresholds{}) {
		cfg.Thresholds = DefaultSeasonThresholds()
	}

	allClosesSet := make(map[int64]struct{})
	symIdx := make(map[string]map[int64]int, len(bars5mBySym))
	syms := make([]string, 0, len(bars5mBySym))
	for sym, b5 := range bars5mBySym {
		syms = append(syms, sym)
		idxMap := make(map[int64]int, len(b5))
		for i, row := range b5 {
			allClosesSet[row.CloseTS] = struct{}{}
			idxMap[row.CloseTS] = i
		}
		symIdx[sym] = idxMap
	}
	sort.Strings(syms)

	sortedTS := make([]int64, 0, len(allClosesSet))
	for ts := range allClosesSet {
		sortedTS = append(sortedTS, ts)
	}
	sort.Slice(sortedTS, func(i, j int) bool { return sortedTS[i] < sortedTS[j] })

	type fundingTickMean struct {
		ts   int64
		mean float64
	}
	var fundingHistoryMeans []fundingTickMean
	if len(fundingBySym) > 0 {
		byTick := make(map[int64][]float64)
		for _, sym := range syms {
			sched, ok := fundingBySym[sym]
			if !ok {
				continue
			}
			for fTS, rate := range sched {
				byTick[fTS] = append(byTick[fTS], rate)
			}
		}
		// Include any symbols present in fundingBySym but not in bars5mBySym.
		for sym, sched := range fundingBySym {
			if _, seen := bars5mBySym[sym]; seen {
				continue
			}
			for fTS, rate := range sched {
				byTick[fTS] = append(byTick[fTS], rate)
			}
		}
		tickKeys := make([]int64, 0, len(byTick))
		for fTS := range byTick {
			tickKeys = append(tickKeys, fTS)
		}
		sort.Slice(tickKeys, func(i, j int) bool { return tickKeys[i] < tickKeys[j] })
		for _, fTS := range tickKeys {
			vals := byTick[fTS]
			if len(vals) > 0 {
				s := 0.0
				for _, v := range vals {
					s += v
				}
				fundingHistoryMeans = append(fundingHistoryMeans, fundingTickMean{
					ts:   fTS,
					mean: s / float64(len(vals)),
				})
			}
		}
	}

	btcBars := bars5mBySym[cfg.BTCSymbol]
	btcIdxMap := symIdx[cfg.BTCSymbol]

	timeline := make(map[int64]SeasonObservation, len(sortedTS))
	prevSeason := types.SeasonRegimeWinter
	streakCandidate := types.SeasonRegimeWinter
	streakLen := 0

	for _, ts := range sortedTS {
		fastUpSum := 0.0
		slowUpSum := 0.0
		volFastSum := 0.0
		volSlowPerFastSum := 0.0
		validSyms := 0

		for _, sym := range syms {
			b5 := bars5mBySym[sym]
			idx, ok := symIdx[sym][ts]
			if !ok || idx < 3 {
				continue
			}
			fStart := idx - cfg.FastBars + 1
			if fStart < 0 {
				fStart = 0
			}
			sStart := idx - cfg.SlowBars + 1
			if sStart < 0 {
				sStart = 0
			}
			fSlice := b5[fStart : idx+1]
			sSlice := b5[sStart : idx+1]
			if len(fSlice) < 2 || len(sSlice) < 4 {
				continue
			}

			// Ensure contiguous window (do not bridge across multi-hour sample gaps)
			if fSlice[len(fSlice)-1].CloseTS-fSlice[0].CloseTS > int64(len(fSlice)+2)*300 {
				continue
			}
			for len(sSlice) > len(fSlice) &&
				(sSlice[len(sSlice)-1].CloseTS-sSlice[0].CloseTS > int64(len(sSlice)+4)*300) {
				sSlice = sSlice[1:]
			}

			retFast := 0.0
			if fSlice[0].Open > 0 {
				retFast = (fSlice[len(fSlice)-1].Close - fSlice[0].Open) / fSlice[0].Open * 100.0
			}
			retSlow := 0.0
			if sSlice[0].Open > 0 {
				retSlow = (sSlice[len(sSlice)-1].Close - sSlice[0].Open) / sSlice[0].Open * 100.0
			}
			if retFast > cfg.NoiseRetPct {
				fastUpSum += 1.0
			}
			if retSlow > cfg.NoiseRetPct {
				slowUpSum += 1.0
			}

			vFast := 0.0
			for _, x := range fSlice {
				vFast += x.QuoteVol
			}
			vFast /= float64(len(fSlice))

			vSlow := 0.0
			for _, x := range sSlice {
				vSlow += x.QuoteVol
			}
			vSlow /= float64(len(sSlice))

			volFastSum += vFast
			volSlowPerFastSum += vSlow
			validSyms++
		}

		if validSyms < cfg.MinValidSyms {
			continue
		}

		bFast := fastUpSum / float64(validSyms)
		bSlow := slowUpSum / float64(validSyms)
		vRatio := 1.0
		if volSlowPerFastSum > 0 {
			vRatio = volFastSum / volSlowPerFastSum
		}

		rawSeason := ClassifySeasonWithThresholds(bFast, bSlow, vRatio, cfg.Thresholds)
		// 2-bar causal hysteresis to prevent single-5m-bar flicker
		var activeSeason types.SeasonRegime
		if rawSeason == prevSeason {
			activeSeason = prevSeason
			streakCandidate = rawSeason
			streakLen = 0
		} else {
			if rawSeason == streakCandidate {
				streakLen++
			} else {
				streakCandidate = rawSeason
				streakLen = 1
			}
			if streakLen >= cfg.HysteresisLen {
				activeSeason = rawSeason
				prevSeason = activeSeason
			} else {
				activeSeason = prevSeason
			}
		}

		// BTC near rolling highs (within 0.6% of trailing 48-bar / 4h high up to ts)
		btcNearHigh := false
		if btcIdxMap != nil {
			if bIdx, ok := btcIdxMap[ts]; ok && bIdx >= 6 && len(btcBars) > 0 {
				wStart := bIdx - 48
				if wStart < 0 {
					wStart = 0
				}
				rollingHi := 0.0
				hasWBars := false
				for _, b := range btcBars[wStart : bIdx+1] {
					if ts-b.CloseTS <= 48*300 {
						if !hasWBars || b.High > rollingHi {
							rollingHi = b.High
							hasWBars = true
						}
					}
				}
				if hasWBars {
					btcClose := btcBars[bIdx].Close
					if rollingHi > 0 && (rollingHi-btcClose)/rollingHi <= 0.006 {
						btcNearHigh = true
					}
				}
			}
		}

		// Causal CrowdingPct in [0, 1]: empirical percentile of latest known 8h funding <= ts
		// blended with short-term volume/breadth heat
		var pastF []float64
		for _, fm := range fundingHistoryMeans {
			if fm.ts <= ts {
				pastF = append(pastF, fm.mean)
			}
		}
		var crowdingPct float64
		if len(pastF) >= 3 {
			curF := pastF[len(pastF)-1]
			leCount := 0
			for _, x := range pastF {
				if x <= curF {
					leCount++
				}
			}
			rankF := float64(leCount) / float64(len(pastF))
			crowdingPct = 0.7*rankF + 0.3*math.Min(1.0, math.Max(0.0, bFast))
		} else {
			crowdingPct = math.Min(1.0, math.Max(0.0, 0.5*bFast+0.25*math.Min(2.0, vRatio)))
		}

		timeline[ts] = SeasonObservation{
			TS:              ts,
			SeasonRegime:    activeSeason,
			BreadthFast:     bFast,
			BreadthSlow:     bSlow,
			BreadthMomentum: bFast - bSlow,
			VolumeRatio:     vRatio,
			BTCNearHigh:     btcNearHigh,
			CrowdingPct:     crowdingPct,
		}
	}

	return timeline
}

// CalibrateOOSSeasonRegime computes the walk-forward out-of-sample SeasonRegime
// calibrated on prior timeline observations strictly before the ticket's wave cluster
// start timestamp (matching lines 708-728 of tools/research/h1_experiment.py).
func CalibrateOOSSeasonRegime(obs SeasonObservation, priorObs []SeasonObservation) types.SeasonRegime {
	if len(priorObs) < 20 {
		return obs.SeasonRegime
	}
	bSorted := make([]float64, len(priorObs))
	vSorted := make([]float64, len(priorObs))
	for i, o := range priorObs {
		bSorted[i] = o.BreadthFast
		vSorted[i] = o.VolumeRatio
	}
	sort.Float64s(bSorted)
	sort.Float64s(vSorted)

	nMinus1 := float64(len(bSorted) - 1)
	q65B := bSorted[int(0.65*nMinus1)]
	q45B := bSorted[int(0.45*nMinus1)]
	q50V := vSorted[int(0.50*nMinus1)]
	q30V := vSorted[int(0.30*nMinus1)]

	return ClassifySeasonWithThresholds(
		obs.BreadthFast,
		obs.BreadthSlow,
		obs.VolumeRatio,
		SeasonThresholds{
			SummerBreadthMin:     math.Max(0.52, math.Min(0.68, q65B)),
			SpringBreadthMin:     math.Max(0.42, math.Min(0.54, q45B)),
			AutumnSlowBreadthMin: math.Max(0.48, math.Min(0.60, 0.5*(q65B+q45B))),
			SummerVolMin:         math.Max(0.90, math.Min(1.15, q50V)),
			WinterVolMax:         math.Max(0.75, math.Min(0.92, q30V)),
		},
	)
}

// LatestSeasonObservationAtOrBefore returns the SeasonObservation at the greatest
// timestamp <= ts from timeline, or false if none exists (strictly causal).
func LatestSeasonObservationAtOrBefore(timeline map[int64]SeasonObservation, ts int64) (SeasonObservation, bool) {
	if len(timeline) == 0 {
		return SeasonObservation{}, false
	}
	if obs, ok := timeline[ts]; ok {
		return obs, true
	}
	var bestTS int64 = -1
	var best SeasonObservation
	for k, obs := range timeline {
		if k <= ts && (bestTS == -1 || k > bestTS) {
			bestTS = k
			best = obs
		}
	}
	if bestTS == -1 {
		return SeasonObservation{}, false
	}
	return best, true
}
