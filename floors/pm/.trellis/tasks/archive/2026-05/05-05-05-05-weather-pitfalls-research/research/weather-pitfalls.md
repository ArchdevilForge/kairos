# Polymarket Weather Market Pitfalls Research

## Critical (🔴)

### 1. Sensor Physical Manipulation ("Blow Dryer Attack")
- **Apr 2026 Paris CDG**: Trader used portable heating device on LFPG sensor, earned ~$35K across 2 events
- Polymarket switched Paris from LFPG (CDG) → LFPB (Le Bourget) on Apr 19 — no announcement, no refunds
- **pm-bot bug**: `CITY_ICAO` still maps Paris → LFPG. Must update to LFPB.
- **Missing**: Multi-source verification, spike anomaly detection, station swap alerts
- **Mitigation**: Parse Wunderground URL from market rules text (dynamic ICAO), add temp spike alert (>3°C in <30min), log station IDs

### 2. Station Changes & Outages
- Polymarket silently changes settlement stations — no API notification
- Paris CDG→Le Bourget is confirmed case
- If airport METAR goes offline, resolution behavior is undocumented
- **Mitigation**: Dynamic ICAO from market description, station offline monitoring, daily station validation

## High (🟠)

### 3. NegRisk Structural Traps
- Hidden "other" outcome slots not shown in UI — must check on-chain metadata
- Invalid outcome risk: if NegRisk adapter receives [1,1] it reverts
- NO→YES conversion is one-way only (liquidity trap)
- V2 migration (Apr 28, 2026) cleared all V1 orders — `nonce` → `timestamp`, `feeRateBps` removed
- **Mitigation**: Dynamic V2 fee query, hidden outcome detection, V2-specific error handling

### 4. Heartbeat & Order Management Traps
- 10s + 5s buffer heartbeat mandatory since Jan 2026
- Heartbeat ID chain: each response contains new `heartbeat_id` for next request
- HTTP timeout → silent duplicate fills (py-clob-client#273, fixed in PR #318)
- **425 matching engine restart**: Every Tue 7AM ET (~90s), must retry with backoff
- Batch order limit: 15 per batch
- **Mitigation**: Heartbeat ID recovery on error, configurable HTTP timeout (≥15s), 425 retry logic, duplicate order detection

## Medium (🟡)

### 5. DST/Timezone Issues
- 2024 "time warp" bug: DST switch caused same start/end times in API
- Weather uses Wunderground local clock time (midnight-to-midnight)
- During DST, WU local clock vs NWS standard time creates 1-hour offset
- `endDate` API field returns `T12:00:00Z` — misleading
- **Mitigation**: Never trust endDate for trading cutoffs, use local timezone with ZoneInfo

### 6. Midnight Rollover Timing
- Market closes when "all data finalized" — typically 1-3AM next day local
- No auto-clearing of orderbook at close (unlike sports)
- Can still trade during resolution period — risky
- **Mitigation**: Track resolution status, avoid trading after WU daily summary published

### 7. Multi-Station Conflicts
- NYC: Polymarket=KLGA (LaGuardia), Kalshi=KNYC (Central Park) — 2-6°F difference
- London: EGLC (City) vs EGLL (Heathrow) — need per-market verification
- Tokyo: RJTT (Haneda) vs RJAA (Narita)
- Retail traders see city-center temps, markets settle on airport — source of edge AND confusion
- **Mitigation**: Parse ICAO from market rules text, not static map

### 8. Truncation vs Rounding (Already Handled ✅)
- Market rules: "resolves to whole-degree Fahrenheit" = truncation
- pm-bot uses `np.floor()` in `bucket_probability_numpy` — correct
- Edge case: exactly integer temps (23.0°C) are unambiguous but WU display format varies

### 9. Low-Temperature Market Differences
- Overnight low occurs **4-6AM local**, not 5PM
- `PEAK_CUTOFF_HOUR = 17` is WRONG for low-temp markets
- Low markets have narrower range (3-5°C), thinner volume
- Radiative cooling, fog, urban heat island effects make lows harder to predict
- **Mitigation**: Separate cutoff logic for measure_type="low" — lock around 6-7AM local

### 10. Market Creation/Expiration Timing
- Markets appear 2-5 days before target date
- Early price discovery is sparse (wide spreads, low volume)
- New cities can be added/rotated without notice
- **Mitigation**: Market age/volume filtering, track creation dates

### 11. Fee & Cost Traps
- V2 dynamic taker fee: `fee = C × feeRate × p × (1-p)`, max ~1.25% at 50-cent midpoint
- pm-bot hardcodes TAKER_FEE=0.05 (5%) — **4x too high**, over-estimates cost
- Builder fees: up to 1% taker if order routed through builder with builderCode
- Holding rewards: 4% APY on qualifying positions offsets some costs
- **Mitigation**: Dynamic V2 fee query per market, fee-adjusted Kelly sizing

### 12. Competing Bot Behavior
- 7+ known weather bots with similar models (GFS + ECMWF + METAR)
- Advanced bots (PolyWeather, weather-edge) use 5+ models + physics corrections
- All bots missing: multi-source oracle verification, sensor manipulation detection
- Edge erodes quickly when multiple bots target same mispricing
- **Mitigation**: Consider adding ECMWF/UKMO models, LGBM calibration

## Summary: Immediate Action Items for pm-bot

| Priority | Fix | Effort |
|----------|-----|--------|
| P0 | Update Paris ICAO LFPG→LFPB | 1 line |
| P0 | Fix TAKER_FEE from 0.05 to dynamic V2 query | Medium |
| P1 | Add low-temp market morning cutoff (6-7AM) | Small |
| P1 | Add 425 matching engine restart retry | Small |
| P1 | Add heartbeat ID recovery on error | Small |
| P2 | Dynamic ICAO from market rules text | Medium |
| P2 | Temp spike anomaly detection | Medium |
| P2 | Hidden NegRisk outcome detection | Medium |
| P3 | Configurable HTTP timeout (≥15s) | Small |
| P3 | Duplicate order detection | Medium |
