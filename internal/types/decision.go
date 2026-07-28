package types

// Schema versions for decision desk contracts.
const (
	DecisionTicketSchemaVersion = "decision_ticket.v1"
	DecisionRecordSchemaVersion = "decision_record.v1"
)

// HumanDecision is the final human action on a ticket.
type HumanDecision string

const (
	DecisionAccepted HumanDecision = "accepted"
	DecisionWaiting  HumanDecision = "waiting"
	DecisionRejected HumanDecision = "rejected"
	DecisionMissed   HumanDecision = "missed"
)

// Standard reason codes for human decisions (free text alone is not enough).
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
//	notional    = loss_budget / stop_distance_pct
type RiskPlan struct {
	TemplateID string `json:"template_id" yaml:"template_id"`

	RiskBudgetPct   float64 `json:"risk_budget_pct" yaml:"risk_budget_pct"`
	EntryPrice      float64 `json:"entry_price" yaml:"entry_price"`
	StopPrice       float64 `json:"stop_price" yaml:"stop_price"`
	StopDistancePct float64 `json:"stop_distance_pct" yaml:"stop_distance_pct"`

	SuggestedNotional float64 `json:"suggested_notional" yaml:"suggested_notional"`
	MaxNotional       float64 `json:"max_notional" yaml:"max_notional"`

	MaxLeverage  float64 `json:"max_leverage" yaml:"max_leverage"`
	AddOnAllowed bool    `json:"add_on_allowed" yaml:"add_on_allowed"`
}

// DecisionTicket is the human-facing structured decision card.
type DecisionTicket struct {
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`

	ID         string `json:"id" yaml:"id"`
	SessionID  string `json:"session_id" yaml:"session_id"`
	Symbol     string `json:"symbol" yaml:"symbol"`
	PlaybookID string `json:"playbook_id" yaml:"playbook_id"`

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

	DecidedAt int64 `json:"decided_at" yaml:"decided_at"`
}
