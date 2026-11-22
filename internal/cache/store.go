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
	createStmts := []string{
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
			PRIMARY KEY (symbol, tf, source, time_sec)
		);`,
		`CREATE TABLE IF NOT EXISTS trade_fills (
			fill_id TEXT PRIMARY KEY,
			exchange TEXT NOT NULL DEFAULT 'binance',
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
		`CREATE TABLE IF NOT EXISTS synced_trades (
			id TEXT PRIMARY KEY,
			exchange TEXT NOT NULL DEFAULT 'binance',
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
		`CREATE TABLE IF NOT EXISTS open_positions (
			exchange TEXT NOT NULL DEFAULT 'binance',
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
			stop_loss REAL NOT NULL DEFAULT 0,
			take_profit REAL NOT NULL DEFAULT 0,
			position_value REAL NOT NULL DEFAULT 0,
			break_even_price REAL NOT NULL DEFAULT 0,
			cur_realised_pnl REAL NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY (exchange, symbol, position_side)
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
		`CREATE TABLE IF NOT EXISTS account_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS account_income (
			tran_id TEXT PRIMARY KEY,
			exchange TEXT NOT NULL DEFAULT 'binance',
			symbol TEXT NOT NULL,
			income_type TEXT NOT NULL,
			income REAL NOT NULL,
			asset TEXT NOT NULL,
			time_ms INTEGER NOT NULL
		);`,
	}
	for _, stmt := range createStmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("cache init schema: %w", err)
		}
	}

	if err := s.migrateSchema(ctx); err != nil {
		return err
	}

	indexStmts := []string{
		`CREATE INDEX IF NOT EXISTS idx_klines_sym_tf_src_time ON klines(symbol, tf, source, time_sec);`,
		`CREATE INDEX IF NOT EXISTS idx_trade_fills_sym_time ON trade_fills(symbol, time_ms);`,
		`CREATE INDEX IF NOT EXISTS idx_trade_fills_trade_id ON trade_fills(trade_id);`,
		`CREATE INDEX IF NOT EXISTS idx_trade_fills_ex_sym_time ON trade_fills(exchange, symbol, time_ms);`,
		`CREATE INDEX IF NOT EXISTS idx_trade_fills_ex_trade_id ON trade_fills(exchange, trade_id);`,
		`CREATE INDEX IF NOT EXISTS idx_synced_trades_sym_entry ON synced_trades(symbol, entry_time_ms);`,
		`CREATE INDEX IF NOT EXISTS idx_synced_trades_ex_sym_entry ON synced_trades(exchange, symbol, entry_time_ms);`,
		`CREATE INDEX IF NOT EXISTS idx_chart_drawings_symbol ON chart_drawings(symbol);`,
		`CREATE INDEX IF NOT EXISTS idx_account_income_sym_time ON account_income(symbol, time_ms);`,
		`CREATE INDEX IF NOT EXISTS idx_account_income_ex_sym_time ON account_income(exchange, symbol, time_ms);`,
	}
	for _, stmt := range indexStmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("cache init indexes: %w", err)
		}
	}

	// Seed binance:* account meta from legacy unprefixed keys non-destructively.
	for _, legacyKey := range []string{"wallet_balance_usd", "unrealized_pnl_usd", "last_sync_at", "sync_error"} {
		if _, err := s.db.ExecContext(ctx, `
			INSERT OR IGNORE INTO account_meta (key, value)
			SELECT ?, value FROM account_meta WHERE key = ?
		`, "binance:"+legacyKey, legacyKey); err != nil {
			return fmt.Errorf("cache seed account_meta %s: %w", legacyKey, err)
		}
	}
	return nil
}

type tableColInfo struct {
	name string
	pk   int
}

func (s *Store) tableColumns(ctx context.Context, table string) (map[string]tableColInfo, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s);", table))
	if err != nil {
		return nil, fmt.Errorf("pragma table_info(%s): %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	cols := make(map[string]tableColInfo)
	for rows.Next() {
		var (
			cid     int
			name    string
			colType string
			notNull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			return nil, fmt.Errorf("scan table_info(%s): %w", table, err)
		}
		cols[name] = tableColInfo{name: name, pk: pk}
	}
	return cols, rows.Err()
}

func (s *Store) migrateSchema(ctx context.Context) error {
	// a) klines: migrate PK to (symbol, tf, source, time_sec) if source is not part of PK.
	kCols, err := s.tableColumns(ctx, "klines")
	if err != nil {
		return err
	}
	if srcCol, ok := kCols["source"]; !ok || srcCol.pk == 0 {
		if err := s.migrateKlinesPK(ctx, ok); err != nil {
			return err
		}
	}

	// b) trade_fills, synced_trades, account_income: add exchange column if missing.
	for _, tbl := range []string{"trade_fills", "synced_trades", "account_income"} {
		cols, err := s.tableColumns(ctx, tbl)
		if err != nil {
			return err
		}
		if _, ok := cols["exchange"]; !ok {
			if _, err := s.db.ExecContext(ctx, fmt.Sprintf(
				"ALTER TABLE %s ADD COLUMN exchange TEXT NOT NULL DEFAULT 'binance';", tbl,
			)); err != nil {
				return fmt.Errorf("cache migrate %s add exchange: %w", tbl, err)
			}
		}
	}

	// c) open_positions: migrate PK to (exchange, symbol, position_side).
	opCols, err := s.tableColumns(ctx, "open_positions")
	if err != nil {
		return err
	}
	if exCol, hasEx := opCols["exchange"]; !hasEx || exCol.pk == 0 {
		if err := s.migrateOpenPositionsPK(ctx, hasEx); err != nil {
			return err
		}
		opCols, err = s.tableColumns(ctx, "open_positions")
		if err != nil {
			return err
		}
	}
	// d) open_positions: add Bybit/enriched position fields non-destructively.
	for _, col := range []string{"stop_loss", "take_profit", "position_value", "break_even_price", "cur_realised_pnl"} {
		if _, ok := opCols[col]; !ok {
			if _, err := s.db.ExecContext(ctx, fmt.Sprintf(
				"ALTER TABLE open_positions ADD COLUMN %s REAL NOT NULL DEFAULT 0;", col,
			)); err != nil {
				return fmt.Errorf("cache migrate open_positions add %s: %w", col, err)
			}
		}
	}
	return nil
}

func (s *Store) migrateKlinesPK(ctx context.Context, hasSource bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("cache migrate klines begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE klines_new (
			symbol TEXT NOT NULL,
			tf TEXT NOT NULL,
			time_sec INTEGER NOT NULL,
			open REAL NOT NULL,
			high REAL NOT NULL,
			low REAL NOT NULL,
			close REAL NOT NULL,
			volume REAL NOT NULL,
			quote_vol REAL NOT NULL DEFAULT 0,
			source TEXT NOT NULL DEFAULT 'binance',
			PRIMARY KEY (symbol, tf, source, time_sec)
		);
	`); err != nil {
		return fmt.Errorf("cache migrate klines create: %w", err)
	}

	copySQL := `
		INSERT INTO klines_new (symbol, tf, time_sec, open, high, low, close, volume, quote_vol, source)
		SELECT symbol, tf, time_sec, open, high, low, close, volume, quote_vol,
		       CASE WHEN source = '' THEN 'binance' ELSE source END
		FROM klines;
	`
	if !hasSource {
		copySQL = `
			INSERT INTO klines_new (symbol, tf, time_sec, open, high, low, close, volume, quote_vol, source)
			SELECT symbol, tf, time_sec, open, high, low, close, volume, quote_vol, 'binance'
			FROM klines;
		`
	}
	if _, err := tx.ExecContext(ctx, copySQL); err != nil {
		return fmt.Errorf("cache migrate klines copy: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE klines;`); err != nil {
		return fmt.Errorf("cache migrate klines drop: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE klines_new RENAME TO klines;`); err != nil {
		return fmt.Errorf("cache migrate klines rename: %w", err)
	}
	return tx.Commit()
}

func (s *Store) migrateOpenPositionsPK(ctx context.Context, hasExchange bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("cache migrate open_positions begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE open_positions_new (
			exchange TEXT NOT NULL DEFAULT 'binance',
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
			PRIMARY KEY (exchange, symbol, position_side)
		);
	`); err != nil {
		return fmt.Errorf("cache migrate open_positions create: %w", err)
	}

	exExpr := "'binance'"
	if hasExchange {
		exExpr = "CASE WHEN exchange = '' THEN 'binance' ELSE exchange END"
	}
	copySQL := fmt.Sprintf(`
		INSERT OR REPLACE INTO open_positions_new (
			exchange, symbol, position_side, margin_mode, leverage, qty,
			entry_price, mark_price, unrealized_pnl, liq_price, isolated_wallet, updated_at
		)
		SELECT %s, symbol, position_side, margin_mode, leverage, qty,
			entry_price, mark_price, unrealized_pnl, liq_price, isolated_wallet, updated_at
		FROM open_positions;
	`, exExpr)
	if _, err := tx.ExecContext(ctx, copySQL); err != nil {
		return fmt.Errorf("cache migrate open_positions copy: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE open_positions;`); err != nil {
		return fmt.Errorf("cache migrate open_positions drop: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE open_positions_new RENAME TO open_positions;`); err != nil {
		return fmt.Errorf("cache migrate open_positions rename: %w", err)
	}
	return tx.Commit()
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
		ON CONFLICT(symbol, tf, source, time_sec) DO UPDATE SET
			open = excluded.open,
			high = excluded.high,
			low = excluded.low,
			close = excluded.close,
			volume = excluded.volume,
			quote_vol = excluded.quote_vol
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
		ON CONFLICT(symbol, tf, source, time_sec) DO UPDATE SET
			open = excluded.open,
			high = excluded.high,
			low = excluded.low,
			close = excluded.close,
			volume = excluded.volume,
			quote_vol = excluded.quote_vol
	`, symbol, tf, ts, c.Open, c.High, c.Low, c.Close, c.Volume, c.QuoteVol, source)
	return err
}

// GetKlines returns up to limit candles for (symbol, tf), strictly earlier than
// beforeSec if beforeSec > 0, sorted ascending by time_sec.
func (s *Store) GetKlines(ctx context.Context, symbol, tf string, limit int, beforeSec int64) ([]types.Candle, error) {
	return s.GetKlinesForSource(ctx, symbol, tf, "", limit, beforeSec)
}

// GetKlinesForSource returns up to limit candles for (symbol, tf, source), strictly
// earlier than beforeSec if beforeSec > 0, sorted ascending by time_sec.
func (s *Store) GetKlinesForSource(ctx context.Context, symbol, tf, source string, limit int, beforeSec int64) ([]types.Candle, error) {
	if s == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 300
	}
	clauses := []string{"symbol = ?", "tf = ?"}
	args := []any{symbol, tf}
	if source != "" {
		clauses = append(clauses, "source = ?")
		args = append(args, source)
	}
	if beforeSec > 0 {
		clauses = append(clauses, "time_sec < ?")
		args = append(args, beforeSec)
	}
	q := `SELECT time_sec, open, high, low, close, volume, quote_vol FROM klines WHERE ` + strings.Join(clauses, " AND ")
	if source == "" {
		q += ` GROUP BY time_sec`
	}
	q += ` ORDER BY time_sec DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanCandlesDescReversed(rows)
}

// GetKlinesUpTo returns up to limit candles with time_sec <= maxSec (zero-lookahead),
// sorted ascending by time_sec.
func (s *Store) GetKlinesUpTo(ctx context.Context, symbol, tf string, maxSec int64, limit int) ([]types.Candle, error) {
	return s.GetKlinesUpToForSource(ctx, symbol, tf, "", maxSec, limit)
}

// GetKlinesUpToForSource returns up to limit candles for source with time_sec <= maxSec
// (zero-lookahead), sorted ascending by time_sec.
func (s *Store) GetKlinesUpToForSource(ctx context.Context, symbol, tf, source string, maxSec int64, limit int) ([]types.Candle, error) {
	if s == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 300
	}
	clauses := []string{"symbol = ?", "tf = ?", "time_sec <= ?"}
	args := []any{symbol, tf, maxSec}
	if source != "" {
		clauses = append(clauses, "source = ?")
		args = append(args, source)
	}
	q := `SELECT time_sec, open, high, low, close, volume, quote_vol FROM klines WHERE ` + strings.Join(clauses, " AND ")
	if source == "" {
		q += ` GROUP BY time_sec`
	}
	q += ` ORDER BY time_sec DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanCandlesDescReversed(rows)
}

// GetKlinesRange returns all candles in [fromSec, toSec] inclusive, sorted ascending.
func (s *Store) GetKlinesRange(ctx context.Context, symbol, tf string, fromSec, toSec int64) ([]types.Candle, error) {
	return s.GetKlinesRangeForSource(ctx, symbol, tf, "", fromSec, toSec)
}

// GetKlinesRangeForSource returns all candles for source in [fromSec, toSec] inclusive,
// sorted ascending.
func (s *Store) GetKlinesRangeForSource(ctx context.Context, symbol, tf, source string, fromSec, toSec int64) ([]types.Candle, error) {
	if s == nil {
		return nil, nil
	}
	clauses := []string{"symbol = ?", "tf = ?", "time_sec >= ?", "time_sec <= ?"}
	args := []any{symbol, tf, fromSec, toSec}
	if source != "" {
		clauses = append(clauses, "source = ?")
		args = append(args, source)
	}
	q := `SELECT time_sec, open, high, low, close, volume, quote_vol FROM klines WHERE ` + strings.Join(clauses, " AND ")
	if source == "" {
		q += ` GROUP BY time_sec`
	}
	q += ` ORDER BY time_sec ASC`

	rows, err := s.db.QueryContext(ctx, q, args...)
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
	return s.LatestKlineTimeForSource(ctx, symbol, tf, "")
}

// LatestKlineTimeForSource returns the latest cached time_sec for (symbol, tf, source), or 0 if empty.
func (s *Store) LatestKlineTimeForSource(ctx context.Context, symbol, tf, source string) (int64, error) {
	if s == nil {
		return 0, nil
	}
	var ts sql.NullInt64
	var err error
	if source != "" {
		err = s.db.QueryRowContext(ctx, `
			SELECT MAX(time_sec) FROM klines WHERE symbol = ? AND tf = ? AND source = ?
		`, symbol, tf, source).Scan(&ts)
	} else {
		err = s.db.QueryRowContext(ctx, `
			SELECT MAX(time_sec) FROM klines WHERE symbol = ? AND tf = ?
		`, symbol, tf).Scan(&ts)
	}
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
	return s.ListCachedSymbolsForSource(ctx, "")
}

// ListCachedSymbolsForSource returns distinct symbols present in the klines cache for source.
func (s *Store) ListCachedSymbolsForSource(ctx context.Context, source string) ([]string, error) {
	if s == nil {
		return nil, nil
	}
	var (
		rows *sql.Rows
		err  error
	)
	if source != "" {
		rows, err = s.db.QueryContext(ctx, `SELECT DISTINCT symbol FROM klines WHERE source = ? ORDER BY symbol ASC`, source)
	} else {
		rows, err = s.db.QueryContext(ctx, `SELECT DISTINCT symbol FROM klines ORDER BY symbol ASC`)
	}
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

func normalizeCacheExchange(exchange, fallback string) string {
	ex := strings.ToLower(strings.TrimSpace(exchange))
	if ex != "" {
		return ex
	}
	fb := strings.ToLower(strings.TrimSpace(fallback))
	if fb != "" {
		return fb
	}
	return "binance"
}

// UpsertTradeFills persists or updates fills in desktop-cache.db.
func (s *Store) UpsertTradeFills(ctx context.Context, fills []TradeFill) error {
	return s.upsertTradeFills(ctx, "", fills)
}

// UpsertTradeFillsForExchange persists or updates fills scoped to exchange in desktop-cache.db.
func (s *Store) UpsertTradeFillsForExchange(ctx context.Context, exchange string, fills []TradeFill) error {
	return s.upsertTradeFills(ctx, normalizeCacheExchange(exchange, ""), fills)
}

func (s *Store) upsertTradeFills(ctx context.Context, forcedExchange string, fills []TradeFill) error {
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
			fill_id, exchange, order_id, symbol, side, position_side,
			price, qty, quote_qty, commission, commission_asset,
			realized_pnl, time_ms, trade_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(fill_id) DO UPDATE SET
			exchange = excluded.exchange,
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
		ex := forcedExchange
		if ex == "" {
			ex = normalizeCacheExchange(f.Exchange, "binance")
		}
		if _, err := stmt.ExecContext(ctx,
			f.FillID, ex, f.OrderID, f.Symbol, f.Side, f.PositionSide,
			f.Price, f.Qty, f.QuoteQty, f.Commission, f.CommissionAsset,
			f.RealizedPnL, f.TimeMs, f.TradeID,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteFlipTradeFillsForExchange removes derived ":flip" fill rows for exchange before re-linking.
func (s *Store) DeleteFlipTradeFillsForExchange(ctx context.Context, exchange string) error {
	if s == nil {
		return nil
	}
	ex := normalizeCacheExchange(exchange, "")
	_, err := s.db.ExecContext(ctx, `DELETE FROM trade_fills WHERE exchange = ? AND fill_id LIKE '%:flip'`, ex)
	return err
}

// ListTradeFills returns cached fills ordered ascending by time_ms.
// If symbol is non-empty, filters by symbol; if maxTimeMs > 0, filters time_ms <= maxTimeMs.
func (s *Store) ListTradeFills(ctx context.Context, symbol string, maxTimeMs int64) ([]TradeFill, error) {
	return s.listTradeFills(ctx, "", symbol, maxTimeMs)
}

// ListTradeFillsForExchange returns cached fills for exchange ordered ascending by time_ms.
func (s *Store) ListTradeFillsForExchange(ctx context.Context, exchange, symbol string, maxTimeMs int64) ([]TradeFill, error) {
	return s.listTradeFills(ctx, normalizeCacheExchange(exchange, ""), symbol, maxTimeMs)
}

func (s *Store) listTradeFills(ctx context.Context, exchange, symbol string, maxTimeMs int64) ([]TradeFill, error) {
	if s == nil {
		return nil, nil
	}
	var (
		clauses []string
		args    []any
	)
	if exchange != "" {
		clauses = append(clauses, "exchange = ?")
		args = append(args, exchange)
	}
	if symbol != "" {
		clauses = append(clauses, "symbol = ?")
		args = append(args, symbol)
	}
	if maxTimeMs > 0 {
		clauses = append(clauses, "time_ms <= ?")
		args = append(args, maxTimeMs)
	}
	q := `SELECT exchange, fill_id, order_id, symbol, side, position_side, price, qty, quote_qty,
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
			&f.Exchange, &f.FillID, &f.OrderID, &f.Symbol, &f.Side, &f.PositionSide,
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
		SELECT exchange, fill_id, order_id, symbol, side, position_side, price, qty, quote_qty,
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
			&f.Exchange, &f.FillID, &f.OrderID, &f.Symbol, &f.Side, &f.PositionSide,
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
	return s.latestFillTimeMs(ctx, "", symbol)
}

// LatestFillTimeMsForExchange returns the max time_ms in trade_fills for exchange and optional symbol.
func (s *Store) LatestFillTimeMsForExchange(ctx context.Context, exchange, symbol string) (int64, error) {
	return s.latestFillTimeMs(ctx, normalizeCacheExchange(exchange, ""), symbol)
}

func (s *Store) latestFillTimeMs(ctx context.Context, exchange, symbol string) (int64, error) {
	if s == nil {
		return 0, nil
	}
	var (
		clauses []string
		args    []any
	)
	if exchange != "" {
		clauses = append(clauses, "exchange = ?")
		args = append(args, exchange)
	}
	if symbol != "" {
		clauses = append(clauses, "symbol = ?")
		args = append(args, symbol)
	}
	q := `SELECT MAX(time_ms) FROM trade_fills`
	if len(clauses) > 0 {
		q += " WHERE " + strings.Join(clauses, " AND ")
	}
	var ts sql.NullInt64
	if err := s.db.QueryRowContext(ctx, q, args...).Scan(&ts); err != nil {
		return 0, err
	}
	if !ts.Valid {
		return 0, nil
	}
	return ts.Int64, nil
}

// UpsertIncomeRecords persists raw income entries.
func (s *Store) UpsertIncomeRecords(ctx context.Context, recs []IncomeRecord) error {
	return s.upsertIncomeRecords(ctx, "", recs)
}

// UpsertIncomeRecordsForExchange persists raw income entries scoped to exchange.
func (s *Store) UpsertIncomeRecordsForExchange(ctx context.Context, exchange string, records []IncomeRecord) error {
	return s.upsertIncomeRecords(ctx, normalizeCacheExchange(exchange, ""), records)
}

func (s *Store) upsertIncomeRecords(ctx context.Context, forcedExchange string, recs []IncomeRecord) error {
	if s == nil || len(recs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO account_income (tran_id, exchange, symbol, income_type, income, asset, time_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tran_id) DO UPDATE SET
			exchange = excluded.exchange,
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
		ex := forcedExchange
		if ex == "" {
			ex = normalizeCacheExchange(r.Exchange, "binance")
		}
		if _, err := stmt.ExecContext(ctx, r.TranID, ex, r.Symbol, r.IncomeType, r.Income, r.Asset, r.TimeMs); err != nil {
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
			SELECT exchange, tran_id, symbol, income_type, income, asset, time_ms
			FROM account_income WHERE income_type = ? ORDER BY time_ms ASC, tran_id ASC
		`, incomeType)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT exchange, tran_id, symbol, income_type, income, asset, time_ms
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
		if err := rows.Scan(&r.Exchange, &r.TranID, &r.Symbol, &r.IncomeType, &r.Income, &r.Asset, &r.TimeMs); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListIncomeRecordsForExchange returns cached income records for exchange (and optional symbol)
// ordered ascending by time_ms.
func (s *Store) ListIncomeRecordsForExchange(ctx context.Context, exchange, symbol string) ([]IncomeRecord, error) {
	if s == nil {
		return nil, nil
	}
	ex := normalizeCacheExchange(exchange, "")
	var (
		rows *sql.Rows
		err  error
	)
	if symbol != "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT exchange, tran_id, symbol, income_type, income, asset, time_ms
			FROM account_income WHERE exchange = ? AND symbol = ? ORDER BY time_ms ASC, tran_id ASC
		`, ex, symbol)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT exchange, tran_id, symbol, income_type, income, asset, time_ms
			FROM account_income WHERE exchange = ? ORDER BY time_ms ASC, tran_id ASC
		`, ex)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []IncomeRecord
	for rows.Next() {
		var r IncomeRecord
		if err := rows.Scan(&r.Exchange, &r.TranID, &r.Symbol, &r.IncomeType, &r.Income, &r.Asset, &r.TimeMs); err != nil {
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

// LatestIncomeTimeMsForExchange returns the max time_ms in account_income for exchange.
func (s *Store) LatestIncomeTimeMsForExchange(ctx context.Context, exchange string) (int64, error) {
	if s == nil {
		return 0, nil
	}
	ex := normalizeCacheExchange(exchange, "")
	var ts sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(time_ms) FROM account_income WHERE exchange = ?`, ex).Scan(&ts); err != nil {
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
			id, exchange, symbol, direction, position_side, margin_mode, leverage,
			entry_price, exit_price, qty, entry_time_ms, exit_time_ms,
			realized_pnl, fee_usd, funding_usd, net_pnl, margin, liq_price,
			status, linked_ticket_id, gate_code
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	for _, t := range trades {
		ex := normalizeCacheExchange(t.Exchange, "binance")
		if _, err := stmt.ExecContext(ctx,
			t.ID, ex, t.Symbol, t.Direction, t.PositionSide, t.MarginMode, t.Leverage,
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
				fill_id, exchange, order_id, symbol, side, position_side,
				price, qty, quote_qty, commission, commission_asset,
				realized_pnl, time_ms, trade_id
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return err
		}
		defer func() { _ = fStmt.Close() }()
		for _, f := range fills {
			ex := normalizeCacheExchange(f.Exchange, "binance")
			if _, err := fStmt.ExecContext(ctx,
				f.FillID, ex, f.OrderID, f.Symbol, f.Side, f.PositionSide,
				f.Price, f.Qty, f.QuoteQty, f.Commission, f.CommissionAsset,
				f.RealizedPnL, f.TimeMs, f.TradeID,
			); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

// ReplaceSyncedTradesForExchange atomically replaces synced_trades rows belonging to exchange.
func (s *Store) ReplaceSyncedTradesForExchange(ctx context.Context, exchange string, trades []SyncedTrade) error {
	if s == nil {
		return nil
	}
	ex := normalizeCacheExchange(exchange, "")
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM synced_trades WHERE exchange = ?`, ex); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO synced_trades (
			id, exchange, symbol, direction, position_side, margin_mode, leverage,
			entry_price, exit_price, qty, entry_time_ms, exit_time_ms,
			realized_pnl, fee_usd, funding_usd, net_pnl, margin, liq_price,
			status, linked_ticket_id, gate_code
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	for _, t := range trades {
		if _, err := stmt.ExecContext(ctx,
			t.ID, ex, t.Symbol, t.Direction, t.PositionSide, t.MarginMode, t.Leverage,
			t.EntryPrice, t.ExitPrice, t.Qty, t.EntryTimeMs, t.ExitTimeMs,
			t.RealizedPnL, t.FeeUSD, t.FundingUSD, t.NetPnL, t.Margin, t.LiqPrice,
			t.Status, t.LinkedTicketID, t.GateCode,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListSyncedTrades returns synced trades ordered newest-first by entry_time_ms.
func (s *Store) ListSyncedTrades(ctx context.Context, symbol string) ([]SyncedTrade, error) {
	return s.listSyncedTrades(ctx, "", symbol)
}

// ListSyncedTradesForExchange returns synced trades for exchange ordered newest-first by entry_time_ms.
func (s *Store) ListSyncedTradesForExchange(ctx context.Context, exchange, symbol string) ([]SyncedTrade, error) {
	return s.listSyncedTrades(ctx, normalizeCacheExchange(exchange, ""), symbol)
}

func (s *Store) listSyncedTrades(ctx context.Context, exchange, symbol string) ([]SyncedTrade, error) {
	if s == nil {
		return nil, nil
	}
	var (
		clauses []string
		args    []any
	)
	if exchange != "" {
		clauses = append(clauses, "exchange = ?")
		args = append(args, exchange)
	}
	if symbol != "" {
		clauses = append(clauses, "symbol = ?")
		args = append(args, symbol)
	}
	q := `SELECT exchange, id, symbol, direction, position_side, margin_mode, leverage,
		entry_price, exit_price, qty, entry_time_ms, exit_time_ms,
		realized_pnl, fee_usd, funding_usd, net_pnl, margin, liq_price,
		status, linked_ticket_id, gate_code
		FROM synced_trades`
	if len(clauses) > 0 {
		q += " WHERE " + strings.Join(clauses, " AND ")
	}
	q += " ORDER BY entry_time_ms DESC, id DESC"

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]SyncedTrade, 0)
	for rows.Next() {
		var t SyncedTrade
		if err := rows.Scan(
			&t.Exchange, &t.ID, &t.Symbol, &t.Direction, &t.PositionSide, &t.MarginMode, &t.Leverage,
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
	return s.replaceOpenPositions(ctx, "", positions)
}

// ReplaceOpenPositionsForExchange atomically replaces open_positions rows belonging to exchange.
func (s *Store) ReplaceOpenPositionsForExchange(ctx context.Context, exchange string, positions []OpenPosition) error {
	return s.replaceOpenPositions(ctx, normalizeCacheExchange(exchange, ""), positions)
}

func (s *Store) replaceOpenPositions(ctx context.Context, forcedExchange string, positions []OpenPosition) error {
	if s == nil {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if forcedExchange != "" {
		if _, err := tx.ExecContext(ctx, `DELETE FROM open_positions WHERE exchange = ?`, forcedExchange); err != nil {
			return err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `DELETE FROM open_positions`); err != nil {
			return err
		}
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO open_positions (
			exchange, symbol, position_side, margin_mode, leverage, qty,
			entry_price, mark_price, unrealized_pnl, liq_price, isolated_wallet,
			stop_loss, take_profit, position_value, break_even_price, cur_realised_pnl,
			updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	for _, p := range positions {
		ex := forcedExchange
		if ex == "" {
			ex = normalizeCacheExchange(p.Exchange, "binance")
		}
		if _, err := stmt.ExecContext(ctx,
			ex, p.Symbol, p.PositionSide, p.MarginMode, p.Leverage, p.Qty,
			p.EntryPrice, p.MarkPrice, p.UnrealizedPnL, p.LiqPrice, p.IsolatedWallet,
			p.StopLoss, p.TakeProfit, p.PositionValue, p.BreakEvenPrice, p.CurRealisedPnL,
			p.UpdatedAt,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListOpenPositions returns all cached open positions ordered by symbol.
func (s *Store) ListOpenPositions(ctx context.Context) ([]OpenPosition, error) {
	return s.listOpenPositions(ctx, "")
}

// ListOpenPositionsForExchange returns cached open positions for exchange ordered by symbol.
func (s *Store) ListOpenPositionsForExchange(ctx context.Context, exchange string) ([]OpenPosition, error) {
	return s.listOpenPositions(ctx, normalizeCacheExchange(exchange, ""))
}

func (s *Store) listOpenPositions(ctx context.Context, exchange string) ([]OpenPosition, error) {
	if s == nil {
		return nil, nil
	}
	var (
		rows *sql.Rows
		err  error
	)
	if exchange != "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT exchange, symbol, position_side, margin_mode, leverage, qty,
				entry_price, mark_price, unrealized_pnl, liq_price, isolated_wallet,
				stop_loss, take_profit, position_value, break_even_price, cur_realised_pnl,
				updated_at
			FROM open_positions
			WHERE exchange = ?
			ORDER BY symbol ASC, position_side ASC
		`, exchange)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT exchange, symbol, position_side, margin_mode, leverage, qty,
				entry_price, mark_price, unrealized_pnl, liq_price, isolated_wallet,
				stop_loss, take_profit, position_value, break_even_price, cur_realised_pnl,
				updated_at
			FROM open_positions
			ORDER BY exchange ASC, symbol ASC, position_side ASC
		`)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]OpenPosition, 0)
	for rows.Next() {
		var p OpenPosition
		if err := rows.Scan(
			&p.Exchange, &p.Symbol, &p.PositionSide, &p.MarginMode, &p.Leverage, &p.Qty,
			&p.EntryPrice, &p.MarkPrice, &p.UnrealizedPnL, &p.LiqPrice, &p.IsolatedWallet,
			&p.StopLoss, &p.TakeProfit, &p.PositionValue, &p.BreakEvenPrice, &p.CurRealisedPnL,
			&p.UpdatedAt,
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

// SetExchangeMeta writes an exchange-scoped entry under "<exchange>:<key>" in account_meta.
func (s *Store) SetExchangeMeta(ctx context.Context, exchange, key, value string) error {
	if s == nil {
		return nil
	}
	ex := normalizeCacheExchange(exchange, "")
	return s.SetAccountMeta(ctx, ex+":"+key, value)
}

// GetExchangeMeta reads "<exchange>:<key>" from account_meta, falling back to the
// legacy unprefixed key when exchange is "binance".
func (s *Store) GetExchangeMeta(ctx context.Context, exchange, key string) (string, error) {
	if s == nil {
		return "", nil
	}
	ex := normalizeCacheExchange(exchange, "")
	var val string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM account_meta WHERE key = ?`, ex+":"+key).Scan(&val)
	if err == nil {
		return val, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	if ex == "binance" {
		return s.GetAccountMeta(ctx, key)
	}
	return "", nil
}

// GetAllExchangeMeta returns exchange-scoped meta keyed by unprefixed key
// (e.g. "wallet_balance_usd", "unrealized_pnl_usd", "last_sync_at", "sync_error"),
// falling back to legacy unprefixed account keys when exchange is "binance".
func (s *Store) GetAllExchangeMeta(ctx context.Context, exchange string) (map[string]string, error) {
	out := make(map[string]string)
	if s == nil {
		return out, nil
	}
	ex := normalizeCacheExchange(exchange, "")
	if ex == "binance" {
		legacyRows, err := s.db.QueryContext(ctx, `
			SELECT key, value FROM account_meta
			WHERE instr(key, ':') = 0 AND substr(key, 1, 8) != 'setting_'
		`)
		if err != nil {
			return nil, err
		}
		for legacyRows.Next() {
			var k, v string
			if err := legacyRows.Scan(&k, &v); err != nil {
				_ = legacyRows.Close()
				return nil, err
			}
			out[k] = v
		}
		if err := legacyRows.Err(); err != nil {
			_ = legacyRows.Close()
			return nil, err
		}
		_ = legacyRows.Close()
	}

	prefix := ex + ":"
	rows, err := s.db.QueryContext(ctx, `
		SELECT key, value FROM account_meta
		WHERE substr(key, 1, ?) = ?
	`, len(prefix), prefix)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[strings.TrimPrefix(k, prefix)] = v
	}
	return out, rows.Err()
}
