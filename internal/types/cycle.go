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

// SeasonRegime is the causal cross-sectional 4-season market state ported from
// tools/research/h1_experiment.py (Phase 2c shadow advisory only).
// Values when present are strictly uppercase: SPRING / SUMMER / AUTUMN / WINTER.
// Empty ("") means cross-sectional inputs were not supplied; never map from
// CycleMap or default to WINTER when unmeasured (owner decision 2026-11-11).
type SeasonRegime string

const (
	SeasonRegimeSpring SeasonRegime = "SPRING"
	SeasonRegimeSummer SeasonRegime = "SUMMER"
	SeasonRegimeAutumn SeasonRegime = "AUTUMN"
	SeasonRegimeWinter SeasonRegime = "WINTER"
)

// DefaultWinterDefenseHint is the human-facing advisory attached ONLY when
// SeasonRegime == SeasonRegimeWinter from genuine cross-sectional calculation
// (Phase 2c owner decision 2026-11-11).
// Parent hypothesis H1a failed its joint bar; this hint rests solely on the
// Winter Defense sub-test passing across two independent samples.
const DefaultWinterDefenseHint = "advisory: consider 0.25x risk scaling (winter tickets historically negative expectancy, defense sub-test passed twice); human decides position size"

// IsValidSeasonRegime reports whether r is one of SPRING, SUMMER, AUTUMN, WINTER.
func IsValidSeasonRegime(r SeasonRegime) bool {
	switch r {
	case SeasonRegimeSpring, SeasonRegimeSummer, SeasonRegimeAutumn, SeasonRegimeWinter:
		return true
	default:
		return false
	}
}

// NormalizeSeasonRegime canonicalizes a valid SeasonRegime value into
// SPRING/SUMMER/AUTUMN/WINTER, or returns "" when empty/unrecognized.
// Never defaults to WINTER when unmeasured.
func NormalizeSeasonRegime(r SeasonRegime) SeasonRegime {
	switch r {
	case SeasonRegimeSpring, SeasonRegime("spring"):
		return SeasonRegimeSpring
	case SeasonRegimeSummer, SeasonRegime("summer"):
		return SeasonRegimeSummer
	case SeasonRegimeAutumn, SeasonRegime("autumn"):
		return SeasonRegimeAutumn
	case SeasonRegimeWinter, SeasonRegime("winter"):
		return SeasonRegimeWinter
	default:
		return ""
	}
}

// WinterDefenseHintFor returns DefaultWinterDefenseHint if and only if
// NormalizeSeasonRegime(regime) == SeasonRegimeWinter; otherwise returns "".
func WinterDefenseHintFor(regime SeasonRegime) string {
	if NormalizeSeasonRegime(regime) == SeasonRegimeWinter {
		return DefaultWinterDefenseHint
	}
	return ""
}

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
