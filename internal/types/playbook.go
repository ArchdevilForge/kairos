package types

// Schema version for playbook match JSON.
const PlaybookMatchSchemaVersion = "playbook_match.v1"

// Playbook IDs (stage-1 only leader_pullback_v1 is enabled).
const (
	PlaybookLeaderPullbackV1 = "leader_pullback_v1"
	PlaybookBoxBreakoutV1    = "box_breakout_v1"
	PlaybookSecondWaveV1     = "second_wave_v1"
)

// TicketGrade is the discrete quality label (not a calibrated probability).
type TicketGrade string

const (
	TicketGradeA TicketGrade = "A"
	TicketGradeB TicketGrade = "B"
	TicketGradeC TicketGrade = "C"
	TicketGradeD TicketGrade = "D" // no trade
)

// PlaybookContext is the input bundle a playbook matcher sees.
// Services fill this; playbook packages consume it. Kept in types for stable JSON/replay.
type PlaybookContext struct {
	SchemaVersion string `json:"schema_version,omitempty" yaml:"schema_version,omitempty"`

	SessionID string `json:"session_id" yaml:"session_id"`
	Symbol    string `json:"symbol" yaml:"symbol"`

	PulseState     MarketState    `json:"pulse_state" yaml:"pulse_state"`
	PulseDirection CycleDirection `json:"pulse_direction" yaml:"pulse_direction"`

	MarketCycle CycleMap `json:"market_cycle" yaml:"market_cycle"`
	SymbolCycle CycleMap `json:"symbol_cycle" yaml:"symbol_cycle"`

	Candidate DirectionalCandidate `json:"candidate" yaml:"candidate"`

	// AsOfUnix is the evaluation clock (closed-bar time).
	AsOfUnix int64 `json:"as_of_unix" yaml:"as_of_unix"`
}

// PlaybookMatch is the result of one playbook against one context.
type PlaybookMatch struct {
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`

	PlaybookID string `json:"playbook_id" yaml:"playbook_id"`
	SessionID  string `json:"session_id" yaml:"session_id"`
	Symbol     string `json:"symbol" yaml:"symbol"`

	Matched    bool           `json:"matched" yaml:"matched"`
	Grade      TicketGrade    `json:"grade" yaml:"grade"`
	Direction  CycleDirection `json:"direction" yaml:"direction"`
	TradeClass TradeClass     `json:"trade_class" yaml:"trade_class"`

	HardFailures []string `json:"hard_failures" yaml:"hard_failures"`
	Reasons      []string `json:"reasons" yaml:"reasons"`
	Warnings     []string `json:"warnings" yaml:"warnings"`

	RiskTemplate string `json:"risk_template" yaml:"risk_template"`
}
