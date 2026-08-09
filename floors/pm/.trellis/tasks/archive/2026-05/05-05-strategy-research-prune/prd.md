# Strategy Research & Prune for Positive EV

## Goal

Research new Polymarket weather strategies from public sources, implement promising ones, run real-data backtests, and delete strategies that cannot achieve positive EV with CLOB prices.

## What I Already Know

### Current strategy performance (30-day real CLOB backtest, $100 bankroll)
- neg_risk_sum: +64% (Sharpe 7.8, 85% win, 72 trades) — **KEEP**
- narrow_no: +55% (Sharpe 4.9, 100% win, 10 trades) — **KEEP**
- gopfan2: +28% (Sharpe 2.4, 35% win, 297 trades, 43% MaxDD) — **RISKY**
- resolution_div: +19% (Sharpe 5.9, 51% win, 51 trades) — **KEEP**
- ladder: +14% (Sharpe 2.8, 49% win, 43 trades) — **MARGINAL**
- station_change: -10% — **DELETE**
- sum_arb: -19% — **DELETE**
- airport_arb: 0 trades — **DELETE**
- metar_obs: 0 trades — **DELETE**
- cross_corr: 0 trades — **DELETE**
- precip_temp: 0 trades — **DELETE**

### Research findings (from research/weather-strategies.md)
10 strategy categories identified with edge estimates. Top new candidates:
1. **Truncation Boundary Edge** (1–5%, low competition) — floor() not round()
2. **Airport vs City Station Bias** (5–20%, medium competition) — forecast at airport ICAO coords
3. **METAR Nowcasting** (15–40%, med-high competition) — intraday convergence 9-10AM local
4. **Ensemble Spread Exploitation** (5–20%, medium) — adaptive σ + tail reweighting
5. **Barbell/Tail Conviction** (gopfan2 variant with model validation)

### Critical infrastructure facts
- Resolution: Wunderground airport station, **truncation** (not rounding)
- NegRisk: ΣYES<$1.00 arb via STANDARD orders (not NegRisk)
- Heartbeat: 10s + 5s buffer = 15s window
- Free data: Open-Meteo (GFS 31 + ECMWF 51 members), METAR, TAF

## Requirements

1. Implement top 3–5 new strategies from research findings
2. Run 30-day real-data backtests on ALL strategies (existing + new)
3. Delete strategies with negative EV or 0 trades in backtest
4. Refine surviving strategies with truncation correction and airport coords
5. Output final strategy roster with verified positive EV

## Acceptance Criteria

- [ ] At least 2 new strategies implemented and tested
- [ ] All strategies with negative real-data P&L removed from codebase
- [ ] Remaining strategies each have positive EV in 30-day real CLOB backtest
- [ ] Truncation boundary correction applied to all forecast→probability calculations
- [ ] Airport ICAO coordinates used for forecast fetching (not city center)
- [ ] ruff + mypy clean

## Technical Approach

1. Add truncation-aware probability calculation (floor not round) to forecast engine
2. Add airport ICAO coordinate resolution per city
3. Implement: TruncationEdge, MetarNowcast, StationBias, EnsembleSpread strategies
4. Refine gopfan2 → BarbellConviction (model-validated tail buying)
5. Run backtests, prune, commit

## Out of Scope

- Cross-platform Kalshi arb (requires separate account)
- Direct ECMWF API (paid, latency arb focus)
- LGBM/ML calibration model (too complex for this round)

## Research References

- `research/weather-strategies.md` — comprehensive strategy catalogue with 10 categories
