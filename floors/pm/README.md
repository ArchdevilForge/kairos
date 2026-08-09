# Polymarket Weather Trading Bot

EMOS 校准多模型集成 + 三策略组合，自动交易 Polymarket 天气市场。

## 策略

| 策略 | 权重 | 价格 | 逻辑 |
|------|------|------|------|
| Ladder | 40% | < 20¢ | 相邻桶覆盖 |
| Tail | 30% | < 15¢ | 尾部低估桶 |
| Gopfan2 | 30% | < 15¢ | 简单价格规则 |

盈利原理：买 8-15¢，赢赚 $0.85+，亏损 8-15¢，盈亏比 8-10:1。

## 使用

```bash
uv sync
pm-bot-v2 scan --cities "Chicago,Miami"
pm-bot-v2 train --all --days 45
pm-bot-v2 paper --bankroll 100
```

## 结构

```
bots/weather/core/weather.py      # 多模型集成 + EMOS
bots/weather/strategies/emos_edge.py  # 生产策略
data/emos_coeffs/                 # 14 城市 EMOS 系数
paper_trade.py                    # 纸面交易
```

## 历史

- **2026-08-09**: 吸收 weatherbot(前身,已归档)的被动挂单限价逻辑 → `bots/weather/core/passive_price.py`,详见 [docs/migration-weatherbot.md](docs/migration-weatherbot.md)。

## Disclaimer

For educational purposes only. Trading involves significant financial risk.
