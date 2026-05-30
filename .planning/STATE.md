---
gsd_state_version: 1.0
milestone: v1.2
milestone_name: — 功能扩展与可视化
status: Executing Phase 01
last_updated: "2026-04-11T10:17:00Z"
progress:
  total_phases: 6
  completed_phases: 0
  total_plans: 2
  completed_plans: 1
  percent: 50
---

# STATE.md — pwatch Project Memory

## Current State

- **Milestone**: v1.2 — 功能扩展与可视化 (NEW)
- **Previous**: v1.1 — Reliability & Quality ✅ ALL PHASES COMPLETE (archived to `.planning/milestones/v1.1-phases/`)
- **Last Updated**: 2026-04-08
- **Baseline**: 675/677 tests pass, 0 lint errors

## Phase Status

| Phase | Name | Status |
|-------|------|--------|
| 1 | 收尾加固 (Bug Fixes) | ⏳ Pending |
| 2 | 新检测器启用 | ⏳ Pending |
| 3 | 多通知渠道 | ⏳ Pending |
| 4 | 警报持久化 | ⏳ Pending |
| 5 | Web Dashboard | ⏳ Pending |
| 6 | 多交易所并行 | ⏳ Pending |

## v1.1 Final Metrics (Baseline)

| Metric | Value |
|--------|-------|
| Lint issues | 0 |
| `sentry.py` lines | 307 |
| Test files | 45 |
| Total tests | 677 |
| Pre-existing failures | 2 (test_cli_misc.py) |
| ConfigHandler coverage | 90% |
| MainLoopHandler coverage | 90% |

## Module Structure

```
src/pwatch/
  app/          cli.py, runner.py
  core/         sentry.py (307), alert_formatter.py (78), config_handler.py (507),
                main_loop.py (261), notifier.py, config_manager.py
  detectors/    price_velocity, volume_spike, high_low_break, momentum,
                price_alert, funding_rate, oi_spike, whale (unused),
                cross_exchange (unused), base.py
  exchanges/    base.py, binance.py, okx.py, bybit.py
  notifications/ telegram.py, telegram_bot_service.py
  utils/        error_handler, cache, config, markets, send_notifications,
                config_validator, parse_timeframe, quiet_hours
tests/          Mirror of src, conftest.py for shared fixtures
```

## Known Issues (Pre-existing, out of v1.1 scope)

- 2 test failures: `test_cli_misc.py` (ask_yes_no fallback, monkeypatch self-interference)
- These are **Phase 1** targets for v1.2

## Context for Future Work

- `self.exchanges` dict already created in sentry.py init (multi-exchange scaffolding 60% done)
- CrossExchangeDetector exists but never receives data
- WhaleDetector exists but never registered
- `_notify_detectors_trades` defined in base.py but never called from any exchange WS handler
- `send_notifications.py` channel loop has extension point for new channels
- `notificationChannels` config array already exists
