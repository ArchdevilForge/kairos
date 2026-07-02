# Directory Structure

> How backend code is organized in this project.

---

## Overview

kairos is a human-controlled crypto futures alert system. It reads exchange market data, runs deterministic detectors/scanners, and sends Telegram hard-data alerts. It does not use LLMs and does not place orders.

`docs/architecture.md` is the architecture source of truth.

**Entrypoints**:
- `go run ./cmd/kairosd` — realtime alerts
- `go run ./cmd/kairos-alert` — one-shot scanner summaries
- `go run ./cmd/kairos-backtest` — local backtests

---

## Directory Layout

```
cmd/
├── kairosd/           # realtime watcher daemon
├── kairos-alert/      # one-shot scanner Telegram alerts
└── kairos-backtest/   # backtest CLI

internal/
├── alert/             # scanner alert HTML formatting
├── backtest/          # backtest engine (OKX OHLCV backward pagination)
├── config/            # YAML config loading
├── data/              # CoinGlass client, RSI map, Python bridge (coinglass_py.go)
├── detector/          # anomaly detectors + resonance scorer
├── engine/            # WS orchestration pipeline → Telegram
├── exchange/          # okx / binance / bybit adapters
├── indicators/        # box pattern + market cycle
├── notify/            # Telegram client + formatting
├── scanner/           # deterministic market scanner
├── types/             # shared types + config structs
└── utils/             # blacklist, market data, zscore, symbol

scripts/               # coinglass_fetch.py (Python decrypt bridge)
tests/                 # cross-package equivalence tests
config/                # config.yaml.example
docs/                  # architecture + trading system docs
deploy/                # deployment assets
```

---

## Data Flow

```
OKX/Binance/Bybit WS feeds
    │
    ▼
engine.Pipeline ─── per-exchange detectors ───→ Telegram (realtime alerts)
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
internal/scanner ──→ internal/alert ──→ Telegram / stdout
```

---

## Naming Conventions

- Go packages: lowercase single word under `internal/`
- Exported types/functions: `PascalCase`
- Unexported helpers: `camelCase`
- CLI binaries live under `cmd/<name>/`
