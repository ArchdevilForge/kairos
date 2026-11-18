# Kairos 行情源与 E2E 审计

**目标**　在 Binance 受限时切换到 OKX，覆盖桌面和后台告警，并以可重复的桌面旅程核查正确性、响应式和性能。

**架构**　Go daemon 维持 Desk API 地址，替换 pipeline 和 Desk API 实例。SQLite 缓存以交易所为键。React 设置复用现有入口。

**技术栈**　Go 1.26.11、SQLite、Electron、React、Vite、Chromium。约束与边界见 [行情源规格](../spec/market-source.md)。

| 步骤 | 入口 | 验收 |
|---|---|---|
| 1. 上游基线 | `go test ./...`、`pnpm --dir desktop run build`、Desk 页面 | 记录原始 SHA、构建及浏览器输出。 |
| 2. 缓存和提供方 | `internal/cache/store.go`、`internal/exchange/okx.go`、`internal/deskapi/{market,ws}.go` | 同一交易对/时间戳的两源价格互不覆盖，历史向前翻页无重复，REST 与 WebSocket 同源。 |
| 3. 后台切换与设置 | `cmd/kairosd/main.go`、`internal/deskapi/server.go`、`desktop/src/components/SettingsModal.tsx` | 保存时重建检测 pipeline，连接原地址，刷新与重启保留来源，YAML 选项恢复原配置。 |
| 4. 浏览器验收 | Watch、Review、Stats、设置、命令面板 | 覆盖宽度 390 / 768 / 1120 / 1440 / 2560、键盘、网络延迟和失败，记录 Console、布局、请求时延与构建体积。 |
| 5. 深入复查 | 缓存迁移、旧源请求、复盘边界、通知与账户 | 以故障注入核对新旧实例时序并记录难以实测的场景。 |

执行详细矩阵及证据见同目录的 [审计报告](2026-11-18-kairos-audit.md)。验证采用临时脚本和一次性命令。

## 响应式和性能复验

| 步骤 | 入口 | 验收 |
|---|---|---|
| 1. 建立对照 | 保留优化前生产构建，对固定 OKX 替身运行冷缓存、150 ms、187500 B/s、CPU 4× | 至少 3 次，记录首图中位数、首批 K 线数量、重复请求和 LCP。 |
| 2. 优化图表启动 | `desktop/src/services/deskClient.ts`、`desktop/src/views/WatchWorkspace.tsx`、`internal/deskapi/market.go` | 首图用 300 根且可往前翻页，首批与 bootstrap 重叠，同源校验，首次连接不重复回补，断线重连可回补。 |
| 3. 验证布局和输入 | `desktop/src/components/CommandPalette.tsx`、`desktop/src/components/TopStatusBar.tsx`、`desktop/electron/main.mjs` | 短视口可滚动找到结果，Tab 不进入背景，打开 ESC 后还焦，窄窗口和中英文主要操作可用。 |
| 4. 验证故障和交付 | 实际 Electron、Go race suite、源切换 E2E、临时一条命令执行的浏览器矩阵 | 缓存故障有可见提示，覆盖窄窗和双图，验收通过后推送并向上游 main 发 PR。 |
