package scanner

import (
	"fmt"
	"math"
	"strings"

	"github.com/ArchdevilForge/kairos/internal/data"
	"github.com/ArchdevilForge/kairos/internal/types"
)

// ---------------------------------------------------------------------------
// Candidate scoring  (ported from MarketScanner._score_candidate)
// 10-pt scale based on liquidity, velocity, OI, funding, relative strength.
// ---------------------------------------------------------------------------

func (s *MarketScanner) scoreCandidate(
	symbol, exchangeName string,
	ticker *types.Ticker,
	btcChange24h *float64,
	watchBoost float64,
	rsiMap map[string]data.CoinRSI,
) types.Candidate {
	cand := types.Candidate{
		Symbol:       symbol,
		Exchange:     exchangeName,
		ScoreReasons: []string{},
		Warnings:     []string{},
	}

	if ticker == nil {
		return cand
	}

	weights := s.config.Scoring.CandidateWeights
	minLiq := s.config.Scoring.MinimumLiquidityQuoteVolume

	score := 0.0

	// 1. Quote volume component (up to 4 pts)
	if ticker.QuoteVolume != nil && *ticker.QuoteVolume > 0 {
		cand.QuoteVolume24h = math.Round(*ticker.QuoteVolume*100) / 100
		volumeRatio := *ticker.QuoteVolume / minLiq
		volWeight := getWeight(weights, "quoteVolume", 4.0)
		volComponent := math.Min(volWeight, volWeight*math.Min(volumeRatio, 4.0)/4.0)
		score += volComponent
		cand.ScoreReasons = append(cand.ScoreReasons, fmt.Sprintf("quote volume component=%.2f", volComponent))
	} else {
		cand.Warnings = append(cand.Warnings, "missing quoteVolume")
	}

	// 2. 24h price change / velocity (up to 2 pts)
	if ticker.ChangePct != nil {
		cand.Change24hPct = ticker.ChangePct
		velWeight := getWeight(weights, "priceVelocity", 2.0)
		velComponent := math.Min(velWeight, math.Abs(*ticker.ChangePct)/5.0*velWeight)
		score += velComponent
		cand.ScoreReasons = append(cand.ScoreReasons, fmt.Sprintf("24h change component=%.2f", velComponent))

		// 3. Relative strength bonus (up to 2 pts, long-only)
		if *ticker.ChangePct > 0 {
			rsWeight := getWeight(weights, "relativeStrength", 2.0)
			rsComponent := math.Min(rsWeight, *ticker.ChangePct/8.0*rsWeight)
			score += math.Max(0, rsComponent)
			if rsComponent > 0 {
				cand.ScoreReasons = append(cand.ScoreReasons, fmt.Sprintf("relative strength component=%.2f", rsComponent))
			}
		}
	} else {
		cand.Warnings = append(cand.Warnings, "missing 24h percentage change")
	}

	// BTC relative strength vs alt 24h change (up to 1.5 pts)
	if btcChange24h != nil && ticker.ChangePct != nil {
		rs := *ticker.ChangePct - *btcChange24h
		rsWeight := getWeight(weights, "btcRelativeStrength", 1.5)
		rsComponent := math.Min(rsWeight, math.Max(0, rs)/5.0*rsWeight)
		if rsComponent > 0 {
			score += rsComponent
			cand.ScoreReasons = append(cand.ScoreReasons, fmt.Sprintf("btc relative strength component=%.2f", rsComponent))
		}
	}

	if watchBoost > 0 {
		score += watchBoost
		cand.ScoreReasons = append(cand.ScoreReasons, fmt.Sprintf("watch hint boost=%.2f", watchBoost))
	}

	// 6. CoinGlass RSI hotness (up to 1 pt) — optional third-party context
	var rsi *data.CoinRSI
	if rsiMap != nil {
		if coin, err := data.NormalizeCoinSymbol(symbol); err == nil {
			if row, ok := rsiMap[coin]; ok {
				r := row
				rsi = &r
			} else {
				cand.Warnings = append(cand.Warnings, "CoinGlass RSI unavailable for symbol")
			}
		}
	}
	if rsi != nil && rsi.RSI4h > 0 {
		rsiWeight := getWeight(weights, "rsiHotness", 1.0)
		rsiComponent := data.RSIHotnessScore(rsi.RSI4h, rsiWeight)
		if rsiComponent > 0 {
			score += rsiComponent
			cand.ScoreReasons = append(cand.ScoreReasons, fmt.Sprintf("rsi4h attention component=%.2f (rsi4h=%.1f)", rsiComponent, rsi.RSI4h))
		}
	}

	// 4. Open interest (up to 1 pt)
	if ticker.OpenInterest != nil && *ticker.OpenInterest > 0 {
		oiWeight := getWeight(weights, "openInterest", 1.0)
		score += math.Min(oiWeight, oiWeight*0.5)
		cand.ScoreReasons = append(cand.ScoreReasons, "open interest available")
	} else {
		cand.Warnings = append(cand.Warnings, "missing open interest data; confidence degraded")
	}

	// 5. Funding rate (up to 1 pt)
	if ticker.FundingRate != nil {
		fundingWeight := getWeight(weights, "funding", 1.0)
		fundingComponent := fundingWeight * 0.5
		if math.Abs(*ticker.FundingRate) > 0.001 {
			cand.Warnings = append(cand.Warnings, "funding rate is elevated; crowded positioning risk")
			fundingComponent *= 0.5
		}
		score += fundingComponent
		cand.ScoreReasons = append(cand.ScoreReasons, fmt.Sprintf("funding component=%.2f", fundingComponent))
	} else {
		cand.Warnings = append(cand.Warnings, "missing funding data; confidence degraded")
	}

	if ticker.LastPrice != nil {
		cand.LastPrice = ticker.LastPrice
	}

	cand.CandidateScore = math.Round(math.Min(score, 10.0)*100) / 100
	return cand
}

// ---------------------------------------------------------------------------
// Setup scoring  (ported from MarketScanner._score_direction)
// Weighted multi-factor scoring — the heart of the system.
// ---------------------------------------------------------------------------

func (s *MarketScanner) scoreDirection(
	direction types.Direction,
	symbol string,
	candidate types.Candidate,
	dailyTrend string,
	structure map[string]any,
	currentPrice float64,
	volumeConfirmed bool,
	btcCtx *btcContext,
) types.Setup {
	score := 0.0
	reasons := []string{}
	warnings := []string{}
	weights := s.config.Scoring.SetupWeights
	phase := string(btcCtx.Cycle.Phase)
	btcTrend := btcCtx.Cycle.BtcTrend

	// 1. Daily trend alignment (up to 1.5 pts)
	if (direction == types.DirectionLong && dailyTrend == "up") ||
		(direction == types.DirectionShort && dailyTrend == "down") {
		score += getWeight(weights, "dailyTrend", 1.5)
		reasons = append(reasons, fmt.Sprintf("1d trend supports %s", string(direction)))
	} else if dailyTrend == "sideways" {
		score += getWeight(weights, "dailyTrend", 1.5) * 0.4
		reasons = append(reasons, "1d trend is sideways")
	} else {
		warnings = append(warnings, fmt.Sprintf("1d trend conflicts with %s", string(direction)))
	}

	// 2. Structure readiness (up to 2 pts)
	if getMapBool(structure, "ready") {
		score += getWeight(weights, "structure", 2.0)
		reasons = append(reasons, fmt.Sprintf("4h %s structure is usable", getMapString(structure, "type")))
	} else {
		warnings = append(warnings, "4h structure is not usable")
	}

	// 3. Entry trigger / price proximity to structure boundary (up to 2 pts)
	risk := s.computeRiskBounds(direction, symbol, structure, currentPrice, phase, btcTrend)
	if risk.Triggered {
		score += getWeight(weights, "entryTrigger", 2.0)
		reasons = append(reasons, "15m trigger is active near structure boundary")
	} else if risk.NearTrigger {
		score += getWeight(weights, "entryTrigger", 2.0) * 0.5
		reasons = append(reasons, "15m price is near trigger; prepare only")
	}

	// 4. BTC resonance (up to 1 pt)
	btcSupports := (direction == types.DirectionLong && btcTrend == "up") ||
		(direction == types.DirectionShort && btcTrend == "down")
	if strings.HasPrefix(symbol, "BTC/") {
		score += getWeight(weights, "btcResonance", 1.0)
		reasons = append(reasons, "BTC setup has no separate BTC resonance requirement")
	} else if btcSupports {
		score += getWeight(weights, "btcResonance", 1.0)
		reasons = append(reasons, "BTC resonance supports direction")
	} else if btcTrend == "sideways" {
		score += getWeight(weights, "btcResonance", 1.0) * 0.4
		warnings = append(warnings, "BTC resonance is neutral")
	} else {
		warnings = append(warnings, "BTC resonance conflicts with setup direction")
	}

	// 5. Market cycle alignment (up to 1 pt)
	cycleComponent := s.cycleComponent(direction, phase)
	score += cycleComponent
	if cycleComponent > 0 {
		reasons = append(reasons, fmt.Sprintf("cycle component=%.2f", cycleComponent))
	} else {
		warnings = append(warnings, fmt.Sprintf("%s cycle does not support %s", phase, string(direction)))
	}

	// 6. Volume confirmation (up to 1 pt)
	if volumeConfirmed {
		score += getWeight(weights, "volumeConfirmation", 1.0)
		reasons = append(reasons, "15m volume confirms move")
	} else {
		warnings = append(warnings, "15m volume confirmation missing")
	}

	// 7. Risk / reward quality (up to 1.5 pts)
	requiredRR := s.requiredRR(direction, phase, btcTrend)
	if risk.RiskReward >= requiredRR {
		score += getWeight(weights, "riskReward", 1.5)
		reasons = append(reasons, fmt.Sprintf("risk/reward %.2f meets requirement %.2f", risk.RiskReward, requiredRR))
	} else if risk.RiskReward > 0 {
		score += getWeight(weights, "riskReward", 1.5) * 0.25
		warnings = append(warnings, fmt.Sprintf("risk/reward %.2f below requirement %.2f", risk.RiskReward, requiredRR))
	} else {
		warnings = append(warnings, "structural stop or target unavailable")
	}

	// --- finalise ---
	threshold := s.threshold(direction, phase, btcTrend)
	setupScore := math.Round(math.Min(score, 10.0)*100) / 100
	actionState := s.determineActionState(setupScore, threshold, risk.RiskReward, requiredRR,
		risk.Triggered, risk.NearTrigger, candidate.CandidateScore)
	actionState, gateWarnings := s.ApplyStrategyActionGate(actionState, direction, phase, btcTrend)
	warnings = append(warnings, gateWarnings...)

	if actionState != string(types.ActionStateTradeCandidate) && setupScore >= threshold {
		warnings = append(warnings, "score threshold met but trigger/RR requirements block trade_candidate")
	}

	setupTypeStr := fmt.Sprintf("%s_%s", getMapString(structure, "type"),
		map[string]string{"long": "breakout", "short": "breakdown"}[string(direction)])
	if getMapString(structure, "entry_mode") == "support" && direction == types.DirectionLong {
		setupTypeStr = "box_support"
	}
	fp := fingerprint(symbol, string(direction), setupTypeStr, structure, risk)

	return types.Setup{
		Symbol:             symbol,
		Direction:          string(direction),
		SetupType:          &setupTypeStr,
		ActionState:        actionState,
		SetupScore:         setupScore,
		Threshold:          &threshold,
		RequiredRiskReward: &requiredRR,
		Structure:          copyMap(structure),
		Risk:               risk,
		ChartSpec:          s.chartSpec(symbol, setupScore, setupTypeStr),
		Fingerprint:        fp,
		Reasons:            reasons,
		Warnings:           warnings,
		Execution:          map[string]any{"enabled": false},
	}
}

// ---------------------------------------------------------------------------
// Risk bounds  (ported from MarketScanner._risk_bounds)
// ---------------------------------------------------------------------------

func (s *MarketScanner) computeRiskBounds(
	direction types.Direction,
	symbol string,
	structure map[string]any,
	currentPrice float64,
	phase string,
	btcTrend string,
) types.RiskBounds {
	high := getMapFloat64(structure, "high")
	low := getMapFloat64(structure, "low")
	height := getMapFloat64(structure, "height")

	symbolClass := "altcoin"
	if strings.HasPrefix(symbol, "BTC/") || strings.HasPrefix(symbol, "ETH/") {
		symbolClass = "major"
	}

	maxPositionPct := getWeight(s.config.Risk.MaxPositionPct, symbolClass, 33.0)
	maxLeverage := getWeight(s.config.Risk.MaxLeverage, symbolClass, 5.0)

	if phase == "autumn" || phase == "winter" {
		maxPositionPct *= s.config.Risk.WeakCyclePositionMultiplier
	}
	if direction == types.DirectionShort {
		maxPositionPct *= s.config.Risk.ShortPositionMultiplier
		if btcTrend != "down" {
			maxLeverage = math.Min(maxLeverage, 3.0)
		}
	}
	if s.cycleComponent(direction, phase) == 0 {
		maxPositionPct *= s.config.Risk.InverseCyclePositionMultiplier
	}

	var entryZone [2]float64
	var stop float64
	var targets [2]float64
	var risk float64
	triggered := false
	nearTrigger := false
	invalidation := ""
	entryMode := getMapString(structure, "entry_mode")

	if direction == types.DirectionLong && entryMode == "support" {
		entryZone = [2]float64{low * 0.997, low * 1.003}
		entryMid := (entryZone[0] + entryZone[1]) / 2
		stop = low * 0.995
		targets = [2]float64{high, high + height}
		risk = entryMid - stop
		triggered = getMapBool(structure, "support_triggered")
		nearTrigger = getMapBool(structure, "support_near")
		invalidation = "long support setup invalid below 4h structure low"
	} else if direction == types.DirectionLong {
		entryZone = [2]float64{high, high * 1.003}
		entryMid := (entryZone[0] + entryZone[1]) / 2
		stop = low * 0.995
		targets = [2]float64{high + height, high + 2*height}
		risk = entryMid - stop
		triggered = currentPrice >= high*1.003
		nearTrigger = currentPrice >= high*0.99
		invalidation = "long setup invalid below 4h structure low"
	} else {
		entryZone = [2]float64{low * 0.997, low}
		entryMid := (entryZone[0] + entryZone[1]) / 2
		stop = high * 1.005
		targets = [2]float64{low - height, low - 2*height}
		risk = stop - entryMid
		triggered = currentPrice <= low*0.997
		nearTrigger = currentPrice <= low*1.01
		invalidation = "short setup invalid above 4h structure high"
	}

	// Build valid target list (only positive values)
	validTargets := make([]float64, 0, 2)
	for _, t := range targets {
		if t > 0 {
			validTargets = append(validTargets, math.Round(t*1e8)/1e8)
		}
	}

	var riskRewardTarget *float64
	if len(validTargets) > 0 {
		riskRewardTarget = &validTargets[len(validTargets)-1]
	}

	reward := 0.0
	if riskRewardTarget != nil {
		if direction == types.DirectionLong {
			reward = *riskRewardTarget - (entryZone[0]+entryZone[1])/2
		} else {
			reward = (entryZone[0]+entryZone[1])/2 - *riskRewardTarget
		}
	}

	rr := 0.0
	if risk > 0 && reward > 0 {
		rr = reward / risk
	}

	return types.RiskBounds{
		MaxPositionPct:  math.Round(maxPositionPct*100) / 100,
		MaxLeverage:     math.Round(maxLeverage*100) / 100,
		EntryZone:       []float64{math.Round(entryZone[0]*1e8) / 1e8, math.Round(entryZone[1]*1e8) / 1e8},
		StructuralStop:  float64Ptr(math.Round(stop*1e8) / 1e8),
		Targets:         validTargets,
		RiskReward:      math.Round(rr*100) / 100,
		RiskRewardTarget: riskRewardTarget,
		Invalidation:    strPtr(invalidation),
		Triggered:       triggered,
		NearTrigger:     nearTrigger,
		AccountSizing:   false,
	}
}

// ---------------------------------------------------------------------------
// Cycle component  (ported from _cycle_component)
// ---------------------------------------------------------------------------

func (s *MarketScanner) cycleComponent(direction types.Direction, phase string) float64 {
	weight := getWeight(s.config.Scoring.SetupWeights, "marketCycle", 1.0)
	if direction == types.DirectionLong && (phase == "spring" || phase == "summer") {
		return weight
	}
	if direction == types.DirectionShort && phase == "winter" {
		return weight
	}
	if phase == "autumn" {
		return weight * 0.4
	}
	return 0.0
}

// ---------------------------------------------------------------------------
// Required risk/reward  (ported from _required_rr)
// ---------------------------------------------------------------------------

func (s *MarketScanner) requiredRR(direction types.Direction, phase, btcTrend string) float64 {
	if direction == types.DirectionShort && (phase != "winter" || btcTrend != "down") {
		return s.config.Scoring.StrictRiskReward
	}
	if phase == "winter" && direction == types.DirectionLong {
		return s.config.Scoring.StrictRiskReward
	}
	return s.config.Scoring.MinimumRiskReward
}

// ---------------------------------------------------------------------------
// Threshold  (ported from _threshold)
// ---------------------------------------------------------------------------

func (s *MarketScanner) threshold(direction types.Direction, phase, btcTrend string) float64 {
	threshold := getWeight(s.config.Scoring.CycleThresholds, phase, 7.5)
	if direction == types.DirectionShort && btcTrend != "down" {
		threshold += s.config.Scoring.ShortThresholdPremium
	}
	return threshold
}

// ---------------------------------------------------------------------------
// Action state machine  (ported from _action_state)
// ---------------------------------------------------------------------------

func (s *MarketScanner) determineActionState(
	setupScore, threshold, riskReward, requiredRR float64,
	triggered, nearTrigger bool,
	candidateScore float64,
) string {
	if setupScore >= threshold && riskReward >= requiredRR && triggered {
		return string(types.ActionStateTradeCandidate)
	}
	if setupScore >= threshold-1.0 && nearTrigger {
		return string(types.ActionStatePrepare)
	}
	if candidateScore >= 2.0 {
		return string(types.ActionStateWatch)
	}
	return string(types.ActionStateNoTrade)
}

// ApplyStrategyActionGate downgrades trade_candidate when market cycle policy
// from docs/trading-system.md conflicts with a deterministic pass.
func (s *MarketScanner) ApplyStrategyActionGate(
	state string,
	direction types.Direction,
	phase, btcTrend string,
) (string, []string) {
	if state != string(types.ActionStateTradeCandidate) {
		return state, nil
	}
	var w []string
	if phase == "winter" && direction == types.DirectionLong {
		w = append(w, "winter cycle: long trade_candidate withheld (strategy: hibernate)")
		return string(types.ActionStatePrepare), w
	}
	if phase == "autumn" && direction == types.DirectionLong && btcTrend != "up" {
		w = append(w, "autumn non-resonance: long trade_candidate downgraded to prepare")
		return string(types.ActionStatePrepare), w
	}
	return state, nil
}

// ---------------------------------------------------------------------------
// Chart spec  (ported from _chart_spec)
// ---------------------------------------------------------------------------

func (s *MarketScanner) chartSpec(symbol string, setupScore float64, setupType string) map[string]any {
	timeframes := []string{"15m"}
	if setupScore >= s.config.Charts.MultiTimeframeScoreThreshold || !strings.Contains(setupType, "range") {
		timeframes = []string{"1d", "4h", "15m"}
	}
	return map[string]any{
		"symbol":              symbol,
		"timeframes":          timeframes,
		"default_chart_count": s.config.Charts.DefaultChartCount,
		"overlays":            []string{"structure", "entry_zone", "structural_stop", "targets"},
		"generate_now":        s.config.Scanner.GenerateChartsByDefault,
		"output_path":         s.config.Charts.OutputPath,
	}
}

// ---------------------------------------------------------------------------
// ptr helpers
// ---------------------------------------------------------------------------

func float64Ptr(v float64) *float64 {
	return &v
}

func strPtr(s string) *string {
	return &s
}

// ---------------------------------------------------------------------------
// map shallow copy
// ---------------------------------------------------------------------------

func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
