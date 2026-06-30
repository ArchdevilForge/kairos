# Error Handling

> How errors are handled in this project.

---

## Overview

kairos uses standard Python error handling with custom exception types and logging. No error handler middleware or circuit breaker framework is used — standard `try/except` + `logging` is sufficient for a deterministic alert system.

---

## Error Handling Patterns

### 1. Exchange errors

```python
try:
    result = await exchange.fetch_ticker(symbol)
except ccxt.NetworkError as e:
    logger.error(f"Network error fetching {symbol}: {e}")
    return {}
except Exception as e:
    logger.warning(f"Failed to fetch ticker for {symbol}: {e}")
    return {}
```

### 2. Configuration errors

```python
try:
    config = load_config(path)
except FileNotFoundError:
    logger.error(f"Config not found: {path}")
    raise
except yaml.YAMLError as e:
    logger.error(f"Config parse error: {e}")
    raise
```

### 3. Data validation errors

```python
def _normalize_symbol(symbol: str) -> str:
    value = symbol.strip().upper()
    if not value:
        raise ValueError("symbol is required")
    # ...
```

---

## Logging Standards

- `logger.info` for normal lifecycle events (start, stop, periodic actions)
- `logger.warning` for recoverable issues (missing data, degraded confidence)
- `logger.error` for operation failures (WS disconnect, API error)
- `logger.exception` (includes traceback) for unexpected exceptions

---

## Common Mistakes

1. **Broad except**: bare `except:` hides real errors — always name the exception
2. **Missing context**: log what operation failed and with what parameters
3. **Inconsistent severity**: distinguish WARNING (recoverable) from ERROR (operation failed)
4. **Silent failures**: don't swallow exceptions without at least a `logger.warning`

---

## Best Practices

1. **Early return on failure** — the scanner returns an error envelope rather than raising.
2. **Graceful degradation** — missing optional data (CoinGlass, BTC context) degrades confidence but doesn't crash.
3. **Telegram delivery is best-effort** — failures are logged, never block the caller.
