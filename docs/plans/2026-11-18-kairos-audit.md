# Kairos 桌面行情、功能与体验审计

基线为上游 `0983dc4031def6bdb69013b01eea1575a31df261`，桌面版本 `0.4.1`。测试使用生产前端构建、实际 Go Desk API 与行情适配器、独立 SQLite、Chromium 和 Linux Electron。交易所和通知服务由隔离网络中的固定行情和本地接收器提供。公开网络验证仅访问 Binance 和 OKX 的公开行情 API。

## 结论

交易终端的基本约束是同一根 K 线必须带有交易所和可见时间。桌面连接、行情新鲜度和通知检测状态应分别可观测。测试用同一交易对的不同价格验证来源隔离，用分钟中点验证复盘因果性，用 REST 与 WebSocket 同时失败验证故障提示。

| 优先级 | 问题与证据 | 处理 |
|---|---|---|
| P1 | 上游 bootstrap 和 K 线校验调用 Binance 交易对列表，图表 WebSocket 在无适配器时再回到 Binance。两条行情链同时返回 `451` 时，上游市场接口仍为 `200`，图表只显示 `No candlestick data`。 | 设置支持 Binance / OKX / 跟随 YAML。应用同时重启桌面 API 与实时告警 pipeline，无缓存时明确返回 `502 MARKET_DATA_UNAVAILABLE`。[实现](../../internal/deskapi/market.go#L16) |
| P1 | 旧 SQLite 主键只有 symbol / timeframe / time，REST 与 WebSocket 对同一时刻的记录可以互相覆盖。旧复盘查询用 1 分钟开盘时刻做上界，在该 K 线收盘前暴露高点、低点和收盘价。 | SQLite 按源索引并保留旧表。复盘等待 1 分钟收盘，切回时过滤旧响应。自动复算按票据来源过滤，币安成交仅自动匹配币安票据与旧未标注票据。[缓存](../../internal/cache/store.go#L92) · [复盘](../../internal/deskapi/market.go#L127) · [票据](../../internal/opportunity/outcomes.go#L37) · [对账](../../internal/exchange/binance_account.go#L1342) |
| P2 | 上游设置在键盘打开后，初始焦点和第一次 Tab 留在背景。390 × 620 时保存按钮必须滚动才能触达。390 px 的 Review 测出标尺、水平线右边界分别为 398 px、426 px。 | 原生对话框限制焦点并在关闭后恢复焦点，设置页底部操作可见，图表工具组在窄屏换行。[设置](../../desktop/src/components/SettingsModal.tsx#L70) · [工具栏](../../desktop/src/chart/KairosChart.tsx#L2038) |
| P2 | 已有行情缓存时，上游异常仍可能被缓存掩盖。桌面 WebSocket 的连接成功只能证明本机 daemon 可达。Review 图中也可以显示来自其他交易所的历史成交或人工标注。 | 状态栏显示当前数据源，Watch 在上游回补失败时明确标出缓存行情。完整成交来源和行情新鲜度监控保留为后续检查。[缓存读取](../../internal/deskapi/market.go#L406) · [状态栏](../../desktop/src/components/TopStatusBar.tsx#L44) |
| P2 | 源码确认 Electron 最小窗体是 `1120 × 700`，浏览器可用的窄屏布局无法直接覆盖原生窄窗口。命令面板声明 `aria-modal`，源码未见限制 Tab 或恢复焦点的逻辑。 | 最小窗口下调为 640 × 480，并在 Linux Electron 原生窗口实测。命令面板获得短视口滚动区、焦点隔离与关闭还焦。平台缩放和读屏留在后续矩阵。[窗口限制](../../desktop/electron/main.mjs#L381) · [命令面板](../../desktop/src/components/CommandPalette.tsx#L309) |

## E2E 执行矩阵

| 场景 | 动作与判据 | 执行 |
|---|---|---|
| 区域限制与切换 | Binance REST/WS 返回 `451`。Settings 保存 OKX，读取 bootstrap、交易对、最新 800 根 K 线与 330 根上一页，校验升序、独占游标和价格归属。收到带 OKX 来源的 K 线及本地 DingTalk 通知。切回 YAML 和重启 daemon 核对持久化。 | Chromium / 本地接收器 |
| 故障与兼容 | OKX 单交易对的 1m REST 注入 `429`，校验正常返回明确错误。非法来源返回 `400`，旧源请求返回 `409`。旧 SQLite 来源行导入后仅在同源可读。新旧行情进同一 symbol 路径，比较价格而非只比较回应码。 | API / SQLite |
| 主要桌面旅程 | Watch 分屏、命令面板搜索 OKX 独有交易对、Review 隐藏未来并回退、Stats 空态、设置保存、重开和 Escape。核对画布、请求及 JS 异常。 | Chromium |
| 响应式与键盘 | `390 / 768 / 1120 / 1440 / 2560` 宽度逐页量按钮位置和文档宽度。另测 `390 × 620` 设置窗口、18 次 Tab、打开与关闭后焦点、窄屏保存按钮命中。 | 15 个视口与页面组合 |
| 真实包装与性能 | Linux Electron 使用正式的 preload、`file://` 渲染和自动启动 sidecar。Chromium 在禁用缓存、CPU 4×、模拟延迟 150 ms、下行 187500 B/s 的条件下测可见图表、LCP、CLS 和长任务。 | Linux / Chromium |

## 补充回归覆盖

| 条件 | 判据 |
|---|---|
| 历史行情缺口、重复包、硬断网及慢重连 | 记录切换期 503、业务恢复时长、缓存陈旧提示和图表回补。校验 1m 至 1d 的时间边界。 |
| 真实票据、成交和大数据量 | 分别走空、单笔、1000+ 记录，完成复盘定位、播放、回退、两窗切换与统计核对。复核没有用其他交易所报价解释历史成交。 |
| 多平台和无障碍 | 验证 macOS、Windows、Linux 的小分辨率和 125% / 200% 缩放，覆盖中英文、亮暗主题、读屏和仅键盘。 |
| 性能长跑 | 双图连续更新 30 分钟，记录内存峰值、长任务、每次切 TF 的首图与 API 请求数。用 P50 / P95 比较冷、热缓存，定位懒加载、重复回补和帧渲染。 |
| 本机接口边界 | 用临时凭据验证已授权与未授权，独立评估 query token、Electron 导航和 preload 的组合。现有源码允许在 query string 提交 token，生产安全影响需结合部署和日志策略确认。[后端认证](../../internal/deskapi/server.go#L199) · [Electron 窗口](../../desktop/electron/main.mjs#L377) |

## 实测结果

| 验收项 | 结果 |
|---|---|
| 区域限制 | 上游 `200` 空结果。新版本 `502 MARKET_DATA_UNAVAILABLE`。选 OKX 后恢复 800 根最新 K 线，历史页 330 根，同一边界无重复。 |
| 后台切换 | 模拟市场每轮向本地 DingTalk 接收器发送 5 条 OKX 异动。启动通知延迟 5000 ms 时，进程启动至 health 可用为 118 ms 与 128 ms。两轮从保存到设置关闭为 146 ms 与 130 ms。重启后仍为 OKX。 |
| 缓存与复盘 | 同一 symbol 的两源价格独立。旧 Binance 行和 UTC 对齐的 OKX 日线可读，旧 UTC+8 日线保存在旧表。游标在分钟中点时，上游返回未收盘分钟。新结果最后一根 1m 在游标前 30 秒已经收盘。 |
| 布局与原生 | 15 个视口 × 页面组合均未测出按钮越界。Linux Electron 35.7.5，Chromium 134.0.6998.205，中文亮色原生窗口 1480 × 920 走通 sidecar、保存和键盘焦点回归。Chromium 157.0.8103.38 英文暗色复查 390 × 620 设置和 Review。 |
| 性能 | 150 ms、187500 B/s、CPU 4×、Chrome 冷缓存下，唯一记录的受控样本首图为 **5147 ms**，LCP **3584 ms**，CLS **0.00085**，发生 **6** 次长任务，最长 **115 ms**。响应式用例使用本地行情替身，指标用于检视加载顺序和主线程成本。 |

优化前首图要等待 bootstrap，随后还有首次 WebSocket 连接触发的额外回补。下方复验记录了首批请求与 bootstrap 并行后的结果。[首次加载](../../desktop/src/App.tsx#L392) · [回补](../../desktop/src/views/WatchWorkspace.tsx#L829)

## 响应式和性能复验

| 场景 | 判据与实测结果 |
|---|---|
| 触屏与布局 | Chrome 使用触屏模拟，检查 Watch、Review、Stats 在 320 × 480、360 × 520、390 × 620、640 × 480、768 × 620、1120 × 700、1440 × 900、2560 × 900 的 24 个组合。没有工具按钮越过水平视口，窄屏导航与保存操作达到 44 px 高。 |
| 原生窗体 | Electron 35.7.5 的 X11 窗体报告最小 640 × 480。实际调整到这个大小，中文亮色下完成 Watch、Review、Stats 及保存。 |
| 键盘 | 短窗口用方向键滚动命令结果，25 次 Tab 未进入背景，Escape 关闭面板并把焦点还给触发器。设置关闭时保留后台图表的绘图模式。 |
| 分页与故障 | 从 Watch 用鼠标拖图触发真实历史请求并取回 800 根唯一 K 线。主动断开并重连 WebSocket 后出现回补请求。注入 OKX REST 限流，旧 K 线可读且出现缓存提示，恢复后提示消失。 |
| 首屏与流量 | 同一固定 OKX 替身，150 ms、187500 B/s、CPU 4×，关闭浏览器缓存。每版独立上下文运行 3 次。首图中位数由 **5161 ms → 4580 ms**，下降 **11.3%**。三次原始数据分别为 5164 / 5139 / 5161 ms 和 4705 / 4580 / 4579 ms。外壳 LCP 中位数为 3564 ms → 3596 ms。首屏 K 线从重复 2 次 × 800 根变为 1 次 × 300 根，首次 K 线响应中位数由 87422 B 降至 36599 B。 |

优化后，首批 K 线在 bootstrap 结束前发出，响应经来源核对后用于 Watch。历史页和真正重连的回补仍各可请求 800 根。应用外壳 LCP 单独统计，避免将图表、外壳与长任务混为一个指标。

## 证据与边界

- 公开 REST 探测中，Binance 永续返回 `HTTP 451`，OKX 永续 `BTC-USDT-SWAP` 的 15m 返回 `HTTP 200` 与有效 K 线。OKX 历史接口实测单页 300 根。用户自己的美国出口、生产 WebSocket 稳定性与真实通知账号未验。[OKX API 文档](https://www.okx.com/docs-v5/en/#rest-api-market-data-get-candlesticks)
- `go vet ./...`、`go test -race -p 4 ./...`、`pnpm --dir desktop run build`、`pnpm --dir desktop run verify:tokens`、`git diff --check` 和规格检查均退出 0。一次性验证也确认了自动复算与对账不会选用其他交易所的票据。测试脚本和数据均为临时材料。
- Vite 生产首屏 JS 与 CSS gzip 由上游 **177.84 kB** 增至 **179.36 kB**，已包含 charts chunk。设置按需块由 **3.17 kB → 3.61 kB**。按 Vite 体积输出计算，浏览器传输受服务器压缩策略影响。
- 显式来源覆盖作用于带 `--desk-listen` 的 daemon。币安只读对账及独立配置的 CoinGlass 仍有各自的数据权限与服务状态。跟随 YAML 的 Bybit 可读取历史 K 线，当前缺少 Bybit 实时 K 线适配器。告警检测器重启后重新预热。交易票据继续使用既有 Journal，未标注来源的旧票据按 YAML primary 归属。
- 复盘稀疏分钟数据、长时间行情回补和真实对账仍需在有真实样本的数据集验证。局部防御性代码检查覆盖 Desk API 和 Electron 入口。完整渗透与平台范围超出此次体验审计。
