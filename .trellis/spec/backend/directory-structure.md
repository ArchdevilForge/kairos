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
├── telegram.py                # Telegram sender and alert formatter
├── watch_runner.py            # realtime alert runner
├── analysis/                  # technical analysis
│   ├── __init__.py
│   ├── box_pattern.py         # 箱体识别算法
│   └── cycle.py               # 春夏秋冬周期判断
├── data/                      # data clients and orchestration
│   ├── __init__.py
│   ├── coinglass_client.py    # optional CoinGlass hard-data context
│   └── data_manager.py        # WS orchestration + detectors + Telegram
├── detectors/                 # anomaly detectors
│   ├── __init__.py
│   ├── base.py                # BaseDetector + AnomalyEvent
│   ├── futures_metrics.py     # open interest / funding rate polling
│   ├── liquidation.py         # CoinGlass liquidation polling
│   ├── long_short_ratio.py    # CoinGlass long/short ratio polling
│   ├── price_velocity.py      # 价格速度检测
│   ├── resonance.py           # multi-dimension signal quality scorer
│   └── volume_spike.py        # 成交量异常检测
├── exchanges/                 # exchange adapters
│   ├── __init__.py
│   ├── base.py                # BaseExchange (CCXT wrapper)
│   ├── binance.py             # Binance WS implementation
│   ├── bybit.py               # Bybit WS implementation
│   └── okx.py                 # OKX WS implementation
└── utils/                     # utilities
    ├── __init__.py
    ├── blacklist.py            # read-only symbol blacklist (.txt file)
    ├── market_data.py          # ticker field extraction helpers
    └── zscore.py               # rolling z-score computation
```

---

## Data Flow

```
OKX/Binance/Bybit WS feeds
    │
    ▼
DataManager ─── per-exchange detectors ───→ Telegram (realtime alerts)
    │
    ▼ (polling)
FuturesMetricsDetector · LongShortRatioDetector · LiquidationDetector
    │
    ▼
ResonanceScorer (multi-dimension signal quality)
    │
    ▼
Telegram hard-data alert
    │
    ▼
Human chart review and decision


Scanner (one-shot, CLI):
scanner.py ──→ Telegram / stdout
    │
    ▼
scan_market() / analyze_symbol_setup()
    │
    ▼
_signal_envelope → alert_runner.py → Telegram
```

---

## Removed Modules

These modules are intentionally absent:

- `src/kairos/mcp/` and `src/kairos/mcp_server.py` — no production MCP layer.
- `src/kairos/webhook.py` — Telegram is sent directly, not through webhook relays.
- `src/kairos/utils/get_exchange.py` — inlined into scanner.py as `_get_exchange`.
- `src/kairos/utils/cache_manager.py` — removed; not needed.
- `src/kairos/utils/error_handler.py` — removed; standard try/except + logging used.
- `src/kairos/utils/performance_monitor.py` — removed; not needed.
- `src/kairos/analysis/support_resistance.py` — removed; box_pattern used instead.
- `skills/` — no assistant skill layer in the trading path.
- Trading execution modules — final decisions and order placement stay human-only.

---

## Naming Conventions

- File names: `snake_case.py`
- Classes: `PascalCase`
- Functions: `snake_case`
- Private attributes: `_leading_underscore`
- Internal helpers shared across modules: `_shared_` prefix (e.g. `_shared_extract_last_price`)
