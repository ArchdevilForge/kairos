# Kairos 文档索引

> 阅读顺序建议：`00-system/NORTH_STAR.md` → `00-system/AUTHORITY.md` → `10-doctrine/KAIROS_DOCTRINE.md` → 按需下钻。

## Authority Chain

| 层 | 文档 | 角色 |
|---|---|---|
| 北星 | `00-system/NORTH_STAR.md` | 仓库定位：Personal Trading Research & Decision OS |
| 权威图 | `00-system/AUTHORITY.md` | 谁赢谁、知识生命周期 |
| 领域语言 | `CONTEXT.md`（根） | 术语表（Realtime Anomaly / Decision Ticket / Alert Gate ...） |
| 教义 | `10-doctrine/KAIROS_DOCTRINE.md` | **canonical freeze**：完整工作流 + 硬规则（唯一权威） |
| 教义来源 | `10-doctrine/SOURCE_BIT_LANGLANG.md` | Bit浪浪原始交易哲学（Source 层，非规则） |
| 模型 | `20-models/MARKET_PULSE.md` | Attention：何时值得看盘 |
| 模型 | `20-models/CYCLE_MODEL.md` | Interpret：Context/Setup/Trigger 三时间尺度 |
| Playbook | `30-playbooks/LEADER_PULLBACK.md` | Decide：第一个 playbook |
| 工程 | `90-engineering/adr/` | 架构决策记录 |

## 六步主干（加任何东西先问：它属于哪一步？）

```text
Observe  →  Interpret  →  Select  →  Decide  →  Execute  →  Learn
```

| 步骤 | 组件 | 仓库位置 |
|---|---|---|
| Observe | MarketPulse / kairos-oiscan / floors(meme,pm,solana,onchain) / data-sources | `internal/detector`, `cmd/kairos-oiscan`, `floors/`, `data-sources/` |
| Interpret | CycleMap | `internal/cycle` |
| Select | Directional Ranker | `internal/ranker` |
| Decide | Playbook → Decision Ticket | `internal/playbook`, `internal/opportunity` |
| Execute | Human（manual）；trader 仅执行辅助 | `tools/trader` |
| Learn | Journal → Counterfactual → EV Attribution | `internal/storage`, `internal/evaluation`, `cmd/kairos-eval` |

## 交易知识目录

| 目录 | 内容 | 状态 |
|---|---|---|
| `40-research/hypotheses/` | 待验证假设（模板见 `HYPOTHESIS_TEMPLATE.md`） | 骨架 |
| `40-research/experiments/` | 实验记录（shadow/backtest/live-small） | 骨架 |
| `40-research/findings/` | 已验证发现（含 newsliquid 三维度研究、MarketPulse 基线/调参） | 有内容 |
| `40-research/sources/` | 外部来源归档 | 骨架 |
| `50-reviews/` | 交易复盘 / 周报 / 周期复盘 | 骨架 |

## 历史档案

`archive/` 只读，不参与权威裁决：

- `ARCHITECTURE_2026-06-27.md` — 旧权威架构（1d/4h/15m 旧策略模型），已降级
- `architecture-review.md` — 旧架构评审

## 路线图（待办，非权威）

- [ ] P0: schemas/event.v1.json（已建）→ 各 floor 统一契约
- [ ] P0: make check-all + Python CI matrix（已建）
- [ ] P1: Journal metadata（git_sha/config_hash/strategy_version/experiment_id/mode）
- [ ] P1: Alpha Attribution 报告（Attention/Context/Selection/Entry/Exit 五段）
- [ ] P1: 首个 playbook 积累数据后扩展第二/第三 playbook
- [ ] P2: JSONL → Parquet/DuckDB research layer
- [ ] P2: 每个 floor 加 manifest.yaml（状态/数据源/事件/执行权限）
- [ ] P3: Rust / ML（暂无必要）
