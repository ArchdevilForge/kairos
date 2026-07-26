# Design：Kairos 全仓契约与流程闭环修复

## 1. 设计原则

1. 修复共享根因，不在每个 caller 复制 guard。
2. 让配置只描述真实能力；固定算法参数使用代码常量。
3. 保持单体 Go 架构和 best-effort 通知，不引入队列/新服务。
4. 关键生产错误 fail fast，由 systemd 等 supervisor 重启；不吞 panic 伪装健康。
5. 维持人工决策、不自动交易的产品边界。

## 2. 边界与权威

### 2.1 运行入口

- `kairosd`：实时数据、detector、policy、通知、hint/outcome。
- `kairos-alert`：一次性 scanner，独立 summary 通知。
- `kairos-backtest`：统一 exchange OHLCV 区间契约。
- `kairos-market-replay`：MarketPulse 算法回放，不宣称覆盖生产 delivery。

### 2.2 Exchange 配置权威

- `exchanges.primary` 是 scanner、MarketPulse 和 provenance 的 primary authority。
- `dataManager.exchanges` 是 realtime 实际启用集合。
- validation 要求 primary 必须出现在 realtime 集合中。
- 顶层 `exchange` 仅作为旧配置兼容 alias；loader 规范化到上述两个字段，文档/example 不再公开它。
- CoinGlass 轮询从 primary 或首个启用 exchange 的 universe 取 symbol，不再硬编码 OKX/Binance。

## 3. 共享事件契约

### 3.1 `AnomalyEvent`

增加：

- `Exchange string`
- `EventID string`

保留 `Timestamp float64`，明确单位为 Unix seconds。Pipeline 在 detector channel 进入 aggregator 时统一补齐：

- 来源 exchange；
- 缺失 timestamp；
- 基于 exchange/symbol/event/timestamp/payload 的确定性 event ID。

`buildAlert` 负责把 Unix seconds 转为 RFC3339 UTC；formatter 不再接收空时间。HintStore 使用事件真实 exchange。跨交易所同币告警仍按 symbol 级 cooldown 去噪，但 provenance 不再丢失。

### 3.2 Map payload

- notify 包增加一个小型 typed parser/view，将 MarketPulse payload 一次解析后供 Telegram/DingTalk 各自渲染。
- down 方向选择 `decliners`、`laggards`、下跌文案；up 方向选择 `advancers`、`leaders`。
- resonance dimensions parser 同时接受进程内 `[]string` 和 JSON round-trip `[]any`。
- nil z-score 不写 payload；负 z-score 使用绝对值计算 extremity。
- `MarketSnapshot` 补 snake_case JSON tags；`Setup` 使用已有 typed direction/action state。

## 4. Exchange 与市场数据契约

### 4.1 OHLCV

将 `FetchOHLCV` 最后一个参数明确为 `beforeMs`：返回不晚于该时刻的 candles；`0` 表示最新。三家 adapter 分别使用 OKX `after`、Binance `endTime`、Bybit `end` 实现同一语义。返回统一按时间升序。

`Candle.Timestamp` 和 `OHLCVArrays.Timestamps` 统一为 Unix seconds；backtest 不再乘 1000。回测 runner 继续从 end 向 start 倒序分页，不再包含 exchange 特判。

### 4.2 Optional metrics

`Ticker` 中的 OI/funding pointer 保留“缺失”和“真实 0”的区别。adapter 只要字段解析成功就设置 pointer，不再以 `value != 0` 判断存在性。metrics detector 接收 optional 值并只评估实际存在的维度，同时携带真实 ticker price。

### 4.3 Volume rate

volume spike 使用累计 quote-volume 的 `delta / elapsedSeconds` 作为采样值，而不是每 tick delta，避免 WS 推送频率改变 ratio。对 reset/负 delta 继续安全跳过。

## 5. Universe 热刷新

保持 `refreshIntervalHours` 的真实热更新承诺：

1. Pipeline 为 universe 提供受锁保护的 snapshot/update helper，消除共享 map race。
2. 每个 exchange subscription 由可重启 loop 管理；收到新 symbol snapshot 后取消旧订阅，等待退出，再用新列表订阅。
3. metrics 与 CoinGlass poll 每轮读取最新 snapshot，不再捕获启动时 slice。
4. MarketPulse 只在 primary universe 更新。
5. market-cap reference 使用一致 snapshot。
6. 三家 WS reconnect backoff 改为 context-aware timer，确保重订阅和 shutdown 可及时取消。

不新增独立 subscription manager interface；现有 Pipeline 内部 helper 足够。

## 6. Lifecycle 与取消

- 将 `kairosd` 主流程拆出可测试的 `run`/wait helper：同时 select OS context 和 `Pipeline.Start` 结果。
- pipeline 先退出/停止并等待 goroutine，再关闭 exchange；提前错误返回非零，systemd 可感知。
- CoinGlass、Telegram、DingTalk 的 HTTP/子进程路径接收 caller context；不再从 `context.Background()` 创建生产请求。
- scanner fallback 始终关闭当前实际 exchange，避免 backup 泄漏或 primary double-close。
- detector panic 不做局部 recover：编程错误直接让进程失败，由 supervisor 重启，避免“部分 detector 已死但进程健康”。

## 7. Policy、dedup 与 delivery

- resonance 转成普通 `AnomalyEvent` 后复用 `deliverEvent`，不再维护一条绕过 policy 的发送路径。
- scorer 只接收单币 detector 维度，排除 `MARKET`、`market_outcome`、`resonance`。
- shadow MarketPulse 不启用 quiet 单币 gate；`shadowMode=false` 时 gate 才生效。
- 删除未实现的 isolated-extreme 配置，而不是保留 no-op。
- dedup 和 symbol cooldown 使用独立状态：dedup 针对 event key，cooldown 针对 symbol；MarketPulse 依赖自身方向/状态 cooldown，不套用通用 45 分钟 symbol cooldown。
- 只有至少一个通知 channel 成功后才提交 delivery dedup/cooldown；全部失败时不创建 outbox，但允许后续事件再次尝试。
- MarketPulse outcome 只在 fresh/DataOK snapshot 上采样；错过有效 horizon 的记录不使用 stale price 伪造结果。

## 8. 配置与 validation

集中 `Validate/Normalize` 在 `Load`/`LoadString` 返回前执行，至少检查：

- primary/realtime exchange 一致；
- scanner core timeframes 恰含 `1d/4h/15m`；
- universe/candidate/deep-analysis limits 与 timeout 为合法正值且层级合理；
- MarketPulse ratio/alpha 在 `[0,1]`；confirmation samples 不大于 window samples；retention/warmup 覆盖固定 60s/300s 窗口；
- alert policy enabled 时 allow-list 非空、severity 合法；
- detector interval/threshold/duration 合法。

移除无运行时消费者的公开字段/default/example，包括 default timeframe、notification timezone、Telegram parse mode、scanner internal interval、exchange rate/quote/settle、storage retention/jsonl knobs、chart cleanup、env-only duplicate scanner alert fields及其他确认 no-op。旧 YAML 未知字段保持宽容。

MarketPulse 60s/300s 计算窗口改为代码常量，移除三个伪 `windowSeconds` 配置；event 按类型报告真实窗口。300s trend 增加独立有效样本数 gate。

Secret 字段统一 `json:"-" yaml:"-" mapstructure:"-"`。

## 9. 文档与兼容性

- 更新 `docs/architecture.md`、README、config example 和 backend spec，使入口、事件、算法和人工控制文案与代码一致。
- `kairos-alert` 尊重 `telegram.enabled`；dry-run 不受影响。
- 删除配置字段属于 schema 收缩；旧 YAML 因宽容解析继续启动，但无效字段不再被宣传。
- 不修改生产服务器配置，不新增数据库 migration。

## 10. 回滚

- 每个修复批次保持可独立回退：types/config、exchange/data、pipeline/lifecycle、notify/policy、docs/tests。
- universe 热刷新若验证失败，可临时把 `refreshIntervalHours` 设为超大值，不影响启动时 universe。
- MarketPulse 可通过 `enabled=false` 或关闭 quiet gate 回滚。
