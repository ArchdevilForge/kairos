# kairos — Personal Trading Research & Decision OS

> καιρός — 关键时刻，恰当时机

**Kairos = 你的个人交易研究、市场感知、主观决策、策略实验、执行辅助、复盘与 Alpha 验证的统一仓库。**

它不是"一堆交易 bot"，而是一个**可执行知识库**：每个交易观点都有路径变成可验证的规则
（`Source → Hypothesis → Experiment → Finding → Canonical`）。

## 六步主干

```text
Observe → Interpret → Select → Decide → Execute → Learn
```

| 步骤 | 组件 | 位置 |
|---|---|---|
| Observe | MarketPulse / 检测器 / kairos-oiscan / floors(meme·pm·solana·onchain·launch) / data-sources | `internal/detector` `cmd/kairos-oiscan` `floors/` |
| Interpret | CycleMap（Context 1d+4h / Setup 1h+15m / Trigger 5m） | `internal/cycle` |
| Select | Directional Ranker | `internal/ranker` |
| Decide | Playbook → Decision Ticket → BehaviorGate（教义 9b 行为红线机械执法） | `internal/playbook` `internal/opportunity` `internal/decision` |
| Execute | Human decision（经 kairos-desk 记录 + gate 判定）+ manual execution（trader 仅执行辅助） | `cmd/kairos-desk` `tools/trader` |
| Learn | Journal → Counterfactual → EV Attribution → Gate 合规报告 | `internal/storage` `internal/evaluation` `cmd/kairos-eval` |

> 加任何东西先问：它属于哪一步？回答不出来就先别加。

## Monorepo 布局

```text
cmd/ internal/ config/     Go 主引擎(检测器/MarketPulse/CycleMap/ranker/playbook/ticket/journal)
bus/                       Python 事件总线(JSONL → 门控 → Telegram → 聚合输出)
floors/meme/               rh-sniper      Robinhood 链 meme sniper
floors/pm/                 pm-bot         Polymarket 天气策略 + 纸面交易
floors/solana/             smartalpha     pump.fun smart-money 分析
floors/onchain/            meme-monitor   Dexscreener 多链新币发现
floors/launch/             launch-collector  Robinhood Chain CCA launchpad 数据管道(H-005/006/007)
data-sources/aicoin/       AiCoin 行情/资金流 wrapper
coinglass/                 CoinGlass 解密参考实现(Go 原生在 internal/data/coinglass.go)
tools/chain-trace/         零 key 链上取证
tools/research/            JSONL → DuckDB 研究层(H-005 等假设判定报告)
tools/trader/              币安现货挂单脚本(执行层, 与 kairos 人控边界无关)
schemas/                   事件契约(所有 floor 统一语言)
docs/                      权威链 + 知识目录, 入口 docs/INDEX.md
```

## 架构

```text
Exchange WebSocket  ──→  单币检测器（价格/成交量/OI/资金费率）
                    ──→  MarketPulse（市场广度/状态机，primary only）
                             ↓
                     events + 60s snapshots JSONL  ──→  kairos-calibrate（lift_5m）
                             ↓
CoinGlass API  ──→  多空比/爆仓（可选）  ──→  Telegram / DingTalk
kairos-oiscan ──→  全市场 OI 异动(发现+确认)  ──→  bus/inbound/futures/*.jsonl
floors/*(pm/meme/solana/onchain/launch) ──→  bus/inbound/<floor>/*.jsonl
```

## 文档与权威

权威链唯一，冲突裁决见 `docs/00-system/AUTHORITY.md`；完整地图见 `docs/INDEX.md`。
90 天主 KPI：告警后 5m 延续 vs **non-alert directional baseline** 的 `experimental_lift_5m`（小样本仅 exploratory）。

## Build & Commands

### Download prebuilt binaries

Push a version tag (`v*`) or run the **Release** workflow on GitHub Actions — assets are published to [GitHub Releases](https://github.com/ArchdevilForge/kairos/releases).

```bash
# 1. Open https://github.com/ArchdevilForge/kairos/releases/latest
# 2. Download kairos-<version>-linux-amd64.tar.gz (Windows: .zip)
tar xzf kairos-*-linux-amd64.tar.gz
cd kairos-*-linux-amd64
export TELEGRAM_BOT_TOKEN=...
export TELEGRAM_CHAT_ID=...
# Optional DingTalk mirror (custom robot webhook):
# export DINGTALK_WEBHOOK_URL=https://oapi.dingtalk.com/robot/send?access_token=...
# export DINGTALK_SECRET=SECxxx   # 加签
# set dingTalk.enabled: true in config
./kairosd --config config/config.yaml.example
```

Each archive contains `kairosd`, `kairos-alert`, `kairos-backtest`, and `config/config.yaml.example`.

To cut a release from your machine:

```bash
git tag v0.1.0
git push origin v0.1.0
```

### Build from source

```bash
make check   # build + vet + golangci-lint + test -race

# Realtime watcher
TELEGRAM_BOT_TOKEN=xxx TELEGRAM_CHAT_ID=xxx go run ./cmd/kairosd --config config/config.yaml

# Optional post-alert scanner summary (not a default cron product)
go run ./cmd/kairos-alert --config config/config.yaml --dry-run

# MarketPulse calibration: events + outcomes + 60s snapshots → lift_5m
go run ./cmd/kairos-calibrate --config config/config.yaml

# Backtest
go run ./cmd/kairos-backtest --symbol BTC/USDT --start 2024-01-01 --end 2024-06-01

# MarketPulse fixture replay (shadow algorithm, no Telegram)
go run ./cmd/kairos-market-replay --input internal/detector/testdata/broad_rally.jsonl
```

## Project layout

```text
cmd/           CLI 入口（kairosd、kairos-calibrate、kairos-alert、kairos-backtest、kairos-market-replay）
internal/      业务实现（detector、scanner、engine、exchange…）
tests/         跨包等价性测试
config/        运行时配置示例
docs/          架构与策略文档
deploy/        部署相关
```

## Alerts

| 维度 | 来源 | 检测方式 |
|---|---|---|
| `price_velocity` | WebSocket | 多窗口绝对涨跌幅阈值 |
| `volume_spike` | WebSocket | 按秒归一化成交速率 vs 滚动基线倍数 |
| `open_interest_change` | REST | 相邻轮询变化率阈值 |
| `funding_rate_anomaly` | REST | 绝对值阈值 + 变化幅度 |
| `long_short_ratio` | CoinGlass | 绝对阈值 + Z-score + 变化速度 |
| `liquidation` | CoinGlass | 金额阈值 + Z-score + 多空主导判定 |
| `resonance` | 聚合 | 信号质量分 ≥55（基于 Z-score 极端度 + 维度共振 + 方向一致性） |
| `market_impulse` / `market_trend` / `market_stress` | 横截面 | 广度 + 中位收益 + BTC/ETH 确认 + 状态机（默认 shadow） |

**默认注意力路径**是 MarketPulse 市场事件，不是 scanner。`kairos-alert` 仍可输出 `watch`/`prepare`/`trade_candidate` 与结构位，但定位为 MarketPulse 叫醒后的**可选人工解释**，请勿当作自动入场指令，也不建议默认 cron 推送。

## Configuration

```bash
export TELEGRAM_BOT_TOKEN="..."
export TELEGRAM_CHAT_ID="..."
```

可选:
```bash
export KAIROS_ALERT_MIN_STATE=prepare
export KAIROS_ALERT_LIMIT=5
```

## Philosophy

顺势而为 · 敬畏市场 · 严格止损 · 人工决策

## License

MIT
