# 通知与持久化闭环审查

> 范围：Telegram、DingTalk、alert policy / dedup / cooldown、HintStore、MarketPulse events / snapshots / outcomes、重启与失败处理。仅静态审查与现有测试验证，未修改代码。

## 1. 端到端流图

```text
Exchange WS / pollers
  -> detector-local history + detector-local cooldown
  -> buffered detector event channel (full => non-blocking drop)
  -> mergeChannels
  -> eventAggregator
       -> ResonanceScorer（当前也接收 market_* / market_outcome）
       -> MarketPulse JSONL（在 alert policy / delivery 之前）
       -> deliveryCh
  -> deliverEvent
       -> channel-present gate
       -> shadow / blacklist / quiet-market gate
       -> alert policy
       -> process-local dedup + cooldown（发送前写入）
       -> AlertEvent conversion
       -> Telegram then DingTalk（串行 fan-out）
       -> any channel success => HintStore append
  -> scanner ActiveBoosts => candidate_score boost
```

## 2. 高优先级发现

### F1 — HIGH — DingTalk 加签值被二次 URL 编码，启用 `DINGTALK_SECRET` 时签名预计无效

**证据**

- `dingTalkSign` 已返回 `url.QueryEscape(base64(...))`：`internal/notify/dingtalk.go:136-142`。
- `signedURL` 又把该结果交给 `url.Values.Set` + `Encode`：`internal/notify/dingtalk.go:115-131`。
- 现有测试只断言 query 中存在 `timestamp=` / `sign=`，未验证服务端解码后的 sign：`internal/notify/dingtalk_test.go:32-57`。

复现 URL 编码语义：`abc+/=` 先变成 `abc%2B%2F%3D`，再经 `url.Values.Encode` 变成 raw query `abc%252B%252F%253D`；服务端一次 query decode 后得到的仍是 `abc%2B%2F%3D`，不是原始 Base64 签名。

**影响**：示例配置推荐加签（`config/config.yaml.example:24-28`），但签名机器人会拒绝请求；未加 secret 的机器人不受影响。

### F2 — HIGH — daemon 不监控 `Pipeline.Start` 的提前退出，会留下“进程存活但告警管线已死”的假健康状态

**证据**

- 启动通知在 pipeline 真正启动前发送，且错误被忽略：`cmd/kairosd/main.go:64-70`。
- `Pipeline.Start` 放进 `errCh`：`cmd/kairosd/main.go:73-76`。
- 主 goroutine 只等待 OS signal 的 `ctx.Done()`：`cmd/kairosd/main.go:78-79`；直到关机阶段才读取 `errCh`：`cmd/kairosd/main.go:88-95`。
- 任一 errgroup goroutine返回错误即可使 `Pipeline.Start` 返回：`internal/engine/pipeline.go:301-389,402-408`。

**影响**：WS 订阅或其他 goroutine 提前失败后，systemd 仍看到主进程存活，不会重启；用户还可能已收到“kairosd started”。这是通知闭环的静默失效。

### F3 — HIGH — universe refresh 只换统计名单，不更新实际订阅；并且共享 map 存在并发读写

**证据**

- 初始 WS 订阅永久使用 `es.symbols`：`internal/engine/pipeline.go:243-251,310-317`。
- metrics 也永久遍历 `es.symbols`：`internal/engine/pipeline.go:594-604`。
- refresh 仅写 `p.symbolsByExchange[name]` 并调用 `MarketPulse.UpdateUniverse`：`internal/engine/pipeline.go:1282-1322`；没有更新 `es.symbols`，也没有取消/重订阅 WS。
- CoinGlass poller 并发读取 `symbolsByExchange`：`internal/engine/pipeline.go:655-659,755-759`；refresh 并发写该 map，无 mutex。

**影响**：新 Top-N 成员进入 MarketPulse 分母，却收不到 ticker；成员变化积累后 fresh ratio 会长期被压低并停止市场告警。旧成员仍从 WS 流入但被 MarketPulse 排除。并发碰撞时存在 Go map data race。现有 race 测试没有执行真实 refresh tick（`internal/engine/start_test.go:83-89` 只测试取消）。

### F4 — HIGH — MarketPulse 下跌通知使用了错误的币种列表、计数和趋势文案

**证据**

- Snapshot 明确把强者放 `Leaders`、弱者放 `Laggards`：`internal/detector/market_pulse.go:558-592`。
- event 同时输出 `leaders` 和 `laggards`：`internal/detector/market_pulse.go:965-991`。
- Telegram/DingTalk 无论方向都读取 `leaders`，方向为 down 时仅把标题改成“领跌”：`internal/notify/telegram.go:255-265`、`internal/notify/dingtalk.go:316-327`。
- 非 trend 的方向广度分子固定读取 `advancers`：`internal/notify/telegram.go:219,246`、`internal/notify/dingtalk.go:282,308`；down 事件的 `breadth` 却是 down breadth（`internal/detector/market_pulse.go:959-963`）。
- `market_trend` 无论 down/up 都写“5分钟市场中位涨幅 / 上涨广度”：`internal/notify/telegram.go:234-236`、`internal/notify/dingtalk.go:296-298`。
- 现有 down stress 测试把弱币故意塞在 `leaders`，因此没有覆盖 detector→formatter 契约：`internal/notify/format_test.go:169-185`。

**影响**：下跌告警会把相对最强币标成领跌，显示 `down breadth` 搭配 `advancers / valid`，下跌趋势仍称“上涨”。直接误导人工看盘。

### F5 — HIGH — 通用 45 分钟 cooldown 会覆盖 MarketPulse 自己的 5/10 分钟 cooldown，并丢掉快速反向状态事件

**证据**

- Detector key 包含 `eventType:direction`，配置使用 `cooldownSeconds` / `stressCooldownSeconds`：`internal/detector/market_pulse.go:926-956`。
- Delivery key 只有 `MARKET__eventType`，没有 direction / state transition：`internal/engine/pipeline.go:1064-1080`。
- 同一个 key 先做 5 秒 dedup，再做默认 45 分钟 symbol cooldown：`internal/engine/pipeline.go:1067-1080`；两次读取实际是同一个 map/key。
- 默认通用 cooldown 为 45 分钟：`internal/config/config.go:96-103`；MarketPulse 默认是 600 秒、stress 300 秒：`internal/config/config.go:341-342`。

**影响**：例如 `market_impulse up` 后 45 分钟内合法的 `market_impulse down` 会被 delivery 层压掉，尽管 detector 已完成 `DECAY -> IMPULSE_DOWN`。持久化日志会有该事件（见 F10），用户却收不到。

### F6 — HIGH — cooldown 在真正发送前提交；瞬时失败或部分 fan-out 失败后不会重试

**证据**

- `dedupLast[dedupKey]` 在 build/send 前写入：`internal/engine/pipeline.go:1064-1083`。
- Telegram 与 DingTalk 失败仅记录日志；`sendToChannels` 没有重试、outbox 或 per-channel 状态：`internal/engine/pipeline.go:923-946`。
- HintStore 只要求任一 channel 成功：`internal/engine/pipeline.go:1083-1089`。
- resonance 路径同样在 send 前写 cooldown：`internal/engine/pipeline.go:965-1007`。

**影响**：两渠道都失败时，事件仍进入 45 分钟 cooldown；一成一败时，失败渠道也被共同 cooldown 阻止重试。该行为虽然符合“best-effort”描述（`.trellis/spec/backend/error-handling.md:70`），但不是可靠 fan-out 闭环，也无法从状态判断哪个 channel 实际收到。

### F7 — HIGH — detector-local cooldown 在 alert policy 之前消耗，且 channel 满时也不回滚

**证据**

- 项目契约要求 policy 在 delivery 和 dedup/cooldown 状态变更前：`.trellis/spec/backend/signal-alert-contracts.md:29`。
- PriceVelocity 在 enqueue 前写 `lastNotify`：`internal/detector/pricevelocity.go:147-180`。
- VolumeSpike 同样在 enqueue 前写：`internal/detector/volumespike.go:197-234`。
- futures/LS/liquidation 的 `canNotify` 也会先写 cooldown，随后 non-blocking enqueue 可失败：`internal/detector/futuresmetrics.go:157-178,211-251,255-264`、`internal/detector/longshortratio.go:113-153,237-240`、`internal/detector/liquidation.go:109-158,249-252`。

**影响**：低流动性币先触发 detector 阈值但被 downstream liquidity policy 拒绝后，随后更强、已满足 policy 的行情仍可能因 detector cooldown 不再发出。channel full 时事件被永久丢弃且本地 cooldown 已烧掉。

## 3. 中优先级发现

### F8 — MEDIUM — Shadow Mode 可以改变单币 Telegram 行为；isolated-extreme 开关是无效配置

**证据**

- 类型契约明确 ShadowMode “must not change Telegram single-symbol behaviour”：`internal/types/market_pulse.go:20-22`。
- `deliverEvent` 对单币 quiet gate 没有检查 ShadowMode：`internal/engine/pipeline.go:1040-1056`。
- `shouldGateIndividualAlert` 在 QUIET 时即返回 true；`AllowIsolatedExtremeWhenQuiet` 分支只把配置赋给 `_`，没有放行逻辑：`internal/engine/pipeline.go:1224-1240`。
- 测试甚至固定了 `shadowMode: true + gate=true => suppress`：`internal/engine/pipeline_test.go:90-127`。

**影响**：误把 quiet gate 与 shadow 同时开启会在“观察模式”静默压掉旧单币告警；配置 `allowIsolatedExtremeWhenQuiet: true` 也不会产生预期效果。

### F9 — MEDIUM — AnomalyEvent→AlertEvent 转换丢失 timestamp、exchange、event ID；配置时区未生效

**证据**

- `AlertEvent` 有 `EventID/Timestamp/Exchange`：`internal/types/types.go:187-198`。
- `buildAlert` 只填 Event/Symbol/Price/Condition/ChangePct/Severity/Data：`internal/engine/pipeline.go:1201-1221`。
- resonance alert 同样不填时间和交易所：`internal/engine/pipeline.go:993-1001`。
- 两个 formatter 都读取 `event.Timestamp` 并固定标 `UTC`：`internal/notify/telegram.go:72,89`、`internal/notify/dingtalk.go:153,170`。
- `notificationTimezone` 只被加载并在启动日志打印：`internal/config/config.go:85`、`cmd/kairosd/main.go:33`；未参与格式化。

**影响**：实时通知通常显示空白时间（`" UTC"`），也没有来源交易所和稳定 event ID；配置的 `Asia/Shanghai` 是死配置。测试直接构造带 Timestamp 的 AlertEvent，绕过了实际转换路径（`internal/notify/telegram_test.go:10-20`、`internal/notify/dingtalk_test.go:60-70`）。

### F10 — MEDIUM — MarketPulse JSONL 记录的是“detector emitted”，不是“用户已收到”；无法从持久化数据统计真实通知

**证据**

- eventAggregator 在 shadow/policy/dedup/send 之前记录 market event：`internal/engine/pipeline.go:817-844`。
- 真正的 shadow/policy/dedup/channel send 在后续 `deliverEvent`：`internal/engine/pipeline.go:1030-1089`。
- `MarketPulseRecord` 没有 policy result、delivery status、channel result 或 notification ID：`internal/storage/market_pulse.go:14-30`。

**影响**：events JSONL 会包含 shadow、policy 拒绝、cooldown 拒绝和发送失败的事件。它适合 detector 校准，但不能用来计算 Telegram/DingTalk 告警数、成功率或“用户收到后”的 outcome。

### F11 — MEDIUM — MarketPulse snapshots 仅保存在内存；storage retention / JSONL 开关未被执行，文件无限增长

**证据**

- 每 5 秒 snapshot 只写 `lastSnap`：`internal/detector/market_pulse.go:396-407`；持久层仅有 events/outcomes 两个文件：`internal/storage/market_pulse.go:40-47`。
- `StorageConfig.RetentionDays/JSONLExport/JSONLPath` 存在：`internal/types/types.go:421-430`，但仓库 Go 代码除定义外没有引用。
- MarketPulse 与 HintStore 都以 `O_APPEND` 写 JSONL，没有 prune/rotation/compaction：`internal/storage/market_pulse.go:97-102,163-168`、`internal/storage/hints.go:72-81`。

**影响**：无法做完整 5 秒快照回放；`retentionDays: 90` 和 `jsonlExport: false` 不控制这些文件，长期运行磁盘和 HintStore 扫描成本持续增长。

### F12 — MEDIUM — outcome 在数据断流时仍用 stale last price 填 horizon；channel 满时完成 outcome 被永久删除

**证据**

- `EvaluateAt` 无条件在 snapshot/state 后调用 `updateOutcomesLocked`：`internal/detector/market_pulse.go:396-407`。
- `updateOutcomesLocked` 不检查 `snap.DataOK`，直接用事件价格与当前 `lastPrice` 计算：`internal/detector/market_pulse.go:1062-1092,1123-1135`。
- 到 15m 后调用 `emitOutcomeLocked`，无论 enqueue 成功与否都不再放回 `remaining`：`internal/detector/market_pulse.go:1113-1120`。
- enqueue full 只日志：`internal/detector/market_pulse.go:1190-1198`。
- pending outcomes 超过 32 时静默截掉最旧记录：`internal/detector/market_pulse.go:1057-1058`。

**影响**：行情断流跨过 +1m/+3m/+5m/+15m 时会记录 stale/零收益并污染 precision；高压时 outcome 直接丢失，无法事后补算。

### F13 — MEDIUM — 所有 restart-sensitive 状态都只在内存，重启会重复/漏掉事件与 outcome

**证据**

- Pipeline dedup map 每次构造为空：`internal/engine/pipeline.go:125-150`。
- MarketPulse 每次构造从 QUIET、空 `lastEmitted` 开始：`internal/detector/market_pulse.go:105-119`。
- `pendingOutcomes` 仅内存持有；Reset 明确清空：`internal/detector/market_pulse.go:55-56,264-281`。
- 启动只 `UpdateUniverse`，没有读取 event/state/outcome 文件恢复：`internal/engine/pipeline.go:267-280`。

**影响**：重启后先经历 warmup，期间可能漏掉整体行情；随后仍持续的行情可能再次发 impulse。重启前未满 15 分钟的 outcomes 永久消失；45 分钟 delivery cooldown 也重置，可能重复通知。

### F14 — MEDIUM — HintStore 闭环丢失真实 exchange provenance，并静默吞掉读取/解析错误

**证据**

- 所有普通事件和 resonance 都把 `cfg.Exchanges.Primary` 写入 hint，而不是事件来源：`internal/engine/pipeline.go:1006-1007,1088-1089`。
- `AnomalyEvent` 本身没有 exchange 字段：`internal/types/events.go:3-10`。
- `readActiveLocked` 对非 ENOENT 打开错误也直接返回空，对坏 JSON 直接 continue，并且不检查 `Scanner.Err()`：`internal/storage/hints.go:98-128`。
- scanner 只消费 symbol→boost，完全不使用 eventType/exchange：`internal/scanner/scanner.go:392-400`。

**影响**：多交易所事件全部被标成 primary；损坏、权限错误、超长 scanner token 等问题表现为“没有 boosts”，没有日志或 warning。HintStore 的 eventType/exchange 当前是写入但不参与评分的审计字段。

### F15 — MEDIUM — ResonanceScorer 接收 MarketPulse/outcome，且窗口只按 symbol 整体清理，会保留过期维度

**证据**

- aggregator 除 `resonance` 外把所有事件都送给 scorer；`market_outcome` 的 continue 发生在此之后：`internal/engine/pipeline.go:817-838`。
- scorer 只排除 `resonance`：`internal/detector/resonance.go:103-107`。
- `pruneWindows` 仅在一个 symbol 的所有维度都过期时删除整个 symbol；不会逐 eventType 删除过期维度：`internal/detector/resonance.go:139-153`。
- `evaluate` 直接按 map 的总维度数计分：`internal/detector/resonance.go:156-178`。

**影响**：`MARKET` 的 impulse/trend/stress/outcome 可成为“共振维度”；单币旧的高 extremity 维度也可被新事件续命并继续计数，产生重复或过期 resonance。低默认配置下不一定触发，但契约边界不干净。

## 4. 低优先级 / 行为不一致

### F16 — LOW — MarketPulse event payload 丢失 leader/laggard 的 return/z，且所有事件的 `window_seconds` 固定成 impulse window

- Snapshot 使用 `[]SymbolMove`，含 `ReturnPct/RelativePct/ZScore`：`internal/types/market_pulse.go:128-143`。
- emit 时降级成 `[]string`：`internal/detector/market_pulse.go:965-991`，Telegram/DingTalk 因此只能显示币名，不能显示涨跌幅。
- `window_seconds` 对 trend/stress 也固定取 `cfg.Impulse.WindowSeconds`：`internal/detector/market_pulse.go:978`。

### F17 — LOW — `kairos-alert` 忽略 `telegram.enabled`，且没有跨轮次 dedup/cooldown

- realtime daemon 会检查 `cfg.Telegram.Enabled`：`cmd/kairosd/main.go:35-47`。
- one-shot 只检查 token/chat ID，直接发送：`cmd/kairos-alert/main.go:62-77`。

**影响**：YAML 中关闭 Telegram 不能阻止 cron one-shot 在环境凭证存在时发送；重复候选由 cron 周期决定，没有 EventID/fingerprint delivery 去重。

## 5. 已确认正常的闭环

- Alert policy 在 pipeline 的 blacklist/quiet gate 后、delivery dedup 前执行：`internal/engine/pipeline.go:1047-1063`。
- Market event policy 正确绕过单币 liquidity weight：`internal/engine/pipeline.go:1111-1152`。
- `market_outcome` 在 delivery 前被明确阻止通知：`internal/engine/pipeline.go:836-839,1035-1038`。
- MARKET / market event 不进入 HintStore boost：`internal/engine/pipeline.go:1094-1103`。
- Hint 仅在至少一个 delivery channel 成功后记录：`internal/engine/pipeline.go:1083-1089`。
- Telegram 和 DingTalk 的普通/市场文案均保留人工判断边界；MarketPulse 明确写“不自动交易”：`internal/notify/telegram.go:229-230`、`internal/notify/dingtalk.go:290-291`。

## 6. 测试结果与覆盖盲区

执行：

```text
go test ./internal/engine ./internal/notify ./internal/storage ./internal/detector
PASS

go test -race ./internal/engine ./internal/notify ./internal/storage ./internal/detector
PASS
```

现有测试没有覆盖：

1. signed DingTalk URL 服务端解码后的 sign 值；
2. `AnomalyEvent -> buildAlert -> formatter` 的 timestamp/exchange round-trip；
3. down MarketPulse 使用真实 detector payload 的 laggards/decliners；
4. send failure 后 dedup/cooldown 是否提交；
5. up→down 同 event type 的 MarketPulse reversal；
6. refresh 后 WS 重订阅与 race；
7. restart state/dedup/outcome 恢复；
8. stale-data outcome horizon；
9. persistence 中“detected / policy-passed / channel-delivered”的区分。

## 7. 建议修复顺序（供实现 Agent）

1. F1、F2、F3、F4：先恢复渠道可用性、daemon fail-fast、universe 数据一致性、下跌文案正确性。
2. F5、F6、F7：统一 delivery acknowledgement 与 cooldown 提交语义；MarketPulse key 至少包含 direction，并使用自己的 cooldown。
3. F8、F9、F10：修复 shadow 契约、AlertEvent 元数据与 delivery audit 状态。
4. F11–F15：再补 rotation/retention、restart/outcome 恢复、HintStore diagnostics、resonance event allow-list。
