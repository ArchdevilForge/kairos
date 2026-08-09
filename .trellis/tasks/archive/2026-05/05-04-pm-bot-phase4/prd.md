# Phase 4: New Strategies + Backtesting Framework

## Goal

Add 6 novel strategies with positive EV (based on research findings) and build a backtesting framework that simulates each strategy with $100 default bankroll.

## Requirements

### New Strategies (6)

1. **MetarObservation** — Same-day METAR observation edge (Priority #2, 3-10% edge, Low complexity)
   - Poll METAR for resolution station every 30min during same-day window
   - Compare observed trajectory to forecast trajectory
   - Adjust probability when observations diverge from forecast
   - Only active when resolution is within 12 hours

2. **ResolutionDivergence** — WU vs NWS resolution source divergence (Priority #1, 2-5% edge, High complexity)
   - Build parallel model for WU-likely and NWS-likely resolution
   - When the two models predict different buckets, trade toward WU model
   - Track DST offset, 6-hour max capture, and Celsius round-trip errors
   - Most bots don't distinguish resolution sources → structural edge

3. **NegRiskSumDeviation** — Exploit bucket sum deviation from $1.00 (Priority #3, 2-8% edge, Medium complexity)
   - Continuously monitor sum of YES prices across all buckets
   - Sum < 0.98: buy YES on all buckets (risk-free arb)
   - Sum > 1.03: buy NO on 2-3 most overpriced buckets via NegRisk
   - Size with quarter-Kelly

4. **CrossMarketCorrelation** — High-Low temperature correlation (Priority #6, 3-8% edge, Low-Medium complexity)
   - Model joint distribution of (high, low) for each city
   - When edge found on high market, calculate conditional shift for low market
   - Trade low market if price hasn't adjusted for high's predicted extreme

5. **StationChangeDetector** — Resolution station change detection (Priority #7, 5-15% edge, Low complexity)
   - Re-read market description every cycle to extract resolution station ICAO
   - Compare to stored station mapping; alert on change
   - After station change, the new station may have different microclimate
   - Rare but huge edge when it occurs

6. **PrecipTempCorrelation** — Precipitation-temperature correlation (Priority #11, 2-6% edge, Low complexity)
   - Check if precipitation markets exist for same city
   - If high-confidence rain forecast, suppress high-temperature probabilities
   - If clear sky, elevate high-temperature probabilities
   - Novel cross-market signal, no known bot uses it

### Backtesting Framework

1. **Data Pipeline**
   - Open-Meteo Historical Forecast API for historical ensemble data
   - Open-Meteo Archive API for actual observed temperatures
   - Polymarket `/prices-history` endpoint for historical market prices
   - SQLite storage for cached historical data

2. **Backtest Engine**
   - Day-by-day simulation loop: load markets → load forecasts → run strategy → resolve → P&L
   - Realistic cost modeling: taker fee 5%, spread 1-3%, slippage 0.5-1%
   - Kelly position sizing simulation (quarter-Kelly by default)
   - Per-strategy P&L tracking with $100 default starting bankroll
   - Walk-forward validation (train on rolling 60-day window)

3. **Performance Metrics**
   - Financial: total P&L, Sharpe, Sortino, max drawdown, win rate, avg win/loss
   - Forecast: Brier Score, CRPS, calibration (reliability diagram data)
   - Execution: fill rate, fee impact, position utilization

4. **CLI Interface**
   - `pm-bot backtest --strategy <name> --bankroll 100 --days 90`
   - `pm-bot backtest --all --bankroll 100` (run all strategies)
   - `pm-bot backtest --compare` (side-by-side comparison table)
   - Rich table output + optional CSV export (`--csv output.csv`)

## Acceptance Criteria

- [ ] 6 new strategy classes in `pm_bot/strategies/`, all pass ruff + mypy
- [ ] `pm-bot backtest --strategy metar_obs --bankroll 100` runs successfully
- [ ] `pm-bot backtest --all --bankroll 100` runs all strategies and shows comparison
- [ ] `pm-bot backtest --compare` shows side-by-side table with P&L, Sharpe, win rate
- [ ] `pm-bot backtest --csv out.csv` exports results to CSV
- [ ] Backtest uses Open-Meteo historical forecast + archive APIs
- [ ] Realistic cost model: taker fee 5%, spread 1-3%, slippage 0.5-1%
- [ ] Kelly position sizing (quarter-Kelly) in backtest
- [ ] All strategies registered in `get_all_strategies()`
- [ ] `pm-bot scan --strategy metar_obs` shows recommendations from new strategies
- [ ] ruff + mypy zero errors

## Definition of Done

- All strategies have working `run()` methods that produce Recommendations
- Backtest engine produces consistent, reproducible results
- Cost model matches real trading costs (taker 5%, maker 0%)
- All 11 strategies (5 existing + 6 new) backtest-able
- ruff + mypy clean

## Technical Approach

### Strategy Registration

Each strategy extends `Strategy` base class with `name` and `run()`. Register in `get_all_strategies()`.

### Backtest Architecture

```
pm_bot/
├── backtest/
│   ├── __init__.py
│   ├── engine.py          # BacktestEngine: day-by-day simulation
│   ├── data.py            # Historical data fetching + caching (SQLite)
│   ├── costs.py           # Cost model (fees, spread, slippage)
│   ├── metrics.py         # Performance metric calculation
│   └── report.py          # Rich table + CSV output
├── cli/
│   └── backtest_cmd.py    # CLI integration
├── strategies/
│   ├── metar_observation.py
│   ├── resolution_divergence.py
│   ├── neg_risk_sum.py
│   ├── cross_market_corr.py
│   ├── station_change.py
│   └── precip_temp.py
```

### Backtest Flow

1. Load historical data (forecasts + observations + market prices) for date range
2. For each day in range:
   a. Construct WeatherEvent from historical market prices
   b. Construct ForecastResult from historical forecast data
   c. Run strategy.run() to get Recommendations
   d. Simulate execution with cost model
   e. Resolve against actual observed temperature
   f. Calculate P&L for each recommendation
3. Aggregate metrics across all days
4. Output Rich table / CSV

### Data Sources

- **Historical forecasts**: `https://historical-forecast-api.open-meteo.com/v1/forecast`
  - GFS from 2021, HRRR from 2018
  - Ensemble members available for recent data
- **Historical observations**: `https://archive-api.open-meteo.com/v1/archive`
  - ERA5 reanalysis, 2017+
- **Market prices**: Polymarket CLOB `/prices-history` endpoint
  - 1min to daily resolution

## Out of Scope

- Maker-side market making strategy (requires real-time WebSocket + inventory management, too complex for this phase)
- EMOS/QRF model training (requires historical data pipeline + sklearn/xgboost, separate phase)
- Weekend effect validation (needs empirical data we don't have yet)
- Model update window timing (requires real-time model subscription, not backtestable)
- New market opening inefficiency (requires monitoring Gamma API for creation events)
- Oracle attack monitoring (defensive, not a primary alpha strategy)

## Research References

- [`research/novel-strategies.md`](research/novel-strategies.md) — 13 novel strategies ranked by priority
- [`research/backtesting-frameworks.md`](research/backtesting-frameworks.md) — Framework comparison + data sources

## Decision (ADR-lite)

**Context**: Need to choose which strategies to implement and what backtesting approach to use
**Decision**: Implement top 6 strategies by feasibility (not just priority), use custom lightweight backtest engine
**Consequences**: Custom engine fits PM-specific structures (neg_risk, daily resolution) better than Backtrader/VectorBT; top 6 strategies cover resolution-source edge, observation edge, structural edge, correlation edge, and defensive edge
