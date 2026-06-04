# kairos

> 古希腊语：καιρός — 关键时刻，恰当时机

Crypto futures trading signal harness for Hermes Agent. Based on Bit浪浪's trading philosophy.

> Copy and send to your Hermes Agent for automatic installation, configuration, and launch:

```
Please install and configure the kairos crypto trading signal harness. The server needs git, uv, and Hermes with Telegram and webhook platforms configured. Steps: 1) git clone https://github.com/Xeron2000/kairos ~/kairos && cd ~/kairos && uv sync  2) mkdir -p ~/.hermes/skills/kairos && cp -r ~/kairos/skills/kairos-* ~/.hermes/skills/kairos/  3) Edit ~/.hermes/config.yaml, set skills.external_dirs to [~/.hermes/skills/kairos]  4) echo 'Y' | hermes webhook subscribe --name kairos-signals --url /webhooks/kairos-signals --prompt "You are a trading signal filter. When receiving SignalEvents from kairos: 1. Reply '静默' for meaningless/duplicate signals 2. Call kairos MCP tools for deep analysis on valuable signals 3. Push confirmed high-quality signals to Telegram. See kairos-harness skill for detailed rules." --delivery telegram (save the secret)  5) echo 'Y' | hermes mcp add kairos --command ~/kairos/run.sh --env KAIROS_WEBHOOK_SECRET=<secret>  6) systemctl --user restart hermes-gateway  7) Tell me if successful.
```

## Architecture

```
Kairos (MCP Server)                  Hermes Agent
┌──────────────────────┐     POST    ┌──────────────┐
│  WebSocket feeds     │ ──────────> │  LLM filter   │
│  Anomaly detectors   │  SignalEvent│  MCP tool calls│
│  Technical analysis  │ <────────── │  Telegram push │
└──────────────────────┘   tools     └──────────────┘
```

Kairos watches markets via WebSocket 24/7, detects anomalies (price velocity, volume spikes), and pushes SignalEvent to Hermes via webhook. Hermes filters noise, calls MCP tools for deep analysis, and pushes high-quality signals to Telegram.

## MCP Tools

| Tool | Description |
|------|-------------|
| `get_market_cycle` | 春夏秋冬 market phase + BTC trend + altcoin season |
| `detect_box_pattern` | Box pattern detection with convergence scoring |
| `scan_symbols` | Symbol scanning with formula-based ranking |
| `detect_signal` | Trading signal (breakout/pullback/reversal) |
| `get_market_sentiment` | 恐惧贪婪指数 + market sentiment overview |
| `check_pyramiding` | Pyramiding condition analysis |
| `check_exit_signals` | Exit signal detection (6 types) |
| `blacklist_symbol` | Ban a noisy coin (Hermes-controlled blacklist) |
| `unblacklist_symbol` | Unban a coin |
| `list_blacklist` | Show all banned coins with reasons |

## Skills

| Skill | Description |
|-------|-------------|
| `kairos-harness` | **How Hermes uses kairos** — signal filtering, tool calling, decision flow |
| `kairos-cycle` | 春夏秋冬周期判断 |
| `kairos-scanner` | 量化选币 |
| `kairos-box` | 箱体识别 |
| `kairos-signal` | 交易信号 |
| `kairos-market-sentiment` | 市场氛围 |
| `kairos-pyramiding` | 加仓分析 |
| `kairos-exit-signals` | 出场信号 |
| `kairos-selection-formula` | 选币完美公式 |
| `kairos-divergence` | 分歧理论 |
| `kairos-new-coin` | 新币交易 |
| `kairos-scanner-orchestrator` | 全自动扫描编排 |

## Quick Start

```bash
git clone https://github.com/Xeron2000/kairos ~/kairos
cd ~/kairos && uv sync
export KAIROS_WEBHOOK_SECRET="<from hermes webhook subscribe>"
uv run kairos-mcp
```

MCP server starts, connects to exchanges, begins scanning.

## Philosophy

顺势而为 · 敬畏市场 · 严格止损

## License

MIT
