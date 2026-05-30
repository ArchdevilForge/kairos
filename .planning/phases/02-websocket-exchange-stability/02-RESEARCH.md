# Phase 2: WebSocket & Exchange Stability — Research

**Date:** 2026-04-06
**Phase Goal:** Fix thread safety, reconnection, and detector registration issues.

---

## R3: WebSocket Reconnection Thread Leak

### Problem
`check_ws_connection()` (base.py:420) calls `start_websocket()` which spawns a new `threading.Thread`. The old thread is NOT joined or terminated. `stop_websocket()` does `self.ws_thread.join(timeout=5)` but `check_ws_connection()` never calls `stop_websocket()` first.

### Current Code Flow
```
check_ws_connection() 
  → if not ws_connected and running:
    → start_websocket(symbols)  # spawns new thread, old one still alive
```

### Recommended Approach: Proper Thread Lifecycle

**Option A: Stop-then-Start (Recommended)**
Before calling `start_websocket()` in `check_ws_connection()`, call `stop_websocket()` first:
```python
def check_ws_connection(self):
    if not self.ws_connected and self.running:
        # Terminate old thread before spawning new one
        self._stop_websocket_thread()
        self.start_websocket(symbols)
```

The `_stop_websocket_thread()` should:
1. Set `self.running = False` — but wait, this flag is used by the outer loop too
2. Better: use a dedicated `_ws_should_stop` flag or `threading.Event`
3. Signal the thread to stop, join with timeout, then spawn new one

**Key Insight:** The `self.running` flag is shared between `start_websocket`, `stop_websocket`, AND the main loop's health check. We need to be careful not to set `running = False` during reconnection since the main loop checks `self.running` (actually it doesn't — the main loop runs independently).

**Best Pattern:**
```python
def _stop_websocket_thread(self):
    """Stop only the WebSocket thread, without affecting the exchange lifecycle."""
    if self.ws_thread and self.ws_thread.is_alive():
        self.running = False  # signal thread to exit
        self.ws_thread.join(timeout=5)
        if self.ws_thread.is_alive():
            logging.warning("WebSocket thread did not exit within timeout")
    self.ws_thread = None
    self.ws_connected = False
```

Then in `check_ws_connection()`:
```python
def check_ws_connection(self):
    if not self.ws_connected and self.running:
        self._stop_websocket_thread()
        self.running = True  # re-enable for new thread
        self.start_websocket(symbols)
```

**Option B: Single Persistent Thread with Reconnect Logic**
Instead of spawning new threads on reconnect, keep one thread that handles its own reconnection loop internally. This is more complex but eliminates thread lifecycle entirely.

**Decision:** Option A is simpler, less invasive, and directly fixes the leak. Option B is a larger refactor better suited for Phase 3 or 4.

### Pitfalls
- `join(timeout=5)` can timeout — must handle this case (log warning, don't block forever)
- `self.running` flag reuse — need to temporarily set it False then True again, which is racy
- Better: introduce a `_ws_stopping` flag or `threading.Event` for clean shutdown signaling

---

## R5: Exchange Reload Race Condition

### Problem
`_reload_runtime_components()` (sentry.py:555) does:
```python
self.exchange.close()          # 1. Old exchange closed
self.exchange = get_exchange() # 2. New exchange created (no detectors, no WS)
self._sync_symbols()           # 3. Symbols synced
self.exchange.start_websocket() # 4. WS started
```
Between steps 1-4, the main loop can access `self.exchange` which is in an inconsistent state.

### Recommended Approach: Atomic Swap with Prepare-Then-Commit

```python
def _reload_runtime_components(self, event):
    # 1. Prepare new exchange OFF the main reference
    new_exchange = get_exchange(exchange_name)
    # 2. Register detectors on new exchange BEFORE making it live
    self._register_detectors_on(new_exchange)
    # 3. Sync symbols on new exchange
    new_symbols = self._sync_symbols_on(new_exchange, exchange_name)
    
    if not new_symbols:
        new_exchange.close()
        return
    
    # 4. Atomic swap under lock
    with self._config_lock:
        old_exchange = self.exchange
        self.exchange = new_exchange
        self.matched_symbols = new_symbols
    
    # 5. Start WS on new exchange (now live reference)
    self.exchange.start_websocket(new_symbols)
    
    # 6. Close old exchange after swap
    old_exchange.close()
```

**Key insight:** The new exchange is fully prepared (detectors, symbols) before being assigned to `self.exchange`. The swap itself is atomic under lock. The old exchange is closed after the swap so the main loop never sees a broken reference.

### Thread Safety
- `self._config_lock` (RLock) already protects `self.config`, `self.matched_symbols`
- The main loop reads `self.exchange` without lock — this is acceptable in CPython due to GIL for simple attribute assignment, but we should still use the lock for the full swap to prevent main loop from accessing the old exchange between close and reassign
- Better: use a dedicated `_exchange_lock` (separate from config lock) to protect `self.exchange` reference

**Decision:** Use `_exchange_lock` for exchange reference swaps. Keep `_config_lock` for config data only. This follows single-responsibility for locks.

---

## R6 & R7: Detector Registration Loss

### Problem
After exchange reload (R6) or auto-refresh WS restart (R7), the new exchange has empty `_detectors`.

### Current Code
- `_setup_detectors()` creates detectors and registers on `self.exchange` — only called in `__init__`
- `_reload_runtime_components()` does NOT call `_setup_detectors()` or re-register
- `_check_auto_refresh()` does `close()` + `start_websocket()` without detector re-registration

### Recommended Approach: Extract Detector Registration as Reusable Method

```python
def _setup_detectors(self) -> None:
    """Create and register detectors on the current exchange."""
    self._register_detectors_on(self.exchange)

def _register_detectors_on(self, exchange: BaseExchange) -> None:
    """Register detectors on a specific exchange instance."""
    self._velocity_detector = PriceVelocityDetector(self.config)
    self._volume_detector = VolumeSpikeDetector(self.config)
    
    def _on_anomaly(event: AnomalyEvent) -> None:
        try:
            self._anomaly_events.put_nowait(event)
        except Exception:
            self._anomaly_events.put(event)
    
    self._velocity_detector.on_event(_on_anomaly)
    self._volume_detector.on_event(_on_anomaly)
    
    exchange.register_detector(self._velocity_detector)
    exchange.register_detector(self._volume_detector)
```

Then:
- `_reload_runtime_components()` calls `_register_detectors_on(new_exchange)` before atomic swap
- `_check_auto_refresh()` calls `_register_detectors_on(self.exchange)` after close but before start_websocket

### Alternative: BaseExchange Preserves Detectors
Add a method to `BaseExchange` that returns its detectors, so the new exchange can inherit:
```python
def get_detectors(self) -> list[BaseDetector]:
    return list(self._detectors)
```

But this couples BaseExchange to detector internals. Better to keep detector management in PriceSentry (the owner).

**Decision:** Extract `_register_detectors_on(exchange)` method in PriceSentry. Call it during reload and auto-refresh.

---

## Validation Architecture

### Observable Truths for Phase 2
1. WS reconnection does not increase thread count
2. After exchange reload, price/volume alerts still fire
3. After auto-refresh symbol change, alerts still fire
4. No crash during exchange swap (main loop sees consistent state)
5. Existing tests pass

### Required Artifacts
- Modified `src/pwatch/exchanges/base.py` — thread-safe reconnection
- Modified `src/pwatch/core/sentry.py` — atomic swap + detector re-registration
- New tests for reconnection, reload, auto-refresh scenarios

### Key Links
- `check_ws_connection()` → must call `_stop_websocket_thread()` before `start_websocket()`
- `_reload_runtime_components()` → must `_register_detectors_on(new_exchange)` before swap
- `_check_auto_refresh()` → must `_register_detectors_on(self.exchange)` after close

---

## Implementation Strategy

**Plan 1: Thread-safe WebSocket reconnection (R3)**
- Fix `check_ws_connection()` to properly stop old thread before starting new one
- Add `_stop_websocket_thread()` method with proper cleanup
- Add threading.Event for clean shutdown signaling

**Plan 2: Atomic exchange reload + detector re-registration (R5, R6, R7)**
- Add `_exchange_lock` for protecting exchange reference
- Extract `_register_detectors_on(exchange)` method
- Fix `_reload_runtime_components()` with prepare-then-commit pattern
- Fix `_check_auto_refresh()` to re-register detectors

This splits into two parallel-incompatible plans (both touch sentry.py), so they must be sequential. Plan 1 first (R3 is simpler, lower risk), then Plan 2 (R5+R6+R7, higher complexity).
