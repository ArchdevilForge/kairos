# Kairos 仓库级运行时架构与端到端数据流审查

> 审查范围：全部 4 个 `cmd` 入口，配置、交易所接入、检测器、聚合/策略、通知、持久化/outcomes、关闭流程。  
> 方法：静态追踪当前 Go 源码与仓库内权威文档；未修改生产代码，也未依赖外部 API 实测。

## 1. 结论摘要

当前仓库实际包含四条彼此不同的运行链：

1. `kairosd`：长驻实时 watcher，唯一完整经过 `ticker → detector → aggregator → alert gate → Telegram/DingTalk → hint/outcome store` 的链路。
2. `kairos-alert`：一次性扫描器，走 REST/OHLCV 和独立的 scanner/alert formatter，绕过实时 aggregator/policy/dedup，并只发送 Telegram。
3. `kairos-backtest`：纯 flags + REST OHLCV + 内存回测 + stdout JSON；不加载应用配置、不持久化。
4. `kairos-market-replay`：JSONL → `MarketPulseDetector` 的算法夹具驱动器；不经过 pipeline/policy/通知/存储。

优先修复项：

- **P0：symbol refresh 不是闭环且存在并发 map 读写风险**：刷新只改 `symbolsByExchange` 和 MarketPulse universe，不更新正在运行的 WS 订阅或 `exchangeState.symbols`；CoinGlass poller 又并发读取同一 map。
- **P0：backtest 的分页器是 OKX 专用语义，却挂在通用 `Exchange` 接口上**：runner 从 `end` 向后翻页，Binance/Bybit adapter 却把同一参数解释为 forward `startTime/start`。
- **P0：MarketPulse 下跌通知展示错误代表币和错误计数**：detector 同时输出 leaders/laggards，但 Telegram/DingTalk 对 down 仍读取 `leaders` 并标成“领跌”，普通 down 消息也始终以 `advancers` 为分子；down trend 文案仍写“涨幅/上涨广度”。
- **P1：关闭和失败传播不闭环**：daemon 只等待 OS signal，不同时等待 `pipeline.Start` 返回；WS backoff 与 CoinGlass 请求/逐币循环不能被 pipeline context 及时取消。
- **P1：`AnomalyEvent → AlertEvent` 丢失 timestamp/exchange/event ID**，导致实时消息时间为空、来源交易所不可追踪，多交易所事件也无法区分。
- **P1：发送失败会先消耗 cooldown，且无 retry/pending queue**；MarketPulse/outcome channel 满时也直接永久丢失。
- **P1：配置存在多套 authority 与大量无消费字段**；`dataManager.exchanges`、`exchanges.primary`、顶层 `exchange` 可彼此冲突，且 loader 不做交叉校验。

---

## 2. 入口清单与实际数据流

仓库只有以下四个 `main`：`cmd/kairosd/main.go:19`、`cmd/kairos-alert/main.go:19`、`cmd/kairos-backtest/main.go:18`、`cmd/kairos-market-replay/main.go:34`。

### 2.1 `kairosd`：实时 watcher

```text
--config YAML
  → config.Load
  → env secrets/overrides
  → Telegram / DingTalk clients
  → engine.NewPipeline
  → Pipeline.Start
      → create exchanges from dataManager.exchanges
      → REST FetchTickers + volume/blacklist Top-N discovery
      → optional CoinGlass market-cap cache
      → per-exchange detector registration
      → optional CoinGlass + resonance + MarketPulse registration
      → WS SubscribeTickers
      → ticker fan-out
      → detector event channels
      → eventAggregator
          ├─ raw event → ResonanceScorer
          ├─ market event/outcome → JSONL store
          └─ non-outcome → deliveryCh
      → telegramDeliverer（实际是多 channel deliverer）
          → shadow/blacklist/quiet gate/policy/dedup/cooldown
          → AlertEvent
          → Telegram + DingTalk mirror
          → success → watch-hints.jsonl
  → SIGINT/SIGTERM
  → Pipeline.Stop + Pipeline.Close
  → wait Start or 15s timeout
```

证据：

- 配置入口：`cmd/kairosd/main.go:23`；YAML/default/env 的装配顺序见 `internal/config/config.go:15-33`。
- 通知 client 只在各自 `enabled` 且 secret 存在时创建：`cmd/kairosd/main.go:35-54`。
- pipeline 创建、启动和 signal context：`cmd/kairosd/main.go:58-75`。
- exchange authority 来自 `DataManager.Exchanges`：`internal/engine/pipeline.go:212-215`。
- discovery 使用 `FetchTickers`，过滤 canonical USDT perpetual、quote volume 和 blacklist，再取 Top-N：`internal/engine/pipeline.go:428-477`。
- detector/channel 注册：`internal/engine/pipeline.go:247-303`。
- WS、ticker fan-out、metrics/CoinGlass、aggregator、resonance、delivery、refresh、MarketPulse goroutine：`internal/engine/pipeline.go:306-403`。
- primary ticker 才进入 MarketPulse：`internal/engine/pipeline.go:541-556`。
- aggregator 先喂 resonance，再持久化 market event/outcome，outcome 不进通知：`internal/engine/pipeline.go:818-844`。
- alert gate 与发送：`internal/engine/pipeline.go:1030-1091`、`internal/engine/pipeline.go:1128-1196`。
- 只有至少一个 channel 发送成功才写 scanner hint：`internal/engine/pipeline.go:925-945`、`internal/engine/pipeline.go:1088-1103`。
- 关闭顺序：`cmd/kairosd/main.go:78-95`。

### 2.2 `kairos-alert`：一次性 scanner summary

```text
--config/--exchange/--min-state/--limit/--dry-run
  → config.Load
  → MarketScanner.ScanMarket
      → primary exchange.New
      → 并行加载 BTC 1d context + CoinGlass RSI
      → REST FetchTickers → Top universe → hint boost → candidate score
      → primary 无 candidate 时 backup discovery
      → candidate 并行拉 1d/4h/15m OHLCV
      → deterministic setup/risk/action state
      → SignalEnvelope
  → alert.SelectSetups
  → alert.FormatAlert
  → dry-run stdout，或 Telegram.SendText
  → exit code 0/1/2
```

证据：

- CLI/config/exit：`cmd/kairos-alert/main.go:19-32`。
- scanner 调用：`cmd/kairos-alert/main.go:36-41`。
- BTC context、RSI、candidate discovery 和 backup fallback：`internal/scanner/scanner.go:68-132`。
- deep analysis 并发与 per-symbol timeout：`internal/scanner/scanner.go:142-177`。
- hint 读取进入 candidate score：`internal/scanner/scanner.go:391-399`。
- OHLCV/方向评分：`internal/scanner/scanner.go:548-613`、`internal/scanner/scoring.go:139-268`。
- setup 选择、HTML 格式化、Telegram：`cmd/kairos-alert/main.go:51-76`、`internal/alert/alert.go:18-44`、`internal/alert/alert.go:85-111`。

这条链**不经过** realtime `AlertPolicy`、liquidity weight、dedup/cooldown、DingTalk、MarketPulse gate 或 outcome store。

### 2.3 `kairos-backtest`：历史回测

```text
flags only（不加载 config.yaml）
  → exchange.New
  → BoxDetector config from --min-bars
  → FetchBtc1d（失败降级）
  → single 或受 semaphore 限制的 multi-symbol Runner.Run
      → paginated FetchOHLCV
      → upfront box detection
      → bar loop / cycle gate / entry / exit
      → Summary + trades
  → stdout JSON
  → exchange.Close
```

证据：

- flags、exchange、runner：`cmd/kairos-backtest/main.go:18-49`。
- single/multi-symbol 分支：`cmd/kairos-backtest/main.go:62-107`。
- `Run`、OHLCV、bar loop、summary：`internal/backtest/engine.go:118-266`。
- 分页：`internal/backtest/engine.go:287-334`。
- 仅 stdout JSON：`cmd/kairos-backtest/main.go:127-133`。

### 2.4 `kairos-market-replay`：MarketPulse fixture runner

```text
JSONL ticks + optional marketPulse config
  → load/sort/validate ticks
  → unique symbols
  → MarketPulseDetector + fixture clock + universe
  → OnTicker + EvaluateAt
  → final repeated evaluations for confirmation windows
  → drain detector.Events
  → stdout state path/events/last snapshot
```

证据：`cmd/kairos-market-replay/main.go:34-79`、`cmd/kairos-market-replay/main.go:90-169`、`cmd/kairos-market-replay/main.go:176-225`。

这不是生产 pipeline 回放：它不会验证 aggregator、policy、format、channel fan-out、persistence 或 shutdown。

---

## 3. 核心运行时契约

| 边界 | 当前类型/行为 | 证据 |
| --- | --- | --- |
| Exchange → detector | `types.Ticker`，canonical symbol + optional price/volume/change/OI/funding | `internal/types/types.go:74-83`；`internal/exchange/exchange.go:11-18` |
| Detector → aggregator | `types.AnomalyEvent`，float Unix seconds timestamp，无 exchange/event ID | `internal/types/events.go:3-10` |
| Aggregator → channel formatter | `types.AlertEvent`，timestamp 变成 RFC3339 string，并增加 exchange/event ID | `internal/types/types.go:187-198` |
| Scanner output | `SignalEnvelope`，map-based `Data/Score` + reasons/warnings/errors | `internal/types/types.go:54-64` |
| Watcher → scanner | delivery 成功后 append `watch-hints.jsonl`；scanner 按 retention 读取并加固定 boost | `internal/storage/hints.go:56-94`；`internal/scanner/scanner.go:391-399` |
| MarketPulse calibration | event/outcome append 到 databasePath 所在目录的两个 JSONL | `internal/storage/market_pulse.go:40-47`、`internal/storage/market_pulse.go:67-103`、`internal/storage/market_pulse.go:129-169` |

---

## 4. 发现：接口不一致、生命周期断链与死分支

## P0 — 直接破坏主流程正确性

### P0.1 Symbol refresh 只更新目录，不更新生产者/消费者，且 map 无锁

初始 WS 和 metrics 都捕获 `exchangeState.symbols`：WS 订阅在 `internal/engine/pipeline.go:310-318`，metrics 在 `internal/engine/pipeline.go:593-605`。refresh 却只写 `p.symbolsByExchange[name]` 并更新 MarketPulse：`internal/engine/pipeline.go:1303-1319`，没有修改 `es.symbols`、重订阅 WS 或重建 detector。

后果：

- 新增 Top-N symbol 不会进入正在运行的 WS，也不会进入 metrics poll。
- 已移除 symbol 仍持续被 WS/per-symbol detector 消费。
- MarketPulse universe 会包含“新增但无 WS tick”的 symbol，warmup 后可能持续降低 fresh ratio。
- `pollLongShort`/`pollLiquidations` 直接读取 `symbolsByExchange`（`internal/engine/pipeline.go:649-664`、`internal/engine/pipeline.go:750-764`），refresh 同时写该 map，无 mutex；刷新与 CoinGlass poll 重叠时存在 Go map data race/并发读写崩溃风险。

这是 refresh lifecycle 的核心非闭环。

### P0.2 通用 backtest runner 与 exchange pagination 语义冲突

`BacktestRunner.fetchOHLCV` 明确从 `cursor := endMs` 向过去回溯，并把 cursor 作为通用 `FetchOHLCV(... sinceMs)` 参数：`internal/backtest/engine.go:287-329`。这与 OKX adapter 的 backward `after` 语义匹配：`internal/exchange/okx.go:306-314`。

但：

- Binance 把同一值发成 forward `startTime`：`internal/exchange/binance.go:150-157`。
- Bybit 把同一值发成 forward `start`：`internal/exchange/bybit.go:185-192`。

因此 `kairos-backtest --exchange binance|bybit` 共享了一个 OKX 专用分页算法，通常从 end 才开始取数，无法回填完整 start→end 区间。`Exchange.FetchOHLCV` 的 `sinceMs` 参数没有表达方向/分页契约（`internal/exchange/exchange.go:11-18`）。

### P0.3 MarketPulse down 通知的数据字段接错

Detector 正确生成 `leaders` 与 `laggards` 两个字段：`internal/detector/market_pulse.go:965-990`。

Telegram 和 DingTalk formatter 却始终读取 `leaders`：`internal/notify/telegram.go:255-266`、`internal/notify/dingtalk.go:316-327`；当方向为 down 时仅把标题改为“领跌”。因此下跌告警实际展示的是横截面最强币，而非领跌币。

同一处还有两个方向错误：

- 普通 down 消息的“广度（x / valid）”始终用 `advancers`：`internal/notify/telegram.go:219-246`、`internal/notify/dingtalk.go:282-306`，但 breadth 本身已是 down breadth。
- down `market_trend` 仍写“5分钟市场中位涨幅/上涨广度”：`internal/notify/telegram.go:234-236`、`internal/notify/dingtalk.go:296-298`。

这会直接误导用户对市场方向和代表币的理解。

## P1 — 可靠性/可观测性/关闭链不闭环

### P1.1 Daemon 不监听 pipeline 提前退出

`pipeline.Start` 在 goroutine 中把结果写入 `errCh`（`cmd/kairosd/main.go:73-76`），主 goroutine 却只阻塞 `<-ctx.Done()`（`cmd/kairosd/main.go:78-80`）。只有收到 OS signal 后才读取 `errCh`（`cmd/kairosd/main.go:89-95`）。

若 pipeline 因内部错误提前返回，进程仍会显示为存活并无限等待 signal，监控也不会自动发现主链已经停止。

关闭时又先 `Stop`、立即 `Close` exchanges，再等待 `Start`：`cmd/kairosd/main.go:86-95`；这与 `Pipeline.Close` 注释所称 “after shutdown” 不一致（`internal/engine/pipeline.go:419-425`）。

### P1.2 多处工作不响应 pipeline cancellation

- 三个 WS adapter 的 reconnect backoff 都直接 `time.Sleep`，最长 30s，不能被 ctx 立即打断：`internal/exchange/okx.go:82-91`、`internal/exchange/binance.go:63-69`、`internal/exchange/bybit.go:61-67`。
- CoinGlass API 没有接收 caller context：Python path 自建 `context.Background()` timeout（`internal/data/coinglass_py.go:35-40`），native path 用无 context 的 `http.NewRequest`（`internal/data/coinglass.go:254-268`）。
- L/S 和 liquidation poller 在逐 symbol 循环内不检查 ctx：`internal/engine/pipeline.go:649-674`、`internal/engine/pipeline.go:750-774`。单次请求还可能先尝试 Python、失败后再尝试 native（`internal/data/coinglass.go:228-232`）。

因此 SIGTERM 后，errgroup 可能长时间等当前逐币批次结束；main 的 15s timeout 只能让进程返回，不能构成真正的 graceful drain。

### P1.3 `AnomalyEvent → AlertEvent` 元数据断裂

`AnomalyEvent.Timestamp` 是 float seconds，且没有 exchange/event ID（`internal/types/events.go:3-10`）；`AlertEvent` 要求 string timestamp、exchange、event ID（`internal/types/types.go:187-198`）。

`buildAlert` 只复制 event/symbol/price/condition/change/severity/data：`internal/engine/pipeline.go:1201-1221`。resonance builder 同样未填这三个字段：`internal/engine/pipeline.go:979-1001`。

formatter 又读取 `AlertEvent.Timestamp`：`internal/notify/telegram.go:63-72`、`internal/notify/dingtalk.go:149-153`。结果是 realtime alert 的时间字符串为空，exchange 和 event ID 也始终缺失。

进一步影响：每个 exchange 各自创建 detector，但进入 aggregator 后没有来源字段；同 symbol 的多交易所事件在 policy、dedup、resonance 与 hint metadata 上不可区分。写 hint 时还统一标为 `cfg.Exchanges.Primary`：`internal/engine/pipeline.go:1088-1103`。

### P1.4 发送失败仍消耗 cooldown，整个链是 at-most-once

普通事件和 resonance 都在发送前写 `dedupLast`：`internal/engine/pipeline.go:965-977`、`internal/engine/pipeline.go:1063-1083`。发送失败只记录 warn（`internal/engine/pipeline.go:925-945`），不会回滚 cooldown、重试或落 pending notification。

所以 Telegram/DingTalk 临时失败后，同事件在完整 `symbolCooldownSeconds` 内都被压制。MarketPulse detector 也只要成功写入自己的 event channel 就更新 `lastEmitted`（`internal/detector/market_pulse.go:1017-1025`），并不知道后续 persistence/policy/channel 是否成功。

### P1.5 Channel 满时状态已前进，但 event/outcome 永久丢失

MarketPulse 状态迁移先发生，随后非阻塞 emit；channel 满时只打日志：`internal/detector/market_pulse.go:1017-1026`。状态已经变化，因此该状态变化事件不会自动重试。

outcome 同样非阻塞发送：`internal/detector/market_pulse.go:1195-1199`；调用方随后把该 pending outcome 从列表移除（`internal/detector/market_pulse.go:1113-1120`）。这会同时失去 calibration persistence。

### P1.6 Outcome 在数据不足时仍用 stale price 结算，且重启丢上下文

`EvaluateAt` 无论 snapshot 的 `DataOK` 是否为 false，都会调用 `updateOutcomesLocked`：`internal/detector/market_pulse.go:396-408`。state transition 会在数据不足时 gate（`internal/detector/market_pulse.go:608-616`），但 outcome 计算不检查 `DataOK`，而是直接读取每个 series 的最后价格：`internal/detector/market_pulse.go:1062-1071`、`internal/detector/market_pulse.go:1123-1135`。

因此断流期间仍可能用 stale prices 填满 +1m/+3m/+5m/+15m horizon。`pendingOutcomes`、state、dedup/cooldown 都只在内存中；`Reset` 会清空 pending（`internal/detector/market_pulse.go:264-281`），启动也总从 QUIET 新建（`internal/detector/market_pulse.go:107-119`），没有恢复链。

### P1.7 `dataManager.exchanges` 与 `exchanges.primary` 是两套未校验 authority

watcher 实例化交易所使用 `cfg.DataManager.Exchanges`：`internal/engine/pipeline.go:212-229`；MarketPulse primary、scanner primary 和 hint metadata 使用 `cfg.Exchanges.Primary`：`internal/engine/pipeline.go:269-279`、`internal/scanner/scanner.go:68-70`、`internal/engine/pipeline.go:1088-1103`；顶层 `cfg.Exchange` 还作为部分 fallback/日志存在：`internal/engine/pipeline.go:269-272`、`cmd/kairosd/main.go:33`。

若 primary 不在 watcher exchange list，MarketPulse 会被注册但 universe 为空、永远没有 primary ticks。config loader 只 Read/Unmarshal/env 后直接返回，没有 validation：`internal/config/config.go:15-33`。

### P1.8 OKX realtime funding 配置无效

配置公开 `FuturesMetrics.FetchFundingPerSymbol`：`internal/types/types.go:312-317`，default/example 都设为 true，但生产代码没有读取该字段。

metrics poll 只调用 bulk `FetchTickers` 并消费其中的 funding：`internal/engine/pipeline.go:593-619`。OKX `FetchTickers` 只追加 OI（`internal/exchange/okx.go:112-180`），funding enrichment 只在 `FetchTicker` 或 scanner 的 `FundingEnricher` 路径发生：`internal/exchange/okx.go:293-295`、`internal/exchange/okx.go:486-488`。

故 primary=OKX 时，即使 `futuresMetrics.fundingRate.enabled=true` 且 `fetchFundingPerSymbol=true`，watcher poll 仍持续向 detector 传 funding=0。

### P1.9 Scanner 接受任意 timeframe 配置，算法却硬索引固定三周期

scanner 只验证“配置中列出的 timeframe 是否拉取成功”：`internal/scanner/scanner.go:560-592`，随后无条件访问 `timeframeData["15m"]`、`["1d"]`、`["4h"]`：`internal/scanner/scanner.go:595-607`。

由于 config loader 无 validation，配置缺少任一固定周期时会 nil dereference；`DeepAnalysisLimit < 0` 也会在 `candidates[:limit]` panic（`internal/scanner/scanner.go:142-154`）。

### P1.10 Detector panic 没有隔离边界

`consumeTickers` 直接同步调用 detector `OnTicker`：`internal/engine/pipeline.go:541-556`；MarketPulse loop 直接调用 `EvaluateAt`：`internal/detector/market_pulse.go:379-390`。这些 goroutine 没有 recover wrapper。任一 detector panic 会终止整个进程，而非只关闭/降级该 detector。

## P2 — 死配置、孤立 API、文档漂移

### P2.1 无运行时消费者的配置字段

静态搜索仅发现以下字段的声明/default/example，未发现生产消费：

- `defaultTimeframe`：`internal/types/types.go:217`。
- `telegram.parseMode`：配置存在，但 client 硬编码 HTML：`internal/types/types.go:243-246`、`internal/notify/telegram.go:42-50`。
- `scanner.intervalSeconds`：`internal/types/types.go:367-376`；实际 command 是 one-shot/外部 cron。
- `futuresMetrics.fetchFundingPerSymbol`：见 P1.8。
- `exchanges.rateLimit/canonicalQuote/settle`：`internal/types/types.go:379-386`。
- `storage.retentionDays/jsonlExport/jsonlPath`：`internal/types/types.go:421-429`。
- `charts.cleanupDays`：`internal/types/types.go:432-438`。
- `AlertMinState`/`AlertLimit` 会被 config env loader 写入（`internal/config/config.go:71-78`），但 `kairos-alert` 再次直接读取 env 构造 flags（`cmd/kairos-alert/main.go:21-24`），不消费 cfg 字段。

`notificationTimezone` 只在 startup log 使用（`cmd/kairosd/main.go:33`）；实时 formatter 固定展示 `UTC`，且当前 timestamp 还是空值。

### P2.2 声称 hot reload 的 API 没有 lifecycle 调用

`PriceVelocityDetector.UpdateConfig`、`VolumeSpikeDetector.UpdateConfig`、`FuturesMetricsDetector.UpdateConfig`、L/S、liquidation、resonance 都存在，但 repository runtime 没有 config watcher，也没有任何生产 caller；例如 `internal/detector/pricevelocity.go:103-115`、`internal/detector/resonance.go:212-231`。

此外 price/volume detector 保存 `enabled` 字段但 `OnTicker` 不检查它（`internal/detector/pricevelocity.go:66-87`、`internal/detector/volumespike.go:76-105`），所以即便未来直接调用 `UpdateConfig(enabled=false)`，现有实例仍会继续检测。该 API 目前既未接线，也不满足其注释契约。

### P2.3 `allowIsolatedExtremeWhenQuiet` 是显式 no-op

配置公开 `AllowIsolatedExtremeWhenQuiet` 和 `IsolatedExtremeMinZ`：`internal/types/market_pulse.go:47-51`。gate 分支只读取后丢弃阈值，最后仍返回 true 压制事件：`internal/engine/pipeline.go:1224-1239`。这是用户可见但完全无效的开关。

### P2.4 Persistence 配置与实际介质/retention 不一致

`storage.databasePath` 看似 SQLite 文件，但 MarketPulse 和 hint store 仅把它当作目录锚点并写三个 sidecar JSONL：`internal/storage/market_pulse.go:40-47`、`internal/storage/hints.go:33-42`。

- `retentionDays`、`jsonlExport`、`jsonlPath` 不生效。
- hints retention 只在读取时过滤旧记录（`internal/storage/hints.go:98-124`），文件本身永不 compact，长期运行会无界增长。
- MarketPulse event/outcome 也没有 retention/rotation。
- 没有持久化所有普通 anomaly、delivery attempt、send result、dedup state 或 MarketPulse state。

### P2.5 Scanner chart 与 scheduler 输出未闭环

Scanner 会生成 `chart_spec.generate_now/output_path`（`internal/scanner/scoring.go:486-495`），但仓库无 chart renderer；返回 envelope 又固定声明 `charts_generated=false`、`telegram_pushed=false`（`internal/scanner/scanner.go:221-226`）。Telegram 实际由 caller 在扫描后发送（`cmd/kairos-alert/main.go:57-76`），故 envelope 只描述 scanner 内部，不代表端到端执行结果。

`scanner.intervalSeconds` 同样没有内部 scheduler；README 明确要求外部 cron/systemd timer（`config/config.yaml.example:11-12`）。

### P2.6 `kairos-alert` 忽略 `telegram.enabled`

Daemon 创建 Telegram client 时检查 `cfg.Telegram.Enabled`：`cmd/kairosd/main.go:35-46`。一次性 alert command 只检查 token/chat ID，不检查 enabled：`cmd/kairos-alert/main.go:63-73`。因此同一配置中的 `telegram.enabled=false` 对两个入口语义不同。

### P2.7 Dedup 与 cooldown 共用同一 key，名称和行为不一致

普通事件先按 `symbol__event_type` 检查 dedup，再用同一个 key 检查 symbol cooldown：`internal/engine/pipeline.go:1063-1080`。默认 cooldown 45 分钟大于 dedup 5 秒，因此 dedup window 不改变是否接受，只改变前 5 秒的 gate reason；而名为 `SymbolCooldownMinutes` 的策略实际是 per-symbol+event-type，不是 per-symbol。

### P2.8 Resonance 输入边界过宽且无 exchange 维度

aggregator 会把除 `resonance` 外的所有 event 先送入 scorer，包括 `market_outcome`，随后才把 outcome 从通知链排除：`internal/engine/pipeline.go:818-839`。scorer 以 `symbol → event_type` 聚合：`internal/detector/resonance.go:118-126`，没有 exchange 维度；因此不同交易所的同 symbol 事件互相覆盖/混合，MarketPulse calibration event 也污染 `MARKET` window。默认阈值下未必发出 alert，但边界与“异常 detector dimensions”并不一致。

### P2.9 文档与代码命名/能力漂移

- 权威 `docs/architecture.md` 仍列出不存在的 `kairos-watch`，且未列 `kairosd`、backtest、replay：`docs/architecture.md:34-46`；README 的 4 个入口才与代码一致：`README.md:53-70`。
- `DataManagerConfig` 注释和 example 也仍称 `kairos-watch`：`internal/types/types.go:258-264`、`config/config.yaml.example:18`。
- README 称 `price_velocity`、`volume_spike`、OI 都是 Z-score 检测；实际 price velocity 是窗口绝对涨跌阈值（`internal/detector/pricevelocity.go:112-168`），volume spike 是 delta ratio（`internal/detector/volumespike.go:143-213`），OI 是相邻 poll 变化率（`internal/detector/futuresmetrics.go:129-178`）。

---

## 5. 当前真正闭合与未闭合的反馈环

| 环 | 状态 | 说明 |
| --- | --- | --- |
| Exchange ticker → single-symbol detector → alert channel | 基本闭合 | 初始 universe 有效；refresh 后断裂。 |
| Primary ticker → MarketPulse → market alert | 基本闭合 | shadow/policy/format 已接线；down formatter 有字段错误。 |
| Raw detector events → resonance → delivery | 闭合但来源模糊 | 无 exchange 维度，输入含 market/outcome。 |
| Delivery success → watch hint → next scanner candidate boost | 闭合 | 仅成功发送才写；metadata 固定 primary。 |
| Market event → post-event outcomes → JSONL | 条件闭合 | 要求进程连续运行约 15m；断流可用 stale price，重启丢 pending。 |
| Symbol refresh → active WS/metrics universe | **未闭合** | 只改目录和 MarketPulse eligibility。 |
| Send failure → retry/recovery | **未闭合** | 先 burn cooldown，无 retry/pending。 |
| Pipeline failure → process supervisor-visible exit | **未闭合** | main 不监听 Start 提前返回。 |
| Config change → hot reload | **未闭合** | UpdateConfig API 无 caller/config watcher。 |
| Storage retention/rotation/recovery | **未闭合** | 只有 append/read-filter，无 compact/state restore。 |
| Replay → production delivery/persistence verification | **未闭合** | replay 只测 detector，不走 pipeline。 |
| Chart spec → chart artifact | **未闭合** | 只有 spec，无 renderer。 |

## 6. 建议修复顺序（供后续 planning 使用）

1. 先定义并实现 symbol refresh 的单一 atomic contract：要么真正重订阅/重建 `exchangeState`，要么删除“动态 refresh”承诺并重启进程；同时给 shared universe 加锁/快照。
2. 把 OHLCV pagination 从 runner 的 OKX 假设移到 adapter，或将接口改成明确的 `[start,end]`/page token 契约。
3. 修正 MarketPulse down formatter：down 用 laggards/decliners/down 文案，并补对称测试。
4. 让 daemon `select { case err := <-errCh; case <-ctx.Done() }`，并把 ctx 贯穿 CoinGlass/backoff。
5. 合并/扩展 event contract，保留 exchange、timestamp、event ID；在发送成功后再提交 cooldown，或增加最小 pending retry。
6. 为 config 增加一次集中 validation，统一 exchange authority，拒绝缺失固定 timeframes/负 limits。
7. 校正 OKX funding enrichment；删除或真正接通其余 dead config/hot-reload/chart API。
8. outcomes 只在 fresh `DataOK` snapshot 上采样，并持久化 pending/state 或明确标注“连续进程内 best-effort”。
