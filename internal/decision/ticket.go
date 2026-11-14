// Package decision builds human-facing Decision Tickets. No order placement.
package decision

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
)

const (
	// DefaultMajorLeverageCap enforces BTC/ETH <= 10x (Work Item 7).
	DefaultMajorLeverageCap = 10.0
	// DefaultAltLeverageCap enforces alts <= 5x (Work Item 7).
	DefaultAltLeverageCap = 5.0
	// DefaultTrancheBaseUSD is the default fixed tranche base in USD (Work Item 10).
	DefaultTrancheBaseUSD = 10_000.0
)

// IsMajorAsset reports whether symbol is BTC or ETH (perps or spot notation).
func IsMajorAsset(symbol string) bool {
	s := strings.ToUpper(strings.TrimSpace(symbol))
	for _, sep := range []string{"/", "-", ":", "_"} {
		if idx := strings.Index(s, sep); idx > 0 {
			s = s[:idx]
			break
		}
	}
	s = strings.TrimSuffix(s, "USDT")
	s = strings.TrimSuffix(s, "USDC")
	s = strings.TrimSuffix(s, "USD")
	s = strings.TrimSuffix(s, "PERP")
	return s == "BTC" || s == "ETH"
}

// MaxAssetLeverage returns the hard leverage cap for symbol (BTC/ETH <= 10x, alts <= 5x).
func MaxAssetLeverage(symbol string, majorCap, altCap float64) float64 {
	if majorCap <= 0 {
		majorCap = DefaultMajorLeverageCap
	}
	if altCap <= 0 {
		altCap = DefaultAltLeverageCap
	}
	if IsMajorAsset(symbol) {
		return majorCap
	}
	return altCap
}

// ComputeSingleTicketNotional computes:
//
//	Notional = min(TrancheBaseUSD * MaxAssetLev, RiskBudgetUSD / EffectiveStopPct)
//
// where effectiveStopPct is expressed in percent (e.g. 2.0 means 2% stop distance).
func ComputeSingleTicketNotional(trancheBaseUSD, maxAssetLev, riskBudgetUSD, effectiveStopPct float64) float64 {
	if trancheBaseUSD <= 0 {
		trancheBaseUSD = DefaultTrancheBaseUSD
	}
	if maxAssetLev <= 0 {
		maxAssetLev = DefaultAltLeverageCap
	}
	capNotional := trancheBaseUSD * maxAssetLev
	if effectiveStopPct <= 0 || riskBudgetUSD <= 0 {
		return 0
	}
	riskNotional := riskBudgetUSD / (effectiveStopPct / 100.0)
	return round2(math.Min(capNotional, riskNotional))
}

// RiskBudgets maps risk template → risk_budget_pct of equity.
// Canonical §9: numeric pct lives in config, not code constants — these are
// fallback defaults only, and the opportunity path always passes config values.
type RiskBudgets map[string]float64

// DefaultRiskBudgets returns conservative stage-1 fallback budgets.
func DefaultRiskBudgets() RiskBudgets {
	return RiskBudgets{
		types.RiskTemplateAlignedSpring: 0.75,
		types.RiskTemplateAlignedSummer: 0.75,
		types.RiskTemplateAlignedAutumn: 0.40,
		types.RiskTemplateCounterTrend:  0.25,
		types.RiskTemplateMixedContext:  0.0,
		types.RiskTemplateNoTrade:       0.0,
	}
}

// BuildInput carries prices for risk sizing.
type BuildInput struct {
	TicketID      string
	WaveClusterID string
	Match         types.PlaybookMatch
	Context       types.PlaybookContext
	// SetupPoint is optional (§3 6-point enum); NEVER fabricated when unset.
	SetupPoint types.SetupPoint
	// StopBasis is optional explicit structural stop basis text; falls back to Invalidations when unset.
	StopBasis string
	// Invalidations copied onto the ticket (required for open tickets).
	Invalidations []string

	EntryPrice float64
	StopPrice  float64
	// Equity for notional suggestion; 0 → leave notionals 0.
	Equity float64
	// TrancheBaseUSD for single-ticket sizing (0 → Equity if >0 else DefaultTrancheBaseUSD).
	TrancheBaseUSD float64
	// SlippageBufferPct optional pre-entry slippage buffer in percent added to EffectiveStopPct.
	SlippageBufferPct float64
	// Budgets optional; nil → DefaultRiskBudgets.
	Budgets RiskBudgets

	// MaxLeverage cap (0 → asset cap: BTC/ETH 10x, alts 5x).
	MaxLeverage      float64
	MaxMajorLeverage float64
	MaxAltLeverage   float64
	// Entry plan fields
	EntryMode string
	ZoneLow   float64
	ZoneHigh  float64
	Trigger   string

	CreatedAt        int64
	SignalAt         int64
	EntryTriggeredAt int64

	// SeasonRegime is the Phase 2c causal 4-season market state (SPRING/SUMMER/AUTUMN/WINTER).
	// Shadow advisory only: never alters RiskPlan, sizing, or filtering.
	SeasonRegime types.SeasonRegime
}

// BuildTicket creates a DecisionTicket from a playbook match.
// Unmatched → status stays open but grade D / no_trade risk; still persistable.
func BuildTicket(in BuildInput) types.DecisionTicket {
	budgets := in.Budgets
	if budgets == nil {
		budgets = DefaultRiskBudgets()
	}
	assetCap := MaxAssetLeverage(in.Match.Symbol, in.MaxMajorLeverage, in.MaxAltLeverage)
	lev := in.MaxLeverage
	if lev <= 0 || lev > assetCap {
		lev = assetCap
	}
	trancheBase := in.TrancheBaseUSD
	if trancheBase <= 0 {
		if in.Equity > 0 {
			trancheBase = in.Equity
		} else {
			trancheBase = DefaultTrancheBaseUSD
		}
	}
	mode := in.EntryMode
	if mode == "" {
		mode = "closed_bar"
	}

	template := in.Match.RiskTemplate
	if template == "" {
		template = types.RiskTemplateNoTrade
	}
	addOn := template == types.RiskTemplateAlignedSpring || template == types.RiskTemplateAlignedSummer
	if in.Match.TradeClass == types.TradeClassCounterTrendLong ||
		in.Match.TradeClass == types.TradeClassCounterTrendShort {
		addOn = false
		template = types.RiskTemplateCounterTrend
	}

	budgetPct := budgets[template]
	risk := types.RiskPlan{
		TemplateID:     template,
		RiskBudgetPct:  budgetPct,
		EntryPrice:     in.EntryPrice,
		StopPrice:      in.StopPrice,
		MaxLeverage:    lev,
		TrancheBaseUSD: trancheBase,
		AddOnAllowed:   addOn && in.Match.Matched,
	}
	if in.EntryPrice > 0 && in.StopPrice > 0 {
		dist := math.Abs(in.EntryPrice-in.StopPrice) / in.EntryPrice * 100
		effectiveStopPct := dist + math.Max(0, in.SlippageBufferPct)
		risk.StopDistancePct = round2(dist)
		risk.EffectiveStopPct = round2(effectiveStopPct)
		if effectiveStopPct > 0 && in.Equity > 0 && budgetPct > 0 {
			lossBudget := in.Equity * (budgetPct / 100.0)
			// Work Item 10: Notional = min(TrancheBaseUSD * MaxAssetLev, RiskBudgetUSD / EffectiveStopPct)
			notional := ComputeSingleTicketNotional(trancheBase, assetCap, lossBudget, effectiveStopPct)
			risk.SuggestedNotional = notional
			risk.MaxNotional = round2(math.Min(trancheBase*assetCap, notional*1.25))
		}
	}

	inv := append([]string{}, in.Invalidations...)
	status := types.TicketStatusOpen
	if !in.Match.Matched || in.Match.Grade == types.TicketGradeD {
		// still open as a rejected-by-system card? mark open with D — desk may record missed
		status = types.TicketStatusOpen
	}

	reasons := append([]string{}, in.Match.Reasons...)
	warnings := append([]string{}, in.Match.Warnings...)
	stopBasis := strings.TrimSpace(in.StopBasis)
	if len(inv) == 0 && stopBasis == "" && in.Match.Matched {
		warnings = append(warnings, "ticket missing invalidation lines")
	}

	created := in.CreatedAt
	if created == 0 {
		created = time.Now().Unix()
	}
	signalAt := in.SignalAt
	if signalAt == 0 {
		signalAt = created
	}

	seasonRegime := types.NormalizeSeasonRegime(in.SeasonRegime)
	winterHint := types.WinterDefenseHintFor(seasonRegime)
	setupPoint := types.NormalizeSetupPoint(in.SetupPoint)

	return types.DecisionTicket{
		SchemaVersion: types.DecisionTicketSchemaVersion,
		ID:            in.TicketID,
		SessionID:     in.Match.SessionID,
		WaveClusterID: in.WaveClusterID,
		Symbol:        in.Match.Symbol,
		PlaybookID:    in.Match.PlaybookID,
		SetupPoint:    setupPoint,
		Direction:     in.Match.Direction,
		Grade:         in.Match.Grade,
		TradeClass:    in.Match.TradeClass,
		MarketCycle:   in.Context.MarketCycle,
		SymbolCycle:   in.Context.SymbolCycle,
		EntryPlan: types.EntryPlan{
			Mode:     mode,
			ZoneLow:  in.ZoneLow,
			ZoneHigh: in.ZoneHigh,
			Trigger:  in.Trigger,
		},
		RiskPlan:          risk,
		StopBasis:         stopBasis,
		Reasons:           reasons,
		Warnings:          warnings,
		Invalidations:     inv,
		Status:            status,
		SeasonRegime:      seasonRegime,
		WinterDefenseHint: winterHint,
		CreatedAt:         created,
		SignalAt:          signalAt,
		EntryTriggeredAt:  in.EntryTriggeredAt,
	}
}

// FormatTicketWithWarnings renders a compact human card (CLI/desk) including A2 stop+basis+1R, A3 tranche plan, and A1/A4 non-blocking warnings.
func FormatTicketWithWarnings(t types.DecisionTicket, behaviorWarnings []string) string {
	dir := string(t.Direction)
	addOn := "no"
	if t.RiskPlan.AddOnAllowed {
		addOn = "yes"
	}
	setupHeader := ""
	if norm := types.NormalizeSetupPoint(t.SetupPoint); norm != "" {
		setupHeader = fmt.Sprintf("  setup=%s(%s)", norm, types.SetupPointLabelZH(norm))
	}
	basis := t.StopBasisText()
	if basis == "" {
		basis = "(missing)"
	}
	oneRPct := t.OneRPct()
	oneRPrice := t.OneRPriceDistance()
	trancheBase := t.RiskPlan.TrancheBaseUSD
	if trancheBase <= 0 {
		trancheBase = DefaultTrancheBaseUSD
	}
	maxLev := t.RiskPlan.MaxLeverage
	if maxLev <= 0 {
		maxLev = MaxAssetLeverage(t.Symbol, DefaultMajorLeverageCap, DefaultAltLeverageCap)
	}

	lines := []string{
		fmt.Sprintf("Ticket %s  %s  %s/%s  grade=%s  playbook=%s%s",
			t.ID, t.Symbol, dir, t.TradeClass, t.Grade, t.PlaybookID, setupHeader),
		fmt.Sprintf("class=%s  risk=%s  budget=%.2f%%  stop_dist=%.2f%%  add_on=%s",
			t.TradeClass, t.RiskPlan.TemplateID, t.RiskPlan.RiskBudgetPct, oneRPct, addOn),
		fmt.Sprintf("stop: entry=%.4f  stop_price=%.4f  1R=%.4f (%.2f%%)  basis=%s",
			t.RiskPlan.EntryPrice, t.RiskPlan.StopPrice, oneRPrice, oneRPct, basis),
		fmt.Sprintf("tranche: base=$%.2f  max_lev=%.1fx  suggested_notional=$%.2f  max_notional=$%.2f",
			trancheBase, maxLev, t.RiskPlan.SuggestedNotional, t.RiskPlan.MaxNotional),
	}
	if t.WinterDefenseHint != "" {
		lines = append(lines, fmt.Sprintf("winter_hint: %s", t.WinterDefenseHint))
	}
	if len(behaviorWarnings) > 0 {
		lines = append(lines, fmt.Sprintf("behavior_warnings: %s", strings.Join(behaviorWarnings, " | ")))
	}
	lines = append(lines,
		fmt.Sprintf("reasons: %v", t.Reasons),
		fmt.Sprintf("warnings: %v", t.Warnings),
		fmt.Sprintf("invalidations: %v", t.Invalidations),
	)
	return strings.Join(lines, "\n")
}

// RecordDecision attaches a human decision (pure data helper).
func RecordDecision(ticketID string, d types.HumanDecision, codes []string, note string, at int64) types.DecisionRecord {
	return types.DecisionRecord{
		SchemaVersion: types.DecisionRecordSchemaVersion,
		TicketID:      ticketID,
		Decision:      d,
		ReasonCodes:   codes,
		Note:          note,
		DecidedAt:     at,
	}
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
