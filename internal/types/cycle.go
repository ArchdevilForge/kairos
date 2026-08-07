package types

// Schema versions for cycle-related JSON contracts.
// Bump only with a deliberate migration; readers must reject unknown majors later.
const (
	CycleMapSchemaVersion  = "cycle_map.v1"
	CycleNodeSchemaVersion = "cycle_node.v1"
)

// CycleDirection is the machine directional state for one timeframe.
// Distinct from legacy MarketPhase climate and from setup Direction (long/short).
type CycleDirection string

const (
	CycleDirectionUp      CycleDirection = "up"
	CycleDirectionDown    CycleDirection = "down"
	CycleDirectionNeutral CycleDirection = "neutral"
)

// WavePhase is the wave stage given a direction.
// Winter means no stable direction / chop — it is NOT a short signal.
type WavePhase string

const (
	WavePhaseSpring WavePhase = "spring" // new direction starting
	WavePhaseSummer WavePhase = "summer" // trend expansion
	WavePhaseAutumn WavePhase = "autumn" // trend decay
	WavePhaseWinter WavePhase = "winter" // no direction / reset / garbage chop
)

// TimeframeRole is one of the three fixed hierarchy roles.
type TimeframeRole string

const (
	TimeframeRoleContext TimeframeRole = "context"
	TimeframeRoleSetup   TimeframeRole = "setup"
	TimeframeRoleTrigger TimeframeRole = "trigger"
)

// CycleAlignment summarizes multi-TF relationship.
type CycleAlignment string

const (
	AlignmentFull         CycleAlignment = "full_alignment"
	AlignmentPullback     CycleAlignment = "trend_pullback"
	AlignmentCounterTrend CycleAlignment = "counter_trend"
	AlignmentMixed        CycleAlignment = "mixed"
	AlignmentNoTrade      CycleAlignment = "no_trade"
)

// TradeClass labels how a ticket relates to context direction.
type TradeClass string

const (
	TradeClassAlignedLong       TradeClass = "aligned_long"
	TradeClassAlignedShort      TradeClass = "aligned_short"
	TradeClassCounterTrendLong  TradeClass = "counter_trend_long"
	TradeClassCounterTrendShort TradeClass = "counter_trend_short"
	TradeClassNoTrade           TradeClass = "no_trade"
)

// Evidence is an interpretable reason behind a CycleNode judgment.
type Evidence struct {
	Code        string  `json:"code" yaml:"code"`
	Description string  `json:"description" yaml:"description"`
	Value       float64 `json:"value,omitempty" yaml:"value,omitempty"`
}

// CycleNode is one timeframe's directional cycle state.
type CycleNode struct {
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`

	Timeframe string        `json:"timeframe" yaml:"timeframe"`
	Role      TimeframeRole `json:"role" yaml:"role"`

	Direction CycleDirection `json:"direction" yaml:"direction"`
	Phase     WavePhase      `json:"phase" yaml:"phase"`

	TrendStrength    float64 `json:"trend_strength" yaml:"trend_strength"`
	StructureQuality float64 `json:"structure_quality" yaml:"structure_quality"`
	MomentumChange   float64 `json:"momentum_change" yaml:"momentum_change"`
	Volatility       float64 `json:"volatility" yaml:"volatility"`
	VolumeQuality    float64 `json:"volume_quality" yaml:"volume_quality"`

	RoomUpPct   float64 `json:"room_up_pct" yaml:"room_up_pct"`
	RoomDownPct float64 `json:"room_down_pct" yaml:"room_down_pct"`

	Confidence float64    `json:"confidence" yaml:"confidence"`
	Evidence   []Evidence `json:"evidence" yaml:"evidence"`
}

// CycleMap is the multi-TF market or symbol cycle snapshot.
// LegacyClimate is display/shadow only and must not grant short permission alone.
type CycleMap struct {
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`
	AsOfUnix      int64  `json:"as_of_unix" yaml:"as_of_unix"`

	// LegacyClimate mirrors MarketPhase for human/UI compare; not trade authority.
	LegacyClimate MarketPhase `json:"legacy_climate" yaml:"legacy_climate"`

	// Nodes keyed by timeframe string (e.g. "1d", "4h", "15m", "5m").
	Nodes map[string]CycleNode `json:"nodes" yaml:"nodes"`

	PrimaryDirection CycleDirection `json:"primary_direction" yaml:"primary_direction"`
	Alignment        CycleAlignment `json:"alignment" yaml:"alignment"`
	TradeClass       TradeClass     `json:"trade_class" yaml:"trade_class"`

	Summary []string `json:"summary" yaml:"summary"`
}

// TransitionPolicy controls phase/direction hysteresis.
type TransitionPolicy struct {
	ConfirmBars       int     `json:"confirm_bars" yaml:"confirm_bars" mapstructure:"confirmBars"`
	MinConfidenceGain float64 `json:"min_confidence_gain" yaml:"min_confidence_gain" mapstructure:"minConfidenceGain"`
	MinStateBars      int     `json:"min_state_bars" yaml:"min_state_bars" mapstructure:"minStateBars"`
}
