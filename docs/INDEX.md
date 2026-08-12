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
| Observe | MarketPulse / kairos-oiscan / floors(meme,pm,solana,onchain,launch) / data-sources | `internal/detector`, `cmd/kairos-oiscan`, `floors/`, `data-sources/` |
| Interpret | CycleMap | `internal/cycle` |
| Select | Directional Ranker | `internal/ranker` |
| Decide | Playbook → Decision Ticket → BehaviorGate（教义 9b 机械执行） | `internal/playbook`, `internal/opportunity`, `internal/decision` |
| Execute | Human（manual，经 kairos-desk 记录决策）；trader 仅执行辅助 | `cmd/kairos-desk`, `tools/trader` |
| Learn | Journal → Counterfactual → EV Attribution → Gate 合规报告 | `internal/storage`, `internal/evaluation`, `cmd/kairos-eval` |

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

> 第一性原理：仓库的价值 = 闭环转速（观点 → 数据 → EV → canonical → ticket → 复盘）。
> 采集器已经够多，当前瓶颈不是写更多代码，而是**让闭环真正跑起来**：假设按样本量出判定、每笔交易过 gate。
> 数据分两类：**链上事件永久可重放**（launch/bid/graduation → 按需回填，不需要常驻 daemon）；
> **链下时序易逝**（交易所细粒度 OI 只保留 ~30 天、订单簿、社交热度 → 只有这类才值得持续落盘）。
> 新增任何 floor/工具前先回答两个问题：它属于六步哪一步？现有 floor 的数据被消费了吗？

已完成：

- [x] P0: schemas/event.v1.json → 各 floor 统一契约（bus Event + oiscan 已对齐）
- [x] P0: make check-all + Python CI matrix（10 个 Python 子项目）
- [x] P1: Journal metadata（git_sha/config_hash/strategy_version/experiment_id/mode）
- [x] P1: Alpha Attribution（SelectionAlpha + HumanAlpha；Entry/Exit 拆分待数据积累）
- [x] P2: JSONL → DuckDB research layer（tools/research/）
- [x] P2: 每个 floor 加 manifest.yaml（状态/数据源/事件/执行权限）
- [x] P0: Launch 数据管道 + 行为 Risk Gate 机械化（2026-08-12 交付）→ `GOAL_LAUNCH_DATA_AND_RISK_GATE.md`

进行中（运营闭环，不是写新代码）：

- [ ] P0: launch 历史回填 + 研究前增量补扫（`watch --once` 断点续扫，本地按需跑；监控 `launch_scan_gap` 缺口）
- [ ] P0: 每笔真实交易走 `kairos-desk` 决策（BehaviorGate 机械执法），复盘进 `50-reviews/`
- [ ] P1: H-005 拍卖样本 ≥100 场后跑 `tools/research h005` 出判定（promote/reject）；H-006/H-007 同理
- [ ] P1: 首个 playbook 积累数据后扩展第二/第三 playbook
- [ ] P3: Rust / ML（暂无必要）
