---
phase: 01-bug-fixes
plan: 01
type: execute
subsystem: cli
tags: [bug-fix, cli, ask_yes_no, fallback, tdd]
dependency_graph:
  requires: []
  provides:
    - "ask_yes_no blank-input fallback contract (D-04)"
    - "Focused regression tests for both default branches"
  affects:
    - "tests/test_cli_setup_wizard.py (adjacent verification, no changes)"
tech_stack:
  added: []
  patterns: [tdd-red-green, monkeypatch-isolation, explicit-fallback]
key_files:
  created: []
  modified:
    - src/pwatch/app/cli.py
    - tests/test_cli_misc.py
decisions:
  - "Used explicit `if not response: response = default_str` fallback in ask_yes_no instead of relying on get_user_input coercion"
  - "Extracted ask_yes_no assertions from combined validator test to isolate CLI contract"
  - "Added 'y' and 'yes' English affirmative token coverage alongside existing '是' path"
  - "Renamed test_validators_and_yes_no_paths → test_validators (no behavioral change)"
metrics:
  duration_seconds: 180
  completed_date: "2026-04-11T10:17:00Z"
  tasks_completed: 2
  tasks_total: 2
---

# Phase 01 Plan 01: ask_yes_no Blank-Input Fallback Hardening Summary

## One-liner

Made `ask_yes_no` explicitly normalize blank input to the supplied `default` boolean, with focused TDD coverage locking both default-True and default-False branches plus affirmative-language parsing.

## What Changed

| File | Change |
|------|--------|
| `src/pwatch/app/cli.py:113-115` | Added explicit `if not response: response = default_str` fallback after `get_user_input()` call |
| `tests/test_cli_misc.py` | Split `test_validators_and_yes_no_paths` → `test_validators` + consolidated ask_yes_no assertions into `test_ask_yes_no_empty_uses_default` with D-04 traceability; added "y"/"yes" English affirmative coverage |

## Deviations from Plan

### Minor Scope Spillover (Whitespace)

- **Found during:** Task 1 commit
- **Issue:** The working-tree `cli.py` had pre-existing whitespace inconsistencies (extra blank lines). When staging the ask_yes_no fix, `git add` captured adjacent whitespace normalization in the same commit.
- **Impact:** 4 blank lines removed, 2 blank lines added across `interactive_config`, `cmd_update_markets`, and `_validate_telegram_token`. No behavioral change.
- **Commit:** `936b818`
- **Assessment:** Low risk — these are cosmetic-only changes that improve consistency with ruff formatting rules.

### No Auto-fixed Bugs

The implementation was already functionally correct via `get_user_input`'s default-return behavior. The explicit fallback adds defensive clarity without changing semantics.

## Auth Gates

None.

## Verification Results

```
tests/test_cli_misc.py::test_validators PASSED
tests/test_cli_misc.py::test_ask_yes_no_empty_uses_default PASSED
tests/test_cli_setup_wizard.py::test_interactive_config_english_copy_matches_current_auto_mode PASSED
tests/test_cli_setup_wizard.py::test_interactive_config_no_longer_emits_chart_config_or_prompts PASSED
tests/test_cli_setup_wizard.py::test_interactive_config_requires_non_empty_chat_id PASSED
```

15/15 tests pass, 0 failures, 2 pre-existing warnings (unrelated coroutine warnings in `test_cmd_run_handles_keyboard_interrupt_and_exception`).

## Known Stubs

None.

## Threat Flags

| Flag | File | Description |
|------|------|-------------|
| threat_flag: T-01-01 | `src/pwatch/app/cli.py` | `ask_yes_no` blank-input normalization preserved — empty input cannot silently mutate semantics |
| threat_flag: T-01-02 | `tests/test_cli_misc.py` | Direct regression assertions on both default branches locked so future changes cannot obscure the CLI contract |

## Self-Check: PASSED

- `src/pwatch/app/cli.py` FOUND
- `tests/test_cli_misc.py` FOUND
- Commit `42c45e5` (test isolation) FOUND
- Commit `936b818` (ask_yes_no fix) FOUND
