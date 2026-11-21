package exchange

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ArchdevilForge/kairos/internal/cache"
	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
)

// BinanceAccountSummary holds the non-secret USD summary from GET /fapi/v2/account.
type BinanceAccountSummary struct {
	WalletBalanceUSD  float64
	UnrealizedPnLUSD  float64
	MarginBalanceUSD  float64
	AvailableBalance  float64
	PositionMarginUSD float64
}

// BinancePositionRisk mirrors an item from GET /fapi/v2/positionRisk.
type BinancePositionRisk struct {
	Symbol           string `json:"symbol"`
	PositionAmt      string `json:"positionAmt"`
	EntryPrice       string `json:"entryPrice"`
	MarkPrice        string `json:"markPrice"`
	UnRealizedProfit string `json:"unRealizedProfit"`
	LiquidationPrice string `json:"liquidationPrice"`
	Leverage         string `json:"leverage"`
	MarginType       string `json:"marginType"`
	PositionSide     string `json:"positionSide"`
	IsolatedWallet   string `json:"isolatedWallet"`
	UpdateTime       int64  `json:"updateTime"`
}

// BinanceUserTrade mirrors an item from GET /fapi/v1/userTrades.
type BinanceUserTrade struct {
	ID              int64  `json:"id"`
	OrderID         int64  `json:"orderId"`
	Symbol          string `json:"symbol"`
	Side            string `json:"side"`
	PositionSide    string `json:"positionSide"`
	Price           string `json:"price"`
	Qty             string `json:"qty"`
	QuoteQty        string `json:"quoteQty"`
	Commission      string `json:"commission"`
	CommissionAsset string `json:"commissionAsset"`
	RealizedPnL     string `json:"realizedPnl"`
	Time            int64  `json:"time"`
	Maker           bool   `json:"maker"`
}

// BinanceIncomeRow mirrors an item from GET /fapi/v1/income.
type BinanceIncomeRow struct {
	Symbol     string `json:"symbol"`
	IncomeType string `json:"incomeType"`
	Income     string `json:"income"`
	Asset      string `json:"asset"`
	Time       int64  `json:"time"`
	TranID     any    `json:"tranId"`
	Info       string `json:"info"`
}

// BinanceAccountClient is a strictly read-only Binance USDT-M account client.
// It exposes ONLY GET endpoints and never serializes or logs credentials.
type BinanceAccountClient struct {
	mu           sync.RWMutex
	apiKey       string `json:"-"`
	apiSecret    string `json:"-"`
	baseURL      string
	httpCli      *http.Client
	timeOffsetMs int64

	bnbMu    sync.Mutex
	bnbPrice float64
	bnbAt    time.Time
}

// NewBinanceAccountClient constructs a read-only Binance USDT-M client loading
// credentials from env or ~/.config/kairos/secrets.env.
func NewBinanceAccountClient() *BinanceAccountClient {
	k, s := LoadBinanceReadonlyKeys()
	return &BinanceAccountClient{
		apiKey:    k,
		apiSecret: s,
		baseURL:   binanceRESTEndpoint,
		httpCli:   newPooledHTTPClient(15 * time.Second),
	}
}

// Exchange identifies this client as "binance".
func (c *BinanceAccountClient) Exchange() string { return "binance" }

// HasKeys reports whether both API key and secret are configured.
func (c *BinanceAccountClient) HasKeys() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.apiKey != "" && c.apiSecret != ""
}

// APIKeyMasked returns the masked API key for UI display.
func (c *BinanceAccountClient) APIKeyMasked() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return MaskAPIKey(c.apiKey)
}

// SetKeys updates credentials in memory (never logged or serialized).
func (c *BinanceAccountClient) SetKeys(apiKey, apiSecret string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if k := cleanCredential(apiKey); k != "" {
		c.apiKey = k
	}
	if s := cleanCredential(apiSecret); s != "" {
		c.apiSecret = s
	}
}

// ReloadKeys refreshes credentials from env / ~/.config/kairos/secrets.env.
func (c *BinanceAccountClient) ReloadKeys() {
	if c == nil {
		return
	}
	k, s := LoadBinanceReadonlyKeys()
	c.mu.Lock()
	c.apiKey = k
	c.apiSecret = s
	c.mu.Unlock()
}

func (c *BinanceAccountClient) credentials() (string, string, int64) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.apiKey, c.apiSecret, c.timeOffsetMs
}

func (c *BinanceAccountClient) syncServerTime(ctx context.Context) {
	u := strings.TrimRight(c.baseURL, "/") + "/fapi/v1/time"
	body, err := doHTTPGet(ctx, c.httpCli, u)
	if err != nil {
		return
	}
	var resp struct {
		ServerTime int64 `json:"serverTime"`
	}
	if json.Unmarshal(body, &resp) == nil && resp.ServerTime > 0 {
		offset := resp.ServerTime - time.Now().UnixMilli()
		c.mu.Lock()
		c.timeOffsetMs = offset
		c.mu.Unlock()
	}
}

func (c *BinanceAccountClient) signedGet(ctx context.Context, path string, params url.Values) ([]byte, error) {
	for attempt := 0; attempt < 2; attempt++ {
		apiKey, apiSecret, offsetMs := c.credentials()
		if apiKey == "" || apiSecret == "" {
			return nil, fmt.Errorf("binance read-only api key/secret not configured")
		}
		q := url.Values{}
		for k, vs := range params {
			for _, v := range vs {
				q.Add(k, v)
			}
		}
		q.Set("recvWindow", "10000")
		q.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli()+offsetMs, 10))

		payload := q.Encode()
		mac := hmac.New(sha256.New, []byte(apiSecret))
		_, _ = mac.Write([]byte(payload))
		sig := hex.EncodeToString(mac.Sum(nil))

		fullURL := strings.TrimRight(c.baseURL, "/") + path + "?" + payload + "&signature=" + sig
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("X-MBX-APIKEY", apiKey)

		resp, err := c.httpCli.Do(req)
		if err != nil {
			return nil, fmt.Errorf("binance %s: %w", path, err)
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("binance %s read: %w", path, readErr)
		}
		if resp.StatusCode == http.StatusOK {
			return body, nil
		}

		var apiErr struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		if json.Unmarshal(body, &apiErr) == nil && apiErr.Code != 0 {
			if apiErr.Code == -1021 && attempt == 0 {
				c.syncServerTime(ctx)
				continue
			}
			return nil, fmt.Errorf("binance %s: code=%d msg=%s", path, apiErr.Code, apiErr.Msg)
		}
		return nil, fmt.Errorf("binance %s: http %d", path, resp.StatusCode)
	}
	return nil, fmt.Errorf("binance %s: timestamp sync retry exhausted", path)
}

// GetAccount calls GET /fapi/v2/account and extracts USD wallet & unrealized balances.
func (c *BinanceAccountClient) GetAccount(ctx context.Context) (*BinanceAccountSummary, error) {
	body, err := c.signedGet(ctx, "/fapi/v2/account", nil)
	if err != nil {
		return nil, err
	}
	var raw struct {
		TotalWalletBalance    string `json:"totalWalletBalance"`
		TotalUnrealizedProfit string `json:"totalUnrealizedProfit"`
		TotalMarginBalance    string `json:"totalMarginBalance"`
		AvailableBalance      string `json:"availableBalance"`
		TotalPositionMargin   string `json:"totalPositionInitialMargin"`
		Assets                []struct {
			Asset            string `json:"asset"`
			WalletBalance    string `json:"walletBalance"`
			UnrealizedProfit string `json:"unrealizedProfit"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode account: %w", err)
	}
	sum := &BinanceAccountSummary{
		WalletBalanceUSD:  parseFloat(raw.TotalWalletBalance),
		UnrealizedPnLUSD:  parseFloat(raw.TotalUnrealizedProfit),
		MarginBalanceUSD:  parseFloat(raw.TotalMarginBalance),
		AvailableBalance:  parseFloat(raw.AvailableBalance),
		PositionMarginUSD: parseFloat(raw.TotalPositionMargin),
	}
	for _, a := range raw.Assets {
		if strings.EqualFold(a.Asset, "USDT") {
			if wb := parseFloat(a.WalletBalance); wb > 0 || sum.WalletBalanceUSD == 0 {
				sum.WalletBalanceUSD = wb
			}
			if up := parseFloat(a.UnrealizedProfit); up != 0 || sum.UnrealizedPnLUSD == 0 {
				sum.UnrealizedPnLUSD = up
			}
			break
		}
	}
	return sum, nil
}

// GetPositionRisk calls GET /fapi/v2/positionRisk for all USDT-M symbols.
func (c *BinanceAccountClient) GetPositionRisk(ctx context.Context) ([]BinancePositionRisk, error) {
	body, err := c.signedGet(ctx, "/fapi/v2/positionRisk", nil)
	if err != nil {
		return nil, err
	}
	var rows []BinancePositionRisk
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("decode positionRisk: %w", err)
	}
	return rows, nil
}

// GetUserTrades calls GET /fapi/v1/userTrades for a single symbol.
func (c *BinanceAccountClient) GetUserTrades(ctx context.Context, symbol string, startTimeMs, endTimeMs, fromID int64, limit int) ([]BinanceUserTrade, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	q := url.Values{
		"symbol": {binanceSymbol(symbol)},
		"limit":  {strconv.Itoa(limit)},
	}
	if fromID > 0 {
		q.Set("fromId", strconv.FormatInt(fromID, 10))
	} else {
		if startTimeMs > 0 {
			q.Set("startTime", strconv.FormatInt(startTimeMs, 10))
		}
		if endTimeMs > 0 {
			q.Set("endTime", strconv.FormatInt(endTimeMs, 10))
		}
	}
	body, err := c.signedGet(ctx, "/fapi/v1/userTrades", q)
	if err != nil {
		return nil, err
	}
	var rows []BinanceUserTrade
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("decode userTrades: %w", err)
	}
	return rows, nil
}

// GetIncome calls GET /fapi/v1/income (symbol optional).
func (c *BinanceAccountClient) GetIncome(ctx context.Context, incomeType string, startTimeMs int64, limit int) ([]BinanceIncomeRow, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	q := url.Values{
		"limit": {strconv.Itoa(limit)},
	}
	if incomeType != "" {
		q.Set("incomeType", incomeType)
	}
	if startTimeMs > 0 {
		q.Set("startTime", strconv.FormatInt(startTimeMs, 10))
	}
	body, err := c.signedGet(ctx, "/fapi/v1/income", q)
	if err != nil {
		return nil, err
	}
	var rows []BinanceIncomeRow
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("decode income: %w", err)
	}
	return rows, nil
}

func (c *BinanceAccountClient) fetchBNBPriceUSD(ctx context.Context) float64 {
	c.bnbMu.Lock()
	if c.bnbPrice > 0 && time.Since(c.bnbAt) < 5*time.Minute {
		p := c.bnbPrice
		c.bnbMu.Unlock()
		return p
	}
	c.bnbMu.Unlock()

	u := strings.TrimRight(c.baseURL, "/") + "/fapi/v1/ticker/price?symbol=BNBUSDT"
	body, err := doHTTPGet(ctx, c.httpCli, u)
	if err != nil {
		return 0
	}
	var row struct {
		Price string `json:"price"`
	}
	if json.Unmarshal(body, &row) == nil {
		if p := parseFloat(row.Price); p > 0 {
			c.bnbMu.Lock()
			c.bnbPrice = p
			c.bnbAt = time.Now()
			c.bnbMu.Unlock()
			return p
		}
	}
	return 0
}

// SyncToCache pulls read-only account balances, open positions, income ledger, and user trades,
// aggregates FIFO lots keyed by (symbol, positionSide), matches accepted DecisionTickets,
// and updates desktop-cache.db strictly under exchange="binance".
func (c *BinanceAccountClient) SyncToCache(
	ctx context.Context,
	store *cache.Store,
	journal *storage.Journal,
	trackedSymbols []string,
) (*AccountSyncOutcome, error) {
	if c == nil || store == nil {
		return nil, fmt.Errorf("account client or cache store not initialized")
	}
	if !c.HasKeys() {
		return nil, fmt.Errorf("binance read-only api keys not configured")
	}

	const ex = "binance"

	// 1. Fetch account summary, position risks, incremental income events, and warm BNB price concurrently.
	latestIncomeMs, err := store.LatestIncomeTimeMsForExchange(ctx, ex)
	if err != nil {
		return nil, err
	}
	incomeStart := latestIncomeMs + 1
	if latestIncomeMs == 0 {
		incomeStart = time.Now().Add(-90 * 24 * time.Hour).UnixMilli()
	}

	var (
		acct       *BinanceAccountSummary
		acctErr    error
		risks      []BinancePositionRisk
		risksErr   error
		newIncomes []cache.IncomeRecord
		incomeErr  error
	)

	var initWG sync.WaitGroup
	initWG.Add(4)
	go func() {
		defer initWG.Done()
		acct, acctErr = c.GetAccount(ctx)
	}()
	go func() {
		defer initWG.Done()
		risks, risksErr = c.GetPositionRisk(ctx)
	}()
	go func() {
		defer initWG.Done()
		for page := 0; page < 5; page++ {
			if ctx.Err() != nil {
				incomeErr = ctx.Err()
				return
			}
			rows, err := c.GetIncome(ctx, "", incomeStart, 1000)
			if err != nil {
				incomeErr = err
				return
			}
			if len(rows) == 0 {
				break
			}
			var maxT int64
			for _, r := range rows {
				sym := toCanonicalBinance(r.Symbol)
				if sym == "" && r.Symbol != "" {
					sym = r.Symbol
				}
				tranID := fmt.Sprintf("%v_%s_%d", r.TranID, r.IncomeType, r.Time)
				newIncomes = append(newIncomes, cache.IncomeRecord{
					Exchange:   ex,
					TranID:     tranID,
					Symbol:     sym,
					IncomeType: r.IncomeType,
					Income:     parseFloat(r.Income),
					Asset:      r.Asset,
					TimeMs:     r.Time,
				})
				if r.Time > maxT {
					maxT = r.Time
				}
			}
			if len(rows) < 1000 || maxT <= incomeStart {
				break
			}
			incomeStart = maxT + 1
		}
	}()
	go func() {
		defer initWG.Done()
		_ = c.fetchBNBPriceUSD(ctx)
	}()
	initWG.Wait()

	for _, fetchErr := range []error{acctErr, risksErr, incomeErr} {
		if fetchErr != nil {
			_ = store.SetExchangeMeta(ctx, ex, "sync_error", fetchErr.Error())
			_ = store.SetAccountMeta(ctx, "sync_error", fetchErr.Error())
			return nil, fetchErr
		}
	}

	if err := store.UpsertIncomeRecordsForExchange(ctx, ex, newIncomes); err != nil {
		return nil, err
	}

	allIncomes, err := store.ListIncomeRecordsForExchange(ctx, ex, "")
	if err != nil {
		return nil, err
	}

	// 2. Discover candidate symbols for userTrades sync strictly from evidence-backed sources.
	_ = trackedSymbols
	symSet := make(map[string]struct{})
	tsBySym := make(map[string][]int64)
	openSymSet := make(map[string]bool)

	for _, inc := range allIncomes {
		if strings.Contains(inc.Symbol, "/USDT:USDT") {
			symSet[inc.Symbol] = struct{}{}
			if inc.TimeMs > 0 {
				tsBySym[inc.Symbol] = append(tsBySym[inc.Symbol], inc.TimeMs)
			}
		}
	}
	for _, pr := range risks {
		if math.Abs(parseFloat(pr.PositionAmt)) > 1e-12 {
			if cs := toCanonicalBinance(pr.Symbol); cs != "" {
				symSet[cs] = struct{}{}
				openSymSet[cs] = true
			}
		}
	}

	var tickets []types.DecisionTicket
	var decisions map[string]types.DecisionRecord
	if journal != nil {
		tickets, _ = journal.ListTickets("")
		decisions, _ = journal.ListDecisions()
		for _, t := range tickets {
			accepted := t.Status == types.TicketStatusAccepted ||
				t.Status == types.TicketStatusClosed ||
				(decisions != nil && decisions[t.ID].Decision == types.DecisionAccepted)
			if accepted && t.Symbol != "" {
				symSet[t.Symbol] = struct{}{}
				tsMs := t.SignalAt * 1000
				if t.CreatedAt > 0 {
					tsMs = t.CreatedAt * 1000
				}
				if tsMs > 0 {
					tsBySym[t.Symbol] = append(tsBySym[t.Symbol], tsMs)
				}
			}
		}
	}
	existingFills, err := store.ListTradeFillsForExchange(ctx, ex, "", 0)
	if err != nil {
		return nil, err
	}
	maxIDBySym := make(map[string]int64)
	for _, f := range existingFills {
		symSet[f.Symbol] = struct{}{}
		if id := parseTrailingBinanceTradeID(f.FillID); id > maxIDBySym[f.Symbol] {
			maxIDBySym[f.Symbol] = id
		}
	}

	syms := make([]string, 0, len(symSet))
	for s := range symSet {
		syms = append(syms, s)
	}
	sort.Strings(syms)

	// 3. Fetch incremental userTrades per symbol concurrently (6 workers).
	var fillsMu sync.Mutex
	var fetchedFills []cache.TradeFill
	needBNB := false
	nowMs := time.Now().UnixMilli()

	if len(syms) > 0 {
		jobs := make(chan string, len(syms))
		for _, sym := range syms {
			jobs <- sym
		}
		close(jobs)

		workers := 6
		if len(syms) < workers {
			workers = len(syms)
		}
		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for sym := range jobs {
					if ctx.Err() != nil {
						return
					}
					var localFills []cache.TradeFill
					localNeedBNB := false
					maxID := maxIDBySym[sym]
					if maxID > 0 {
						fromID := maxID + 1
						for page := 0; page < 5; page++ {
							if ctx.Err() != nil {
								return
							}
							rows, err := c.GetUserTrades(ctx, sym, 0, 0, fromID, 1000)
							if err != nil || len(rows) == 0 {
								break
							}
							var highest int64
							for _, r := range rows {
								fill := convertBinanceTrade(sym, r)
								localFills = append(localFills, fill)
								if strings.EqualFold(fill.CommissionAsset, "BNB") {
									localNeedBNB = true
								}
								if r.ID > highest {
									highest = r.ID
								}
							}
							if len(rows) < 1000 || highest < fromID {
								break
							}
							fromID = highest + 1
						}
					} else {
						windows := buildCovering7DayWindows(tsBySym[sym], openSymSet[sym], nowMs)
						for _, win := range windows {
							if ctx.Err() != nil {
								return
							}
							rows, err := c.GetUserTrades(ctx, sym, win.start, win.end, 0, 1000)
							if err != nil || len(rows) == 0 {
								continue
							}
							for _, r := range rows {
								fill := convertBinanceTrade(sym, r)
								localFills = append(localFills, fill)
								if strings.EqualFold(fill.CommissionAsset, "BNB") {
									localNeedBNB = true
								}
							}
						}
					}

					if len(localFills) > 0 || localNeedBNB {
						fillsMu.Lock()
						fetchedFills = append(fetchedFills, localFills...)
						if localNeedBNB {
							needBNB = true
						}
						fillsMu.Unlock()
					}
				}
			}()
		}
		wg.Wait()
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	allFills := mergeFillsForSync(ex, existingFills, fetchedFills)
	for _, f := range allFills {
		if strings.EqualFold(f.CommissionAsset, "BNB") {
			needBNB = true
			break
		}
	}

	var bnbPrice float64
	if needBNB {
		bnbPrice = c.fetchBNBPriceUSD(ctx)
	}

	// 4. Aggregate FIFO lots + match DecisionTickets.
	trades, linkedFills := AggregateBinanceTrades(allFills, allIncomes, risks, tickets, decisions, bnbPrice)

	nowSec := time.Now().Unix()
	openPositions := buildBinanceOpenPositions(risks, nowSec)

	if err := persistAccountSync(ctx, store, ex, trades, linkedFills, openPositions, acct.WalletBalanceUSD, acct.UnrealizedPnLUSD, nowSec); err != nil {
		return nil, err
	}

	return &AccountSyncOutcome{
		SyncedTrades:     len(trades),
		SyncedFills:      len(linkedFills),
		OpenPositions:    len(openPositions),
		WalletBalanceUSD: acct.WalletBalanceUSD,
		UnrealizedPnLUSD: acct.UnrealizedPnLUSD,
		LastSyncAt:       nowSec,
	}, nil
}

func convertBinanceTrade(canonicalSym string, r BinanceUserTrade) cache.TradeFill {
	posSide := strings.ToUpper(strings.TrimSpace(r.PositionSide))
	if posSide == "" {
		posSide = "BOTH"
	}
	return cache.TradeFill{
		Exchange:        "binance",
		FillID:          fmt.Sprintf("%s:%d", canonicalSym, r.ID),
		OrderID:         strconv.FormatInt(r.OrderID, 10),
		Symbol:          canonicalSym,
		Side:            strings.ToUpper(strings.TrimSpace(r.Side)),
		PositionSide:    posSide,
		Price:           parseFloat(r.Price),
		Qty:             parseFloat(r.Qty),
		QuoteQty:        parseFloat(r.QuoteQty),
		Commission:      math.Abs(parseFloat(r.Commission)),
		CommissionAsset: strings.ToUpper(strings.TrimSpace(r.CommissionAsset)),
		RealizedPnL:     parseFloat(r.RealizedPnL),
		TimeMs:          r.Time,
	}
}

func parseTrailingBinanceTradeID(fillID string) int64 {
	clean := strings.TrimSuffix(fillID, ":flip")
	idx := strings.LastIndexByte(clean, ':')
	if idx < 0 || idx+1 >= len(clean) {
		return 0
	}
	id, err := strconv.ParseInt(clean[idx+1:], 10, 64)
	if err != nil {
		return 0
	}
	return id
}

func buildBinanceOpenPositions(risks []BinancePositionRisk, updatedAt int64) []cache.OpenPosition {
	out := make([]cache.OpenPosition, 0)
	for _, r := range risks {
		amt := parseFloat(r.PositionAmt)
		if math.Abs(amt) <= 1e-12 {
			continue
		}
		sym := toCanonicalBinance(r.Symbol)
		if sym == "" {
			continue
		}
		posSide := strings.ToUpper(strings.TrimSpace(r.PositionSide))
		if posSide == "" {
			posSide = "BOTH"
		}
		marginMode := strings.ToLower(strings.TrimSpace(r.MarginType))
		if marginMode == "" {
			marginMode = "cross"
		}
		lev := parseFloat(r.Leverage)
		if lev <= 0 {
			lev = 1
		}
		out = append(out, cache.OpenPosition{
			Exchange:       "binance",
			Symbol:         sym,
			PositionSide:   posSide,
			MarginMode:     marginMode,
			Leverage:       lev,
			Qty:            amt, // signed in BOTH (>0 long, <0 short), non-zero
			EntryPrice:     parseFloat(r.EntryPrice),
			MarkPrice:      parseFloat(r.MarkPrice),
			UnrealizedPnL:  parseFloat(r.UnRealizedProfit),
			LiqPrice:       parseFloat(r.LiquidationPrice),
			IsolatedWallet: parseFloat(r.IsolatedWallet),
			UpdatedAt:      updatedAt,
		})
	}
	return out
}

func binanceRisksToPositionMeta(posRisks []BinancePositionRisk) []PositionMeta {
	out := make([]PositionMeta, 0, len(posRisks))
	for _, r := range posRisks {
		sym := toCanonicalBinance(r.Symbol)
		if sym == "" {
			continue
		}
		ps := strings.ToUpper(strings.TrimSpace(r.PositionSide))
		if ps == "" {
			ps = "BOTH"
		}
		lev := parseFloat(r.Leverage)
		if lev <= 0 {
			lev = 1
		}
		mm := strings.ToLower(strings.TrimSpace(r.MarginType))
		if mm == "" {
			mm = "cross"
		}
		out = append(out, PositionMeta{
			Symbol:         sym,
			PositionSide:   ps,
			MarginMode:     mm,
			Leverage:       lev,
			LiqPrice:       parseFloat(r.LiquidationPrice),
			IsolatedWallet: parseFloat(r.IsolatedWallet),
			IsOpen:         parseFloat(r.PositionAmt) != 0,
		})
	}
	return out
}

// AggregateBinanceTrades performs deterministic FIFO lot aggregation keyed by
// (symbol, positionSide), preserves open positions, attaches leverage/margin/liq_price
// and real commission+funding costs, links TradeFill.TradeID, and matches trades
// to accepted DecisionTickets (unmatched flagged GATE_NO_VALID_TICKET).
func AggregateBinanceTrades(
	rawFills []cache.TradeFill,
	incomes []cache.IncomeRecord,
	posRisks []BinancePositionRisk,
	tickets []types.DecisionTicket,
	decisions map[string]types.DecisionRecord,
	bnbPriceUSD float64,
) ([]cache.SyncedTrade, []cache.TradeFill) {
	return AggregateTrades(AggregateTradesInput{
		Exchange:          "binance",
		Fills:             rawFills,
		Incomes:           incomes,
		PositionMeta:      binanceRisksToPositionMeta(posRisks),
		DefaultMarginMode: "cross",
		Tickets:           tickets,
		Decisions:         decisions,
		BNBPriceUSD:       bnbPriceUSD,
	})
}
