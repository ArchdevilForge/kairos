---
scope: backend
---

# PM-Bot $100 小资金激进模式

## 概述

$100 小资金需要激进仓位管理，否则无法产生有意义收益。
核心区别：激进仓位 + 高置信度 edge = 可控风险。

## 资金规则

| 参数 | 值 | 说明 |
|------|-----|------|
| kelly_fraction | 0.50 | Half Kelly |
| max_single_pct | 0.15 | 单笔最大 15% = $15 |
| max_position_usd | $15 | 单笔上限 |
| max_total_pct | 0.80 | 最多 80% 暴露 = $80 |
| cash_reserve_pct | 0.20 | 至少 $20 现金 |
| stop_loss_pct | 0.50 | 亏损 $50 后降为半仓 |

### 破产概率分析

假设 8% edge (model_prob=0.12, price=$0.04)：
- Half Kelly: f* = 0.5 × 8%/96% ≈ 4.2%
- $15 下注 = 15% bankroll > kelly 建议的 4.2%
- 但 max_single_pct=15% 是硬上限，实际 kelly 会小于此

风险缓释：
- gopfan2 只在 edge ≥ 8% 时交易（model_prob 必须确认）
- forecast_arb 只在 mispricing ≥ 15% 时交易
- 赔率是 1:24 到 1:99（$0.01→$0.25-$1.00）
- 连续亏损 6 次才亏 $90，概率 (0.95)^6 ≈ 74%

## 策略参数

### gopfan2

```python
kelly_fraction = 0.50
max_single_pct = 0.15
max_position_usd = 15.0
edge_threshold = 0.08  # model_prob - price ≥ 8%
yes_max = 0.15
```

### forecast_arb

```python
kelly_fraction = 0.50
max_single_pct = 0.15
max_position_usd = 15.0
min_mispricing = 0.15  # model_prob - price ≥ 15%
max_market_price = 0.30
max_per_event = 3
```

## 止损机制

```python
if current_bankroll < initial_bankroll * 0.50:
    # 亏损 50%，降为半仓
    max_single_pct = 0.075
    max_position_usd = 7.50
    max_total_pct = 0.40
```

## 增长路径

| 阶段 | Bankroll | 策略 |
|------|----------|------|
| 起步 | $100 | 激进模式，验证策略 |
| 第一目标 | $300 | 保持激进 |
| 第二目标 | $1000 | 降为 kelly=0.40, max_single=10% |
| 成熟 | $2000+ | 降为 kelly=0.25, max_single=5% |

## 前提条件（必须满足）

1. **模型验证**: 用真实数据回测，确认 edge 存在
2. **EMOS 校准**: 原始 ensemble 欠散 20-40%，必须校准
3. **概率校准**: reliability diagram 确认 model_prob 准确
4. **Edge 阈值**: 不低于 8% (gopfan2) / 15% (forecast_arb)

## 禁止行为

- 用未校准模型直接交易（edge 可能是假的）
- 不验证就实盘
- 赌没有 edge 的市场

---

**最后更新**: 2026-05-14
