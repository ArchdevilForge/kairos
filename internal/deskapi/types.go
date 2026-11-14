// Package deskapi implements the local loopback HTTP & WebSocket API for Kairos Desk.
// It is strictly read-only toward exchange accounts (zero order-placement endpoints)
// and delegates behavior-gate enforcement and journal persistence to internal/decision
// and internal/storage.
package deskapi

import (
	"github.com/ArchdevilForge/kairos/internal/cache"
	"github.com/ArchdevilForge/kairos/internal/evaluation"
	"github.com/ArchdevilForge/kairos/internal/exchange"
	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
)

// Bar mirrors desktop/src/types/kairos.ts Bar (time in Unix seconds).
type Bar struct {
	Time     int64   `json:"time"`
	Open     float64 `json:"open"`
	High     float64 `json:"high"`
	Low      float64 `json:"low"`
	Close    float64 `json:"close"`
	Volume   float64 `json:"volume"`
	QuoteVol float64 `json:"quote_vol"`
}

// MarketBarsResponse is returned by GET /api/v1/market/bars.
type MarketBarsResponse struct {
	Symbol      string                  `json:"symbol"`
	TF          string                  `json:"tf"`
	Bars        []Bar                   `json:"bars"`
	MarketCycle *types.CycleMap         `json:"market_cycle,omitempty"`
	SymbolCycle *types.CycleMap         `json:"symbol_cycle,omitempty"`
	Box         *types.BoxPattern       `json:"box,omitempty"`
	Metrics     []exchange.MetricsPoint `json:"metrics"`
}

// PulseStatus summarizes the live MarketPulse state for TopStatusBar and WS updates.
type PulseStatus struct {
	State           types.MarketState     `json:"state"`
	Direction       string                `json:"direction,omitempty"`
	UpdatedAt       int64                 `json:"updated_at,omitempty"`
	DataOK          bool                  `json:"data_ok"`
	MedianReturn60s float64               `json:"median_return_60s"`
	MedianReturn300 float64               `json:"median_return_300s"`
	MedianZ60s      float64               `json:"median_z_60s"`
	UpBreadth60s    float64               `json:"up_breadth_60s"`
	DownBreadth60s  float64               `json:"down_breadth_60s"`
	Snapshot        *types.MarketSnapshot `json:"snapshot,omitempty"`
}

// CooldownEntry describes an active per-symbol loss cooldown timer.
type CooldownEntry struct {
	Symbol       string `json:"symbol"`
	ExpiresAt    int64  `json:"expires_at"`
	RemainingSec int64  `json:"remaining_sec"`
}

// GateHealth reports active 24h locks and cooldowns for the status bar.
type GateHealth struct {
	Locked24hSymbols    []string        `json:"locked_24h_symbols"`
	ActiveCooldowns     []CooldownEntry `json:"active_cooldowns"`
	ConsecutiveStops24h int             `json:"consecutive_stops_24h"`
}

// RiskConfigView exposes the active risk gate bounds to the desktop UI.
type RiskConfigView struct {
	TradingBankrollUSD float64 `json:"trading_bankroll_usd"`
	MaxLossUSD         float64 `json:"max_loss_usd"`
	MaxMajorLeverage   float64 `json:"max_major_leverage"`
	MaxAltLeverage     float64 `json:"max_alt_leverage"`
	CooldownMinutes    int     `json:"cooldown_minutes"`
}

// AccountStatusView holds redacted read-only account state (never exposes secrets).
type AccountStatusView struct {
	Configured       bool    `json:"configured"`
	APIKeyMasked     string  `json:"api_key_masked"`
	WalletBalanceUSD float64 `json:"wallet_balance_usd"`
	UnrealizedPnLUSD float64 `json:"unrealized_pnl_usd"`
	LastSyncAt       int64   `json:"last_sync_at"`
	SyncError        string  `json:"sync_error,omitempty"`
}

// DeskStatus is status inside BootstrapPayload.
type DeskStatus struct {
	Exchange          string             `json:"exchange"`
	Symbols           []string           `json:"symbols"`
	AvailableSymbols  []string           `json:"available_symbols,omitempty"`
	Timezone          string             `json:"timezone"`
	InLiveWindow      bool               `json:"in_live_window"`
	WindowStartHour   int                `json:"window_start_hour"`
	WindowEndHour     int                `json:"window_end_hour"`
	Pulse             PulseStatus        `json:"pulse"`
	SeasonRegime      types.SeasonRegime `json:"season_regime"`
	WinterDefenseHint string             `json:"winter_defense_hint,omitempty"`
	MarketCycle       *types.CycleMap    `json:"market_cycle,omitempty"`
	GateHealth        GateHealth         `json:"gate_health"`
	RiskConfig        RiskConfigView     `json:"risk_config"`
	Account           AccountStatusView  `json:"account"`
}

// TicketBundle joins a DecisionTicket with its authoritative journal records and cached execution details.
type TicketBundle struct {
	Ticket      types.DecisionTicket           `json:"ticket"`
	Decision    *types.DecisionRecord          `json:"decision,omitempty"`
	Outcome     *storage.CounterfactualOutcome `json:"outcome,omitempty"`
	Annotations []storage.GateAnnotation       `json:"annotations"`
	Fills       []cache.TradeFill              `json:"fills,omitempty"`
	LinkedTrade *cache.SyncedTrade             `json:"linked_trade,omitempty"`
}

// BootstrapPayload is returned by GET /api/v1/bootstrap.
type BootstrapPayload struct {
	Status          DeskStatus           `json:"status"`
	Tickets         []TicketBundle       `json:"tickets"`
	UnplannedTrades []cache.SyncedTrade  `json:"unplanned_trades"`
	OpenPositions   []cache.OpenPosition `json:"open_positions"`
	Notices         []types.Notice       `json:"notices"`
	Anomalies       []types.AnomalyEvent `json:"anomalies"`
}

// CreateDrawingRequest is accepted by POST /api/v1/drawings.
type CreateDrawingRequest struct {
	Symbol string                    `json:"symbol"`
	Kind   string                    `json:"kind"`
	Points []cache.ChartDrawingPoint `json:"points"`
	Label  string                    `json:"label,omitempty"`
}

// UpdateDrawingRequest is accepted by PUT /api/v1/drawings/{id}.
type UpdateDrawingRequest struct {
	Points []cache.ChartDrawingPoint `json:"points"`
	Label  *string                   `json:"label,omitempty"`
}

// ReplayStateResponse is returned by GET /api/v1/replay/state with strict zero-lookahead <= cursor_ts.
type ReplayStateResponse struct {
	Symbol      string               `json:"symbol"`
	CursorTS    int64                `json:"cursor_ts"`
	BarsByTF    map[string][]Bar     `json:"bars_by_tf"`
	Drawings    []cache.ChartDrawing `json:"drawings"`
	Anomalies   []types.AnomalyEvent `json:"anomalies"`
	Notices     []types.Notice       `json:"notices"`
	Tickets     []TicketBundle       `json:"tickets"`
	Fills       []cache.TradeFill    `json:"fills"`
	MarketCycle *types.CycleMap      `json:"market_cycle,omitempty"`
	SymbolCycle *types.CycleMap      `json:"symbol_cycle,omitempty"`
	Box         *types.BoxPattern    `json:"box,omitempty"`
}

// UpdateSettingsRequest is accepted by PUT /api/v1/settings.
type UpdateSettingsRequest struct {
	TradingBankrollUSD       *float64 `json:"trading_bankroll_usd,omitempty"`
	MaxLossUSD               *float64 `json:"max_loss_usd,omitempty"`
	Timezone                 *string  `json:"timezone,omitempty"`
	BinanceReadonlyAPIKey    *string  `json:"binance_readonly_api_key,omitempty"`
	BinanceReadonlyAPISecret *string  `json:"binance_readonly_api_secret,omitempty"`
}

// AccountSyncResponse is returned by POST /api/v1/account/sync.
type AccountSyncResponse struct {
	Ok            bool              `json:"ok"`
	SyncedTrades  int               `json:"synced_trades"`
	SyncedFills   int               `json:"synced_fills"`
	OpenPositions int               `json:"open_positions"`
	Account       AccountStatusView `json:"account"`
}

// EquityPoint is one point on the trading bankroll equity & drawdown curve.
type EquityPoint struct {
	Time        int64    `json:"time"`
	BankrollUSD float64  `json:"bankroll_usd"`
	WalletUSD   *float64 `json:"wallet_usd,omitempty"`
	TradePnLUSD float64  `json:"trade_pnl_usd"`
	CumulativeR float64  `json:"cumulative_r"`
	DrawdownPct float64  `json:"drawdown_pct"`
}

// WalletReconciliation reconciles TradingBankroll + realized net PnL against Binance wallet balance.
type WalletReconciliation struct {
	TradingBankrollUSD  float64 `json:"trading_bankroll_usd"`
	RealizedPnLUSD      float64 `json:"realized_pnl_usd"`
	FeeUSD              float64 `json:"fee_usd"`
	FundingUSD          float64 `json:"funding_usd"`
	NetPnLUSD           float64 `json:"net_pnl_usd"`
	ExpectedBankrollUSD float64 `json:"expected_bankroll_usd"`
	WalletBalanceUSD    float64 `json:"wallet_balance_usd"`
	UnrealizedPnLUSD    float64 `json:"unrealized_pnl_usd"`
	DiscrepancyUSD      float64 `json:"discrepancy_usd"`
	LastSyncAt          int64   `json:"last_sync_at"`
}

// RBucket represents one bin of the realized R histogram.
type RBucket struct {
	Label     string  `json:"label"`
	MinR      float64 `json:"min_r"`
	MaxR      float64 `json:"max_r"`
	Count     int     `json:"count"`
	NetPnLUSD float64 `json:"net_pnl_usd"`
}

// RDistribution summarizes R-multiple outcomes with real leverage, fees, funding, and missing-stop count.
type RDistribution struct {
	Buckets      []RBucket `json:"buckets"`
	TotalTrades  int       `json:"total_trades"`
	WithoutStop  int       `json:"without_stop"`
	MeanR        float64   `json:"mean_r"`
	MedianR      float64   `json:"median_r"`
	WinRate      float64   `json:"win_rate"`
	ProfitFactor float64   `json:"profit_factor"`
}

// GateComplianceReport summarizes BehaviorGate & AuditExit adherence.
type GateComplianceReport struct {
	TotalAnnotations         int            `json:"total_annotations"`
	Overrides                int            `json:"overrides"`
	RejectionsByCode         map[string]int `json:"rejections_by_code"`
	ProtectiveStopExemptions int            `json:"protective_stop_exemptions"`
	EarlyProfitNotes         int            `json:"early_profit_notes"`
	ScalpViolations          int            `json:"scalp_violations"`
	UnplannedTradeCount      int            `json:"unplanned_trade_count"`
	ComplianceScore          float64        `json:"compliance_score"`
}

// HourlyBucket summarizes closed trade metrics by UTC+8 hour-of-day.
type HourlyBucket struct {
	Hour         int     `json:"hour"`
	Label        string  `json:"label"`
	Count        int     `json:"count"`
	Wins         int     `json:"wins"`
	WinRate      float64 `json:"win_rate"`
	NetPnLUSD    float64 `json:"net_pnl_usd"`
	MeanR        float64 `json:"mean_r"`
	InLiveWindow bool    `json:"in_live_window"`
}

// TagMetricRow aggregates trade performance by review tag, grade, or emotion.
type TagMetricRow struct {
	Tag            string  `json:"tag"`
	Count          int     `json:"count"`
	Wins           int     `json:"wins"`
	WinRate        float64 `json:"win_rate"`
	NetPnLUSD      float64 `json:"net_pnl_usd"`
	MeanR          float64 `json:"mean_r"`
	SampleTicketID string  `json:"sample_ticket_id,omitempty"`
	SampleTradeID  string  `json:"sample_trade_id,omitempty"`
	SampleSymbol   string  `json:"sample_symbol,omitempty"`
	SampleTS       int64   `json:"sample_ts,omitempty"`
}

// TagBreakdown groups review tag analytics for StatsWorkspace.
type TagBreakdown struct {
	SetupTags []TagMetricRow `json:"setup_tags"`
	ErrorTags []TagMetricRow `json:"error_tags"`
	Grades    []TagMetricRow `json:"grades"`
	Emotions  []TagMetricRow `json:"emotions"`
}

// StatsPayload is returned by GET /api/v1/stats.
type StatsPayload struct {
	EquityCurve          []EquityPoint            `json:"equity_curve"`
	WalletReconciliation WalletReconciliation     `json:"wallet_reconciliation"`
	Summary              evaluation.SummaryReport `json:"summary"`
	RDistribution        RDistribution            `json:"r_distribution"`
	GateCompliance       GateComplianceReport     `json:"gate_compliance"`
	HourlyUTC8           []HourlyBucket           `json:"hourly_utc8"`
	TagBreakdown         TagBreakdown             `json:"tag_breakdown"`
}
