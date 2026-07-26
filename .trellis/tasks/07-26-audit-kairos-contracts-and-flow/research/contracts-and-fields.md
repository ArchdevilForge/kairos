# Kairos 合约与字段一致性审查

> 审查范围：Go interface / concrete implementation、共享类型、配置 struct / defaults / example YAML / validation、`AnomalyEvent.Data` 生产消费键、序列化标签、事件名、单位与可选性。  
> 仅静态审查，不修改代码。验证命令：`go test ./...`、`go vet ./...` 均通过。

## 结论摘要

当前代码可以编译且测试全绿，但测试主要验证局部 happy path，没有覆盖若干跨层合约断裂。最值得优先修复的是：

1. `AlertEvent` 的时间、交易所和事件 ID 在 `AnomalyEvent -> AlertEvent` 转换时全部丢失；实际消息时间为空，多交易所事件无来源。
2. resonance 走独立发送路径，绕过 `alertPolicy` allow-list / severity 等门控。
3. `Exchange.FetchOHLCV(... sinceMs)` 在 OKX 与 Binance/Bybit 的游标语义相反，而 backtest 对所有交易所都按 OKX 倒序语义调用。
4. MarketPulse 的 impulse/trend/stress `windowSeconds` 配置并不控制计算窗口。
5. 下跌 MarketPulse 消息把 `advancers` 和 `leaders` 标成下跌计数和领跌币，内容与方向相反。
6. 配置没有统一 validation；若 timeframes 或 scanner limits 不合法，可直接 panic；不少公开配置字段实际无效。
7. Telegram / DingTalk 密钥虽然不从 YAML 解码，却会被 JSON 序列化。

---

## P0 / 高风险发现

### C-01 `AnomalyEvent -> AlertEvent` 元数据丢失：时间为空、交易所为空、事件 ID 为空

**证据**

- `AnomalyEvent.Timestamp` 是 Unix 秒 `float64`：`internal/types/events.go:4-9`。
- `AlertEvent.Timestamp` 是字符串，且另有 `EventID`、`Exchange`：`internal/types/types.go:187-197`。
- `buildAlert` 只设置 `Event/Symbol/Price/Condition/ChangePct/Severity/Data`，没有转换 `Timestamp`，也没有设置 `EventID` 或 `Exchange`：`internal/engine/pipeline.go:1201-1221`。
- Telegram / DingTalk 都读取 `event.Timestamp` 并显示 `UTC`：`internal/notify/telegram.go:72,89`、`internal/notify/dingtalk.go:153,170`。
- repo 内除测试外没有任何 `EventID` 赋值；detector event 也没有 exchange 字段。

**影响**

- 实时消息会显示空时间（形如 `|  UTC`）。
- 多交易所同 symbol 的事件无法在消息、持久化 hint 或排障日志中追溯来源。
- `EventID` 的共享类型承诺从未实现。
- dedup key 只有 `symbol__event_type`（`internal/engine/pipeline.go:1064`），多个交易所同币同事件会互相压制。

**建议合约**

明确一个统一事件时间类型（推荐 Unix milliseconds 或 `time.Time`，边界处格式化），并让 `AnomalyEvent` 携带 `Exchange` / `EventID`；dedup key 包含 exchange，或明确全市场跨交易所 dedup 是产品意图。

### C-02 resonance 绕过 `alertPolicy`

**证据**

- 普通事件在 `deliverEvent` 中调用 `passesAlertPolicy`：`internal/engine/pipeline.go:1059`。
- resonance 从独立 `resonanceDeliverer -> sendResonanceAlert` 路径发送：`internal/engine/pipeline.go:893-1007`。
- 该路径直接 `sendToChannels(alert)`，没有调用 `passesAlertPolicy`：`internal/engine/pipeline.go:948-1007`。
- allow-list 只在 `passesAlertPolicy` 中检查：`internal/engine/pipeline.go:1128-1141`。
- 默认 allow-list 不含 `resonance`：`internal/config/config.go:107-113`、`config/config.yaml.example:40-45`。
- spec 明确要求 Pipeline 在 delivery 和 dedup/cooldown mutation 前应用 alert policy：`.trellis/spec/backend/signal-alert-contracts.md:28`。

**影响**

启用 `resonanceScorer.enabled` 后，即使 `alertPolicy.allowedEventTypes` 没有 `resonance`，仍会发送 resonance；`alertPolicy.enabled=false/true`、`minSeverity` 对该路径也没有统一语义。

### C-03 `Exchange.FetchOHLCV` 游标语义不一致，非 OKX backtest 很可能取不到正确区间

**证据**

- interface 将最后参数命名为 `sinceMs`：`internal/exchange/exchange.go:16`。
- OKX 把它解释为倒序结束游标 `after=`：`internal/exchange/okx.go:299-314`。
- Binance 把它解释为正序起点 `startTime=`：`internal/exchange/binance.go:150-157`。
- Bybit 把它解释为正序起点 `start=`：`internal/exchange/bybit.go:185-192`。
- backtest 无条件从 `endMs` 开始并不断把 cursor 设为最老 candle：`internal/backtest/engine.go:292-322`。
- CLI 公开支持 `--exchange` 任意 adapter：`cmd/kairos-backtest/main.go:20-21,42`。
- 现有回归测试只模拟 OKX 式 backward pagination：`internal/backtest/fetch_ohlcv_test.go:31-66`；spec 也只要求 OKX：`.trellis/spec/backend/signal-alert-contracts.md:49`。

**影响**

`kairos-backtest --exchange binance|bybit` 会把结束日期作为 `startTime/start`，随后仍按倒序更新 cursor，可能返回区间外数据、空数据或提前停止。

**建议合约**

不要用含混的 `sinceMs` 同时表达两种分页。可由 adapter 暴露统一的 `[start,end]` 查询，或由 backtest 按 capability / exchange 明确选择方向。

### C-04 MarketPulse 的 `windowSeconds` 是伪配置

**证据**

- 三个公开字段：`Impulse.WindowSeconds`、`Trend.WindowSeconds`、`Stress.WindowSeconds`：`internal/types/market_pulse.go:62-86`。
- detector 始终硬编码计算 60 / 180 / 300 秒收益：`internal/detector/market_pulse.go:359,463-474`。
- impulse/trend/stress conditions 分别读取固定的 `MedianReturn60s` / `MedianReturn300s`：`internal/detector/market_pulse.go:1210-1279`。
- event 的 `window_seconds` 对所有事件都固定写 `d.cfg.Impulse.WindowSeconds`：`internal/detector/market_pulse.go:978`。

**影响**

用户修改 `trend.windowSeconds` 或 `stress.windowSeconds` 不会改变算法；修改 impulse window 只改变事件字段显示，不改变实际 60s 计算。趋势事件甚至会报告 impulse window。

### C-05 下跌 MarketPulse 消息消费了错误字段

**证据**

- producer 对 down 正确选择 `down_breadth`，并同时输出 `advancers/decliners`、`leaders/laggards`：`internal/detector/market_pulse.go:960-991`。
- Telegram formatter 无条件读取 `advancers`：`internal/notify/telegram.go:218-220`，并在 down 消息中仍打印为 breadth 分子：`internal/notify/telegram.go:243`。
- formatter 无条件读取 `leaders`：`internal/notify/telegram.go:255`，down 时仅把标题改成“领跌”：`internal/notify/telegram.go:257-265`。
- DingTalk 有同样问题：`internal/notify/dingtalk.go:281-283,305,316-327`。
- trend down 仍写“5分钟市场中位涨幅 / 上涨广度”：`internal/notify/telegram.go:235-236`、`internal/notify/dingtalk.go:297-298`。

**影响**

下跌告警会把上涨币数量作为下跌 breadth 分子，并把横截面最强的币标为“领跌”。这不是文案瑕疵，而是方向性事实错误。

### C-06 配置无 validation；scanner 的合法 YAML 可触发 panic

**证据**

- `config.Load/LoadString` 仅 read、unmarshal、env override 后直接返回，没有 Validate：`internal/config/config.go:15-53`。
- `scanner.timeframes` 是任意字符串 slice：`internal/types/types.go:375`。
- analyzer 只确认“配置中列出的 timeframe”已加载：`internal/scanner/scanner.go:560-590`，随后无条件解引用 `15m/1d/4h`：`internal/scanner/scanner.go:595-607`。配置若遗漏任一核心周期，会 nil dereference。
- `UniverseSize` / `CandidateLimit` / `DeepAnalysisLimit` 直接用于 slice 或 channel capacity：`internal/scanner/scanner.go:142-154,176-177,379-380,410-411`；负数可 panic，零值可产生空流程。
- MarketPulse 只给 `<=0` 值回填默认，没有检查 `MinFreshRatio <= 1`、`EWMAAlpha <= 1`、`ConfirmationSamples <= ConfirmationWindowSamples`、retention 是否覆盖最大窗口等：`internal/detector/market_pulse.go:118-222`。
- `minSeverity` typo 会被当作 LOW：`internal/engine/pipeline.go:1398-1407`。

**影响**

配置错误不是启动时报清晰错误，而是静默改变语义、永久不触发，或运行期 panic。

### C-07 env-only secret 可被 JSON 序列化

**证据**

- `BotToken/ChatID/WebhookURL/Secret` 使用 `mapstructure:"-"`、`yaml:"-"`，但 JSON tags 分别是 `bot_token/chat_id/webhook_url/secret`：`internal/types/types.go:243-255`。
- env override 将真实 secret 写回 Config：`internal/config/config.go:58-70`。

**影响**

任何 `json.Marshal(cfg)`、诊断 dump 或未来 API 都会泄露 Telegram token / chat ID / DingTalk webhook 和 secret。`omitempty` 不能保护已加载的非空密钥。

---

## P1 / 中风险发现

### C-08 三套 exchange 配置来源不闭环

**证据**

- root `exchange`、`dataManager.exchanges`、`exchanges.primary/backups` 同时存在：`internal/types/types.go:215-235,260-265,380-385`。
- realtime 实际创建的 adapters 只看 `DataManager.Exchanges`：`internal/engine/pipeline.go:212-225`。
- MarketPulse primary 看 `Exchanges.Primary`，空时才 fallback root `Exchange`：`internal/engine/pipeline.go:267-275,550-555`。
- scanner 默认只看 `Exchanges.Primary`：`internal/scanner/scanner.go:68-71,236-239`。
- config 测试甚至只覆盖修改 root `exchange`：`internal/config/config_test.go:26-48`，这不会同步改变 scanner primary 或 realtime exchange list。

**影响**

只改 `exchange: binance` 看似成功加载，但 realtime 仍可能跑 `dataManager.exchanges: [okx]`，scanner/MarketPulse 仍可能使用 `exchanges.primary: okx`。若 primary 不在 realtime list，MarketPulse universe 为空。

### C-09 symbol refresh 只更新 map，不更新已订阅 WS / metrics state

**证据**

- WS 启动时使用一次 `es.symbols`：`internal/engine/pipeline.go:311-318`。
- metrics poll 也持续使用 `es.symbols`：`internal/engine/pipeline.go:594-604`。
- refresh 只写 `p.symbolsByExchange[name] = newSymbols` 并更新 MarketPulse universe：`internal/engine/pipeline.go:1298-1319`；没有更新 `es.symbols`，也没有重新订阅。

**影响**

日志与 MarketPulse eligible universe 已变，但真实 ticker/metrics 仍是旧集合。新币会永远不 fresh，旧币仍被消费；可能导致 MarketPulse 长期 gated。

### C-10 Futures metrics config / adapter capability 不一致

**证据**

- `futuresMetrics.fetchFundingPerSymbol` 在 type/default/example 中公开：`internal/types/types.go:315`、`internal/config/config.go:150`、`config/config.yaml.example:79`。
- runtime repo-wide 没有读取该字段；`pollMetrics` 只调用 `FetchTickers` 并读取可选 OI/funding：`internal/engine/pipeline.go:598-619`。
- OKX `FetchTickers` enrich OI 但不 enrich funding：`internal/exchange/okx.go:111-176`；其 per-symbol funding capability 只在 scanner 使用：`internal/scanner/scanner.go:387-389`。
- Binance `FetchTickers` enrich funding，但不提供 OI：`internal/exchange/binance.go:79-116,196-220`。
- Bybit 同时提供 OI 和 funding：`internal/exchange/bybit.go:84-134`。
- missing optional value 被 pipeline 转成数值 `0` 后传 detector：`internal/engine/pipeline.go:609-619`，接口无法区分“真实 0”与“缺失”。

**影响**

同一配置在不同 exchange 上实际启用的维度不同且无告警；OKX 的 `fetchFundingPerSymbol: true` 没有效果，Binance OI detector 永远无数据。

### C-11 resonance `dimensions` 的运行时 slice 类型不匹配 formatter

**证据**

- 两个 producer 都构造 `[]string`：`internal/detector/resonance.go:23-30`、`internal/engine/pipeline.go:980-987`。
- Telegram / DingTalk formatter 都断言 `data["dimensions"].([]any)`：`internal/notify/telegram.go:102`、`internal/notify/dingtalk.go:183`。
- 当前 formatter 测试手工提供 `[]any`，没有使用真实 producer shape：`internal/notify/format_test.go:11-29`。

**影响**

进程内没有 JSON round-trip，因此真实 `[]string` 断言失败，维度列表为空；消息只显示 dimension count，不显示各维度明细。

### C-12 resonance 会吞入 MARKET 事件，可能生成无意义的 `MARKET resonance`

**证据**

- aggregator 除了 `resonance` 外把所有事件都送入 scorer：`internal/engine/pipeline.go:818-821`。
- scorer 仅排除 event type `resonance`：`internal/detector/resonance.go:103-106`。
- market impulse / trend / stress 都使用同一个 symbol `MARKET`：`internal/detector/market_pulse.go:11,1009-1014`。
- `directionBias` 不识别任何 market event，因此它们全部 neutral：`internal/detector/resonance.go:404-450`。

**影响**

启用 resonance 后，一次 stress 启动可能连续产生 market impulse + market stress，凑够多个“维度”，随后通过 C-02 的绕门控路径发出 `MARKET resonance`。

### C-13 notification timezone 与 Telegram parse mode 是无效配置

**证据**

- `notificationTimezone` 默认/example 为 `Asia/Shanghai`：`internal/config/config.go:85`、`config/config.yaml.example:14`。
- runtime 只在启动日志读取它：`cmd/kairosd/main.go:33`；消息 formatter 始终标 `UTC`。
- `telegram.parseMode` 存在于 type/default/example：`internal/types/types.go:245`、`internal/config/config.go:89`、`config/config.yaml.example:22`。
- Telegram client 硬编码 `ParseMode: "HTML"`：`internal/notify/telegram.go:41-50`。

**影响**

用户修改这两个字段不会改变通知行为。

### C-14 `allowIsolatedExtremeWhenQuiet` / `isolatedExtremeMinZ` 是公开但未实现的开关

**证据**

- 字段公开于 config type/example：`internal/types/market_pulse.go:49-51`、`config/config.yaml.example:253-254`。
- gate 中仅读取后丢弃阈值，最后仍无条件返回 true（压制）：`internal/engine/pipeline.go:1225-1239`。

**影响**

设置 `allowIsolatedExtremeWhenQuiet: true` 仍不会放行任何 price velocity 事件。

### C-15 `Detector` interface 声称覆盖所有 detector，但并非实际抽象

**证据**

- 注释称“all anomaly detectors implement”，interface 强制四种输入：`internal/detector/detector.go:12-20`。
- Pipeline 从未持有 `Detector`；全部依赖 concrete fields：`internal/engine/pipeline.go:101-119`。
- 只有 Liquidation 有 compile-time assertion：`internal/detector/liquidation.go:170`。
- `MarketPulseDetector` 没有 `OnMetricsUpdate/OnLSSnapshot/OnLiquidationSnapshot`，不满足该 interface；`ResonanceScorer` 事件 channel 类型也不同。
- FuturesMetrics 注释明确 interface 缺 price，故事件强制写 `price: 0.0`：`internal/detector/futuresmetrics.go:76-78,171-178,233-241`。

**影响**

interface 是未使用的“大而全”伪合约，迫使 concrete 实现大量 no-op，同时没有为新增 detector 提供编译保证；还直接造成 futures metric alert 无价格上下文。

### C-16 有效的零值被不同 adapter 当成“缺失”

**证据**

- Binance / Bybit 仅在 24h change `v != 0` 时设置 `Ticker.ChangePct`：`internal/exchange/binance.go:107-109,142-144,252-254`、`internal/exchange/bybit.go:123-126,172-175`。
- funding 也只在非零时设置：`internal/exchange/binance.go:215-217`、`internal/exchange/bybit.go:130-132,179-181`、`internal/exchange/okx.go:549-551`。
- OKX 只要 open/last 有效就设置 change pointer，包括 0：`internal/exchange/okx.go:168-172`。
- scanner 将 nil change 解释为数据缺失并加 warning：`internal/scanner/scoring.go:42-68`。

**影响**

横盘 0% 和真实 0 funding 在 Binance/Bybit/OKX 被错误表示为 absent；同一 `Ticker` optionality 在 adapter 间不一致。

### C-17 OHLCV timestamp 单位在共享类型中不稳定

**证据**

- exchange adapters 将 API milliseconds 除以 1000，`Candle.Timestamp` 实际存 Unix seconds：`internal/exchange/helpers.go:74-82`、`internal/exchange/bybit.go:216-224`。
- scanner `OHLCVToArrays` 原样复制 seconds：`internal/scanner/helpers.go:142-158`。
- backtest `candlesToArrays` 乘 1000，存 milliseconds：`internal/backtest/engine.go:595-612`。
- 共享 `OHLCVArrays.Timestamps []float64` 没有单位命名或注释：`internal/types/types.go:201-207`。
- `BoxPattern.StartTime/EndTime` 直接继承输入时间数组，因此也可能是 seconds 或 milliseconds：`internal/types/types.go:145-160`、`internal/indicators/boxpattern.go:173-174`。

**影响**

同一个共享类型按 producer 不同使用两种单位，序列化与跨模块复用容易产生 1000 倍错误。

### C-18 MarketPulse 300s trend 的样本数没有独立 gate

**证据**

- `ValidSymbols` 只等于有 60s return 的 `moves` 数：`internal/detector/market_pulse.go:463-482`。
- 300s returns 仅对 `m.ok300` 子集 append：`internal/detector/market_pulse.go:497-509`。
- `MinValidSymbols` gate 检查的是 60s `ValidSymbols`：`internal/detector/market_pulse.go:489-493`。
- trend 直接使用可能来自更小子集的 `MedianReturn300s`：`internal/detector/market_pulse.go:1250-1255`，事件却只输出统一 `valid_symbols`。

**影响**

在历史 gap / universe churn 下，trend 可能由远少于 `minValidSymbols` 的 300s 样本触发，消息仍声称较大的 valid count。

### C-19 negative z-score 在 resonance 极端度中被忽略；nil z-score 又被 formatter 显示为 `Z=?`

**证据**

- `extremityZ` 只有 `z > 0` 才取 abs：`internal/detector/resonance.go:324-326`，负向异常不走 direct zscore 分支。
- L/S 与 liquidation producer 在无 z 时保留 key 并赋 `nil`：`internal/detector/longshortratio.go:232`、`internal/detector/liquidation.go:244`。
- formatter 只检查 key 是否存在，就附加 Z 文案：`internal/notify/telegram.go:163-164,189-190`、`internal/notify/dingtalk.go:228-229,251-252`。

**影响**

同等强度的负 z 异常被低估；无 z 的事件显示 `Z=?`，optional contract 不清晰。

### C-20 `alertPolicy.allowedEventTypes: []` 表示“全部允许”，不是“全部禁止”

**证据**

- 只有 policy enabled 且 list 非空时才创建 allow map：`internal/engine/pipeline.go:154-160`。
- map 为 nil 时跳过 event type gate：`internal/engine/pipeline.go:1132-1137`。

**影响**

显式空列表通常被理解为 deny-all，但当前是 allow-all。需文档化或改为三态语义。

### C-21 dedup window 与 symbol cooldown 使用完全相同的 key / timestamp

**证据**

- 两次检查都读取 `p.dedupLast[dedupKey]`，key 均为 `symbol__event_type`：`internal/engine/pipeline.go:1064-1079`。
- 默认 dedup 5s、cooldown 45m：`config/config.yaml.example:35-36`。

**影响**

5 秒 dedup 检查被 45 分钟 cooldown 完全覆盖，不存在独立“重复 payload”语义；配置名暗示两个层次，实际只有一个 per-symbol/event cooldown。

---

## P2 / 一致性与维护性发现

### C-22 多个配置字段只存在于 schema/default/example，运行时未使用

repo-wide usage 显示以下字段没有实际行为，或只有日志作用：

| 字段 | 证据 / 实际情况 |
| --- | --- |
| `defaultTimeframe` | 仅 type/default/example；scanner 使用 `scanner.timeframes`。 |
| `scanner.intervalSeconds` | 仅 type/default/example；`kairos-alert` 是 one-shot，由外部 cron/timer 调度。 |
| `exchanges.rateLimit/canonicalQuote/settle` | adapters 固定 USDT、固定 client；没有读取这些字段。 |
| `storage.retentionDays/jsonlExport/jsonlPath` | `MarketPulseStore` 无条件基于 `databasePath` 旁路写固定文件名：`internal/storage/market_pulse.go:40-44`。 |
| `charts.cleanupDays` | 未被 chart/runtime 读取。 |
| `AlertMinState/AlertLimit` | env override 写入 Config：`internal/config/config.go:71-77`，但 `cmd/kairos-alert` 独立从 env/flags 读取，不使用 Config 字段：`cmd/kairos-alert/main.go:22-25`。 |

这些字段应删除、实现，或明确标注 deprecated/no-op；否则 example YAML 不是可执行契约。

### C-23 CoinGlass L/S / liquidation source 选择硬编码，忽略配置 primary 和 Bybit-only universe

**证据**

- L/S 固定先取 `symbolsByExchange["okx"]`，再取 Binance：`internal/engine/pipeline.go:655-660`。
- liquidation 同样：`internal/engine/pipeline.go:755-760`。
- `DataManager.Exchanges` 可以只配置 Bybit，但这两个 detector 将永久无输入。

### C-24 `MarketSnapshot` 序列化不一致；typed enum 没用于对应字段

**证据**

- `MarketSnapshot` 全字段无 json/yaml tags，而同文件 `SymbolMove` 有 snake_case JSON tags：`internal/types/market_pulse.go:106-142`。
- 已定义 `Direction`、`ActionState` typed aliases：`internal/types/types.go:4-19`，但 `Setup.Direction` / `Setup.ActionState` 仍是 plain `string`：`internal/types/types.go:113-127`。

**影响**

如果 snapshot 被直接序列化，会输出 Go PascalCase；Setup 可构造任意非法状态，编译器不能帮助维护 contract。

### C-25 `CycleToDict` 丢失共享类型字段

**证据**

- `MarketCycle` 包含 `MarketCapChange30D`：`internal/types/types.go:131-141`。
- `CycleToDict` 输出其余字段，但不输出 `market_cap_change_30d`：`internal/scanner/helpers.go:171-183`。

### C-26 volume spike 事件单位依赖 ticker 频率，不是时间归一化 volume

**证据**

- 输入 `Ticker.QuoteVolume` 的 schema 名为 24h quote volume：`internal/types/types.go:78`。
- detector 对 cumulative value 做差，但比较的是“平均每 tick delta”，不是每秒/每分钟：`internal/detector/volumespike.go:25,141-195`。
- event 字段只叫 `recent_avg/window_avg`，没有单位：`internal/detector/volumespike.go:222-229`。

**影响**

不同 exchange / symbol 的 WS 推送频率变化会改变 ratio。字段名不足以让 consumer 判断其单位。

### C-27 文档 / spec 与 runtime event registry 已漂移

**证据**

- authoritative baseline 只列四种 realtime event：`docs/architecture.md:58-63`，且 runtime 名称仍写 `kairos-watch`：`docs/architecture.md:37`。
- runtime 还产生 `long_short_ratio`、`liquidation`、`resonance`、四种 `market_*` 和内部 `market_outcome`。
- README 的算法标签也不准确：称 `price_velocity`、OI、funding 使用 Z-score：`README.md:82-85`；实际 price velocity 是窗口 threshold，OI 是相邻 poll 百分比，funding 是绝对值/shift。
- spec 声明 `docs/architecture.md` 为产品 source of truth：`.trellis/spec/backend/signal-alert-contracts.md:6`。

### C-28 人工控制文案未统一满足 spec 的精确句子

**证据**

- spec 当前要求 `仅供人工判断，不自动交易。`：`.trellis/spec/backend/signal-alert-contracts.md:26`。
- 只有 MarketPulse formatter 包含“不自动交易”：`internal/notify/telegram.go:228`、`internal/notify/dingtalk.go:291`。
- generic/resonance/liquidation/L/S 仅写 `仅供人工判断`：`internal/notify/telegram.go:88,111,169,195`、`internal/notify/dingtalk.go:169,192,237,260`。
- scanner summary 也只写 `仅供人工判断`：`internal/alert/alert.go:87`。

---

## `AnomalyEvent.Data` 生产 / 消费核对矩阵

| EventType | Producer keys | Consumer | 结论 |
| --- | --- | --- | --- |
| `price_velocity` | `change_pct`, `window_seconds`, `threshold`, `price_from`, `price_to` (`internal/detector/pricevelocity.go:168-174`) | policy / buildCondition / generic formatter | 键一致；无 zscore，故 isolated extreme 配置无法实现。 |
| `volume_spike` | `price`, `ratio`, `recent_avg`, `window_avg`, `window_minutes` (`internal/detector/volumespike.go:222-228`) | policy / buildCondition / resonance formatter | 键一致；`recent_avg/window_avg` 单位不明确。 |
| `open_interest_change` | `price`, `open_interest`, `previous_open_interest`, `change_pct`, `threshold_pct` (`internal/detector/futuresmetrics.go:171-178`) | policy / buildCondition / resonance | 键一致；price 永远 0。 |
| `funding_rate_anomaly` | `price`, `funding_rate`, `previous_funding_rate`, `change_abs`, thresholds, `reason` (`internal/detector/futuresmetrics.go:233-242`) | policy / buildCondition / resonance | 键一致；price 永远 0，rate 是 decimal fraction 而不是 percent。 |
| `long_short_ratio` | rates、`ls_ratio`、reason、trigger、zscore、thresholds (`internal/detector/longshortratio.go:220-232`) | specialized formatter / resonance | 主键一致；nil z optionality 错误见 C-19。 |
| `liquidation` | USD、millions、side pct、reason、trigger、zscore、thresholds (`internal/detector/liquidation.go:229-244`) | policy / specialized formatter / resonance | 主键一致；nil z optionality 错误见 C-19。 |
| `resonance` | `signal_score`, `dimension_count`, `dimensions`, `<dim>_data` (`internal/detector/resonance.go:23-32`; pipeline duplicate producer at `980-990`) | specialized formatter | `dimensions` concrete type mismatch，见 C-11。 |
| `market_impulse/trend/stress/decay` | direction/state/returns/z/breadths/counts/leaders/laggards/shadow (`internal/detector/market_pulse.go:974-999`) | policy / storage / specialized formatter | storage keys一致；down formatter 与 window 字段错误，见 C-04/C-05。 |
| `market_outcome` | source/direction/horizon returns/MFE/MAE/breadth/duration/precision (`internal/detector/market_pulse.go:1157-1172`) | `MarketPulseStore.RecordOutcome` (`internal/storage/market_pulse.go:137-154`) | 键一致；明确被 delivery 排除 (`internal/engine/pipeline.go:838-840,1036-1038`)。 |

---

## 配置结构对齐结果

- `types.Config` 的 `mapstructure/json/yaml` key 与 `setDefaults`、`config/config.yaml.example` 的主结构总体一致，未发现明显拼写错位。
- `watchHintsPath` 只存在于 type、未列入 defaults/example，但 storage 明确提供 `databasePath` 同目录 fallback：`internal/storage/hints.go:34-39`，属于可接受 optional field。
- 主要问题不是 key 拼写，而是：**无 validation、重复 authority、公开 no-op 字段、零值/空列表语义不清、secret JSON tags 不安全**。

## 测试盲区

现有 `go test ./...` 与 `go vet ./...` 均通过，但以下跨层断言缺失：

1. 使用真实 resonance producer shape 调 formatter（会暴露 `[]string` / `[]any`）。
2. `buildAlert` 保留 timestamp / exchange / event ID。
3. down market alert 使用 decliners / laggards，且 down trend 文案正确。
4. 修改 MarketPulse `windowSeconds` 后实际计算窗口变化。
5. Binance / Bybit backtest pagination。
6. config validation（timeframes、负 limits、ratio/alpha ranges、confirmation relations）。
7. resonance 必须经过 allow-list/policy。
8. primary exchange 与 realtime exchange list 一致性，以及 refresh 后真实 subscription 更新。
9. Config JSON marshal 不包含 secret。
