# Polymarket Weather Market Strategies — 2025-2026 Research

**Date**: 2026-05-05  
**Scope**: New strategies NOT already implemented in `pm_bot/strategies/`  
**Existing strategies covered**: `truncation_edge`, `neg_risk_sum`, `gopfan2` (barbell tail), `ensemble_spread`, `resolution_div`

---

## Top 3 Most Promising NEW Strategies

1. **METAR Lock (Same-Day Certainty)** — ~88% win rate, 3–15% edge per trade, 30–60 trades/month when active
2. **Cross-Day NegRisk Correlation / Conditional Laddering** — 5–12% edge, 20–40 trades/month, structural not timing-dependent
3. **Station Bias Kalman Filter (Decaying-Average UHI Correction)** — 2–8% persistent edge on every trade, 200+ trades/month, lowest implementation risk

---

## 1. METAR Lock Strategy (Same-Day Certainty)

### Edge Source
By 2–5 PM local time at most mid-latitude stations, the daily high has **already been observed** at the resolution station via METAR reports. The market hasn't closed yet — but the physical outcome is nearly locked. If the station has recorded 82°F at 3:00 PM and typical diurnal curves show the peak has passed, the probability that the day's high exceeds 82°F drops to near zero.

Polymarket prices lag this reality by minutes to hours because:
- No automated feed pushes METAR observations into market prices
- Only bots with live METAR feeds capture this alpha
- Retail traders don't check aviation weather

### Quantification

| Metric | Estimate |
|--------|----------|
| **Expected Edge** | 3–28% per trade (wider early, tighter late) |
| **Win Rate** | ~88% (documented by KalshiWeatherEdge) |
| **Trade Frequency** | 30–60 trades/month (active only 2–5 PM local per city) |
| **Key Risk** | Late-afternoon convective surge pushes temp above observed max; METAR reading error; Wunderground finalization differs from live METAR |
| **Data Source** | aviationweather.gov METAR feed (free), station-specific diurnal curve model |

### Implementation Details
- Subscribe to METAR observations for each resolution station (KLGA, EGLC, RJTT, etc.)
- Track `6-hour max temp` in METAR specials (e.g., `T02460028` = 24.6°C max, 2.8°C min)
- Compare observed max to each bucket boundary: if max already observed > bucket_high, that bucket is dead (buy NO at any price > $0.02)
- If max observed is within a bucket with high confidence it won't be exceeded, buy YES on that bucket
- **Diurnal curve modeling**: For each station and season, model the typical hour of max temp. If we're past that hour and temp is declining, lock probability approaches 1.0

### Why This Is NOT Covered by Existing Strategies
- `truncation_edge` works on forecast-model boundaries, not observed reality
- `ensemble_spread` requires forecast uncertainty; this strategy benefits from certainty
- `resolution_div` compares WU vs NWS sources; this uses raw METAR before either source finalizes

---

## 2. Cross-Day NegRisk Correlation / Conditional Laddering

### Edge Source
Polymarket runs temperature markets for the **same city on consecutive days**. These are separate NegRisk events (each day is an independent multi-outcome market). However, the underlying temperatures are **serially correlated** — if today's high is 30°C, tomorrow's is very likely within ±4°C.

This creates opportunities that go beyond simple ΣYES≠$1:

**A. Cross-Day NegRisk Basket Arbitrage**
If Day N shows a very tight distribution (σ≈0.8°C, high confidence) centered at 28°C, but Day N+1 at the same city has buckets priced as if they're independent — you can construct a cross-day conditional basket. The market for Day N+1 doesn't fully reflect the information in Day N's near-certainty outcome.

**B. Regime-Conditioned Laddering**
When a stable synoptic pattern (blocking high, persistent air mass) spans multiple days, the correlation between consecutive days' temperatures increases. Under these conditions:
- Buy a ladder of YES across 3 adjacent buckets on Day N+1 weighted by Day N's resolved or near-resolved outcome
- The NegRisk structure means $1 of collateral covers the entire ladder
- Edge comes from the market pricing Day N+1 buckets as if they're independent draws rather than conditioned on Day N

**C. Frontal-Passage Conditional Shift**
When a cold front is forecast to pass between Day N and Day N+1:
- Day N will be warm (high confidence)
- Day N+1 will be significantly cooler (high uncertainty → wide spread)
- The market often underestimates the magnitude of the temperature drop
- Buy NO on warm buckets for Day N+1, YES on cool buckets, conditioned on Day N resolving warm

### Quantification

| Metric | Estimate |
|--------|----------|
| **Expected Edge** | 5–12% per trade (conditional basket) |
| **Win Rate** | 65–75% (depends on regime detection accuracy) |
| **Trade Frequency** | 20–40 trades/month (requires consecutive-day pairs + identifiable regime) |
| **Key Risk** | Front timing uncertainty; regime breaks unexpectedly; non-atomic execution across two NegRisk events |
| **Data Source** | ECMWF/GFS synoptic charts; historical autocorrelation by city/season; CLOB API for both days' prices |

### Why This Is NOT Covered by Existing Strategies
- `neg_risk_sum` only checks ΣYES within a single day's event
- No existing strategy treats consecutive days as a correlated portfolio
- The NegRisk laddering benefit (capital efficiency across multiple days) is unexploited

### Academic Backing
The AFT 2025 paper (IMDEA Networks, 86M bets) documented "Combinatorial Arbitrage" across dependent markets — $95K extracted from 13 dependent pairs. Weather consecutive-day markets are a natural application of this pattern that the paper didn't specifically address but the math generalizes.

---

## 3. Station Bias Kalman Filter (Decaying-Average UHI Correction)

### Edge Source
Every resolution station has a **systematic bias** relative to model forecasts:
- Urban heat island: airport surrounded by pavement reads 2–5°F warmer than model grid cell
- Coastal effects: sea breeze not resolved at 9km grid spacing
- Elevation mismatch: model grid elevation ≠ station elevation

The `pm_bot` already uses `bucket_probability_numpy` with a Gaussian model, but it does NOT apply station-specific bias correction. The polymarketweather.com bot documentation describes a Kalman-filter approach:

```
bias_new = α * (observed - predicted) + (1 - α) * bias_old
```

Where α = 0.1–0.3 and the bias is tracked per station, per lead time, over the last 30 days. This single correction captures:
- Urban heat island offsets
- Airport-vs-city differences  
- Systematic model bias for a given station

**This is the single most impactful improvement to the existing forecast→probability pipeline.** It's not a standalone strategy but a layer that improves ALL existing strategies.

### Quantification

| Metric | Estimate |
|--------|----------|
| **Expected Edge** | 2–8% improvement on every trade (shifts probability estimates toward truth) |
| **Win Rate** | Improves all strategy win rates by 3–5 percentage points |
| **Trade Frequency** | Affects 200+ trades/month (every trade using forecast) |
| **Key Risk** | Bias drifts over seasons; needs 30-day warm-up; Kalman α tuning |
| **Data Source** | Wunderground historical data (resolution outcomes); Open-Meteo forecast archives; 30-day rolling window |

### Implementation Approach
1. After each market resolves, record: `(predicted_high, actual_high, station, lead_time_hours)`
2. Update bias per (station, lead_time_bucket) using Kalman update
3. Apply bias correction to forecast mean before computing bucket probabilities
4. Example: KLGA (LaGuardia) shows +1.8°F bias at 24h lead time → subtract 1.8°F from forecast before probability computation

### Specific Station Biases Documented
| Station | Known Bias | Source |
|---------|-----------|--------|
| KLGA (LaGuardia) | -3 to -6°F vs Central Park (cooler, waterfront) | polymarketweather.com, wethr.net |
| KMDW (Chicago Midway) | +2 to +5°F vs O'Hare (urban heat island) | wethr.net |
| KNYC (Central Park) | +5 to +10°F overnight lows vs suburbs | wethr.net |
| EGLC (London City) | Varies by wind direction; southerly flow = warmer | weatherstationadvisor.com |
| RJTT (Tokyo Haneda) | Coastal moderation; model often overestimates highs | polymarketweather.com |
| LFPB (Paris Le Bourget) | Consistently cooler than city center | polymarketweather.com |

### Why This Is NOT Covered by Existing Strategies
- `truncation_edge` and `ensemble_spread` use raw forecast mean without bias correction
- `resolution_div` compares WU vs NWS but doesn't maintain a rolling bias tracker
- No existing strategy applies Kalman-filter station correction

---

## 4. Information Latency Exploitation (Model-Update Windows)

### Edge Source
ECMWF and GFS update on fixed schedules:
- **GFS**: 00Z, 06Z, 12Z, 18Z — output available ~3.5 hours after run start
- **ECMWF HRES**: 00Z, 12Z — output available ~7 hours after run start
- **HRRR**: Hourly — output available within ~1 hour

The **lag between model publication and market repricing** is the single most documented edge in Polymarket weather trading. Hans323 reportedly made $1.1M+ exploiting this window.

### Timeline (from polymarkets.co.il weather guide)
| UTC Time | What Publishes | Market Impact Window |
|----------|---------------|---------------------|
| 03:30 | GFS 00Z complete | First priceable signal |
| 07:00 | ECMWF 00Z HRES complete | Strongest medium-range repricing |
| 15:30 | GFS 12Z complete | US morning repricing |
| 19:00 | ECMWF 12Z HRES complete | **Biggest retail reaction window** |

### Quantification

| Metric | Estimate |
|--------|----------|
| **Expected Edge** | 5–25% per trade (immediately after model run; decays within 15–60 min) |
| **Win Rate** | 60–70% (depends on model accuracy; ECMWF > GFS) |
| **Trade Frequency** | 8–16 trades/month (4 GFS + 2 ECMWF cycles per day × relevant cities) |
| **Key Risk** | Window shrinking as more bots compete; model run may not shift forecast; execution speed critical |
| **Data Source** | Direct GRIB file ingestion from NOAA NOMADS (GFS/HRRR) or ECMWF API (subscription); pre-staged orders |

### Implementation Requirements
- **Raw GRIB access**: Pull directly from NOAA NOMADS or ECMWF API (not Open-Meteo, which adds processing delay)
- **Pre-staged orders**: For each city, pre-compute which buckets would become attractive under various forecast shifts, and have limit orders ready to submit within seconds
- **VPS near Polygon RPC**: Dublin/Frankfurt VPS achieves 2–6ms round-trip vs 180–420ms residential (documented 14% edge loss per 100ms latency)
- **WebSocket price streaming**: `wss://ws-subscriptions-clob.polymarket.com/ws/market` for sub-second price updates

### Competitive Reality
The window is shrinking. In 2024, the latency window was 10–30 minutes. In 2026, it's 2–15 minutes on liquid markets. However:
- Thin international markets (Ankara, Wuhan, Taipei) still show 30–60 minute windows
- The ECMWF 12Z run completing at 19:00 UTC still catches many retail traders off-guard
- Hans323's approach was semi-manual; fully automated GRIB parsing is faster

### Why This Is NOT Covered by Existing Strategies
- No existing `pm_bot` strategy monitors model-release timing
- No strategy ingests raw GRIB files or uses pre-staged orders
- This is the **highest-edge** strategy but requires the most infrastructure investment

---

## 5. NegRisk Field-Fade (Over-Round Exploitation)

### Edge Source
When ΣYES > $1.00 across all buckets in a NegRisk event (empirically ~65% of large-field markets), buying NO on the most overpriced outcomes provides structural edge. The NegRisk adapter caps your collateral at $1.00 regardless of how many NO positions you hold.

This is distinct from `neg_risk_sum` which only exploits ΣYES < $1.00 (underpriced). The **over-round** (ΣYES > $1.00) case is actually more common but requires a different execution path.

### Quantification

| Metric | Estimate |
|--------|----------|
| **Expected Edge** | 2–5% per basket (after fees and slippage) |
| **Win Rate** | 70–80% (one of N-1 buckets must win) |
| **Trade Frequency** | 15–30 trades/month (on markets where ΣYES > $1.02 after fees) |
| **Key Risk** | Taker fees on 5–8 NO positions add up (0–1.8% each); execution is non-atomic; must use limit orders |
| **Data Source** | CLOB API prices for all buckets in event; NegRisk adapter conversion mechanics |

### Implementation Details
- Monitor all bucket YES prices in each NegRisk weather event
- When ΣYES > $1.02 (after accounting for taker fees), buy NO on 5–8 most overpriced buckets
- Size position by `min(liquidity_across_all_conditions) × |ΣYES - 1.0|`
- Use limit orders exclusively (taker fees on 5–8 positions destroy edge)
- NegRisk conversion: 1 NO token converts to YES in every other outcome

### Why This Differs from Existing `neg_risk_sum`
- Current `neg_risk_sum` only fires when ΣYES < 0.98 (buying all YES for risk-free arb)
- This strategy exploits the more common over-round case (buying NO on overpriced outcomes)
- Requires different execution (multi-leg NO basket vs single-bucket YES)

---

## 6. Day-of-Week / Seasonal Mean-Reversion Patterns

### Edge Source
Temperature exhibits systematic patterns:
- **Weekend effect**: Urban heat island is stronger on weekdays (industrial/commercial activity) vs weekends → weekday highs slightly warmer at urban stations
- **Seasonal mean-reversion**: After 3+ consecutive days above normal, the probability of reversion to climatology increases
- **Monthly anomaly persistence**: If the monthly temperature anomaly is +2°C through the 20th, remaining days are slightly more likely to be above normal too (persistence)

### Quantification

| Metric | Estimate |
|--------|----------|
| **Expected Edge** | 1–3% per trade (small but persistent) |
| **Win Rate** | 55–62% |
| **Trade Frequency** | 40–80 trades/month (applies on most days) |
| **Key Risk** | Signal is weak; easily overwhelmed by synoptic pattern; requires many trades to show statistical significance |
| **Data Source** | NOAA NCEI historical base rates; 10-year climatology per station/month/day-of-week |

### Implementation
- Compute 10-year average high temperature for each (station, month, day_of_week) combination
- When forecast = climatology ± small delta, the prior reinforces the forecast
- When forecast deviates significantly from climatology AND it's been 3+ days in the same direction, add a mean-reversion adjustment to the probability distribution
- Bayesian blend: `adjusted_prob = 0.85 * model_prob + 0.15 * climatology_prob`

---

## 7. Humidity/Dewpoint Correlation Strategy

### Edge Source
High dewpoint temperatures correlate with:
- **Suppressed daytime highs**: Humid air has higher heat capacity, requiring more energy to warm
- **Elevated overnight lows**: Moisture acts as thermal blanket
- **Convective initiation risk**: When dewpoint depression is small, thunderstorms develop more readily, which can cap afternoon highs early

Specifically for daily high markets:
- If morning dewpoint is within 3°C of forecast high → convective risk → buy NO on high-end buckets
- If dewpoint is very low (dry air) → expect wider diurnal swing → buy YES on high-end and low-end (wider distribution)

### Quantification

| Metric | Estimate |
|--------|----------|
| **Expected Edge** | 2–6% per trade (conditional on humidity regime) |
| **Win Rate** | 60–68% |
| **Trade Frequency** | 20–40 trades/month (only when humidity regime is identifiable) |
| **Key Risk** | Dewpoint data quality varies by station; convective timing is stochastic |
| **Data Source** | METAR observations (includes dewpoint/relative humidity); NBM dewpoint forecasts |

### Implementation
- Ingest morning METAR (06:00–10:00 local) dewpoint/temperature
- Compute dewpoint depression = T_forecast_high - Td_morning
- If depression < 5°C: apply convective suppression adjustment (shift probability distribution 1–2°C cooler)
- If depression > 15°C: apply dry-air amplification adjustment (widen σ by 0.5–1.0°C)
- Blend with ensemble forecast probabilities

---

## 8. New Profitable Wallets (2026) Beyond gopfan2/ColdMath/Hans323

### Updated Weather Leaderboard (polyintel.io, April 2026)

| Rank | Wallet | P&L | Win% | Markets | Avg Trade | Strategy Inference |
|------|--------|-----|------|---------|-----------|-------------------|
| 1 | **gopfan2** | $182K (recent) / $344K (all-time) | 73% | 104 | $262 | Tail-buying (<15¢ YES, >45¢ NO), extreme consistency |
| 2 | **Handsanitizer23** | $87K | 57.6% | 33 | $15,035 | Large bets, low frequency; concentrated in few markets |
| 3 | **aenews2** | $79K (recent) / $277K (all-time) | 84% | 125 | $1,779 | Highest win rate; likely multi-model ensemble + Kalman correction |
| 4 | **BeefSlayer** | $59K | 59.4% | 1247 | $37 | Very high frequency, small size; likely automated bot |
| 5 | **WeatherTraderBot** | $55K | 43.4% | 918 | $54 | Low win rate but positive P&L = asymmetric payoff (tail strategy) |
| 6 | **bama124** | $87K (all-time) | — | — | — | Unknown strategy; secondary city focus suspected |
| 7 | **automatedAItradingbot** | $65K (all-time) | — | — | — | Multi-source meteorologist bot (JMA + KMA + HKO + NOAA) |

### Key New Wallets Not Previously Tracked

**aenews2**: 84% win rate across 125 markets — the highest win rate of any weather trader. This suggests extremely conservative entry criteria (high edge threshold) with multi-model calibration. Likely uses station-specific bias correction given the consistency.

**Handsanitizer23**: Only 33 markets but $87K profit with $15K average trade size. This is a concentrated-bet player — possibly exploiting specific thin markets (secondary cities) where a single large order moves the price.

**BeefSlayer**: 1,247 markets resolved with $37 average trade. This is a high-frequency bot making many small bets. The 59.4% win rate with positive P&L suggests consistent +EV across many tiny edges.

**automatedAItradingbot**: Self-described "Meteorologist. IT engineer." Active across Seoul, Tokyo, Chicago, Dallas, Houston, Denver, Buenos Aires — secondary city focus. Uses JMA AMeDAS (Japan), KMA (Korea), HKO (Hong Kong) in addition to standard NOAA models. Asian market emphasis.

---

## 9. Resolution Source Divergence — Enhanced (Beyond Existing `resolution_div`)

The existing `resolution_div` strategy compares WU vs NWS probability distributions. New research reveals additional exploitable divergence:

### A. Celsius vs Fahrenheit Truncation Differences
Polymarket markets specify the unit (°C or °F). The same physical temperature truncates differently:
- 23.4°C → 23°C bucket
- 74.1°F → 74°F bucket (but 23.4°C = 74.12°F → rounds to 74°F)

**The conversion between units introduces systematic edge** at boundary temperatures. When a Celsius market's forecast is near X.5°C, the probability of landing in bucket X vs X+1 is approximately 50/50 (truncation), but models assuming continuous distribution may assign different probabilities.

### B. WU Finalization Delay Window
Wunderground "finalizes" data hours after the day ends. During this window:
- The METAR max temperature is known (from aviation weather)
- But WU occasionally adjusts the final reading (quality control)
- If you can predict the direction of WU adjustment (they tend to validate METAR), you can trade before finalization

### C. Station Change Detection
Polymarket occasionally changes the resolution station for a city mid-cycle (documented by polymarketweather.com). When this happens:
- All existing price signals are based on the old station's forecast
- The new station may have a different bias profile
- A bot that detects the station change first gets a 30–60 minute edge window

---

## 10. Summary: Strategy Comparison Matrix

| Strategy | Edge/Trade | Win% | Freq/Mo | Implementation Effort | Data Requirement | Competition Risk |
|----------|-----------|------|---------|----------------------|-----------------|-----------------|
| METAR Lock | 3–28% | 88% | 30–60 | Medium | METAR feed (free) | Low-Medium |
| Cross-Day NegRisk | 5–12% | 65–75% | 20–40 | High | Both days' prices + synoptic | Low |
| Station Bias Kalman | 2–8% | +3–5pp on all | 200+ | Low | Resolution outcomes + forecasts | Very Low |
| Info Latency | 5–25% | 60–70% | 8–16 | Very High | Raw GRIB + VPS infrastructure | High (shrinking) |
| NegRisk Field-Fade | 2–5% | 70–80% | 15–30 | Medium | All bucket prices per event | Medium |
| Day-of-Week/Seasonal | 1–3% | 55–62% | 40–80 | Low | Historical climatology | Low |
| Humidity/Dewpoint | 2–6% | 60–68% | 20–40 | Medium | METAR dewpoint + NBM | Low |

---

## Key References

1. **polymarketweather.com** (2026) — Primary source for bot architecture, resolution mechanics, station-specific biases, strategy documentation
2. **AFT 2025 Paper** (IMDEA Networks) — 86M bets analyzed; $29M extracted via NegRisk rebalancing; 73% of profits from multi-condition arb
3. **PolySwarm Paper** (arXiv:2604.03888) — Multi-agent LLM framework; latency arbitrage pipeline; 5-second scan loop
4. **Navnoor Bawa Substack** — NegRisk market rebalancing analysis; $29M extraction; capital efficiency advantage
5. **polyintel.io** — Live weather leaderboard with P&L, win%, trade count per wallet
6. **wethr.net** — City-by-city station guides (Chicago KMDW, NYC KNYC) with UHI quantification
7. **weatherstationadvisor.com** — Two-week home station vs Kalshi test; local bias correction methodology
8. **kalshiweatheredge.com** — METAR Lock strategy documentation (88% win rate)
9. **EdgeScouts** — Temporal structure of weather edge (72h+ vs 24–48h vs 0–12h windows)
10. **yangyuan-zhen/PolyWeather** (GitHub) — 52-city monitoring; DEB blending; TAF integration; LGBM calibrated probability
11. **alteregoeth-ai/weatherbot** (GitHub) — Open-source bot; 20 cities; ECMWF + HRRR + METAR; Kelly sizing
12. **WeatherBot.fi** — 4-model ensemble; Claude AI trade analysis; 5-layer exit system; CloudChaser $700→$85K case study
13. **ECMWF IFS documentation** — Early delivery system (SCDA) timing; 4h25min data cutoff; explains why ECMWF runs have 7h publication delay
