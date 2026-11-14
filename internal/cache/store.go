// Package cache implements the derived SQLite cache (desktop-cache.db) in WAL mode.
// trading-journal.jsonl remains the authoritative source of truth for sessions,
// tickets, human decisions, gate annotations, and counterfactual outcomes.
package cache

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
	_ "modernc.org/sqlite" // Pure Go SQLite driver (CGO_ENABLED=0 compatible).
)

// DBPath returns the canonical derived SQLite cache path next to trading-journal.jsonl.
func DBPath(cfg types.StorageConfig) string {
	return filepath.Join(storage.Dir(cfg), "desktop-cache.db")
}

// Store wraps a WAL-mode SQLite connection for derived desktop cache tables.
type Store struct {
	db   *sql.DB
	path string
}

// OpenFromStorageConfig opens or creates desktop-cache.db under storage.Dir(cfg).
func OpenFromStorageConfig(cfg types.StorageConfig) (*Store, error) {
	return Open(DBPath(cfg))
}

// Open opens or creates the SQLite cache database at path with WAL mode enabled.
func Open(path string) (*Store, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("cache mkdir %s: %w", dir, err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("cache open %s: %w", path, err)
	}
	// Single writer/connection in-process avoids SQLITE_BUSY under concurrent goroutines.
	db.SetMaxOpenConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA synchronous=NORMAL;",
		"PRAGMA busy_timeout=5000;",
		"PRAGMA foreign_keys=ON;",
		"PRAGMA temp_store=MEMORY;",
		"PRAGMA cache_size=-8000;",
	}
	for _, p := range pragmas {
		if _, err := db.ExecContext(ctx, p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("cache pragma %q: %w", p, err)
		}
	}

	s := &Store{db: db, path: path}
	if err := s.initSchema(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Path returns the filesystem path of desktop-cache.db.
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Close closes the underlying SQLite database handle.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) initSchema(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS klines (
			symbol TEXT NOT NULL,
			tf TEXT NOT NULL,
			time_sec INTEGER NOT NULL,
			open REAL NOT NULL,
			high REAL NOT NULL,
			low REAL NOT NULL,
			close REAL NOT NULL,
			volume REAL NOT NULL,
			quote_vol REAL NOT NULL DEFAULT 0,
			source TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (symbol, tf, time_sec)
		);`,
		`CREATE TABLE IF NOT EXISTS trade_fills (
			fill_id TEXT PRIMARY KEY,
			order_id TEXT NOT NULL,
			symbol TEXT NOT NULL,
			side TEXT NOT NULL,
			position_side TEXT NOT NULL,
			price REAL NOT NULL,
			qty REAL NOT NULL,
			quote_qty REAL NOT NULL,
			commission REAL NOT NULL,
			commission_asset TEXT NOT NULL,
			realized_pnl REAL NOT NULL,
			time_ms INTEGER NOT NULL,
			trade_id TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE INDEX IF NOT EXISTS idx_trade_fills_sym_time ON trade_fills(symbol, time_ms);`,
		`CREATE INDEX IF NOT EXISTS idx_trade_fills_trade_id ON trade_fills(trade_id);`,
		`CREATE TABLE IF NOT EXISTS synced_trades (
			id TEXT PRIMARY KEY,
			symbol TEXT NOT NULL,
			direction TEXT NOT NULL,
			position_side TEXT NOT NULL,
			margin_mode TEXT NOT NULL,
			leverage REAL NOT NULL DEFAULT 1,
			entry_price REAL NOT NULL,
			exit_price REAL NOT NULL,
			qty REAL NOT NULL,
			entry_time_ms INTEGER NOT NULL,
			exit_time_ms INTEGER NOT NULL,
			realized_pnl REAL NOT NULL,
			fee_usd REAL NOT NULL,
			funding_usd REAL NOT NULL,
			net_pnl REAL NOT NULL,
			margin REAL NOT NULL,
			liq_price REAL NOT NULL DEFAULT 0,
			status TEXT NOT NULL,
			linked_ticket_id TEXT NOT NULL DEFAULT '',
			gate_code TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE INDEX IF NOT EXISTS idx_synced_trades_sym_entry ON synced_trades(symbol, entry_time_ms);`,
		`CREATE TABLE IF NOT EXISTS open_positions (
			symbol TEXT NOT NULL,
			position_side TEXT NOT NULL,
			margin_mode TEXT NOT NULL,
			leverage REAL NOT NULL,
			qty REAL NOT NULL,
			entry_price REAL NOT NULL,
			mark_price REAL NOT NULL,
			unrealized_pnl REAL NOT NULL,
			liq_price REAL NOT NULL,
			isolated_wallet REAL NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY (symbol, position_side)
		);`,
		`CREATE TABLE IF NOT EXISTS chart_drawings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			symbol TEXT NOT NULL,
			kind TEXT NOT NULL,
			points_json TEXT NOT NULL,
			label TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_chart_drawings_symbol ON chart_drawings(symbol);`,
		`CREATE TABLE IF NOT EXISTS account_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS account_income (
			tran_id TEXT PRIMARY KEY,
			symbol TEXT NOT NULL,
			income_type TEXT NOT NULL,
			income REAL NOT NULL,
			asset TEXT NOT NULL,
			time_ms INTEGER NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_account_income_sym_time ON account_income(symbol, time_ms);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("cache init schema: %w", err)
		}
	}
	return nil
}

// -----------------------------------------------------------------------------
// Klines
// -----------------------------------------------------------------------------

// UpsertKlines inserts or replaces candles in a single transaction.
func (s *Store) UpsertKlines(ctx context.Context, symbol, tf, source string, candles []types.Candle) error {
	if s == nil || len(candles) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO klines (symbol, tf, time_sec, open, high, low, close, volume, quote_vol, source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(symbol, tf, time_sec) DO UPDATE SET
			open = excluded.open,
			high = excluded.high,
			low = excluded.low,
			close = excluded.close,
			volume = excluded.volume,
			quote_vol = excluded.quote_vol,
			source = excluded.source
	`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	for _, c := range candles {
		ts := normalizeSec(c.Timestamp)
		if ts <= 0 {
			continue
		}
		if _, err := stmt.ExecContext(ctx, symbol, tf, ts, c.Open, c.High, c.Low, c.Close, c.Volume, c.QuoteVol, source); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// UpsertKline inserts or updates a single candle.
func (s *Store) UpsertKline(ctx context.Context, symbol, tf, source string, c types.Candle) error {
	if s == nil {
		return nil
	}
	ts := normalizeSec(c.Timestamp)
	if ts <= 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO klines (symbol, tf, time_sec, open, high, low, close, volume, quote_vol, source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(symbol, tf, time_sec) DO UPDATE SET
			open = excluded.open,
			high = excluded.high,
			low = excluded.low,
			close = excluded.close,
			volume = excluded.volume,
			quote_vol = excluded.quote_vol,
			source = excluded.source
	`, symbol, tf, ts, c.Open, c.High, c.Low, c.Close, c.Volume, c.QuoteVol, source)
	return err
}

// GetKlines returns up to limit candles for (symbol, tf), strictly earlier than
// beforeSec if beforeSec > 0, sorted ascending by time_sec.
func (s *Store) GetKlines(ctx context.Context, symbol, tf string, limit int, beforeSec int64) ([]types.Candle, error) {
	if s == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 300
	}
	var (
		rows *sql.Rows
		err  error
	)
	if beforeSec > 0 {
		rows, err = s.db.QueryContext(ctx, `
			SELECT time_sec, open, high, low, close, volume, quote_vol
			FROM klines
			WHERE symbol = ? AND tf = ? AND time_sec < ?
			ORDER BY time_sec DESC
			LIMIT ?
		`, symbol, tf, beforeSec, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT time_sec, open, high, low, close, volume, quote_vol
			FROM klines
			WHERE symbol = ? AND tf = ?
			ORDER BY time_sec DESC
			LIMIT ?
		`, symbol, tf, limit)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanCandlesDescReversed(rows)
}

// GetKlinesUpTo returns up to limit candles with time_sec <= maxSec (zero-lookahead),
// sorted ascending by time_sec.
func (s *Store) GetKlinesUpTo(ctx context.Context, symbol, tf string, maxSec int64, limit int) ([]types.Candle, error) {
	if s == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 300
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT time_sec, open, high, low, close, volume, quote_vol
		FROM klines
		WHERE symbol = ? AND tf = ? AND time_sec <= ?
		ORDER BY time_sec DESC
		LIMIT ?
	`, symbol, tf, maxSec, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanCandlesDescReversed(rows)
}

// GetKlinesRange returns all candles in [fromSec, toSec] inclusive, sorted ascending.
func (s *Store) GetKlinesRange(ctx context.Context, symbol, tf string, fromSec, toSec int64) ([]types.Candle, error) {
	if s == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT time_sec, open, high, low, close, volume, quote_vol
		FROM klines
		WHERE symbol = ? AND tf = ? AND time_sec >= ? AND time_sec <= ?
		ORDER BY time_sec ASC
	`, symbol, tf, fromSec, toSec)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []types.Candle
	for rows.Next() {
		var c types.Candle
		if err := rows.Scan(&c.Timestamp, &c.Open, &c.High, &c.Low, &c.Close, &c.Volume, &c.QuoteVol); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// LatestKlineTime returns the latest cached time_sec for (symbol, tf), or 0 if empty.
func (s *Store) LatestKlineTime(ctx context.Context, symbol, tf string) (int64, error) {
	if s == nil {
		return 0, nil
	}
	var ts sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT MAX(time_sec) FROM klines WHERE symbol = ? AND tf = ?
	`, symbol, tf).Scan(&ts)
	if err != nil {
		return 0, err
	}
	if !ts.Valid {
		return 0, nil
	}
	return ts.Int64, nil
}

// ListCachedSymbols returns distinct symbols present in the klines cache.
func (s *Store) ListCachedSymbols(ctx context.Context) ([]string, error) {
	if s == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT symbol FROM klines ORDER BY symbol ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var sym string
		if err := rows.Scan(&sym); err != nil {
			return nil, err
		}
		out = append(out, sym)
	}
	return out, rows.Err()
}

func scanCandlesDescReversed(rows *sql.Rows) ([]types.Candle, error) {
	var desc []types.Candle
	for rows.Next() {
		var c types.Candle
		if err := rows.Scan(&c.Timestamp, &c.Open, &c.High, &c.Low, &c.Close, &c.Volume, &c.QuoteVol); err != nil {
			return nil, err
		}
		desc = append(desc, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(desc)-1; i < j; i, j = i+1, j-1 {
		desc[i], desc[j] = desc[j], desc[i]
	}
	return desc, nil
}

func normalizeSec(ts int64) int64 {
	if ts > 1_000_000_000_000 {
		return ts / 1000
	}
	return ts
}

// -----------------------------------------------------------------------------
// Trade fills, synced trades, open positions, income
// -----------------------------------------------------------------------------

// UpsertTradeFills persists or updates fills in desktop-cache.db.
func (s *Store) UpsertTradeFills(ctx context.Context, fills []TradeFill) error {
	if s == nil || len(fills) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO trade_fills (
			fill_id, order_id, symbol, side, position_side,
			price, qty, quote_qty, commission, commission_asset,
			realized_pnl, time_ms, trade_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(fill_id) DO UPDATE SET
			order_id = excluded.order_id,
			symbol = excluded.symbol,
			side = excluded.side,
			position_side = excluded.position_side,
			price = excluded.price,
			qty = excluded.qty,
			quote_qty = excluded.quote_qty,
			commission = excluded.commission,
			commission_asset = excluded.commission_asset,
			realized_pnl = excluded.realized_pnl,
			time_ms = excluded.time_ms,
			trade_id = excluded.trade_id
	`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	for _, f := range fills {
		if _, err := stmt.ExecContext(ctx,
			f.FillID, f.OrderID, f.Symbol, f.Side, f.PositionSide,
			f.Price, f.Qty, f.QuoteQty, f.Commission, f.CommissionAsset,
			f.RealizedPnL, f.TimeMs, f.TradeID,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListTradeFills returns cached fills ordered ascending by time_ms.
// If symbol is non-empty, filters by symbol; if maxTimeMs > 0, filters time_ms <= maxTimeMs.
func (s *Store) ListTradeFills(ctx context.Context, symbol string, maxTimeMs int64) ([]TradeFill, error) {
	if s == nil {
		return nil, nil
	}
	var (
		clauses []string
		args    []any
	)
	if symbol != "" {
		clauses = append(clauses, "symbol = ?")
		args = append(args, symbol)
	}
	if maxTimeMs > 0 {
		clauses = append(clauses, "time_ms <= ?")
		args = append(args, maxTimeMs)
	}
	q := `SELECT fill_id, order_id, symbol, side, position_side, price, qty, quote_qty,
		commission, commission_asset, realized_pnl, time_ms, trade_id
		FROM trade_fills`
	if len(clauses) > 0 {
		q += " WHERE " + strings.Join(clauses, " AND ")
	}
	q += " ORDER BY time_ms ASC, fill_id ASC"

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]TradeFill, 0)
	for rows.Next() {
		var f TradeFill
		if err := rows.Scan(
			&f.FillID, &f.OrderID, &f.Symbol, &f.Side, &f.PositionSide,
			&f.Price, &f.Qty, &f.QuoteQty, &f.Commission, &f.CommissionAsset,
			&f.RealizedPnL, &f.TimeMs, &f.TradeID,
		); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ListTradeFillsByTradeID returns fills linked to a synced trade ID.
func (s *Store) ListTradeFillsByTradeID(ctx context.Context, tradeID string) ([]TradeFill, error) {
	if s == nil || tradeID == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT fill_id, order_id, symbol, side, position_side, price, qty, quote_qty,
			commission, commission_asset, realized_pnl, time_ms, trade_id
		FROM trade_fills
		WHERE trade_id = ?
		ORDER BY time_ms ASC, fill_id ASC
	`, tradeID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]TradeFill, 0)
	for rows.Next() {
		var f TradeFill
		if err := rows.Scan(
			&f.FillID, &f.OrderID, &f.Symbol, &f.Side, &f.PositionSide,
			&f.Price, &f.Qty, &f.QuoteQty, &f.Commission, &f.CommissionAsset,
			&f.RealizedPnL, &f.TimeMs, &f.TradeID,
		); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// LatestFillTimeMs returns the max time_ms in trade_fills for symbol (or overall if symbol=="").
func (s *Store) LatestFillTimeMs(ctx context.Context, symbol string) (int64, error) {
	if s == nil {
		return 0, nil
	}
	var ts sql.NullInt64
	var err error
	if symbol != "" {
		err = s.db.QueryRowContext(ctx, `SELECT MAX(time_ms) FROM trade_fills WHERE symbol = ?`, symbol).Scan(&ts)
	} else {
		err = s.db.QueryRowContext(ctx, `SELECT MAX(time_ms) FROM trade_fills`).Scan(&ts)
	}
	if err != nil {
		return 0, err
	}
	if !ts.Valid {
		return 0, nil
	}
	return ts.Int64, nil
}

// UpsertIncomeRecords persists raw Binance income entries.
func (s *Store) UpsertIncomeRecords(ctx context.Context, recs []IncomeRecord) error {
	if s == nil || len(recs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO account_income (tran_id, symbol, income_type, income, asset, time_ms)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(tran_id) DO UPDATE SET
			symbol = excluded.symbol,
			income_type = excluded.income_type,
			income = excluded.income,
			asset = excluded.asset,
			time_ms = excluded.time_ms
	`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	for _, r := range recs {
		if _, err := stmt.ExecContext(ctx, r.TranID, r.Symbol, r.IncomeType, r.Income, r.Asset, r.TimeMs); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListIncomeRecords returns cached income records ordered ascending by time_ms.
func (s *Store) ListIncomeRecords(ctx context.Context, incomeType string) ([]IncomeRecord, error) {
	if s == nil {
		return nil, nil
	}
	var (
		rows *sql.Rows
		err  error
	)
	if incomeType != "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT tran_id, symbol, income_type, income, asset, time_ms
			FROM account_income WHERE income_type = ? ORDER BY time_ms ASC, tran_id ASC
		`, incomeType)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT tran_id, symbol, income_type, income, asset, time_ms
			FROM account_income ORDER BY time_ms ASC, tran_id ASC
		`)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []IncomeRecord
	for rows.Next() {
		var r IncomeRecord
		if err := rows.Scan(&r.TranID, &r.Symbol, &r.IncomeType, &r.Income, &r.Asset, &r.TimeMs); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// LatestIncomeTimeMs returns the max time_ms in account_income.
func (s *Store) LatestIncomeTimeMs(ctx context.Context) (int64, error) {
	if s == nil {
		return 0, nil
	}
	var ts sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(time_ms) FROM account_income`).Scan(&ts); err != nil {
		return 0, err
	}
	if !ts.Valid {
		return 0, nil
	}
	return ts.Int64, nil
}

// ReplaceAllSyncedTrades atomically replaces synced_trades and fill trade_id links.
func (s *Store) ReplaceAllSyncedTrades(ctx context.Context, trades []SyncedTrade, fills []TradeFill) error {
	if s == nil {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM synced_trades`); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO synced_trades (
			id, symbol, direction, position_side, margin_mode, leverage,
			entry_price, exit_price, qty, entry_time_ms, exit_time_ms,
			realized_pnl, fee_usd, funding_usd, net_pnl, margin, liq_price,
			status, linked_ticket_id, gate_code
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	for _, t := range trades {
		if _, err := stmt.ExecContext(ctx,
			t.ID, t.Symbol, t.Direction, t.PositionSide, t.MarginMode, t.Leverage,
			t.EntryPrice, t.ExitPrice, t.Qty, t.EntryTimeMs, t.ExitTimeMs,
			t.RealizedPnL, t.FeeUSD, t.FundingUSD, t.NetPnL, t.Margin, t.LiqPrice,
			t.Status, t.LinkedTicketID, t.GateCode,
		); err != nil {
			return err
		}
	}

	if len(fills) > 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM trade_fills`); err != nil {
			return err
		}
		fStmt, err := tx.PrepareContext(ctx, `
			INSERT INTO trade_fills (
				fill_id, order_id, symbol, side, position_side,
				price, qty, quote_qty, commission, commission_asset,
				realized_pnl, time_ms, trade_id
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return err
		}
		defer func() { _ = fStmt.Close() }()
		for _, f := range fills {
			if _, err := fStmt.ExecContext(ctx,
				f.FillID, f.OrderID, f.Symbol, f.Side, f.PositionSide,
				f.Price, f.Qty, f.QuoteQty, f.Commission, f.CommissionAsset,
				f.RealizedPnL, f.TimeMs, f.TradeID,
			); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

// ListSyncedTrades returns synced trades ordered newest-first by entry_time_ms.
func (s *Store) ListSyncedTrades(ctx context.Context, symbol string) ([]SyncedTrade, error) {
	if s == nil {
		return nil, nil
	}
	var (
		rows *sql.Rows
		err  error
	)
	base := `SELECT id, symbol, direction, position_side, margin_mode, leverage,
		entry_price, exit_price, qty, entry_time_ms, exit_time_ms,
		realized_pnl, fee_usd, funding_usd, net_pnl, margin, liq_price,
		status, linked_ticket_id, gate_code
		FROM synced_trades`
	if symbol != "" {
		rows, err = s.db.QueryContext(ctx, base+" WHERE symbol = ? ORDER BY entry_time_ms DESC, id DESC", symbol)
	} else {
		rows, err = s.db.QueryContext(ctx, base+" ORDER BY entry_time_ms DESC, id DESC")
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]SyncedTrade, 0)
	for rows.Next() {
		var t SyncedTrade
		if err := rows.Scan(
			&t.ID, &t.Symbol, &t.Direction, &t.PositionSide, &t.MarginMode, &t.Leverage,
			&t.EntryPrice, &t.ExitPrice, &t.Qty, &t.EntryTimeMs, &t.ExitTimeMs,
			&t.RealizedPnL, &t.FeeUSD, &t.FundingUSD, &t.NetPnL, &t.Margin, &t.LiqPrice,
			&t.Status, &t.LinkedTicketID, &t.GateCode,
		); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ReplaceOpenPositions atomically updates the live open_positions snapshot table.
func (s *Store) ReplaceOpenPositions(ctx context.Context, positions []OpenPosition) error {
	if s == nil {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM open_positions`); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO open_positions (
			symbol, position_side, margin_mode, leverage, qty,
			entry_price, mark_price, unrealized_pnl, liq_price, isolated_wallet, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	for _, p := range positions {
		if _, err := stmt.ExecContext(ctx,
			p.Symbol, p.PositionSide, p.MarginMode, p.Leverage, p.Qty,
			p.EntryPrice, p.MarkPrice, p.UnrealizedPnL, p.LiqPrice, p.IsolatedWallet, p.UpdatedAt,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListOpenPositions returns all cached open positions ordered by symbol.
func (s *Store) ListOpenPositions(ctx context.Context) ([]OpenPosition, error) {
	if s == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT symbol, position_side, margin_mode, leverage, qty,
			entry_price, mark_price, unrealized_pnl, liq_price, isolated_wallet, updated_at
		FROM open_positions
		ORDER BY symbol ASC, position_side ASC
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]OpenPosition, 0)
	for rows.Next() {
		var p OpenPosition
		if err := rows.Scan(
			&p.Symbol, &p.PositionSide, &p.MarginMode, &p.Leverage, &p.Qty,
			&p.EntryPrice, &p.MarkPrice, &p.UnrealizedPnL, &p.LiqPrice, &p.IsolatedWallet, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// -----------------------------------------------------------------------------
// Chart drawings
// -----------------------------------------------------------------------------

// CreateDrawing inserts a new chart drawing and returns it with its assigned ID.
func (s *Store) CreateDrawing(ctx context.Context, d ChartDrawing) (ChartDrawing, error) {
	if s == nil {
		return d, fmt.Errorf("cache store is nil")
	}
	if d.Points == nil {
		d.Points = []ChartDrawingPoint{}
	}
	now := time.Now().Unix()
	if d.CreatedAt <= 0 {
		d.CreatedAt = now
	}
	if d.UpdatedAt <= 0 {
		d.UpdatedAt = now
	}
	ptsJSON, err := json.Marshal(d.Points)
	if err != nil {
		return d, err
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO chart_drawings (symbol, kind, points_json, label, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, d.Symbol, d.Kind, string(ptsJSON), d.Label, d.CreatedAt, d.UpdatedAt)
	if err != nil {
		return d, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return d, err
	}
	d.ID = id
	return d, nil
}

// UpdateDrawing updates the anchor points (and optional label) of a persisted drawing.
func (s *Store) UpdateDrawing(ctx context.Context, id int64, points []ChartDrawingPoint, label *string) (ChartDrawing, error) {
	if s == nil {
		return ChartDrawing{}, fmt.Errorf("cache store is nil")
	}
	if points == nil {
		points = []ChartDrawingPoint{}
	}
	ptsJSON, err := json.Marshal(points)
	if err != nil {
		return ChartDrawing{}, err
	}
	now := time.Now().Unix()

	var res sql.Result
	if label != nil {
		res, err = s.db.ExecContext(ctx, `
			UPDATE chart_drawings
			SET points_json = ?, label = ?, updated_at = ?
			WHERE id = ?
		`, string(ptsJSON), *label, now, id)
	} else {
		res, err = s.db.ExecContext(ctx, `
			UPDATE chart_drawings
			SET points_json = ?, updated_at = ?
			WHERE id = ?
		`, string(ptsJSON), now, id)
	}
	if err != nil {
		return ChartDrawing{}, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return ChartDrawing{}, err
	}
	if affected == 0 {
		return ChartDrawing{}, sql.ErrNoRows
	}

	var (
		d          ChartDrawing
		rawPtsJSON string
	)
	err = s.db.QueryRowContext(ctx, `
		SELECT id, symbol, kind, points_json, label, created_at, updated_at
		FROM chart_drawings
		WHERE id = ?
	`, id).Scan(&d.ID, &d.Symbol, &d.Kind, &rawPtsJSON, &d.Label, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return ChartDrawing{}, err
	}
	d.Points = []ChartDrawingPoint{}
	_ = json.Unmarshal([]byte(rawPtsJSON), &d.Points)
	if d.Points == nil {
		d.Points = []ChartDrawingPoint{}
	}
	return d, nil
}

// ListDrawings returns drawings for symbol. When maxTimeSec > 0 (zero-lookahead replay),
// only drawings whose points ALL satisfy point.Time <= maxTimeSec are returned.
func (s *Store) ListDrawings(ctx context.Context, symbol string, maxTimeSec int64) ([]ChartDrawing, error) {
	if s == nil {
		return []ChartDrawing{}, nil
	}
	var (
		rows *sql.Rows
		err  error
	)
	if symbol != "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, symbol, kind, points_json, label, created_at, updated_at
			FROM chart_drawings
			WHERE symbol = ?
			ORDER BY id ASC
		`, symbol)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, symbol, kind, points_json, label, created_at, updated_at
			FROM chart_drawings
			ORDER BY id ASC
		`)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]ChartDrawing, 0)
	for rows.Next() {
		var (
			d       ChartDrawing
			ptsJSON string
		)
		if err := rows.Scan(&d.ID, &d.Symbol, &d.Kind, &ptsJSON, &d.Label, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		d.Points = []ChartDrawingPoint{}
		_ = json.Unmarshal([]byte(ptsJSON), &d.Points)
		if d.Points == nil {
			d.Points = []ChartDrawingPoint{}
		}
		if maxTimeSec > 0 && !drawingAllPointsAtOrBefore(d, maxTimeSec) {
			continue
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func drawingAllPointsAtOrBefore(d ChartDrawing, maxTimeSec int64) bool {
	if len(d.Points) == 0 {
		return d.CreatedAt <= maxTimeSec
	}
	for _, p := range d.Points {
		if p.Time > maxTimeSec {
			return false
		}
	}
	return true
}

// DeleteDrawing deletes a single drawing by ID.
func (s *Store) DeleteDrawing(ctx context.Context, id int64) error {
	if s == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM chart_drawings WHERE id = ?`, id)
	return err
}

// DeleteDrawingsForSymbol deletes all drawings for a symbol.
func (s *Store) DeleteDrawingsForSymbol(ctx context.Context, symbol string) error {
	if s == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM chart_drawings WHERE symbol = ?`, symbol)
	return err
}

// -----------------------------------------------------------------------------
// Account meta
// -----------------------------------------------------------------------------

// SetAccountMeta sets a key/value pair in account_meta.
func (s *Store) SetAccountMeta(ctx context.Context, key, value string) error {
	if s == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO account_meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value)
	return err
}

// GetAccountMeta returns a single key from account_meta (empty string if missing).
func (s *Store) GetAccountMeta(ctx context.Context, key string) (string, error) {
	if s == nil {
		return "", nil
	}
	var val string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM account_meta WHERE key = ?`, key).Scan(&val)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return val, err
}

// GetAllAccountMeta returns all key/value entries in account_meta.
func (s *Store) GetAllAccountMeta(ctx context.Context) (map[string]string, error) {
	out := make(map[string]string)
	if s == nil {
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM account_meta`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}
