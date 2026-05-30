# Plan 02 Summary — Fix Silent Failure Modes

## Completed: 2026-04-06

### Changes Made

**Fix `PriceSentry.__init__` silent failure on symbol sync** (R2)
- **File**: `src/pwatch/core/sentry.py:98` — `_sync_symbols` failure now raises `RuntimeError` instead of silent `return`
- **File**: `src/pwatch/core/sentry.py:114-117` — empty `matched_symbols` now raises `RuntimeError` instead of silent `return`
- **File**: `src/pwatch/core/sentry.py:133` — `run()` guard changed from early `return` to `assert`
- **Impact**: Users will see clear error messages when symbol sync fails, instead of silent non-functioning monitor

**Fix config listener error swallowing** (R4)
- **File**: `src/pwatch/core/config_manager.py:476` — added `logging.exception()` in `_notify_listeners` except block
- **Impact**: Config listener errors now appear in logs with full traceback, making debugging possible

### Test Updates (consequence of behavioral change)
- `test_init_with_no_matched_symbols` — expects `RuntimeError` instead of silent empty init
- `test_notification_symbols_invalid_fallback` — expects `RuntimeError` instead of silent fallback
- `test_run_with_no_symbols` — tests assertion directly via `object.__new__`
- `test_check_auto_refresh_restarts_websocket_on_symbol_change` — fixed mock scope (patch was ending before `_check_auto_refresh` call)

### Verification
- **Lint**: `uv run ruff check` — passed on changed files
- **Tests**: 487 passed, 0 failed (excluding 1 pre-existing `test_cli_misc.py` failure unrelated to changes)
- **No regression**: All existing tests pass with new behavior

### Key Files
- `src/pwatch/core/sentry.py` — bug fix (raise instead of return)
- `src/pwatch/core/config_manager.py` — bug fix (add error logging)
- `tests/test_core_sentry.py` — test updates for new behavior
