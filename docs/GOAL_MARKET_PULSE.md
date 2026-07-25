# Goal: 市场整体异动监控（Market Pulse）

> 状态：Engineering complete (Phase 0–4 tooling); product observation ongoing  
> 核心原则：只有市场从安静切换到整体活跃时才叫醒用户；单币异动只用于解释谁在领涨/领跌。  
> 实施优先级：P0 价格广度 → P1 告警门控 → P2 衍生品确认 → P3 板块扩散。

## 一句话目标

将 Kairos 从「监控某个币突然涨跌」升级为：

> 监控整个加密永续市场何时从安静切换为同步上涨、同步下跌、趋势扩散或系统性高波动，只在真正值得打开盘面时发送 Telegram 告警。

## 回答的三个问题

1. 现在是否值得看盘？
2. 市场整体向上、向下，还是仅局部币种异动？
3. 哪些币最能代表这次市场趋势？

## 事件与状态机

事件：`market_impulse` / `market_trend` / `market_stress` / `market_decay`

状态：

```text
QUIET → IMPULSE_UP|IMPULSE_DOWN → TRENDING_UP|TRENDING_DOWN → DECAY → QUIET
```

反向：`TRENDING_UP` 遇强下跌 impulse 时走 `DECAY → IMPULSE_DOWN`（不直接翻转）。

## 核心指标

- Universe：primary exchange Top-N USDT 永续
- Fresh ratio / valid symbols
- 60s / 180s / 300s 中位收益率
- Up / down breadth（相对 `noiseReturnPct`）
- Median Z（EWMA 波动率）
- BTC/ETH 核心确认
- Leaders / laggards（相对市场中位）

## 阶段

| Phase | 目标 | 状态 | 验收要点 |
| --- | --- | --- | --- |
| 0 | 基线 | done | `docs/research/MARKET_PULSE_BASELINE.md` |
| 1 | Shadow Mode | done | 计算 + 日志 + 测试；默认 shadow |
| 2 | 市场告警 | done | format + policy + allow-list 含 market_* |
| 3 | 单币门控 | done | `gateIndividualAlertsWhenQuiet`（默认 off） |
| 4 | 回放校准 | tooling done | `market_outcome` + outcomes JSONL；参数观察仍需生产 |
| 5 | 衍生品 enrichment | out of scope | OI / 量 / 费率 / 爆仓（非硬门槛） |
| 6 | 板块 impulse | non-blocking | |

## 工程约束

- 不新增微服务 / Redis / Kafka
- 不引入 LLM 生产判断
- 复用 ticker → aggregator → alert policy → Telegram
- 可配置关闭：`marketPulse.enabled: false`
- 单币门控独立开关：`gateIndividualAlertsWhenQuiet`

## 推荐配置

Shadow（安全首跑）：

```yaml
marketPulse:
  enabled: true
  shadowMode: true
```

生产市场告警（Phase 2+）：

```yaml
marketPulse:
  enabled: true
  shadowMode: false
  freshnessSeconds: 30
  volatility:
    enabled: false
  gateIndividualAlertsWhenQuiet: true   # Phase 3

alertPolicy:
  allowedEventTypes:
    - "price_velocity"
    - "market_impulse"
    - "market_trend"
    - "market_stress"
    - "market_decay"
```

## 成功标准（摘要）

- 工程：`make check`、race 测试、断流/universe 刷新不误触发
- 产品（≥7 天观察）：市场告警 1–8 条/天；Telegram 总量下降 ≥50%；主观有效 impulse ≥60%

## 相关文件

- Detector：`internal/detector/market_pulse.go`
- Config types：`internal/types/market_pulse.go`
- Baseline：`docs/research/MARKET_PULSE_BASELINE.md`
- Tuning log：`docs/research/MARKET_PULSE_TUNING.md`（Phase 4）
