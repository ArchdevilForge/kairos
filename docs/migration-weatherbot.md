# Migration: weatherbot → pm-bot

> Absorbed 2026-08-09. Source: `Xeron2000/weatherbot` (archived).

## 结论

weatherbot 是 pm-bot 的前身,两者都是 Polymarket 天气市场机器人。
pm-bot(EMOS 校准多模型集成 + Ladder/Tail/Gopfan2 三策略)是其 successor,
weatherbot 不再作为独立仓库维护,并入 pm-bot。

## 已吸收

| 内容 | 去向 |
|---|---|
| 被动挂单限价(YES: bid + improve_ticks,不越过 ask) | `bots/weather/core/passive_price.py` |
| NO 锚定限价(fair_no − 0.10,步进到 tick 网格) | `bots/weather/core/passive_price.py` |
| 订单策略默认值(GTC/GTD、price_improve_ticks 等) | `DEFAULT_ORDER_POLICY` |
| 上述逻辑的单测 | `tests/test_passive_price.py` |

## 未吸收(理由)

- Kelly/EV 过滤 → pm-bot 已有 `core/kelly.py` + `strategies/emos_edge.py`
- 纸面交易订单生命周期 → pm-bot 已有 `core/paper_trade.py`(DB 持久化)
- status/report/replay CLI → pm-bot 已有 `cli/` 对应命令
- Polymarket/Gamma/CLOB 解析 → pm-bot 已有 `core/polymarket.py` / `core/clob.py`

## 后续接线(未做,保持最小改动)

pm-bot 目前以 `rec.price`(市价)直接挂限价单,`passive_price.py` 尚未接入
策略/执行路径。接入时让 `emos_edge` 的评估和订单 intent 共用
`compute_passive_limit_price`,避免评估用中间价、成交却吃 spread 的偏差。

## 验证

```bash
cd pm-bot
uv run pytest tests/test_passive_price.py -q
```
