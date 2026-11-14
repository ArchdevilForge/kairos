# Agent Instructions

> Scope: This file is the entrypoint. It keeps only always-on rules; task details live in docs/.

## Project Overview

kairos is a human-controlled cryptocurrency futures alert system.

It scans futures markets, detects hard-data anomalies, scores deterministic setup candidates, and pushes Telegram alerts. It does not use LLMs and does not place orders.

## Core Principles

- **Research first**: Unfamiliar tech/architecture → research before coding
- **Tool priority**: use fast code search first, built-in tools for web search
- **Reasoning**: Complex → deep thinking, simple → direct response
- **Code quality**: Handle errors, add type annotations, document large files

## Read-On-Demand Index

| Task type | Read first | Trigger |
| --- | --- | --- |
| 文档地图 | `docs/INDEX.md` | 权威链 + 六步主干 + 知识目录 |
| 教义/策略 | `docs/10-doctrine/KAIROS_DOCTRINE.md` | canonical freeze: 工作流 + 硬规则(唯一权威) |
| 模型 | `docs/20-models/` | MarketPulse / CycleModel |
| 工程架构 | `docs/90-engineering/adr/` | 架构决策记录 |
| Config changes | `config/config.yaml.example` | Detector thresholds, risk gate, Telegram settings |

## Trading System Boundary

### Architecture

- **kairosd** (`cmd/kairosd`): realtime WebSocket anomaly watcher → Telegram
- **kairos-desk** (`cmd/kairos-desk`): human decision CLI; accept 前经 BehaviorGate（教义 9b 行为红线）机械判定
- **kairos-eval** (`cmd/kairos-eval`): journal EV attribution + gate 合规报告
- **kairos-calibrate** (`cmd/kairos-calibrate`): events + outcomes + 60s snapshots → lift_5m 校准
- **kairos-market-replay** (`cmd/kairos-market-replay`): MarketPulse 影子算法回放
- **kairos-oiscan** (`cmd/kairos-oiscan`): 全市场 OI 异动扫描 → data/inbound/futures
- **human**: final chart review, trade selection, sizing, entries, exits

### CLI Commands
```bash
go build -o kairosd ./cmd/kairosd
go build -o kairos-desk ./cmd/kairos-desk
go build -o kairos-eval ./cmd/kairos-eval
go build -o kairos-calibrate ./cmd/kairos-calibrate
go build -o kairos-market-replay ./cmd/kairos-market-replay
go build -o kairos-oiscan ./cmd/kairos-oiscan   # 全市场 OI 异动扫描 → data/inbound/futures

./kairosd --config config/config.yaml               # Realtime hard-data Telegram alerts
go run ./cmd/kairos-desk gate <ticket-id>           # BehaviorGate dry-run 判定
go run ./cmd/kairos-eval gate                       # gate 合规报告
make check                                          # build + vet + lint + test -race
make check-python                                   # tools/research
```

### Risk Constraints
- Altcoins: 33% position, max 5x leverage
- BTC/ETH: 33% position, max 10x leverage
- Risk gate output is a bound, not a trade instruction
- No automatic order placement

## Always-On Rules

- **Language**: Default to Chinese for user communication
- **Evidence**: Verify high-risk claims before citing
- **Efficiency**: Batch operations, avoid redundant reads
- **Commit messages**: Conventional Commits — `type(scope): description`, e.g. `feat(detector): add volume confirmation gate`. Types: feat/fix/refactor/docs/test/chore. Scope is the module name (detector, decision, engine, etc). Keep under 72 chars. If you need more detail, use the body (blank line after subject).

## Priority

1. User's current explicit instruction.
2. Nearest project instruction file.
3. This file.
4. Routed docs details.
