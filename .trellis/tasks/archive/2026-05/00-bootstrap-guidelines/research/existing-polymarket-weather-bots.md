# Research: Existing Polymarket Weather Bots

- **Query**: Open-source projects for Polymarket weather trading bots (GitHub repos, Python/TypeScript, polymarketweather.com)
- **Scope**: External
- **Date**: 2026-05-04

## Findings

### 1. alteregoeth-ai/weatherbot (Python - Most Comprehensive Open-Source)

**Repository:** https://github.com/alteregoeth-ai/weatherbot
**Language:** Python
**Last push:** 2026-03-03
**Stars:** Not listed in search results but appears actively maintained

**Features:**
- **20 cities** across 4 continents (US, Europe, Asia, South America, Oceania)
- **3 forecast sources** -- ECMWF (global), HRRR/GFS (US, hourly), METAR (real-time observations)
- **Expected Value** -- Skips trades where math doesn't work
- **Kelly Criterion** -- Sizes positions based on edge strength
- **Stop-loss + trailing stop** -- 20% stop, moves to breakeven at +20%
- **Slippage filter** -- Skips markets with spread > $0.03
- **Self-calibration** -- Learns forecast accuracy per city over time
- **Full data storage** -- Every forecast snapshot, trade, and resolution saved to JSON
- Simulation (paper-trading) mode and live mode

**Architecture:**
1. Fetches forecasts from ECMWF and HRRR via Open-Meteo (free, no key)
2. Gets real-time observations from METAR airport stations
3. Finds matching temperature bucket on Polymarket
4. Calculates Expected Value -- only enters if positive
5. Sizes using fractional Kelly Criterion
6. Monitors stops every 10 minutes, full scan every hour
7. Auto-resolves by querying Polymarket API directly

**API dependencies:**
| API | Auth | Purpose |
|---|---|---|
| Open-Meteo | None | ECMWF + HRRR forecasts |
| Aviation Weather (METAR) | None | Real-time station observations |
| Polymarket Gamma | None | Market data |
| Visual Crossing | Free key | Historical temps for resolution |

### 2. MoonsatProtocol/Polymarket-Weather-Bot (TypeScript - Kelly + NWS)

**Repository:** https://github.com/MoonsatProtocol/Polymarket-Weather-Bot
**Language:** TypeScript (49.3%), HTML (42.9%), JavaScript (7.8%)
**Stars:** 108 | **Forks:** 61
**Created:** 2026-03-18 | **Last push:** 2026-03-29
**Primary contributor:** seabra98

**Features:**
- Kelly Criterion-driven sizing
- NWS forecast scanning
- Simulation mode with `simulation.json` output
- Web dashboard for tracking (`sim_dashboard_repost.html`)
- Configurable locations, thresholds, and intervals
- Paper mode (signals only) and live mode

**Configuration:**
```env
POLYMARKET_PRIVATE_KEY=0x...64 hex...
POLYMARKET_PROXY_WALLET_ADDRESS=0x...40 hex...
ENTRY_THRESHOLD=0.15
EXIT_THRESHOLD=0.45
MAX_TRADES_PER_RUN=5
MIN_HOURS_TO_RESOLUTION=2
LOCATIONS="nyc,chicago,miami,dallas,seattle,atlanta"
```

**Usage:**
```bash
# Paper mode
node dist/index.js

# Live mode with $1,000 balance
node dist/index.js --live

# Run every 30 minutes
node dist/index.js --live --interval 30

# Show open positions and PnL
node dist/index.js --positions
```

**Note:** Has a paid version with enhanced Kelly sizing, risk controls, extended market coverage, and deeper analytics.

### 3. solship/Polymarket-Weather-Trading-Bot (TypeScript - Similar to MoonsatProtocol)

**Repository:** https://github.com/solship/polymarket-trading-bot
**Language:** TypeScript (49.3%), HTML (42.9%), JavaScript (7.8%)
**Stars:** 70 | **Forks:** 41
**Created:** 2025-05-27 | **Last push:** 2026-03-19
**Primary contributor:** solship

**Features:**
- Nearly identical feature set to MoonsatProtocol bot
- Kelly-driven sizing
- NWS forecast data
- Simulation mode
- Configurable thresholds
- Also has a paid version

**Note:** This appears to be the original repo; MoonsatProtocol may be a fork/copy with additional features.

### 4. suislanchez/polymarket-kalshi-weather-bot (Multi-Platform)

**Repository:** https://github.com/suislanchez/polymarket-kalshi-weather-bot
**Language:** TypeScript + Python backend
**Stars:** Not specified (highest reported profits: $1.8k)

**Features:**
- **Multi-platform**: Trades both Kalshi (KXHIGH series) AND Polymarket simultaneously
- **31-member GFS ensemble** from Open-Meteo for probabilistic temperature predictions
- **BTC 5-minute microstructure analysis** as additional signal
- **React dashboard** for monitoring
- **Ensemble counting**: 28/31 members above 70F = 90% probability
- Trades when edge > 8%
- RSA-PSS auth for Kalshi; Gamma API for Polymarket

**API endpoints (internal):**
| Route | Method | Description |
|---|---|---|
| `/api/weather/ensemble` | GET | Ensemble forecasts for all cities |
| `/api/weather/markets` | GET | Weather markets (Kalshi + Polymarket) |
| `/api/weather/signals` | GET | Weather trading signals (both platforms) |

**Kalshi series tickers:** KXHIGHNY, KXHIGHCHI, KXHIGHMIA, KXHIGHLAX, KXHIGHDEN

### 5. santox422/weather-edge (Microservice Architecture)

**Repository:** https://github.com/santox422/weather-edge
**Language:** TypeScript + Python 3.12 (FastAPI microservices)
**Stars:** 3

**Architecture (distributed microservices):**

| Service | Port | Purpose | Stack |
|---|---|---|---|
| service-polymarket | 8001 | Market data gateway | Python 3.12 / FastAPI |
| service-forecast | 8004 | Forecast ingestion | Python 3.12 / FastAPI |
| service-live-temp | 8003 | Real-time station temperature | Python 3.12 / FastAPI |
| service-probability | 8005 | 5-stage probability pipeline | Python 3.12 / FastAPI |
| service-strategy-engine | 8006 | Orchestrator + trading strategy | Python 3.12 / FastAPI / scipy |
| weatherbot-test | 8007 | Standalone simulation bot | Python 3.10 |
| service-paper-trade | 8008 | Automated paper trading | Python 3.11 / FastAPI / asyncpg |
| weather-edge | 3001->3000 | Next.js dashboard | Node.js / Next.js 15 / React 19 |

**Features:**
- **5-stage physics-based probability pipeline** with 11 atmospheric correction factors
- **28 global cities** coverage
- Edge scoring: `edge = P_FINAL - market_price`
- Multi-source confirmation required
- Kelly criterion + Black-Litterman portfolio optimization
- Monte Carlo copula-based correlation simulation
- Liquidity gating (orderbook depth checks before execution)
- Caddy reverse proxy + TLS

### 6. whisdev/openclaw-simmer-polyther-trading-ai-agent (Python - Simmer API)

**Repository:** https://github.com/whisdev/openclaw-simmer-polyther-trading-ai-agent
**Language:** Python
**Last push:** 2026-03-06

**Features:**
- Typed clients for Simmer (forecast ingestion) and Polymarket (market discovery + trading) via `httpx`
- Config management via `pydantic-settings` with `.env` support
- **Bonding + tail strategy stack** (curve-scaled directional bets + tail hedges)
- Signal pipeline converting forecast deltas into trade recommendations
- Risk guardrails (position sizing caps, min edge, hedge unwinds)
- SQLite journaling + CLI history for run auditing
- Monte Carlo backtester
- CLI (`weather-trader ...`) with subcommands

**Directory structure:**
```
src/weather_trader/
  clients/simmer.py        # Forecast ingestion
  clients/polymarket.py    # Market discovery, quoting, trading
  signals/temperature.py   # Signal generation based on forecast deltas
  config.py                # Settings + secrets wrapper
  strategies/              # Bonding + tail strategy implementations
  strategy.py              # Orchestrates all strategies per market
  runner.py                # Async orchestrator / cron entry point
  cli.py                   # Typer-based CLI interface
```

### 7. YoungseokOh/polymarket-tmax-lab (Python - Research-First)

**Repository:** https://github.com/YoungseokOh/polymarket-tmax-lab
**Language:** Python
**Created:** 2026-03-17

**Philosophy:** Research-first, trading-aware. Focuses exclusively on "Highest temperature in [city] on [date]" markets.

**Features:**
- Discovers active Polymarket max-temperature markets
- Parses rule text into structured `MarketSpec`
- Retrieves weather forecasts from Open-Meteo
- Backfills bronze/silver market, forecast, and truth tables into **DuckDB + Parquet**
- Reconstructs historical forecast slices without lookahead
- Trains probabilistic postprocessing models
- Maps predictive distributions to Polymarket outcomes
- Computes edge versus market prices
- Rolling-origin research backtests
- Paper trading with public data
- Live trading implemented but **disabled by default**

**Data sources:**
- Open-Meteo (forecast, historical forecast, previous-runs, ensemble endpoints)
- Wunderground station pages (scraped)
- Hong Kong open data
- DuckDB + Parquet for storage

### 8. weatherbot.fi (Node.js - Commercial/SaaS)

**Website:** https://weatherbot.fi/
**Language:** Node.js / Express
**Status:** Commercial product (not fully open-source)

**Features:**
- Connects to Polymarket CLOB via **WebSocket** for sub-second price streaming
- **4-model ensemble**: GFS, ECMWF, UKMO, NWS
- Bayesian edge detection with Normal CDF probability
- Kelly criterion position sizing with quarter-Kelly safety caps
- **Claude AI** as final decision maker for trade analysis
- Copy trading signals from top 10 Polymarket weather traders
- **67+ global cities** support
- 5-layer automated exit system (profit target, edge convergence, trailing stop, stop loss, time decay)
- NOAA NCEI 10-year historical base rate integration
- Client-side wallet security (private keys never leave device)

### 9. MusicBoiyzzz/Polymarket-Weather-Bot (TypeScript - Desktop Tool)

**Repository:** https://github.com/MusicBoiyzzz/Polymarket-Weather-Bot
**Language:** TypeScript
**Stars:** 0 | **Forks:** 0
**Created:** 2023-03-04 | **Last push:** 2026-04-05

**Features:**
- Windows desktop-focused
- Weather market tracking and trade automation
- Simple monitoring tool rather than sophisticated trading engine
- Contributors: seabra98, MusicBoiyzzz

### polymarketweather.com Blog Content

**URL:** https://polymarketweather.com/blog
**Published articles (as of 2026-05-04):**

| Title | Category | Date | Key Takeaways |
|---|---|---|---|
| How to Trade Polymarket Weather Markets | Guide | 2026-04-17 | Full setup guide from wallet to edge calculation |
| Polymarket Weather Predictions: How the Odds Are Set | Research | 2026-04-20 | Model-update cadence drives price movement; ECMWF lag is key edge |
| Polymarket Weather Markets Explained | Technical | 2026-04-15 | Market structure, NWP models, ensemble forecasts, resolution mechanics |
| Polymarket Weather Bot: How Automated Trading Works | Guide | 2026-04-25 | Bot architecture, edge calculation, Kelly sizing, open-source options |
| Polymarket Weather Leaderboard | Research | 2026-04-22 | Top trader analysis; gopfan2 = 10K+ positions systematic tail strategy |
| ColdMath | - | - | Advanced strategy content |
| Resolution: How Your Bet Gets Settled | Technical | 2026-04-29 | UMA oracle, liveness window, Wunderground data, edge cases |
| Kelly Criterion for Weather Trading | Strategy | 2026-05-01 | Fractional Kelly sizing, hard caps, why full Kelly is dangerous |
| How to Find and Keep an Edge | Strategy | 2026-04-28 | Hypothesis generation, historical testing, edge magnitude, decay monitoring |

**Key strategy insights from polymarketweather.com:**
1. **Forecast latency arbitrage** is the #1 documented edge -- ECMWF 12 UTC run moves prices before other traders react
2. **Resolution station mismatch** -- consumer apps report city-center temps; markets resolve against airport stations (3-8F difference possible)
3. **Minimum edge threshold**: 8% for weather markets (5% too low given calibration error)
4. **Position sizing**: 0.15-0.25x fractional Kelly with hard caps ($50-$100 per position)
5. **Ensemble underdispersion**: Apply 1.15x spread multiplier to raw ensemble counts
6. **Volume game**: Edge only shows up reliably at hundreds/thousands of trials
7. **Model run cadence**: ECMWF updates 00/12 UTC; GFS updates 00/06/12/18 UTC; HRRR updates hourly
8. **Best window**: 24-48 hours out; beyond 72h requires significant skill
9. **Top trader (gopfan2)**: 10,000+ positions, systematic tail/mispricing strategy, $500K+ profit
10. **Liquidity limit**: ~$500K-$2M annual profit ceiling before liquidity constraints bind

**Minimal bot architecture from polymarketweather.com:**
1. Forecast: Open-Meteo GFS ensemble (free, no API key, hourly updates) -- query by airport ICAO coordinates
2. Probability: Count GFS ensemble members by bucket, apply 1.15x spread multiplier for underdispersion
3. Market discovery: Gamma API, filter for `tag=weather`, extract condition IDs and token IDs
4. Prices: CLOB API `GET /prices` or WebSocket subscription
5. Edge threshold: 8% minimum
6. Sizing: 0.15x Kelly, hard cap at $50
7. Execution: CLOB API `POST /order` with limit order inside current spread

### Comparison of Open-Source Bots

| Bot | Language | Forecast Source | Strategy | Cities | Paper Trading | Live Trading |
|---|---|---|---|---|---|---|
| alteregoeth/weatherbot | Python | ECMWF+HRRR+METAR (Open-Meteo) | EV + Kelly | 20 | Yes | Yes |
| MoonsatProtocol | TypeScript | NWS | Kelly + NWS | 6 US | Yes | Yes |
| solship | TypeScript | NWS | Kelly + NWS | 6 US | Yes | Yes |
| suislanchez | TypeScript/Python | GFS ensemble (Open-Meteo) | Ensemble counting + Kelly | 5 US | Yes | Yes (Kalshi+Polymarket) |
| weather-edge | Python/TypeScript | Multi-model | 5-stage pipeline + Black-Litterman | 28 | Yes | Partial |
| openclaw/simmer | Python | Simmer API | Bonding + tail | Unknown | Yes | Planned |
| polymarket-tmax-lab | Python | Open-Meteo | Postprocessing models + DuckDB | Unknown | Yes | Disabled |
| weatherbot.fi | Node.js | GFS+ECMWF+UKMO+NWS | Bayesian + Claude AI | 67+ | No | Yes (commercial) |

### Related Specs

- None yet (new project)

## Caveats / Not Found

- Several repos (MoonsatProtocol, solship, MusicBoiyzzz) appear to be forks or copies of the same original codebase with minor modifications
- The "paid version" mentioned in MoonsatProtocol and solship is not open-source and details are not available
- weatherbot.fi is commercial/SaaS, not fully open-source; the open-source portion's scope is unclear
- Star counts and contributor counts may be inflated by bot activity or cross-promotion
- The most technically sophisticated open-source bot appears to be alteregoeth-ai/weatherbot (multi-source forecasts, self-calibration, stop management) or santox422/weather-edge (microservice architecture, portfolio optimization)
- polymarketweather.com appears to be a content/SEO site with genuinely useful strategy content, but is not itself a trading tool
