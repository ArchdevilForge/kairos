package evaluation

import (
	"fmt"
	"math"
	"sort"

	"github.com/ArchdevilForge/kairos/internal/decision"
	"github.com/ArchdevilForge/kairos/internal/types"
)

// PolicyKind identifies a path-dependent holding policy (Work Item 5 & 14).
type PolicyKind string

const (
	PolicyFixed5m      PolicyKind = "fixed_5m"
	PolicyFixed30m     PolicyKind = "fixed_30m"
	PolicyFixedBracket PolicyKind = "fixed_bracket" // +1.5R / -1.0R
	PolicyWave         PolicyKind = "wave"          // -1R structural stop + next-open BE at k_be R + 15m/1h/4h HL/LH trail
)

// DefaultWaveKBE is the P0 plateau winner (k_be = 0.8R).
const DefaultWaveKBE = 0.8

// KBEGrid is the canonical breakeven activation grid from Work Item 5.
var KBEGrid = []float64{0.8, 1.2, 1.5, 2.0, math.Inf(1)}

// PolicySimInput configures one holding-policy simulation on causal forward bars.
type PolicySimInput struct {
	SignalID      string
	WaveClusterID string
	Symbol        string
	Direction     types.CycleDirection
	EntryUnix     int64
	EntryPrice    float64
	StopPrice     float64
	TickSize      float64
	ATR1mEntry    float64
	ADV5mEntryUSD float64
	NotionalUSD   float64

	Bars            []Bar
	BarPeriodSec    int64 // default 60 (1m bars); 300 if 5m bars
	Policy          PolicyKind
	KBE             float64 // breakeven trigger in R for PolicyWave (default 0.8)
	TargetR         float64 // target in R for PolicyFixedBracket (default 1.5)
	MaxHoldMinutes  int     // default 4320 (72h)
	UseLimitTarget  bool
	FundingSchedule map[int64]float64
	Params          *FrictionParams
}

// PolicySimResult is the outcome of simulating one holding policy on one candidate signal.
type PolicySimResult struct {
	PolicyName    string                `json:"policy_name"`
	SignalID      string                `json:"signal_id"`
	WaveClusterID string                `json:"wave_cluster_id"`
	Symbol        string                `json:"symbol"`
	ExitUnix      int64                 `json:"exit_unix"`
	ExitPrice     float64               `json:"exit_price"`
	ExitKind      string                `json:"exit_kind"` // initial_stop | breakeven_stop | trail_stop | target | time_exit
	HoldSeconds   int64                 `json:"hold_seconds"`
	Cost          FullFrictionBreakdown `json:"cost"`
}

// NetR returns the canonical full-friction NetR for the simulated policy.
func (r PolicySimResult) NetR() float64 {
	return r.Cost.NetR
}

type htfBar struct {
	closeTS int64
	open    float64
	high    float64
	low     float64
	close   float64
}

func aggregateHTFBars(bars []Bar, barPeriodSec, htfPeriodSec int64) []htfBar {
	if len(bars) == 0 || htfPeriodSec <= 0 {
		return nil
	}
	if barPeriodSec <= 0 {
		barPeriodSec = 60
	}
	buckets := make(map[int64][]Bar)
	var keys []int64
	for _, b := range bars {
		bOpen := (b.TS / htfPeriodSec) * htfPeriodSec
		if _, ok := buckets[bOpen]; !ok {
			keys = append(keys, bOpen)
		}
		buckets[bOpen] = append(buckets[bOpen], b)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	var out []htfBar
	for _, bOpen := range keys {
		grp := buckets[bOpen]
		closeTS := bOpen + htfPeriodSec
		if grp[len(grp)-1].TS+barPeriodSec < closeTS {
			continue
		}
		o := grp[0].Open
		h := grp[0].High
		l := grp[0].Low
		c := grp[len(grp)-1].Close
		for _, x := range grp[1:] {
			if x.High > h {
				h = x.High
			}
			if x.Low < l {
				l = x.Low
			}
		}
		out = append(out, htfBar{closeTS: closeTS, open: o, high: h, low: l, close: c})
	}
	return out
}

func buildCausalStructureTrailSchedule(
	bars []Bar,
	barPeriodSec int64,
	direction types.CycleDirection,
	entryPrice, initialStop float64,
) map[int64]float64 {
	sgn := 1.0
	if direction == types.CycleDirectionDown {
		sgn = -1.0
	}
	rPrice := math.Abs(entryPrice - initialStop)
	updates := make(map[int64]float64)

	for _, periodSec := range []int64{900, 3600, 14400} {
		htf := aggregateHTFBars(bars, barPeriodSec, periodSec)
		if len(htf) < 3 {
			continue
		}
		priorPivot := initialStop
		maxFavSeen := entryPrice
		for k := 1; k < len(htf)-1; k++ {
			prev := htf[k-1]
			cur := htf[k]
			next := htf[k+1]
			confirmCloseTS := next.closeTS

			if sgn > 0 {
				maxFavSeen = math.Max(maxFavSeen, math.Max(prev.high, math.Max(cur.high, next.high)))
				isPivotLow := cur.low <= prev.low && cur.low <= next.low
				if isPivotLow && maxFavSeen >= entryPrice+rPrice && cur.low > priorPivot {
					priorPivot = cur.low
					if prevSched, ok := updates[confirmCloseTS]; !ok || cur.low > prevSched {
						updates[confirmCloseTS] = cur.low
					}
				}
			} else {
				maxFavSeen = math.Min(maxFavSeen, math.Min(prev.low, math.Min(cur.low, next.low)))
				isPivotHigh := cur.high >= prev.high && cur.high >= next.high
				if isPivotHigh && maxFavSeen <= entryPrice-rPrice && cur.high < priorPivot {
					priorPivot = cur.high
					if prevSched, ok := updates[confirmCloseTS]; !ok || cur.high < prevSched {
						updates[confirmCloseTS] = cur.high
					}
				}
			}
		}
	}
	return updates
}

// SimulateHoldingPolicy runs causal bar-by-bar replay for Work Item 5 / 13 / 14:
//   - Intra-bar worst case: if one bar touches both stop and target/breakeven threshold, settle as stop-first.
//   - Next-minute-open breakeven activation: when confirming minute closes >= k_be R,
//     the fee-inclusive breakeven stop takes effect at the next bar open.
//   - Causal 15m/1h/4h HL/LH structure trailing.
func SimulateHoldingPolicy(in PolicySimInput) PolicySimResult {
	p := DefaultFrictionParams()
	if in.Params != nil {
		p = *in.Params
	}
	barPeriod := in.BarPeriodSec
	if barPeriod <= 0 {
		barPeriod = 60
	}
	notional := in.NotionalUSD
	if notional <= 0 {
		notional = decision.DefaultTrancheBaseUSD
	}
	var fwd []Bar
	for _, b := range in.Bars {
		if in.EntryUnix <= 0 || b.TS >= in.EntryUnix {
			if len(fwd) > 0 && b.TS-fwd[len(fwd)-1].TS > 5*barPeriod {
				break
			}
			fwd = append(fwd, b)
		}
	}
	if len(fwd) == 0 && len(in.Bars) > 0 {
		fwd = in.Bars
	}
	if len(fwd) == 0 || in.EntryPrice <= 0 {
		return PolicySimResult{
			PolicyName:    string(in.Policy),
			SignalID:      in.SignalID,
			WaveClusterID: in.WaveClusterID,
			Symbol:        in.Symbol,
		}
	}

	sgn := 1.0
	if in.Direction == types.CycleDirectionDown {
		sgn = -1.0
	}
	entry := in.EntryPrice
	stopPx := in.StopPrice
	rPrice := math.Abs(entry - stopPx)
	if rPrice <= 0 {
		rPrice = entry * 0.01
		stopPx = entry - sgn*rPrice
	}

	maxHoldMin := in.MaxHoldMinutes
	if maxHoldMin <= 0 {
		maxHoldMin = 4320
	}
	barsPerMin := math.Max(1.0, float64(barPeriod)/60.0)
	maxBars := int(math.Ceil(float64(maxHoldMin) / barsPerMin))
	if maxBars < 1 {
		maxBars = 1
	}

	var horizonBars int
	var targetPx float64
	hasTarget := false
	var policyName string
	kbe := in.KBE
	if kbe <= 0 {
		kbe = DefaultWaveKBE
	}

	switch in.Policy {
	case PolicyFixed5m:
		horizonBars = int(math.Max(1, math.Round(300.0/float64(barPeriod))))
		policyName = "fixed_5m"
	case PolicyFixed30m:
		horizonBars = int(math.Max(1, math.Round(1800.0/float64(barPeriod))))
		policyName = "fixed_30m"
	case PolicyFixedBracket:
		tr := in.TargetR
		if tr <= 0 {
			tr = 1.5
		}
		horizonBars = maxBars
		targetPx = entry + sgn*tr*rPrice
		hasTarget = true
		policyName = fmt.Sprintf("fixed_+%.1fR/-1.0R", tr)
	default:
		horizonBars = maxBars
		if math.IsInf(kbe, 1) {
			policyName = "wave_k_be=inf"
		} else {
			policyName = fmt.Sprintf("wave_k_be=%.1f", kbe)
		}
	}

	roundTripFeeFrac := 2.0 * p.TakerFeeFrac
	bePrice := entry * (1.0 + sgn*roundTripFeeFrac)

	activeStop := stopPx
	stopState := "initial_stop"
	beArmedNextOpen := false
	beApplied := false

	limitCount := len(fwd)
	if horizonBars < limitCount {
		limitCount = horizonBars
	}
	limitSlice := fwd[:limitCount]

	var trailTimes []int64
	var trailSchedule map[int64]float64
	if in.Policy == PolicyWave || in.Policy == "" {
		trailSchedule = buildCausalStructureTrailSchedule(limitSlice, barPeriod, in.Direction, entry, stopPx)
		for ts := range trailSchedule {
			trailTimes = append(trailTimes, ts)
		}
		sort.Slice(trailTimes, func(i, j int) bool { return trailTimes[i] < trailTimes[j] })
	}
	trailPtr := 0

	exitIdx := len(limitSlice) - 1
	exitPrice := limitSlice[exitIdx].Close
	exitUnix := limitSlice[exitIdx].TS + barPeriod
	exitKind := "time_exit"
	isStopExit := false
	closeIsMaker := false

	tickSize := in.TickSize
	if tickSize <= 0 {
		tickSize = entry * 0.0001
	}

	for i, bar := range limitSlice {
		if in.Policy == PolicyWave || in.Policy == "" {
			if beArmedNextOpen && !beApplied {
				if sgn > 0 && bePrice > activeStop {
					activeStop = bePrice
					stopState = "breakeven_stop"
				} else if sgn < 0 && bePrice < activeStop {
					activeStop = bePrice
					stopState = "breakeven_stop"
				}
				beApplied = true
			}
			for trailPtr < len(trailTimes) && trailTimes[trailPtr] <= bar.TS {
				candStop := trailSchedule[trailTimes[trailPtr]]
				if sgn > 0 && candStop > activeStop {
					activeStop = candStop
					stopState = "trail_stop"
				} else if sgn < 0 && candStop < activeStop {
					activeStop = candStop
					stopState = "trail_stop"
				}
				trailPtr++
			}
		}

		// Gap-through at bar open
		gapStop := (sgn > 0 && bar.Open <= activeStop) || (sgn < 0 && bar.Open >= activeStop)
		if gapStop {
			exitIdx = i
			exitPrice = bar.Open
			exitUnix = bar.TS
			exitKind = stopState
			isStopExit = true
			break
		}

		// Intra-bar check: WORST CASE STOP-FIRST (Work Item 4)
		hitStop := (sgn > 0 && bar.Low <= activeStop) || (sgn < 0 && bar.High >= activeStop)
		hitTarget := false
		if hasTarget {
			if in.UseLimitTarget {
				hitTarget = LimitOrderFills(sgn < 0, targetPx, bar.Low, bar.High, tickSize)
			} else {
				hitTarget = (sgn > 0 && bar.High >= targetPx) || (sgn < 0 && bar.Low <= targetPx)
			}
		}

		if hitStop {
			exitIdx = i
			exitPrice = activeStop
			exitUnix = bar.TS + barPeriod
			exitKind = stopState
			isStopExit = true
			break
		}

		if hitTarget {
			exitIdx = i
			exitPrice = targetPx
			exitUnix = bar.TS + barPeriod
			exitKind = "target"
			isStopExit = false
			closeIsMaker = in.UseLimitTarget
			break
		}

		// End-of-bar close check for breakeven arming (activates at NEXT bar open)
		if (in.Policy == PolicyWave || in.Policy == "") && !beApplied && !beArmedNextOpen && !math.IsInf(kbe, 1) {
			closeR := sgn * (bar.Close - entry) / rPrice
			if closeR >= kbe {
				beArmedNextOpen = true
			}
		}
	}

	_ = exitIdx
	entryTS := in.EntryUnix
	if entryTS <= 0 {
		entryTS = limitSlice[0].TS
	}
	if exitUnix <= entryTS {
		exitUnix = entryTS + barPeriod
	}

	cost := ComputeFullFrictionNetR(FullFrictionInput{
		Symbol:          in.Symbol,
		Direction:       in.Direction,
		EntryPrice:      entry,
		ExitPrice:       exitPrice,
		StopPrice:       stopPx,
		EntryUnix:       entryTS,
		ExitUnix:        exitUnix,
		NotionalUSD:     notional,
		OpenIsMaker:     false,
		CloseIsMaker:    closeIsMaker,
		IsStopExit:      isStopExit,
		ATR1mOpen:       in.ATR1mEntry,
		ADV5mOpenUSD:    in.ADV5mEntryUSD,
		ATR1mClose:      in.ATR1mEntry,
		ADV5mCloseUSD:   in.ADV5mEntryUSD,
		FundingSchedule: in.FundingSchedule,
		Params:          &p,
	})

	return PolicySimResult{
		PolicyName:    policyName,
		SignalID:      in.SignalID,
		WaveClusterID: in.WaveClusterID,
		Symbol:        in.Symbol,
		ExitUnix:      exitUnix,
		ExitPrice:     exitPrice,
		ExitKind:      exitKind,
		HoldSeconds:   exitUnix - entryTS,
		Cost:          cost,
	}
}
