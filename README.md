# kairos

> καιρός — 关键时刻，恰当时机

Crypto futures anomaly alert system. 确定性的链上门槛数据监控。

## Architecture

```text
Exchange WebSocket  ──→  价格/成交量/OI/资金费率检测器
                             ↓
CoinGlass API  ──→  多空比/爆仓检测器  ──→  共振评分器  ──→  Telegram
```

六维异动 + Z-score 动态阈值 + 多维度共振聚合。

## Commands

```bash
# Realtime watcher
TELEGRAM_BOT_TOKEN=xxx TELEGRAM_CHAT_ID=xxx uv run kairos-watch

# One-shot scanner
uv run kairos-alert [--dry-run]

# Backtest
uv run kairos-backtest --help
```

## Alerts

| 维度 | 来源 | 检测方式 |
|---|---|---|
| `price_velocity` | WebSocket | Z-score / 多窗口滑动 |
| `volume_spike` | WebSocket | Z-score / 滚动基线 |
| `open_interest_change` | REST | Z-score / 变化率 |
| `funding_rate_anomaly` | REST | Z-score / 绝对值 |
| `long_short_ratio` | CoinGlass | 绝对阈值 + Z-score + 变化速度 |
| `liquidation` | CoinGlass | 金额阈值 + Z-score + 多空主导判定 |
| `resonance` | 聚合 | 信号质量分 ≥55（基于 Z-score 极端度 + 维度共振 + 方向一致性） |

Z-score 滚动窗口自适应波动率 regime，不依赖固定阈值。共振评分在多个维度同时异动时产生一级/二级/三级警报，显著降低噪音。

Scanner 输出 `watch`/`prepare`/`trade_candidate` 状态、评分、入场区间、止损、目标、风险回报比。

## Configuration

```bash
export TELEGRAM_BOT_TOKEN="..."
export TELEGRAM_CHAT_ID="..."
```

可选:
```bash
export KAIROS_ALERT_MIN_STATE=prepare
export KAIROS_ALERT_LIMIT=5
```

## Philosophy

顺势而为 · 敬畏市场 · 严格止损 · 人工决策

## License

MIT
