# Pitfall Fixes: V2 Fees, Low-Temp Cutoff, CLOB Robustness

## Goal

Fix all actionable pitfall issues identified in the weather-pitfalls research that have concrete code fixes.

## Requirements

### P0: V2 Dynamic Taker Fee
- Replace hardcoded TAKER_FEE=0.05 with V2 dynamic formula: `fee = C × feeRate × p × (1-p)`
- For weather markets: C=1, feeRateBps varies per market, max ~1.25% at 50-cent midpoint
- Update `neg_risk_sum.py` strategy to use dynamic fee
- Update `backtest/costs.py` CostModel.taker_fee_rate to 0.0125 (V2 max for weather)
- Add `fetch_market_fee()` to clob.py that queries CLOB API for per-market fee rate

### P1: Low-Temperature Morning Cutoff
- `PEAK_CUTOFF_HOUR=17` is wrong for low-temp markets (overnight low occurs 4-6AM local)
- Add `LOW_CUTOFF_HOUR=7` (7AM local = after overnight low has passed)
- `fetch_observed_high` → rename to `fetch_observation` returning both high and low info
- `is_past_peak` becomes conditional on measure_type: high→17, low→7
- Update `filter_recommendations` to use correct cutoff per measure_type
- Update scan.py, trade.py, watch.py, daemon.py call sites

### P1: CLOB 425 Retry + Heartbeat ID Recovery
- Add HTTP 425 (matching engine restart, every Tue 7AM ET) handling with exponential backoff
- Add heartbeat ID recovery: on heartbeat error, re-fetch valid heartbeat_id from API
- Add configurable HTTP timeout (≥15s default) to prevent silent duplicate fills

### P2: Temperature Spike Anomaly Detection
- Add spike detection in `fetch_observation`: if METAR temp jumped >3°C in <30min vs previous obs, log warning
- Log anomaly but do NOT block trading (could be legitimate frontal passage)
- Add `anomaly_detected` field to ObservedHigh dataclass

### P2: Dynamic ICAO from Market Description
- Add `resolve_icao_from_market()` that parses Wunderground URL from market description text
- Fallback to static CITY_ICAO map if no URL found
- Log warning when static ICAO used vs dynamic resolution

## Acceptance Criteria

- [ ] neg_risk_sum.py uses dynamic V2 fee, not hardcoded 0.05
- [ ] backtest CostModel uses V2-appropriate taker_fee_rate
- [ ] Low-temp markets use 7AM cutoff instead of 5PM
- [ ] ClobTrader handles HTTP 425 with retry
- [ ] Heartbeat loop recovers from ID errors
- [ ] HTTP timeout configurable (default 15s)
- [ ] Spike anomaly detection logs warnings for >3°C jumps
- [ ] Dynamic ICAO resolution from market description text
- [ ] ruff + mypy clean

## Definition of Done

- All acceptance criteria met
- Lint + typecheck clean
- Manual test of observation filtering with low-temp market
- No hardcoded API keys

## Out of Scope

- ECMWF/UKMO model integration
- LGBM calibration
- AI decision layer
- Multi-source oracle verification
- Hidden NegRisk outcome detection
- Builder fee support

## Technical Notes

- V2 fee formula: `fee = feeRateBps/10000 * price * (1-price)` where feeRateBps comes from market metadata
- CLOB API: `GET /market/{condition_id}` returns feeRateBps field
- AWC METAR API: returns `temp` in Celsius, `obsTime` in ISO format
- Market description text contains Wunderground URLs like `wunderground.com/history/daily/us/ny/new-york-city/KLGA/`
- 425 error occurs weekly Tue ~7AM ET for ~90 seconds
