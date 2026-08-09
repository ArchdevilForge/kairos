# pm-bot Phase 3: Fully Automated 24/7 Trading

## Goal

将 pm-bot 从半自动交易升级为全自动量化交易系统：Kelly 公式仓位管理、多源天气预报聚合、24/7 守护进程运行、SQLite 持久化 + 风控系统。

## Requirements

### 3A: Kelly 公式仓位管理

- **Quarter-Kelly (0.25x)**：预测市场标准做法，全 Kelly 在有估计误差时产生 50%+ 回撤
- **二值公式**：`f* = edge / payout_if_correct`，其中 `edge = p_true - yes_price`，`payout_if_correct = 1 - yes_price`
- **多桶公式**：对温度桶市场（negRisk），每个桶独立计算 Kelly 分数，总暴露不超过 bankroll 的 X%
- **硬上限**：单笔 $50，单日 $200，单城市 $100，总暴露 ≤ bankroll 的 30%
- **配置**：
  ```toml
  [sizing]
  kelly_fraction = 0.25     # quarter-Kelly
  max_single = 50.0
  max_daily = 200.0
  max_per_city = 100.0
  max_total_pct = 0.30
  bankroll = 500.0
  ```

### 3B: 多源天气预报聚合

- **数据源优先级**（免费组合）：
  1. Open-Meteo（HRRR day-of + GFS/ECMWF）— 主源
  2. NWS API (weather.gov) — US 城市二级预报 + 站点观测
  3. AWC METAR — 机场站实时温度（用于 resolution 确认）
- **聚合方法**：
  - BMA（Bayesian Model Averaging）加权共识概率
  - 源一致性因子：3+源一致 → edge × 1.5~2.0 置信
  - 源分歧 → 降低 Kelly 分数（÷ 分歧因子）
- **数据模型扩展**：
  - `ForecastResult` 新增 `sources: dict[str, SourceForecast]`
  - `SourceForecast`: source_name, temp_low, temp_high, confidence, weight
  - `consensus_prob`: 加权共识概率
  - `agreement_score`: 0-1 源一致性分数
- **缓存策略**：每源独立 TTL，METAR 5min，NWS 15min，Open-Meteo 1h

### 3C: 24/7 守护进程

- **`pm-bot daemon start`**：启动后台守护进程
  - 主循环：scan → compute Kelly → auto-trade → sleep(interval)
  - 默认间隔 300s（5分钟）
- **`pm-bot daemon stop`**：优雅停机
  - SIGTERM → 取消所有挂单 → 持久化状态 → 发通知 → 退出
  - 等待 pending fills（最多 30s）
- **`pm-bot daemon status`**：显示运行状态
  - PID, uptime, 今日 P&L, 当前挂单数, 下一扫描时间
- **信号处理**：
  - SIGTERM/SIGINT → 优雅停机
  - SIGUSR1 → 重新加载 config.toml
- **健康检查**：每 60s 写 heartbeat 文件（`~/.pm-bot/heartbeat`）
- **崩溃恢复**：重启后从 SQLite 恢复状态，与 Polymarket API 对账

### 3D: SQLite 持久化

- **数据库位置**：`~/.pm-bot/pm-bot.db`
- **Schema**：
  ```sql
  CREATE TABLE trades (
    id INTEGER PRIMARY KEY,
    order_id TEXT UNIQUE,
    market_id TEXT,
    condition_id TEXT,
    strategy TEXT,
    side TEXT,          -- 'YES' or 'NO'
    price REAL,
    amount_usd REAL,
    kelly_fraction REAL,
    fill_status TEXT,   -- 'open','partial','filled','cancelled'
    edge REAL,
    created_at TEXT,
    filled_at TEXT,
    cancelled_at TEXT
  );

  CREATE TABLE positions (
    id INTEGER PRIMARY KEY,
    condition_id TEXT UNIQUE,
    market_id TEXT,
    city TEXT,
    strategy TEXT,
    side TEXT,
    total_shares REAL,
    avg_price REAL,
    unrealized_pnl REAL,
    realized_pnl REAL,
    updated_at TEXT
  );

  CREATE TABLE daily_state (
    id INTEGER PRIMARY KEY,
    date TEXT UNIQUE,
    total_spent REAL,
    total_pnl REAL,
    trade_count INTEGER,
    bankroll_start REAL,
    bankroll_end REAL
  );

  CREATE TABLE daemon_state (
    key TEXT PRIMARY KEY,
    value TEXT
  );
  ```
- **每日重置**：UTC 00:00 重置 `daily_spent` 计数器
- **P&L 追踪**：每笔成交更新 position 和 daily_state

### 3E: 风控系统

- **三级熔断**：
  1. Level 1（警告）：单日亏损 > bankroll 的 5% → 减半 Kelly，通知
  2. Level 2（减速）：单日亏损 > bankroll 的 10% → quarter Kelly，仅通知
  3. Level 3（停止）：单日亏损 > bankroll 的 15% → 暂停所有新开仓，仅平仓
- **时间风控**：
  - 结算前 6h 不开新仓（温度市场通常美东午夜结算）
  - 不在开盘前 1h 下单（流动性不足）
- **单城市暴露上限**：同城市所有仓位 ≤ config `max_per_city`
- **连续亏损检测**：5 笔连续亏损 → 暂停 1h + 通知
- **异常市场检测**：spread > 10¢ → 跳过该市场

### 3F: 通知增强

- **Discord/Telegram 通知扩展**：
  - 开仓/平仓/取消 → 立即通知
  - 每日 P&L 汇总（UTC 00:00）
  - 熔断触发 → 立即通知
  - 守护进程启动/停止 → 通知
  - 崩溃恢复 → 通知
- **通知格式**：
  ```
  🔴 [L3 CIRCUIT BREAKER] Daily loss 8.2% > 15% threshold
  ⚸ Pausing new positions. Open positions will be managed normally.
  Bankroll: $485.50 | Today P&L: -$41.00
  ```

## Acceptance Criteria

- [ ] `pm-bot daemon start` 启动 24/7 自动交易
- [ ] `pm-bot daemon stop` 优雅停机（取消挂单 + 持久化 + 通知）
- [ ] `pm-bot daemon status` 显示运行状态 + P&L
- [ ] Kelly 公式计算仓位大小（quarter-Kelly, 0.25x）
- [ ] 硬上限生效：单笔 $50, 单日 $200, 单城市 $100
- [ ] 多源聚合：Open-Meteo + NWS + METAR，BMA 加权
- [ ] 3+源一致时 edge 置信提升
- [ ] 源分歧时 Kelly 分数降低
- [ ] SQLite 持久化：trades/positions/daily_state/daemon_state
- [ ] 崩溃恢复：重启后与 Polymarket API 对账
- [ ] 每日 UTC 00:00 重置计数器 + P&L 汇总通知
- [ ] 三级熔断生效（5%/10%/15%）
- [ ] 结算前 6h 不开新仓
- [ ] 连续 5 笔亏损 → 暂停 1h + 通知
- [ ] SIGTERM → 取消挂单 + 持久化 + 通知 + 退出
- [ ] SIGUSR1 → 重新加载 config.toml
- [ ] 通知覆盖：开仓/平仓/熔断/每日汇总/守护进程事件
- [ ] ruff + mypy 零错误
- [ ] 无硬编码凭证

## Definition of Done

- 所有 Acceptance Criteria 通过
- `uv run pm-bot daemon start` 端到端可用
- SQLite 正确持久化所有交易和状态
- 风控系统在模拟条件下正确触发
- ruff + mypy 零错误
- 无硬编码凭证

## Technical Approach

### 新增项目结构

```
pm_bot/
  core/
    kelly.py           # Kelly 公式 + 仓位计算
    aggregation.py     # 多源预报聚合 + BMA
    sources/
      __init__.py
      nws.py           # NWS API 客户端
      metar.py         # AWC METAR 客户端
    db.py              # SQLite 持久化层
    risk.py            # 风控系统（熔断 + 限制检查）
  cli/
    daemon.py          # daemon start/stop/status 命令
  models/
    forecast.py        # SourceForecast, ConsensusForecast 数据模型
```

### 新增依赖

- `aiosqlite>=0.20` — 异步 SQLite（与现有 async httpx 配合）
- `apscheduler>=3.10` — 定时任务（每日重置、定期扫描）
- 无新天气 API 依赖（NWS/METAR 均为免费 HTTP API）

### 守护进程架构

```
daemon start
  → 初始化 ClobClient + DB + WebSocket
  → 从 DB 恢复状态
  → 与 Polymarket API 对账
  → 主循环：
    → scan markets（多源聚合）
    → compute edges + Kelly sizes
    → 风控检查（熔断/限制/时间）
    → auto-trade（下单 + 通知）
    → update positions + P&L in DB
    → sleep(interval)
  → 信号处理：
    → SIGTERM: cancel_all → persist → notify → exit
    → SIGUSR1: reload config
```

### Kelly 计算流程

```python
def kelly_size(edge: float, yes_price: float, bankroll: float, kelly_fraction: float = 0.25) -> float:
    payout_if_correct = 1.0 - yes_price
    full_kelly = edge / payout_if_correct
    fraction_kelly = full_kelly * kelly_fraction
    size_usd = bankroll * fraction_kelly
    return min(size_usd, max_single)  # hard cap
```

### 多源聚合流程

```python
async def compute_consensus(city: str, date: str) -> ConsensusForecast:
    sources = await asyncio.gather(
        fetch_open_meteo(city, date),
        fetch_nws(city, date),
        fetch_metar(city),
    )
    # BMA weighting
    weights = compute_bma_weights(sources)
    consensus = weighted_average(sources, weights)
    agreement = compute_agreement_score(sources)
    return ConsensusForecast(
        temp_low_c=consensus.low,
        temp_high_c=consensus.high,
        prob_by_bucket=consensus.probabilities,
        agreement_score=agreement,
        sources={s.name: s for s in sources},
    )
```

## Decision (ADR-lite)

**Context**: Phase 2 已验证半自动交易可行，需升级为 24/7 全自动
**Decision**: Quarter-Kelly (0.25x) + SQLite + 三级熔断 + BMA 多源聚合
**Consequences**: Quarter-Kelly 在 edge 估计有误差时更稳健；SQLite 简单可靠无需外部 DB；三级熔断防止黑天鹅；多源聚合显著提升 edge 可靠度

## Out of Scope

- Web UI / 移动端
- 回测框架（Phase 4）
- 多用户/账户系统
- 链上结算监控
- 跨市场套利（非天气市场）

## Research References

- [`research/kelly-risk-management.md`](research/kelly-risk-management.md) — Quarter-Kelly 公式、三级熔断、8 个开源 bot 对比
- [`research/multi-source-weather.md`](research/multi-source-weather.md) — Open-Meteo+NWS+METAR 最优组合、BMA 聚合、一致性因子
- [`research/daemon-persistence.md`](research/daemon-persistence.md) — 现有 watch.py 循环模式、SQLite schema、SIGTERM 处理、崩溃恢复
