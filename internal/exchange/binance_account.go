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
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ArchdevilForge/kairos/internal/cache"
	"github.com/ArchdevilForge/kairos/internal/decision"
	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
)

const (
	envBinanceReadonlyKey    = "BINANCE_READONLY_API_KEY"
	envBinanceReadonlySecret = "BINANCE_READONLY_API_SECRET"
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

// AccountSyncOutcome summarizes a completed read-only sync into desktop-cache.db.
type AccountSyncOutcome struct {
	SyncedTrades     int
	SyncedFills      int
	OpenPositions    int
	WalletBalanceUSD float64
	UnrealizedPnLUSD float64
	LastSyncAt       int64
}

// SecretsFilePath returns ~/.config/kairos/secrets.env.
func SecretsFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "", fmt.Errorf("resolve user home dir: %w", err)
	}
	return filepath.Join(home, ".config", "kairos", "secrets.env"), nil
}

func cleanCredential(v string) string {
	return strings.Trim(strings.TrimSpace(v), "\"'")
}

// LoadBinanceReadonlyKeys loads BINANCE_READONLY_API_KEY and BINANCE_READONLY_API_SECRET
// from environment variables first, falling back to ~/.config/kairos/secrets.env.
func LoadBinanceReadonlyKeys() (apiKey, apiSecret string) {
	apiKey = cleanCredential(os.Getenv(envBinanceReadonlyKey))
	apiSecret = cleanCredential(os.Getenv(envBinanceReadonlySecret))
	if apiKey != "" && apiSecret != "" {
		return apiKey, apiSecret
	}
	p, err := SecretsFilePath()
	if err != nil {
		return apiKey, apiSecret
	}
	kv := parseEnvFile(p)
	if apiKey == "" {
		apiKey = cleanCredential(kv[envBinanceReadonlyKey])
	}
	if apiSecret == "" {
		apiSecret = cleanCredential(kv[envBinanceReadonlySecret])
	}
	return apiKey, apiSecret
}

// SaveBinanceReadonlyKeys stores read-only Binance API credentials in ~/.config/kairos/secrets.env
// with 0600 permissions, preserving any other key/value entries in that file.
func SaveBinanceReadonlyKeys(apiKey, apiSecret string) error {
	p, err := SecretsFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return fmt.Errorf("secrets mkdir: %w", err)
	}
	existing := parseEnvFile(p)
	if k := cleanCredential(apiKey); k != "" {
		existing[envBinanceReadonlyKey] = k
	}
	if s := cleanCredential(apiSecret); s != "" {
		existing[envBinanceReadonlySecret] = s
	}

	keys := make([]string, 0, len(existing))
	for k := range existing {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString("# Kairos read-only secrets (0600, never commit)\n")
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(existing[k])
		b.WriteByte('\n')
	}

	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		return fmt.Errorf("write secrets.env: %w", err)
	}
	_ = os.Chmod(p, 0o600)
	return nil
}

// MaskAPIKey returns a redacted form suitable for UI display and logs.
func MaskAPIKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}

func parseEnvFile(path string) map[string]string {
	out := make(map[string]string)
	raw, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		idx := strings.IndexByte(line, '=')
		if idx <= 0 {
			continue
		}
		k := strings.TrimSpace(line[:idx])
		v := strings.TrimSpace(line[idx+1:])
		if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
			v = v[1 : len(v)-1]
		}
		if k != "" {
			out[k] = v
		}
	}
	return out
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
// and updates desktop-cache.db.
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

	// 1. Fetch account summary, position risks, incremental income events, and warm BNB price concurrently.
	latestIncomeMs, err := store.LatestIncomeTimeMs(ctx)
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
			_ = store.SetAccountMeta(ctx, "sync_error", fetchErr.Error())
			return nil, fetchErr
		}
	}

	if err := store.UpsertIncomeRecords(ctx, newIncomes); err != nil {
		return nil, err
	}

	allIncomes, err := store.ListIncomeRecords(ctx, "")
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
	existingFills, err := store.ListTradeFills(ctx, "", 0)
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

	// Merge existing + newly fetched fills deduplicating by base FillID.
	mergedFillMap := make(map[string]cache.TradeFill, len(existingFills)+len(fetchedFills))
	for _, f := range existingFills {
		// Re-collapse prior flip splits back to canonical base FillID before re-aggregating.
		if strings.HasSuffix(f.FillID, ":flip") {
			baseID := strings.TrimSuffix(f.FillID, ":flip")
			if base, ok := mergedFillMap[baseID]; ok {
				base.Qty += f.Qty
				base.QuoteQty += f.QuoteQty
				base.Commission += f.Commission
				base.RealizedPnL += f.RealizedPnL
				base.TradeID = ""
				mergedFillMap[baseID] = base
			}
			continue
		}
		f.TradeID = ""
		mergedFillMap[f.FillID] = f
	}
	for _, f := range fetchedFills {
		if strings.EqualFold(f.CommissionAsset, "BNB") {
			needBNB = true
		}
		mergedFillMap[f.FillID] = f
	}
	allFills := make([]cache.TradeFill, 0, len(mergedFillMap))
	for _, f := range mergedFillMap {
		if strings.EqualFold(f.CommissionAsset, "BNB") {
			needBNB = true
		}
		allFills = append(allFills, f)
	}

	var bnbPrice float64
	if needBNB {
		bnbPrice = c.fetchBNBPriceUSD(ctx)
	}

	// 4. Aggregate FIFO lots + match DecisionTickets.
	trades, linkedFills := AggregateBinanceTrades(allFills, allIncomes, risks, tickets, decisions, bnbPrice)

	nowSec := time.Now().Unix()
	openPositions := buildOpenPositions(risks, nowSec)

	if err := store.ReplaceAllSyncedTrades(ctx, trades, linkedFills); err != nil {
		return nil, err
	}
	if err := store.ReplaceOpenPositions(ctx, openPositions); err != nil {
		return nil, err
	}
	_ = store.SetAccountMeta(ctx, "wallet_balance_usd", strconv.FormatFloat(acct.WalletBalanceUSD, 'f', 6, 64))
	_ = store.SetAccountMeta(ctx, "unrealized_pnl_usd", strconv.FormatFloat(acct.UnrealizedPnLUSD, 'f', 6, 64))
	_ = store.SetAccountMeta(ctx, "last_sync_at", strconv.FormatInt(nowSec, 10))
	_ = store.SetAccountMeta(ctx, "sync_error", "")

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

func buildOpenPositions(risks []BinancePositionRisk, updatedAt int64) []cache.OpenPosition {
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

type fifoLot struct {
	qty   float64
	price float64
}

type activeTradeAcc struct {
	id                 string
	symbol             string
	direction          string
	positionSide       string
	entryTimeMs        int64
	exitTimeMs         int64
	openQty            float64
	totalOpenedQty     float64
	totalClosedQty     float64
	totalEntryNotional float64
	totalExitNotional  float64
	realizedPnL        float64
	feeUSD             float64
	lots               []fifoLot
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
	if len(rawFills) == 0 {
		return nil, nil
	}

	fills := make([]cache.TradeFill, len(rawFills))
	copy(fills, rawFills)
	sort.Slice(fills, func(i, j int) bool {
		if fills[i].TimeMs != fills[j].TimeMs {
			return fills[i].TimeMs < fills[j].TimeMs
		}
		return fills[i].FillID < fills[j].FillID
	})

	type posMeta struct {
		marginMode     string
		leverage       float64
		liqPrice       float64
		isolatedWallet float64
	}
	metaByKey := make(map[string]posMeta)
	metaBySym := make(map[string]posMeta)
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
		pm := posMeta{
			marginMode:     mm,
			leverage:       lev,
			liqPrice:       parseFloat(r.LiquidationPrice),
			isolatedWallet: parseFloat(r.IsolatedWallet),
		}
		metaByKey[sym+"|"+ps] = pm
		if _, exists := metaBySym[sym]; !exists || parseFloat(r.PositionAmt) != 0 {
			metaBySym[sym] = pm
		}
	}

	type streamKey struct {
		symbol  string
		posSide string
	}
	grouped := make(map[streamKey][]cache.TradeFill)
	var keys []streamKey
	for _, f := range fills {
		ps := strings.ToUpper(strings.TrimSpace(f.PositionSide))
		if ps == "" {
			ps = "BOTH"
		}
		f.PositionSide = ps
		f.Side = strings.ToUpper(strings.TrimSpace(f.Side))
		k := streamKey{symbol: f.Symbol, posSide: ps}
		if _, ok := grouped[k]; !ok {
			keys = append(keys, k)
		}
		grouped[k] = append(grouped[k], f)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].symbol != keys[j].symbol {
			return keys[i].symbol < keys[j].symbol
		}
		return keys[i].posSide < keys[j].posSide
	})

	var trades []cache.SyncedTrade
	linkedFills := make([]cache.TradeFill, 0, len(fills))

	finalizeTrade := func(acc *activeTradeAcc, open bool) cache.SyncedTrade {
		meta := metaByKey[acc.symbol+"|"+acc.positionSide]
		if meta.leverage <= 0 {
			meta = metaBySym[acc.symbol]
		}
		if meta.leverage <= 0 {
			meta.leverage = 1
		}
		if meta.marginMode == "" {
			meta.marginMode = "cross"
		}

		entryPx := 0.0
		qty := 0.0
		exitPx := 0.0
		status := "closed"
		exitMs := acc.exitTimeMs

		if open {
			status = "open"
			exitMs = 0
			var remNotional, remQty float64
			for _, lot := range acc.lots {
				remQty += lot.qty
				remNotional += lot.qty * lot.price
			}
			qty = remQty
			if remQty > 0 {
				entryPx = remNotional / remQty
			}
		} else {
			qty = acc.totalOpenedQty
			if acc.totalOpenedQty > 0 {
				entryPx = acc.totalEntryNotional / acc.totalOpenedQty
			}
			if acc.totalClosedQty > 0 {
				exitPx = acc.totalExitNotional / acc.totalClosedQty
			}
		}

		margin := 0.0
		if entryPx > 0 && qty > 0 && meta.leverage > 0 {
			margin = (entryPx * qty) / meta.leverage
		}
		if open && meta.marginMode == decision.MarginModeIsolated && meta.isolatedWallet > 0 {
			margin = meta.isolatedWallet
		}
		liqPx := 0.0
		if open {
			liqPx = meta.liqPrice
		}

		return cache.SyncedTrade{
			ID:           acc.id,
			Symbol:       acc.symbol,
			Direction:    acc.direction,
			PositionSide: acc.positionSide,
			MarginMode:   meta.marginMode,
			Leverage:     meta.leverage,
			EntryPrice:   entryPx,
			ExitPrice:    exitPx,
			Qty:          qty,
			EntryTimeMs:  acc.entryTimeMs,
			ExitTimeMs:   exitMs,
			RealizedPnL:  acc.realizedPnL,
			FeeUSD:       acc.feeUSD,
			Margin:       margin,
			LiqPrice:     liqPx,
			Status:       status,
		}
	}

	newAcc := func(f cache.TradeFill, dir string) *activeTradeAcc {
		return &activeTradeAcc{
			id:           fmt.Sprintf("trd-%s-%s-%d-%d", strings.ToLower(binanceSymbol(f.Symbol)), strings.ToLower(f.PositionSide), f.TimeMs, parseTrailingBinanceTradeID(f.FillID)),
			symbol:       f.Symbol,
			direction:    dir,
			positionSide: f.PositionSide,
			entryTimeMs:  f.TimeMs,
		}
	}

	for _, k := range keys {
		stream := grouped[k]
		var acc *activeTradeAcc

		for _, f := range stream {
			if f.Qty <= 0 {
				continue
			}
			feeUSD := fillFeeUSD(f, bnbPriceUSD)
			fillDir := fillSideDirection(f.PositionSide, f.Side)

			// Check if fill opens/scales in or closes/reduces existing open lots.
			if acc == nil {
				dir := fillDir
				if dir == "" {
					if f.Side == "BUY" {
						dir = "long"
					} else {
						dir = "short"
					}
				}
				acc = newAcc(f, dir)
				acc.lots = append(acc.lots, fifoLot{qty: f.Qty, price: f.Price})
				acc.openQty += f.Qty
				acc.totalOpenedQty += f.Qty
				acc.totalEntryNotional += f.Qty * f.Price
				acc.feeUSD += feeUSD

				fc := f
				fc.TradeID = acc.id
				linkedFills = append(linkedFills, fc)
				continue
			}

			isOpening := false
			switch f.PositionSide {
			case "LONG":
				isOpening = f.Side == "BUY"
			case "SHORT":
				isOpening = f.Side == "SELL"
			default: // BOTH
				isOpening = (acc.direction == "long" && f.Side == "BUY") || (acc.direction == "short" && f.Side == "SELL")
			}

			if isOpening {
				acc.lots = append(acc.lots, fifoLot{qty: f.Qty, price: f.Price})
				acc.openQty += f.Qty
				acc.totalOpenedQty += f.Qty
				acc.totalEntryNotional += f.Qty * f.Price
				acc.feeUSD += feeUSD

				fc := f
				fc.TradeID = acc.id
				linkedFills = append(linkedFills, fc)
				continue
			}

			// Reducing / closing / flipping.
			closeQty := math.Min(f.Qty, acc.openQty)
			remOpenQty := f.Qty - closeQty
			closeRatio := 1.0
			if f.Qty > 0 {
				closeRatio = closeQty / f.Qty
			}
			closeFee := feeUSD * closeRatio
			openFee := feeUSD - closeFee

			consumeFIFOLots(&acc.lots, closeQty)
			acc.openQty -= closeQty
			acc.totalClosedQty += closeQty
			acc.totalExitNotional += closeQty * f.Price
			acc.exitTimeMs = f.TimeMs
			acc.realizedPnL += f.RealizedPnL
			acc.feeUSD += closeFee

			if remOpenQty <= 1e-10 {
				fc := f
				fc.TradeID = acc.id
				linkedFills = append(linkedFills, fc)
				if acc.openQty <= 1e-10 {
					trades = append(trades, finalizeTrade(acc, false))
					acc = nil
				}
			} else {
				// Position flip in BOTH mode: split fill across closing trade and newly opened opposite trade.
				fcClose := f
				fcClose.Qty = closeQty
				fcClose.QuoteQty = closeQty * f.Price
				fcClose.Commission = closeFee
				fcClose.TradeID = acc.id
				linkedFills = append(linkedFills, fcClose)

				trades = append(trades, finalizeTrade(acc, false))

				newDir := "short"
				if f.Side == "BUY" {
					newDir = "long"
				}
				acc = newAcc(f, newDir)
				acc.lots = append(acc.lots, fifoLot{qty: remOpenQty, price: f.Price})
				acc.openQty = remOpenQty
				acc.totalOpenedQty = remOpenQty
				acc.totalEntryNotional = remOpenQty * f.Price
				acc.feeUSD = openFee

				fcOpen := f
				fcOpen.FillID = f.FillID + ":flip"
				fcOpen.Qty = remOpenQty
				fcOpen.QuoteQty = remOpenQty * f.Price
				fcOpen.Commission = openFee
				fcOpen.RealizedPnL = 0
				fcOpen.TradeID = acc.id
				linkedFills = append(linkedFills, fcOpen)
			}
		}

		if acc != nil && acc.openQty > 1e-10 {
			trades = append(trades, finalizeTrade(acc, true))
		}
	}

	// Allocate FUNDING_FEE income to trades active at each funding settlement timestamp.
	allocateFundingFees(trades, incomes)

	// Compute NetPnL = RealizedPnL - FeeUSD + FundingUSD (FundingUSD is signed cashflow: negative = paid, positive = received).
	for i := range trades {
		trades[i].NetPnL = trades[i].RealizedPnL - trades[i].FeeUSD + trades[i].FundingUSD
	}

	// Match synced trades against accepted DecisionTickets.
	matchTradesToTickets(trades, tickets, decisions)

	// Sort newest-first by entry_time_ms.
	sort.Slice(trades, func(i, j int) bool {
		if trades[i].EntryTimeMs != trades[j].EntryTimeMs {
			return trades[i].EntryTimeMs > trades[j].EntryTimeMs
		}
		return trades[i].ID > trades[j].ID
	})

	return trades, linkedFills
}

func fillSideDirection(posSide, side string) string {
	switch posSide {
	case "LONG":
		return "long"
	case "SHORT":
		return "short"
	default:
		if side == "BUY" {
			return "long"
		}
		if side == "SELL" {
			return "short"
		}
		return ""
	}
}

func consumeFIFOLots(lots *[]fifoLot, qty float64) {
	rem := qty
	arr := *lots
	idx := 0
	for idx < len(arr) && rem > 1e-12 {
		if arr[idx].qty <= rem+1e-12 {
			rem -= arr[idx].qty
			idx++
		} else {
			arr[idx].qty -= rem
			rem = 0
		}
	}
	*lots = arr[idx:]
}

func fillFeeUSD(f cache.TradeFill, bnbPriceUSD float64) float64 {
	comm := math.Abs(f.Commission)
	if comm == 0 {
		return 0
	}
	switch strings.ToUpper(f.CommissionAsset) {
	case "", "USDT", "USDC", "BUSD", "FDUSD":
		return comm
	case "BNB":
		if bnbPriceUSD > 0 {
			return comm * bnbPriceUSD
		}
		return comm
	default:
		return comm
	}
}

func allocateFundingFees(trades []cache.SyncedTrade, incomes []cache.IncomeRecord) {
	if len(trades) == 0 || len(incomes) == 0 {
		return
	}
	for _, inc := range incomes {
		if inc.IncomeType != "FUNDING_FEE" || inc.Symbol == "" {
			continue
		}
		// Binance income is signed cashflow: negative = paid, positive = received.
		cashflowUSD := inc.Income
		var matches []int
		var totalNotional float64
		for i := range trades {
			tr := &trades[i]
			if tr.Symbol != inc.Symbol {
				continue
			}
			if inc.TimeMs < tr.EntryTimeMs {
				continue
			}
			if tr.Status != "open" && tr.ExitTimeMs > 0 && inc.TimeMs > tr.ExitTimeMs {
				continue
			}
			matches = append(matches, i)
			totalNotional += tr.EntryPrice * tr.Qty
		}
		if len(matches) == 0 {
			continue
		}
		if len(matches) == 1 || totalNotional <= 0 {
			share := cashflowUSD / float64(len(matches))
			for _, idx := range matches {
				trades[idx].FundingUSD += share
			}
			continue
		}
		for _, idx := range matches {
			w := (trades[idx].EntryPrice * trades[idx].Qty) / totalNotional
			trades[idx].FundingUSD += cashflowUSD * w
		}
	}
}

func matchTradesToTickets(trades []cache.SyncedTrade, tickets []types.DecisionTicket, decisions map[string]types.DecisionRecord) {
	type candTicket struct {
		ticket    types.DecisionTicket
		dec       *types.DecisionRecord
		anchorSec int64
	}
	var cands []candTicket
	for _, t := range tickets {
		var decPtr *types.DecisionRecord
		if decisions != nil {
			if d, ok := decisions[t.ID]; ok {
				dCopy := d
				decPtr = &dCopy
			}
		}
		accepted := t.Status == types.TicketStatusAccepted ||
			t.Status == types.TicketStatusClosed ||
			(decPtr != nil && decPtr.Decision == types.DecisionAccepted)
		if !accepted {
			continue
		}
		anchor := t.EntryTriggeredAt
		if decPtr != nil && decPtr.DecidedAt > 0 {
			anchor = decPtr.DecidedAt
		}
		if anchor <= 0 {
			anchor = t.CreatedAt
		}
		if anchor <= 0 {
			anchor = t.SignalAt
		}
		cands = append(cands, candTicket{ticket: t, dec: decPtr, anchorSec: anchor})
	}

	usedTickets := make(map[string]bool)
	const preWindowSec int64 = 15 * 60
	const postWindowSec int64 = 4 * 3600

	for i := range trades {
		tr := &trades[i]
		entrySec := tr.EntryTimeMs / 1000
		wantCycleDir := types.CycleDirectionUp
		if tr.Direction == "short" {
			wantCycleDir = types.CycleDirectionDown
		}

		bestIdx := -1
		var bestDist int64 = math.MaxInt64
		for ci, c := range cands {
			if usedTickets[c.ticket.ID] {
				continue
			}
			if c.ticket.Symbol != tr.Symbol || c.ticket.Direction != wantCycleDir {
				continue
			}
			if c.anchorSec > 0 && entrySec > 0 {
				if entrySec < c.anchorSec-preWindowSec || entrySec > c.anchorSec+postWindowSec {
					continue
				}
				dist := entrySec - c.anchorSec
				if dist < 0 {
					dist = -dist
				}
				if dist < bestDist {
					bestDist = dist
					bestIdx = ci
				}
			}
		}

		if bestIdx < 0 {
			tr.GateCode = decision.GateNoValidTicket
			continue
		}

		matched := cands[bestIdx]
		usedTickets[matched.ticket.ID] = true
		tr.LinkedTicketID = matched.ticket.ID

		if tr.MarginMode != "" && tr.MarginMode != decision.MarginModeIsolated {
			tr.GateCode = decision.GateCrossMargin
			continue
		}
		if tr.Status == "closed" && tr.EntryTimeMs > 0 && tr.ExitTimeMs > tr.EntryTimeMs {
			var er types.ExitReason
			if matched.ticket.ExitReason != "" {
				er = matched.ticket.ExitReason
			} else if matched.dec != nil {
				er = matched.dec.ExitReason
			}
			r1USD := 0.0
			if matched.dec != nil && matched.dec.PlannedMaxLossUSD != nil && *matched.dec.PlannedMaxLossUSD > 0 {
				r1USD = *matched.dec.PlannedMaxLossUSD
			} else if matched.ticket.RiskPlan.EntryPrice > 0 && matched.ticket.RiskPlan.StopPrice > 0 && tr.Qty > 0 {
				r1USD = math.Abs(matched.ticket.RiskPlan.EntryPrice-matched.ticket.RiskPlan.StopPrice) * tr.Qty
			}
			realizedR := 0.0
			if r1USD > 0 {
				realizedR = tr.NetPnL / r1USD
			}
			res := decision.AuditExit(decision.ExitAuditInput{
				TicketID:        matched.ticket.ID,
				HasValidTicket:  true,
				Symbol:          tr.Symbol,
				Direction:       wantCycleDir,
				OpenedAt:        time.UnixMilli(tr.EntryTimeMs),
				ClosedAt:        time.UnixMilli(tr.ExitTimeMs),
				ExitPrice:       tr.ExitPrice,
				PresetStopPrice: matched.ticket.RiskPlan.StopPrice,
				RealizedR:       realizedR,
				ExitReason:      er,
			})
			tr.GateCode = res.Code
		}
	}
}

type timeWindow struct {
	start int64
	end   int64
}

func buildCovering7DayWindows(timestamps []int64, isOpen bool, nowMs int64) []timeWindow {
	const sevenDaysMs = int64(7 * 24 * 3600 * 1000)

	sorted := make([]int64, 0, len(timestamps))
	for _, ts := range timestamps {
		if ts > 0 {
			sorted = append(sorted, ts)
		}
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	var windows []timeWindow
	coveredUntil := int64(-1)
	for _, ts := range sorted {
		if coveredUntil >= 0 && ts <= coveredUntil-60_000 {
			continue
		}
		winStart := ts - 3600_000
		if winStart < 0 {
			winStart = 0
		}
		winEnd := winStart + sevenDaysMs - 1000
		if winEnd > nowMs {
			winEnd = nowMs
		}
		windows = append(windows, timeWindow{start: winStart, end: winEnd})
		coveredUntil = winEnd
	}

	if isOpen || len(windows) == 0 {
		recentStart := nowMs - sevenDaysMs + 1
		if recentStart < 0 {
			recentStart = 0
		}
		if len(windows) == 0 || windows[len(windows)-1].end < nowMs {
			windows = append(windows, timeWindow{start: recentStart, end: nowMs})
		}
	}

	if len(windows) > 8 {
		windows = windows[len(windows)-8:]
	}
	return windows
}
