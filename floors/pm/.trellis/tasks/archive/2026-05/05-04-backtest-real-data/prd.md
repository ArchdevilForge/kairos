# Backtest with Real Market Data

## Goal

Replace synthetic price backtesting with real historical Polymarket prices + resolved outcomes, producing credible dry-run results for strategy selection before live deployment.

## What I already know

* CLOB API `GET /prices-history?market=TOKEN_ID&startTs=X&endTs=Y` — no auth, returns `{t, p}` time series
* CLOB `POST /batch-prices-history` — up to 20 tokens per request
* Gamma API `GET /events?closed=true` — resolved events, `outcomePrices=["1.0","0.0"]` indicates winner
* Gamma API markets have `clobTokenIds` (JSON string array) needed for prices-history
* Polymeteo `/v1/resolutions` + `/v1/candles` — weather-specific, $10/mo Trader tier
* Open-Meteo previous-runs API — "what forecast said on date X", available from Jan 2024
* Open-Meteo ensemble individual members only 3 days; mean/spread retained longer
* Dune `polymarket_polygon.market_prices_hourly` — hourly snapshots, needs Dune account
* Current backtest uses synthetic `_build_synthetic_event` — prices = forecast probability by definition, so no edge detectable

## Assumptions (temporary)

* Free CLOB + Gamma + Open-Meteo only (no paid Dune/PredMktData/Polymeteo)
* 30-90 days of history is enough for meaningful backtesting
* Weather markets have enough resolved events in the past 90 days

## Open Questions

* ~~How many resolved weather events exist in the past 90 days?~~ (will verify during implementation)

## Requirements

* Fetch resolved weather events from Gamma API (closed=true, temperature keyword)
* For each resolved event: extract winning bucket from outcomePrices
* For each bucket market: fetch historical price series via CLOB prices-history
* Fetch historical forecast from Open-Meteo previous-runs API
* Run strategies on historical prices at multiple time points (T-24h, T-12h, T-6h, T-1h)
* Compare strategy recommendations vs actual resolutions → compute real P&L
* Cost model: taker fee 5%, spread 2%, slippage 1%
* Output: per-strategy P&L, Sharpe, MaxDD, Win%, Brier score — same metrics as current
* CLI: `pm-bot backtest --real --strategy X --bankroll 100 --days 30`

## Acceptance Criteria

- [ ] `pm-bot backtest --real --strategy gopfan2 --bankroll 100 --days 30` produces results with non-zero trades
- [ ] Resolved outcomes match Polymarket official results
- [ ] Historical prices fetched from CLOB API, not synthesized
- [ ] Historical forecasts fetched from Open-Meteo previous-runs
- [ ] P&L accounts for taker fee + spread + slippage
- [ ] Results table shows same metrics as current backtest
- [ ] CSV export works for --real mode
- [ ] ruff + mypy clean

## Definition of Done

* Tests added/updated
* Lint / typecheck green
* Backtest results credible (no +1341% returns)
* Rollback: --real flag is additive, existing --days mode still works

## Out of Scope

* Ensemble member distribution backtesting (only 3 days retention)
* Dune/PredMktData paid data sources
* Paper trading mode (future task)
* Live daemon integration

## Technical Notes

* Key files: `pm_bot/backtest/data.py`, `pm_bot/backtest/engine.py`, `pm_bot/cli/backtest_cmd.py`
* Gamma API rate limits: /events 500/10s, /markets 300/10s
* CLOB batch-prices-history: 20 tokens max per request
* Weather events typically have 9-11 bucket markets each → batch API efficient
* Previous-runs API: `https://previous-runs-api.open-meteo.com/v1/forecast?...&past_days=N`
