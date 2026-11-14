package cache

// TradeFill mirrors a single Binance USDT-M execution fill cached in desktop-cache.db.
type TradeFill struct {
	FillID          string  `json:"fill_id"`
	OrderID         string  `json:"order_id"`
	Symbol          string  `json:"symbol"`
	Side            string  `json:"side"`
	PositionSide    string  `json:"position_side"`
	Price           float64 `json:"price"`
	Qty             float64 `json:"qty"`
	QuoteQty        float64 `json:"quote_qty"`
	Commission      float64 `json:"commission"`
	CommissionAsset string  `json:"commission_asset"`
	RealizedPnL     float64 `json:"realized_pnl"`
	TimeMs          int64   `json:"time_ms"`
	TradeID         string  `json:"trade_id"`
}

// SyncedTrade represents a FIFO-aggregated position lifecycle (open or closed)
// keyed by (symbol, position_side). FundingUSD uses Binance signed cashflow
// convention (negative = paid, positive = received), so
// NetPnL = RealizedPnL - FeeUSD + FundingUSD.
type SyncedTrade struct {
	ID             string      `json:"id"`
	Symbol         string      `json:"symbol"`
	Direction      string      `json:"direction"`
	PositionSide   string      `json:"position_side"`
	MarginMode     string      `json:"margin_mode"`
	Leverage       float64     `json:"leverage"`
	EntryPrice     float64     `json:"entry_price"`
	ExitPrice      float64     `json:"exit_price"`
	Qty            float64     `json:"qty"`
	EntryTimeMs    int64       `json:"entry_time_ms"`
	ExitTimeMs     int64       `json:"exit_time_ms"`
	RealizedPnL    float64     `json:"realized_pnl"`
	FeeUSD         float64     `json:"fee_usd"`
	FundingUSD     float64     `json:"funding_usd"`
	NetPnL         float64     `json:"net_pnl"`
	Margin         float64     `json:"margin"`
	LiqPrice       float64     `json:"liq_price"`
	Status         string      `json:"status"`
	LinkedTicketID string      `json:"linked_ticket_id,omitempty"`
	GateCode       string      `json:"gate_code,omitempty"`
	Fills          []TradeFill `json:"fills,omitempty"`
}

// OpenPosition holds a live USDT-M position snapshot from Binance /fapi/v2/positionRisk.
type OpenPosition struct {
	Symbol         string  `json:"symbol"`
	PositionSide   string  `json:"position_side"`
	MarginMode     string  `json:"margin_mode"`
	Leverage       float64 `json:"leverage"`
	Qty            float64 `json:"qty"`
	EntryPrice     float64 `json:"entry_price"`
	MarkPrice      float64 `json:"mark_price"`
	UnrealizedPnL  float64 `json:"unrealized_pnl"`
	LiqPrice       float64 `json:"liq_price"`
	IsolatedWallet float64 `json:"isolated_wallet"`
	UpdatedAt      int64   `json:"updated_at"`
	LinkedTicketID string  `json:"linked_ticket_id,omitempty"`
}

// ChartDrawingPoint is a single anchor point (time in Unix seconds, price).
type ChartDrawingPoint struct {
	Time  int64   `json:"time"`
	Price float64 `json:"price"`
}

// ChartDrawing is a persistent user chart drawing (hline, trend, region, fib).
type ChartDrawing struct {
	ID        int64               `json:"id"`
	Symbol    string              `json:"symbol"`
	Kind      string              `json:"kind"`
	Points    []ChartDrawingPoint `json:"points"`
	Label     string              `json:"label,omitempty"`
	CreatedAt int64               `json:"created_at"`
	UpdatedAt int64               `json:"updated_at"`
}

// IncomeRecord caches raw Binance USDT-M income ledger entries (e.g. FUNDING_FEE).
type IncomeRecord struct {
	TranID     string  `json:"tran_id"`
	Symbol     string  `json:"symbol"`
	IncomeType string  `json:"income_type"`
	Income     float64 `json:"income"`
	Asset      string  `json:"asset"`
	TimeMs     int64   `json:"time_ms"`
}
