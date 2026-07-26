# PRD：Kairos 全仓一致性与流程闭环审查

## Goal

对 Kairos 全仓生产代码执行端到端审查，修复已确认的接口、类型、字段、配置、生命周期和数据流错配，使四个 CLI 入口的真实行为可解释、可验证，并通过完整单元测试与静态质量门禁。

## Background

当前代码可编译且现有测试通过，但仓库级追踪已发现局部测试未覆盖的跨层断裂，包括：运行中 universe 刷新未更新真实订阅且共享 map 无锁、非 OKX 回测分页语义不一致、MarketPulse 下跌消息消费错误字段、daemon 可能在 pipeline 退出后假活、事件元数据在转换中丢失、配置存在重复 authority 与公开 no-op 字段、通知/持久化的 best-effort 语义未被类型和测试准确表达。

本任务以当前仓库代码、`docs/architecture.md`、`docs/trading-system.md` 和 `.trellis/spec/backend/` 为依据，不改变“确定性告警、人工决策、不自动交易”的产品边界。

## Requirements

### R1. 全入口与全链路审查

覆盖以下入口及其共享路径：

- `cmd/kairosd`
- `cmd/kairos-alert`
- `cmd/kairos-backtest`
- `cmd/kairos-market-replay`

逐条核对 config → exchange/data → detector/scanner/backtest → aggregation/policy → notify/storage → shutdown/output 的接口和生命周期。

### R2. 接口、类型与字段一致性

- 核对 interface 与 concrete implementation 的参数语义、可选值和单位。
- 核对共享类型、序列化标签、事件名称及 producer/consumer 字段。
- 修复会产生错误结果、错误消息、panic、数据竞争、假健康或敏感信息泄漏的错配。
- `AnomalyEvent.Data` 等 map payload 必须由跨层测试证明真实 producer shape 可被 consumer 正确读取。

### R3. 配置可执行契约

- 对齐 config struct、defaults、example YAML、环境变量、validation 和运行时消费者。
- 统一 exchange authority，拒绝会造成 panic、永不触发或语义冲突的非法配置。
- 不把未实现能力继续伪装成可用配置。
- 密钥不得通过 JSON/YAML 序列化泄漏。

### R4. 运行时闭环与并发安全

- daemon 必须在 pipeline 提前失败时退出，避免 systemd 看到假健康进程。
- universe refresh 的日志、统计名单、真实 WS/metrics/CoinGlass 消费集合必须遵循一个明确且无 data race 的契约。
- shutdown/cancellation 必须覆盖关键 reconnect/backoff 和 I/O 路径，不引入无限等待。
- scanner fallback、exchange close、channel 和 goroutine 生命周期必须完整。

### R5. 告警、门控与持久化一致性

- MarketPulse up/down 的计数、字段、币种列表、窗口和文案必须方向对称且事实正确。
- resonance、单币和市场事件必须遵循明确的 alert policy、dedup/cooldown 语义。
- shadow mode 不得意外改变既有单币通知行为。
- detected、policy-passed、delivered、persisted 等状态不得在代码或文档中混为一谈。
- 保留 Telegram/DingTalk 的人工判断边界。

### R6. 最小根因修复

- 优先在共享边界修一次，不在各 caller 重复打补丁。
- 不新增 LLM、交易执行、微服务、Redis/Kafka 或无证据抽象。
- 不修改与本任务无关的现有 `.pi/`、`.trellis/` 工作区改动。

### R7. 测试与质量门禁

- 为每个修复补最小但真实的回归测试，重点覆盖跨层 producer→consumer、错误注入、并发刷新、取消和 CLI 生命周期。
- 修复浅层/no-op 测试，使测试实际经过被命名的 gate/path。
- 运行 fresh race tests、静态检查、格式检查、覆盖率门禁、模块一致性和可达漏洞检查。

## Acceptance Criteria

- [ ] 研究中所有 P0/P1 发现均有一项明确结果：已修复并有回归测试，或经证据确认非缺陷并记录理由。
- [ ] MarketPulse 下跌消息使用 `decliners`、`laggards` 和下跌方向文案；真实 detector payload 到 Telegram/DingTalk 的测试通过。
- [ ] Binance、Bybit、OKX 的 OHLCV 查询/分页契约一致，回测区间测试通过。
- [ ] universe refresh 不再并发读写共享 map，且真实订阅/metrics 行为与选择的 refresh 契约一致。
- [ ] `kairosd` 在 pipeline 提前返回时不会假活；shutdown/cancellation 回归测试通过。
- [ ] config validation 可在启动阶段拦截重复 authority 冲突、缺失核心 timeframe、非法 limits/ranges/confirmation 关系。
- [ ] 事件时间、来源和稳定标识在生产链中不再静默丢失；secret 不会被 JSON/YAML 序列化。
- [ ] resonance、shadow、policy、dedup/cooldown 的真实行为与配置和文档一致。
- [ ] scanner fallback exchange 被正确关闭，无资源生命周期断链。
- [ ] 文档只声明代码实际支持的入口、事件和算法。
- [ ] `gofmt -l cmd internal tests` 无输出。
- [ ] `go mod verify`、`go mod tidy -diff`、`make check`、`make cover-check`、`go test -race -count=1 ./...`、关键并发包 shuffle/race 和 `govulncheck ./...` 全部通过。
- [ ] 最终报告只保留按风险排序的结论、已修复项、验证证据和少量最佳建议。

## Decisions

- 本轮修复全部高置信度正确性、安全性、并发与生命周期缺陷，闭合现有公开承诺。
- 不新增 outbox/retry queue、跨重启状态恢复、存储轮转等尚未被生产证据证明必要的能力；这些只作为后续建议。
- 对公开但无运行时消费者的伪配置，从类型、defaults 和 example YAML 中移除；loader 保持对旧 YAML 未知字段的宽容，避免旧部署配置启动失败。

## Out of Scope

- 自动下单或持仓管理。
- 新增分布式基础设施。
- 部署到 CCS 或修改服务器生产配置；本任务默认只修改和验证仓库代码。
