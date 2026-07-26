# Implementation Plan：Kairos 全仓一致性与流程闭环

## 0. Safety Gate

- [ ] 记录 `git status --short`，明确现有 `.pi/`、`.trellis/` 改动不属于本任务。
- [ ] 只修改 `cmd/`、`internal/`、`config/`、`docs/`、README、Makefile/CI 及本任务文件。
- [ ] 先运行目标包 fresh tests 建立基线。

## 1. Config / Types Contract

- [ ] 为 config normalize/validate、secret serialization、legacy exchange alias 写失败优先测试。
- [ ] 统一 exchange authority，增加启动期 validation。
- [ ] 删除确认无消费者的配置字段/default/example，保留旧 YAML unknown-key 宽容。
- [ ] 扩展 `AnomalyEvent` provenance/event ID，明确 timestamp 单位；补 serialization tests。
- [ ] 对齐 MarketSnapshot tags、Setup enums、CycleToDict 字段和 OHLCV timestamp 单位。
- [ ] 删除未使用的大而全 Detector interface/no-op 承诺，但保留实际共享 helper。

Validation:

```bash
go test -count=1 ./internal/config ./internal/types ./internal/scanner ./internal/backtest
```

Rollback point: config/type changes必须在进入 pipeline 修改前全绿。

## 2. Exchange / Data Contract

- [ ] 将 `FetchOHLCV` 统一为 before/end cursor，修正 Binance/Bybit query 参数并保留 OKX 行为。
- [ ] 增加三 adapter 的区间分页回归测试及 backtest runner test。
- [ ] 保留 ticker 有效零值，optional metrics 不再把缺失转换为 0。
- [ ] 将 WS reconnect sleep 改为 context-aware。
- [ ] 将 CoinGlass request/Python subprocess 贯穿 caller context。
- [ ] 将 volume spike delta 按 elapsed seconds 归一化并补推送频率不变性测试。

Validation:

```bash
go test -race -count=1 ./internal/exchange ./internal/data ./internal/detector ./internal/backtest
```

Rollback point: adapter query 与 cursor tests 必须覆盖三家交易所。

## 3. Runtime Lifecycle / Universe Refresh

- [ ] 为 Pipeline 增加受锁 universe snapshot/update helper。
- [ ] 让 WS subscription loop 可在 universe 更新时取消并重新订阅。
- [ ] metrics、CoinGlass、MarketPulse、market-cap reference 每轮读取一致 snapshot。
- [ ] 增加 refresh 与 poll 并发 race test，以及新增/删除 symbol 的重订阅测试。
- [ ] `kairosd` 同时等待 signal 与 Pipeline.Start 结果，修正 stop/wait/close 顺序。
- [ ] 补 pipeline early-error、backoff cancel、in-flight request cancel、scanner fallback close 测试。

Validation:

```bash
go test -race -count=1 ./cmd/kairosd ./internal/engine ./internal/exchange ./internal/scanner
```

Rollback point: early exit 与 refresh tests 必须先在旧实现失败、修复后通过。

## 4. Alert / Policy / Notify / Outcome

- [ ] DingTalk sign 只编码一次并按服务端 query decode 值测试。
- [ ] 增加共享 MarketPulse payload view；修复 down count/list/window/文案。
- [ ] 真实 detector payload 贯穿 Telegram/DingTalk 测试。
- [ ] resonance 复用普通 policy/delivery；过滤 MARKET/outcome 输入；修复 dimensions concrete type。
- [ ] shadow 不触发 quiet gate；移除 isolated-extreme no-op 配置。
- [ ] 分离 dedup/cooldown 状态，市场事件绕过通用 symbol cooldown；成功发送后才提交状态。
- [ ] `buildAlert` 转换 timestamp、exchange、event ID；HintStore 记录真实 provenance。
- [ ] outcome 只在 fresh snapshot 采样；300s trend 使用独立有效样本 gate。
- [ ] notifier API 贯穿 context；`kairos-alert` 尊重 `telegram.enabled`。
- [ ] 所有通知文案统一包含 `仅供人工判断，不自动交易。`

Validation:

```bash
go test -race -count=1 ./internal/engine ./internal/notify ./internal/detector ./internal/storage ./internal/alert ./cmd/kairos-alert
```

## 5. Replay / Docs / Test Quality

- [ ] 参数化执行全部现有 MarketPulse replay fixtures；补 parser/invalid/empty input tests。
- [ ] 修复 no-op/shallow pipeline tests，使事件真实经过 policy/delivery gate。
- [ ] 更新 `docs/architecture.md`、README、config example 和相关 backend spec。
- [ ] 对 `cmd internal tests` 执行 gofmt；只提交格式变化属于本任务范围的 Go 文件，若全仓 fmt gate要求清理既有文件则单独列入提交批次。
- [ ] 检查 Makefile/CI：最小加入可重复 fmt/mod/coverage 门禁；`govulncheck` 若 CI 工具安装成本不合理则保留为显式本地/计划命令，不新增自定义脚本。

## 6. Full Verification

按顺序执行且全部 fresh：

```bash
set -euo pipefail
go mod verify
go mod tidy -diff
test -z "$(gofmt -l cmd internal tests)"
go test -race -count=1 -timeout=5m ./...
go test -race -shuffle=on -count=5 -timeout=5m \
  ./internal/engine ./internal/exchange ./internal/detector ./internal/scanner
make check
make cover-check
govulncheck ./...
```

同时核对：

- [ ] 没有新增 reachable vulnerability。
- [ ] 研究中的每个 P0/P1 finding 都映射到测试/修复或明确 non-defect 结论。
- [ ] 不相关 dirty files 未被覆盖、格式化或 staged。
- [ ] 最终 `git diff --check` 通过。

## 7. Review Gates

- [ ] `trellis-implement` 完成实现和目标测试。
- [ ] `trellis-check` 做全仓 spec/contract/cross-layer review 并直接修复发现。
- [ ] 再跑第 6 节全量验证。
- [ ] 判断是否需要更新 `.trellis/spec/backend/`。
- [ ] 提交前单独列出本任务文件与既有未识别 dirty files，等待用户确认 commit plan。

## Explicit Non-Deliverables

- 不实现 outbox/retry queue。
- 不持久化跨重启 MarketPulse/dedup/pending outcome 状态。
- 不实现 storage rotation、snapshot database 或 chart renderer。
- 不部署 CCS。
