---
phase: 01-critical-bug-fixes
plan: 02
type: execute
wave: 1
depends_on: []
files_modified:
  - src/pwatch/core/sentry.py
  - src/pwatch/core/config_manager.py
autonomous: true
requirements:
  - R2
  - R4
user_setup: []

must_haves:
  truths:
    - "Failed symbol sync raises error visible to caller (not silent return)"
    - "Config listener errors are logged with full traceback context"
    - "All existing tests pass after changes"
  artifacts:
    - path: "src/pwatch/core/sentry.py"
      provides: "Symbol sync failure raises exception instead of silent return"
      contains: "raise on sync failure"
      exports: ["PriceSentry.__init__"]
    - path: "src/pwatch/core/config_manager.py"
      provides: "Listener error logging in _notify_listeners"
      contains: "logging.exception or error_handler in except block"
      exports: ["ConfigManager._notify_listeners"]
  key_links:
    - from: "src/pwatch/core/sentry.py"
      to: "src/pwatch/core/config_manager.py"
      via: "config_manager.subscribe(self._enqueue_config_update)"
      pattern: "config_manager\\.subscribe"
    - from: "src/pwatch/core/sentry.py __init__"
      to: "caller (run_monitoring)"
      via: "exception propagation"
      pattern: "raise|ValueError|Exception"
---

<objective>
Fix two silent failure modes: (1) PriceSentry.__init__ silently returns on symbol sync failure, (2) config listener exceptions are swallowed without logging.

Purpose: R2 + R4 — Both bugs hide real failures from users and operators. Symbol sync failure causes the monitor to appear running but process no data. Config listener failure means hot-reload silently stops working.
Output: Visible error propagation for both failure modes.
</objective>

<execution_context>
@$HOME/.config/opencode/get-shit-done/workflows/execute-plan.md
@$HOME/.config/opencode/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/STATE.md
@.planning/ROADMAP.md
@.planning/REQUIREMENTS.md
@src/pwatch/core/sentry.py
@src/pwatch/core/config_manager.py
</context>

<tasks>

<task type="auto">
  <name>Task 1: Fix PriceSentry.__init__ silent return on symbol sync failure</name>
  <files>src/pwatch/core/sentry.py</files>
  <read_first>
    - src/pwatch/core/sentry.py (lines 86-114) — current _sync_symbols error handling
    - src/pwatch/core/sentry.py (lines 30-128) — full __init__ to understand error flow
  </read_first>
  <action>
    In `src/pwatch/core/sentry.py`, `__init__` method:
    
    **Fix 1: Replace `return` on sync failure with `raise` (lines 97-98)**
    Current code:
    ```python
            except ValueError as exc:
                error_handler.handle_config_error(...)
                logging.error("Failed to bootstrap symbols: %s", exc)
                return
    ```
    Change to:
    ```python
            except ValueError as exc:
                error_handler.handle_config_error(...)
                logging.error("Failed to bootstrap symbols: %s", exc)
                raise RuntimeError(f"Failed to bootstrap symbols for {exchange_name}: {exc}") from exc
    ```
    
    **Fix 2: Replace `return` on empty matched_symbols with `raise` (lines 100-114)**
    Current code:
    ```python
            if not self.matched_symbols:
                logging.warning(...)
                error_handler.handle_config_error(...)
                return
    ```
    Change to:
    ```python
            if not self.matched_symbols:
                logging.warning(...)
                error_handler.handle_config_error(...)
                raise RuntimeError(f"No matched symbols for exchange {exchange_name}. Run pwatch update-markets to refresh.")
    ```
    
    This ensures PriceSentry.__init__ either fully succeeds or raises — never silently returns in a broken state.
    
    Also update `run()` method (line 131-132) to remove the empty `matched_symbols` guard since __init__ now guarantees symbols exist:
    Current code:
    ```python
    async def run(self):
        if not self.matched_symbols:
            return
    ```
    Change to:
    ```python
    async def run(self):
        # __init__ guarantees matched_symbols is non-empty
        assert self.matched_symbols, "PriceSentry.__init__ must succeed before run()"
    ```
  </action>
  <verify>
    <automated>uv run pytest tests/ -v -x -k "sentry" 2>&1 | tail -30</automated>
    <manual>grep -n "return$" src/pwatch/core/sentry.py → no bare returns in __init__</manual>
  </verify>
  <acceptance_criteria>
    - `src/pwatch/core/sentry.py` __init__ contains `raise RuntimeError` instead of `return` for sync failures
    - `src/pwatch/core/sentry.py` __init__ contains `raise RuntimeError` instead of `return` for empty matched_symbols
    - `src/pwatch/core/sentry.py` run() method contains assert or equivalent guard
    - `uv run pytest tests/ -x` passes
  </acceptance_criteria>
  <done>PriceSentry.__init__ raises on symbol sync failure, never silently returns; run() has assertion guard; tests pass</done>
</task>

<task type="auto">
  <name>Task 2: Fix config listener error swallowing in _notify_listeners</name>
  <files>src/pwatch/core/config_manager.py</files>
  <read_first>
    - src/pwatch/core/config_manager.py (lines 453-476) — current _notify_listeners implementation
  </read_first>
  <action>
    In `src/pwatch/core/config_manager.py`, `_notify_listeners` method, lines 471-476:
    
    Current code:
    ```python
        for listener in listeners:
            try:
                listener(event)
            except Exception:
                # Listener errors should not terminate notification chain.
                continue
    ```
    
    Change to:
    ```python
        for listener in listeners:
            try:
                listener(event)
            except Exception:
                # Listener errors should not terminate notification chain.
                import logging
                logging.exception("Config listener failed: %s", listener.__name__ if hasattr(listener, '__name__') else repr(listener))
                continue
    ```
    
    Key change: Add `logging.exception()` which logs the full traceback at ERROR level. This preserves the non-terminating behavior (continue) but makes failures visible in logs.
    
    Do NOT re-raise the exception — the comment says "should not terminate notification chain" and other listeners still need to receive the event.
  </action>
  <verify>
    <automated>uv run pytest tests/test_config_manager.py -v -x 2>&1 | tail -20</automated>
    <manual>grep -A3 "except Exception:" src/pwatch/core/config_manager.py → must contain logging.exception</manual>
  </verify>
  <acceptance_criteria>
    - `src/pwatch/core/config_manager.py` _notify_listeners except block contains `logging.exception(` call
    - The `continue` statement is preserved (listener chain not terminated)
    - `import logging` is present at top of file (already imported, verify with `grep "import logging"`)
    - `uv run pytest tests/test_config_manager.py -x` passes
  </acceptance_criteria>
  <done>Config listener errors logged with full traceback, notification chain preserved, tests pass</done>
</task>

</tasks>

<verification>
- File: src/pwatch/core/sentry.py — __init__ raises RuntimeError on sync failure, no bare returns
- File: src/pwatch/core/config_manager.py — _notify_listeners logs exceptions with logging.exception
- All existing tests pass: uv run pytest exits 0
- No new lint issues: uv run ruff check src/pwatch/core/sentry.py src/pwatch/core/config_manager.py returns 0
</verification>

<success_criteria>
- PriceSentry.__init__ raises RuntimeError on symbol sync failure (not silent return)
- PriceSentry.__init__ raises RuntimeError on empty matched_symbols (not silent return)
- PriceSentry.run() has assertion guard for matched_symbols
- Config listener exceptions logged with full traceback via logging.exception
- Notification chain continues after listener error (continue preserved)
- All existing tests pass
</success_criteria>

<output>
After completion, create `.planning/phases/01-critical-bug-fixes/01-critical-bug-fixes-02-SUMMARY.md`
</output>
