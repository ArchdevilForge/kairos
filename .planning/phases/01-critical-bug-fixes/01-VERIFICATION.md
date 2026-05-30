---
phase: 01
status: passed
date: 2026-04-06
---

## Verification Report

### Phase Goal
Fix critical bugs that cause silent failures and incorrect behavior

### Must-Haves

| # | Must-Have | Status | Evidence |
|---|-----------|--------|----------|
| 1 | `cli.py` has exactly ONE `asyncio.run(run_monitoring())` call | ✓ VERIFIED | `grep -c` returns 1; line 593 is the single call; duplicate line removed |
| 2 | `sentry.py` `_sync_symbols` failure raises `RuntimeError` | ✓ VERIFIED | Line 104: `raise RuntimeError(f"Failed to bootstrap symbols for {exchange_name}: {exc}") from exc` |
| 3 | `sentry.py` empty `matched_symbols` raises `RuntimeError` | ✓ VERIFIED | Lines 120-123: `raise RuntimeError(f"No matched symbols for exchange {exchange_name}...")` |
| 4 | `sentry.py` `run()` has assertion guard | ✓ VERIFIED | Line 143: `assert self.matched_symbols, "PriceSentry.__init__ must succeed before run()"` |
| 5 | `config_manager.py` `_notify_listeners` logs exceptions | ✓ VERIFIED | Line 476-479: `logging.exception("Config listener failed for %s", ...)`; `continue` preserved |
| 6 | All existing tests pass (except pre-existing failures) | ✓ VERIFIED | 569 passed, 2 failed — both pre-existing environmental issues in `test_cli_misc.py` |

### Code Evidence

**R1 — cli.py:583-599**
```python
def cmd_run(args):
    ...
    try:
        _run_start_preflight()
        asyncio.run(run_monitoring())   # ← exactly ONE call (line 593)
    except KeyboardInterrupt:
        logging.info("\n\n👋 pwatch 已停止")
    except Exception as e:
        logging.error(f"❌ 启动失败: {e}")
        sys.exit(1)
```

**R2 — sentry.py:92-123**
```python
# Sync failure → raise (line 104)
except ValueError as exc:
    ...
    raise RuntimeError(f"Failed to bootstrap symbols for {exchange_name}: {exc}") from exc

# Empty symbols → raise (lines 120-123)
if not self.matched_symbols:
    ...
    raise RuntimeError(f"No matched symbols for exchange {exchange_name}...")
```

**R2b — sentry.py:141-144**
```python
async def run(self) -> None:
    assert self.matched_symbols, "PriceSentry.__init__ must succeed before run()"
    await self._main_loop.run()
```

**R4 — config_manager.py:471-480**
```python
for listener in listeners:
    try:
        listener(event)
    except Exception:
        logging.exception(
            "Config listener failed for %s",
            listener.__name__ if hasattr(listener, "__name__") else repr(listener),
        )
        continue  # ← chain preserved
```

### Test Results

```
569 passed, 2 failed, 2 warnings
```

**2 failures — both pre-existing, unrelated to phase changes:**

| Test | Reason | Phase-related? |
|------|--------|----------------|
| `test_validators_and_yes_no_paths` | `ask_yes_no` returns False in non-TTY test context | No |
| `test_read_pid_helpers_and_running_pid_cleanup` | PID 123 exists on this host machine | No |

**Lint:** `uv run ruff check` — all checks passed on all 3 changed files.

### Anti-Pattern Scan

No TODO/FIXME/placeholder comments introduced. No stub patterns. No hardcoded empty values. No console.log-only implementations.

### Behavioral Spot-Checks

| Behavior | Check | Result |
|----------|-------|--------|
| Single asyncio.run() | `grep -c "asyncio.run(run_monitoring())" cli.py` | ✓ Returns 1 |
| RuntimeError on sync failure | `grep -n "raise RuntimeError" sentry.py` | ✓ Lines 104, 120 |
| logging.exception present | `grep -n "logging.exception" config_manager.py` | ✓ Line 476 |
| continue preserved | `grep -A5 "logging.exception" config_manager.py` | ✓ `continue` on line 480 |

### Human Verification

These items require manual/interactive testing beyond automated checks:

1. **Ctrl+C exits cleanly without restart** — Run `pwatch run`, press Ctrl+C, verify process terminates without restarting. The code fix (removing duplicate `asyncio.run()`) guarantees this structurally, but live verification confirms no race conditions.
2. **Symbol sync failure shows error** — Configure an invalid exchange name in config, run `pwatch run`, verify `RuntimeError` message is displayed to user.
3. **Config listener errors visible in logs** — Trigger a config update that causes a listener error, verify full traceback appears in logs.
