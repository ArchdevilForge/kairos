# Error Handling

> How errors are handled in this project.

---

## Overview

kairos uses Go idiomatic error handling: functions return `(T, error)` and callers log or propagate. No middleware or circuit-breaker framework — `log/slog` plus explicit error checks are sufficient for a deterministic alert system.

---

## Error Handling Patterns

### 1. Exchange errors

```go
ticker, err := exch.FetchTicker(ctx, symbol)
if err != nil {
    s.log.Warn("ticker fetch failed", "symbol", symbol, "error", err)
    return nil // degrade; scanner continues with warnings
}
```

### 2. Configuration errors

```go
cfg, err := config.Load(path)
if err != nil {
    return fmt.Errorf("load config: %w", err)
}
```

### 3. Optional third-party data (CoinGlass RSI)

```go
m, err := data.FetchSpotRSIMap(timeout)
if err != nil {
    s.log.Debug("coinglass rsi unavailable", "error", err)
    return nil, fmt.Sprintf("CoinGlass RSI unavailable: %v", err)
}
```

Scanner treats optional enrichment as best-effort: log/warn, never panic or block the envelope.

---

## Logging Standards

- `slog.Info` for normal lifecycle events (start, stop, periodic actions)
- `slog.Warn` for recoverable issues (missing data, degraded confidence)
- `slog.Error` for operation failures (WS disconnect, API error)
- Use structured key/value pairs: `"symbol", symbol, "error", err`

---

## Common Mistakes

1. **Ignored errors**: `_ = err` on required paths — check and log or return
2. **Missing context**: log what operation failed and with what parameters
3. **Inconsistent severity**: distinguish Warn (recoverable) from Error (operation failed)
4. **Silent failures**: optional data paths must still emit Debug/Warn when unavailable

---

## Best Practices

1. **Early return on failure** — the scanner returns an error envelope rather than raising.
2. **Graceful degradation** — missing optional data (CoinGlass, BTC context) degrades confidence but doesn't crash.
3. **Telegram delivery is best-effort** — failures are logged, never block the caller.
