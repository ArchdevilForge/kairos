# Directory Structure

> How backend code is organized in this project.

---

## Overview

kairos is a human-controlled crypto futures alert system. It reads exchange market data, runs deterministic detectors/scanners, and sends Telegram hard-data alerts. It does not use LLMs and does not place orders.

`docs/architecture.md` is the architecture source of truth. If older task documents conflict with it, update the old document instead of reviving removed systems.

**Entrypoints**: `uv run kairos-watch` for realtime alerts, `uv run kairos-alert` for one-shot scanner summaries, and `uv run kairos-backtest` for local backtests.

---

## Directory Layout

```
src/kairos/
├── __init__.py
├── alert_runner.py            # one-shot scanner Telegram alerts
├── backtest.py                # local backtest utilities
├── config.py                  # YAML config loading
├── paths.py                   # XDG paths
├── scanner.py                 # deterministic market scanner
├── signal_schema.py           # shared scanner response envelope
├── telegram.py                # Telegram sender and alert formatter
├── watch_runner.py            # realtime alert runner
├── analysis/                  # technical analysis
│   ├── __init__.py
│   ├── box_pattern.py         # 箱体识别算法
│   ├── cycle.py               # 春夏秋冬周期判断
│   └── support_resistance.py  # 支撑阻力位
├── data/                      # data clients and orchestration
│   ├── __init__.py
│   ├── coinglass_client.py    # optional hard-data context
│   └── data_manager.py        # WS orchestration + detectors + Telegram alerts
├── detectors/                 # anomaly detectors
│   ├── __init__.py
│   ├── base.py                # 基础检测器 + AnomalyEvent
│   ├── price_velocity.py      # 价格速度检测
│   └── volume_spike.py        # 成交量异常检测
├── exchanges/                 # exchange adapters
│   ├── __init__.py
│   ├── base.py                # 基础交易所类（CCXT封装）
│   ├── binance.py             # Binance WS实现
│   ├── bybit.py               # Bybit WS实现
│   └── okx.py                 # OKX WS实现
└── utils/                     # utilities
    ├── __init__.py
    ├── cache_manager.py        # 缓存管理
    ├── error_handler.py        # 错误处理 + 断路器 + 重试
    ├── get_exchange.py         # 交易所工厂函数
    └── performance_monitor.py  # 性能监控
```

---

## Data Flow

```
OKX/Binance/Bybit WS feeds
    │
    ▼
DataManager
    │
    ▼
PriceVelocityDetector · VolumeSpikeDetector (per-exchange)
    │  AnomalyEvent callback
    ▼
DataManager._on_anomaly_event (5s dedup + Telegram dispatch)
    │
    ▼
Telegram hard-data alert
    │
    ▼
Human chart review and decision
```

---

## Removed Modules

These modules are intentionally absent:

- `src/kairos/mcp/` and `src/kairos/mcp_server.py` — no production MCP layer.
- `src/kairos/webhook.py` — Telegram is sent directly, not through webhook relays.
- `skills/` — no assistant skill layer in the trading path.
- Trading execution modules — final decisions and order placement stay human-only.

---

## Naming Conventions

- File names: `snake_case.py`
- Classes: `PascalCase`
- Functions: `snake_case`
- Private attributes: `_leading_underscore`
