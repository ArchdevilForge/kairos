package types

// ActionState represents the explicit scanner action state for a setup.
type ActionState string

const (
	ActionStateNoTrade        ActionState = "no_trade"
	ActionStateWatch          ActionState = "watch"
	ActionStatePrepare        ActionState = "prepare"
	ActionStateTradeCandidate ActionState = "trade_candidate"
)

// Direction represents the setup direction (long or short).
type Direction string

const (
	DirectionLong  Direction = "long"
	DirectionShort Direction = "short"
)

// BoxStatus represents the status of a detected box pattern.
type BoxStatus string

const (
	BoxStatusForming      BoxStatus = "forming"
	BoxStatusConverging   BoxStatus = "converging"
	BoxStatusBreakoutUp   BoxStatus = "breakout_up"
	BoxStatusBreakoutDown BoxStatus = "breakout_down"
	BoxStatusInvalid      BoxStatus = "invalid"
)

// MarketPhase represents the market cycle phase (spring/summer/autumn/winter).
type MarketPhase string

const (
	MarketPhaseSpring MarketPhase = "spring"
	MarketPhaseSummer MarketPhase = "summer"
	MarketPhaseAutumn MarketPhase = "autumn"
	MarketPhaseWinter MarketPhase = "winter"
)

// Severity represents alert severity level.
type Severity string

const (
	SeverityLow    Severity = "LOW"
	SeverityMedium Severity = "MEDIUM"
	SeverityHigh   Severity = "HIGH"
)

// SignalEnvelope is the top-level response wrapper returned by the scanner.
type SignalEnvelope struct {
	Success      bool              `json:"success" yaml:"success"`
	SchemaVersion string            `json:"schema_version" yaml:"schema_version"`
	Timestamp    string             `json:"timestamp" yaml:"timestamp"`
	Symbol       *string            `json:"symbol" yaml:"symbol"`
	Data         map[string]any     `json:"data" yaml:"data"`
	Score        map[string]any     `json:"score" yaml:"score"`
	Reasons      []string           `json:"reasons" yaml:"reasons"`
	Warnings     []string           `json:"warnings" yaml:"warnings"`
	Errors       []string           `json:"errors" yaml:"errors"`
}

// Candle represents a single OHLCV candle.
type Candle struct {
	Timestamp int64   `json:"timestamp" yaml:"timestamp"`
	Open      float64 `json:"open" yaml:"open"`
	High      float64 `json:"high" yaml:"high"`
	Low       float64 `json:"low" yaml:"low"`
	Close     float64 `json:"close" yaml:"close"`
	Volume    float64 `json:"volume" yaml:"volume"`
}

// Ticker represents normalized market data from an exchange ticker.
type Ticker struct {
	Symbol       string   `json:"symbol" yaml:"symbol"`
	LastPrice    *float64 `json:"last_price,omitempty" yaml:"last_price,omitempty"`
	QuoteVolume  *float64 `json:"quote_volume_24h,omitempty" yaml:"quote_volume_24h,omitempty"`
	ChangePct    *float64 `json:"change_24h_pct,omitempty" yaml:"change_24h_pct,omitempty"`
	OpenInterest *float64 `json:"open_interest,omitempty" yaml:"open_interest,omitempty"`
	FundingRate  *float64 `json:"funding_rate,omitempty" yaml:"funding_rate,omitempty"`
	Info         map[string]any `json:"info,omitempty" yaml:"info,omitempty"`
}

// Candidate represents a scored market candidate from the scanner's discovery phase.
type Candidate struct {
	Symbol         string   `json:"symbol" yaml:"symbol"`
	Exchange       string   `json:"exchange" yaml:"exchange"`
	QuoteVolume24h float64  `json:"quote_volume_24h" yaml:"quote_volume_24h"`
	LastPrice      *float64 `json:"last_price,omitempty" yaml:"last_price,omitempty"`
	Change24hPct   *float64 `json:"change_24h_pct,omitempty" yaml:"change_24h_pct,omitempty"`
	CandidateScore float64  `json:"candidate_score" yaml:"candidate_score"`
	ScoreReasons   []string `json:"score_reasons" yaml:"score_reasons"`
	Warnings       []string `json:"warnings" yaml:"warnings"`
}

// RiskBounds represents structure-based risk boundaries for a setup.
type RiskBounds struct {
	MaxPositionPct  float64   `json:"max_position_pct" yaml:"max_position_pct"`
	MaxLeverage     float64   `json:"max_leverage" yaml:"max_leverage"`
	EntryZone       []float64 `json:"entry_zone" yaml:"entry_zone"`
	StructuralStop  *float64  `json:"structural_stop,omitempty" yaml:"structural_stop,omitempty"`
	Targets         []float64 `json:"targets" yaml:"targets"`
	RiskReward      float64   `json:"risk_reward" yaml:"risk_reward"`
	RiskRewardTarget *float64 `json:"risk_reward_target,omitempty" yaml:"risk_reward_target,omitempty"`
	Invalidation    *string   `json:"invalidation,omitempty" yaml:"invalidation,omitempty"`
	Triggered       bool      `json:"triggered" yaml:"triggered"`
	NearTrigger     bool      `json:"near_trigger" yaml:"near_trigger"`
	AccountSizing   bool      `json:"account_sizing" yaml:"account_sizing"`
}

// Setup represents a fully analyzed setup for one symbol and direction.
type Setup struct {
	Symbol             string         `json:"symbol" yaml:"symbol"`
	Direction          string         `json:"direction" yaml:"direction"`
	SetupType          *string        `json:"setup_type,omitempty" yaml:"setup_type,omitempty"`
	ActionState        string         `json:"action_state" yaml:"action_state"`
	SetupScore         float64        `json:"setup_score" yaml:"setup_score"`
	Threshold          *float64       `json:"threshold,omitempty" yaml:"threshold,omitempty"`
	RequiredRiskReward *float64       `json:"required_risk_reward,omitempty" yaml:"required_risk_reward,omitempty"`
	Structure          map[string]any `json:"structure" yaml:"structure"`
	Risk               RiskBounds     `json:"risk" yaml:"risk"`
	ChartSpec          map[string]any `json:"chart_spec" yaml:"chart_spec"`
	Fingerprint        string         `json:"fingerprint" yaml:"fingerprint"`
	Reasons            []string       `json:"reasons" yaml:"reasons"`
	Warnings           []string       `json:"warnings" yaml:"warnings"`
	Execution          map[string]any `json:"execution" yaml:"execution"`
}

// MarketCycle represents the current market cycle state.
type MarketCycle struct {
	Phase              MarketPhase `json:"phase" yaml:"phase"`
	Confidence         float64     `json:"confidence" yaml:"confidence"`
	BtcTrend           string      `json:"btc_trend" yaml:"btc_trend"`
	BtcChange30D       float64     `json:"btc_change_30d" yaml:"btc_change_30d"`
	BtcChange7D        float64     `json:"btc_change_7d" yaml:"btc_change_7d"`
	Volatility         float64     `json:"volatility" yaml:"volatility"`
	VolumeTrend        string      `json:"volume_trend" yaml:"volume_trend"`
	AltcoinCorrelation float64     `json:"altcoin_correlation" yaml:"altcoin_correlation"`
	FundingRatesAvg    float64     `json:"funding_rates_avg" yaml:"funding_rates_avg"`
	MarketCapChange30D float64     `json:"market_cap_change_30d" yaml:"market_cap_change_30d"`
}

// BoxPattern represents a detected box pattern.
type BoxPattern struct {
	Symbol          string     `json:"symbol" yaml:"symbol"`
	Timeframe       string     `json:"timeframe" yaml:"timeframe"`
	High            float64    `json:"high" yaml:"high"`
	Low             float64    `json:"low" yaml:"low"`
	StartTime       float64    `json:"start_time" yaml:"start_time"`
	EndTime         float64    `json:"end_time" yaml:"end_time"`
	Status          BoxStatus  `json:"status" yaml:"status"`
	TouchHigh       int        `json:"touch_high" yaml:"touch_high"`
	TouchLow        int        `json:"touch_low" yaml:"touch_low"`
	SecondTestHigh  bool       `json:"second_test_high" yaml:"second_test_high"`
	SecondTestLow   bool       `json:"second_test_low" yaml:"second_test_low"`
	ConvergencePct  float64    `json:"convergence_pct" yaml:"convergence_pct"`
	VolumeDeclining bool       `json:"volume_declining" yaml:"volume_declining"`
	BreakoutPrice   *float64   `json:"breakout_price,omitempty" yaml:"breakout_price,omitempty"`
	BreakoutTime    *float64   `json:"breakout_time,omitempty" yaml:"breakout_time,omitempty"`
}

// Height returns the box height (high - low).
func (b *BoxPattern) Height() float64 {
	return b.High - b.Low
}

// HeightPct returns the box height as a percentage of low.
func (b *BoxPattern) HeightPct() float64 {
	if b.Low <= 0 {
		return 0
	}
	return b.Height() / b.Low * 100
}

// Midpoint returns the box midpoint.
func (b *BoxPattern) Midpoint() float64 {
	return (b.High + b.Low) / 2
}

// IsReady returns whether the box is ready for breakout (has second test and convergence).
func (b *BoxPattern) IsReady() bool {
	return (b.SecondTestHigh || b.SecondTestLow) && b.ConvergencePct > 0.7
}

// AlertEvent represents a hard-data market alert for Telegram delivery.
type AlertEvent struct {
	Event     string         `json:"event" yaml:"event"`
	Symbol    string         `json:"symbol" yaml:"symbol"`
	Price     float64        `json:"price" yaml:"price"`
	Condition string         `json:"condition" yaml:"condition"`
	EventID   string         `json:"event_id" yaml:"event_id"`
	Timestamp string         `json:"timestamp" yaml:"timestamp"`
	Exchange  string         `json:"exchange" yaml:"exchange"`
	ChangePct float64        `json:"change_pct" yaml:"change_pct"`
	Severity  Severity       `json:"severity" yaml:"severity"`
	Data      map[string]any `json:"data,omitempty" yaml:"data,omitempty"`
}

// OHLCVArrays holds numpy-style OHLCV arrays (all same length).
type OHLCVArrays struct {
	Timestamps []float64 `json:"timestamps" yaml:"timestamps"`
	Opens      []float64 `json:"opens" yaml:"opens"`
	Highs      []float64 `json:"highs" yaml:"highs"`
	Lows       []float64 `json:"lows" yaml:"lows"`
	Closes     []float64 `json:"closes" yaml:"closes"`
	Volumes    []float64 `json:"volumes" yaml:"volumes"`
}

// =============================================================================
// Config types — mirror config.yaml.example structure
// =============================================================================

// Config is the top-level Kairos configuration, loaded from config.yaml.
type Config struct {
	Exchange             string              `mapstructure:"exchange" json:"exchange" yaml:"exchange"`
	DefaultTimeframe     string              `mapstructure:"defaultTimeframe" json:"defaultTimeframe" yaml:"defaultTimeframe"`
	NotificationTimezone string              `mapstructure:"notificationTimezone" json:"notificationTimezone" yaml:"notificationTimezone"`
	Telegram             TelegramConfig      `mapstructure:"telegram" json:"telegram" yaml:"telegram"`
	DataManager          DataManagerConfig   `mapstructure:"dataManager" json:"dataManager" yaml:"dataManager"`
	AlertPolicy          AlertPolicyConfig   `mapstructure:"alertPolicy" json:"alertPolicy" yaml:"alertPolicy"`
	PriceVelocity        PriceVelocityConfig `mapstructure:"priceVelocity" json:"priceVelocity" yaml:"priceVelocity"`
	VolumeSpike          VolumeSpikeConfig   `mapstructure:"volumeSpike" json:"volumeSpike" yaml:"volumeSpike"`
	FuturesMetrics       FuturesMetricsConfig `mapstructure:"futuresMetrics" json:"futuresMetrics" yaml:"futuresMetrics"`
	LongShortRatio       LongShortRatioConfig `mapstructure:"longShortRatio" json:"longShortRatio" yaml:"longShortRatio"`
	Liquidation          LiquidationConfig   `mapstructure:"liquidation" json:"liquidation" yaml:"liquidation"`
	ResonanceScorer      ResonanceScorerConfig `mapstructure:"resonanceScorer" json:"resonanceScorer" yaml:"resonanceScorer"`
	Scanner              ScannerConfig       `mapstructure:"scanner" json:"scanner" yaml:"scanner"`
	Exchanges            ExchangesConfig     `mapstructure:"exchanges" json:"exchanges" yaml:"exchanges"`
	Scoring              ScoringConfig       `mapstructure:"scoring" json:"scoring" yaml:"scoring"`
	Risk                 RiskConfig          `mapstructure:"risk" json:"risk" yaml:"risk"`
	Storage              StorageConfig       `mapstructure:"storage" json:"storage" yaml:"storage"`
	Charts               ChartConfig         `mapstructure:"charts" json:"charts" yaml:"charts"`
	// AlertMinState is the minimum action state for alerting (env: KAIROS_ALERT_MIN_STATE).
	AlertMinState string `mapstructure:"-" json:"alert_min_state,omitempty" yaml:"-"`
	// AlertLimit is the max number of alerts per cycle (env: KAIROS_ALERT_LIMIT).
	AlertLimit int `mapstructure:"-" json:"alert_limit,omitempty" yaml:"-"`
}

// TelegramConfig holds Telegram delivery settings.
type TelegramConfig struct {
	Enabled   bool   `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	ParseMode string `mapstructure:"parseMode" json:"parseMode" yaml:"parseMode"`
	BotToken  string `mapstructure:"-" json:"bot_token,omitempty" yaml:"-"` // env: TELEGRAM_BOT_TOKEN
	ChatID    string `mapstructure:"-" json:"chat_id,omitempty" yaml:"-"`   // env: TELEGRAM_CHAT_ID
}

// DataManagerConfig controls ticker polling and deduplication.
type DataManagerConfig struct {
	Exchanges            []string `mapstructure:"exchanges" json:"exchanges" yaml:"exchanges"`
	TopSymbols           int      `mapstructure:"topSymbols" json:"topSymbols" yaml:"topSymbols"`
	RefreshIntervalHours int      `mapstructure:"refreshIntervalHours" json:"refreshIntervalHours" yaml:"refreshIntervalHours"`
	DedupWindowSeconds   int      `mapstructure:"dedupWindowSeconds" json:"dedupWindowSeconds" yaml:"dedupWindowSeconds"`
	SymbolCooldownMinutes int     `mapstructure:"symbolCooldownMinutes" json:"symbolCooldownMinutes" yaml:"symbolCooldownMinutes"`
}

// AlertPolicyConfig defines which event types and severity thresholds trigger alerts.
type AlertPolicyConfig struct {
	Enabled                 bool     `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	AllowedEventTypes       []string `mapstructure:"allowedEventTypes" json:"allowedEventTypes" yaml:"allowedEventTypes"`
	MinSeverity             string   `mapstructure:"minSeverity" json:"minSeverity" yaml:"minSeverity"`
	MinPriceChangePct       float64  `mapstructure:"minPriceChangePct" json:"minPriceChangePct" yaml:"minPriceChangePct"`
	MinVolumeRatio          float64  `mapstructure:"minVolumeRatio" json:"minVolumeRatio" yaml:"minVolumeRatio"`
	MinOpenInterestChangePct float64 `mapstructure:"minOpenInterestChangePct" json:"minOpenInterestChangePct" yaml:"minOpenInterestChangePct"`
	MinFundingRateAbs       float64  `mapstructure:"minFundingRateAbs" json:"minFundingRateAbs" yaml:"minFundingRateAbs"`
	MinFundingRateChangeAbs float64  `mapstructure:"minFundingRateChangeAbs" json:"minFundingRateChangeAbs" yaml:"minFundingRateChangeAbs"`
}

// PriceVelocityConfig defines short-term price velocity thresholds.
type PriceVelocityConfig struct {
	Enabled         bool               `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	Windows         []PriceWindow      `mapstructure:"windows" json:"windows" yaml:"windows"`
	CooldownSeconds int                `mapstructure:"cooldownSeconds" json:"cooldownSeconds" yaml:"cooldownSeconds"`
}

// PriceWindow defines a single velocity observation window.
type PriceWindow struct {
	Seconds   int     `mapstructure:"seconds" json:"seconds" yaml:"seconds"`
	Threshold float64 `mapstructure:"threshold" json:"threshold" yaml:"threshold"`
}

// VolumeSpikeConfig defines volume spike detection parameters.
type VolumeSpikeConfig struct {
	Enabled          bool   `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	Multiplier       float64 `mapstructure:"multiplier" json:"multiplier" yaml:"multiplier"`
	WindowMinutes    int    `mapstructure:"windowMinutes" json:"windowMinutes" yaml:"windowMinutes"`
	MinHistorySeconds int   `mapstructure:"minHistorySeconds" json:"minHistorySeconds" yaml:"minHistorySeconds"`
	MinNotifyInterval string `mapstructure:"minNotifyInterval" json:"minNotifyInterval" yaml:"minNotifyInterval"`
}

// FuturesMetricsConfig controls open interest and funding rate polling.
type FuturesMetricsConfig struct {
	Enabled              bool                    `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	PollIntervalSeconds  int                     `mapstructure:"pollIntervalSeconds" json:"pollIntervalSeconds" yaml:"pollIntervalSeconds"`
	FetchFundingPerSymbol bool                   `mapstructure:"fetchFundingPerSymbol" json:"fetchFundingPerSymbol" yaml:"fetchFundingPerSymbol"`
	OpenInterest         OIConfig                `mapstructure:"openInterest" json:"openInterest" yaml:"openInterest"`
	FundingRate          FundingRateConfig       `mapstructure:"fundingRate" json:"fundingRate" yaml:"fundingRate"`
}

// OIConfig controls open interest change detection.
type OIConfig struct {
	Enabled         bool   `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	MinChangePct    float64 `mapstructure:"minChangePct" json:"minChangePct" yaml:"minChangePct"`
	MinNotifyInterval string `mapstructure:"minNotifyInterval" json:"minNotifyInterval" yaml:"minNotifyInterval"`
}

// FundingRateConfig controls funding rate anomaly detection.
type FundingRateConfig struct {
	Enabled         bool    `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	AbsRateThreshold float64 `mapstructure:"absRateThreshold" json:"absRateThreshold" yaml:"absRateThreshold"`
	MinChangeAbs    float64 `mapstructure:"minChangeAbs" json:"minChangeAbs" yaml:"minChangeAbs"`
	MinNotifyInterval string `mapstructure:"minNotifyInterval" json:"minNotifyInterval" yaml:"minNotifyInterval"`
}

// LongShortRatioConfig controls long/short ratio anomaly detection.
type LongShortRatioConfig struct {
	Enabled             bool    `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	PollIntervalSeconds int     `mapstructure:"pollIntervalSeconds" json:"pollIntervalSeconds" yaml:"pollIntervalSeconds"`
	AbsThreshold        float64 `mapstructure:"absThreshold" json:"absThreshold" yaml:"absThreshold"`
	ZscoreThreshold     float64 `mapstructure:"zscoreThreshold" json:"zscoreThreshold" yaml:"zscoreThreshold"`
	ZscoreWindow        int     `mapstructure:"zscoreWindow" json:"zscoreWindow" yaml:"zscoreWindow"`
	VelocityThresholdPct float64 `mapstructure:"velocityThresholdPct" json:"velocityThresholdPct" yaml:"velocityThresholdPct"`
	MinNotifyInterval   string  `mapstructure:"minNotifyInterval" json:"minNotifyInterval" yaml:"minNotifyInterval"`
}

// LiquidationConfig controls liquidation event detection.
type LiquidationConfig struct {
	Enabled             bool    `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	PollIntervalSeconds int     `mapstructure:"pollIntervalSeconds" json:"pollIntervalSeconds" yaml:"pollIntervalSeconds"`
	AbsThresholdUsd     float64 `mapstructure:"absThresholdUsd" json:"absThresholdUsd" yaml:"absThresholdUsd"`
	ZscoreThreshold     float64 `mapstructure:"zscoreThreshold" json:"zscoreThreshold" yaml:"zscoreThreshold"`
	ZscoreWindow        int     `mapstructure:"zscoreWindow" json:"zscoreWindow" yaml:"zscoreWindow"`
	ImbalanceThreshold  float64 `mapstructure:"imbalanceThreshold" json:"imbalanceThreshold" yaml:"imbalanceThreshold"`
	MinNotifyInterval   string  `mapstructure:"minNotifyInterval" json:"minNotifyInterval" yaml:"minNotifyInterval"`
}

// ResonanceScorerConfig controls multi-dimension signal resonance scoring.
type ResonanceScorerConfig struct {
	Enabled         bool   `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	WindowSeconds   int    `mapstructure:"windowSeconds" json:"windowSeconds" yaml:"windowSeconds"`
	MinDimensions   int    `mapstructure:"minDimensions" json:"minDimensions" yaml:"minDimensions"`
	MinScore        float64 `mapstructure:"minScore" json:"minScore" yaml:"minScore"`
	CooldownSeconds int    `mapstructure:"cooldownSeconds" json:"cooldownSeconds" yaml:"cooldownSeconds"`
}

// ScannerConfig defines scanner workflow limits and timeout budgets.
type ScannerConfig struct {
	IntervalSeconds              int      `mapstructure:"intervalSeconds" json:"intervalSeconds" yaml:"intervalSeconds"`
	UniverseSize                 int      `mapstructure:"universeSize" json:"universeSize" yaml:"universeSize"`
	CandidateLimit               int      `mapstructure:"candidateLimit" json:"candidateLimit" yaml:"candidateLimit"`
	DeepAnalysisLimit            int      `mapstructure:"deepAnalysisLimit" json:"deepAnalysisLimit" yaml:"deepAnalysisLimit"`
	TotalTimeoutSeconds          int      `mapstructure:"totalTimeoutSeconds" json:"totalTimeoutSeconds" yaml:"totalTimeoutSeconds"`
	ExchangeRequestTimeoutSeconds int     `mapstructure:"exchangeRequestTimeoutSeconds" json:"exchangeRequestTimeoutSeconds" yaml:"exchangeRequestTimeoutSeconds"`
	SymbolAnalysisTimeoutSeconds  int     `mapstructure:"symbolAnalysisTimeoutSeconds" json:"symbolAnalysisTimeoutSeconds" yaml:"symbolAnalysisTimeoutSeconds"`
	Timeframes                    []string `mapstructure:"timeframes" json:"timeframes" yaml:"timeframes"`
	GenerateChartsByDefault       bool     `mapstructure:"generateChartsByDefault" json:"generateChartsByDefault" yaml:"generateChartsByDefault"`
}

// ExchangesConfig defines exchange selection and symbol-normalization settings.
type ExchangesConfig struct {
	Primary        string   `mapstructure:"primary" json:"primary" yaml:"primary"`
	Backups        []string `mapstructure:"backups" json:"backups" yaml:"backups"`
	RateLimit      bool     `mapstructure:"rateLimit" json:"rateLimit" yaml:"rateLimit"`
	CanonicalQuote string   `mapstructure:"canonicalQuote" json:"canonicalQuote" yaml:"canonicalQuote"`
	Settle         string   `mapstructure:"settle" json:"settle" yaml:"settle"`
}

// ScoringConfig defines deterministic scoring thresholds and weights.
type ScoringConfig struct {
	CandidateWeights             map[string]float64 `mapstructure:"candidateWeights" json:"candidateWeights" yaml:"candidateWeights"`
	SetupWeights                 map[string]float64 `mapstructure:"setupWeights" json:"setupWeights" yaml:"setupWeights"`
	CycleThresholds              map[string]float64 `mapstructure:"cycleThresholds" json:"cycleThresholds" yaml:"cycleThresholds"`
	MinimumLiquidityQuoteVolume  float64            `mapstructure:"minimumLiquidityQuoteVolume" json:"minimumLiquidityQuoteVolume" yaml:"minimumLiquidityQuoteVolume"`
	MinimumRiskReward            float64            `mapstructure:"minimumRiskReward" json:"minimumRiskReward" yaml:"minimumRiskReward"`
	StrictRiskReward             float64            `mapstructure:"strictRiskReward" json:"strictRiskReward" yaml:"strictRiskReward"`
	ShortThresholdPremium        float64            `mapstructure:"shortThresholdPremium" json:"shortThresholdPremium" yaml:"shortThresholdPremium"`
}

// RiskConfig defines signal-only risk bounds (not execution commands).
type RiskConfig struct {
	MaxPositionPct              map[string]float64 `mapstructure:"maxPositionPct" json:"maxPositionPct" yaml:"maxPositionPct"`
	MaxLeverage                 map[string]float64 `mapstructure:"maxLeverage" json:"maxLeverage" yaml:"maxLeverage"`
	WeakCyclePositionMultiplier float64            `mapstructure:"weakCyclePositionMultiplier" json:"weakCyclePositionMultiplier" yaml:"weakCyclePositionMultiplier"`
	ShortPositionMultiplier     float64            `mapstructure:"shortPositionMultiplier" json:"shortPositionMultiplier" yaml:"shortPositionMultiplier"`
	InverseCyclePositionMultiplier float64          `mapstructure:"inverseCyclePositionMultiplier" json:"inverseCyclePositionMultiplier" yaml:"inverseCyclePositionMultiplier"`
}

// StorageConfig defines persistence configuration.
type StorageConfig struct {
	DatabasePath   string `mapstructure:"databasePath" json:"databasePath" yaml:"databasePath"`
	RetentionDays  int    `mapstructure:"retentionDays" json:"retentionDays" yaml:"retentionDays"`
	JSONLExport    bool   `mapstructure:"jsonlExport" json:"jsonlExport" yaml:"jsonlExport"`
	JSONLPath      string `mapstructure:"jsonlPath" json:"jsonlPath" yaml:"jsonlPath"`
}

// ChartConfig defines chart generation policy.
type ChartConfig struct {
	DefaultChartCount          int     `mapstructure:"defaultChartCount" json:"defaultChartCount" yaml:"defaultChartCount"`
	OutputPath                 string  `mapstructure:"outputPath" json:"outputPath" yaml:"outputPath"`
	CleanupDays                int     `mapstructure:"cleanupDays" json:"cleanupDays" yaml:"cleanupDays"`
	MultiTimeframeScoreThreshold float64 `mapstructure:"multiTimeframeScoreThreshold" json:"multiTimeframeScoreThreshold" yaml:"multiTimeframeScoreThreshold"`
}
