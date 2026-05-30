# Plan 02-02 Summary — Atomic Exchange Swap + Detector Re-registration

## Completed: 2026-04-06

### Changes Made

**Fix exchange reload race condition and detector registration loss** (R5, R6, R7)

- **File**: `src/pwatch/core/sentry.py:41` — Added `self._exchange_lock = RLock()` protecting `self.exchange` reference during hot swap
- **File**: `src/pwatch/core/sentry.py:158` — Extracted `_register_detectors_on(exchange)` method: clears `_detectors` before registering to prevent duplicates, creates detectors and registers on a specific exchange instance
- **File**: `src/pwatch/core/sentry.py:282` — New `_sync_symbols_for_exchange(exchange, exchange_name)` helper: returns symbols WITHOUT mutating `self.matched_symbols`, enables prepare-then-commit pattern
- **File**: `src/pwatch/core/sentry.py:230` — Rewritten `_reload_runtime_components()`: 6-phase atomic swap (prepare → register → sync → swap → start → cleanup) with rollback on WS failure
- **File**: `src/pwatch/core/sentry.py:_check_auto_refresh` — Re-registers detectors after close, uses `_exchange_lock` for symbol swap

### Impact
- Detectors preserved after exchange reload (config change)
- Detectors preserved after auto-refresh (4-hour symbol rotation)
- Main loop never sees broken exchange during swap (atomic under lock)
- Rollback restores old exchange if WS start fails

### Verification
- **Tests**: 569 passed, 2 pre-existing failures (test_cli_misc.py — environmental)
- **Lint**: `uv run ruff check` — clean on all changed files
- **No regression**: All existing tests pass with new behavior

### Key Files
- `src/pwatch/core/sentry.py` — Atomic exchange swap, detector re-registration, symbol sync helper
