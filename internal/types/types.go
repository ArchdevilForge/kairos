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
	Success       bool           `json:"success" yaml:"success"`
	SchemaVersion string         `json:"schema_version" yaml:"schema_version"`
	Timestamp     string         `json:"timestamp" yaml:"timestamp"`
	Symbol        *string        `json:"symbol" yaml:"symbol"`
	Data          map[string]any `json:"data" yaml:"data"`
	Score         map[string]any `json:"score" yaml:"score"`
	Reasons       []string       `json:"reasons" yaml:"reasons"`
	Warnings      []string       `json:"warnings" yaml:"warnings"`
	Errors        []string       `json:"errors" yaml:"errors"`
}

// Candle represents a single OHLCV candle. Timestamp is Unix seconds;
// adapters convert exchange-native milliseconds at the boundary.
// QuoteVol is the quote-currency volume (e.g. OKX volCcyQuote row[7] in USDT).
type Candle struct {
	Timestamp int64   `json:"timestamp" yaml:"timestamp"`
	Open      float64 `json:"open" yaml:"open"`
	High      float64 `json:"high" yaml:"high"`
	Low       float64 `json:"low" yaml:"low"`
	Close     float64 `json:"close" yaml:"close"`
	Volume    float64 `json:"volume" yaml:"volume"`
	QuoteVol  float64 `json:"quote_vol" yaml:"quote_vol"`
}

// Ticker represents normalized market data from an exchange ticker.
type Ticker struct {
	Symbol       string         `json:"symbol" yaml:"symbol"`
	LastPrice    *float64       `json:"last_price,omitempty" yaml:"last_price,omitempty"`
	QuoteVolume  *float64       `json:"quote_volume_24h,omitempty" yaml:"quote_volume_24h,omitempty"`
	ChangePct    *float64       `json:"change_24h_pct,omitempty" yaml:"change_24h_pct,omitempty"`
	OpenInterest *float64       `json:"open_interest,omitempty" yaml:"open_interest,omitempty"`
	FundingRate  *float64       `json:"funding_rate,omitempty" yaml:"funding_rate,omitempty"`
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
	MaxPositionPct   float64   `json:"max_position_pct" yaml:"max_position_pct"`
	MaxLeverage      float64   `json:"max_leverage" yaml:"max_leverage"`
	EntryZone        []float64 `json:"entry_zone" yaml:"entry_zone"`
	StructuralStop   *float64  `json:"structural_stop,omitempty" yaml:"structural_stop,omitempty"`
	Targets          []float64 `json:"targets" yaml:"targets"`
	RiskReward       float64   `json:"risk_reward" yaml:"risk_reward"`
	RiskRewardTarget *float64  `json:"risk_reward_target,omitempty" yaml:"risk_reward_target,omitempty"`
	Invalidation     *string   `json:"invalidation,omitempty" yaml:"invalidation,omitempty"`
	Triggered        bool      `json:"triggered" yaml:"triggered"`
	NearTrigger      bool      `json:"near_trigger" yaml:"near_trigger"`
	AccountSizing    bool      `json:"account_sizing" yaml:"account_sizing"`
}

// Setup represents a fully analyzed setup for one symbol and direction.
type Setup struct {
	Symbol             string         `json:"symbol" yaml:"symbol"`
	Direction          Direction      `json:"direction" yaml:"direction"`
	SetupType          *string        `json:"setup_type,omitempty" yaml:"setup_type,omitempty"`
	ActionState        ActionState    `json:"action_state" yaml:"action_state"`
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
	Symbol          string    `json:"symbol" yaml:"symbol"`
	Timeframe       string    `json:"timeframe" yaml:"timeframe"`
	High            float64   `json:"high" yaml:"high"`
	Low             float64   `json:"low" yaml:"low"`
	StartTime       float64   `json:"start_time" yaml:"start_time"`
	EndTime         float64   `json:"end_time" yaml:"end_time"`
	Status          BoxStatus `json:"status" yaml:"status"`
	TouchHigh       int       `json:"touch_high" yaml:"touch_high"`
	TouchLow        int       `json:"touch_low" yaml:"touch_low"`
	SecondTestHigh  bool      `json:"second_test_high" yaml:"second_test_high"`
	SecondTestLow   bool      `json:"second_test_low" yaml:"second_test_low"`
	ConvergencePct  float64   `json:"convergence_pct" yaml:"convergence_pct"`
	VolumeDeclining bool      `json:"volume_declining" yaml:"volume_declining"`
	BreakoutPrice   *float64  `json:"breakout_price,omitempty" yaml:"breakout_price,omitempty"`
	BreakoutTime    *float64  `json:"breakout_time,omitempty" yaml:"breakout_time,omitempty"`
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

// IsReady returns whether the box is ready for breakout (has second test and convergence).
func (b *BoxPattern) IsReady() bool {
	return (b.SecondTestHigh || b.SecondTestLow) && b.ConvergencePct > 0.7
}

// AlertEvent represents a hard-data market alert for Delivery Channel fan-out.
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
// Timestamps are Unix seconds, matching Candle.Timestamp.
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
	// Exchange is a legacy alias for exchanges.primary / dataManager.exchanges.
	// Load normalizes it into those fields; new configs should not set it.
	Exchange        string                `mapstructure:"exchange" json:"exchange,omitempty" yaml:"exchange,omitempty"`
	Telegram        TelegramConfig        `mapstructure:"telegram" json:"telegram" yaml:"telegram"`
	DingTalk        DingTalkConfig        `mapstructure:"dingTalk" json:"dingTalk" yaml:"dingTalk"`
	DataManager     DataManagerConfig     `mapstructure:"dataManager" json:"dataManager" yaml:"dataManager"`
	AlertPolicy     AlertPolicyConfig     `mapstructure:"alertPolicy" json:"alertPolicy" yaml:"alertPolicy"`
	PriceVelocity   PriceVelocityConfig   `mapstructure:"priceVelocity" json:"priceVelocity" yaml:"priceVelocity"`
	VolumeSpike     VolumeSpikeConfig     `mapstructure:"volumeSpike" json:"volumeSpike" yaml:"volumeSpike"`
	FuturesMetrics  FuturesMetricsConfig  `mapstructure:"futuresMetrics" json:"futuresMetrics" yaml:"futuresMetrics"`
	LongShortRatio  LongShortRatioConfig  `mapstructure:"longShortRatio" json:"longShortRatio" yaml:"longShortRatio"`
	Liquidation     LiquidationConfig     `mapstructure:"liquidation" json:"liquidation" yaml:"liquidation"`
	ResonanceScorer ResonanceScorerConfig `mapstructure:"resonanceScorer" json:"resonanceScorer" yaml:"resonanceScorer"`
	Exchanges       ExchangesConfig       `mapstructure:"exchanges" json:"exchanges" yaml:"exchanges"`
	Storage         StorageConfig         `mapstructure:"storage" json:"storage" yaml:"storage"`
	MarketPulse     MarketPulseConfig     `mapstructure:"marketPulse" json:"marketPulse" yaml:"marketPulse"`
	Opportunity     OpportunityConfig     `mapstructure:"opportunity" json:"opportunity" yaml:"opportunity"`
	RiskGate        RiskGateConfig        `mapstructure:"riskGate" json:"riskGate" yaml:"riskGate"`
	Notice          NoticeConfig          `mapstructure:"notice" json:"notice" yaml:"notice"`
}

// RiskGateConfig mechanizes KAIROS_DOCTRINE §9b behavior rules
// (evaluated in internal/decision; numbers config-owned per Canonical §9).
type RiskGateConfig struct {
	Enabled bool `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	// CooldownMinutes: 同币亏损平仓后的再入冷却(§9b 规则 3, 30–60)。
	CooldownMinutes int `mapstructure:"cooldownMinutes" json:"cooldownMinutes" yaml:"cooldownMinutes"`
	// WindowStartHour/WindowEndHour: live 窗口(本地时区小时,跨午夜;§9b 规则 5)。
	WindowStartHour int `mapstructure:"windowStartHour" json:"windowStartHour" yaml:"windowStartHour"`
	WindowEndHour   int `mapstructure:"windowEndHour" json:"windowEndHour" yaml:"windowEndHour"`
	// Timezone: IANA 时区名(默认 "Asia/Shanghai",回退 UTC+8)。
	Timezone string `mapstructure:"timezone" json:"timezone,omitempty" yaml:"timezone,omitempty"`
	// TradingBankrollUSD: Engine A 合约实盘本金口径(默认 $40)。
	TradingBankrollUSD float64 `mapstructure:"tradingBankrollUSD" json:"tradingBankrollUSD,omitempty" yaml:"tradingBankrollUSD,omitempty"`
	// MaxLossUSD: 单笔预设最大损失上限(§9b 规则 6, 挑战账户 $100 口径)。
	MaxLossUSD float64 `mapstructure:"maxLossUSD" json:"maxLossUSD" yaml:"maxLossUSD"`
	// EnforceLeverageCap: 机械执行杠杆硬上限(默认 true: BTC/ETH <=10x, 山寨 <=5x)。
	EnforceLeverageCap *bool   `mapstructure:"enforceLeverageCap" json:"enforceLeverageCap,omitempty" yaml:"enforceLeverageCap,omitempty"`
	MaxMajorLeverage   float64 `mapstructure:"maxMajorLeverage" json:"maxMajorLeverage,omitempty" yaml:"maxMajorLeverage,omitempty"`
	MaxAltLeverage     float64 `mapstructure:"maxAltLeverage" json:"maxAltLeverage,omitempty" yaml:"maxAltLeverage,omitempty"`
	TrancheBaseUSD     float64 `mapstructure:"trancheBaseUSD" json:"trancheBaseUSD,omitempty" yaml:"trancheBaseUSD,omitempty"`
	// Phase 3b A1: 同点位试错预算上限(默认 3,即滚动窗口内第 4 张票硬拒;默认窗口 24h)。
	TrialBudgetMax         int     `mapstructure:"trialBudgetMax" json:"trialBudgetMax,omitempty" yaml:"trialBudgetMax,omitempty"`
	TrialBudgetWindowHours float64 `mapstructure:"trialBudgetWindowHours" json:"trialBudgetWindowHours,omitempty" yaml:"trialBudgetWindowHours,omitempty"`
	// Phase 3b A4: STRATEGY §12 犯病图谱事实警告阈值(只警告、永不拦截)。
	TiltReverseMinutes    int     `mapstructure:"tiltReverseMinutes" json:"tiltReverseMinutes,omitempty" yaml:"tiltReverseMinutes,omitempty"`
	ChurnConsecutiveStops int     `mapstructure:"churnConsecutiveStops" json:"churnConsecutiveStops,omitempty" yaml:"churnConsecutiveStops,omitempty"`
	ChurnWindowMinutes    int     `mapstructure:"churnWindowMinutes" json:"churnWindowMinutes,omitempty" yaml:"churnWindowMinutes,omitempty"`
	ChurnTicketCount      int     `mapstructure:"churnTicketCount" json:"churnTicketCount,omitempty" yaml:"churnTicketCount,omitempty"`
	SizeEscalationMult    float64 `mapstructure:"sizeEscalationMult" json:"sizeEscalationMult,omitempty" yaml:"sizeEscalationMult,omitempty"`
	RecentRiskSampleSize  int     `mapstructure:"recentRiskSampleSize" json:"recentRiskSampleSize,omitempty" yaml:"recentRiskSampleSize,omitempty"`
}

// OpportunityConfig gates the decision-desk path off MarketPulse.
type OpportunityConfig struct {
	Enabled              bool `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	ShadowMode           bool `mapstructure:"shadowMode" json:"shadowMode" yaml:"shadowMode"`
	MaxTicketsPerSession int  `mapstructure:"maxTicketsPerSession" json:"maxTicketsPerSession" yaml:"maxTicketsPerSession"`
	// AssumeSpreadOK allows tickets without L2 spread (shadow only; default false).
	AssumeSpreadOK bool `mapstructure:"assumeSpreadOK" json:"assumeSpreadOK" yaml:"assumeSpreadOK"`
	// OutcomeMaxAgeHours caps forward tracking (default 6).
	OutcomeMaxAgeHours float64 `mapstructure:"outcomeMaxAgeHours" json:"outcomeMaxAgeHours" yaml:"outcomeMaxAgeHours"`
	// RiskBudgets maps risk template (camelCase key) → risk_budget_pct of
	// equity. Canonical §9: numeric pct lives in config, not code constants.
	RiskBudgets map[string]float64 `mapstructure:"riskBudgets" json:"riskBudgets" yaml:"riskBudgets"`
	// MaxLeverage caps leverage on decision tickets (0 → 5 fallback).
	MaxLeverage float64 `mapstructure:"maxLeverage" json:"maxLeverage" yaml:"maxLeverage"`
	// MaxMajorLeverage / MaxAltLeverage: per-asset-class hard caps (defaults: BTC/ETH 10x, alts 5x).
	MaxMajorLeverage float64 `mapstructure:"maxMajorLeverage" json:"maxMajorLeverage,omitempty" yaml:"maxMajorLeverage,omitempty"`
	MaxAltLeverage   float64 `mapstructure:"maxAltLeverage" json:"maxAltLeverage,omitempty" yaml:"maxAltLeverage,omitempty"`
	// TrancheBaseUSD is the fixed accounting tranche base (e.g. 10000; profit-withdraw / loss-refill accounting only).
	TrancheBaseUSD float64 `mapstructure:"trancheBaseUSD" json:"trancheBaseUSD,omitempty" yaml:"trancheBaseUSD,omitempty"`
}

// TelegramConfig holds Telegram delivery settings.
// Secrets come from env only and are excluded from every serialization format.
type TelegramConfig struct {
	Enabled  bool   `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	BotToken string `mapstructure:"-" json:"-" yaml:"-"` // env: TELEGRAM_BOT_TOKEN
	ChatID   string `mapstructure:"-" json:"-" yaml:"-"` // env: TELEGRAM_CHAT_ID
}

// DingTalkConfig holds DingTalk custom-robot webhook delivery settings.
// Secrets come from env only and are excluded from every serialization format.
type DingTalkConfig struct {
	Enabled    bool   `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	WebhookURL string `mapstructure:"-" json:"-" yaml:"-"` // env: DINGTALK_WEBHOOK_URL
	Secret     string `mapstructure:"-" json:"-" yaml:"-"` // env: DINGTALK_SECRET (optional sign)
}

// DataManagerConfig controls kairosd realtime ticker subscriptions and
// delivery dedup/cooldown windows.
type DataManagerConfig struct {
	Exchanges             []string `mapstructure:"exchanges" json:"exchanges" yaml:"exchanges"`
	TopSymbols            int      `mapstructure:"topSymbols" json:"topSymbols" yaml:"topSymbols"`
	RefreshIntervalHours  int      `mapstructure:"refreshIntervalHours" json:"refreshIntervalHours" yaml:"refreshIntervalHours"`
	DedupWindowSeconds    int      `mapstructure:"dedupWindowSeconds" json:"dedupWindowSeconds" yaml:"dedupWindowSeconds"`
	SymbolCooldownMinutes int      `mapstructure:"symbolCooldownMinutes" json:"symbolCooldownMinutes" yaml:"symbolCooldownMinutes"`
	// FeedSilentWarnSeconds: a connected ticker feed that delivers nothing for
	// this long logs a throttled WARN (default 300 = 5m; 0 disables). Logging
	// only — reconnect policy is a separate concern.
	FeedSilentWarnSeconds int `mapstructure:"feedSilentWarnSeconds" json:"feedSilentWarnSeconds" yaml:"feedSilentWarnSeconds"`
}

// LiquidityWeightConfig down-weights alerts for low market-cap symbols.
// Caps are fetched once from CoinGlass at subscribe/refresh (not realtime).
type LiquidityWeightConfig struct {
	Enabled      bool     `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	MinWeight    float64  `mapstructure:"minWeight" json:"minWeight" yaml:"minWeight"`
	MajorSymbols []string `mapstructure:"majorSymbols" json:"majorSymbols" yaml:"majorSymbols"`
}

// AlertPolicyConfig defines which event types and severity thresholds trigger alerts.
type AlertPolicyConfig struct {
	Enabled                  bool                  `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	AllowedEventTypes        []string              `mapstructure:"allowedEventTypes" json:"allowedEventTypes" yaml:"allowedEventTypes"`
	MinSeverity              string                `mapstructure:"minSeverity" json:"minSeverity" yaml:"minSeverity"`
	MinPriceChangePct        float64               `mapstructure:"minPriceChangePct" json:"minPriceChangePct" yaml:"minPriceChangePct"`
	MinVolumeRatio           float64               `mapstructure:"minVolumeRatio" json:"minVolumeRatio" yaml:"minVolumeRatio"`
	MinOpenInterestChangePct float64               `mapstructure:"minOpenInterestChangePct" json:"minOpenInterestChangePct" yaml:"minOpenInterestChangePct"`
	MinFundingRateAbs        float64               `mapstructure:"minFundingRateAbs" json:"minFundingRateAbs" yaml:"minFundingRateAbs"`
	MinFundingRateChangeAbs  float64               `mapstructure:"minFundingRateChangeAbs" json:"minFundingRateChangeAbs" yaml:"minFundingRateChangeAbs"`
	LiquidityWeight          LiquidityWeightConfig `mapstructure:"liquidityWeight" json:"liquidityWeight" yaml:"liquidityWeight"`
}

// PriceVelocityConfig defines short-term price velocity thresholds.
type PriceVelocityConfig struct {
	Enabled         bool          `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	Windows         []PriceWindow `mapstructure:"windows" json:"windows" yaml:"windows"`
	CooldownSeconds int           `mapstructure:"cooldownSeconds" json:"cooldownSeconds" yaml:"cooldownSeconds"`
}

// PriceWindow defines a single velocity observation window.
type PriceWindow struct {
	Seconds   int     `mapstructure:"seconds" json:"seconds" yaml:"seconds"`
	Threshold float64 `mapstructure:"threshold" json:"threshold" yaml:"threshold"`
}

// VolumeSpikeConfig defines volume spike detection parameters.
type VolumeSpikeConfig struct {
	Enabled           bool    `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	Multiplier        float64 `mapstructure:"multiplier" json:"multiplier" yaml:"multiplier"`
	WindowMinutes     int     `mapstructure:"windowMinutes" json:"windowMinutes" yaml:"windowMinutes"`
	MinHistorySeconds int     `mapstructure:"minHistorySeconds" json:"minHistorySeconds" yaml:"minHistorySeconds"`
	MinNotifyInterval string  `mapstructure:"minNotifyInterval" json:"minNotifyInterval" yaml:"minNotifyInterval"`
}

// FuturesMetricsConfig controls open interest and funding rate polling.
type FuturesMetricsConfig struct {
	Enabled             bool              `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	PollIntervalSeconds int               `mapstructure:"pollIntervalSeconds" json:"pollIntervalSeconds" yaml:"pollIntervalSeconds"`
	OpenInterest        OIConfig          `mapstructure:"openInterest" json:"openInterest" yaml:"openInterest"`
	FundingRate         FundingRateConfig `mapstructure:"fundingRate" json:"fundingRate" yaml:"fundingRate"`
}

// OIConfig controls open interest change detection.
type OIConfig struct {
	Enabled           bool    `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	MinChangePct      float64 `mapstructure:"minChangePct" json:"minChangePct" yaml:"minChangePct"`
	MinNotifyInterval string  `mapstructure:"minNotifyInterval" json:"minNotifyInterval" yaml:"minNotifyInterval"`
}

// FundingRateConfig controls funding rate anomaly detection.
type FundingRateConfig struct {
	Enabled           bool    `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	AbsRateThreshold  float64 `mapstructure:"absRateThreshold" json:"absRateThreshold" yaml:"absRateThreshold"`
	MinChangeAbs      float64 `mapstructure:"minChangeAbs" json:"minChangeAbs" yaml:"minChangeAbs"`
	MinNotifyInterval string  `mapstructure:"minNotifyInterval" json:"minNotifyInterval" yaml:"minNotifyInterval"`
}

// LongShortRatioConfig controls long/short ratio anomaly detection.
type LongShortRatioConfig struct {
	Enabled              bool    `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	PollIntervalSeconds  int     `mapstructure:"pollIntervalSeconds" json:"pollIntervalSeconds" yaml:"pollIntervalSeconds"`
	AbsThreshold         float64 `mapstructure:"absThreshold" json:"absThreshold" yaml:"absThreshold"`
	ZscoreThreshold      float64 `mapstructure:"zscoreThreshold" json:"zscoreThreshold" yaml:"zscoreThreshold"`
	ZscoreWindow         int     `mapstructure:"zscoreWindow" json:"zscoreWindow" yaml:"zscoreWindow"`
	VelocityThresholdPct float64 `mapstructure:"velocityThresholdPct" json:"velocityThresholdPct" yaml:"velocityThresholdPct"`
	MinNotifyInterval    string  `mapstructure:"minNotifyInterval" json:"minNotifyInterval" yaml:"minNotifyInterval"`
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
	Enabled         bool    `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	WindowSeconds   int     `mapstructure:"windowSeconds" json:"windowSeconds" yaml:"windowSeconds"`
	MinDimensions   int     `mapstructure:"minDimensions" json:"minDimensions" yaml:"minDimensions"`
	MinScore        float64 `mapstructure:"minScore" json:"minScore" yaml:"minScore"`
	CooldownSeconds int     `mapstructure:"cooldownSeconds" json:"cooldownSeconds" yaml:"cooldownSeconds"`
}

// ExchangesConfig defines exchange selection. Symbol normalization is fixed
// to canonical USDT perpetuals in the adapters.
type ExchangesConfig struct {
	Primary string `mapstructure:"primary" json:"primary" yaml:"primary"`
	// WSEndpoints overrides adapter ticker WebSocket endpoints. Empty means the
	// adapter default, so an exchange-side endpoint migration is config-only.
	WSEndpoints WSEndpointsConfig `mapstructure:"wsEndpoints" json:"wsEndpoints,omitempty" yaml:"wsEndpoints,omitempty"`
}

// WSEndpointsConfig holds per-exchange WebSocket endpoint overrides.
type WSEndpointsConfig struct {
	OKX string `mapstructure:"okx" json:"okx,omitempty" yaml:"okx,omitempty"`
}

// StorageConfig defines persistence configuration. DatabasePath anchors the
// storage directory; MarketPulse events/outcomes and watch hints are JSONL
// sidecar files next to it.
type StorageConfig struct {
	DatabasePath            string  `mapstructure:"databasePath" json:"databasePath" yaml:"databasePath"`
	WatchHintsPath          string  `mapstructure:"watchHintsPath" json:"watchHintsPath" yaml:"watchHintsPath"`
	WatchHintRetentionHours float64 `mapstructure:"watchHintRetentionHours" json:"watchHintRetentionHours" yaml:"watchHintRetentionHours"`
	WatchHintScoreBoost     float64 `mapstructure:"watchHintScoreBoost" json:"watchHintScoreBoost" yaml:"watchHintScoreBoost"`
}
