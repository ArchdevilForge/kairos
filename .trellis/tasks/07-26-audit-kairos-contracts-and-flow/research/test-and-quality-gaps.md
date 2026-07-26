# Kairos 测试与质量门禁审查

> 审查范围：全部 Go package、`*_test.go`、`Makefile`、`.golangci.yml`、GitHub Actions、关键并发/通道/取消路径。  
> 审查方式：源码静态走查 + 本地只读构建/测试/覆盖率/漏洞扫描。未修改生产代码。

## 1. 结论摘要

当前仓库的基础质量门禁可运行，且本次实测全部通过：build、vet、golangci-lint、全量 race test、关键包 shuffle/race、模块校验、依赖漏洞可达性扫描。

但“门禁通过”不能覆盖以下高风险路径：

1. `refreshLoop` 并发读写 `symbolsByExchange`，存在高置信度 data race；刷新后也没有更新现有 WS 订阅和 `exchangeState.symbols`。
2. CoinGlass 轮询、Telegram/DingTalk 发送和 WS 重连退避不能被 pipeline context 及时取消，15 秒 shutdown 上限可能失效。
3. `kairosd` 只等待 OS signal，不监听 pipeline 提前退出；pipeline 崩停后进程可能继续假活。
4. alert dedup/cooldown 的现有测试实际在 `hasDeliveryChannel()==false` 处提前返回，没有测试其命名行为；代码中两个窗口读取同一 key/时间戳，短 dedup 窗口实际上被长 cooldown 覆盖。
5. CI 没有执行已有的 `cover-check`；内部聚合覆盖率仅 `70.044%`，刚好越过 70% 阈值，且关键 package 仍在 56%～67%。
6. CI 没有格式化门禁；`gofmt -l cmd internal tests` 当前报告 22 个文件。

## 2. 实测结果

环境：

- Go：`go1.26.5-X:nodwarf5 linux/amd64`
- golangci-lint：`v2.1.6`
- 测试函数：232
- Benchmark：0
- Fuzz test：0
- `t.Parallel()`：0

| 检查 | 结果 | 说明 |
| --- | --- | --- |
| `make check` | PASS | build + vet + golangci-lint + race tests；首次输出使用了 Go test cache |
| `go test -race -count=1 ./...` | PASS | 强制 fresh，全 18 packages 通过 |
| 关键包 `-race -shuffle=on -count=5` | PASS | `engine exchange detector scanner` |
| `go mod verify` | PASS | `all modules verified` |
| `go mod tidy -diff` | PASS | 无 diff |
| 内部覆盖率等价检查 | PASS，余量极低 | 3622/5171 blocks，`70.044479%`；Makefile 显示为 `70.0%` |
| `govulncheck ./...` | PASS（无可达漏洞） | 0 reachable；依赖树仍含 2 个不可达/平台限定公告，见下文 |
| `gofmt -l cmd internal tests` | FAIL | 22 个文件未格式化 |
| live integration tests | SKIP 3 | CI 未设置 `KAIROS_LIVE*` |

漏洞扫描的已验证结果：

- `GO-2026-5970`：`golang.org/x/text@v0.28.0`，项目未调用漏洞符号；修复版本 `v0.39.0`。来源：<https://pkg.go.dev/vuln/GO-2026-5970>
- `GO-2026-5024`：`golang.org/x/sys@v0.29.0` Windows 路径，项目未调用漏洞符号；修复版本 `v0.44.0`。来源：<https://pkg.go.dev/vuln/GO-2026-5024>

## 3. Package → 测试覆盖映射

覆盖率来自 `go test -count=1 -cover ./...`。

| Package | 测试数 | 覆盖率 | 评价与主要缺口 |
| --- | ---: | ---: | --- |
| `cmd/kairos-alert` | 7 | 53.1% | `runOnce` 主要分支有测；真实 Telegram send/main flag 流未测 |
| `cmd/kairos-backtest` | 3 | 12.5% | multi-symbol goroutine、共享 exchange、错误聚合和 CLI 参数流未测 |
| `cmd/kairos-market-replay` | 0 | 0.0% | `loadTicks`、排序、坏 JSON、空输入、最终确认循环均未测 |
| `cmd/kairosd` | 1 | 5.4% | 只有 `parseChatID`；启动、pipeline 提前退出、signal/shutdown、通知生命周期均未测 |
| `internal/alert` | 9 | 92.2% | 覆盖良好；仍可补 malformed envelope 边界 |
| `internal/backtest` | 15 | 82.2% | 核心计算较好；`FetchBtc1d` 0%，`Run` 55.3% |
| `internal/config` | 9 | 90.7% | defaults/env/example 对齐较好；缺非法范围/负数配置验证 |
| `internal/data` | 26 | 56.3% | 解密 parser 较好；生产 Python/Go fetch 选择、脚本定位、marketcap/RSI live fetch 大量未覆盖 |
| `internal/detector` | 37 | 69.6% | Market Pulse 算法覆盖较强；timer `Start` 0%，多个 detector Reset/UpdateConfig/接口分支 0% |
| `internal/engine` | 31 | 64.4% | helper/policy 有测；真实 delivery、refresh、CoinGlass loops、watch hints、DingTalk fan-out、提前退出薄弱 |
| `internal/exchange` | 23 | 62.9% | REST happy path 使用 `httptest`；三个 WS `readTickers` 均 0%，重连/退避/取消未测 |
| `internal/indicators` | 12 | 80.7% | 主要检测逻辑覆盖较好 |
| `internal/notify` | 15 | 66.7% | 文案格式有测；Telegram `SendText/SendEvent` 0%，DingTalk `SendEvent` 0%，异常响应覆盖有限 |
| `internal/scanner` | 19 | 73.0% | scoring/gates 较好；`AnalyzeSymbolSetup` 0%，backup fallback、超时和资源释放未测 |
| `internal/storage` | 5 | 73.8% | 正常 JSONL append 有测；权限、磁盘失败、并发写、坏行恢复未测 |
| `internal/types` | 4 | 100.0% | round-trip/方法覆盖完整 |
| `internal/utils` | 13 | 85.0% | 基础 helper 覆盖较好 |
| `tests` | 3 | 无 statements | equivalence/default contract smoke tests |

## 4. 高风险未测分支与流程断点

### H1. Universe refresh 存在并发 map 风险，且没有真正刷新 WS universe

证据：

- `internal/engine/pipeline.go:649`、`:750` 的 CoinGlass poller 读取 `p.symbolsByExchange`。
- `internal/engine/pipeline.go:1282` 的 `refreshLoop` 写入同一 map，没有 mutex 或 snapshot。
- `refreshLoop` 只更新 `symbolsByExchange[name]` 和 MarketPulse universe，没有更新 `p.exchangeStates[name].symbols`，也没有重启 `SubscribeTickers`。

影响：

- 定时刷新与 poller 重叠时可能触发 race；现有 race tests 因 refresh interval=999h 或预先 cancel，无法覆盖。
- 日志显示 universe changed，但 WS 和 futures metrics 仍使用启动时 symbol 集合，形成“配置/日志已刷新、实际数据流未刷新”的流程断点。

缺失测试：短刷新周期下并发 poll、刷新后新增/删除 symbol 是否进入/退出 WS 与 metrics、race detector 验证。

### H2. Shutdown/cancellation 不能约束多个 I/O 路径

证据：

- `internal/exchange/binance.go:67`、`bybit.go:65`、`okx.go:89` 使用 `time.Sleep(backoff)`；最长 30 秒，不 select `ctx.Done()`。
- `internal/engine/pipeline.go:649`、`:750` 按 symbol 串行调用 CoinGlass，每次超时 10 秒。
- `internal/data/coinglass_py.go:35` 与 `coinglass.go:254` 从 `context.Background()`/无 context request 派生，pipeline cancel 不能中断当前请求。
- `internal/notify/telegram.go:46` 使用 `context.Background()`；`dingtalk.go:87` 使用无 context request。
- `cmd/kairosd/main.go:83` 只给 shutdown 等待 15 秒。

影响：

- Top 30 的 CoinGlass 串行轮询理论上可阻塞数分钟；shutdown 可能超时并强制结束。
- Telegram 卡住会阻塞 delivery goroutine；DingTalk 虽有 10 秒 client timeout，但同样不可被 pipeline cancel。
- 同步 `sendToChannels` 串行 fan-out，一条通道变慢会拖住另一条，并使上游 delivery channel 堆积、detector 最终 drop event。

缺失测试：in-flight HTTP cancel、退避期间 cancel、慢/挂起 notifier、channel saturation、shutdown deadline。

### H3. `kairosd` 不处理 pipeline 提前退出

`cmd/kairosd/main.go:73-79` 启动 pipeline 后只阻塞在 `<-ctx.Done()`；`errCh` 仅在收到 OS signal 后读取。

若 `Pipeline.Start` 因非取消错误提前返回，daemon 仍等待 signal，进程存活但监控已停止。现有 `cmd/kairosd` 测试只测 chat ID；`TestPipeline_Start_MockExchange` 也不覆盖 main 的 signal/error select。

需要测试：pipeline 立即返回 error 时 main lifecycle 必须退出非零或触发 shutdown，而不是假活。

### H4. Alert dedup/cooldown 测试名与实际覆盖不一致

`TestDeliverEvent_BlacklistAndDedup` 创建的 pipeline 没有 Telegram/DingTalk；`deliverEvent` 在 `hasDeliveryChannel()==false` 立即返回，因此 blacklist、policy、dedup、cooldown 均未执行。

同时 `internal/engine/pipeline.go:1060-1080`：

- dedup 检查读取 `p.dedupLast[dedupKey]`；
- cooldown 紧接着再次读取完全相同的 `dedupKey`；
- 默认 1800 秒 cooldown 因而完全覆盖 5 秒 dedup window。

这可能是预期的 per-event cooldown，也可能是 key 设计错误；当前测试不能证明任何一方。发送失败前已写入 cooldown state，同样无测试。

### H5. Market replay 验收素材没有被完整执行

`internal/detector/testdata/` 有 5 个 fixtures：

- `broad_rally.jsonl`
- `broad_selloff.jsonl`
- `btc_only_pump.jsonl`
- `quiet.jsonl`
- `smallcap_only_pump.jsonl`

但 `market_pulse_replay_test.go` 只执行 `broad_rally.jsonl`。其余四个 fixture 当前只是未被引用的数据文件。Goal 中的 data outage、universe refresh、trend→decay、reversal fixtures 也不存在。

此外 CLI `cmd/kairos-market-replay` 为 0% 覆盖，不能保证 CLI 与 test helper 的 replay 语义一致。

### H6. Scanner backup fallback 的资源释放路径未测

`internal/scanner/scanner.go:85` 的 defer 在首次 exchange 创建后就绑定原 exchange。fallback 成功时，原 exchange 被立即关闭并再次由 defer 关闭；成功的 backup exchange 没有对应 defer close。

现有 scanner integration 只测 primary happy path；backup 成功/失败、BTC context reload、每个 exchange 的 Close 次数均无断言。

## 5. Race / deadlock / channel / cancellation 风险清单

| 风险 | 位置 | 当前测试状态 | 优先级 |
| --- | --- | --- | --- |
| `symbolsByExchange` 无锁并发读写 | `engine/pipeline.go` pollers + refresh | 未触发 refresh race | 高 |
| refresh 不更新 WS/metrics universe | `engine/pipeline.go:1282` | 只测预取消返回 | 高 |
| WS backoff sleep 不可取消 | 三个 exchange `SubscribeTickers` | 只测进入函数前已取消 | 高 |
| CoinGlass per-symbol 串行且不可被 ctx 中断 | engine/data | 只测 no-symbol 分支 | 高 |
| daemon 忽略 pipeline early error | `cmd/kairosd/main.go` | 无 lifecycle test | 高 |
| notifier 同步、Telegram 无 caller context | notify/engine | Telegram send 0% | 高 |
| event/ticker channel 满时静默 drop 或仅日志 | exchange/detector | MarketPulse 有 nonblock test；端到端丢失无测 | 中 |
| storage 写入位于 aggregator 同步路径 | `eventAggregator` → `mpStore.Record` | 只测正常文件写 | 中 |
| `p.cancel`、`SetDingTalk` 生命周期字段无同步 | `engine.Pipeline` | 仅按单线程约定使用；无并发生命周期测试 | 中 |
| scanner semaphore acquisition 不看 ctx | scanner deep analysis | 依赖 worker 最终返回；超时/挂起 adapter 无测 | 中 |
| detector runtime Reset/UpdateConfig 并发安全 | detector | 多个方法 0% 或仅顺序调用 | 中 |

积极项：

- detector event channel 多数采用 non-blocking send，不会直接堵住 ticker 入口。
- `mergeChannels` 的 forwarding send 同时监听 `ctx.Done()`，并由 WaitGroup 关闭输出 channel。
- 本次 fresh `-race` 和 5 次 shuffle/race 均通过，但现有 tests 没有制造上述定时刷新/慢 I/O 场景。

## 6. 测试质量问题

### 6.1 Shallow/no-op tests

- `TestDeliverEvent_BlacklistAndDedup`：在 delivery gate 前返回，核心断言缺失。
- `TestRefreshLoop_Cancel`：context 在进入函数前已取消，只覆盖一个 return。
- `TestTelegramDeliverer_Cancel`：关闭 channel 且预取消，未处理事件。
- `TestResonanceDeliverer_Cancel`：scorer 被设为 nil，只覆盖 nil guard。
- `TestConsumeTickersAndPollMetrics`：只断言 `pollMetrics` 无 error，不断言 detector state/event。
- `TestPipeline_Start_MockExchange`：只断言未返回意外 error，不验证事件流、goroutine 退出数、channel close 或 detector 结果。

### 6.2 缺少错误注入

关键接口虽然有 test hook（如 `exchangeNew`、scanner factory/loader），但很少注入：

- HTTP 500、invalid JSON、partial body/read error；
- notifier timeout/429/5xx；
- storage permission/disk write error；
- exchange WS malformed frame/reconnect；
- context 在请求进行中取消；
- channel 满、消费者慢、event drop 计数。

### 6.3 Live tests 不属于 CI gate

以下 3 个测试默认 skip：

- `TestFetchSpotRSIMap_LiveGo`：`KAIROS_LIVE=1`
- `TestFetchSpotRSIMap_LivePython`：`KAIROS_LIVE=1` 且 decrypt repo 可用
- `TestFetchMarketCapMap_Live`：`KAIROS_LIVE_COINGLASS=1`

没有真实 exchange WS、Telegram、DingTalk smoke test。live probe 适合 schedule/manual workflow，不宜阻塞普通 PR。

## 7. Makefile / tooling / CI 审查

### 7.1 已有门禁

`make check`：

```text
build → go build ./...
vet   → go vet ./...
lint  → golangci-lint v2（errcheck, govet, ineffassign, staticcheck, unused）
test  → go test -race ./...
```

CI 在 push/PR 到 `main` 时安装固定 `golangci-lint@v2.1.6` 并执行 `make check`。

### 7.2 门禁缺口

1. **coverage 不在 CI**：`cover-check` 已定义但 `check` 不依赖它，CI 也不单独调用。
2. **聚合覆盖率掩盖关键低覆盖包**：data 56.3%、exchange 62.9%、engine 64.4%、notify 66.7%。
3. **格式化不在门禁**：golangci 配置未启用 gofmt/goimports；当前已有 22 个 `gofmt -l` 命中。
4. **依赖完整性/漏洞不在 CI**：未运行 `go mod verify`、`go mod tidy -diff`、`govulncheck`。
5. **release 不依赖 CI check**：tag/workflow_dispatch 可直接执行 cross-build/release，没有先跑 unit/static gates。
6. **Dockerfile 不在 CI build**：镜像可构建性、scratch runtime/config 挂载未验证。
7. **无测试超时和 anti-flake run**：普通 gate 没有 `-count=1`、`-shuffle` 或关键并发包重复跑。
8. **Actions 只固定 major tag**：`checkout@v4`、`setup-go@v5`、`softprops/action-gh-release@v2` 未固定 commit SHA；属于供应链加固项。
9. **release matrix 只 build 不 test**：Linux CI 能覆盖业务测试，但 Windows/Darwin/ARM 仅到发布时才验证编译。

### 7.3 当前 gofmt 命中

共 22 个，分布在 `internal/alert`、`backtest`、`config`、`data`、`detector`、`exchange`、`indicators`、`scanner`、`storage`、`utils`。完整列表可由：

```bash
gofmt -l cmd internal tests
```

重现。

## 8. 推荐的完整 unit/static 验证命令

### 8.1 每个 PR 必须执行

```bash
# 依赖与格式
set -euo pipefail
go mod verify
go mod tidy -diff
test -z "$(gofmt -l cmd internal tests)"

# 编译、静态检查、race tests
make check

# 覆盖率门禁（应纳入 CI；当前 Makefile 已有）
make cover-check

# 漏洞可达性
govulncheck ./...
```

注意：当前执行 `test -z "$(gofmt -l ...)"` 会失败；需先单独处理既有 22 个格式化文件，避免和功能变更混在一个 PR。

### 8.2 并发/架构变更额外执行

```bash
go test -race -count=1 -timeout=5m ./...
go test -race -shuffle=on -count=5 -timeout=5m \
  ./internal/engine ./internal/exchange ./internal/detector ./internal/scanner
```

### 8.3 覆盖率诊断

```bash
go test -count=1 -cover ./...
packages=$(go list ./internal/... | rg -v '/types$' | tr '\n' ' ')
go test -count=1 $packages -coverprofile=coverage.out -covermode=atomic
go tool cover -func=coverage.out
```

建议除 aggregate 70% 外，再为 `engine`、`exchange`、`data`、`notify` 建立 package floor，先从不低于当前基线开始，避免被高覆盖 helper package 抵消。

### 8.4 非阻塞 scheduled/manual probes

```bash
KAIROS_LIVE=1 go test -count=1 -v ./internal/data \
  -run 'TestFetchSpotRSIMap_Live(Go|Python)'
KAIROS_LIVE_COINGLASS=1 go test -count=1 -v ./internal/data \
  -run TestFetchMarketCapMap_Live

docker build -t kairos:ci .
```

应使用专用测试凭据/环境，不能在普通 fork PR 暴露 secrets。

## 9. 建议修复顺序（测试优先）

1. 为 refresh + poll 并发写最小 race regression test，并定义 refresh 后 WS/metrics universe 的真实合同。
2. 为 `kairosd` 加 pipeline early-error lifecycle test。
3. 将 CoinGlass/通知发送改为 caller context 可取消前，先补 in-flight cancel 与 15 秒 shutdown tests。
4. 用可注入 deliverer 测实 blacklist/policy/dedup/cooldown/send-failure 状态；澄清 dedup key 与 cooldown key。
5. 参数化执行全部 Market Pulse fixtures，并给 replay CLI 的 parser/sort/invalid input 加测试。
6. 补 scanner backup close-count/timeout tests。
7. CI 加 gofmt、cover-check、go mod verify/tidy diff、govulncheck；release 依赖 check。

## 10. 证据文件

均位于当前 task 的 `research/`：

- `make-check.log`
- `race-fresh.log`
- `concurrency-shuffle.log`
- `package-coverage.log`
- `internal-coverage.out`
- `function-coverage.txt`
- `go-mod-verify.log`
- `data-test-verbose.log`
- `govulncheck.log`
- `govulncheck-verbose.log`
- `compile-tests.log`
