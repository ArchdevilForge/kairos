# Directory Structure

> How backend code is organized in this project.

---

## Overview

Single-package Python CLI app using Typer + Rich. No frontend layer (TUI only).

**Important**: Package is `bots.weather` on disk, but all imports use `pm_bot` via symlink (`pm_bot -> bots/weather`).

---

## Directory Layout

```
pm/
├── bots/
│   └── weather/
│       ├── core/                    # I/O boundary: API clients, parsers, caching
│       │   ├── clob.py             # Polymarket CLOB trading (py_clob_client_v2)
│       │   ├── config_loader.py    # TOML config + env var loading
│       │   ├── db.py               # SQLite trade database
│       │   ├── kelly.py            # Kelly criterion position sizing
│       │   ├── observation.py      # METAR observed temperature fetching
│       │   ├── paper_trade.py      # Paper trading SQLite DB
│       │   ├── parser.py           # Temperature bucket title parser (regex)
│       │   ├── polymarket.py       # Gamma API client, event parsing
│       │   ├── risk.py             # Circuit breakers, city/daily limits
│       │   ├── staged_entry.py     # Time-to-resolution position scaling
│       │   ├── weather.py          # Open-Meteo GEFS ensemble forecast
│       │   └── ws.py               # WebSocket real-time price feed
│       ├── strategies/              # Pure computation: models → recommendations
│       │   ├── base.py             # Strategy protocol + Gopfan2Strategy
│       │   └── forecast_arb.py     # ForecastArbStrategy (model vs market)
│       ├── backtest/                # Backtesting framework
│       │   ├── engine.py           # Backtest runner
│       │   ├── real_data.py        # Historical market data fetching
│       │   ├── costs.py            # Fee/slippage/fill modeling
│       │   ├── data.py             # Historical forecast data
│       │   ├── metrics.py          # Sharpe, Sortino, MaxDD
│       │   └── report.py           # Rich table rendering
│       ├── cli/                     # Presentation layer: Typer + Rich
│       │   ├── app.py              # Typer app, command registration
│       │   ├── scan.py             # scan command
│       │   ├── watch.py            # watch TUI (WebSocket)
│       │   ├── trade.py            # trade command (live execution)
│       │   ├── daemon.py           # 24/7 background daemon
│       │   ├── settle.py           # Redeem winning positions
│       │   ├── orders.py           # Show open orders
│       │   ├── explain.py          # Detailed strategy reasoning
│       │   ├── markets.py          # List weather markets
│       │   ├── backtest_cmd.py     # backtest command
│       │   ├── config_cmd.py       # config command
│       │   ├── display.py          # Rich table/panel rendering
│       │   └── notifications.py    # Discord/Telegram alerts
│       └── models/                  # Data definitions (no logic)
│           ├── market.py           # WeatherEvent, TemperatureBucket, ForecastResult, Recommendation
│           └── config.py           # CITY_COORDS, STRATEGY_DEFAULTS, CACHE_TTL
├── config.toml                      # Runtime config (CLOB creds, sizing, stations)
├── docs/
│   └── polymarket-trading-bot-plan.md  # Smart wallet strategy research (not implemented)
└── pyproject.toml                   # Package config, entry point
```

---

## Module Organization

- `core/` — I/O boundary: API clients, parsers, caching. All external calls live here.
- `strategies/` — Pure computation: takes models, returns recommendations. No I/O.
- `backtest/` — Backtesting: historical data, simulation, metrics.
- `cli/` — Presentation layer: Typer commands, Rich output. Orchestrates core + strategies.
- `models/` — Data definitions: dataclasses, config constants. No logic.

### Dependency direction

```
cli → strategies → models ← core
  ↘     core    ↗
  ↘   backtest ↗
```

- `models` is the shared leaf (no imports from other app modules)
- `core` imports from `models` only
- `strategies` imports from `models` only
- `backtest` imports from `models` and `core`
- `cli` imports from all other modules

---

## Package Name Issue

The package is `bots.weather` on disk, but all code uses `pm_bot` imports. A symlink resolves this:

```
pm_bot -> bots/weather
```

The `pyproject.toml` entry point uses the real path:
```toml
[project.scripts]
pm-bot = "bots.weather.cli.app:app"
```

---

## Naming Conventions

- Package: `pm_bot` (snake_case) — actual dir is `bots/weather/`
- CLI entry: `pm-bot` (kebab-case) via `pyproject.toml [project.scripts]`
- Files: snake_case (`config_cmd.py` to avoid `config` shadowing stdlib)
- Strategy classes: PascalCase (`Gopfan2Strategy`, `ForecastArbStrategy`)
- Config keys: snake_case (`yes_max`, `min_edge`, `max_market_price`)

---

## Removed Modules (2026-05-14 cleanup)

The following were deleted as dead code:

| Module | Reason |
|--------|--------|
| `core/aggregation.py` | BMA consensus probability — no callers |
| `core/station_bias.py` | Station bias learning — not integrated |
| `core/city_variance.py` | City variance filter — over-engineered |
| `core/sources/` | NWS/METAR duplicate implementations |
| `backtest/monte_carlo.py` | Fake random simulations |
| `backtest/snowball_metrics.py` | Hopium milestone tracking |
| `models/forecast.py` | ConsensusForecast — unused |
| `shared/` | Duplicate of core/polymarket.py |
| `tests/` | 47 files, 0 passing |

---

## Examples

Well-organized module: `pm_bot/core/polymarket.py`
- Single responsibility: Gamma API client + event parsing
- All I/O in one place, cached via module-level TTLCache
- Returns typed dataclasses from `models`
