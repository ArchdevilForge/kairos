# pm-bot Phase 3: Daemon, Persistence & Automation Research

## Query
Python daemon patterns, SQLite persistence, and 24/7 automated trading infrastructure for pm-bot.

---

## 1. Python Daemon/Scheduler Approaches

### Comparison Table

| Approach | Complexity | Best For | Async Support | Persistence |
|----------|-----------|----------|---------------|-------------|
| **APScheduler 4.x (AsyncScheduler)** | Medium | Recurring scheduled jobs with async | Native async (AsyncScheduler) | SQLAlchemy/Memory data stores |
| **asyncio loop (manual)** | Low | Simple polling loop (what watch.py already does) | Native | Manual (code your own) |
| **systemd timer** | Low | System-level cron replacement | No (runs script) | External |

### Recommendation: asyncio loop with signal handling

For pm-bot, the **simplest approach is a plain asyncio loop** — it matches the existing `watch.py` pattern (lines 40-103 in `pm_bot/cli/watch.py`) which already runs `while True: ... await asyncio.sleep(interval)`. Adding APScheduler adds dependency complexity for a single recurring task. The bot's core loop is: poll markets → run strategies → place orders → sleep. This is naturally an asyncio loop.

**APScheduler 4.x** is worth considering only if you need:
- Multiple independent schedules (e.g., market scan every 5min, P&L rollup every hour, config reload at midnight)
- Persistent job state across restarts
- Misfire handling (catching up on missed runs)

The `AsyncScheduler` flavor works natively with asyncio:
```python
from apscheduler import AsyncScheduler
from apscheduler.triggers.interval import IntervalTrigger

async with AsyncScheduler() as scheduler:
    scheduler.add_schedule(trade_cycle, IntervalTrigger(minutes=5), id="trade_cycle", conflict_policy=ConflictPolicy.replace)
    await scheduler.run_until_stopped()
```

### Running as Background Daemon

**Option A: systemd service (recommended for production)**
```ini
# /etc/systemd/system/pm-bot.service
[Unit]
Description=PM-Bot Automated Trading
After=network.target

[Service]
Type=simple
User=pm-bot
WorkingDirectory=/opt/pm-bot
ExecStart=/opt/pm-bot/.venv/bin/pm-bot run
Restart=on-failure
RestartSec=30
Environment=POLY_PK=<key>
Environment=PM_BOT_CONFIG=/opt/pm-bot/config.toml

# Signal handling
KillSignal=SIGTERM
TimeoutStopSec=60

[Install]
WantedBy=multi-user.target
```

**Option B: nohup / screen / tmux (quick dev)**
```bash
nohup pm-bot run --daemon &
```

**Option C: Python daemonization (python-daemon lib)**
- Overkill for this use case; systemd handles daemonization better.

### Signal Handling

Python's `signal` module handles SIGTERM/SIGINT/SIGUSR1. Key facts:
- Signal handlers execute only in the **main thread** of the main interpreter
- `signal.signal(signalnum, handler)` sets a handler; the handler receives `(signum, frame)`
- For asyncio, use `loop.add_signal_handler()` which integrates with the event loop
- **Important**: `threading.Lock` must NOT be used inside signal handlers (deadlock risk)

**Pattern for pm-bot:**
```python
import signal
import asyncio

shutdown_event = asyncio.Event()

def _sigterm_handler():
    log.info("sigterm_received", action="graceful_shutdown")
    shutdown_event.set()

async def main():
    loop = asyncio.get_running_loop()
    loop.add_signal_handler(signal.SIGTERM, _sigterm_handler)
    loop.add_signal_handler(signal.SIGINT, _sigterm_handler)

    # SIGUSR1 → reload config
    def _sigusr1_handler():
        log.info("sigusr1_received", action="reload_config")
        # Set a flag; actual reload happens in the main loop
    loop.add_signal_handler(signal.SIGUSR1, _sigusr1_handler)

    while not shutdown_event.is_set():
        await trade_cycle()
        try:
            await asyncio.wait_for(shutdown_event.wait(), timeout=interval)
        except asyncio.TimeoutError:
            pass

    # Graceful shutdown
    await graceful_shutdown()
```

### Health Check / Heartbeat

pm-bot already has a `ClobTrader.start_heartbeat()` (lines 241-262 in `pm_bot/core/clob.py`) using a background thread that POSTs to Polymarket's heartbeat endpoint every 5 seconds.

For self-monitoring, add a **file-based heartbeat**:
```python
# In trade cycle, after each successful run:
Path("/tmp/pm-bot.heartbeat").write_text(json.dumps({
    "ts": time.time(),
    "status": "running",
    "last_cycle": cycle_start,
    "daily_spent": daily_spent,
    "open_orders": len(open_orders),
}))
```

Or a lightweight HTTP endpoint using `aiohttp`:
```python
from aiohttp import web

async def health_handler(request):
    return web.json_response({"status": "ok", "uptime": uptime, "daily_spent": daily_spent})

app = web.Application()
app.router.add_get("/health", health_handler)
# Run alongside trade loop
```

---

## 2. SQLite for Trade Logging

### aiosqlite vs sqlite3 (sync)

| Factor | sqlite3 (sync) | aiosqlite |
|--------|---------------|-----------|
| Blocking | Yes (blocks event loop) | No (runs in background thread) |
| Complexity | Simpler API | Slight async wrapper overhead |
| Performance | Same underlying SQLite | Same (one thread per connection) |
| At pm-bot scale (~100 trades/day) | **Perfectly fine** with `run_in_executor` | Also fine, marginally cleaner |

**Verdict**: For pm-bot's scale (few dozen trades per day), `sqlite3` with `asyncio.to_thread()` is simpler and avoids an extra dependency. If the project already uses aiosqlite, use it. At this scale, there's no meaningful performance difference.

### Schema Design

```sql
-- Schema version tracking (simple migration approach)
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Core trades table
CREATE TABLE IF NOT EXISTS trades (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id TEXT NOT NULL UNIQUE,           -- Polymarket order ID
    market_id TEXT NOT NULL,                 -- Token/condition ID
    event_id TEXT,                           -- Parent event ID
    strategy TEXT NOT NULL,                  -- gopfan2, sum_arb, ladder, narrow_no, airport_arb
    side TEXT NOT NULL,                      -- BUY or SELL
    direction TEXT NOT NULL,                 -- YES or NO
    price REAL NOT NULL,                     -- Limit price
    amount_usd REAL NOT NULL,                -- Dollar amount
    size REAL NOT NULL,                      -- Number of shares
    fill_status TEXT NOT NULL DEFAULT 'open', -- open, partial, filled, cancelled
    filled_size REAL NOT NULL DEFAULT 0,
    fill_price REAL,                         -- Average fill price
    city TEXT,                               -- Weather city
    temp_label TEXT,                         -- e.g. "25-30°C"
    edge REAL,                               -- Expected edge at time of order
    reasoning TEXT,                          -- Strategy reasoning
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    filled_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_trades_order_id ON trades(order_id);
CREATE INDEX IF NOT EXISTS idx_trades_market_id ON trades(market_id);
CREATE INDEX IF NOT EXISTS idx_trades_strategy ON trades(strategy);
CREATE INDEX IF NOT EXISTS idx_trades_created_at ON trades(created_at);
CREATE INDEX IF NOT EXISTS idx_trades_fill_status ON trades(fill_status);

-- Position tracking (materialized view of current exposure)
CREATE TABLE IF NOT EXISTS positions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    market_id TEXT NOT NULL,
    strategy TEXT NOT NULL,
    city TEXT,
    direction TEXT NOT NULL,                 -- YES or NO
    net_size REAL NOT NULL DEFAULT 0,        -- Positive = long, negative = short
    avg_entry_price REAL NOT NULL DEFAULT 0,
    unrealized_pnl REAL NOT NULL DEFAULT 0,
    realized_pnl REAL NOT NULL DEFAULT 0,
    last_price REAL,                         -- Last known market price
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(market_id, strategy, direction)
);

CREATE INDEX IF NOT EXISTS idx_positions_market ON positions(market_id);
CREATE INDEX IF NOT EXISTS idx_positions_city ON positions(city);

-- Daily P&L tracking
CREATE TABLE IF NOT EXISTS daily_pnl (
    date TEXT NOT NULL,                      -- YYYY-MM-DD in UTC
    strategy TEXT NOT NULL DEFAULT 'all',
    realized_pnl REAL NOT NULL DEFAULT 0,
    unrealized_pnl REAL NOT NULL DEFAULT 0,
    total_spent REAL NOT NULL DEFAULT 0,
    total_fees REAL NOT NULL DEFAULT 0,
    trade_count INTEGER NOT NULL DEFAULT 0,
    win_count INTEGER NOT NULL DEFAULT 0,
    loss_count INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (date, strategy)
);

-- State persistence (daily counters, config hash, etc.)
CREATE TABLE IF NOT EXISTS bot_state (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,                     -- JSON-encoded value
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
```

### Migration Approach

Simple versioned schema — one `.sql` file per version, applied sequentially:

```python
SCHEMA_DIR = Path(__file__).parent / "migrations"

def get_current_version(db: sqlite3.Connection) -> int:
    try:
        row = db.execute("SELECT MAX(version) FROM schema_version").fetchone()
        return row[0] if row[0] else 0
    except sqlite3.OperationalError:
        return 0

def run_migrations(db: sqlite3.Connection) -> None:
    current = get_current_version(db)
    for sql_file in sorted(SCHEMA_DIR.glob("*.sql")):
        version = int(sql_file.stem.split("_")[0])  # e.g. "001_initial.sql"
        if version > current:
            db.executescript(sql_file.read_text())
            db.execute("INSERT INTO schema_version (version) VALUES (?)", (version,))
            db.commit()
            log.info("migration_applied", version=version)
```

### Position Tracking Queries

```sql
-- Current exposure per market
SELECT market_id, direction, net_size, avg_entry_price, unrealized_pnl
FROM positions
WHERE net_size != 0;

-- Current exposure per city
SELECT city, direction, SUM(net_size) as total_size, SUM(unrealized_pnl) as total_pnl
FROM positions
WHERE net_size != 0
GROUP BY city, direction;

-- Current exposure per strategy
SELECT strategy, direction, SUM(net_size) as total_size, SUM(unrealized_pnl) as total_pnl
FROM positions
WHERE net_size != 0
GROUP BY strategy, direction;

-- Daily P&L
SELECT date, strategy, realized_pnl, trade_count, win_count, loss_count
FROM daily_pnl
WHERE date = date('now');

-- Win rate over last 7 days
SELECT
    SUM(win_count) * 100.0 / NULLIF(SUM(win_count + loss_count), 0) as win_rate,
    SUM(realized_pnl) as total_pnl,
    SUM(trade_count) as total_trades
FROM daily_pnl
WHERE date >= date('now', '-7 days');
```

---

## 3. State Persistence Patterns

### What to Track

| State | Storage | Reset Logic |
|-------|---------|-------------|
| `daily_spent` | `bot_state` table | Reset to 0 at midnight UTC |
| `open_orders` | `trades` table (fill_status='open') | Reconcile with Polymarket API on restart |
| `realized_pnl` | `daily_pnl` table | Per-day, rolls forward |
| `positions` | `positions` table | Update on each fill, never reset |
| `last_cycle_ts` | `bot_state` table | Updated after each cycle |
| `config_hash` | `bot_state` table | Compare on reload |

### Daily Counter Reset

```python
def _check_daily_reset(state: dict) -> None:
    """Reset daily counters at midnight UTC."""
    today = datetime.now(timezone.utc).strftime("%Y-%m-%d")
    if state.get("last_date") != today:
        log.info("daily_reset", old_date=state.get("last_date"), new_date=today)
        state["daily_spent"] = 0.0
        state["last_date"] = today
        state["trade_count_today"] = 0
        state["win_count_today"] = 0
        state["loss_count_today"] = 0
```

### Crash Recovery

On restart, the bot must:

1. **Reconcile open orders**: Query Polymarket API for all open orders, compare with `trades` table
2. **Update fill status**: For each order in DB with `fill_status='open'`, check actual status via API
3. **Recalculate daily_spent**: Sum `amount_usd` for today's filled/partial orders
4. **Resume positions**: Positions table is authoritative; update from recent fills

```python
async def recover_state(trader: ClobTrader, db: sqlite3.Connection) -> None:
    """Called once on startup to reconcile state after crash."""
    # 1. Get actual open orders from API
    api_orders = trader.get_open_orders()
    api_order_ids = {str(o.get("id", "")) for o in api_orders}

    # 2. Get orders marked as open in DB
    db_orders = db.execute(
        "SELECT order_id FROM trades WHERE fill_status = 'open'"
    ).fetchall()
    db_order_ids = {row[0] for row in db_orders}

    # 3. Reconcile: orders in DB but not on API → likely filled or cancelled
    filled_or_cancelled = db_order_ids - api_order_ids
    for oid in filled_or_cancelled:
        status = trader.get_order_status(oid)
        if status:
            new_fill = "filled" if status.get("status") == "filled" else "cancelled"
            db.execute(
                "UPDATE trades SET fill_status = ?, updated_at = datetime('now') WHERE order_id = ?",
                (new_fill, oid),
            )

    # 4. Orders on API but not in DB → orphaned, cancel them
    orphaned = api_order_ids - db_order_ids
    for oid in orphaned:
        trader.cancel_order(oid)
        log.warning("orphaned_order_cancelled", order_id=oid)

    # 5. Recalculate daily spent from today's fills
    today = datetime.now(timezone.utc).strftime("%Y-%m-%d")
    row = db.execute(
        "SELECT COALESCE(SUM(amount_usd), 0) FROM trades WHERE date(created_at) = ? AND fill_status IN ('filled', 'partial')",
        (today,),
    ).fetchone()
    db.execute(
        "INSERT OR REPLACE INTO bot_state (key, value) VALUES (?, ?)",
        ("daily_spent", json.dumps({"amount": row[0], "date": today})),
    )
    db.commit()
```

### Idempotency: Avoiding Double-Ordering

Key mechanisms:

1. **UNIQUE constraint on `order_id`**: The `trades` table has `order_id TEXT NOT NULL UNIQUE`, so attempting to insert a duplicate will raise `sqlite3.IntegrityError`
2. **Pre-trade check**: Before placing any order, check DB for existing open order on same market + direction + strategy
3. **Order ID from API**: Always use the order_id returned by `ClobTrader.place_limit_buy()` as the canonical ID

```python
def is_duplicate_order(db: sqlite3.Connection, market_id: str, strategy: str, direction: str) -> bool:
    """Check if there's already an open order for this market+strategy+direction."""
    row = db.execute(
        "SELECT COUNT(*) FROM trades WHERE market_id = ? AND strategy = ? AND direction = ? AND fill_status = 'open'",
        (market_id, strategy, direction),
    ).fetchone()
    return row[0] > 0
```

---

## 4. Graceful Shutdown for Trading Bots

### Shutdown Sequence

```
SIGTERM received
  ↓
1. Set shutdown flag (stop accepting new orders)
  ↓
2. Cancel all open orders via ClobTrader.cancel_all_orders()
  ↓
3. Persist final state to SQLite (daily_spent, positions, last_cycle_ts)
  ↓
4. Wait for pending fills (with timeout, e.g. 10s)
   - Poll trader.get_order_status() for any recently placed orders
   - If order fills within timeout, update DB
  ↓
5. Send shutdown notification via Discord/Telegram
  ↓
6. Close DB connection
  ↓
7. Exit
```

### Implementation Pattern

```python
async def graceful_shutdown(trader: ClobTrader, db: sqlite3.Connection, config: dict) -> None:
    log.info("graceful_shutdown_start")

    # 1. Cancel all open orders
    try:
        result = trader.cancel_all_orders()
        log.info("all_orders_cancelled", result=result)
    except Exception as e:
        log.error("cancel_all_failed", error=str(e))

    # 2. Persist final state
    _persist_state(db)
    db.commit()
    log.info("state_persisted")

    # 3. Wait for pending fills (up to 10s)
    pending = db.execute(
        "SELECT order_id FROM trades WHERE fill_status = 'partial'"
    ).fetchall()
    if pending:
        log.info("waiting_for_pending_fills", count=len(pending))
        try:
            await asyncio.wait_for(_poll_fills(trader, db, pending), timeout=10)
        except asyncio.TimeoutError:
            log.warning("pending_fill_timeout", remaining=len(pending))

    # 4. Stop heartbeat
    trader.stop_heartbeat()

    # 5. Notify
    from pm_bot.cli.notifications import notify
    await notify(config, "shutdown", "system", "", "", "", 0, 0)

    # 6. Close DB
    db.close()
    log.info("graceful_shutdown_complete")
```

### Existing Infrastructure in pm-bot

- `ClobTrader.cancel_all_orders()` — already exists (line 190-198 in `pm_bot/core/clob.py`)
- `ClobTrader.cancel_order(order_id)` — already exists (line 178-188)
- `ClobTrader.stop_heartbeat()` — already exists (line 247-251)
- `notify()` in `pm_bot/cli/notifications.py` — already sends Discord/Telegram (line 55-73)
- The `trade.py` command already uses `try/finally: trader.stop_heartbeat()` (line 151-152)

---

## 5. Monitoring and Alerting

### Self-Monitoring Metrics

| Metric | Source | Alert Threshold |
|--------|--------|----------------|
| Bot health (heartbeat) | File-based or HTTP | No update in 2x cycle interval |
| Order fill rate | `trades` table | < 30% over last 24h |
| Daily P&L | `daily_pnl` table | Loss exceeds configurable threshold |
| Consecutive losses | `daily_pnl` table | 5+ consecutive losing days |
| API error rate | structlog counters | > 10 errors/hour |
| Open orders count | Polymarket API | > 20 (stale orders) |
| Daily spend vs limit | `bot_state` table | > 80% of max_daily |
| Position concentration | `positions` table | > 50% in single city/strategy |

### Alert Implementation

```python
async def check_alerts(db: sqlite3.Connection, config: dict) -> list[str]:
    alerts = []

    # Consecutive losses
    rows = db.execute("""
        SELECT date, realized_pnl FROM daily_pnl
        ORDER BY date DESC LIMIT 7
    """).fetchall()
    consecutive_losses = 0
    for date, pnl in rows:
        if pnl < 0:
            consecutive_losses += 1
        else:
            break
    if consecutive_losses >= 5:
        alerts.append(f"⚠️ {consecutive_losses} consecutive losing days — consider pausing")

    # High error rate (from recent structlog — would need log aggregator)
    # For now, track API errors in bot_state

    # Daily spend approaching limit
    sizing = get_sizing(config)
    state = _load_state(db)
    if state["daily_spent"] > sizing["max_daily"] * 0.8:
        alerts.append(f"💰 Daily spend ${state['daily_spent']:.2f} > 80% of limit ${sizing['max_daily']:.2f}")

    return alerts
```

### Daily Summary Notification

```python
async def send_daily_summary(db: sqlite3.Connection, config: dict) -> None:
    today = datetime.now(timezone.utc).strftime("%Y-%m-%d")
    row = db.execute("""
        SELECT
            COALESCE(SUM(realized_pnl), 0),
            COALESCE(SUM(trade_count), 0),
            COALESCE(SUM(win_count), 0),
            COALESCE(SUM(loss_count), 0)
        FROM daily_pnl WHERE date = ?
    """, (today,)).fetchone()

    pnl, trades, wins, losses = row
    win_rate = wins * 100.0 / max(trades, 1)

    msg = (
        f"📊 <b>Daily Summary — {today}</b>\n"
        f"  P&L: ${pnl:.2f}\n"
        f"  Trades: {trades} (W:{wins} L:{losses} → {win_rate:.0f}%)\n"
    )

    # Positions
    positions = db.execute("""
        SELECT city, direction, SUM(net_size), SUM(unrealized_pnl)
        FROM positions WHERE net_size != 0
        GROUP BY city, direction
    """).fetchall()
    if positions:
        msg += "  Open Positions:\n"
        for city, direction, size, upnl in positions:
            msg += f"    {city} {direction}: {size:.0f} shares, unrealized ${upnl:.2f}\n"

    await notify(config, "daily_summary", "system", "", "", "", 0, 0)
    # Or directly: await send_discord(webhook, msg); await send_telegram(token, chat_id, msg)
```

---

## Files Found

| File Path | Description |
|-----------|-------------|
| `pm_bot/cli/app.py` | CLI entry point (Typer), all commands defined here |
| `pm_bot/cli/trade.py` | One-shot trade execution with manual confirm |
| `pm_bot/cli/watch.py` | Continuous TUI monitoring (closest to daemon pattern) |
| `pm_bot/cli/orders.py` | Open orders display |
| `pm_bot/cli/notifications.py` | Discord/Telegram notification (already working) |
| `pm_bot/core/clob.py` | ClobTrader class — order placement, cancellation, heartbeat |
| `pm_bot/core/config_loader.py` | Config loading from TOML + env vars |
| `pm_bot/core/polymarket.py` | Market data fetching from Gamma API |
| `pm_bot/core/ws.py` | WebSocket client for real-time prices |
| `pm_bot/core/weather.py` | Weather forecast fetching |
| `pm_bot/models/market.py` | Dataclasses: TemperatureBucket, WeatherEvent, Recommendation, ForecastResult |
| `pm_bot/models/config.py` | City coords, strategy defaults, cache TTL |
| `pm_bot/strategies/base.py` | Strategy classes: Gopfan2, SumArb, Ladder, NarrowNo, AirportArb |
| `config.toml.example` | Config template with [sizing], [notifications], [stations] sections |
| `pyproject.toml` | Python 3.12+, dependencies (no SQLite/DB deps yet) |

## Code Pattern Analysis

### Existing patterns that Phase 3 can build on:

1. **Watch loop** (`watch.py:40-103`): Already has `while True: ... await asyncio.sleep(interval)` pattern — this IS the daemon loop, just needs trade execution added.

2. **ClobTrader heartbeat** (`clob.py:241-262`): Background thread posting heartbeat every 5s — already works, just needs to be started/stopped properly in daemon mode.

3. **Daily spend tracking** (`clob.py:24`): `_daily_spent: float = 0.0` exists but is **in-memory only** — lost on restart. Phase 3 persists this to SQLite.

4. **Sizing checks** (`clob.py:54-61`): `_check_sizing()` already enforces `max_single` and `max_daily` limits.

5. **Notification infrastructure** (`notifications.py`): Discord webhook + Telegram bot already implemented and used in `trade.py:145-148`.

6. **Signal handling**: Currently only `KeyboardInterrupt` in watch.py (line 102-103). Phase 3 needs proper SIGTERM/SIGUSR1.

7. **Graceful shutdown**: `trade.py:151-152` has `try/finally: trader.stop_heartbeat()` — this pattern extends to full shutdown.

## Related Spec Documents

- `.trellis/spec/backend/database-guidelines.md` — Empty template, needs to be filled with SQLite conventions
- `.trellis/spec/backend/error-handling.md` — Defensive fail-safe pattern: catch at I/O boundary, log + continue
- `.trellis/spec/backend/logging-guidelines.md` — structlog with keyword args, debug/info/warning/error levels

## Not Found

- No existing SQLite or database code in the project
- No daemon/signal handling code beyond KeyboardInterrupt
- No state persistence beyond in-memory `_daily_spent`
- No position tracking or P&L calculation
- No migration system
- The `strategies/narrow_no.py` and `strategies/airport_arb.py` files exist but were not read (lazy-loaded in base.py)
