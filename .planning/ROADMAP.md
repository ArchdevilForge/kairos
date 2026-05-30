# ROADMAP.md — pwatch v1.2 Feature Expansion

## Milestone: v1.2 — 功能扩展与可视化

### Phase 1: 收尾加固 (Bug Fixes)
Fix 2 pre-existing test failures from v1.1.

**Plans:** 2 plans

Plans:
- [x] 01-01-PLAN.md — Make `ask_yes_no` own blank-input fallback and lock focused CLI coverage
- [ ] 01-02-PLAN.md — Stabilize `test_cli_misc.py` monkeypatch isolation and rerun locked phase verification

**UAT**:
- [ ] `uv run pytest tests/test_cli_misc.py` — all pass
- [ ] `uv run pytest` — 677+ tests pass (no regression)

**Files**: `src/pwatch/app/cli.py`, `tests/test_cli_misc.py`

---

### Phase 2: 新检测器启用
Enable WhaleDetector (large trade detection) and CrossExchangeDetector (price deviation across exchanges).

**Plans:** 2 plans

Plans:
- [ ] 02-01-PLAN.md — Wire WhaleDetector: register + WS trade notify + tests
- [ ] 02-02-PLAN.md — Wire CrossExchangeDetector: main loop feed + tests

**UAT**:
- [ ] WhaleDetector fires on large trades (OKX/Bybit)
- [ ] CrossExchangeDetector detects price deviation between ≥2 exchanges
- [ ] Both detectors appear in anomaly event queue
- [ ] All detector tests pass

**Files**: `src/pwatch/core/sentry.py`, `src/pwatch/exchanges/base.py`, `src/pwatch/exchanges/okx.py`, `src/pwatch/exchanges/bybit.py`, `src/pwatch/core/main_loop.py`, tests/

---

### Phase 3: 多通知渠道
Add Discord Webhook and DingTalk notification channels alongside Telegram.

**Plans:** 3 plans

Plans:
- [ ] 03-01-PLAN.md — Notification channel registry + validator update
- [ ] 03-02-PLAN.md — Discord Webhook implementation
- [ ] 03-03-PLAN.md — DingTalk Webhook implementation (HMAC signing)

**UAT**:
- [ ] `notificationChannels: ["telegram", "discord"]` sends to both
- [ ] `notificationChannels: ["dingtalk"]` sends to DingTalk
- [ ] Each channel has independent retry logic
- [ ] All notification tests pass

**Files**: `src/pwatch/utils/send_notifications.py`, `src/pwatch/notifications/discord.py`, `src/pwatch/notifications/dingtalk.py`, `src/pwatch/utils/config_validator.py`, tests/

---

### Phase 4: 警报持久化
SQLite-based alert persistence with query API.

**Plans:** 2 plans

Plans:
- [ ] 04-01-PLAN.md — SQLite alert store schema + repository
- [ ] 04-02-PLAN.md — Persistence hook in main loop + query API

**UAT**:
- [ ] Every anomaly event persisted to SQLite
- [ ] `AlertStore.query()` returns filtered results (by symbol, severity, date range)
- [ ] `AlertStore.stats()` returns aggregated statistics
- [ ] Thread-safe writes (WAL mode)
- [ ] All persistence tests pass

**Files**: `src/pwatch/persistence/store.py`, `src/pwatch/persistence/__init__.py`, `src/pwatch/core/main_loop.py`, `src/pwatch/core/sentry.py`, tests/

---

### Phase 5: Web Dashboard
FastAPI backend + simple frontend for real-time monitoring and alert history.

**Plans:** 3 plans

Plans:
- [ ] 05-01-PLAN.md — FastAPI backend (alerts API, status API)
- [ ] 05-02-PLAN.md — Simple HTML/JS frontend (alert table, filters, auto-refresh)
- [ ] 05-03-PLAN.md — `pwatch dashboard` CLI command + launcher

**UAT**:
- [ ] `pwatch dashboard` starts web server on http://127.0.0.1:8080
- [ ] `/api/alerts` returns paginated alert list with filters
- [ ] `/api/alerts/stats` returns aggregated statistics
- [ ] `/api/status` returns system status (exchanges, symbols, WS health)
- [ ] Frontend renders and auto-refreshes every 5s
- [ ] All dashboard tests pass

**Files**: `src/pwatch/dashboard/api.py`, `src/pwatch/dashboard/static/index.html`, `src/pwatch/dashboard/launcher.py`, `src/pwatch/app/cli.py`, tests/

---

### Phase 6: 多交易所并行
Simultaneous monitoring of multiple exchanges with per-exchange symbol routing.

**Plans:** 3 plans

Plans:
- [ ] 06-01-PLAN.md — Multi-exchange config + validator
- [ ] 06-02-PLAN.md — Multi-exchange main loop + all WS start
- [ ] 06-03-PLAN.md — Symbol routing per-exchange + config handler hot reload

**UAT**:
- [ ] `exchanges: ["okx", "binance"]` starts WS for both
- [ ] Price checks run against all exchanges
- [ ] CrossExchangeDetector receives price feeds from all exchanges
- [ ] Config hot-reload adds/removes exchanges without restart
- [ ] All multi-exchange tests pass

**Files**: `src/pwatch/core/main_loop.py`, `src/pwatch/core/config_handler.py`, `src/pwatch/core/sentry.py`, `src/pwatch/utils/config_validator.py`, tests/
