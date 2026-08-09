# brainstorm: research new weather strategies

## Goal

Research and implement new profitable strategies for Polymarket daily-settlement weather markets, enriching the strategy library beyond the existing 5 strategies.

## What I already know

* Existing 5 strategies: truncation_edge (+1489%), neg_risk_sum (+1127%), gopfan2 (+1173%), ensemble_spread (+666%), resolution_div (+266%)
* narrow_no deleted (0 trades in real CLOB backtest)
* METAR observation infrastructure already in `pm_bot/core/observation.py`
* AWC METAR API already integrated for intraday filtering
* CLOB V2 dynamic taker fee (feeRateBps=50, exponent=0.5) already implemented
* °F truncation bugs fixed

## Research References

* [research/weather-strategies-2026.md](research/weather-strategies-2026.md) — 7 new strategy candidates + new wallets

## Requirements

### R1: Station Bias Kalman Filter (priority 1 — multiplier on all strategies)
* Track per-station bias using Kalman update: `bias_new = α * (observed - predicted) + (1-α) * bias_old`
* Record (predicted_high, actual_high, station, lead_time) after each market resolution
* Apply bias correction to forecast mean before bucket probability computation
* Rolling 30-day window, α=0.15 default
* Affects ALL existing strategies as a forecast improvement layer

### R2: METAR Lock Strategy (priority 2 — standalone)
* Track 6-hour max temp from METAR specials
* When observed max already exceeds a bucket boundary → buy NO (impossible to reach)
* When observed max is within a bucket AND past diurnal peak → buy YES (locked)
* Build on existing `observation.py` infrastructure
* Diurnal curve model per station/season

### R3: NegRisk Field-Fade (priority 3 — standalone)
* Monitor ΣYES across all buckets — exploit over-round (ΣYES > $1.02 after fees)
* Buy NO on 5-8 most overpriced outcomes
* Use limit orders exclusively (taker fees on multiple positions destroy edge)
* Distinct from neg_risk_sum which only exploits under-round

### R4: Cross-Day NegRisk Correlation (priority 4 — deferred)
* Exploit serial correlation between consecutive days' temperatures
* Conditional basket laddering under stable synoptic regimes
* Complex implementation — defer to future task

### R5: Day-of-Week / Seasonal Mean-Reversion (priority 5 — supplementary)
* 10-year climatology per (station, month, day_of_week)
* Bayesian blend: adjusted_prob = 0.85 * model_prob + 0.15 * climatology_prob
* Low edge (1-3%) but high frequency (40-80 trades/month)

## Acceptance Criteria

* [ ] Station Bias Kalman filter implemented and integrated into forecast pipeline
* [ ] METAR Lock strategy implemented as standalone strategy
* [ ] NegRisk Field-Fade strategy implemented (over-round ΣYES>$1.02 exploitation)
* [ ] Day-of-Week/Seasonal strategy implemented
* [ ] All new strategies pass ruff + mypy
* [ ] 90-day real-data backtest run for all strategies (old + new)
* [ ] Negative-EV strategies deleted if any

## Definition of Done

* ruff + mypy clean
* Backtest results documented
* New strategies registered in base.py and config.py

## Out of Scope

* Information Latency strategy (requires raw GRIB + VPS infrastructure — separate project)
* Humidity/Dewpoint strategy (edge too uncertain, no backtested data)
* Cross-Day NegRisk (complex, deferred)
