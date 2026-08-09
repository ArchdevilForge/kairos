# PRD: Backtest Fidelity Improvements

## Problem

Two systematic biases inflate backtest returns vs live-trading reality:

1. **42% of markets use forecast-derived prices** — CLOB `/prices-history` returns empty for resolved markets, falling back to forecast probabilities that are smoother and less extreme than real market prices, systematically overstating edge.

2. **Maker limit orders assumed 100% fill** — Live mode uses maker-side pricing but assumes all orders fill at limit price. Weather market thin books mean 30-50% fill at best bid/ask, <10% at tail prices.

## Solution

### 1. Dune Analytics Price Backfill

- Add `fetch_dune_prices()` to `real_data.py` using Dune Analytics `polymarket_polygon.market_prices_hourly` table
- Query by `condition_id` with `hour` filter at T-24h settlement offset
- Dune API key from config.toml `[dune]` section
- Insert into existing `bt_price_history` SQLite cache table
- Priority chain: CLOB T-24h → Dune hourly → Gamma active outcomePrices → forecast-derived (with penalty)

### 2. Forecast Fallback Conservative Penalty

- When falling back to forecast-derived prices, apply 5¢ spread penalty per bucket
- Flag these trades in trade log with `price_source="forecast"` for separate analysis
- Add `--forecast-penalty` CLI flag (default 0.05) to control penalty amount

### 3. Maker Fill Probability Model

- Add `FillModel` dataclass to `costs.py`:
  - `fill_prob_at_best: float = 0.50` (50% at best bid/ask)
  - `fill_prob_inside: float = 0.25` (25% 1¢ inside spread)
  - `fill_prob_tail: float = 0.10` (10% at tail prices 0.01-0.15 or 0.85-0.99)
  - `tail_price_range: tuple = (0.01, 0.15, 0.85, 0.99)`
- When `live_mode=True`, each trade rolls Bernoulli(fill_prob) — if not filled, skip the trade entirely
- Multiply expected P&L by fill_prob for conservative EV estimate
- Log fill/skip counts per strategy

### 4. Dual-Run Comparison Mode

- Add `--compare-forecast` flag to backtest CLI
- Runs backtest twice: (1) all markets including forecast fallback, (2) excluding forecast-only markets
- Print side-by-side showing the forecast bias delta per strategy
- Key metric: `forecast_bias = (return_all - return_clob_only) / return_clob_only`

## Acceptance Criteria

- [ ] Dune hourly prices fetched and cached for markets missing CLOB data
- [ ] Forecast fallback markets flagged with 5¢ penalty by default
- [ ] FillModel applied in live_mode with Bernoulli sampling
- [ ] `--compare-forecast` dual-run prints bias delta
- [ ] All existing 947 tests pass
- [ ] New tests for FillModel, Dune fetch, forecast penalty, dual-run
- [ ] ruff + mypy clean
