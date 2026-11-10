package evaluation

import (
	"fmt"
	"math"
	"sort"

	"github.com/ArchdevilForge/kairos/internal/decision"
	"github.com/ArchdevilForge/kairos/internal/types"
)

// EightHoursSec is the perpetual funding settlement interval (00:00, 08:00, 16:00 UTC).
const EightHoursSec int64 = 8 * 3600

// TwentyFourHoursSec is the wave cluster merge window (<24h MarketPulse wakes merge into one cluster).
const TwentyFourHoursSec int64 = 24 * 3600

// FrictionParams parameterizes the perpetual full-friction cost model (Work Items 2 & 12):
//
//	Cost = V * (f_open + f_close + s_open + s_close) + sum(funding over 8h ticks)
type FrictionParams struct {
	TakerFeeFrac          float64 `json:"taker_fee_frac"`
	MakerFeeFrac          float64 `json:"maker_fee_frac"`
	S0MajorFrac           float64 `json:"s0_major_frac"`
	S0AltFrac             float64 `json:"s0_alt_frac"`
	SlippageLambda        float64 `json:"slippage_lambda"`
	DefaultFundingPerTick float64 `json:"default_funding_per_tick"`
}

// DefaultFrictionParams returns the canonical Phase 1 friction parameters:
// taker f = 5 bps, maker f = 2 bps (>=1 tick trade-through), s0 = 1.5 bps majors / 5 bps alts, lambda = 0.5.
func DefaultFrictionParams() FrictionParams {
	return FrictionParams{
		TakerFeeFrac:          0.0005,  // 5 bps
		MakerFeeFrac:          0.0002,  // 2 bps
		S0MajorFrac:           0.00015, // 1.5 bps (BTC/ETH)
		S0AltFrac:             0.0005,  // 5.0 bps (alts)
		SlippageLambda:        0.5,
		DefaultFundingPerTick: 0.0001, // 1 bp per 8h fallback when schedule is nil
	}
}

// BaseSlippageFrac returns s0 (1.5 bps for BTC/ETH, 5.0 bps for alts).
func BaseSlippageFrac(symbol string, p FrictionParams) float64 {
	if decision.IsMajorAsset(symbol) {
		if p.S0MajorFrac > 0 {
			return p.S0MajorFrac
		}
		return 0.00015
	}
	if p.S0AltFrac > 0 {
		return p.S0AltFrac
	}
	return 0.0005
}

// DynamicSlippageFrac computes dynamic slippage as a fraction of notional V:
//
//	s = s0 + lambda * (ATR_1m / P) * sqrt(V / ADV_5m)
func DynamicSlippageFrac(symbol string, atr1m, price, notionalUSD, adv5mUSD float64, p FrictionParams) float64 {
	s0 := BaseSlippageFrac(symbol, p)
	if price <= 0 || atr1m <= 0 || notionalUSD <= 0 || adv5mUSD <= 0 {
		return s0
	}
	lam := p.SlippageLambda
	if lam < 0 {
		lam = 0.5
	}
	volTerm := math.Max(0, atr1m/price)
	partTerm := math.Sqrt(math.Max(0, notionalUSD/adv5mUSD))
	return s0 + lam*volTerm*partTerm
}

// LimitOrderFills reports whether a resting maker limit order fills on a bar.
// Maker limit orders fill ONLY if price trades through the limit price by >= 1 tick.
func LimitOrderFills(isBuy bool, limitPrice, barLow, barHigh, tickSize float64) bool {
	if tickSize <= 0 {
		return false
	}
	eps := tickSize * 1e-9
	if isBuy {
		return barLow <= limitPrice-tickSize+eps
	}
	return barHigh >= limitPrice+tickSize-eps
}

// Initial1RPct computes the pre-entry initial 1R denominator as a fraction of entry price,
// including the pre-entry slippage buffer (Work Item 12).
func Initial1RPct(entryPrice, stopPrice, openSlipFrac float64) float64 {
	if entryPrice <= 0 {
		return 0
	}
	rawDistFrac := math.Abs(entryPrice-stopPrice) / entryPrice
	if rawDistFrac <= 0 {
		rawDistFrac = 0.01
	}
	return rawDistFrac + math.Max(0, openSlipFrac)
}

// FundingTicksCrossed returns all 8h UTC settlement timestamps (00:00, 08:00, 16:00 UTC)
// strictly after entryUnix and up to exitUnix: t in (entryUnix, exitUnix].
func FundingTicksCrossed(entryUnix, exitUnix int64) []int64 {
	if exitUnix <= entryUnix {
		return nil
	}
	firstTick := (entryUnix/EightHoursSec + 1) * EightHoursSec
	var ticks []int64
	for t := firstTick; t <= exitUnix; t += EightHoursSec {
		ticks = append(ticks, t)
	}
	return ticks
}

// ComputeFundingUSD sums accumulated funding cost across all 8h ticks in (entryUnix, exitUnix].
// Positive return value is a cost reducing PnL; negative return value is a funding rebate.
func ComputeFundingUSD(
	direction types.CycleDirection,
	notionalUSD float64,
	entryUnix, exitUnix int64,
	schedule map[int64]float64,
	defaultRatePerTick float64,
) float64 {
	if notionalUSD <= 0 || exitUnix <= entryUnix {
		return 0
	}
	ticks := FundingTicksCrossed(entryUnix, exitUnix)
	if len(ticks) == 0 {
		return 0
	}
	sgn := 1.0
	if direction == types.CycleDirectionDown {
		sgn = -1.0
	}
	totalFrac := 0.0
	for _, ts := range ticks {
		rate := defaultRatePerTick
		if schedule != nil {
			if r, ok := schedule[ts]; ok {
				rate = r
			} else {
				rate = 0
			}
		}
		totalFrac += sgn * rate
	}
	return notionalUSD * totalFrac
}

// FullFrictionInput carries all trade parameters for canonical full-friction NetR evaluation (Work Item 12).
type FullFrictionInput struct {
	Symbol      string
	Direction   types.CycleDirection
	EntryPrice  float64
	ExitPrice   float64
	StopPrice   float64
	EntryUnix   int64
	ExitUnix    int64
	NotionalUSD float64

	OpenIsMaker  bool
	CloseIsMaker bool
	IsStopExit   bool // Protective / breakeven stop exits are always charged taker + dynamic slippage

	ATR1mOpen     float64
	ADV5mOpenUSD  float64
	ATR1mClose    float64
	ADV5mCloseUSD float64

	// RealizedFeeUSD: if > 0, overrides modeled open+close fee USD.
	RealizedFeeUSD float64
	// RealizedFundingUSD: ifHasRealizedFunding is true or RealizedFundingUSD != 0, overrides modeled 8h schedule.
	RealizedFundingUSD float64
	HasRealizedFunding bool
	FundingSchedule    map[int64]float64
	Params             *FrictionParams
}

// FullFrictionBreakdown details all cost components and canonical NetR for one trade (Work Item 12).
type FullFrictionBreakdown struct {
	NotionalUSD  float64 `json:"notional_usd"`
	Initial1RPct float64 `json:"initial_1r_pct"`
	Initial1RUSD float64 `json:"initial_1r_usd"`
	FOpenFrac    float64 `json:"f_open_frac"`
	FCloseFrac   float64 `json:"f_close_frac"`
	SOpenFrac    float64 `json:"s_open_frac"`
	SCloseFrac   float64 `json:"s_close_frac"`
	FeeUSD       float64 `json:"fee_usd"`
	SlippageUSD  float64 `json:"slippage_usd"`
	FundingUSD   float64 `json:"funding_usd"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	GrossPnLUSD  float64 `json:"gross_pnl_usd"`
	NetPnLUSD    float64 `json:"net_pnl_usd"`
	GrossR       float64 `json:"gross_r"`
	FrictionR    float64 `json:"friction_r"`
	NetR         float64 `json:"net_r"`
}

// ComputeFullFrictionNetR evaluates canonical full-friction NetR (Work Item 12):
//   - Numerator: GrossPnLUSD - FeeUSD - SlippageUSD - FundingUSD
//   - Denominator: NotionalUSD * Initial1RPct (pre-entry initial 1R including slippage buffer)
func ComputeFullFrictionNetR(in FullFrictionInput) FullFrictionBreakdown {
	p := DefaultFrictionParams()
	if in.Params != nil {
		p = *in.Params
	}
	notional := in.NotionalUSD
	if notional <= 0 {
		notional = decision.DefaultTrancheBaseUSD
	}
	if in.EntryPrice <= 0 {
		return FullFrictionBreakdown{NotionalUSD: notional}
	}

	sgn := 1.0
	if in.Direction == types.CycleDirectionDown {
		sgn = -1.0
	}

	fOpen := p.TakerFeeFrac
	sOpen := DynamicSlippageFrac(in.Symbol, in.ATR1mOpen, in.EntryPrice, notional, in.ADV5mOpenUSD, p)
	if in.OpenIsMaker {
		fOpen = p.MakerFeeFrac
		sOpen = 0
	}

	atrClose := in.ATR1mClose
	if atrClose <= 0 {
		atrClose = in.ATR1mOpen
	}
	advClose := in.ADV5mCloseUSD
	if advClose <= 0 {
		advClose = in.ADV5mOpenUSD
	}

	var fClose, sClose float64
	if in.IsStopExit {
		// Protective & breakeven stop exits are ALWAYS charged taker fee + dynamic slippage.
		fClose = p.TakerFeeFrac
		sClose = DynamicSlippageFrac(in.Symbol, atrClose, in.ExitPrice, notional, advClose, p)
	} else if in.CloseIsMaker {
		fClose = p.MakerFeeFrac
		sClose = 0
	} else {
		fClose = p.TakerFeeFrac
		sClose = DynamicSlippageFrac(in.Symbol, atrClose, in.ExitPrice, notional, advClose, p)
	}

	// Pre-entry 1R denominator always includes pre-entry slippage buffer.
	preEntrySlipBuffer := DynamicSlippageFrac(in.Symbol, in.ATR1mOpen, in.EntryPrice, notional, in.ADV5mOpenUSD, p)
	r1Pct := Initial1RPct(in.EntryPrice, in.StopPrice, preEntrySlipBuffer)
	r1USD := notional * r1Pct
	if r1USD <= 0 {
		r1USD = notional * 0.01
	}

	feeUSD := notional * (fOpen + fClose)
	if in.RealizedFeeUSD > 0 {
		feeUSD = in.RealizedFeeUSD
	}
	slipUSD := notional * (sOpen + sClose)

	var fundingUSD float64
	if in.HasRealizedFunding || in.RealizedFundingUSD != 0 {
		fundingUSD = in.RealizedFundingUSD
	} else {
		fundingUSD = ComputeFundingUSD(
			in.Direction,
			notional,
			in.EntryUnix,
			in.ExitUnix,
			in.FundingSchedule,
			p.DefaultFundingPerTick,
		)
	}

	totalCostUSD := feeUSD + slipUSD + fundingUSD
	grossRetFrac := sgn * ((in.ExitPrice - in.EntryPrice) / in.EntryPrice)
	grossPnLUSD := notional * grossRetFrac
	netPnLUSD := grossPnLUSD - totalCostUSD

	grossR := grossPnLUSD / r1USD
	frictionR := totalCostUSD / r1USD
	netR := netPnLUSD / r1USD

	return FullFrictionBreakdown{
		NotionalUSD:  notional,
		Initial1RPct: r1Pct,
		Initial1RUSD: r1USD,
		FOpenFrac:    fOpen,
		FCloseFrac:   fClose,
		SOpenFrac:    sOpen,
		SCloseFrac:   sClose,
		FeeUSD:       feeUSD,
		SlippageUSD:  slipUSD,
		FundingUSD:   fundingUSD,
		TotalCostUSD: totalCostUSD,
		GrossPnLUSD:  grossPnLUSD,
		NetPnLUSD:    netPnLUSD,
		GrossR:       grossR,
		FrictionR:    frictionR,
		NetR:         netR,
	}
}

// WakeEventTimestamp pairs an event/session/ticket ID with its Unix timestamp for wave clustering.
type WakeEventTimestamp struct {
	ID        string
	Timestamp int64
}

// BuildWaveClusters merges MarketPulse market-wide wake events <24h apart into one wave_cluster_id (Work Item 3).
func BuildWaveClusters(events []WakeEventTimestamp) map[string]string {
	out := make(map[string]string, len(events))
	if len(events) == 0 {
		return out
	}
	sorted := append([]WakeEventTimestamp(nil), events...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Timestamp == sorted[j].Timestamp {
			return sorted[i].ID < sorted[j].ID
		}
		return sorted[i].Timestamp < sorted[j].Timestamp
	})

	clusterIdx := 1
	prevTS := sorted[0].Timestamp
	cid := fmt.Sprintf("wave_cluster_%04d", clusterIdx)
	out[sorted[0].ID] = cid

	for _, ev := range sorted[1:] {
		if ev.Timestamp-prevTS >= TwentyFourHoursSec {
			clusterIdx++
			cid = fmt.Sprintf("wave_cluster_%04d", clusterIdx)
		}
		out[ev.ID] = cid
		prevTS = ev.Timestamp
	}
	return out
}

// ClusterMeanStat holds a mean NetR estimate with CR1 cluster-robust standard error grouped by wave_cluster_id (Work Item 3).
type ClusterMeanStat struct {
	N         int     `json:"n"`
	NClusters int     `json:"n_clusters"`
	Mean      float64 `json:"mean"`
	ClusterSE float64 `json:"cluster_se"`
	TStat     float64 `json:"t_stat"`
	CI95Low   float64 `json:"ci95_low"`
	CI95High  float64 `json:"ci95_high"`
}

// ClusterRobustMean computes the sample mean and CR1 cluster-robust standard error grouped by wave_cluster_id.
func ClusterRobustMean(values []float64, clusterIDs []string) ClusterMeanStat {
	n := len(values)
	if n == 0 || len(clusterIDs) != n {
		return ClusterMeanStat{}
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(n)

	groups := make(map[string][]float64)
	var order []string
	for i, v := range values {
		cid := clusterIDs[i]
		if cid == "" {
			cid = "wave_cluster_0001"
		}
		if _, ok := groups[cid]; !ok {
			order = append(order, cid)
		}
		groups[cid] = append(groups[cid], v)
	}
	gCount := len(order)
	if gCount < 2 {
		return ClusterMeanStat{
			N:         n,
			NClusters: gCount,
			Mean:      round4(mean),
		}
	}

	sqSum := 0.0
	for _, cid := range order {
		sG := 0.0
		for _, y := range groups[cid] {
			sG += y - mean
		}
		sqSum += sG * sG
	}
	dofFactor := float64(gCount) / float64(gCount-1)
	if n > 1 {
		dofFactor *= float64(n-1) / float64(n)
	}
	varHat := dofFactor * sqSum / (float64(n) * float64(n))
	se := math.Sqrt(math.Max(0, varHat))
	tStat := 0.0
	if se > 0 {
		tStat = mean / se
	}
	halfWidth := 1.96 * se
	return ClusterMeanStat{
		N:         n,
		NClusters: gCount,
		Mean:      round4(mean),
		ClusterSE: round4(se),
		TStat:     round4(tStat),
		CI95Low:   round4(mean - halfWidth),
		CI95High:  round4(mean + halfWidth),
	}
}
