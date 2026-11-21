package exchange

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ArchdevilForge/kairos/internal/cache"
	"github.com/ArchdevilForge/kairos/internal/decision"
	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
)

const (
	envBinanceReadonlyKey    = "BINANCE_READONLY_API_KEY"
	envBinanceReadonlySecret = "BINANCE_READONLY_API_SECRET"
	envBybitReadonlyKey      = "BYBIT_READONLY_API_KEY"
	envBybitReadonlySecret   = "BYBIT_READONLY_API_SECRET"
)

// AccountClient is the shared read-only perpetual account sync interface
// implemented by exchange account adapters (e.g. Bybit V5, Binance USDT-M).
type AccountClient interface {
	Exchange() string
	HasKeys() bool
	APIKeyMasked() string
	SetKeys(apiKey, apiSecret string)
	ReloadKeys()
	SyncToCache(ctx context.Context, store *cache.Store, journal *storage.Journal, trackedSymbols []string) (*AccountSyncOutcome, error)
}

// NewAccountClient constructs a strictly read-only account client for exchangeName.
// Empty or unknown names default to "bybit".
func NewAccountClient(exchangeName string) AccountClient {
	switch strings.ToLower(strings.TrimSpace(exchangeName)) {
	case "binance":
		return NewBinanceAccountClient()
	default:
		return NewBybitAccountClient()
	}
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

// LoadReadonlyKeys loads keyEnv and secretEnv from process environment variables
// first, falling back to ~/.config/kairos/secrets.env.
func LoadReadonlyKeys(keyEnv, secretEnv string) (apiKey, apiSecret string) {
	apiKey = cleanCredential(os.Getenv(keyEnv))
	apiSecret = cleanCredential(os.Getenv(secretEnv))
	if apiKey != "" && apiSecret != "" {
		return apiKey, apiSecret
	}
	p, err := SecretsFilePath()
	if err != nil {
		return apiKey, apiSecret
	}
	kv := parseEnvFile(p)
	if apiKey == "" {
		apiKey = cleanCredential(kv[keyEnv])
	}
	if apiSecret == "" {
		apiSecret = cleanCredential(kv[secretEnv])
	}
	return apiKey, apiSecret
}

// SaveReadonlyKeys stores keyEnv/secretEnv in ~/.config/kairos/secrets.env with 0600
// permissions, preserving all existing entries in that file.
func SaveReadonlyKeys(keyEnv, secretEnv, apiKey, apiSecret string) error {
	p, err := SecretsFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return fmt.Errorf("secrets mkdir: %w", err)
	}
	existing := parseEnvFile(p)
	if k := cleanCredential(apiKey); k != "" {
		existing[keyEnv] = k
	}
	if s := cleanCredential(apiSecret); s != "" {
		existing[secretEnv] = s
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

// LoadBybitReadonlyKeys loads BYBIT_READONLY_API_KEY and BYBIT_READONLY_API_SECRET
// from environment variables first, falling back to ~/.config/kairos/secrets.env.
func LoadBybitReadonlyKeys() (apiKey, apiSecret string) {
	return LoadReadonlyKeys(envBybitReadonlyKey, envBybitReadonlySecret)
}

// SaveBybitReadonlyKeys stores read-only Bybit API credentials in ~/.config/kairos/secrets.env
// with 0600 permissions, preserving all existing entries.
func SaveBybitReadonlyKeys(apiKey, apiSecret string) error {
	return SaveReadonlyKeys(envBybitReadonlyKey, envBybitReadonlySecret, apiKey, apiSecret)
}

// LoadBinanceReadonlyKeys loads BINANCE_READONLY_API_KEY and BINANCE_READONLY_API_SECRET
// from environment variables first, falling back to ~/.config/kairos/secrets.env.
func LoadBinanceReadonlyKeys() (apiKey, apiSecret string) {
	return LoadReadonlyKeys(envBinanceReadonlyKey, envBinanceReadonlySecret)
}

// SaveBinanceReadonlyKeys stores read-only Binance API credentials in ~/.config/kairos/secrets.env
// with 0600 permissions, preserving any other key/value entries in that file.
func SaveBinanceReadonlyKeys(apiKey, apiSecret string) error {
	return SaveReadonlyKeys(envBinanceReadonlyKey, envBinanceReadonlySecret, apiKey, apiSecret)
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

// PositionMeta carries exchange-normalized position metadata for FIFO trade aggregation.
type PositionMeta struct {
	Symbol         string
	PositionSide   string
	MarginMode     string
	Leverage       float64
	LiqPrice       float64
	IsolatedWallet float64
	IsOpen         bool
}

// ClosedTradeHint carries per-order leverage and net PnL metadata from exchange
// closed-PnL endpoints (e.g. Bybit /v5/position/closed-pnl) to enrich closed trades.
type ClosedTradeHint struct {
	Symbol      string
	OrderID     string
	Leverage    float64
	ClosedPnL   float64
	UpdatedAtMs int64
}

// AggregateTradesInput bundles normalized fills, funding income, position metadata,
// optional closed-PnL hints, and decision journal inputs for FIFO reconciliation.
type AggregateTradesInput struct {
	Exchange          string
	Fills             []cache.TradeFill
	Incomes           []cache.IncomeRecord
	PositionMeta      []PositionMeta
	ClosedHints       []ClosedTradeHint
	DefaultMarginMode string
	Tickets           []types.DecisionTicket
	Decisions         map[string]types.DecisionRecord
	BNBPriceUSD       float64
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

// AggregateTrades performs deterministic FIFO lot aggregation keyed by
// (symbol, positionSide), preserves open positions, attaches leverage/margin/liq_price
// and real commission+funding costs (NetPnL = RealizedPnL - FeeUSD + FundingUSD),
// links TradeFill.TradeID, and matches trades to accepted DecisionTickets.
func AggregateTrades(in AggregateTradesInput) ([]cache.SyncedTrade, []cache.TradeFill) {
	if len(in.Fills) == 0 {
		return nil, nil
	}

	ex := strings.ToLower(strings.TrimSpace(in.Exchange))
	if ex == "" {
		ex = "binance"
	}
	defaultMM := strings.ToLower(strings.TrimSpace(in.DefaultMarginMode))
	if defaultMM == "" {
		defaultMM = "cross"
	}

	fills := make([]cache.TradeFill, len(in.Fills))
	copy(fills, in.Fills)
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
		isOpen         bool
	}
	metaByKey := make(map[string]posMeta)
	metaBySym := make(map[string]posMeta)
	for _, r := range in.PositionMeta {
		if r.Symbol == "" {
			continue
		}
		ps := strings.ToUpper(strings.TrimSpace(r.PositionSide))
		if ps == "" {
			ps = "BOTH"
		}
		lev := r.Leverage
		if lev <= 0 {
			lev = 1
		}
		mm := strings.ToLower(strings.TrimSpace(r.MarginMode))
		if mm == "" {
			mm = defaultMM
		}
		pm := posMeta{
			marginMode:     mm,
			leverage:       lev,
			liqPrice:       r.LiqPrice,
			isolatedWallet: r.IsolatedWallet,
			isOpen:         r.IsOpen,
		}
		metaByKey[r.Symbol+"|"+ps] = pm
		if prev, exists := metaBySym[r.Symbol]; !exists || (!prev.isOpen && pm.isOpen) {
			metaBySym[r.Symbol] = pm
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
		f.Exchange = ex
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
			meta.marginMode = defaultMM
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
			Exchange:     ex,
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
			id:           buildSyncedTradeID(ex, f),
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
			feeUSD := fillFeeUSD(f, in.BNBPriceUSD)
			fillDir := fillSideDirection(f.PositionSide, f.Side)

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
				commClose := f.Commission * closeRatio
				commOpen := f.Commission - commClose

				fcClose := f
				fcClose.Qty = closeQty
				fcClose.QuoteQty = closeQty * f.Price
				fcClose.Commission = commClose
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
				fcOpen.Commission = commOpen
				fcOpen.RealizedPnL = 0
				fcOpen.TradeID = acc.id
				linkedFills = append(linkedFills, fcOpen)
			}
		}

		if acc != nil && acc.openQty > 1e-10 {
			trades = append(trades, finalizeTrade(acc, true))
		}
	}

	// Enrich closed trades' leverage (and margin) from closed-PnL hints when available.
	applyClosedTradeHints(trades, linkedFills, in.ClosedHints)

	// Allocate FUNDING_FEE income to trades active at each funding settlement timestamp.
	allocateFundingFees(trades, in.Incomes)

	// Compute NetPnL = RealizedPnL - FeeUSD + FundingUSD (FundingUSD is signed cashflow: negative = paid, positive = received).
	for i := range trades {
		trades[i].NetPnL = trades[i].RealizedPnL - trades[i].FeeUSD + trades[i].FundingUSD
	}

	// Match synced trades against accepted DecisionTickets.
	matchTradesToTickets(trades, in.Tickets, in.Decisions)

	// Sort newest-first by entry_time_ms.
	sort.Slice(trades, func(i, j int) bool {
		if trades[i].EntryTimeMs != trades[j].EntryTimeMs {
			return trades[i].EntryTimeMs > trades[j].EntryTimeMs
		}
		return trades[i].ID > trades[j].ID
	})

	return trades, linkedFills
}

func buildSyncedTradeID(exchange string, f cache.TradeFill) string {
	if exchange == "binance" {
		return fmt.Sprintf("trd-%s-%s-%d-%d",
			strings.ToLower(binanceSymbol(f.Symbol)),
			strings.ToLower(f.PositionSide),
			f.TimeMs,
			parseTrailingBinanceTradeID(f.FillID),
		)
	}
	suffix := f.FillID
	if idx := strings.LastIndexByte(suffix, ':'); idx >= 0 && idx+1 < len(suffix) {
		suffix = suffix[idx+1:]
	}
	suffix = sanitizeTradeIDSuffix(suffix)
	if suffix == "" {
		suffix = "0"
	}
	return fmt.Sprintf("trd-%s-%s-%s-%d-%s",
		exchange,
		strings.ToLower(binanceSymbol(f.Symbol)),
		strings.ToLower(f.PositionSide),
		f.TimeMs,
		suffix,
	)
}

func sanitizeTradeIDSuffix(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) > 24 {
		return out[len(out)-24:]
	}
	return out
}

func applyClosedTradeHints(trades []cache.SyncedTrade, linkedFills []cache.TradeFill, hints []ClosedTradeHint) {
	if len(trades) == 0 || len(hints) == 0 {
		return
	}
	tradeIdxByID := make(map[string]int, len(trades))
	for i := range trades {
		tradeIdxByID[trades[i].ID] = i
	}
	closedIdxByOrder := make(map[string]int, len(linkedFills))
	for _, f := range linkedFills {
		if f.OrderID == "" || f.TradeID == "" || strings.HasSuffix(f.FillID, ":flip") {
			continue
		}
		if idx, ok := tradeIdxByID[f.TradeID]; ok && trades[idx].Status == "closed" {
			closedIdxByOrder[f.Symbol+"|"+f.OrderID] = idx
		}
	}

	for _, h := range hints {
		if h.Leverage <= 0 {
			continue
		}
		targetIdx := -1
		if h.OrderID != "" {
			if idx, ok := closedIdxByOrder[h.Symbol+"|"+h.OrderID]; ok {
				targetIdx = idx
			}
		}
		if targetIdx < 0 && h.Symbol != "" && h.UpdatedAtMs > 0 {
			for i := range trades {
				tr := &trades[i]
				if tr.Status != "closed" || tr.Symbol != h.Symbol {
					continue
				}
				if h.UpdatedAtMs >= tr.EntryTimeMs && h.UpdatedAtMs <= tr.ExitTimeMs+5000 {
					targetIdx = i
					break
				}
			}
		}
		if targetIdx >= 0 {
			tr := &trades[targetIdx]
			tr.Leverage = h.Leverage
			if tr.EntryPrice > 0 && tr.Qty > 0 {
				tr.Margin = (tr.EntryPrice * tr.Qty) / tr.Leverage
			}
		}
	}
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
	comm := f.Commission
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
		// Signed cashflow: negative = paid, positive = received.
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

func mergeFillsForSync(exchange string, existingFills, fetchedFills []cache.TradeFill) []cache.TradeFill {
	mergedFillMap := make(map[string]cache.TradeFill, len(existingFills)+len(fetchedFills))
	var flipFills []cache.TradeFill
	for _, f := range existingFills {
		if strings.HasSuffix(f.FillID, ":flip") {
			flipFills = append(flipFills, f)
			continue
		}
		f.Exchange = exchange
		f.TradeID = ""
		mergedFillMap[f.FillID] = f
	}
	for _, f := range flipFills {
		baseID := strings.TrimSuffix(f.FillID, ":flip")
		if base, ok := mergedFillMap[baseID]; ok {
			base.Qty += f.Qty
			base.QuoteQty += f.QuoteQty
			base.Commission += f.Commission
			base.RealizedPnL += f.RealizedPnL
			base.TradeID = ""
			mergedFillMap[baseID] = base
		}
	}
	for _, f := range fetchedFills {
		f.Exchange = exchange
		f.TradeID = ""
		mergedFillMap[f.FillID] = f
	}
	allFills := make([]cache.TradeFill, 0, len(mergedFillMap))
	for _, f := range mergedFillMap {
		allFills = append(allFills, f)
	}
	return allFills
}

func persistAccountSync(
	ctx context.Context,
	store *cache.Store,
	exchange string,
	trades []cache.SyncedTrade,
	linkedFills []cache.TradeFill,
	openPositions []cache.OpenPosition,
	walletBalanceUSD, unrealizedPnLUSD float64,
	nowSec int64,
) error {
	if err := store.ReplaceSyncedTradesForExchange(ctx, exchange, trades); err != nil {
		return err
	}
	if len(linkedFills) > 0 {
		if err := store.DeleteFlipTradeFillsForExchange(ctx, exchange); err != nil {
			return err
		}
		if err := store.UpsertTradeFillsForExchange(ctx, exchange, linkedFills); err != nil {
			return err
		}
	}
	if err := store.ReplaceOpenPositionsForExchange(ctx, exchange, openPositions); err != nil {
		return err
	}
	walletStr := strconv.FormatFloat(walletBalanceUSD, 'f', 6, 64)
	unrealStr := strconv.FormatFloat(unrealizedPnLUSD, 'f', 6, 64)
	lastSyncStr := strconv.FormatInt(nowSec, 10)
	_ = store.SetExchangeMeta(ctx, exchange, "wallet_balance_usd", walletStr)
	_ = store.SetExchangeMeta(ctx, exchange, "unrealized_pnl_usd", unrealStr)
	_ = store.SetExchangeMeta(ctx, exchange, "last_sync_at", lastSyncStr)
	_ = store.SetExchangeMeta(ctx, exchange, "sync_error", "")
	if exchange == "binance" {
		// Mirror legacy unprefixed keys while Wave A deskapi still reads GetAllAccountMeta.
		_ = store.SetAccountMeta(ctx, "wallet_balance_usd", walletStr)
		_ = store.SetAccountMeta(ctx, "unrealized_pnl_usd", unrealStr)
		_ = store.SetAccountMeta(ctx, "last_sync_at", lastSyncStr)
		_ = store.SetAccountMeta(ctx, "sync_error", "")
	}
	return nil
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

func splitTimeChunks(startMs, endMs, maxSpanMs int64) []timeWindow {
	if maxSpanMs <= 0 {
		maxSpanMs = int64(7*24*3600*1000) - 1000
	}
	if endMs <= startMs {
		return []timeWindow{{start: startMs, end: endMs}}
	}
	var chunks []timeWindow
	cur := startMs
	for cur <= endMs {
		winEnd := cur + maxSpanMs
		if winEnd > endMs {
			winEnd = endMs
		}
		chunks = append(chunks, timeWindow{start: cur, end: winEnd})
		if winEnd >= endMs {
			break
		}
		cur = winEnd + 1
	}
	return chunks
}
