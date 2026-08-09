# Intraday Temperature Observation Filter

## Goal

Add real-time observed temperature filtering to pm-bot strategies, so that when the daily high temperature has already been reached (i.e., it's afternoon/evening in the target city and the observed high is known), strategies can correctly zero out probabilities for buckets above the observed max and avoid placing losing bets.

## What I already know

- Polymarket weather markets resolve based on Wunderground airport station data (whole-degree truncation)
- Markets settle daily — the "highest temperature" is the daily max at the airport station
- After the daily high has been observed (typically 2-4 PM local time), temperatures above that max have 0% probability
- Current strategies only use forecast data — they have no awareness of intraday observations
- This creates false opportunities: model says bucket X has 5% probability, but observed temp already proves that bucket is impossible
- METAR data is available from aviation weather APIs (AWC/NOAA) with ~1 hour latency
- Cities have known airport ICAO codes (KLGA=NYC, EGLL=London, etc.)

## Assumptions (temporary)

- METAR observations are available with <1hr latency for major airport stations
- The daily high temperature typically occurs between 12-4 PM local time
- After the daily high is reached, remaining time until market close is "free money" for observed-max filtering
- Some markets may have late-afternoon temperature spikes (especially in desert/dry climates)

## Open Questions

- What's the best real-time observation source? (METAR via AWC, Open-Meteo observations, Wunderground API)
- How to handle the "peak may still be coming" problem — is 3PM local always safe, or do we need to check if temperature is still rising?

## Requirements

1. Fetch current/recent observed temperature for each city's airport station (AWC METAR API)
2. Track max observed temp for current local day per city
3. After 5PM local time: set model_prob = 0 for all buckets with temp_high_c > observed_high (hard cutoff)
4. Before 5PM local time: no filtering applied
5. Add `--observed` flag to scan/trade/daemon commands to enable observation filtering
6. Integrate with existing strategies via forecast probability override
7. Handle timezone correctly for all 14 active cities

## Acceptance Criteria

- [ ] METAR observation fetcher returns current observed high for target city airport
- [ ] After 5PM local, strategies correctly zero out impossible buckets (temp > observed high)
- [ ] Before 5PM local, no filtering applied (conservative)
- [ ] Backtest with observation filtering shows improved P&L vs without
- [ ] No false positives — never zero out a bucket before 5PM local
- [ ] Timezone handling is correct for all 14 active cities

## Definition of Done

- Lint / typecheck clean
- Backtest validates improvement
- Edge cases handled (no METAR available, partial data, timezone boundaries)

## Research References

* [`research/observation-sources.md`](research/observation-sources.md) — AWC METAR is best source (free, JSON, 1-60min latency); Open-Meteo not real obs; 5PM local = high confidence daily max is set

## Research Notes

### Best observation source

**AWC METAR API** (Recommended): `https://aviationweather.gov/api/data/metar?ids=KLGA&format=json`
- Free, no auth, pre-decoded JSON with `temp` field (float °C)
- Hourly observations at :52-:55 past the hour
- Cache updated every 60 seconds

### Peak temperature timing

| Local Time | Confidence Daily Max Reached |
|-----------|------------------------------|
| 3:00 PM | ~75-85% |
| 4:00 PM | ~85-92% |
| 5:00 PM | ~92-97% (recommended filter activation) |
| 6:00 PM | ~97%+ |

### Polymarket settlement

- Resolution source: Wunderground History tab (airport station)
- Trading day: midnight-to-midnight local time
- Market resolves next day after data finalized
- Resolution precision: whole °F

### Feasible approaches

**Approach A: Pure METAR filter** (Recommended)

- Fetch METAR obs every 5 min for target ICAO stations
- Track max observed temp for current local day
- After 5PM local: set model_prob=0 for buckets above observed_max
- Before 5PM: use observed_max as soft upper bound (reduce probabilities above it)
- Simple, no extra dependencies

**Approach B: Progressive confidence scaling**

- Instead of hard cutoff at 5PM, use time-based confidence curve
- 3PM: 20% confidence → reduce upper-bucket probs by 20%
- 4PM: 50% → reduce by 50%
- 5PM: 80% → reduce by 80%
- 6PM: 95% → nearly zero out upper buckets
- More nuanced but more complex

**Approach C: Dual-source cross-validation**

- Compare METAR obs with Open-Meteo "current" model estimate
- When both agree temp is declining, higher confidence
- Unnecessary complexity for MVP

## Decision (ADR-lite)

**Context**: Need to filter impossible buckets when daily high already observed
**Decision**: Approach A (Pure METAR filter) — simplest, most reliable, AWC API returns decoded JSON directly
**Consequences**: Hourly observation granularity means we might miss spikes between METARs; 5PM cutoff is conservative but safe
