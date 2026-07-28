// Package decision builds human-facing Decision Tickets. No order placement.
package decision

import (
	"fmt"
	"math"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// RiskBudgets maps template → default risk_budget_pct of equity.
// Override via Config; these are desk defaults only.
type RiskBudgets map[string]float64

// DefaultRiskBudgets returns conservative stage-1 budgets.
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
	TicketID string
	Match    types.PlaybookMatch
	Context  types.PlaybookContext
	// Invalidations copied onto the ticket (required for open tickets).
	Invalidations []string

	EntryPrice float64
	StopPrice  float64
	// Equity for notional suggestion; 0 → leave notionals 0.
	Equity float64
	// Budgets optional; nil → DefaultRiskBudgets.
	Budgets RiskBudgets

	// MaxLeverage cap (0 → 5).
	MaxLeverage float64
	// Entry plan fields
	EntryMode string
	ZoneLow   float64
	ZoneHigh  float64
	Trigger   string

	CreatedAt        int64
	SignalAt         int64
	EntryTriggeredAt int64
}

// BuildTicket creates a DecisionTicket from a playbook match.
// Unmatched → status stays open but grade D / no_trade risk; still persistable.
func BuildTicket(in BuildInput) types.DecisionTicket {
	budgets := in.Budgets
	if budgets == nil {
		budgets = DefaultRiskBudgets()
	}
	lev := in.MaxLeverage
	if lev <= 0 {
		lev = 5
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
		TemplateID:    template,
		RiskBudgetPct: budgetPct,
		EntryPrice:    in.EntryPrice,
		StopPrice:     in.StopPrice,
		MaxLeverage:   lev,
		AddOnAllowed:  addOn && in.Match.Matched,
	}
	if in.EntryPrice > 0 && in.StopPrice > 0 {
		dist := math.Abs(in.EntryPrice-in.StopPrice) / in.EntryPrice * 100
		risk.StopDistancePct = round2(dist)
		if dist > 0 && in.Equity > 0 && budgetPct > 0 {
			lossBudget := in.Equity * (budgetPct / 100.0)
			// budgetPct stored as percent of equity (0.75 = 0.75%)
			notional := lossBudget / (dist / 100.0)
			risk.SuggestedNotional = round2(notional)
			risk.MaxNotional = round2(notional * 1.25)
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
	if len(inv) == 0 && in.Match.Matched {
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

	return types.DecisionTicket{
		SchemaVersion: types.DecisionTicketSchemaVersion,
		ID:            in.TicketID,
		SessionID:     in.Match.SessionID,
		Symbol:        in.Match.Symbol,
		PlaybookID:    in.Match.PlaybookID,
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
		RiskPlan:         risk,
		Reasons:          reasons,
		Warnings:         warnings,
		Invalidations:    inv,
		Status:           status,
		CreatedAt:        created,
		SignalAt:         signalAt,
		EntryTriggeredAt: in.EntryTriggeredAt,
	}
}

// FormatTicket returns a compact human card (Telegram/CLI).
func FormatTicket(t types.DecisionTicket) string {
	dir := string(t.Direction)
	addOn := "no"
	if t.RiskPlan.AddOnAllowed {
		addOn = "yes"
	}
	return fmt.Sprintf(
		"Ticket %s  %s  %s/%s  grade=%s  playbook=%s\n"+
			"class=%s  risk=%s  budget=%.2f%%  stop_dist=%.2f%%  add_on=%s\n"+
			"reasons: %v\nwarnings: %v\ninvalidations: %v",
		t.ID, t.Symbol, dir, t.TradeClass, t.Grade, t.PlaybookID,
		t.TradeClass, t.RiskPlan.TemplateID, t.RiskPlan.RiskBudgetPct, t.RiskPlan.StopDistancePct, addOn,
		t.Reasons, t.Warnings, t.Invalidations,
	)
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
