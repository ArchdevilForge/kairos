package types

import "strings"

// Schema versions for decision desk contracts.
const (
	DecisionTicketSchemaVersion = "decision_ticket.v1"
	DecisionRecordSchemaVersion = "decision_record.v1"
)

// ExitReason is the structured close-out classification on a ticket/journal record (Work Item 11).
type ExitReason string

const (
	ExitReasonProtectiveStop  ExitReason = "PROTECTIVE_STOP"
	ExitReasonFakeoutAbort    ExitReason = "FAKEOUT_ABORT"
	ExitReasonStructuralTrail ExitReason = "STRUCTURAL_TRAIL"
	ExitReasonTargetPartial   ExitReason = "TARGET_PARTIAL"
	ExitReasonImpulseEarly    ExitReason = "IMPULSE_EARLY"
)

var validExitReasons = []ExitReason{
	ExitReasonProtectiveStop,
	ExitReasonFakeoutAbort,
	ExitReasonStructuralTrail,
	ExitReasonTargetPartial,
	ExitReasonImpulseEarly,
}

// ValidExitReasons returns all canonical exit reasons in single-key menu order.
func ValidExitReasons() []ExitReason {
	return append([]ExitReason(nil), validExitReasons...)
}

// IsValidExitReason reports whether r is a canonical ExitReason.
func IsValidExitReason(r ExitReason) bool {
	for _, v := range validExitReasons {
		if r == v {
			return true
		}
	}
	return false
}

// ParseExitReasonKey parses a single-key shortcut ("1".."5" or "p"/"f"/"s"/"t"/"i")
// or full enum name into a canonical ExitReason.
func ParseExitReasonKey(input string) (ExitReason, bool) {
	s := strings.TrimSpace(strings.ToUpper(input))
	switch s {
	case "1", "P", string(ExitReasonProtectiveStop):
		return ExitReasonProtectiveStop, true
	case "2", "F", string(ExitReasonFakeoutAbort):
		return ExitReasonFakeoutAbort, true
	case "3", "S", string(ExitReasonStructuralTrail):
		return ExitReasonStructuralTrail, true
	case "4", "T", string(ExitReasonTargetPartial):
		return ExitReasonTargetPartial, true
	case "5", "I", string(ExitReasonImpulseEarly):
		return ExitReasonImpulseEarly, true
	default:
		return "", false
	}
}

// HumanDecision is the final human action on a ticket.
type HumanDecision string

const (
	DecisionAccepted HumanDecision = "accepted"
	DecisionWaiting  HumanDecision = "waiting"
	DecisionRejected HumanDecision = "rejected"
	DecisionMissed   HumanDecision = "missed"
)

// Standard reason codes for human decisions (free text alone is not enough).
// Canonical (TRADING_SYSTEM_CANONICAL §8): reason codes are required.
const (
	ReasonStructureGood     = "structure_good"
	ReasonStructureBad      = "structure_bad"
	ReasonTooExtended       = "too_extended"
	ReasonNotRealLeader     = "not_real_leader"
	ReasonInsufficientRoom  = "insufficient_room"
	ReasonCounterTrend      = "counter_trend"
	ReasonMarketBreadthWeak = "market_breadth_weak"
	ReasonFundingCrowded    = "funding_crowded"
	ReasonLateToEvent       = "late_to_event"
	ReasonEmotionalSkip     = "emotional_skip"
	ReasonFearOfLoss        = "fear_of_loss"
	ReasonManualOverride    = "manual_override"
)

// standardReasonCodes is the single source of truth for valid codes.
var standardReasonCodes = []string{
	ReasonStructureGood, ReasonStructureBad, ReasonTooExtended,
	ReasonNotRealLeader, ReasonInsufficientRoom, ReasonCounterTrend,
	ReasonMarketBreadthWeak, ReasonFundingCrowded, ReasonLateToEvent,
	ReasonEmotionalSkip, ReasonFearOfLoss, ReasonManualOverride,
}

// ValidReasonCodes returns the canonical reason codes.
func ValidReasonCodes() []string {
	return append([]string(nil), standardReasonCodes...)
}

// IsValidReasonCode reports whether c is a canonical reason code.
func IsValidReasonCode(c string) bool {
	for _, v := range standardReasonCodes {
		if c == v {
			return true
		}
	}
	return false
}

// TicketStatus is the desk lifecycle of a DecisionTicket.
type TicketStatus string

const (
	TicketStatusOpen     TicketStatus = "open"
	TicketStatusAccepted TicketStatus = "accepted"
	TicketStatusWaiting  TicketStatus = "waiting"
	TicketStatusRejected TicketStatus = "rejected"
	TicketStatusMissed   TicketStatus = "missed"
	TicketStatusClosed   TicketStatus = "closed"
	TicketStatusExpired  TicketStatus = "expired"
)

// Risk template IDs (numeric budgets live in config, not code constants).
const (
	RiskTemplateAlignedSpring = "aligned_spring"
	RiskTemplateAlignedSummer = "aligned_summer"
	RiskTemplateAlignedAutumn = "aligned_autumn"
	RiskTemplateCounterTrend  = "counter_trend"
	RiskTemplateMixedContext  = "mixed_context"
	RiskTemplateNoTrade       = "no_trade"
)

// EntryPlan is the structured entry intent on a ticket.
type EntryPlan struct {
	Mode     string   `json:"mode" yaml:"mode"` // e.g. closed_bar | intrabar
	ZoneLow  float64  `json:"zone_low" yaml:"zone_low"`
	ZoneHigh float64  `json:"zone_high" yaml:"zone_high"`
	Trigger  string   `json:"trigger" yaml:"trigger"`
	Notes    []string `json:"notes,omitempty" yaml:"notes,omitempty"`
}

// RiskPlan sizes by max loss, not position% first.
//
//	loss_budget = equity × risk_budget_pct
//	notional    = min(tranche_base_usd × max_asset_lev, loss_budget / effective_stop_pct)
type RiskPlan struct {
	TemplateID string `json:"template_id" yaml:"template_id"`

	RiskBudgetPct    float64 `json:"risk_budget_pct" yaml:"risk_budget_pct"`
	EntryPrice       float64 `json:"entry_price" yaml:"entry_price"`
	StopPrice        float64 `json:"stop_price" yaml:"stop_price"`
	StopDistancePct  float64 `json:"stop_distance_pct" yaml:"stop_distance_pct"`
	EffectiveStopPct float64 `json:"effective_stop_pct,omitempty" yaml:"effective_stop_pct,omitempty"`

	SuggestedNotional float64 `json:"suggested_notional" yaml:"suggested_notional"`
	MaxNotional       float64 `json:"max_notional" yaml:"max_notional"`

	MaxLeverage    float64 `json:"max_leverage" yaml:"max_leverage"`
	TrancheBaseUSD float64 `json:"tranche_base_usd,omitempty" yaml:"tranche_base_usd,omitempty"`
	AddOnAllowed   bool    `json:"add_on_allowed" yaml:"add_on_allowed"`
}

// DecisionTicket is the human-facing structured decision card.
type DecisionTicket struct {
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`

	ID            string `json:"id" yaml:"id"`
	SessionID     string `json:"session_id" yaml:"session_id"`
	WaveClusterID string `json:"wave_cluster_id,omitempty" yaml:"wave_cluster_id,omitempty"`
	Symbol        string `json:"symbol" yaml:"symbol"`
	PlaybookID    string `json:"playbook_id" yaml:"playbook_id"`

	Direction  CycleDirection `json:"direction" yaml:"direction"`
	Grade      TicketGrade    `json:"grade" yaml:"grade"`
	TradeClass TradeClass     `json:"trade_class" yaml:"trade_class"`

	MarketCycle CycleMap `json:"market_cycle" yaml:"market_cycle"`
	SymbolCycle CycleMap `json:"symbol_cycle" yaml:"symbol_cycle"`

	EntryPlan EntryPlan `json:"entry_plan" yaml:"entry_plan"`
	RiskPlan  RiskPlan  `json:"risk_plan" yaml:"risk_plan"`

	Reasons       []string `json:"reasons" yaml:"reasons"`
	Warnings      []string `json:"warnings" yaml:"warnings"`
	Invalidations []string `json:"invalidations" yaml:"invalidations"`

	Status TicketStatus `json:"status" yaml:"status"`

	// Phase 2c shadow-only advisory fields (owner decision 2026-11-11).
	// Populated ONLY when genuinely computed from faithful cross-sectional inputs;
	// omitted when unmeasured. Never alters sizing, filtering, or alert suppression.
	SeasonRegime      SeasonRegime `json:"season_regime,omitempty" yaml:"season_regime,omitempty"`
	WinterDefenseHint string       `json:"winter_defense_hint,omitempty" yaml:"winter_defense_hint,omitempty"`

	// Additive close-out metadata (Work Item 11).
	ExitReason         ExitReason `json:"exit_reason,omitempty" yaml:"exit_reason,omitempty"`
	RealizedFundingUSD float64    `json:"realized_funding_usd,omitempty" yaml:"realized_funding_usd,omitempty"`
	ExitPrice          float64    `json:"exit_price,omitempty" yaml:"exit_price,omitempty"`
	ClosedAt           int64      `json:"closed_at,omitempty" yaml:"closed_at,omitempty"`

	// Timestamps (unix seconds) — required for honest forward outcome paths.
	CreatedAt        int64 `json:"created_at" yaml:"created_at"`
	SignalAt         int64 `json:"signal_at" yaml:"signal_at"`                                       // pulse/session clock
	EntryTriggeredAt int64 `json:"entry_triggered_at,omitempty" yaml:"entry_triggered_at,omitempty"` // mechanical trigger bar
}

// DecisionRecord is the persisted human decision on a ticket.
type DecisionRecord struct {
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`

	TicketID string        `json:"ticket_id" yaml:"ticket_id"`
	Decision HumanDecision `json:"decision" yaml:"decision"`

	ReasonCodes []string `json:"reason_codes" yaml:"reason_codes"`
	Note        string   `json:"note" yaml:"note"`

	PlannedEntry *float64 `json:"planned_entry,omitempty" yaml:"planned_entry,omitempty"`
	PlannedStop  *float64 `json:"planned_stop,omitempty" yaml:"planned_stop,omitempty"`
	PlannedRiskR *float64 `json:"planned_risk_r,omitempty" yaml:"planned_risk_r,omitempty"`

	// Additive close-out metadata (Work Item 11).
	ExitReason         ExitReason `json:"exit_reason,omitempty" yaml:"exit_reason,omitempty"`
	RealizedFundingUSD float64    `json:"realized_funding_usd,omitempty" yaml:"realized_funding_usd,omitempty"`
	ExitPrice          float64    `json:"exit_price,omitempty" yaml:"exit_price,omitempty"`
	ClosedAt           int64      `json:"closed_at,omitempty" yaml:"closed_at,omitempty"`
	TouchedPresetStop  bool       `json:"touched_preset_stop,omitempty" yaml:"touched_preset_stop,omitempty"`
	FakeoutConfirmed   bool       `json:"fakeout_confirmed,omitempty" yaml:"fakeout_confirmed,omitempty"`
	MarketStressActive bool       `json:"market_stress_active,omitempty" yaml:"market_stress_active,omitempty"`
	StructureIntact    bool       `json:"structure_intact,omitempty" yaml:"structure_intact,omitempty"`

	DecidedAt int64 `json:"decided_at" yaml:"decided_at"`
}
