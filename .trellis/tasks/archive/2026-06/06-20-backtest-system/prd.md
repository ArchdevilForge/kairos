# brainstorm: 回测系统

## Goal

为 kairos 交易系统添加历史回测能力：用历史 OHLCV 数据重跑信号检测逻辑（box breakout + cycle + S/R），模拟入场/止损/止盈，统计 P&L、胜率、盈亏比等指标，验证策略有效性。

## What I already know

* kairos 当前是实时信号系统：通过 WebSocket/Webhook 推送异常事件给 Hermes Agent 做 LLM 判断
* 核心信号来源：box pattern breakout（箱体突破）、market cycle（春夏秋冬）、support/resistance
* 策略逻辑在 `scanner.py` 的 `MarketScanner` 中：`_score_candidate` 打分 → `_structure` 识别箱体 → `_setup` 计算入场/止损/止盈 → `_action_state` 判断 trade_candidate
* OHLCV 数据通过 CCXT `fetch_ohlcv` 获取，已在 scanner 中使用
* 目前没有历史数据存储层，所有数据依赖 CCXT 实时拉取
* 项目用 pytest 测试，已有 632 tests, 89% coverage
* 依赖：ccxt, numpy, pyyaml, anyio, httpx 等
* 没有 pandas 依赖，但有 numpy

## Assumptions (temporary)

* 回测目标：验证 scanner 的策略信号在历史上是否有效
* 回测范围：可能是单 symbol 或批量 symbol
* 数据来源：CCXT 历史 OHLCV（大多数交易所提供 1d/4h/1h 等）
* 不需要实时数据回放，静态历史数据即可

## Decisions Made
* MVP 范围：单 symbol 精细回测（逐笔交易明细 + 汇总统计）
* 技术方案：手写 numpy，复用 scanner 方法，零新依赖
* 数据来源：CCXT 实时拉取历史 OHLCV

## Open Questions

（无——所有关键决策已确认）

## Requirements

* 对指定 symbol 在历史区间上逐 K 线运行 scanner 策略
* 模拟入场/止损/止盈逻辑，记录每笔交易明细
* 输出关键统计：总交易数、胜率、平均盈亏比、最大回撤、总收益
* CLI 命令入口：`kairos backtest --symbol BTC/USDT --start 2024-01-01 --end 2024-06-01`
* 数据通过 CCXT `fetch_ohlcv` 实时拉取（翻页取完整区间）
* 复用 scanner 的 `_structure`、`_setup`、`_score_candidate` 方法
* 现有 scanner 的 `_fetch_ohlcv` 已支持翻页，可直接复用

## Acceptance Criteria

* [ ] `kairos backtest --symbol BTC/USDT --start 2024-01-01 --end 2024-06-01` 可运行
* [ ] 输出包含每笔交易的 symbol、方向、入场价、出场价、PnL%
* [ ] 输出汇总统计：总交易数、胜率、平均盈亏比、最大回撤、总收益%
* [ ] 无新 pip 依赖
* [ ] 测试覆盖：mock OHLCV 数据的回测逻辑

## Acceptance Criteria (evolving)

* [ ] 能对 BTC/USDT 在指定历史区间上运行回测
* [ ] 输出包含交易明细和汇总统计
* [ ] 测试覆盖核心回测逻辑

## Definition of Done (team quality bar)

* Tests added/updated (unit/integration where appropriate)
* Lint / typecheck / CI green
* Docs/notes updated if behavior changes
* Rollout/rollback considered if risky

## Out of Scope (explicit)

* 批量多 symbol 回测
* 参数扫描/优化
* 实时回放/paper trading
* 图表可视化
* 本地数据缓存/存储
* Hermes 集成（纯 CLI 工具）

## Research Notes

### 主流回测方案对比

| 方案 | 复杂度 | 新依赖 | 适合度 |
|------|--------|--------|--------|
| **手写 numpy 向量化** | 低 | 无 | ⭐⭐⭐ 最佳 |
| backtesting.py 轻量库 | 中 | backtesting | ⭐⭐ |
| pandas 批量处理 | 中 | pandas | ⭐⭐ |
| NautilusTrader 事件驱动 | 高 | nautilus_trader | ⭐ 过度 |
| Freqtrade 框架 | 高 | freqtrade | ⭐ 过度 |

### 手写方案核心思路
1. CCXT `fetch_ohlcv` 拉取历史数据（已有此能力）
2. 逐 K 线（或每 N 根）调用 scanner 的 `_structure` + `_setup` 做信号检测
3. 信号触发后模拟入场 → 跟踪止损/止盈 → 记录盈亏
4. 输出统计：总交易数、胜率、平均盈亏比、最大回撤、总收益

### Constraints from repo
* 已有 numpy + ccxt，无需新增依赖
* scanner 方法已封装良好，可直接复用
* OHLCV 数据格式统一为 `{"opens": np.ndarray, ...}`

## Feasible Approaches

**Approach A: 手写 numpy 回测（推荐）**
* 无新依赖，复用 scanner 现有方法
* 逐 bar 循环检测信号 + 模拟交易
* CLI: `kairos backtest --symbol BTC/USDT --start 2024-01-01 --end 2024-06-01`
* 输出 JSON 统计报告

**Approach B: 引入 backtesting.py**
* 轻量单文件库，提供策略基类和图表
* 需适配 scanner 到其 Strategy 接口
* 有新依赖

**Approach C: pandas 向量化**
* DataFrame 批量计算指标
* 需引入 pandas 并改写 numpy 逻辑

## Technical Approach

* 新建 `src/kairos/backtest.py`：`BacktestRunner` 类
* 核心流程：
  1. CCXT 拉取历史 OHLCV → numpy arrays
  2. 逐 bar 构造滑动窗口数据，调用 scanner 的信号检测方法
  3. 检测到 trade_candidate 信号 → 模拟入场
  4. 每 bar 检查止损/止盈条件 → 触发则出场
  5. 记录每笔交易 → 输出统计
* 复用 scanner 的 `_structure()`、`_setup()` 方法
* 由于 scanner 的方法依赖完整的 `MarketScanner` 实例和 exchange wrapper，需要注入 mock exchange 或直接调用底层分析模块
* CLI 入口：在 `mcp_server.py` 同级加子命令，或独立脚本

## Implementation Plan (small PRs)

* **PR1**: 核心回测引擎 `BacktestRunner`（数据拉取 + bar 循环 + 交易模拟）+ 测试
* **PR2**: CLI 入口（`kairos backtest` 命令）+ JSON 输出 + 集成验证

## Decision (ADR-lite)

**Context**: 需要验证 scanner 策略在历史数据上的有效性
**Decision**: 手写 numpy 回测引擎，复用 scanner 分析逻辑，CCXT 实时拉数据
**Consequences**: 零新依赖，代码量最小（预计 150-200 行核心逻辑）；不支持批量/参数扫描（后续按需添加）
