# Polymarket Weather Bot — AI Agent 指南

## 项目概述

Polymarket 每日天气市场自动交易机器人。基于 Open-Meteo GEFS 集合预报的模型 vs 市场定价偏差策略。

## 目录结构

```
pm/
├── bots/weather/           # 天气 Bot（唯一 bot）
│   ├── core/               # 核心模块
│   ├── strategies/         # 策略
│   ├── backtest/           # 回测
│   ├── cli/                # CLI
│   └── models/             # 数据模型
├── docs/                   # 文档
├── config.toml             # 配置
└── AGENTS.md               # 本文件
```

## 策略

所有策略已删除 (2026-05-16)。回测显示边际盈利或亏损。

框架保留，等待更好的预报模型。

详见: `.trellis/spec/backend/trading-config.md`

## 共享模块

| 模块 | 用途 |
|------|------|
| `core/weather.py` | Open-Meteo 集合预报 |
| `core/clob.py` | Polymarket CLOB 交易执行 |
| `core/risk.py` | 熔断器（L1/L2/L3）、城市限额、日限额 |
| `core/kelly.py` | Kelly criterion 仓位计算 |
| `core/polymarket.py` | Polymarket 市场数据 API |
| `core/ws.py` | WebSocket 实时价格 |
| `core/observation.py` | METAR 观测值过滤 |
| `core/paper_trade.py` | 纸面交易（SQLite） |
| `core/staged_entry.py` | 按时间分级建仓 |
| `backtest/engine.py` | 回测引擎 |
| `backtest/real_data.py` | 真实历史数据 |
| `backtest/costs.py` | 费用/滑点模型 |

## 开发指南

### 代码风格
- Python 3.12+
- `uv` 管理依赖
- `ruff` 格式化 + `mypy` 类型检查
- 异步优先 (`asyncio`, `httpx`)

### API 端点

| 端点 | 用途 | 认证 |
|------|------|------|
| `https://gamma-api.polymarket.com/markets` | 市场列表 | 无需 |
| `https://clob.polymarket.com/*` | 交易执行 | 需要 API Key |
| `wss://ws-subscriptions-clob.polymarket.com/ws/market` | 实时价格 | 无需 |

### CLI 命令

```bash
pm-bot scan          # 扫描市场机会
pm-bot watch         # 实时监控（WebSocket）
pm-bot trade         # 执行交易
pm-bot settle        # 结算已结束市场
pm-bot orders        # 查看待完成订单
pm-bot backtest      # 回测
pm-bot daemon start  # 后台守护进程
```

---

<!-- TRELLIS:START -->
# Trellis Instructions

These instructions are for AI assistants working in this project.

This project is managed by Trellis. The working knowledge you need lives under `.trellis/`:

- `.trellis/workflow.md` — development phases, when to create tasks, skill routing
- `.trellis/spec/` — package- and layer-scoped coding guidelines (read before writing code in a given layer)
- `.trellis/workspace/` — per-developer journals and session traces
- `.trellis/tasks/` — active and archived tasks (PRDs, research, jsonl context)

If a Trellis command is available on your platform (e.g. `/trellis:finish-work`, `/trellis:continue`), prefer it over manual steps. Not every platform exposes every command.

If you're using Codex or another agent-capable tool, additional project-scoped helpers may live in:
- `.agents/skills/` — reusable Trellis skills
- `.codex/agents/` — optional custom subagents

## Subagents

- ALWAYS wait for all subagents to complete before yielding.
- Spawn subagents automatically when:
  - Parallelizable work (e.g., install + verify, npm test + typecheck, multiple tasks from plan)
  - Long-running or blocking tasks where a worker can run independently.
  - Isolation for risky changes or checks

Managed by Trellis. Edits outside this block are preserved; edits inside may be overwritten by a future `trellis update`.

<!-- TRELLIS:END -->
