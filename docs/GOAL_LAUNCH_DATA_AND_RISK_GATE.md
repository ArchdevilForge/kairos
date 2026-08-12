# Goal: Launch 数据管道 + 行为 Risk Gate 机械化

> 状态：Draft / 可直接执行
> 目标仓库：`ArchdevilForge/kairos`
> 建议路径：`docs/GOAL_LAUNCH_DATA_AND_RISK_GATE.md`
> 核心原则：数据窗口期优先（Pools.trade 2026-08-05 才公开，叙事第 1 个月）；教义只在被代码执行时才有价值。
> 实施优先级：P0 launch collector v2 → P1 行为 Risk Gate → P2 demand 特征 + H-005 判定 → P3 aftermarket outcomes。

---

## 0. 一句话目标

完成 roadmap 的两个 Now 项：

> A. 把 `floors/launch` 采集器升级为覆盖全部 launch 类型、可断点续扫、可长期常驻的
> 数据管道，让 H-005/H-006/H-007 的 shadow 样本从今天开始积累。
> B. 把 KAIROS_DOCTRINE 9b 的 7 条行为硬规则从文档变成 `internal/decision` 里的
> 代码 gate，接入 kairos-desk 与 kairos-eval。

完成后 Kairos 应回答两个新问题：

1. **这个 launch 的拍卖需求特征是什么？**（数据自动落盘，无人值守）
2. **这笔交易是否违反我自己的行为红线？**（ticket 生成时机器判定，不靠自觉）

---

## 1. 背景与第一性原理

### 1.1 为什么是这两件事（第一性原理拆解）

**实测事实（binding）：**

- 交易画像（70 笔 + 25 笔独立样本）证明负 EV 来自行为（单币反复、<5m 剥头皮
  0 胜 -29.76、报复性重进 -29.35、全 Cross），不是信号缺失。
  → 修复行为的 EV 提升是唯一已被实测确认为正的改进。
- 手动 perp 交易样本吞吐 ~70 笔/半年，统计上学不出任何东西；
  Pools.trade 单日 11,437 个 launch（2026-08-05 实测），shadow 采集一周即上万样本。
  → **样本吞吐是知识生产的第一性瓶颈，launch floor 是当前吞吐最高的数据源。**
- Pools.trade 有**两个入口合约同时活跃**（2026-08-05 当天 6,907 + 4,530 分布），
  只监听一个会漏 ~40%（含 FRONG、POOLS 两个最大标的）。
  → 覆盖率是数据资产的第一性约束，漏采 = 选择偏差 = 数据不可用。

**应丢弃的惯例（convention）：**

- "先做告警再做数据"——launch floor 不需要告警，只需要完整的原始事件流。
- "行为纪律靠自觉"——画像已证明自觉不可靠，gate 必须在 ticket 生成路径上。

### 1.2 外部技术事实（2026-08 调研）

| 事实 | 来源 | 对设计的影响 |
|---|---|---|
| Robinhood Chain 块时间 ~100ms | blockmachine 文档 | 现采集器 `sleep(3*30)` 注释"块时间~1s"错误；watch 循环按秒而非按块规划 |
| 公共 RPC 限流、无归档保证、明确不建议生产 | docs.robinhood.com ToS | endpoint 必须可配置 + retry/backoff + 分段 getLogs |
| Alchemy 免费层有 Robinhood Chain wss/http | docs.robinhood.com/chain/connecting | 可选 env `ROBINHOOD_RPC_URL` 覆盖默认公共端点 |
| blockmachine 免 key 标准端点（http+wss） | blockmachine.io/docs/robinhood-rpc | 备用 endpoint，零成本 |
| 入口合约 `0x0000ffffbe8efe702c8703ae3477ff5de3d319c0`（现行）+ `0x00004c4ccc709ef590f7c81102c0689f0263d4e9`（原始，仍活跃） | Bitquery pools.trade API 文档 | 两个都监听，事件签名逐字节一致 |
| `TokenCreated(address)` topic0 `2e2b3f61b70d2d131b2a807371103cc98d51adcaa5e9a8f9c32658ad8426e74e`（入口）；metadata 重载 `4ef8284e...`（factory `0x000000e200088d55c39a11f609e5f667729ad49b`）；`TokenDistributed` `67226bac...` | 同上 | curve/instant launch 的唯一 launch 信号；同名事件两种签名，必须用 topic0 精确过滤 |
| CCA 每 auction 独立合约：`BidSubmitted` topic0 `650baad5...`、`ClearingPriceUpdated` `30adbe99...` | 同上 | H-005 demand 特征（unique_bidders/top5 share/bid growth）直接从这两个事件流构建 |
| 无现成开源采集器；权威地址源是 Uniswap `liquidity-launcher-sdk/src/addresses.ts` | gh 搜索 | 自建；地址变更时对照 SDK |

### 1.3 代码落点（已确认）

- `floors/launch/launch_collector.py`：已有 CCA `AuctionCreated` 扫描 + auction 视图快照，
  缺 TokenCreated（launch 总量大头）、bid 级事件、断点续扫、endpoint 配置。
- `internal/decision/ticket.go`：`BuildTicket` + `DefaultRiskBudgets` 已存在，gate 挂这里。
- `internal/storage/journal.go`：append-only JSONL，kinds
  `session/ticket/decision/outcome/candidates/annotation`，cooldown 判定读 outcome 记录。
- `cmd/kairos-desk`：人工录入入口，gate 违规在此拦截并写 annotation。
- `tools/research/`：DuckDB research 层已存在，H-005 判定脚本放这里，不新建基础设施。

---

## 2. Goal

### Workstream A：launch collector v2（P0 + P2 + P3）

升级 `floors/launch/launch_collector.py`（仍是单文件 + uv，不引入框架）：

1. **TokenCreated 双入口监听**：`eth_getLogs` 按 topic0 `2e2b3f61...` 过滤两个入口合约，
   每个 launch 落一条 `launch_token_created` 事件（kairos.event.v1）。
2. **CCA demand 采集**：对每个活跃 auction 合约订阅/轮询 `BidSubmitted` 与
   `ClearingPriceUpdated`，聚合为每分钟 demand 快照：
   `unique_bidders / top1_share / top5_share / bid_count / clearing_price /
   clearing_over_floor / bid_growth_1m_5m / minutes_to_end`。
3. **RPC 健壮性**：endpoint 从 env `ROBINHOOD_RPC_URL` 读取（默认公共 RPC，
   备用 blockmachine）；指数退避重试；getLogs 范围自适应缩小（被拒绝时折半）。
4. **断点续扫**：`state.json` 持久化 `last_scanned_block`，重启不漏不重
   （event_id 去重键已含 block+address）。
5. **修正块时间假设**：watch 循环按目标延迟（秒）规划，不按块数；
   活跃 auction（4h 窗口内）用 15–30s 快轮询，launch 发现用 60s 慢轮询。
6. **aftermarket outcomes（P3）**：监听 `TokenDistributed` / graduation，
   对已毕业 token 采样 v4 池价格 1m/5m/15m/1h/4h → `launch_outcome` 事件。
   第一版可只记 graduation 时间戳与初始价，价格采样允许后补。

输出统一到 `bus/inbound/launch/YYYY-MM-DD.jsonl`（已有约定），按日轮转。

### Workstream B：行为 Risk Gate（P1）

教义 9b 七条规则逐条机械化，新增 `internal/decision/behavior_gate.go`：

| # | 规则 | 机械化方式 |
|---|---|---|
| 1 | Isolated only | ticket 必填 `margin_mode` 字段，非 isolated → reject |
| 2 | no <5m discretionary | 入场时无法预判，作为事后审计规则（eval 标记违规） |
| 3 | loss → 同币 cooldown 30–60min | 读 journal 最近同 symbol outcome，亏损且 <cooldown → reject（新 Setup ID 可豁免，需显式传入） |
| 4 | direction from CycleMap | ticket 携带 cycle context，方向与 context 冲突且无 rank 支持 → reject |
| 5 | evening live window 18:00–01:00 UTC+8 | 时间窗外 `live_eligible=false`，只允许 shadow/observe |
| 6 | risk sizing $2–4 | `max_loss_usd` 超出预算 → reject（预算读 config，默认 $4） |
| 7 | liquidation ≠ stop | ticket 的 stop 字段为空或等于强平价 → reject |

设计约束：

- Gate 是纯函数：`EvaluateBehaviorGate(input GateInput) GateResult`，
  输入含 now、symbol、direction、ticket 字段、journal 查询结果；无 IO，可单测。
- reject 输出 reason codes（`GATE_CROSS_MARGIN` / `GATE_COOLDOWN` / ...），
  desk 拦截时写 `annotation` 记录到 journal，被拦截的 ticket 标记 `not_eligible`，
  不产生 live ticket 但保留 shadow 记录（counterfactual 数据）。
- `kairos-eval` 增加 gate 合规报告：每笔 outcome 回填规则 2（持仓时长）等
  事后规则，输出 process-grade（A=全合规 … D=多条违规）与 R-multiple 口径统计
  （对齐 journal 最佳实践：入场时打标防事后偏差、process 与 P&L 分离）。
- config 增加 `riskGate:` 节（enabled / cooldownMinutes / eveningWindow /
  maxLossUSD），默认 enabled，可整体关闭回滚。

---

## 3. 非目标

- 不交易、不自动下单（launch floor `execution_permissions: none` 不变）。
- 不接付费 API（Bitquery/Alchemy 付费层）；免费层可选。
- 不做 UI/dashboard；不上 ML/LLM。
- 不改 perp 检测器与 MarketPulse（另有观察任务，人工执行）。
- 不实现 H-006 creator score / H-007 pullback 策略本身（只保证数据字段够用）。

---

## 4. 实施顺序（每个 PR 可独立回滚）

### PR 1（P0）：collector v2 基础

- [ ] TokenCreated 双入口 getLogs（topic0 精确过滤）
- [ ] endpoint env 配置 + 指数退避 + getLogs 范围自适应
- [ ] `state.json` 断点续扫
- [ ] watch 循环按秒规划，修正块时间注释
- [ ] `probe` 子命令扩展：校验两个入口合约 + factory 均有 code
- [ ] 自检：`uv run python launch_collector.py watch --once` 在真实链上跑通，
      当日 JSONL 有 `launch_token_created` 记录

### PR 2（P2）：CCA demand 特征

- [ ] AuctionCreated → 注册活跃 auction，4h 窗口内快轮询
- [ ] BidSubmitted / ClearingPriceUpdated 事件解析
- [ ] 每分钟 demand 快照事件 `launch_auction_update`
- [ ] 单元自检：用一段真实历史区块 scan，断言 demand 快照字段齐全

### PR 3（P1）：BehaviorGate 纯逻辑

- [ ] `internal/decision/behavior_gate.go` + 表驱动单测（7 条规则 × pass/reject）
- [ ] **历史回放校验（必做的 runnable check）**：用交易画像里的已知案例构造
      fixture——ZEC 亏损后 30min 内重进 4 笔必须全部 reject（`GATE_COOLDOWN`），
      12:00–18:00 时段 ticket 必须 `live_eligible=false`
- [ ] config `riskGate:` 节 + 默认值 + config 测试

### PR 4（P1）：desk / eval 集成

- [ ] kairos-desk 录入路径接 gate，reject 写 annotation + not_eligible
- [ ] kairos-eval gate 合规报告（含事后规则 2、process-grade、R-multiple）
- [ ] `make check` 全绿

### PR 5（P3）：outcomes + H-005 判定脚本

- [ ] graduation/TokenDistributed 监听 + 初始价记录
- [ ] 毕业 token 价格采样 1m/5m/15m/1h/4h
- [ ] `tools/research/` 增加 H-005 判定查询：demand 分位分组 × median return / PF，
      输出到 `docs/40-research/experiments/`

---

## 5. 验收与 kill test

**工程验收：**

- `make check` 通过；Python 侧 `uv run pytest`（collector 加最小测试文件）通过。
- collector 连续 watch 48h 无崩溃、无内存增长、断网自动恢复。
- **覆盖率 kill test**：任选一整天，collector 记录的 launch 数与 Bitquery
  公开口径对照，偏差 >5% 即视为覆盖缺陷，必须修复后才算 P0 完成。

**研究验收（goal 完成后 2–4 周，人工判定）：**

- H-005 kill test：top demand 分位 vs 全体 median 1h return 无可辨别差异
  → 假设 reject，launch floor 降级为低维护采集，资源回收到 MarketPulse 校准主线。
- Risk Gate 生效性：4 周后 journal 中 `GATE_COOLDOWN` 拦截次数 > 0 且无违规 live 单。

---

## 6. 默认决策

```text
简单优先于复杂：单文件 collector，不引入 web3.py 之外的新依赖（现有 requests + pycryptodome 已够）
数据不足时不猜测：RPC 失败段记录 gap，不伪造
公共端点优先：不强制要求付费 key
gate 宁可误拦不可漏放：边界情况 reject 并写 reason，人工可覆盖但必须留痕
所有事件 schema_version=kairos.event.v1，mode=shadow
人工决策边界不变：gate 拦截的是 ticket 资格，不是人的最终决定权
```

---

## 7. Goal 执行指令（交给执行 Agent）

```text
Implement docs/GOAL_LAUNCH_DATA_AND_RISK_GATE.md in PR order (PR1 → PR5).

Scope per PR is defined in section 4. Hard rules:
1. Read section 1.2 facts table before touching the collector; contract
   addresses and topic0 hashes there are authoritative (cross-check against
   Uniswap liquidity-launcher-sdk addresses.ts if anything fails on chain).
2. floors/launch stays a single-file uv script; no new deps beyond
   requests + pycryptodome unless strictly necessary.
3. internal/decision/behavior_gate.go must be a pure function with
   table-driven tests covering all 7 rules, plus the ZEC revenge-trade
   replay fixture from docs/40-research/findings/2026-08-09-交易画像分析.md.
4. Every PR: gofmt, go test -race ./..., make check; Python: uv run pytest.
5. Do not: place orders, add ML/LLM, add services/queues, touch MarketPulse,
   modify perp detectors, or require paid API keys.
6. Commit per PR with Conventional Commits (feat(launch)/feat(decision)/...),
   push to main after each green make check.

Return per PR: changed files, test results, known limitations.
Stop conditions: RPC permanently unreachable for >1h during验收 (report and
pause), or coverage kill test fails twice after fixes (pause and report).
```

---

## 8. Goal 之外的人工动作（不进本 goal）

- [ ] 生产主机（ccs）启 `marketPulse.enabled=true, shadowMode=false,
      gateIndividualAlertsWhenQuiet=true`，观察 ≥7 天回填 TUNING（07-19 goal 遗留）。
- [ ] 归档 `07-19-market-pulse-phase1` 任务；决定 `07-26-audit` 做或砍。
- [ ] （可选）注册 Alchemy 免费 key，设 `ROBINHOOD_RPC_URL` 提升采集稳定性。
- [ ] 2–4 周后跑 H-005 kill test 判定，写入 `docs/40-research/experiments/`。
