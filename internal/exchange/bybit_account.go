package exchange

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	"github.com/ArchdevilForge/kairos/internal/decision"
	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
)

const (
	bybitRecvWindow   = "10000"
	bybitMaxWindowMs  = int64(7*24*3600*1000) - 1000
	bybitLookbackDays = 90
	bybitMaxPages     = 100
)

// flexInt decodes JSON integers or stringified integers (Bybit V5 occasionally
// varies numeric vs string encodings across accounts/fixtures).
type flexInt int

func (fi *flexInt) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" || trimmed == `""` {
		*fi = 0
		return nil
	}
	if strings.HasPrefix(trimmed, `"`) && strings.HasSuffix(trimmed, `"`) {
		trimmed = strings.Trim(trimmed, `"`)
	}
	v, err := strconv.Atoi(trimmed)
	if err != nil {
		return err
	}
	*fi = flexInt(v)
	return nil
}

// BybitAccountInfo holds non-secret UTA/Classic account capabilities from GET /v5/account/info.
type BybitAccountInfo struct {
	UnifiedMarginStatus int    `json:"unifiedMarginStatus"`
	MarginMode          string `json:"marginMode"`
	UpdateTime          string `json:"updateTime"`
}

// IsUTA reports whether the account runs Unified Trading Account (UTA 1.0/2.0).
func (info *BybitAccountInfo) IsUTA() bool {
	if info == nil {
		return true
	}
	if info.UnifiedMarginStatus == 1 {
		return false
	}
	return info.UnifiedMarginStatus > 1 || info.MarginMode != ""
}

// ResolvedMarginMode maps UTA marginMode ("ISOLATED_MARGIN" -> "isolated",
// "REGULAR_MARGIN"/"PORTFOLIO_MARGIN" -> "cross"). Empty on Classic accounts.
func (info *BybitAccountInfo) ResolvedMarginMode() string {
	if info == nil || !info.IsUTA() {
		return ""
	}
	switch strings.ToUpper(strings.TrimSpace(info.MarginMode)) {
	case "ISOLATED_MARGIN":
		return decision.MarginModeIsolated
	case "REGULAR_MARGIN", "PORTFOLIO_MARGIN":
		return "cross"
	default:
		return "cross"
	}
}

// BybitAccountSummary holds the non-secret USD summary from GET /v5/account/wallet-balance.
type BybitAccountSummary struct {
	AccountType      string
	WalletBalanceUSD float64
	UnrealizedPnLUSD float64
	MarginBalanceUSD float64
	AvailableBalance float64
	TotalEquity      float64
	InitialMarginUSD float64
}

// BybitPositionRow mirrors a linear position item from GET /v5/position/list.
type BybitPositionRow struct {
	Symbol          string `json:"symbol"`
	Side            string `json:"side"`
	Size            string `json:"size"`
	AvgPrice        string `json:"avgPrice"`
	MarkPrice       string `json:"markPrice"`
	UnrealisedPnl   string `json:"unrealisedPnl"`
	LiqPrice        string `json:"liqPrice"`
	Leverage        string `json:"leverage"`
	TradeMode       int    `json:"tradeMode"`
	PositionIM      string `json:"positionIM"`
	PositionBalance string `json:"positionBalance"`
	PositionIdx     int    `json:"positionIdx"`
	UpdatedTime     string `json:"updatedTime"`
}

// BybitExecutionRow mirrors an execution item from GET /v5/execution/list.
type BybitExecutionRow struct {
	ExecID      string `json:"execId"`
	OrderID     string `json:"orderId"`
	Symbol      string `json:"symbol"`
	Side        string `json:"side"`
	ExecPrice   string `json:"execPrice"`
	ExecQty     string `json:"execQty"`
	ExecValue   string `json:"execValue"`
	ExecFee     string `json:"execFee"`
	FeeCurrency string `json:"feeCurrency"`
	ExecPnl     string `json:"execPnl"`
	ClosedSize  string `json:"closedSize"`
	ExecType    string `json:"execType"`
	IsMaker     bool   `json:"isMaker"`
	ExecTime    string `json:"execTime"`
	Seq         int64  `json:"seq"`
}

// BybitClosedPnLRow mirrors a closed PnL item from GET /v5/position/closed-pnl.
type BybitClosedPnLRow struct {
	Symbol        string `json:"symbol"`
	OrderID       string `json:"orderId"`
	Side          string `json:"side"`
	Qty           string `json:"qty"`
	AvgEntryPrice string `json:"avgEntryPrice"`
	AvgExitPrice  string `json:"avgExitPrice"`
	ClosedSize    string `json:"closedSize"`
	CumEntryValue string `json:"cumEntryValue"`
	CumExitValue  string `json:"cumExitValue"`
	ClosedPnl     string `json:"closedPnl"`
	OpenFee       string `json:"openFee"`
	CloseFee      string `json:"closeFee"`
	Leverage      string `json:"leverage"`
	CreatedTime   string `json:"createdTime"`
	UpdatedTime   string `json:"updatedTime"`
}

// BybitTransactionLogRow mirrors an item from GET /v5/account/transaction-log.
type BybitTransactionLogRow struct {
	ID              string `json:"id"`
	Symbol          string `json:"symbol"`
	Side            string `json:"side"`
	Type            string `json:"type"`
	Funding         string `json:"funding"`
	Fee             string `json:"fee"`
	CashFlow        string `json:"cashFlow"`
	Change          string `json:"change"`
	Currency        string `json:"currency"`
	TransactionTime string `json:"transactionTime"`
}

type bybitAPIError struct {
	Code       int
	Msg        string
	Path       string
	HTTPStatus int
	LimitReset string
}

func (e *bybitAPIError) Error() string {
	switch e.Code {
	case 10003:
		return fmt.Sprintf("bybit %s: invalid API key (retCode=10003): verify BYBIT_READONLY_API_KEY matches mainnet api.bybit.com (%s)", e.Path, e.Msg)
	case 10004:
		return fmt.Sprintf("bybit %s: signature verification failed (retCode=10004): verify BYBIT_READONLY_API_SECRET and system clock (%s)", e.Path, e.Msg)
	case 10005:
		return fmt.Sprintf("bybit %s: permission denied (retCode=10005): enable Read-Only Unified Trading & Wallet permissions on API key (%s)", e.Path, e.Msg)
	case 10006:
		if e.LimitReset != "" {
			return fmt.Sprintf("bybit %s: rate limit exceeded (retCode=10006, reset=%s): %s", e.Path, e.LimitReset, e.Msg)
		}
		return fmt.Sprintf("bybit %s: rate limit exceeded (retCode=10006): %s", e.Path, e.Msg)
	case 10001:
		return fmt.Sprintf("bybit %s: invalid parameter (retCode=10001): %s", e.Path, e.Msg)
	default:
		if e.Code != 0 {
			return fmt.Sprintf("bybit %s: retCode=%d retMsg=%s", e.Path, e.Code, e.Msg)
		}
		return fmt.Sprintf("bybit %s: http %d: %s", e.Path, e.HTTPStatus, e.Msg)
	}
}

func isBybitErrCode(err error, code int) bool {
	var apiErr *bybitAPIError
	return errors.As(err, &apiErr) && apiErr.Code == code
}

// BybitAccountClient is a strictly read-only Bybit V5 linear account client.
// It exposes ONLY HTTP GET endpoints and never serializes or logs credentials.
type BybitAccountClient struct {
	mu           sync.RWMutex
	apiKey       string `json:"-"`
	apiSecret    string `json:"-"`
	baseURL      string
	httpCli      *http.Client
	timeOffsetMs int64
}

// NewBybitAccountClient constructs a strictly read-only Bybit V5 client loading
// credentials from env or ~/.config/kairos/secrets.env.
func NewBybitAccountClient() *BybitAccountClient {
	k, s := LoadBybitReadonlyKeys()
	return &BybitAccountClient{
		apiKey:    k,
		apiSecret: s,
		baseURL:   bybitRESTEndpoint,
		httpCli:   newPooledHTTPClient(15 * time.Second),
	}
}

// Exchange identifies this client as "bybit".
func (c *BybitAccountClient) Exchange() string { return "bybit" }

// HasKeys reports whether both API key and secret are configured.
func (c *BybitAccountClient) HasKeys() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.apiKey != "" && c.apiSecret != ""
}

// APIKeyMasked returns the masked API key for UI display.
func (c *BybitAccountClient) APIKeyMasked() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return MaskAPIKey(c.apiKey)
}

// SetKeys updates credentials in memory (never logged or serialized).
func (c *BybitAccountClient) SetKeys(apiKey, apiSecret string) {
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
func (c *BybitAccountClient) ReloadKeys() {
	if c == nil {
		return
	}
	k, s := LoadBybitReadonlyKeys()
	c.mu.Lock()
	c.apiKey = k
	c.apiSecret = s
	c.mu.Unlock()
}

func (c *BybitAccountClient) credentials() (string, string, int64) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.apiKey, c.apiSecret, c.timeOffsetMs
}

func (c *BybitAccountClient) syncServerTime(ctx context.Context) {
	u := strings.TrimRight(c.baseURL, "/") + "/v5/market/time"
	body, err := doHTTPGet(ctx, c.httpCli, u)
	if err != nil {
		return
	}
	var resp struct {
		RetCode int   `json:"retCode"`
		Time    int64 `json:"time"`
		Result  struct {
			TimeSecond string `json:"timeSecond"`
			TimeNano   string `json:"timeNano"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &resp) != nil {
		return
	}
	serverMs := resp.Time
	if serverMs <= 0 && resp.Result.TimeSecond != "" {
		if sec, err := strconv.ParseInt(resp.Result.TimeSecond, 10, 64); err == nil && sec > 0 {
			serverMs = sec * 1000
		}
	}
	if serverMs > 0 {
		offset := serverMs - time.Now().UnixMilli()
		c.mu.Lock()
		c.timeOffsetMs = offset
		c.mu.Unlock()
	}
}

func prepareBybitQueryString(params url.Values) string {
	if len(params) == 0 {
		return ""
	}
	q := url.Values{}
	for k, vs := range params {
		for _, v := range vs {
			if k == "cursor" && v != "" {
				// Bybit cursors are often already percent-encoded; PathUnescape avoids
				// double-encoding % -> %25 without corrupting '+' in base64 cursors.
				if unescaped, err := url.PathUnescape(v); err == nil {
					v = unescaped
				}
			}
			q.Add(k, v)
		}
	}
	return q.Encode()
}

func (c *BybitAccountClient) signedGet(ctx context.Context, path string, params url.Values) (json.RawMessage, error) {
	for attempt := 0; attempt < 2; attempt++ {
		apiKey, apiSecret, offsetMs := c.credentials()
		if apiKey == "" || apiSecret == "" {
			return nil, fmt.Errorf("bybit read-only api key/secret not configured")
		}

		queryString := prepareBybitQueryString(params)
		ts := strconv.FormatInt(time.Now().UnixMilli()+offsetMs, 10)
		prehash := ts + apiKey + bybitRecvWindow + queryString

		mac := hmac.New(sha256.New, []byte(apiSecret))
		_, _ = mac.Write([]byte(prehash))
		sig := hex.EncodeToString(mac.Sum(nil))

		fullURL := strings.TrimRight(c.baseURL, "/") + path
		if queryString != "" {
			fullURL += "?" + queryString
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("X-BAPI-API-KEY", apiKey)
		req.Header.Set("X-BAPI-TIMESTAMP", ts)
		req.Header.Set("X-BAPI-RECV-WINDOW", bybitRecvWindow)
		req.Header.Set("X-BAPI-SIGN-TYPE", "2")
		req.Header.Set("X-BAPI-SIGN", sig)

		resp, err := c.httpCli.Do(req)
		if err != nil {
			return nil, fmt.Errorf("bybit %s: %w", path, err)
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("bybit %s read: %w", path, readErr)
		}

		var env struct {
			RetCode int             `json:"retCode"`
			RetMsg  string          `json:"retMsg"`
			Result  json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(body, &env); err != nil {
			if resp.StatusCode >= 400 {
				return nil, &bybitAPIError{
					Path:       path,
					HTTPStatus: resp.StatusCode,
					Msg:        strings.TrimSpace(string(body)),
				}
			}
			return nil, fmt.Errorf("bybit %s decode: %w", path, err)
		}

		if env.RetCode == 10002 && attempt == 0 {
			c.syncServerTime(ctx)
			continue
		}
		if env.RetCode != 0 {
			return nil, &bybitAPIError{
				Code:       env.RetCode,
				Msg:        env.RetMsg,
				Path:       path,
				HTTPStatus: resp.StatusCode,
				LimitReset: resp.Header.Get("X-Bapi-Limit-Reset-Timestamp"),
			}
		}
		if resp.StatusCode >= 400 {
			return nil, &bybitAPIError{
				Path:       path,
				HTTPStatus: resp.StatusCode,
				Msg:        env.RetMsg,
			}
		}
		return env.Result, nil
	}
	return nil, fmt.Errorf("bybit %s: timestamp sync retry exhausted", path)
}

// GetAccountInfo calls GET /v5/account/info to resolve UTA status and account margin mode.
func (c *BybitAccountClient) GetAccountInfo(ctx context.Context) (*BybitAccountInfo, error) {
	rawResult, err := c.signedGet(ctx, "/v5/account/info", nil)
	if err != nil {
		return nil, err
	}
	var raw struct {
		UnifiedMarginStatus flexInt `json:"unifiedMarginStatus"`
		MarginMode          string  `json:"marginMode"`
		UpdateTime          string  `json:"updateTime"`
	}
	if err := json.Unmarshal(rawResult, &raw); err != nil {
		return nil, fmt.Errorf("decode bybit account info: %w", err)
	}
	return &BybitAccountInfo{
		UnifiedMarginStatus: int(raw.UnifiedMarginStatus),
		MarginMode:          raw.MarginMode,
		UpdateTime:          raw.UpdateTime,
	}, nil
}

// GetWalletBalance calls GET /v5/account/wallet-balance?accountType=UNIFIED,
// falling back to accountType=CONTRACT&coin=USDT for Classic accounts.
func (c *BybitAccountClient) GetWalletBalance(ctx context.Context) (*BybitAccountSummary, error) {
	sum, err := c.getWalletBalanceForAccountType(ctx, "UNIFIED")
	if err == nil && sum != nil {
		return sum, nil
	}
	if err != nil && !isBybitErrCode(err, 10001) {
		return nil, err
	}
	return c.getWalletBalanceForAccountType(ctx, "CONTRACT")
}

func (c *BybitAccountClient) getWalletBalanceWithMode(ctx context.Context, isUTA bool) (*BybitAccountSummary, error) {
	if !isUTA {
		sum, err := c.getWalletBalanceForAccountType(ctx, "CONTRACT")
		if err == nil && sum != nil {
			return sum, nil
		}
	}
	return c.GetWalletBalance(ctx)
}

func (c *BybitAccountClient) getWalletBalanceForAccountType(ctx context.Context, accountType string) (*BybitAccountSummary, error) {
	q := url.Values{"accountType": {accountType}}
	if accountType == "CONTRACT" {
		q.Set("coin", "USDT")
	}
	rawResult, err := c.signedGet(ctx, "/v5/account/wallet-balance", q)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		List []struct {
			AccountType           string `json:"accountType"`
			TotalWalletBalance    string `json:"totalWalletBalance"`
			TotalPerpUPL          string `json:"totalPerpUPL"`
			TotalMarginBalance    string `json:"totalMarginBalance"`
			TotalEquity           string `json:"totalEquity"`
			TotalAvailableBalance string `json:"totalAvailableBalance"`
			TotalInitialMargin    string `json:"totalInitialMargin"`
			Coin                  []struct {
				Coin                string `json:"coin"`
				WalletBalance       string `json:"walletBalance"`
				UnrealisedPnl       string `json:"unrealisedPnl"`
				Equity              string `json:"equity"`
				USDValue            string `json:"usdValue"`
				AvailableToWithdraw string `json:"availableToWithdraw"`
			} `json:"coin"`
		} `json:"list"`
	}
	if err := json.Unmarshal(rawResult, &parsed); err != nil {
		return nil, fmt.Errorf("decode bybit wallet balance: %w", err)
	}
	if len(parsed.List) == 0 {
		return nil, nil
	}
	row := parsed.List[0]
	acctType := row.AccountType
	if acctType == "" {
		acctType = accountType
	}
	sum := &BybitAccountSummary{
		AccountType:      acctType,
		WalletBalanceUSD: parseFloat(row.TotalWalletBalance),
		UnrealizedPnLUSD: parseFloat(row.TotalPerpUPL),
		MarginBalanceUSD: parseFloat(row.TotalMarginBalance),
		AvailableBalance: parseFloat(row.TotalAvailableBalance),
		TotalEquity:      parseFloat(row.TotalEquity),
		InitialMarginUSD: parseFloat(row.TotalInitialMargin),
	}
	for _, coin := range row.Coin {
		if strings.EqualFold(coin.Coin, "USDT") {
			if wb := parseFloat(coin.WalletBalance); wb > 0 || sum.WalletBalanceUSD == 0 {
				sum.WalletBalanceUSD = wb
			}
			if up := parseFloat(coin.UnrealisedPnl); up != 0 || sum.UnrealizedPnLUSD == 0 {
				sum.UnrealizedPnLUSD = up
			}
			if sum.AvailableBalance == 0 {
				sum.AvailableBalance = parseFloat(coin.AvailableToWithdraw)
			}
			if sum.TotalEquity == 0 {
				sum.TotalEquity = parseFloat(coin.Equity)
			}
			break
		}
	}
	return sum, nil
}

// GetPositions calls GET /v5/position/list?category=linear&settleCoin=USDT with
// cursor pagination and returns active linear USDT positions.
func (c *BybitAccountClient) GetPositions(ctx context.Context) ([]cache.OpenPosition, error) {
	rows, err := c.getRawPositions(ctx, "")
	if err != nil {
		return nil, err
	}
	return buildBybitOpenPositions(rows, nil, time.Now().Unix()), nil
}

func (c *BybitAccountClient) getRawPositions(ctx context.Context, rawSymbol string) ([]BybitPositionRow, error) {
	var all []BybitPositionRow
	seenCursors := make(map[string]bool)
	cursor := ""

	for page := 0; page < bybitMaxPages; page++ {
		q := url.Values{
			"category": {"linear"},
			"limit":    {"200"},
		}
		if rawSymbol != "" {
			q.Set("symbol", rawSymbol)
		} else {
			q.Set("settleCoin", "USDT")
		}
		if cursor != "" {
			q.Set("cursor", cursor)
		}

		rawResult, err := c.signedGet(ctx, "/v5/position/list", q)
		if err != nil {
			return nil, err
		}
		var parsed struct {
			NextPageCursor string `json:"nextPageCursor"`
			List           []struct {
				Symbol          string  `json:"symbol"`
				Side            string  `json:"side"`
				Size            string  `json:"size"`
				AvgPrice        string  `json:"avgPrice"`
				MarkPrice       string  `json:"markPrice"`
				UnrealisedPnl   string  `json:"unrealisedPnl"`
				LiqPrice        string  `json:"liqPrice"`
				Leverage        string  `json:"leverage"`
				TradeMode       flexInt `json:"tradeMode"`
				PositionIM      string  `json:"positionIM"`
				PositionBalance string  `json:"positionBalance"`
				PositionIdx     flexInt `json:"positionIdx"`
				UpdatedTime     string  `json:"updatedTime"`
			} `json:"list"`
		}
		if err := json.Unmarshal(rawResult, &parsed); err != nil {
			return nil, fmt.Errorf("decode bybit position list: %w", err)
		}
		for _, r := range parsed.List {
			all = append(all, BybitPositionRow{
				Symbol:          r.Symbol,
				Side:            r.Side,
				Size:            r.Size,
				AvgPrice:        r.AvgPrice,
				MarkPrice:       r.MarkPrice,
				UnrealisedPnl:   r.UnrealisedPnl,
				LiqPrice:        r.LiqPrice,
				Leverage:        r.Leverage,
				TradeMode:       int(r.TradeMode),
				PositionIM:      r.PositionIM,
				PositionBalance: r.PositionBalance,
				PositionIdx:     int(r.PositionIdx),
				UpdatedTime:     r.UpdatedTime,
			})
		}

		next := strings.TrimSpace(parsed.NextPageCursor)
		if next == "" || seenCursors[next] {
			break
		}
		seenCursors[next] = true
		cursor = next
	}
	return all, nil
}

// GetExecutions fetches linear execution fills over [startTimeMs, endTimeMs]
// in <=7-day chunks with cursor pagination, keeping ONLY Trade/AdlTrade/BustTrade/BlockTrade
// and excluding Funding rows.
func (c *BybitAccountClient) GetExecutions(ctx context.Context, startTimeMs, endTimeMs int64) ([]BybitExecutionRow, error) {
	return c.getRawExecutions(ctx, startTimeMs, endTimeMs, "")
}

func (c *BybitAccountClient) getRawExecutions(ctx context.Context, startTimeMs, endTimeMs int64, execTypeFilter string) ([]BybitExecutionRow, error) {
	chunks := splitTimeChunks(startTimeMs, endTimeMs, bybitMaxWindowMs)
	var all []BybitExecutionRow
	seenIDs := make(map[string]bool)

	for _, ch := range chunks {
		cursor := ""
		seenCursors := make(map[string]bool)
		for page := 0; page < bybitMaxPages; page++ {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			q := url.Values{
				"category":  {"linear"},
				"limit":     {"100"},
				"startTime": {strconv.FormatInt(ch.start, 10)},
				"endTime":   {strconv.FormatInt(ch.end, 10)},
			}
			if execTypeFilter != "" {
				q.Set("execType", execTypeFilter)
			}
			if cursor != "" {
				q.Set("cursor", cursor)
			}

			rawResult, err := c.signedGet(ctx, "/v5/execution/list", q)
			if err != nil {
				return nil, err
			}
			var parsed struct {
				NextPageCursor string              `json:"nextPageCursor"`
				List           []BybitExecutionRow `json:"list"`
			}
			if err := json.Unmarshal(rawResult, &parsed); err != nil {
				return nil, fmt.Errorf("decode bybit execution list: %w", err)
			}
			for _, r := range parsed.List {
				if canonicalBybitLinearSymbol(r.Symbol) == "" {
					continue
				}
				if execTypeFilter == "" && !isBybitFillExecType(r.ExecType) {
					continue
				}
				if r.ExecID != "" {
					if seenIDs[r.ExecID] {
						continue
					}
					seenIDs[r.ExecID] = true
				}
				all = append(all, r)
			}
			next := strings.TrimSpace(parsed.NextPageCursor)
			if next == "" || seenCursors[next] {
				break
			}
			seenCursors[next] = true
			cursor = next
		}
	}
	return all, nil
}

// GetClosedPnL calls GET /v5/position/closed-pnl?category=linear in <=7-day chunks
// with cursor pagination to enrich leverage and verify closed trade PnL.
func (c *BybitAccountClient) GetClosedPnL(ctx context.Context, startTimeMs, endTimeMs int64) ([]BybitClosedPnLRow, error) {
	chunks := splitTimeChunks(startTimeMs, endTimeMs, bybitMaxWindowMs)
	var all []BybitClosedPnLRow

	for _, ch := range chunks {
		cursor := ""
		seenCursors := make(map[string]bool)
		for page := 0; page < bybitMaxPages; page++ {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			q := url.Values{
				"category":  {"linear"},
				"limit":     {"100"},
				"startTime": {strconv.FormatInt(ch.start, 10)},
				"endTime":   {strconv.FormatInt(ch.end, 10)},
			}
			if cursor != "" {
				q.Set("cursor", cursor)
			}
			rawResult, err := c.signedGet(ctx, "/v5/position/closed-pnl", q)
			if err != nil {
				return nil, err
			}
			var parsed struct {
				NextPageCursor string              `json:"nextPageCursor"`
				List           []BybitClosedPnLRow `json:"list"`
			}
			if err := json.Unmarshal(rawResult, &parsed); err != nil {
				return nil, fmt.Errorf("decode bybit closed pnl: %w", err)
			}
			for _, r := range parsed.List {
				if canonicalBybitLinearSymbol(r.Symbol) == "" {
					continue
				}
				all = append(all, r)
			}
			next := strings.TrimSpace(parsed.NextPageCursor)
			if next == "" || seenCursors[next] {
				break
			}
			seenCursors[next] = true
			cursor = next
		}
	}
	return all, nil
}

// GetTransactionLog fetches UTA funding settlement records over [startTimeMs, endTimeMs]
// via GET /v5/account/transaction-log (explicit startTime/endTime in <=7-day chunks,
// limit=50, cursor pagination), falling back to Classic execution/list?execType=Funding
// when UNIFIED transaction-log is unsupported.
func (c *BybitAccountClient) GetTransactionLog(ctx context.Context, startTimeMs, endTimeMs int64) ([]cache.IncomeRecord, error) {
	recs, err := c.getFundingFromTransactionLog(ctx, startTimeMs, endTimeMs)
	if err == nil {
		return recs, nil
	}
	if isBybitErrCode(err, 10001) {
		return c.getFundingFromExecutionList(ctx, startTimeMs, endTimeMs)
	}
	return nil, err
}

func (c *BybitAccountClient) getFundingFromTransactionLog(ctx context.Context, startTimeMs, endTimeMs int64) ([]cache.IncomeRecord, error) {
	chunks := splitTimeChunks(startTimeMs, endTimeMs, bybitMaxWindowMs)
	var all []cache.IncomeRecord
	seenIDs := make(map[string]bool)

	for _, ch := range chunks {
		cursor := ""
		seenCursors := make(map[string]bool)
		for page := 0; page < bybitMaxPages; page++ {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			q := url.Values{
				"accountType": {"UNIFIED"},
				"category":    {"linear"},
				"limit":       {"50"},
				"startTime":   {strconv.FormatInt(ch.start, 10)},
				"endTime":     {strconv.FormatInt(ch.end, 10)},
			}
			if cursor != "" {
				q.Set("cursor", cursor)
			}

			rawResult, err := c.signedGet(ctx, "/v5/account/transaction-log", q)
			if err != nil {
				return nil, err
			}
			var parsed struct {
				NextPageCursor string                   `json:"nextPageCursor"`
				List           []BybitTransactionLogRow `json:"list"`
			}
			if err := json.Unmarshal(rawResult, &parsed); err != nil {
				return nil, fmt.Errorf("decode bybit transaction log: %w", err)
			}
			for _, r := range parsed.List {
				if !strings.EqualFold(strings.TrimSpace(r.Type), "SETTLEMENT") {
					continue
				}
				funding := parseFloat(r.Funding)
				if funding == 0 {
					continue
				}
				sym := canonicalBybitLinearSymbol(r.Symbol)
				if sym == "" {
					continue
				}
				txMs, _ := strconv.ParseInt(strings.TrimSpace(r.TransactionTime), 10, 64)
				id := strings.TrimSpace(r.ID)
				if id == "" {
					id = fmt.Sprintf("%s_%d_%s", sym, txMs, r.Funding)
				}
				tranID := "bybit:" + id
				if seenIDs[tranID] {
					continue
				}
				seenIDs[tranID] = true
				asset := strings.ToUpper(strings.TrimSpace(r.Currency))
				if asset == "" {
					asset = "USDT"
				}
				all = append(all, cache.IncomeRecord{
					Exchange:   "bybit",
					TranID:     tranID,
					Symbol:     sym,
					IncomeType: "FUNDING_FEE",
					Income:     funding,
					Asset:      asset,
					TimeMs:     txMs,
				})
			}

			next := strings.TrimSpace(parsed.NextPageCursor)
			if next == "" || seenCursors[next] {
				break
			}
			seenCursors[next] = true
			cursor = next
		}
	}
	return all, nil
}

func (c *BybitAccountClient) getFundingFromExecutionList(ctx context.Context, startTimeMs, endTimeMs int64) ([]cache.IncomeRecord, error) {
	rows, err := c.getRawExecutions(ctx, startTimeMs, endTimeMs, "Funding")
	if err != nil {
		return nil, err
	}
	all := make([]cache.IncomeRecord, 0, len(rows))
	for _, r := range rows {
		sym := canonicalBybitLinearSymbol(r.Symbol)
		if sym == "" {
			continue
		}
		fee := parseFloat(r.ExecFee)
		if fee == 0 {
			continue
		}
		execMs, _ := strconv.ParseInt(strings.TrimSpace(r.ExecTime), 10, 64)
		id := strings.TrimSpace(r.ExecID)
		if id == "" {
			id = fmt.Sprintf("%s_%d_%s", sym, execMs, r.ExecFee)
		}
		asset := strings.ToUpper(strings.TrimSpace(r.FeeCurrency))
		if asset == "" {
			asset = "USDT"
		}
		// In execution/list?execType=Funding, execFee>0 means paid and <0 means received;
		// Kairos IncomeRecord uses + received / - paid.
		all = append(all, cache.IncomeRecord{
			Exchange:   "bybit",
			TranID:     "bybit:funding:" + id,
			Symbol:     sym,
			IncomeType: "FUNDING_FEE",
			Income:     -fee,
			Asset:      asset,
			TimeMs:     execMs,
		})
	}
	return all, nil
}

// SyncToCache pulls read-only Bybit V5 account balances, positions, executions,
// closed-PnL leverage hints, and funding settlements, runs FIFO lot aggregation
// and DecisionTicket matching, and updates desktop-cache.db strictly under exchange="bybit".
func (c *BybitAccountClient) SyncToCache(
	ctx context.Context,
	store *cache.Store,
	journal *storage.Journal,
	trackedSymbols []string,
) (*AccountSyncOutcome, error) {
	if c == nil || store == nil {
		return nil, fmt.Errorf("account client or cache store not initialized")
	}
	if !c.HasKeys() {
		return nil, fmt.Errorf("bybit read-only api keys not configured")
	}

	const ex = "bybit"
	_ = trackedSymbols

	// 1. Fetch account/info once to distinguish UTA vs Classic and resolve UTA marginMode.
	acctInfo, err := c.GetAccountInfo(ctx)
	if err != nil {
		_ = store.SetExchangeMeta(ctx, ex, "sync_error", err.Error())
		return nil, err
	}
	isUTA := acctInfo.IsUTA()

	latestIncomeMs, err := store.LatestIncomeTimeMsForExchange(ctx, ex)
	if err != nil {
		return nil, err
	}
	latestFillMs, err := store.LatestFillTimeMsForExchange(ctx, ex, "")
	if err != nil {
		return nil, err
	}

	nowMs := time.Now().UnixMilli()
	defaultStartMs := nowMs - int64(bybitLookbackDays)*24*3600*1000
	incomeStartMs := defaultStartMs
	if latestIncomeMs > 0 {
		incomeStartMs = latestIncomeMs - 1000
		if incomeStartMs < 0 {
			incomeStartMs = 0
		}
	}
	fillStartMs := defaultStartMs
	if latestFillMs > 0 {
		fillStartMs = latestFillMs - 1000
		if fillStartMs < 0 {
			fillStartMs = 0
		}
	}

	// 2. Fetch wallet balance, positions, executions, closed PnL, and funding concurrently.
	var (
		wallet     *BybitAccountSummary
		walletErr  error
		rawPos     []BybitPositionRow
		posErr     error
		rawExecs   []BybitExecutionRow
		execErr    error
		closedRows []BybitClosedPnLRow
		closedErr  error
		newIncomes []cache.IncomeRecord
		incomeErr  error
	)

	var wg sync.WaitGroup
	wg.Add(5)
	go func() {
		defer wg.Done()
		wallet, walletErr = c.getWalletBalanceWithMode(ctx, isUTA)
	}()
	go func() {
		defer wg.Done()
		rawPos, posErr = c.getRawPositions(ctx, "")
	}()
	go func() {
		defer wg.Done()
		rawExecs, execErr = c.getRawExecutions(ctx, fillStartMs, nowMs, "")
	}()
	go func() {
		defer wg.Done()
		closedRows, closedErr = c.GetClosedPnL(ctx, fillStartMs, nowMs)
	}()
	go func() {
		defer wg.Done()
		if isUTA {
			newIncomes, incomeErr = c.GetTransactionLog(ctx, incomeStartMs, nowMs)
		} else {
			newIncomes, incomeErr = c.getFundingFromExecutionList(ctx, incomeStartMs, nowMs)
		}
	}()
	wg.Wait()

	for _, fetchErr := range []error{walletErr, posErr, execErr, closedErr, incomeErr} {
		if fetchErr != nil {
			_ = store.SetExchangeMeta(ctx, ex, "sync_error", fetchErr.Error())
			return nil, fetchErr
		}
	}
	if wallet == nil {
		wallet = &BybitAccountSummary{}
	}

	// 3. Disambiguate One-Way vs Hedge mode per symbol using position/list positionIdx.
	hedgeModeBySym := make(map[string]bool)
	rawSymByCanon := make(map[string]string)
	for _, p := range rawPos {
		canon := canonicalBybitLinearSymbol(p.Symbol)
		if canon == "" {
			continue
		}
		recordBybitPositionMode(hedgeModeBySym, canon, p.PositionIdx)
	}
	for _, r := range rawExecs {
		canon := canonicalBybitLinearSymbol(r.Symbol)
		if canon == "" {
			continue
		}
		if _, known := hedgeModeBySym[canon]; !known {
			rawSymByCanon[canon] = strings.ToUpper(strings.TrimSpace(r.Symbol))
		}
	}
	if len(rawSymByCanon) > 0 {
		missingSyms := make([]string, 0, len(rawSymByCanon))
		for canon := range rawSymByCanon {
			missingSyms = append(missingSyms, canon)
		}
		sort.Strings(missingSyms)
		for _, canon := range missingSyms {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			extraRows, err := c.getRawPositions(ctx, rawSymByCanon[canon])
			if err != nil {
				continue
			}
			for _, p := range extraRows {
				if pCanon := canonicalBybitLinearSymbol(p.Symbol); pCanon != "" {
					recordBybitPositionMode(hedgeModeBySym, pCanon, p.PositionIdx)
				}
			}
			rawPos = append(rawPos, extraRows...)
		}
	}
	// If any symbol still lacks positionIdx metadata, infer One-Way when a partial-close flip occurs.
	for _, r := range rawExecs {
		canon := canonicalBybitLinearSymbol(r.Symbol)
		if canon == "" {
			continue
		}
		if _, known := hedgeModeBySym[canon]; known {
			continue
		}
		qty := parseFloat(r.ExecQty)
		closed := parseFloat(r.ClosedSize)
		if closed > 1e-12 && closed < qty-1e-12 {
			hedgeModeBySym[canon] = false
		}
	}

	fetchedFills := make([]cache.TradeFill, 0, len(rawExecs))
	for _, r := range rawExecs {
		canon := canonicalBybitLinearSymbol(r.Symbol)
		if canon == "" {
			continue
		}
		fetchedFills = append(fetchedFills, convertBybitExecution(canon, r, hedgeModeBySym[canon]))
	}

	// 4. Upsert incomes and merge existing + newly fetched fills strictly for "bybit".
	if err := store.UpsertIncomeRecordsForExchange(ctx, ex, newIncomes); err != nil {
		return nil, err
	}
	allIncomes, err := store.ListIncomeRecordsForExchange(ctx, ex, "")
	if err != nil {
		return nil, err
	}
	existingFills, err := store.ListTradeFillsForExchange(ctx, ex, "", 0)
	if err != nil {
		return nil, err
	}
	allFills := mergeFillsForSync(ex, existingFills, fetchedFills)

	var tickets []types.DecisionTicket
	var decisions map[string]types.DecisionRecord
	if journal != nil {
		tickets, _ = journal.ListTickets("")
		decisions, _ = journal.ListDecisions()
	}

	posMeta := bybitPositionsToMeta(rawPos, acctInfo)
	closedHints := bybitClosedPnLToHints(closedRows)
	defaultMM := acctInfo.ResolvedMarginMode()
	if defaultMM == "" {
		defaultMM = "cross"
	}

	// 5. Aggregate FIFO lots + match DecisionTickets.
	trades, linkedFills := AggregateTrades(AggregateTradesInput{
		Exchange:          ex,
		Fills:             allFills,
		Incomes:           allIncomes,
		PositionMeta:      posMeta,
		ClosedHints:       closedHints,
		DefaultMarginMode: defaultMM,
		Tickets:           tickets,
		Decisions:         decisions,
	})

	nowSec := time.Now().Unix()
	openPositions := buildBybitOpenPositions(rawPos, acctInfo, nowSec)

	if err := persistAccountSync(ctx, store, ex, trades, linkedFills, openPositions, wallet.WalletBalanceUSD, wallet.UnrealizedPnLUSD, nowSec); err != nil {
		return nil, err
	}
	if defaultMM != "" {
		_ = store.SetExchangeMeta(ctx, ex, "margin_mode", defaultMM)
	}
	if wallet.AccountType != "" {
		_ = store.SetExchangeMeta(ctx, ex, "account_type", wallet.AccountType)
	}

	return &AccountSyncOutcome{
		SyncedTrades:     len(trades),
		SyncedFills:      len(linkedFills),
		OpenPositions:    len(openPositions),
		WalletBalanceUSD: wallet.WalletBalanceUSD,
		UnrealizedPnLUSD: wallet.UnrealizedPnLUSD,
		LastSyncAt:       nowSec,
	}, nil
}

func recordBybitPositionMode(hedgeModeBySym map[string]bool, canon string, positionIdx int) {
	if positionIdx == 1 || positionIdx == 2 {
		hedgeModeBySym[canon] = true
		return
	}
	if positionIdx == 0 {
		if _, exists := hedgeModeBySym[canon]; !exists {
			hedgeModeBySym[canon] = false
		}
	}
}

func canonicalBybitLinearSymbol(raw string) string {
	trimmed := strings.ToUpper(strings.TrimSpace(raw))
	if trimmed == "" || strings.Contains(trimmed, "-") {
		return ""
	}
	return toCanonicalBybit(trimmed)
}

func isBybitFillExecType(execType string) bool {
	switch strings.ToLower(strings.TrimSpace(execType)) {
	case "trade", "adltrade", "busttrade", "blocktrade":
		return true
	default:
		return false
	}
}

func bybitPositionSide(idx int) string {
	switch idx {
	case 1:
		return "LONG"
	case 2:
		return "SHORT"
	default:
		return "BOTH"
	}
}

func bybitExecutionPositionSide(sideUpper string, closedSize float64, hedgeMode bool) string {
	if !hedgeMode {
		return "BOTH"
	}
	if (sideUpper == "BUY" && closedSize <= 1e-12) || (sideUpper == "SELL" && closedSize > 1e-12) {
		return "LONG"
	}
	return "SHORT"
}

func bybitResolvedRowMarginMode(tradeMode int, info *BybitAccountInfo) string {
	if mm := info.ResolvedMarginMode(); mm != "" {
		return mm
	}
	if tradeMode == 1 {
		return decision.MarginModeIsolated
	}
	return "cross"
}

func convertBybitExecution(canonicalSym string, r BybitExecutionRow, hedgeMode bool) cache.TradeFill {
	sideUpper := strings.ToUpper(strings.TrimSpace(r.Side))
	closedSize := parseFloat(r.ClosedSize)
	posSide := bybitExecutionPositionSide(sideUpper, closedSize, hedgeMode)
	px := parseFloat(r.ExecPrice)
	qty := parseFloat(r.ExecQty)
	quoteQty := parseFloat(r.ExecValue)
	if quoteQty == 0 && px > 0 && qty > 0 {
		quoteQty = px * qty
	}
	feeAsset := strings.ToUpper(strings.TrimSpace(r.FeeCurrency))
	if feeAsset == "" {
		feeAsset = "USDT"
	}
	execMs, _ := strconv.ParseInt(strings.TrimSpace(r.ExecTime), 10, 64)
	return cache.TradeFill{
		Exchange:        "bybit",
		FillID:          "bybit:" + canonicalSym + ":" + strings.TrimSpace(r.ExecID),
		OrderID:         strings.TrimSpace(r.OrderID),
		Symbol:          canonicalSym,
		Side:            sideUpper,
		PositionSide:    posSide,
		Price:           px,
		Qty:             qty,
		QuoteQty:        quoteQty,
		Commission:      parseFloat(r.ExecFee), // preserve negative maker rebates
		CommissionAsset: feeAsset,
		RealizedPnL:     parseFloat(r.ExecPnl), // GROSS realized PnL
		TimeMs:          execMs,
	}
}

func buildBybitOpenPositions(rows []BybitPositionRow, info *BybitAccountInfo, fallbackUpdatedSec int64) []cache.OpenPosition {
	type posKey struct {
		sym  string
		side string
	}
	dedup := make(map[posKey]cache.OpenPosition)
	var keys []posKey

	for _, r := range rows {
		size := math.Abs(parseFloat(r.Size))
		if size <= 1e-12 || strings.TrimSpace(r.Side) == "" {
			continue
		}
		sym := canonicalBybitLinearSymbol(r.Symbol)
		if sym == "" {
			continue
		}
		posSide := bybitPositionSide(r.PositionIdx)
		qty := size
		if strings.EqualFold(strings.TrimSpace(r.Side), "Sell") {
			qty = -size
		}
		lev := parseFloat(r.Leverage)
		if lev <= 0 {
			lev = 1
		}
		isoWallet := parseFloat(r.PositionIM)
		if isoWallet <= 0 {
			isoWallet = parseFloat(r.PositionBalance)
		}
		updatedSec := fallbackUpdatedSec
		if ms, err := strconv.ParseInt(strings.TrimSpace(r.UpdatedTime), 10, 64); err == nil && ms > 0 {
			updatedSec = ms / 1000
		}
		k := posKey{sym: sym, side: posSide}
		if _, exists := dedup[k]; !exists {
			keys = append(keys, k)
		}
		dedup[k] = cache.OpenPosition{
			Exchange:       "bybit",
			Symbol:         sym,
			PositionSide:   posSide,
			MarginMode:     bybitResolvedRowMarginMode(r.TradeMode, info),
			Leverage:       lev,
			Qty:            qty,
			EntryPrice:     parseFloat(r.AvgPrice),
			MarkPrice:      parseFloat(r.MarkPrice),
			UnrealizedPnL:  parseFloat(r.UnrealisedPnl),
			LiqPrice:       parseFloat(r.LiqPrice),
			IsolatedWallet: isoWallet,
			UpdatedAt:      updatedSec,
		}
	}

	sort.Slice(keys, func(i, j int) bool {
		if keys[i].sym != keys[j].sym {
			return keys[i].sym < keys[j].sym
		}
		return keys[i].side < keys[j].side
	})
	out := make([]cache.OpenPosition, 0, len(keys))
	for _, k := range keys {
		out = append(out, dedup[k])
	}
	return out
}

func bybitPositionsToMeta(rows []BybitPositionRow, info *BybitAccountInfo) []PositionMeta {
	out := make([]PositionMeta, 0, len(rows))
	for _, r := range rows {
		sym := canonicalBybitLinearSymbol(r.Symbol)
		if sym == "" {
			continue
		}
		lev := parseFloat(r.Leverage)
		if lev <= 0 {
			lev = 1
		}
		isoWallet := parseFloat(r.PositionIM)
		if isoWallet <= 0 {
			isoWallet = parseFloat(r.PositionBalance)
		}
		out = append(out, PositionMeta{
			Symbol:         sym,
			PositionSide:   bybitPositionSide(r.PositionIdx),
			MarginMode:     bybitResolvedRowMarginMode(r.TradeMode, info),
			Leverage:       lev,
			LiqPrice:       parseFloat(r.LiqPrice),
			IsolatedWallet: isoWallet,
			IsOpen:         math.Abs(parseFloat(r.Size)) > 1e-12,
		})
	}
	return out
}

func bybitClosedPnLToHints(rows []BybitClosedPnLRow) []ClosedTradeHint {
	out := make([]ClosedTradeHint, 0, len(rows))
	for _, r := range rows {
		sym := canonicalBybitLinearSymbol(r.Symbol)
		if sym == "" {
			continue
		}
		lev := parseFloat(r.Leverage)
		if lev <= 0 {
			continue
		}
		updatedMs, _ := strconv.ParseInt(strings.TrimSpace(r.UpdatedTime), 10, 64)
		out = append(out, ClosedTradeHint{
			Symbol:      sym,
			OrderID:     strings.TrimSpace(r.OrderID),
			Leverage:    lev,
			ClosedPnL:   parseFloat(r.ClosedPnl),
			UpdatedAtMs: updatedMs,
		})
	}
	return out
}
