package types

// MarketState is the MarketPulse state-machine state.
type MarketState string

const (
	MarketStateQuiet        MarketState = "QUIET"
	MarketStateImpulseUp    MarketState = "IMPULSE_UP"
	MarketStateImpulseDown  MarketState = "IMPULSE_DOWN"
	MarketStateTrendingUp   MarketState = "TRENDING_UP"
	MarketStateTrendingDown MarketState = "TRENDING_DOWN"
	MarketStateStressUp     MarketState = "STRESS_UP"
	MarketStateStressDown   MarketState = "STRESS_DOWN"
	MarketStateDecay        MarketState = "DECAY"
)

// MarketPulseConfig configures the cross-sectional market detector.
type MarketPulseConfig struct {
	Enabled bool `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	// ShadowMode computes and logs only; events may still be emitted for
	// observability but must not change Telegram single-symbol behaviour.
	ShadowMode bool `mapstructure:"shadowMode" json:"shadowMode" yaml:"shadowMode"`

	SnapshotIntervalSeconds int `mapstructure:"snapshotIntervalSeconds" json:"snapshotIntervalSeconds" yaml:"snapshotIntervalSeconds"`
	FreshnessSeconds        int `mapstructure:"freshnessSeconds" json:"freshnessSeconds" yaml:"freshnessSeconds"`
	MaxLookupGapSeconds     int `mapstructure:"maxLookupGapSeconds" json:"maxLookupGapSeconds" yaml:"maxLookupGapSeconds"`
	WarmupSeconds           int `mapstructure:"warmupSeconds" json:"warmupSeconds" yaml:"warmupSeconds"`
	HistoryRetentionSeconds int `mapstructure:"historyRetentionSeconds" json:"historyRetentionSeconds" yaml:"historyRetentionSeconds"`

	MinFreshRatio   float64 `mapstructure:"minFreshRatio" json:"minFreshRatio" yaml:"minFreshRatio"`
	MinValidSymbols int     `mapstructure:"minValidSymbols" json:"minValidSymbols" yaml:"minValidSymbols"`
	NoiseReturnPct  float64 `mapstructure:"noiseReturnPct" json:"noiseReturnPct" yaml:"noiseReturnPct"`

	PrimarySymbols             []string `mapstructure:"primarySymbols" json:"primarySymbols" yaml:"primarySymbols"`
	RequirePrimaryConfirmation bool     `mapstructure:"requirePrimaryConfirmation" json:"requirePrimaryConfirmation" yaml:"requirePrimaryConfirmation"`

	Volatility MarketPulseVolatilityConfig `mapstructure:"volatility" json:"volatility" yaml:"volatility"`
	Impulse    MarketPulseImpulseConfig    `mapstructure:"impulse" json:"impulse" yaml:"impulse"`
	Trend      MarketPulseTrendConfig      `mapstructure:"trend" json:"trend" yaml:"trend"`
	Stress     MarketPulseStressConfig     `mapstructure:"stress" json:"stress" yaml:"stress"`
	Decay      MarketPulseDecayConfig      `mapstructure:"decay" json:"decay" yaml:"decay"`
	Leaders    MarketPulseLeadersConfig    `mapstructure:"leaders" json:"leaders" yaml:"leaders"`

	CooldownSeconds       int `mapstructure:"cooldownSeconds" json:"cooldownSeconds" yaml:"cooldownSeconds"`
	StressCooldownSeconds int `mapstructure:"stressCooldownSeconds" json:"stressCooldownSeconds" yaml:"stressCooldownSeconds"`

	// GateIndividualAlertsWhenQuiet suppresses ordinary price_velocity
	// Telegram alerts while market state is QUIET (Phase 3).
	GateIndividualAlertsWhenQuiet bool    `mapstructure:"gateIndividualAlertsWhenQuiet" json:"gateIndividualAlertsWhenQuiet" yaml:"gateIndividualAlertsWhenQuiet"`
	AllowIsolatedExtremeWhenQuiet bool    `mapstructure:"allowIsolatedExtremeWhenQuiet" json:"allowIsolatedExtremeWhenQuiet" yaml:"allowIsolatedExtremeWhenQuiet"`
	IsolatedExtremeMinZ           float64 `mapstructure:"isolatedExtremeMinZ" json:"isolatedExtremeMinZ" yaml:"isolatedExtremeMinZ"`
}

// MarketPulseVolatilityConfig configures per-symbol EWMA volatility / Z.
type MarketPulseVolatilityConfig struct {
	Enabled   bool    `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	EWMAAlpha float64 `mapstructure:"ewmaAlpha" json:"ewmaAlpha" yaml:"ewmaAlpha"`
	FloorPct  float64 `mapstructure:"floorPct" json:"floorPct" yaml:"floorPct"`
}

// MarketPulseImpulseConfig is the short-window market launch detector.
type MarketPulseImpulseConfig struct {
	WindowSeconds             int     `mapstructure:"windowSeconds" json:"windowSeconds" yaml:"windowSeconds"`
	MinBreadth                float64 `mapstructure:"minBreadth" json:"minBreadth" yaml:"minBreadth"`
	MinMedianReturnPct        float64 `mapstructure:"minMedianReturnPct" json:"minMedianReturnPct" yaml:"minMedianReturnPct"`
	MinMedianZ                float64 `mapstructure:"minMedianZ" json:"minMedianZ" yaml:"minMedianZ"`
	ConfirmationSamples       int     `mapstructure:"confirmationSamples" json:"confirmationSamples" yaml:"confirmationSamples"`
	ConfirmationWindowSamples int     `mapstructure:"confirmationWindowSamples" json:"confirmationWindowSamples" yaml:"confirmationWindowSamples"`
}

// MarketPulseTrendConfig confirms sustained direction after impulse.
type MarketPulseTrendConfig struct {
	WindowSeconds             int     `mapstructure:"windowSeconds" json:"windowSeconds" yaml:"windowSeconds"`
	MinBreadth                float64 `mapstructure:"minBreadth" json:"minBreadth" yaml:"minBreadth"`
	MinMedianReturnPct        float64 `mapstructure:"minMedianReturnPct" json:"minMedianReturnPct" yaml:"minMedianReturnPct"`
	MinPersistSeconds         int     `mapstructure:"minPersistSeconds" json:"minPersistSeconds" yaml:"minPersistSeconds"`
	ConfirmationSamples       int     `mapstructure:"confirmationSamples" json:"confirmationSamples" yaml:"confirmationSamples"`
	ConfirmationWindowSamples int     `mapstructure:"confirmationWindowSamples" json:"confirmationWindowSamples" yaml:"confirmationWindowSamples"`
}

// MarketPulseStressConfig detects extreme synchronous moves.
type MarketPulseStressConfig struct {
	WindowSeconds      int     `mapstructure:"windowSeconds" json:"windowSeconds" yaml:"windowSeconds"`
	MinBreadth         float64 `mapstructure:"minBreadth" json:"minBreadth" yaml:"minBreadth"`
	MinMedianReturnPct float64 `mapstructure:"minMedianReturnPct" json:"minMedianReturnPct" yaml:"minMedianReturnPct"`
	MinMedianZ         float64 `mapstructure:"minMedianZ" json:"minMedianZ" yaml:"minMedianZ"`
}

// MarketPulseDecayConfig detects loss of breadth after a trend.
type MarketPulseDecayConfig struct {
	Enabled            bool    `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	MaxBreadth         float64 `mapstructure:"maxBreadth" json:"maxBreadth" yaml:"maxBreadth"`
	MaxMedianReturnPct float64 `mapstructure:"maxMedianReturnPct" json:"maxMedianReturnPct" yaml:"maxMedianReturnPct"`
	PersistSeconds     int     `mapstructure:"persistSeconds" json:"persistSeconds" yaml:"persistSeconds"`
	Notify             bool    `mapstructure:"notify" json:"notify" yaml:"notify"`
	// QuietResetSeconds: after decay conditions clear, wait this long before QUIET.
	QuietResetSeconds int `mapstructure:"quietResetSeconds" json:"quietResetSeconds" yaml:"quietResetSeconds"`
}

// MarketPulseLeadersConfig controls leader/laggard ranking.
type MarketPulseLeadersConfig struct {
	Limit int `mapstructure:"limit" json:"limit" yaml:"limit"`
}

// MarketSnapshot is one cross-sectional observation.
type MarketSnapshot struct {
	Timestamp       float64
	UniverseSize    int
	EligibleSymbols int
	ValidSymbols    int
	FreshRatio      float64

	MedianReturn60s  float64
	MedianReturn180s float64
	MedianReturn300s float64
	MedianZ60s       float64

	UpBreadth60s   float64
	DownBreadth60s float64

	Advancers int
	Decliners int
	Neutral   int

	BTCReturn60s *float64
	ETHReturn60s *float64

	Leaders  []SymbolMove
	Laggards []SymbolMove

	// DataOK is false when freshness/valid-count gates fail.
	DataOK bool
	// GateReason explains why DataOK is false (empty when ok).
	GateReason string
}

// SymbolMove is a symbol's return relative to the market median.
type SymbolMove struct {
	Symbol      string  `json:"symbol"`
	ReturnPct   float64 `json:"return_pct"`
	RelativePct float64 `json:"relative_pct"`
	ZScore      float64 `json:"z_score"`
}
