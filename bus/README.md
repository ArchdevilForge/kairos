# kairos-bus

交易套件的整合层(事件总线)。各交易楼层(预测市场 / 合约 / 链上)落 JSONL,
bus 统一做 severity 门控、dedup、cooldown,Telegram 统一推送,输出聚合 JSONL
给 calibration 和内容管线。

> 定位:不是监控程序,是**事件总线**。kairos(Go,合约楼层)不动,
> kairos-bus(Python)把各楼层的事件纪律收口到一层。

## 架构

```text
pm-bot / smartalpha / rh-sniper / kairos ...
    │  各楼层往 inbound/<floor>/*.jsonl 追加事件
    ▼
kairos-bus
    ingest(增量 tail) → gate(severity / dedup / cooldown / blacklist)
    → Telegram 推送(仅通过的) → out/YYYY-MM-DD.jsonl(所有事件 + gate 结果)
    ▼
calibration(复盘) / 内容管线(赌场观察日报)
```

- **Telegram 只发通过门控的事件;聚合 JSONL 记录全部事件(含被拒的)** ——
  calibration 需要完整事件流,推送只需要高信号。与 kairos 一致。
- 密钥不进仓库:Telegram token 从环境变量 `KAIBUS_TG_TOKEN` / `KAIBUS_TG_CHAT` 读。

## 告警纪律(1:1 复制自 kairos internal/engine/pipeline.go)

1. **事件类型 allow-list**:每楼层 `allowed_event_types`,不在列表直接拒。
2. **severity 门控**:每楼层 `min_severity`(LOW/MEDIUM/HIGH)。
3. **blacklist**:每楼层 symbol 黑名单。
4. **dedup**:键 = `floor + event_type + key`,`dedup_window_seconds`(默认 5s)内
   重复丢弃;attempt 级,**立即提交** —— 推送失败也限流,防止故障通道被锤爆。
5. **symbol cooldown**:同 symbol 默认 1800s,**发送成功后才提交** —— notifier
   短暂故障不烧掉长冷却。
6. **skip_cooldown_event_types**(市场级事件)不占 symbol cooldown —— 市场级
   有 direction 感知的去重键,单边冲动不应压掉几分钟后的反向信号。
7. **never_notify_event_types**(outcome / calibration)永不推送,只进聚合 JSONL。
8. **attention budget**:bus 级每日推送上限 `max_alerts_per_day`。

所有门控拒绝都有结构化原因(`dedup` / `cooldown` / `severity` / `event_type` /
`blacklist` / `never_notify` / `attention_budget` / `unknown_floor`),写入聚合 JSONL。

## 事件格式

每楼层往 `inbound/<floor>/*.jsonl` 追加,每行一个 JSON:

```json
{
  "ts": "2026-08-09T15:04:05Z",
  "event_type": "spread_alert",
  "severity": "HIGH",
  "key": "fed-rate-sep-poly-kalshi",
  "symbol": "FED-RATE-SEP",
  "title": "Polymarket/Kalshi 同事件价差 12¢",
  "message": "价差 12¢ > 阈值 8¢",
  "data": {"spread": 0.12, "threshold": 0.08}
}
```

- `key`:去重键(同 event_type 内),`symbol`:冷却键。
- `floor` 默认取目录名,行内可覆盖。
- 未知字段自动进 `data`,不丢弃。

## 使用

```bash
uv sync
kairos-bus run --config config.toml        # 轮询循环(生产)
kairos-bus run --once                      # 单轮(调试 / cron)
kairos-bus status                          # 楼层与门控配置
```

## 测试

```bash
uv run pytest tests/ -q
```

## 楼层接入(config.toml)

```toml
[floors.pm]
source = "pm"                              # 相对 inbound_dir
allowed_event_types = ["spread_alert", "edge_alert"]
min_severity = "MEDIUM"
blacklist = []
skip_cooldown_event_types = []
never_notify_event_types = ["outcome", "calibration"]
```

## 路线

- [x] 骨架:config 驱动楼层接入 + 统一告警纪律 + 聚合 JSONL 输出
- [ ] Module 1:Polymarket/Robinhood/Kalshi 同事件价差扫描器接入(> 阈值 → spread_alert)
- [ ] 内容钩子:每日聚合 JSONL → AIGC 管线 →「赌场观察」日报
- [ ] 楼层 SDK/示例写入器(pm-bot / smartalpha 侧落 JSONL 的小工具)
