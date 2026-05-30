# REQUIREMENTS.md — pwatch Bug Fixes & Optimizations

## Source

User request: "这是一个加密货币短线合约监控程序，但是现在很多bug和需要优化的点"

## Current State Summary

- **25 Python modules**, **497 tests**, **14 lint issues** (13 auto-fixable)
- Architecture: PriceSentry (core) → Exchange adapters (strategy) → Detectors (observer) → Telegram notifier
- WebSocket runs in daemon thread, cross-thread via `queue.Queue`
- Config hot-reload via observer pattern
- Single exchange at a time, auto-mode with quality filters

## Requirements (Priority-Ordered)

### R1: Fix `cmd_run` Double Invocation (CRITICAL)
**File**: `src/pwatch/app/cli.py:595-597`
**Problem**: `asyncio.run(run_monitoring())` called twice — after Ctrl+C stops first run, it starts again.
**Expected**: Single call. Remove duplicate line.

### R2: Fix `PriceSentry.__init__` Silent Failure (CRITICAL)
**File**: `src/pwatch/core/sentry.py:88-114`
**Problem**: Early `return` on symbol sync failure doesn't raise exception. Runner continues blindly, then `run()` exits silently because `matched_symbols` is empty.
**Expected**: Raise exception or return error state so caller can handle it.

### R3: Fix WebSocket Reconnection Thread Leak (HIGH)
**File**: `src/pwatch/exchanges/base.py:442`
**Problem**: `check_ws_connection()` calls `start_websocket()` which spawns new daemon thread. Old thread may not have fully exited (`join(timeout=5)` can timeout).
**Expected**: Ensure old thread is fully terminated before spawning new one, or use a reconnection mechanism that doesn't create new threads.

### R4: Fix Config Listener Error Swallowing (HIGH)
**File**: `src/pwatch/core/config_manager.py:472-476`
**Problem**: Listener exceptions are silently `continue`d with no logging. If PriceSentry's listener fails, config updates appear successful but have no effect.
**Expected**: Log exception at minimum. Consider error propagation.

### R5: Fix Exchange Reload Race Condition (HIGH)
**File**: `src/pwatch/core/sentry.py:551-624`
**Problem**: `_reload_runtime_components()` closes exchange, creates new one, syncs symbols, starts WS — all outside lock. During this window, main loop may access stale/broken exchange reference.
**Expected**: Atomic swap pattern — prepare new exchange fully, then atomically replace reference under lock.

### R6: Fix Pre-WS Detector Registration Loss (HIGH)
**File**: `src/pwatch/core/sentry.py:551-624`
**Problem**: After `_reload_runtime_components()` creates new exchange, detectors are NOT re-registered. The new exchange has empty `_detectors` list.
**Expected**: Re-register detectors after exchange reload, or preserve detector registration across exchange instances.

### R7: Auto-refresh WebSocket Restart Loses Detectors (HIGH)
**File**: `src/pwatch/core/sentry.py:810-824`
**Problem**: `_check_auto_refresh()` calls `exchange.close()` + `start_websocket()` but does NOT re-register detectors. Same issue as R6.
**Expected**: Re-register detectors after auto-refresh WS restart.

### R8: Lint Cleanup (MEDIUM)
**Files**: Multiple (14 issues)
**Problem**: 14 ruff lint issues, mostly trivial (unused imports, import ordering, missing newline).
**Expected**: `uv run ruff check . --fix` should resolve 13 of 14.

### R9: Reduce `sentry.py` Complexity (MEDIUM)
**File**: `src/pwatch/core/sentry.py` (852 lines)
**Problem**: Single file handles: initialization, main loop, auto-refresh, config updates, alert formatting, detector setup, WS health checks.
**Expected**: Extract `main_loop.py`, `auto_refresh.py`, `alert_formatter.py` — keep `sentry.py` as coordinator only.

### R10: Pre-parse Cooldown Value (LOW)
**File**: `src/pwatch/core/sentry.py:340-345`
**Problem**: `parse_timeframe()` called on every alert send. Value comes from config string.
**Expected**: Parse once in `_refresh_runtime_settings()`, cache as `self._cooldown_seconds`.

### R11: Improve Anomaly Event Processing Latency (LOW)
**File**: `src/pwatch/core/sentry.py:166-288`
**Problem**: `_process_anomaly_events()` runs once per second in polling loop. Real-time alerts can be delayed up to 1 second.
**Expected**: Use `asyncio.Event` or queue-based async waiting for near-immediate anomaly processing.

### R12: Add Graceful Degradation for Empty Symbol Sets (LOW)
**File**: `src/pwatch/core/sentry.py:673-776`
**Problem**: If `fetch_top_volume_symbols()` fails in auto-mode bootstrap, sentry initialization aborts entirely.
**Expected**: Fall back to last known good symbol set or manual mode symbols before failing.

## Non-Goals (Out of Scope)

- Multi-exchange simultaneous monitoring
- Web UI / dashboard
- Alert history persistence
- Windows support for background mode
- New notification channels (Discord, Slack, etc.)
- Breaking changes to config file format

## Output Contract

- All critical bugs (R1-R4) fixed with tests
- High-priority race conditions (R5-R7) resolved
- Lint clean (`uv run ruff check .` returns 0 errors)
- All 497 existing tests pass
- New tests cover fixed bugs
- No regression in WebSocket stability or alert delivery
