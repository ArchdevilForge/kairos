# Quality Guidelines

> Code quality standards for backend development.

---

## Overview

Enforced via `ruff` (lint) and `mypy` (type-check). Both must pass with zero errors before commit.

---

## Forbidden Patterns

- `bare except:` — always catch specific exceptions
- Hardcoded API keys/secrets — use environment variables via `python-dotenv`
- `# type: ignore` without specific error code — use `# type: ignore[no-any-return]` etc.
- Magic numbers for log levels — use `logging.DEBUG`, `logging.WARNING` etc.
- Raw `temp_low`/`temp_high` for temperature comparisons — always use `temp_low_c`/`temp_high_c` (handles °F→°C)

---

## Required Patterns

- All `httpx` calls must be in try/except blocks
- Dataclasses for all models (not Pydantic BaseModel for internal models)
- TTLCache for all external API responses
- `resolve_city_alias()` for city name matching (handles NYC→New York etc.)
- Type annotations on all public functions

---

## Testing Requirements

Phase 1 has no automated test suite. Validation is manual via CLI commands. Future phases should add pytest with API mocking.

---

## Code Review Checklist

- [ ] `uv run ruff check pm_bot/` passes
- [ ] `uv run mypy pm_bot/` passes
- [ ] No hardcoded API keys
- [ ] API calls have error handling (try/except)
- [ ] Temperature comparisons use `*_c` properties
- [ ] City matching uses `resolve_city_alias()`
