# Research: Backtesting Frameworks for Prediction Market Trading Bots

- **Query**: Backtesting frameworks and approaches for Polymarket weather market trading bots
- **Scope**: Mixed (internal codebase + external frameworks + data sources)
- **Date**: 2026-05-04

---

## 1. Data Sources for Backtesting

### 1.1 Polymarket Historical Market Data

**CLOB API `/prices-history` endpoint** (documented, public, no auth):

| Parameter | Type | Description |
|-----------|------|-------------|
| `market` | string (required) | Token ID (asset ID) to query |
| `startTs` | number | Unix timestamp start |
| `endTs` | number | Unix timestamp end |
| `interval` | enum | `1h`, `6h`, `1d`, `1w`, `1m`, `max`, `all` |
| `fidelity` | integer | Accuracy in minutes (default 1 min) |

Response: `{ "history": [{ "t": uint32, "p": float }, ...] }`

**Batch endpoint**: `POST /batch-prices-history` — up to 20 token IDs in one request.

**Available resolution**: From 1-minute fidelity up to daily/weekly intervals. Intervals `1h`/`6h`/`1d` cover last hour to last day; `1m` covers last month; `max`/`all` covers all available data.

**Historical depth**: The API returns data for markets that have existed — no explicit documented limit on how far back data goes, but realistically data exists from when each market was created (weather markets have been running since ~early 2025).

**Orderbook snapshots**: `GET /book?token_id=TOKEN_ID` returns current orderbook. Historical orderbook data is NOT available through the standard API. **MarketLens** (third-party, paid) offers tick-level L2 orderbook snapshots and deltas for backtesting.

**Gamma API for closed markets**: `GET /events?closed=true` and `GET /markets?closed=true` return resolved market data including `outcomePrices` at resolution time. This is critical for knowing which bucket won.

**Key limitation**: The `/prices-history` endpoint provides *price* history but NOT *orderbook depth* history. For realistic execution simulation you need orderbook depth, which requires either:
- MarketLens API (paid, tick-level L2 data)
- Dome API via emulo-backtest (paid, historical books from Oct 14, 2025)
- Store your own snapshots during live operation going forward

### 1.2 Historical Weather Forecast Data

**Open-Meteo Historical Forecast API** (`historical-forecast-api.open-meteo.com/v1/forecast`):

| Model | Resolution | Available Since | Update Freq |
|-------|-----------|-----------------|-------------|
| GFS | 0.11° (~13 km) | 2021-03-23 | Every 6h |
| HRRR | 3 km (US only) | 2018-01-01 | Every hour |
| ICON | 0.1° (~11 km) | 2022-11-24 | Every 6h |
| ICON-EU | 0.0625° (~7 km) | 2022-11-24 | Every 3h |
| ECMWF IFS | 0.25° (~25 km) | available | Every 6h |

- **API is identical to the Forecast API** — same parameters, just different host
- Supports `start_date`/`end_date` for absolute ranges
- This is the **exact same data** that the live bot would have seen at that time — perfect for avoiding lookahead bias

**Open-Meteo Ensemble API** (`ensemble-api.open-meteo.com/v1/ensemble`):

| Model | Members | Forecast Length | Resolution |
|-------|---------|----------------|-----------|
| GFS Ensemble 0.25° | 31 | 10 days | 25 km |
| GFS Ensemble 0.5° | 31 | 35 days | 50 km |
| ECMWF IFS 0.25° | 51 | 15 days | 25 km |
| ICON-EPS | 40 | 7.5 days | 26 km |

- `past_days` parameter returns archived ensemble forecasts (up to 92 days)
- Individual member data kept for 1 month; ensemble mean/spread stored longer
- **Critical for ladder strategy backtesting**: need ensemble member distribution to compute per-bucket probabilities

**Open-Meteo Historical Weather API** (`archive-api.open-meteo.com/v1/archive`):

| Dataset | Resolution | Available | Delay |
|---------|-----------|-----------|-------|
| ECMWF IFS | 9 km | 2017+ | 0 (real-time) |
| ERA5 | 0.25° (~25 km) | 1940+ | 5 days |
| ERA5-Land | 0.1° (~11 km) | 1950+ | 5 days |

- Use for **actual observed temperatures** (ground truth for resolution)
- `daily=temperature_2m_max,temperature_2m_min` for daily highs/lows

### 1.3 Resolution Data (Ground Truth)

**Visual Crossing Timeline API** (`https://weather.visualcrossing.com/VisualCrossingWebServices/rest/services/timeline/{location}`):

- Used by alteregoeth/weatherbot for resolution verification
- Free tier: 1000 records/day (~$0.0001/record after)
- Supports querying by **specific station ID** (e.g., KLGA, KDAL)
- `include=obs` filters to historical observations only
- `maxStations=1` + `maxDistance=1000` isolates to a single airport station
- Returns `stations` field listing which stations contributed to each data point
- **This is the EXACT data source Polymarket uses for resolution** — Polymarket weather markets resolve based on Weather Underground data, which pulls from the same airport METAR stations

**NOAA ISD / METAR archives**:

- AWC (Aviation Weather Center): `https://www.aviationweather.gov/metar` — current METARs
- Historical METARs available via NOAA ISD: `https://www.ncei.noaa.gov/products/land-based-station/integrated-surface-database`
- The pm_bot already has `pm_bot/core/sources/metar.py` for live METAR fetching

**Weather Underground** (the actual resolution source):

- Polymarket weather markets explicitly reference Weather Underground station data
- Historical data available at `https://www.wunderground.com/history/daily/{station}`
- No official API; requires scraping or using Visual Crossing as proxy

### 1.4 Third-Party Data Aggregators

| Service | Data Available | Cost |
|---------|---------------|------|
| **PolymarketData** (`polymarketdata.co`) | 1-min price data, L2 metrics, orderbook snapshots | Paid |
| **MarketLens** (`marketlens`) | Tick-level L2 orderbook, full book reconstruction, surfaces | Paid (API key) |
| **Dome API** (via emulo-backtest) | Historical markets, prices, candlesticks, orderbooks (from Oct 2025) | Paid (API key) |
| **polymarketweather.com** | Live market summaries, bot comparison | Free |
| **Kaggle** | No Polymarket-specific datasets found | — |

---

## 2. Backtesting Methodology

### 2.1 Event-Driven vs Vectorized

| Approach | How | Best For | pm-bot Fit |
|----------|-----|----------|------------|
| **Event-driven** | Replay market events tick-by-tick | Complex order logic, limit orders, stop-losses | Less needed — pm-bot uses market orders |
| **Vectorized** | Apply strategy to price series | Fast parameter sweeps, simple signals | **Best fit** — strategies are signal → enter → hold → resolve |

**Recommendation for pm-bot**: **Hybrid vectorized + event-at-resolution**.

Weather markets have a unique structure:
1. Market opens (daily, ~48h before resolution)
2. Prices update as forecasts change
3. Market resolves at end of day (binary: YES or NO, payout = $1 or $0)

The strategies in pm-bot (gopfan2, sum_arb, ladder, narrow_no, airport_arb) are all **one-shot entry strategies**: evaluate signal → enter position → hold until resolution. No intraday trading, no stop-loss management (yet), no limit order book interaction. This makes vectorized backtesting appropriate.

### 2.2 Handling Finite-Lifespan Markets

Weather markets resolve daily. The backtest must:

1. **Day-by-day simulation**: For each date in backtest range:
   - Find markets for that date (from Gamma API historical data)
   - Fetch forecast data that would have been available at that time
   - Run strategies on those markets
   - Record entry prices and positions
   - Look up actual resolution (from Visual Crossing or Open-Meteo historical weather)
   - Calculate P&L per position

2. **Avoid lookahead bias**: Only use forecast data that was available at the time of the trade decision. The Open-Meteo Historical Forecast API provides exactly this.

3. **Resolution timing**: Markets typically resolve the day after the target date. Positions should be marked at entry price until resolution.

### 2.3 Simulating Order Execution

**Simplified model** (appropriate for pm-bot's current strategies):

```
Fill price = yes_price  (for YES direction)
Fill price = no_price   (for NO direction)
Slippage = hardcoded parameter (e.g., 0.5-1 cent)
Fee = 1% of notional (maker) or 2% (taker) — Polymarket fee schedule
```

**More realistic model** (if orderbook data available):

Walk the orderbook levels to estimate actual fill:
```python
def weighted_fill(levels, target_size):
    """levels: [[price, size], ...] sorted best-to-worst"""
    remaining = float(target_size)
    filled = notional = 0.0
    for price, size in levels:
        take = min(remaining, float(size))
        notional += take * float(price)
        filled += take
        remaining -= take
        if remaining <= 0:
            break
    if filled == 0:
        return None, 0.0, float(target_size)
    avg_fill = notional / filled
    unfilled = max(0.0, float(target_size) - filled)
    return avg_fill, filled, unfilled
```

Source: PolymarketData backtesting guide

### 2.4 Neg Risk Market Structure in Backtesting

Polymarket weather markets are **neg_risk events**: multiple mutually-exclusive temperature buckets where exactly one resolves YES.

**Implications for backtesting**:

1. **Sum of YES prices should ≈ $1.00**: Σ(YES_prices) ≈ 1.0. Deviations = structural edge (sum_arb strategy).
2. **Capital efficiency**: A NO position on one bucket is equivalent to YES positions on ALL other buckets. This means:
   - If you buy NO on bucket A at 0.60, you're effectively buying YES on all other buckets for 0.40
   - Your max loss is $0.60 (if bucket A wins), max gain is $0.40 (if any other bucket wins)
3. **Portfolio-level risk**: Within a single weather event, positions are correlated — at most one bucket wins. The backtest must model this correctly: if you hold NO positions on 3 buckets and the 4th bucket wins, all 3 NOs pay out.

**Implementation**: When running a backtest for a single date/city, treat the entire event as one simulation unit. Track positions across all buckets, and compute P&L based on the single winning outcome.

### 2.5 Position Sizing in Backtest

The pm_bot already has `pm_bot/core/kelly.py` with:
- `kelly_fraction(p_true, yes_price, direction, kelly_multiplier=0.25)` → fractional Kelly (default 25%)
- `kelly_size(edge, yes_price, bankroll, kelly_fraction_val=0.25, max_single=50.0)` → USD size
- `compute_kelly_for_recommendation(rec, bankroll, ...)` → full recommendation with size

**Backtest should support**:
- Kelly sizing (as currently implemented)
- Fixed-size (e.g., $5 per trade)
- Risk-parity (equal risk allocation across positions)
- Flat $ per trade (simplest baseline)

---

## 3. Existing Open-Source Backtesting Frameworks

### 3.1 General Trading Frameworks

| Framework | Type | Speed | Python | Status | PM Fit |
|-----------|------|-------|--------|--------|--------|
| **Backtrader** | Event-driven | Slow | 3.x+ | Maintenance only | Overkill for PM |
| **VectorBT** | Vectorized | Very fast (Numba) | 3.8+ | Active (v1.0) | Good for signal research |
| **Zipline** | Event-driven | Medium | 3.x | Dead (Quantopian) | No |
| **freqtrade** | Event-driven | Medium | 3.x | Active (crypto) | Not PM-specific |
| **backtesting.py** | Vectorized | Fast | 3.x+ | Active | Simple but limited |

**Assessment**: None of these frameworks natively support prediction market structures (neg_risk, daily resolution, binary payout). Adapting them requires significant custom work. A **custom lightweight framework** is more appropriate.

### 3.2 Prediction-Market-Specific Tools

| Tool | Description | PM Fit |
|------|-------------|--------|
| **emulo-backtest** (`tweidv/emulo-backtest`) | Drop-in DomeClient replacement for backtesting Polymarket/Kalshi. Maintains simulation clock, injects historical timestamps, prevents lookahead, tracks portfolio. | **Excellent reference** — similar architecture to what pm-bot needs. But requires Dome API key. |
| **MarketLens** (`pawelsibyl/marketlens-python`) | Tick-level L2 orderbook backtesting for Polymarket. Strategy hooks: `on_book`, `on_trade`, `on_fill`, `on_market_start`, `on_market_end`. Execution realism: latency, queue position, slippage, fees. | **Best execution realism** but paid API, focuses on crypto UP/DOWN markets, not weather. |
| **AutoPredict** (`howdymary/autopredict`) | Backtesting engine with slippage/fill-rate simulation. Epistemic metrics (Brier, calibration) + financial (PnL, Sharpe, MDD) + execution (slippage, fill rate, market impact). | Good reference for metric set. |
| **openclaw-weather** (`lamenting-hawthorn/openclaw-weather`) | NOAA + Open-Meteo ensemble + Kelly bot. Has `scripts/backtest.py` for NOAA CDO historical data + simulated prices + Kelly eval. `scripts/simulate_paper.py` for synthetic paper trading. | **Most similar to pm-bot** — uses same data sources, SQLite storage, Kelly sizing. |
| **Simmer Weather Trader** (`whisdev/openclaw-simmer-polyther-trading-ai-agent`) | Monte Carlo backtester for what-if analysis. CLI: `weather-trader backtest --sigma 3 --trials 5000`. Typer-based CLI, SQLite journaling. | Good reference for Monte Carlo approach. |
| **polymarket-alpha-lab** (`sueun-dev/polymarket-alpha-lab`) | 100 strategies with shared backtest engine. `python3 main.py backtest --data-dir data/historical/`. Has Kelly, risk management, Streamlit dashboard. | Overly complex reference. |

### 3.3 Weather Forecast Backtesting

**Verification metrics** (from the `scores` Python package, `properscoring`, `xskillscore`):

| Metric | What It Measures | Formula | Range |
|--------|-----------------|---------|-------|
| **Brier Score** | Mean squared error of probability forecast | BS = (1/N) Σ (f_i - o_i)² | [0, 1], lower=better |
| **Brier Skill Score** | Improvement over reference forecast | BSS = 1 - BS/BS_ref | (-∞, 1], higher=better |
| **CRPS** | Integrated Brier Score across all thresholds | CRPS = ∫ BS(F(z), H(z-x)) dz | [0, ∞), lower=better |
| **Reliability Diagram** | Calibration of probability forecasts | Plot forecast prob vs observed freq | Visual |

**Python packages for forecast verification**:
- `scores` (v2.5.0) — Brier, CRPS, reliability, isotonic regression. xarray-native.
- `properscoring` — Brier, CRPS for ensembles, threshold Brier. NumPy-native.
- `sklearn.metrics.brier_score_loss` — Simple Brier for binary classification.
- `xskillscore.brier_score` — xarray-native Brier with fair correction for ensembles.
- `SkillMetrics` — Brier Skill Score computation.

**Walk-forward validation for weather forecasts**:
1. Train window: e.g., 30 days of historical forecast accuracy
2. Test window: next 7 days
3. Roll forward by 7 days
4. Measure Brier Score / BSS on each test window
5. This is how the self-calibration in alteregoeth/weatherbot works

---

## 4. Performance Metrics

### 4.1 Financial Metrics

| Metric | Formula | Description |
|--------|---------|-------------|
| Total P&L | Σ (payout - cost) | Net profit/loss across all trades |
| Sharpe Ratio | (mean_return - r_f) / std_return | Risk-adjusted return (annualized) |
| Sortino Ratio | (mean_return - r_f) / std_neg_return | Downside risk only |
| Max Drawdown | max(peak - trough) / peak | Largest peak-to-trough decline |
| Win Rate | wins / total_trades | % of profitable trades |
| Avg Win/Loss | mean(win_pnl) / |mean(loss_pnl)| | Profit factor |
| Kelly Growth Rate | Σ log(1 + f*return) | Theoretical vs actual |
| Calmar Ratio | annualized_return / max_drawdown | Risk-adjusted over MDD |

### 4.2 Forecast Metrics

| Metric | Description | Use For |
|--------|-------------|---------|
| Brier Score | Probability forecast accuracy | Per-strategy, per-city, per-horizon |
| Brier Skill Score | Improvement over climatological baseline | Strategy comparison |
| CRPS | Probabilistic forecast quality (ensemble) | Ladder strategy evaluation |
| Reliability Diagram | Calibration visualization | Identify over/underconfidence |
| Resolution | Ability to distinguish outcomes | Per-strategy discrimination |
| Sharpness | Spread of forecast distributions | Ensemble quality |

### 4.3 Execution Metrics

| Metric | Description |
|--------|-------------|
| Fill Rate | % of attempted trades that got filled |
| Average Slippage | Actual fill - expected price (in bps) |
| Fee Impact | Total fees / gross P&L |
| Position Utilization | Avg daily capital deployed / bankroll |
| Blocked Trade Count | Trades rejected by risk limits |

---

## 5. Python Implementation Patterns

### 5.1 SQLite Storage for Backtest Data

The pm_bot already has SQLite infrastructure in `pm_bot/core/db.py` with:
- `trades` table (order_id, market_id, city, strategy, side, price, size, fill_status, pnl)
- `positions` table (condition_id, strategy, side, total_shares, avg_price, unrealized/realized PnL)
- `daily_state` table (date, bankroll_start/end, trade_count, win/loss_count, total_spent/pnl)

**Backtest should add**:
- `backtest_runs` table (run_id, strategy, start_date, end_date, bankroll, params_json, created_at)
- `backtest_trades` table (run_id, date, city, market_id, direction, entry_price, size_usd, resolution, payout, pnl, kelly_fraction)
- `backtest_daily` table (run_id, date, bankroll_start, bankroll_end, trade_count, win_count, loss_count, total_pnl, brier_score)

### 5.2 Pandas for Vectorized Analysis

**Core backtest loop pattern** (day-by-day simulation):

```python
import pandas as pd
import numpy as np

async def run_backtest(
    strategy_name: str,
    start_date: str,
    end_date: str,
    bankroll: float = 100.0,
) -> BacktestResult:
    results = []
    current_bankroll = bankroll
    
    for date in pd.date_range(start_date, end_date, freq="D"):
        # 1. Load market data for this date (from pre-fetched cache)
        events = load_events_for_date(date)
        if not events:
            continue
        
        # 2. Load forecast data that was available at this time (no lookahead!)
        forecasts = load_historical_forecasts(date)
        
        # 3. Run strategy on each event
        for event in events:
            event.forecast = forecasts.get(event.city)
            recs = strategy.run(event, **strategy_params)
            
            for rec in recs:
                # 4. Size position
                sized = compute_kelly_for_recommendation(rec, current_bankroll, ...)
                if not sized:
                    continue
                
                # 5. Simulate fill
                fill_price = sized.price + slippage
                cost = sized.size_usd
                
                # 6. Look up resolution
                actual_temp = load_actual_temp(event.city, date)
                resolved_yes = temp_in_bucket(actual_temp, sized.bucket)
                
                # 7. Compute P&L
                if sized.direction == "YES":
                    payout = sized.size_usd / fill_price * (1.0 if resolved_yes else 0.0)
                else:
                    payout = sized.size_usd / fill_price * (1.0 if not resolved_yes else 0.0)
                
                pnl = payout - cost
                current_bankroll += pnl
                
                results.append({
                    "date": date, "city": event.city,
                    "strategy": strategy_name, "direction": sized.direction,
                    "entry_price": fill_price, "size_usd": cost,
                    "resolved_yes": resolved_yes, "payout": payout, "pnl": pnl,
                    "bankroll_after": current_bankroll,
                })
    
    return BacktestResult(results=pd.DataFrame(results), final_bankroll=current_bankroll)
```

### 5.3 Walk-Forward Optimization

```python
def walk_forward_backtest(
    strategy, data, train_window=30, test_window=7, step=7
):
    """Train on window, test on next window, roll forward."""
    results = []
    dates = data["date"].unique()
    
    for i in range(0, len(dates) - train_window, step):
        train_dates = dates[i : i + train_window]
        test_dates = dates[i + train_window : i + train_window + test_window]
        
        # Calibrate strategy on training period
        train_results = backtest(strategy, train_dates)
        optimal_params = optimize_params(strategy, train_results)
        
        # Test with those params on out-of-sample period
        test_results = backtest(strategy, test_dates, params=optimal_params)
        results.append(test_results)
    
    return pd.concat(results)
```

### 5.4 Monte Carlo Simulation

```python
def monte_carlo_backtest(trade_results: pd.DataFrame, n_simulations=10000):
    """Bootstrap returns for confidence intervals."""
    returns = trade_results["pnl"].values
    n_trades = len(returns)
    
    equity_curves = []
    for _ in range(n_simulations):
        sampled = np.random.choice(returns, size=n_trades, replace=True)
        equity = np.cumsum(sampled) + initial_bankroll
        equity_curves.append(equity)
    
    curves = np.array(equity_curves)
    return {
        "mean_final": curves[:, -1].mean(),
        "p5_final": np.percentile(curves[:, -1], 5),
        "p95_final": np.percentile(curves[:, -1], 95),
        "mean_mdd": np.mean([max_drawdown(c) for c in curves]),
        "pct_profitable": (curves[:, -1] > initial_bankroll).mean(),
    }
```

---

## 6. Existing pm_bot Codebase Relevant to Backtesting

### 6.1 Strategy Interface

All strategies implement `Strategy.run(event: WeatherEvent, **kwargs) -> list[Recommendation]` from `pm_bot/strategies/base.py`. This interface is already clean and testable — a backtester can call `.run()` with historical data and get recommendations.

### 6.2 Data Models

| Model | File | Relevance |
|-------|------|-----------|
| `TemperatureBucket` | `pm_bot/models/market.py` | Price, temp range, volume |
| `WeatherEvent` | `pm_bot/models/market.py` | Event + buckets + sum_gap |
| `ForecastResult` | `pm_bot/models/market.py` | City, date, model, members (ensemble) |
| `Recommendation` | `pm_bot/models/market.py` | Strategy, edge, direction, size_usd, kelly_fraction |
| `STRATEGY_DEFAULTS` | `pm_bot/models/config.py` | Per-strategy threshold params |

### 6.3 Core Infrastructure

| Component | File | Reuse for Backtest |
|-----------|------|-------------------|
| `kelly_fraction()`, `kelly_size()`, `compute_kelly_for_recommendation()` | `pm_bot/core/kelly.py` | Direct reuse |
| `TradeDB` + SQLite schema (trades, positions, daily_state) | `pm_bot/core/db.py` | Extend with backtest tables |
| `fetch_weather_events()` | `pm_bot/core/polymarket.py` | Adapt for historical fetching |
| `bucket_probability_numpy()` | `pm_bot/core/weather.py` | Direct reuse for ladder strategy |
| `parse_bucket()` | `pm_bot/core/parser.py` | Direct reuse |
| City coordinates + aliases | `pm_bot/models/config.py` | Direct reuse |
| `CITY_ALIASES`, `DEFAULT_CITIES` | `pm_bot/models/config.py` | Direct reuse |

### 6.4 What's Missing (Needs New Code)

| Need | Description |
|------|-------------|
| Historical market data fetcher | Fetch closed weather events from Gamma API with price history |
| Historical forecast data fetcher | Query Open-Meteo Historical Forecast API for past forecasts |
| Resolution data fetcher | Get actual temperatures from Visual Crossing / Open-Meteo archive |
| Backtest engine | Day-by-day simulation loop |
| Backtest result storage | SQLite tables for backtest_runs, backtest_trades, backtest_daily |
| Performance metrics calculator | Financial + forecast + execution metrics |
| CLI command | `pm-bot backtest --strategy narrow_no --bankroll 100` |
| Comparison framework | Head-to-head strategy comparison with Rich tables |

---

## 7. Specific Implementation Approach for pm-bot

### 7.1 Recommended Architecture

```
pm_bot/
  backtest/
    __init__.py
    engine.py          # Day-by-day simulation loop
    data.py            # Historical data loaders (market + forecast + resolution)
    metrics.py         # Financial + forecast + execution metrics
    cli.py             # backtest CLI command (Typer)
    display.py         # Rich tables for backtest results
  core/
    db.py              # Add backtest tables to existing TradeDB
```

### 7.2 Data Loading Pipeline

**Phase 1: Prefetch historical data** (one-time, cached in SQLite):

```bash
pm-bot backtest prefetch --start 2025-06-01 --end 2026-04-30
```

This fetches:
1. Closed weather events from Gamma API (`closed=true`)
2. Price history from CLOB `/prices-history` for each bucket token ID
3. Historical forecasts from Open-Meteo Historical Forecast API
4. Actual temperatures from Open-Meteo archive API + Visual Crossing

**Phase 2: Run backtest** (fast, uses cached data):

```bash
pm-bot backtest run --strategy narrow_no --bankroll 100 --start 2025-06-01 --end 2026-04-30
pm-bot backtest run --strategy all --bankroll 100
pm-bot backtest compare --strategies gopfan2,ladder,narrow_no --bankroll 100
```

### 7.3 CLI Interface

```bash
# Single strategy backtest
pm-bot backtest --strategy narrow_no --bankroll 100

# All strategies, compare
pm-bot backtest --strategy all --bankroll 100

# Specific date range
pm-bot backtest --strategy ladder --start 2025-09-01 --end 2026-03-31

# Custom params
pm-bot backtest --strategy gopfan2 --bankroll 200 --edge 0.10

# Monte Carlo confidence intervals
pm-bot backtest --strategy narrow_no --monte-carlo 10000

# Export to CSV
pm-bot backtest --strategy all --output results.csv

# Prefetch data only
pm-bot backtest prefetch --start 2025-06-01 --end 2026-04-30
```

### 7.4 Key Implementation Considerations

1. **Lookahead prevention**: Only use forecasts that were available at the time of the trade decision. Open-Meteo Historical Forecast API provides this natively.

2. **Resolution data accuracy**: Must use the SAME data source Polymarket uses for resolution. Polymarket weather markets resolve on Weather Underground data, which pulls from airport METAR stations. Visual Crossing with `maxStations=1` and a specific airport station ID is the closest proxy.

3. **Airport station matching**: The pm_bot already extracts airport codes from market descriptions (`_extract_airport()` in `polymarket.py`). The backtest must verify temperatures against these specific stations, NOT city-center coordinates.

4. **Fee modeling**: Polymarket charges maker 0% / taker up to 2% (crypto tier) or 1.25% (sports/other). Weather markets are likely "other" category at 1.25% taker. With limit orders (maker), fee = 0. Default backtest should assume taker fees for conservative estimates.

5. **Bankroll tracking**: Start with configurable bankroll (default $100), track per-day bankroll evolution, enforce max_daily_loss and max_exposure limits from existing risk.py.

6. **Neg risk portfolio modeling**: Within a single event, positions are NOT independent. The backtest must compute event-level P&L based on the single winning outcome, not individual bucket P&Ls.

---

## 8. Not Found / Gaps

| Item | Status |
|------|--------|
| Free tick-level historical orderbook data for Polymarket | Not available — MarketLens/Dome are paid |
| Polymarket weather market resolution database (which bucket won) | Must reconstruct from Gamma API closed events or fetch resolution data |
| Historical ensemble member data older than 1 month | Only ensemble mean/spread stored longer; individual members expire |
| Open-source PM-specific backtesting framework that handles neg_risk | Not found — must build custom |
| Polymarket fee schedule for weather markets specifically | Not explicitly documented; assume "other" category (1.25% taker, 0% maker) |
| How far back Polymarket weather markets go | Not explicitly documented; first markets appeared ~early 2025 based on community reports |
