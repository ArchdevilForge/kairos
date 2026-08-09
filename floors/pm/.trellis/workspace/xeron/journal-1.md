# Journal - xeron (Part 1)

> AI development session journal
> Started: 2026-05-04

---



## Session 1: pm-bot Phase 1-4: full implementation

**Date**: 2026-05-04
**Task**: pm-bot Phase 1-4: full implementation
**Branch**: `main`

### Summary

Built pm-bot from scratch across 4 phases: (1) CLI scanner with 5 commands and 3 strategies, (2) semi-auto CLOB trading with WebSocket/config/notifications, (3) fully automated daemon with Kelly sizing/risk controls/SQLite persistence, (4) 6 new strategies + backtesting framework. Fixed critical bugs: JSON price parsing, city alias resolution, airport coordinates, forecast measure_type. Total ~9000 lines, ruff+mypy clean.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `6e63c40` | (see git log) |
| `ef8096b` | (see git log) |
| `b42d8ba` | (see git log) |
| `b7fccdf` | (see git log) |
| `8e1a413` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 2: Real CLOB price backtesting with series_slug discovery

**Date**: 2026-05-05
**Task**: Real CLOB price backtesting with series_slug discovery
**Branch**: `main`

### Summary

Replaced broken Gamma closed=true pagination with series_slug endpoint (14 city series). Added CLOB /prices-history T-24h price fetching for resolved markets. 30-day real backtest: neg_risk_sum +64% (Sharpe 7.8), narrow_no +55%, gopfan2 +28% (43% MaxDD), sum_arb -19%. Key finding: forecast-derived prices vastly overstate edge vs real CLOB prices.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `ec2541b` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 3: Strategy research, prune, and CLOB backtest

**Date**: 2026-05-05
**Task**: Strategy research, prune, and CLOB backtest
**Branch**: `main`

### Summary

Researched weather market strategies via web search. Implemented TruncationEdge (truncation bias) and EnsembleSpread (forecast spread → tail underpricing). Fixed bucket_probability_numpy to use floor semantics matching Polymarket truncation. Ran 30-day real CLOB price backtest across NYC+London. Pruned 8 negative/marginal-EV strategies (sum_arb, station_change, airport_arb, metar_obs, cross_corr, precip_temp, ladder, station_bias). Final 6 positive-EV strategies: ensemble_spread +91%, narrow_no +55%, neg_risk_sum +51%, truncation_edge +41%, gopfan2 +29%, resolution_div +0.3%.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `a515cd3` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 4: Intraday METAR observation filtering + strategy research/prune

**Date**: 2026-05-05
**Task**: Intraday METAR observation filtering + strategy research/prune
**Branch**: `main`

### Summary

Two features: (1) Strategy research → added TruncationEdge + EnsembleSpread, fixed bucket_probability_numpy to floor semantics, pruned 8 negative-EV strategies, 30d real CLOB backtest confirmed 6 positive strategies. (2) METAR observation filtering → AWC API integration for real-time airport temperatures, 5PM local cutoff with floor(observed_high) confirmed bucket logic, --observed flag on scan/trade/watch + daemon auto-integration.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `a515cd3` | (see git log) |
| `ed04d5a` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 5: Weather market pitfalls research + Paris ICAO fix

**Date**: 2026-05-05
**Task**: Weather market pitfalls research + Paris ICAO fix
**Branch**: `main`

### Summary

Web research identified 12 pitfall categories in Polymarket weather markets. Critical: sensor manipulation (Paris CDG attack), station changes without notice. High: NegRisk structural traps, heartbeat/425 errors. Fixed Paris ICAO LFPG→LFPB. Saved 5 constraint memories. Research doc at .trellis/tasks/archive/2026-05/05-05-05-05-weather-pitfalls-research/research/weather-pitfalls.md

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `b43fdba` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 6: Pitfall fixes: V2 fees, low-temp cutoff, CLOB robustness

**Date**: 2026-05-05
**Task**: Pitfall fixes: V2 fees, low-temp cutoff, CLOB robustness
**Branch**: `main`

### Summary

Fixed 6 pitfall categories from weather-pitfalls research: (1) V2 dynamic taker fee replacing hardcoded 5% with feeRateBps×p×(1-p) max 1.25%, (2) low-temp markets use 7AM cutoff not 5PM, (3) HTTP 425 retry with exponential backoff, (4) heartbeat ID recovery after 3 consecutive errors, (5) temperature spike anomaly detection ≥3°C, (6) dynamic ICAO resolution from Wunderground URL in market description. 12 files, +336/-68 lines.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `91f3140` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 7: Deep backtest audit: 8 bugs found and fixed

**Date**: 2026-05-05
**Task**: Deep backtest audit: 8 bugs found and fixed
**Branch**: `main`

### Summary

Exhaustive web research + code audit found 8 bugs affecting backtest accuracy. CRITICAL: taker fee RateBps=100→50 + exponent 0.5 (was 2x overpriced), CLOB T-24h timezone fix (UTC→local midnight), bucket upper bound <=→< for floor semantics, °F truncation in °C space (new temp_unit param), ensemble city-specific std, CLOB price filter 0.01→0.005, continuous approx +1.0 removal, neg_risk_sum fee consistency. All fixes ruff+mypy clean.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `a458d95` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 8: °F bucket bug fixes and corrected backtest

**Date**: 2026-05-05
**Task**: °F bucket bug fixes and corrected backtest
**Branch**: `main`

### Summary

Found and fixed °F-related bugs: bucket_probability_numpy floor truncation in °F space, _bucket_hit floor semantics, narrow_no °F bucket width, aggregation.py °F support. Also fixed in prior commit: feeRateBps 50+exponent 0.5, timezone-aware T-24h, CLOB tail price filter, ensemble std scaling. Corrected 90-day backtest: truncation_edge +1489%, neg_risk_sum +1127%, gopfan2 +1173%, ensemble_spread +666%, resolution_div +266%.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `a458d95` | (see git log) |
| `d4ab0b5` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 9: New strategies, auto-settle, 90% test coverage

**Date**: 2026-05-05
**Task**: New strategies, auto-settle, 90% test coverage
**Branch**: `main`

### Summary

Implemented neg_risk_field_fade (+1754%/90d, ΣYES>1.02 over-round exploitation), station_bias kalman filter, auto-settle with poly-web3 (redeem/merge for compound reinvestment). Deleted metar_lock (-710% look-ahead bias) and mean_reversion (0 trades). Built test suite from scratch: 947 tests, 90% coverage across 51 test files. 60 files changed, +9821 lines.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `b826652` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 10: Backtest fidelity: FillModel, Dune prices, forecast penalty, portfolio mode

**Date**: 2026-05-05
**Task**: Backtest fidelity: FillModel, Dune prices, forecast penalty, portfolio mode
**Branch**: `main`

### Summary

Implemented backtest fidelity improvements: FillModel (maker fill probability 50/25/10% by price zone), Dune Analytics hourly price backfill for 42% markets missing CLOB data, forecast penalty (5¢/share), dual-run --compare-forecast showing forecast bias delta, portfolio mode --portfolio (all strategies share bankroll, merged signals per event). 90d 8-city live portfolio: +454% Sharpe 4.6 MaxDD 2.9%. Found only 3 active cities (NYC/London/Tokyo) have resolved events; Dallas/Atlanta/LA have zero. 11 files, +1243 lines.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `cc6c8ab` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 11: Remove low-temp markets + bug fixes

**Date**: 2026-05-05
**Task**: Remove low-temp markets + bug fixes
**Branch**: `main`

### Summary

Fixed 3 critical bugs (low-temp forecast hardcoded temperature_2m_max → 0.0°C, gopfan2 fallback edge giving 80% when model=0, truncation_edge °F frac using °C mean). Then removed all low-temp market support: deleted _detect_measure_type, LOW_CUTOFF_HOUR, temperature_2m_min parsing, dual forecast fetching. Hardcoded measure_type=high across all modules. 977 tests pass, ruff+mypy clean. 6-strategy 90-day live backtest: +273% to +433%.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `057d3e9` | (see git log) |
| `ca4a037` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 12: Remove low-temp markets + active market backtest

**Date**: 2026-05-05
**Task**: Remove low-temp markets + active market backtest
**Branch**: `main`

### Summary

Deleted all measure_type=low support (forecast, observation, event filtering) to focus on daily high-temp only. Added fetch_active_weather_events() and fetch_actual_temps() (Open-Meteo archive API) to include unsettled markets in backtest. Fixed 30-day 0-trades: 6 strategies now +0%~+82% in 30d, +278%~+425% in 90d. 977 tests pass.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `ca4a037` | (see git log) |
| `a620165` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 13: ERA5 bias fix + ensemble calibration + exhaustive grid search

**Date**: 2026-05-06
**Task**: ERA5 bias fix + ensemble calibration + exhaustive grid search
**Branch**: `main`

### Summary

Fixed 3 backtest fidelity issues: ERA5 Tmax cold bias correction (+0.5~1.2°C per city), seasonal ensemble calibration (summer 1.1-1.4x, winter 0.7-0.9x), GFS 1.15x under-dispersion factor. Added preloaded_fetcher/preloaded_events to BacktestEngine for fast grid search. Ran exhaustive 4-phase grid search (48 portfolio configs, 144 single strategy configs, 57 subset combos, time/bankroll scaling). Result: k=0.15 dominates all kelly values. Best return: gopfan2+neg_risk_sum+ensemble_spread (8c, k=0.15, sl=20%) = +699%/90d. Best score: resolution_div+ensemble_spread+neg_risk_field_fade = Sharpe 5.3. Maker-only eliminates taker fee. 00→34 in 90 days.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `c3599b4` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 14: Dry-run paper trading daemon, AWS deployment

**Date**: 2026-05-06
**Task**: Dry-run paper trading daemon, AWS deployment
**Branch**: `main`

### Summary

Added dry-run mode with PaperTradeDB (00 bankroll, Kelly=0.15, SL=20%), RiskDB protocol for risk manager, paper-pnl CLI command. Deployed to AWS as systemd service with optimal strategy config (gopfan2+neg_risk_sum+ensemble_spread, 8 cities). Fixed bankroll-scaled position limits (max_single=15% bankroll). Confirmed .trellis/ safe to push (no secrets).

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `9011d50` | (see git log) |
| `2055ae1` | (see git log) |
| `03f62bc` | (see git log) |
| `0b98936` | (see git log) |
| `ec6bacd` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 15: Deploy dry-run daemon to rn server, log cleanup

**Date**: 2026-05-06
**Task**: Deploy dry-run daemon to rn server, log cleanup
**Branch**: `main`

### Summary

Deployed pm-bot dry-run daemon to rn server (systemd service, logrotate hourly/5M/2copies). Fixed: bankroll-scaled position limits (max_single=15% bankroll), PaperTradeDB implements RiskDB protocol, NWS 404 downgraded to debug, CLOB order fetch to warning. Paper trading running: 10 open positions, 00 bankroll, gopfan2 tail signals active.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `1519030` | (see git log) |
| `9011d50` | (see git log) |
| `2055ae1` | (see git log) |
| `03f62bc` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 16: Expand weather city universe to 36 and comprehensive backtest

**Date**: 2026-05-07
**Task**: Expand weather city universe to 36 and comprehensive backtest
**Branch**: `main`

### Summary

Expanded Polymarket weather cities from 14 to 36 (added Beijing, Madrid, Wellington, Milan, Wuhan, Munich, Moscow, San Francisco, Istanbul, Jakarta, Mexico City, Amsterdam, Busan, Seattle, Toronto, Cape Town, Atlanta, Dallas, LA, São Paulo, Buenos Aires). Ran comprehensive backtests across all 6 strategies × 9/14/34 cities × 90/180/365 days. Best strategy: neg_risk_field_fade (+53,710% return, 79.8% win, 0.3% MaxDD). Best portfolio: all 6 strategies combined (+57,102% return, Sharpe 5.00). Key finding: live mode (maker-only, 8% min edge) produces 0 trades — the live↔backtest fidelity gap is the core issue.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `fa89f46` | (see git log) |
| `cc4be95` | (see git log) |
| `67f98bf` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 17: Fix P&L bug, add realistic spread, remove unviable strategies

**Date**: 2026-05-07
**Task**: Fix P&L bug, add realistic spread, remove unviable strategies
**Branch**: `main`

### Summary

Fixed critical NO P&L formula inversion, added realistic Polymarket orderbook spread model (--spread param), discovered all mid-bucket strategies are negative EV with real bid/ask (/bin/bash.01//bin/bash.99). Removed 5 unviable strategies (neg_risk_field_fade, neg_risk_sum, truncation_edge, ensemble_spread, resolution_div), kept only gopfan2 modified for tail-YES only. Updated trading-config spec with 4 pitfalls documented. Realistic return estimate: ~22%/year via tail-YES lottery tickets.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `9e7f009` | (see git log) |
| `73c11a6` | (see git log) |
| `69ef7ce` | (see git log) |
| `97e0b57` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete
