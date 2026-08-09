# Error Handling

> How errors are handled in this project.

---

## Overview

This project uses a defensive, fail-safe approach: external API failures must never crash the app. Errors are caught at the I/O boundary and logged with structlog. The CLI continues with whatever data it could fetch.

---

## Error Types

No custom exception hierarchy. All errors are caught as standard library exceptions:
- `httpx.HTTPError` for API failures (network, timeout, HTTP status)
- `ValueError` / `KeyError` for malformed API response data
- `asyncio` exceptions for timeout handling

---

## Error Handling Patterns

### API Call Pattern

```python
try:
    resp = await client.get(url, params=params)
    resp.raise_for_status()
    data = resp.json()
except httpx.HTTPError as e:
    log.error("api_error", endpoint=url, error=str(e))
    return None  # or empty list
```

Key rules:
- Every `httpx` call is wrapped in try/except
- On failure: log + return `None` or empty collection
- Caller checks for `None` and skips that market/forecast
- Never propagate exceptions past the I/O boundary

### Graceful Degradation

```python
for event in events:
    forecast = await fetch_forecast(client, event.city, event.date)
    if forecast is None:
        continue  # skip this event, keep processing others
```

---

## API Error Responses

Not applicable — this is a CLI tool, not a server. Errors are shown to the user via:
- Rich console output (skipped markets noted at bottom)
- `--debug` flag enables full structlog output with error details

---

## Common Mistakes

- **°F tail boundary bug**: When checking `is_high_tail`/`is_low_tail`, check the *raw* value before unit conversion. `999°F → 537°C` fails the `>= 999` check after conversion. Always check `self.is_high_tail` first, then convert.
- **Bare `except:`**: Never use. Always catch specific exceptions.
- **Caching + errors**: Don't cache `None` results — TTLCache will return stale None for the full TTL window.
