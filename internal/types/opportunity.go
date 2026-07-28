package types

// Schema version for opportunity session JSON.
const OpportunitySessionSchemaVersion = "opportunity_session.v1"

// OpportunitySessionStatus is the lifecycle of one MarketPulse-driven session.
type OpportunitySessionStatus string

const (
	OpportunitySessionOpen        OpportunitySessionStatus = "open"
	OpportunitySessionWatching    OpportunitySessionStatus = "watching"
	OpportunitySessionExpired     OpportunitySessionStatus = "expired"
	OpportunitySessionCompleted   OpportunitySessionStatus = "completed"
	OpportunitySessionInvalidated OpportunitySessionStatus = "invalidated"
)

// OpportunitySession groups candidates/tickets born from one market event.
// One valid MarketPulse event → one session (not one per coin).
type OpportunitySession struct {
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`

	ID        string `json:"id" yaml:"id"`
	EventID   string `json:"event_id" yaml:"event_id"`
	CreatedAt int64  `json:"created_at" yaml:"created_at"`
	ExpiresAt int64  `json:"expires_at" yaml:"expires_at"`

	PulseState     MarketState    `json:"pulse_state" yaml:"pulse_state"`
	PulseDirection CycleDirection `json:"pulse_direction" yaml:"pulse_direction"`

	MarketCycle CycleMap `json:"market_cycle" yaml:"market_cycle"`

	Status OpportunitySessionStatus `json:"status" yaml:"status"`
}

// DirectionalCandidate is a dual-sided rank row (not a single long-biased score).
type DirectionalCandidate struct {
	SchemaVersion string `json:"schema_version,omitempty" yaml:"schema_version,omitempty"`

	Symbol string `json:"symbol" yaml:"symbol"`

	LongScore  float64 `json:"long_score" yaml:"long_score"`
	ShortScore float64 `json:"short_score" yaml:"short_score"`

	RelativeStrength float64 `json:"relative_strength" yaml:"relative_strength"`
	RelativeWeakness float64 `json:"relative_weakness" yaml:"relative_weakness"`

	PullbackStrength float64 `json:"pullback_strength" yaml:"pullback_strength"`
	ReboundWeakness  float64 `json:"rebound_weakness" yaml:"rebound_weakness"`

	LiquidityOK bool `json:"liquidity_ok" yaml:"liquidity_ok"`
	SpreadOK    bool `json:"spread_ok" yaml:"spread_ok"`

	Reasons  []string `json:"reasons" yaml:"reasons"`
	Warnings []string `json:"warnings" yaml:"warnings"`
}

// DirectionalCandidateSchemaVersion is the JSON contract for rank rows.
const DirectionalCandidateSchemaVersion = "directional_candidate.v1"
