# Logging Guidelines

> How logging is done in this project.

---

## Overview

Uses `structlog` for structured logging. Default mode is quiet (only Rich output to terminal). `--debug` flag enables full structured log output to stderr.

---

## Log Levels

| Level | When to use |
|-------|-------------|
| `debug` | API request URLs, params, cache hits/misses |
| `info` | Not used in default mode — Rich output replaces it |
| `warning` | Skipped markets, missing forecasts, degraded functionality |
| `error` | API failures, parse failures, unexpected exceptions |

---

## Structured Logging

All log calls use keyword arguments for structured fields:

```python
log.debug("api_request", url=url, params=params)
log.error("api_error", endpoint=url, error=str(e))
log.warning("forecast_missing", city=city, date=date)
```

Required fields per log type:
- API calls: `url`, optional `params`
- Errors: `error` (exception message), context-specific fields
- Cache: `key`, `hit` (bool)

---

## What to Log

- API request URLs and parameters (debug level)
- API response status codes and sizes (debug level)
- Cache hit/miss decisions (debug level)
- Skipped markets or events (warning level)
- Strategy computation details (debug level)

---

## What NOT to Log

- API keys or tokens (use env vars, never log)
- Full response bodies (too noisy, log status + size)
- Personal data (not applicable for this project)
