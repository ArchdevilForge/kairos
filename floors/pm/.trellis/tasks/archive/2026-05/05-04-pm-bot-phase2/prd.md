# pm-bot Phase 2: Semi-Auto Trading

## Goal

在 Phase 1 扫描+推荐基础上，接入 Polymarket CLOB 交易能力，实现 `trade --confirm` 一键确认下单；新增窄桶买NO和机场套利策略；接入 WebSocket 实时价格推送；添加交易通知。

## Requirements

### 2A: CLOB 交易集成

- **py-clob-client-v2 集成**：安装 `py-clob-client-v2>=1.0.0`，封装 `ClobClient`
- **两层认证**：
  - L1: 钱包私钥签名（EIP-712）
  - L2: API key + API secret + passphrase（HMAC）
- **凭证管理**：`config.toml` 的 `[clob]` 段存放凭证，`.env` 可覆盖，`POLY_PK` 环境变量存私钥
- **neg_risk 支持**：天气市场下单必须设 `neg_risk=True`
- **订单类型**：GTC（默认）、FOK（立即成交或取消）
- **`pm-bot trade --confirm`**：扫描 → 列出推荐 → 用户 y/n 逐条确认 → 执行下单
- **订单查询**：`pm-bot orders` 查看当前挂单和成交状态
- **安全约束**：单笔最大金额（默认 $5）、单日最大金额（默认 $50）、确认前显示完整订单详情

### 2B: 新策略

- **窄桶买NO策略 (NarrowNoStrategy)**：
  - 对宽度 ≤2°C（或 ≤4°F）的中心桶，若 YES 价 ≥0.45 则买 NO
  - 依据：零售集中在模态结果，窄桶系统性过定价
  - 与 gopfan2 重叠部分统一（gopfan2 的 `no_min=0.45` 逻辑合并到 NarrowNo）
- **机场套利策略 (AirportArbStrategy)**：
  - 修正 CITY_COORDS 为机场气象站坐标（KLGA 非 NYC 市中心）
  - 对比机场站预报 vs 市中心天气 app 温度 → 检测 3-8°F 系统性偏差
  - 在偏差方向上买 YES/NO（零售锚定市中心温度，实际结算在机场站）
  - 需新增 `[stations.ICAO]` 配置段（ICAO 代码、坐标、关联市场）

### 2C: 实时价格推送

- **WebSocket 连接**：`wss://ws-subscriptions-clob.polymarket.com/ws/market`，无需认证
- **订阅事件**：`best_bid_ask`（买一卖一）、`last_trade_price`（成交价）
- **动态订阅**：通过 `subscribe`/`unsubscribe` 操作增删 token ID，无需重连
- **心跳**：10秒 PING，超时自动重连
- **集成到 watch TUI**：替代当前 60s 轮询为实时推送
- **依赖**：`websockets>=12.0`

### 2D: 交易通知

- **Discord Webhook**：优先实现，只需 URL + HTTP POST（用现有 httpx）
- **Telegram Bot**：可选，token + chat_id + POST
- **通知内容**：订单创建/成交/取消 + edge 信息 + 策略名
- **配置**：`config.toml` 的 `[notifications]` 段

### 2E: config.toml 配置

- **使用 `tomllib`（stdlib 3.11+）读取** + `tomlkit` 写入（保留格式/注释）
- **配置层级**：`config.toml` < 环境变量 < CLI flag
- **配置段**：
  ```toml
  [clob]
  api_key = ""
  api_secret = ""
  api_passphrase = ""
  # 私钥用环境变量 POLY_PK

  [strategies.gopfan2]
  yes_max = 0.15
  no_min = 0.45

  [strategies.narrow_no]
  max_bucket_width_c = 2.0
  no_min = 0.45

  [strategies.airport_arb]
  min_delta_f = 3.0

  [sizing]
  max_single = 5.0    # 单笔最大 USD
  max_daily = 50.0    # 单日最大 USD

  [notifications.discord]
  webhook_url = ""

  [notifications.telegram]
  bot_token = ""
  chat_id = ""

  [stations.KLGA]
  name = "LaGuardia Airport"
  lat = 40.7769
  lon = -73.8740
  city = "New York"

  [stations.KMIA]
  name = "Miami International"
  lat = 25.7959
  lon = -80.2870
  city = "Miami"
  ```
- **`pm-bot config` 更新**：显示当前 config.toml + 环境变量覆盖情况
- **`pm-bot config init`**：生成模板 config.toml

## Acceptance Criteria

- [ ] `pm-bot trade --confirm` 扫描推荐 → 逐条确认 → 下单
- [ ] `pm-bot orders` 显示当前挂单和成交状态
- [ ] 下单使用 `neg_risk=True`（天气市场必需）
- [ ] 10秒心跳保活，超时不会导致所有订单被取消
- [ ] 窄桶买NO策略检测到 ≤2°C 宽的中心桶，YES≥0.45 时推荐买NO
- [ ] 机场套利策略检测到机场站 vs 市中心 3°F+ 偏差
- [ ] 修正 CITY_COORDS 为机场气象站坐标
- [ ] watch TUI 使用 WebSocket 实时推送替代 60s 轮询
- [ ] Discord Webhook 发送交易通知
- [ ] config.toml 生效，`pm-bot config init` 生成模板
- [ ] `POLY_PK` 环境变量管理私钥，不硬编码
- [ ] 单笔 $5 / 单日 $50 安全约束生效
- [ ] API 失败不崩溃

## Definition of Done

- 所有 Acceptance Criteria 通过
- `uv run pm-bot trade --confirm` 端到端可用
- ruff + mypy 零错误
- 无硬编码凭证
- 错误处理：API 失败不崩，跳过继续

## Technical Approach

### 新增项目结构

```
pm_bot/
  core/
    clob.py          # ClobClient 封装，认证，下单
    ws.py            # WebSocket 客户端，实时价格
    config_loader.py # config.toml 读取 + 环境变量覆盖
  strategies/
    narrow_no.py     # 窄桶买NO策略
    airport_arb.py   # 机场套利策略
  cli/
    trade.py         # trade --confirm 命令
    orders.py        # orders 命令
    notifications.py # Discord/Telegram 通知
  config.toml.example # 模板配置
```

### 新增依赖

- `py-clob-client-v2>=1.0.0`
- `websockets>=12.0`
- `tomlkit>=0.13`（写 config.toml）
- `tomllib`（stdlib 3.11+，读 config.toml）

### 认证流程

1. `config.toml` 的 `[clob]` 段提供 api_key/secret/passphrase
2. `POLY_PK` 环境变量提供钱包私钥
3. `ClobClient` 初始化时用 L1（私钥）+ L2（HMAC）认证
4. 下单时 `neg_risk=True` 指向 Neg Risk CTF Exchange 合约

### WebSocket 集成

- `watch` 命令：连接 WS → 订阅推荐市场的 token_id → 实时更新价格 → 刷新 TUI
- 心跳：10秒 PING，超时重连
- 与现有 scan 逻辑共享推荐引擎

### 通知流程

- Discord: `httpx.post(webhook_url, json={"content": msg})`
- Telegram: `httpx.post(f"https://api.telegram.org/bot{token}/sendMessage", json={"chat_id": chat_id, "text": msg})`
- 触发点：订单创建/成交/取消

## Decision (ADR-lite)

**Context**: Phase 1 已验证扫描+推荐逻辑正确，需升级交易能力
**Decision**: 用 py-clob-client-v2 SDK（而非裸 HTTP），WebSocket 实时推送，Discord Webhook 通知，tomllib+tomlkit 配置
**Consequences**: SDK 减少了手写签名/编码的 bug 风险；但 SDK 更新节奏依赖上游；WebSocket 需要长期连接管理

## Out of Scope

- Phase 3 全自动交易（Kelly 公式、风控系统、24/7 无人值守）
- 回测框架
- Web UI / 移动端
- 多用户/账户系统
- 多源聚合 + Edge 计算

## Research References

- [`research/clob-trading-api.md`](research/clob-trading-api.md) — py-clob-client-v2 两层认证、neg_risk 下单、心跳保活
- [`research/websocket-notifications.md`](research/websocket-notifications.md) — WS 无需认证、best_bid_ask 事件、Discord Webhook
- [`research/narrow-bucket-airport-arb.md`](research/narrow-bucket-airport-arb.md) — 窄桶系统性过定价、机场vs市中心偏差、tomlkit 配置
