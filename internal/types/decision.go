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

// SetupPoint enumerates the 6 fixed logical points from Bit浪浪 STRATEGY §3.
// Unlabeled tickets keep SetupPoint == "" (never fabricated).
type SetupPoint string

const (
	SetupPoint1Breakout    SetupPoint = "POINT_1_BREAKOUT"      // 点位 1：大级别平台/箱体突破启动
	SetupPoint2MinorDiv    SetupPoint = "POINT_2_MINOR_DIV"     // 点位 2：主升途中回踩（小分歧承接）
	SetupPoint3MidTopShort SetupPoint = "POINT_3_MID_TOP_SHORT" // 点位 3：主升途中逆势摸顶空
	SetupPoint4SecondWave  SetupPoint = "POINT_4_SECOND_WAVE"   // 点位 4：大分歧后二波
	SetupPoint5EndTopShort SetupPoint = "POINT_5_END_TOP_SHORT" // 点位 5：主升结束摸顶空
	SetupPoint6BoxBounce   SetupPoint = "POINT_6_BOX_BOUNCE"    // 点位 6：箱体内反弹
)

var validSetupPoints = []SetupPoint{
	SetupPoint1Breakout,
	SetupPoint2MinorDiv,
	SetupPoint3MidTopShort,
	SetupPoint4SecondWave,
	SetupPoint5EndTopShort,
	SetupPoint6BoxBounce,
}

// ValidSetupPoints returns all 6 canonical setup point enums in §3 order.
func ValidSetupPoints() []SetupPoint {
	return append([]SetupPoint(nil), validSetupPoints...)
}

// IsValidSetupPoint reports whether p is one of the 6 canonical §3 setup points.
func IsValidSetupPoint(p SetupPoint) bool {
	for _, v := range validSetupPoints {
		if p == v {
			return true
		}
	}
	return false
}

// NormalizeSetupPoint trims/uppercases p and returns "" if it is not a canonical SetupPoint.
func NormalizeSetupPoint(p SetupPoint) SetupPoint {
	s := SetupPoint(strings.TrimSpace(strings.ToUpper(string(p))))
	if IsValidSetupPoint(s) {
		return s
	}
	return ""
}

// ParseSetupPoint parses a canonical enum name or shorthand ("1".."6" / "POINT_1".."POINT_6").
func ParseSetupPoint(raw string) (SetupPoint, bool) {
	s := strings.TrimSpace(strings.ToUpper(raw))
	switch s {
	case "1", "POINT_1", string(SetupPoint1Breakout):
		return SetupPoint1Breakout, true
	case "2", "POINT_2", string(SetupPoint2MinorDiv):
		return SetupPoint2MinorDiv, true
	case "3", "POINT_3", string(SetupPoint3MidTopShort):
		return SetupPoint3MidTopShort, true
	case "4", "POINT_4", string(SetupPoint4SecondWave):
		return SetupPoint4SecondWave, true
	case "5", "POINT_5", string(SetupPoint5EndTopShort):
		return SetupPoint5EndTopShort, true
	case "6", "POINT_6", string(SetupPoint6BoxBounce):
		return SetupPoint6BoxBounce, true
	default:
		return "", false
	}
}

// SetupPointLabelZH returns the Chinese label for a canonical SetupPoint.
func SetupPointLabelZH(p SetupPoint) string {
	switch p {
	case SetupPoint1Breakout:
		return "点位1(平台突破启动)"
	case SetupPoint2MinorDiv:
		return "点位2(主升小分歧回踩)"
	case SetupPoint3MidTopShort:
		return "点位3(主升途中摸顶空)"
	case SetupPoint4SecondWave:
		return "点位4(大分歧后二波)"
	case SetupPoint5EndTopShort:
		return "点位5(主升结束摸顶空)"
	case SetupPoint6BoxBounce:
		return "点位6(箱体内反弹)"
	default:
		return ""
	}
}

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

	ID            string     `json:"id" yaml:"id"`
	SessionID     string     `json:"session_id" yaml:"session_id"`
	WaveClusterID string     `json:"wave_cluster_id,omitempty" yaml:"wave_cluster_id,omitempty"`
	Symbol        string     `json:"symbol" yaml:"symbol"`
	PlaybookID    string     `json:"playbook_id" yaml:"playbook_id"`
	SetupPoint    SetupPoint `json:"setup_point,omitempty" yaml:"setup_point,omitempty"`

	Direction  CycleDirection `json:"direction" yaml:"direction"`
	Grade      TicketGrade    `json:"grade" yaml:"grade"`
	TradeClass TradeClass     `json:"trade_class" yaml:"trade_class"`

	MarketCycle CycleMap `json:"market_cycle" yaml:"market_cycle"`
	SymbolCycle CycleMap `json:"symbol_cycle" yaml:"symbol_cycle"`

	EntryPlan EntryPlan `json:"entry_plan" yaml:"entry_plan"`
	RiskPlan  RiskPlan  `json:"risk_plan" yaml:"risk_plan"`

	StopBasis     string   `json:"stop_basis,omitempty" yaml:"stop_basis,omitempty"`
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

// StopBasisText returns the explicit structural stop basis annotation, falling
// back to non-empty Invalidations lines when StopBasis was not set separately.
func (t DecisionTicket) StopBasisText() string {
	if s := strings.TrimSpace(t.StopBasis); s != "" {
		return s
	}
	var parts []string
	for _, inv := range t.Invalidations {
		if s := strings.TrimSpace(inv); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "; ")
}

// OneRPriceDistance returns |EntryPrice - StopPrice| when both are > 0, else 0.
func (t DecisionTicket) OneRPriceDistance() float64 {
	if t.RiskPlan.EntryPrice <= 0 || t.RiskPlan.StopPrice <= 0 {
		return 0
	}
	d := t.RiskPlan.EntryPrice - t.RiskPlan.StopPrice
	if d < 0 {
		d = -d
	}
	return d
}

// OneRPct returns the 1R stop distance in percent.
func (t DecisionTicket) OneRPct() float64 {
	if t.RiskPlan.StopDistancePct > 0 {
		return t.RiskPlan.StopDistancePct
	}
	if t.RiskPlan.EntryPrice > 0 && t.RiskPlan.StopPrice > 0 {
		return t.OneRPriceDistance() / t.RiskPlan.EntryPrice * 100
	}
	return 0
}

// DecisionRecord is the persisted human decision on a ticket.
type DecisionRecord struct {
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`

	TicketID string        `json:"ticket_id" yaml:"ticket_id"`
	Decision HumanDecision `json:"decision" yaml:"decision"`

	ReasonCodes []string `json:"reason_codes" yaml:"reason_codes"`
	Note        string   `json:"note" yaml:"note"`

	PlannedEntry      *float64 `json:"planned_entry,omitempty" yaml:"planned_entry,omitempty"`
	PlannedStop       *float64 `json:"planned_stop,omitempty" yaml:"planned_stop,omitempty"`
	PlannedRiskR      *float64 `json:"planned_risk_r,omitempty" yaml:"planned_risk_r,omitempty"`
	PlannedMaxLossUSD *float64 `json:"planned_max_loss_usd,omitempty" yaml:"planned_max_loss_usd,omitempty"`

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
