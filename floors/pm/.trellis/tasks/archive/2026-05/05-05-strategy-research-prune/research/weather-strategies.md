# Polymarket Weather (Daily High Temperature) Market Strategies

> Research compiled from public sources: blog posts, GitHub repos, Twitter/X threads,
> academic papers, and Polymarket documentation. Date: 2026-05-05.

---

## Table of Contents

1. [Market Structure Overview](#market-structure-overview)
2. [Strategy Catalogue](#strategy-catalogue)
3. [Competition Landscape](#competition-landscape)
4. [Risk Management Practices](#risk-management-practices)
5. [Known Open-Source Bots](#known-open-source-bots)
6. [Academic Research](#academic-research)
7. [Key Infrastructure Details](#key-infrastructure-details)

---

## Market Structure Overview

Polymarket runs daily high-temperature markets for ~12+ cities (NYC, London, Paris, Tokyo, Shanghai, Beijing, Hong Kong, Seoul, Taipei, Wuhan, LA, etc.). Each market is a **multi-outcome NegRisk event** with 6–15 temperature "buckets" (e.g., "<60°F", "60–61°F", "62–63°F", etc.). Exactly one bucket resolves YES at $1.00; all others resolve NO.

### Key Structural Features

| Feature | Detail |
|---------|--------|
| **Bucket width** | US cities: 1–2°F; International cities: 1°C (~1.8°F) |
| **Resolution source** | Weather Underground (Wunderground) History tab for named airport station |
| **Resolution precision** | Whole-degree **truncation** (23.4°C → 23°C bucket, NOT rounding) |
| **Resolution stations** | NYC→KLGA (LaGuardia), Paris→LFPB (Le Bourget), London→EGLC/Heathrow, Chicago→KORD (O'Hare), Dallas→KDAL (Love Field, NOT DFW), etc. |
| **UMA Oracle** | Optimistic oracle with ~2hr liveness window; MOOV2 whitelist of ~37 proposers; typical 2–4hr settlement |
| **NegRisk** | NO in bucket i = YES in all other buckets; 1 NO token convertible to 1 YES in every other bucket |
| **Heartbeat** | 10s + 5s buffer; missed heartbeat = all open orders cancelled (since Jan 2026) |
| **Order type** | All limit orders on CLOB; FOK or GTC; batch cap 15 per request |
| **Fees** | No taker fees on Global CLOB (maker rebate model) |

---

## Strategy Catalogue

### Strategy 1: Forecast-vs-Market Probability Mispricing

| Field | Value |
|-------|-------|
| **Strategy Category** | Core systematic — model probability edge |
| **Edge Source** | Market crowd prices a bucket at a probability lower than what calibrated weather models imply. Retail traders use point forecasts (or gut feel) rather than probability distributions, creating systematic mispricing across buckets. |
| **Estimated Edge** | 8–30% per trade (edge = model_prob − market_price). Average realized edge for well-calibrated bots: 10–15% before costs. |
| **Competition Level** | Medium-High on major cities (NYC, London, Tokyo); Low-Medium on secondary cities (Buenos Aires, Cape Town, Atlanta) |
| **Data Requirements** | Multi-model ensemble forecasts (ECMWF 51 members, GFS/GEFS 31 members, UKMO, HRRR for US); Polymarket Gamma API for market discovery; CLOB API/WebSocket for live prices; Historical station observations for calibration |
| **Implementation Complexity** | Medium — requires forecast ingestion, probability distribution construction, edge calculation, and Kelly sizing |
| **Source Links** | https://polymarketweather.com/blog/how-to-trade-polymarket-weather-markets · https://polymarketweather.com/ · https://github.com/alteregoeth-ai/weatherbot |

**Mechanism:**

1. Fetch ensemble forecasts from Open-Meteo (free) for the exact airport ICAO station.
2. Convert ensemble output to bucket probability distribution:
   - **Ensemble counting**: Fraction of members landing in each bucket (raw, underdispersive)
   - **Gaussian CDF**: Fit Normal(μ=ensemble_mean, σ=calibrated_by_lead_time) then integrate CDF over bucket edges
   - **EMOS/BMA**: Bayesian Model Averaging or Ensemble Model Output Statistics for 20–40% better calibration (requires 30–90 days training data per station)
   - **Student-t**: ν=4 (fat tails) for better tail bucket estimation
3. Apply spread multiplier (×1.15 typical) to raw ensemble counts for underdispersion correction.
4. Compare each bucket's model_prob vs market_yes_price.
5. Trade when edge ≥ threshold (8% is common; 3–5% for lower-threshold bots).
6. Size with fractional Kelly (15–25% of full Kelly).

**Probability Estimation Methods (ranked by sophistication):**

| Method | Accuracy | Complexity | Data Needed |
|--------|----------|------------|-------------|
| Raw ensemble counting | Low (underdispersive tails) | Low | Ensemble API |
| Ensemble + spread multiplier (×1.15) | Medium | Low | Ensemble API |
| Gaussian CDF with dynamic σ | Medium-High | Medium | Ensemble + historical σ per station/season |
| BMA / EMOS | High (+20–40% Brier skill) | High | 30–90 days station history |
| LGBM/ML calibration | Highest | Highest | 90+ days features + outcomes |

---

### Strategy 2: Tail-Buying / Longshot Mispricing (gopfan2 Strategy)

| Field | Value |
|-------|-------|
| **Strategy Category** | Systematic rule-based tail exploitation |
| **Edge Source** | Reverse favorite-longshot bias. Retail traders concentrate probability mass on the modal (most likely) bucket and systematically underprice tail buckets. Buckets trading at <15¢ often have true probability 18–25%. Similarly, NO on buckets >45¢ where true probability is <55%. |
| **Estimated Edge** | 3–15% per position (small per-trade, but compounds over thousands of trades) |
| **Competition Level** | Medium — simple to implement but requires extreme volume; some tail edges are being arb'd away |
| **Data Requirements** | Minimal — just market prices. No forecast model strictly required (though combining with forecasts improves edge) |
| **Implementation Complexity** | Easy — buy YES below $0.15, buy NO above $0.45, $1 per position, repeat |
| **Source Links** | https://polymarketweather.com/blog/polymarket-weather-leaderboard · https://predictionmarketspicks.com/articles/polymarket-whale-playbooks |

**gopfan2 documented rules:**
- Buy YES if price < $0.15
- Buy NO if price > $0.45
- Risk ~$1 per position
- Execute 10,000+ times
- Result: $343K+ cumulative profit, ~73% win rate

**Why it works:** Temperature is a continuous variable. The market creates discrete buckets but the true probability distribution assigns non-zero probability to many buckets. Retail traders anchor on the single most-likely bucket and underweight adjacent and tail outcomes. Over thousands of trades, the 3–5% per-bucket underpricing in the tails compounds.

**Barbell variant (Hans323 / ColdMath):**
- Concentrate capital on 5–15¢ tail buckets with high conviction (model says 40–50%, market says 8%)
- Hans323 put $92,632 on a London temp bucket at 8¢ that resolved YES → $1.11M single-trade profit
- Requires forecast model validation for tail bucket identification

---

### Strategy 3: Model-Update Latency Arbitrage

| Field | Value |
|-------|-------|
| **Strategy Category** | Latency/information-speed edge |
| **Edge Source** | New NWP model runs are published at fixed times (ECMWF 12 UTC run available ~18:00–18:30 UTC; GFS updates 4× daily; HRRR hourly). Polymarket prices don't adjust instantly. In the minutes after a significant model shift, prices lag the new information. |
| **Estimated Edge** | 5–40% per trade (higher on large forecast shifts; lower on small updates) |
| **Competition Level** | High — multiple bots compete for the same window; edge shrinks as more participants automate |
| **Data Requirements** | Direct access to model output as soon as published (ECMWF via Open-Meteo or direct API; GFS via NOMADS); WebSocket subscription to Polymarket CLOB for real-time price monitoring; pre-staged orders |
| **Implementation Complexity** | Hard — requires sub-minute latency pipeline, real-time parsing, and pre-positioned execution |
| **Source Links** | https://polymarketweather.com/blog/polymarket-weather-leaderboard (Hans323 profile) · https://polymarketweather.com/blog/polymarket-weather-bot |

**Model release schedule (key windows):**

| Model | Update Frequency | Availability Latency | Significance |
|-------|-----------------|---------------------|--------------|
| ECMWF IFS | 2× daily (00/12 UTC) | ~6h after cycle start | Most impactful for medium-range; best global accuracy |
| GFS/GEFS | 4× daily (00/06/12/18 UTC) | ~3.5h after cycle start | Major US-city driver |
| HRRR | Hourly | ~1h | Dominant inside 18-hour window for US cities |
| NAM | 4× daily | ~2h | Secondary US model |
| UKMO | 2× daily | ~4h | Important for London markets |

**Hans323 approach (documented):** Semi-manual monitoring of key model releases, with pre-staged orders ready to execute immediately after significant forecast shifts. Focused on London and Paris markets where ECMWF shifts correlate strongly with price moves.

**Advanced variant:** Direct ECMWF API access (paid) can provide model output 30–60 minutes before Open-Meteo ingests and publishes it, creating a genuine latency advantage.

---

### Strategy 4: ΣYES < $1.00 Structural Arbitrage (NegRisk)

| Field | Value |
|-------|-------|
| **Strategy Category** | Risk-free structural arbitrage |
| **Edge Source** | In NegRisk markets, if sum of all YES prices < $1.00, buying all YES buckets via standard (non-NegRisk) orders guarantees $1.00 payout at cost < $1.00. This is pure risk-free profit. Appears most often on thin international markets with low liquidity. |
| **Estimated Edge** | 2–5% per opportunity (small but risk-free) |
| **Competition Level** | High on major cities (arb'd away quickly); Low on thin/illiquid markets |
| **Data Requirements** | Real-time price feed for all buckets in an event; fast execution for batch orders; fee/slippage estimates |
| **Implementation Complexity** | Medium — requires batch execution, fee/slippage validation, and simultaneous fill across all buckets |
| **Source Links** | https://pkg.go.dev/github.com/ivanzzeth/polymarket-go-gamma-client/examples/find-negrisk-opportunities · https://termo.ai/skills/polymarket-negrisk-arb · https://polymarketweather.com/blog/polymarket-weather-markets-explained |

**Critical implementation detail:** For ΣYES < $1.00 (underpriced), use STANDARD market orders, NOT NegRisk. NegRisk costs exactly $1.00 in collateral regardless, eliminating the profit. Standard orders let you pay the actual sum (e.g., $0.95) and collect $1.00 at resolution.

| Sum State | Correct Approach | Profit |
|-----------|-----------------|--------|
| ΣYES < $1.00 | Buy ALL YES via standard orders | $1.00 − ΣYES (risk-free) |
| ΣYES > $1.00 | Buy ALL via NegRisk ($1.00 collateral) | $0 (capital efficiency only, NOT profit) |

**Filters needed:**
- Min profit after fees (3% floor typical)
- Liquidity check per bucket (skip if volume < $100 24h)
- Slippage estimate (skip if spread > $0.03)
- Fee check (skip if any bucket has taker fees)
- Max outcomes: 3–15 (skip binary; skip >15 as too illiquid)
- Batch execution for simultaneous fills

**Frequency:** Rare on actively-traded US/EU markets. Appears more on thin Asian/international markets at off-peak hours.

---

### Strategy 5: Settlement Source Exploitation (Airport vs. City Center)

| Field | Value |
|-------|-------|
| **Strategy Category** | Systematic resolution-station bias |
| **Edge Source** | Markets resolve on airport station readings, but most retail traders use city-center weather apps. The difference between airport and city-center temperatures is 3–8°F, systematic, and varies by synoptic pattern. NYC: LaGuardia (waterfront, cooler) vs. Central Park (inland). Dallas: Love Field vs. DFW vs. city center. |
| **Estimated Edge** | 5–20% on affected buckets (higher when synoptic pattern maximizes airport-city differential) |
| **Competition Level** | Medium — known edge but many retail traders still ignore it |
| **Data Requirements** | Historical airport vs. city-center temperature differential data per station; forecast data queried at airport ICAO coordinates (not city center) |
| **Implementation Complexity** | Easy-Medium — requires mapping each market to its exact resolution station and fetching forecasts at airport coordinates |
| **Source Links** | https://polymarketweather.com/blog/polymarket-weather-markets-explained · https://github.com/alteregoeth-ai/weatherbot · https://www.wethr.net/market-resolution |

**Key station differentials (documented):**

| City | Resolution Station | Common App Location | Typical ΔT |
|------|-------------------|-------------------|------------|
| NYC | KLGA (LaGuardia) | Manhattan/Central Park | 3–6°F (waterfront cooler) |
| Chicago | KORD (O'Hare) | Downtown Loop | 2–4°F |
| Dallas | KDAL (Love Field) | DFW / city center | 3–8°F (different airport!) |
| Paris | LFPB (Le Bourget) | Montsouris garden | 2–5°F (north of city) |
| London | EGLC (London City) / Heathrow | City center | 2–4°F |

**Resolution source quirks (Wunderground vs. NWS CLI):**
- Wunderground uses only hourly METARs + SPECIs (no 6-hour maxima, no DSMs)
- This means short-lived temperature spikes between observations may NOT be captured
- Result: NWS CLI can report 1°F+ higher than Wunderground for the same station/day
- Since Polymarket uses Wunderground, model the WU reading, not the NWS reading

---

### Strategy 6: METAR Nowcasting / Intraday Convergence

| Field | Value |
|-------|-------|
| **Strategy Category** | Intraday observational edge |
| **Edge Source** | By 9–10 AM local time, the day's temperature trajectory is largely determined (meteorological momentum). Real-time METAR observations from the resolution airport station show the actual temperature path. Buying YES on the converging bucket when price still reflects pre-dawn uncertainty captures the convergence edge. |
| **Estimated Edge** | 15–40% per trade (high conviction, short time to resolution) |
| **Competition Level** | Medium-High — multiple bots scan METAR; advantage goes to lowest latency |
| **Data Requirements** | Real-time METAR feed from aviationweather.gov (free, 30–60 min updates); TAF (Terminal Aerodrome Forecast) for next-24h guidance; airport station coordinates |
| **Implementation Complexity** | Medium — requires METAR parsing, temperature tracking against bucket boundaries, and fast execution |
| **Source Links** | https://termo.ai/skills/polymarket-weather-high-temp-sniper · https://github.com/alteregoeth-ai/weatherbot · https://github.com/yangyuan-zhen/PolyWeather |

**Mechanism (Termo Sniper logic):**
1. Every 5 minutes during 9:00–9:55 AM local time: scan markets where price > $0.60
2. If current METAR observation trajectory confirms the bucket: buy YES (≤$1)
3. At 10:00 AM: fallback — buy highest-volume unowned market
4. Max 1 share per market per day, no duplicate positions

**METAR observation weight (AadiXD200 bot):**
- Observation weight rises from 0% at midnight to 80% by market close
- Blended into ensemble forecast using time-weighted regime
- As actual observations accumulate, forecast uncertainty collapses → high-conviction trades

**TAF integration (PolyWeather):**
- TAF provides Terminal Aerodrome Forecast with timing markers near expected peak window
- Used as airport-side confirmation layer, not primary temperature model
- Particularly valuable in final 24 hours

---

### Strategy 7: Ensemble Spread / Model Disagreement Exploitation

| Field | Value |
|-------|-------|
| **Strategy Category** | Uncertainty-structure edge |
| **Edge Source** | When models disagree significantly (wide ensemble spread), the market often prices the modal bucket too high and tail buckets too low. The wider the spread, the more probability mass should be distributed to tails. Conversely, when models tightly agree (std < 0.8°C), NO bets on unlikely buckets are high-confidence. |
| **Estimated Edge** | 5–20% (higher during high-spread events: frontal passages, convective days, regime changes) |
| **Competition Level** | Medium — requires understanding of ensemble structure |
| **Data Requirements** | All ensemble member outputs (not just mean); model spread statistics; per-model bias corrections |
| **Implementation Complexity** | Medium — ensemble counting + spread analysis + adaptive σ |
| **Source Links** | https://github.com/AadiXD200/polymarket-weather-bot · https://polymarketweather.com/blog/polymarket-weather-bot |

**Key implementation rules (from AadiXD200):**
- Skip NO bets when ensemble std < 0.8°C (models tightly agree → temperature is likely heading to specific bucket → bad for NO)
- NO entry price gate: $0.20–$0.75 (empirically optimal range)
- Adaptive min edge: scales with lead time (wider at longer horizons)
- Model outlier detection: down-weight models >1.5σ from ensemble mean by 50%

**Per-model known biases (from NOAA/NWS verification):**
- GFS: slight cold bias at temperatures below 25°C; warm bias above 30°C
- HRRR: cool-to-warm bias transition as forecast temperatures warm
- NAM: higher frequency bias for precipitation, which affects convective temperature days
- ECMWF: generally best calibrated but still underdispersive in tails

---

### Strategy 8: Cross-Platform Arbitrage (Polymarket vs. Kalshi)

| Field | Value |
|-------|-------|
| **Strategy Category** | Cross-platform structural arb |
| **Edge Source** | Kalshi resolves on NWS CLI (6-hour maxima included) while Polymarket resolves on Wunderground (hourly METAR only). Same city, same day can settle at different temperatures (1°F+ difference). Additionally, Kalshi uses threshold binary markets while Polymarket uses bucket markets, creating different pricing dynamics. |
| **Estimated Edge** | 2–10% per opportunity (residual risk on KLGA vs. KNYC differential) |
| **Competition Level** | Low — jurisdictional barriers (US residents → Kalshi only; international → Polymarket only) limit who can execute |
| **Data Requirements** | Separate forecasts for each platform's resolution station; capital on both platforms simultaneously; NWS CLI vs. WU historical differential data |
| **Implementation Complexity** | Hard — requires dual-platform accounts, dual API integration, and jurisdictional compliance |
| **Source Links** | https://polymarketweather.com/blog/kalshi-vs-polymarket-weather · https://github.com/suislanchez/polymarket-kalshi-weather-bot · https://www.wethr.net/market-resolution |

**Key differences:**

| Dimension | Polymarket | Kalshi |
|-----------|-----------|--------|
| Market structure | Multi-bucket NegRisk | Threshold binary |
| Resolution source | Wunderground (METAR hourly) | NWS CLI (6h maxima) |
| Time standard | Local clock time (DST-aware) | Local Standard Time (year-round) |
| Trading day | 12:00 AM – 11:59 PM always | Shifts during DST (1:00 AM – 12:59 AM EST) |
| Fees | No taker fees | Commission per trade |
| Jurisdiction | International (not US/EU) | US only (CFTC regulated) |

**DST edge:** During DST months, Kalshi's "trading day" shifts by 1 hour relative to local clock time. The NWS CLI 24-hour high may capture a different peak hour window than Wunderground's midnight-to-midnight clock. This creates systematic 1–2°F differences during spring/fall transition periods.

---

### Strategy 9: Whole-Degree Truncation Boundary Exploitation

| Field | Value |
|-------|-------|
| **Strategy Category** | Resolution-rules edge |
| **Edge Source** | Polymarket resolves using whole-degree **truncation** (23.4°C → 23°C bucket). Most models and probability calculations assume continuous distributions or rounding. Near bucket boundaries (e.g., 22.9°C vs. 23.0°C), the truncation rule shifts probability mass systematically downward. Bots that model truncation correctly make different trades near boundaries. |
| **Estimated Edge** | 1–5% per trade (small but systematic; concentrates near boundary cases) |
| **Competition Level** | Low-Medium — many bots ignore this subtlety |
| **Data Requirements** | Correct integration of CDF with floor/truncation semantics rather than rounding; sub-degree forecast precision |
| **Implementation Complexity** | Easy (mathematical fix) — use floor() not round() in probability calculation |
| **Source Links** | https://polymarketweather.com/blog/polymarket-weather-markets-explained |

**Impact:** At a station where the high is equally likely to be 64.4°F or 64.6°F (symmetric around 64.5°F):
- **Rounding assumption:** P(64°F bucket) ≈ P(65°F bucket) ≈ 50%
- **Truncation reality:** P(64°F bucket) includes all readings 64.0–64.99°F; P(65°F bucket) includes 65.0–65.99°F. For a distribution centered at 64.5°F, P(64°F) ≈ 55%, P(65°F) ≈ 45%
- Over thousands of trades near boundaries, this 5% shift accumulates

---

### Strategy 10: Laddering / NegRisk Capital Efficiency

| Field | Value |
|-------|-------|
| **Strategy Category** | Structural capital efficiency |
| **Edge Source** | NegRisk architecture allows buying YES in multiple buckets with only $1 total collateral (since only one can win). "Laddering" across 3–4 most probable buckets diversifies risk while maintaining EV. Standard (non-NegRisk) buying all buckets requires collateral = sum of prices. |
| **Estimated Edge** | Not a direct probability edge — a capital efficiency edge. Enables deploying 2–5% more capital per unit of risk. |
| **Competition Level** | Low — structural feature, not competitive |
| **Data Requirements** | NegRisk flag on market; bucket probability distribution |
| **Implementation Complexity** | Easy — buy YES across top 3–4 buckets via NegRisk orders |
| **Source Links** | https://docs.polymarket.com/developers/neg-risk/overview · https://polymarketweather.com/blog/polymarket-weather-markets-explained |

**Example:** If top 3 buckets have probabilities 35%, 30%, 20%:
- Standard: buying all 3 YES costs $0.35 + $0.30 + $0.20 = $0.85, payout $1.00
- NegRisk: collateral = $1.00, payout = $1.00 (but you risk only $1.00 total, not $0.85)
- Advantage: when ΣYES > $1.00 (overpriced markets), NegRisk saves the excess

---

## Competition Landscape

### Known Active Bots / Traders

| Entity | Type | Strategy | Profit (Weather) | Notes |
|--------|------|----------|-------------------|-------|
| **gopfan2** | Algorithmic wallet | Tail-buying (<15¢ YES, >45¢ NO) | $343K+ | ~10K+ positions, 73% win rate, $1/position |
| **aenews2** | Algorithmic wallet | Unknown (likely model-driven) | $277K+ | 84% win rate, ~$1,779 avg trade |
| **ColdMath** | Algorithmic wallet | Barbell: tails + central | $120K+ | $50–$150/bet, secondary cities (BA, CPT, DAL, ATL) |
| **gopfan** | Algorithmic wallet | Similar to gopfan2 | $118K+ | Possibly related wallet |
| **Hans323** | Semi-manual | Latency arb on model releases | $81K+ | 23yo German law student; London/Paris focus |
| **Handsanitizer23** | Unknown | Unknown | $71K+ | $15,035 avg trade, 57.6% win rate |
| **automatedAItradingbot** | Bot | Likely Claude/GPT-assisted | $65K+ | — |
| **Jua** | Swiss AI startup | Proprietary AI weather model | Undisclosed | CEO confirmed trading; "liquidity too low for well-sized fund" |
| **WeatherCaster** (polymarketweather.com) | Commercial bot | 4-model ensemble (ECMWF/GEFS/UKMO/NWS) | Undisclosed | 8% edge threshold, 0.25× Kelly, circuit breaker |
| **WeatherBot** (weatherbot.fi) | Commercial bot | 4-model + Claude AI + copy-trading | Undisclosed | WebSocket streaming, 67+ cities, 5-layer exit |
| **IAMxBOTx** | Commercial bot | 4-model + crypto latency arb | Undisclosed | 113 ensemble members, 583+ markets, MeteoFrance added |
| **PolyWeather** (yangyuan-zhen) | Open-source | DEB+LGBM+METAR+TAF, 52 cities | — | Most comprehensive Asian market coverage |
| **openclaw-weather** | Open-source | NOAA+Open-Meteo+Kelly | — | React dashboard, 30-min scan |
| **alteregoeth-ai/weatherbot** | Open-source | ECMWF+HRRR+METAR+Kelly+stops | — | 20 cities, self-calibrating |
| **solship/bot** | Open-source | NWS+Kelly simulation | — | Paper trading mode, paid edition exists |
| **AadiXD200/bot** | Open-source | 5-model+Student-t+nowcasting | — | Most sophisticated open-source pipeline |
| **suislanchez/bot** | Open-source | GFS+Kalshi+Polymarket+BTC | $1.8K reported | Multi-strategy, cross-platform |

### Competition Saturation Assessment (2026)

| City Tier | Competition | Edge Availability | Notes |
|-----------|-------------|-------------------|-------|
| **Tier 1** (NYC, London, Tokyo) | High | Low-Medium | Most bots active here; spreads tight (1–3¢); edge windows close in seconds |
| **Tier 2** (Paris, Chicago, LA, Shanghai, Seoul) | Medium | Medium | Good volume; some model-update lag opportunities |
| **Tier 3** (Buenos Aires, Cape Town, Atlanta, Dallas) | Low-Medium | Medium-High | Lower bot saturation; wider spreads (5–10¢); ColdMath specialized here |
| **Tier 4** (Wellington, Taipei, Wuhan, Istanbul) | Low | High | Thin markets; ΣYES < $1.00 arb appears; but low liquidity limits position size |

**Trend:** "Weather markets were significantly less competitive in 2024 than they are in 2026. New entrants today face better-calibrated competition on the most liquid markets. The edge is still there — the leaderboard keeps growing — but the lowest-hanging fruit is more crowded than it was 18 months ago." — polymarketweather.com

**Liquidity ceiling:** Jua CEO estimates practical capacity limit of $500K–$2M annual profit before liquidity constraints bind, even with institutionally superior forecasts.

---

## Risk Management Practices

### Position Sizing: Kelly Criterion Adaptation

| Parameter | Typical Range | Source |
|-----------|---------------|--------|
| Full Kelly fraction | 0.15–0.25 (15–25% of theoretical) | All production bots |
| Hard cap per trade | $15–$100 | Varies by bankroll |
| Max % of bankroll per trade | 5% | WeatherCaster |
| Max total deployed fraction | 40% | AadiXD200 bot |
| Max open positions | 20 concurrent | WeatherCaster |

**Kelly formula for YES position:** f = (q − p) / (1 − p) where q = model probability, p = market price. Then multiply by Kelly fraction (0.15–0.25) and apply hard dollar cap.

**Kelly formula for NO position:** f = (p − q) / p where p = market price, q = model probability.

### Risk Controls (Consensus Across Bots)

| Control | Typical Setting | Purpose |
|---------|----------------|---------|
| Circuit breaker | Halt if daily P&L < −10% | Prevent cascading losses |
| Slippage filter | Skip if spread > $0.03 | Avoid paying away edge |
| Min edge threshold | 8% (3–5% for aggressive bots) | Buffer against model error |
| Min z-score | 1.5 | Statistical significance gate |
| Expiry filter | Skip if resolves < 2h out | Avoid high-variance last-minute trades |
| Order book depth | Skip if top-5 depth < $150 | Avoid moving market |
| Max total exposure | $200–$500 | Prevent overconcentration |
| Account floor | $100 (auto-pause) | Prevent total drawdown |
| Min hours to resolution | 2–6h | Avoid overnight gap risk |

### Exit Systems (5-Layer Model from WeatherBot/IAMxBOTx)

| Layer | Trigger | Action |
|-------|---------|--------|
| Profit target | 50–60% of edge captured | Close position |
| Edge convergence | Edge < 2% | Close position |
| Trailing stop | 40% pullback from peak | Close position |
| Stop loss | −15% to −25% from entry | Close position |
| Time decay | 2h before resolution | Close position |

### Heartbeat Management

Since January 2026, Polymarket requires heartbeat messages every 10 seconds (with 5-second buffer = 15-second window). Missed heartbeat → ALL open orders cancelled.

**Implementation pattern:**
```python
heartbeat_id = ""
while True:
    resp = client.post_heartbeat(heartbeat_id)
    heartbeat_id = resp["heartbeat_id"]
    time.sleep(5)  # send every 5s for safety margin
```

**Implications for strategies:**
- Any bot with resting limit orders MUST maintain heartbeat loop
- Bot crashes or network interruptions = automatic position flattening
- This is a safety feature but also an operational constraint
- Heartbeat_id must chain correctly (use previous response ID in next request)

---

## Known Open-Source Bots

| Repository | Language | Stars | Key Features |
|------------|----------|-------|--------------|
| [alteregoeth-ai/weatherbot](https://github.com/alteregoeth-ai/weatherbot) | Python | — | 20 cities, ECMWF+HRRR+METAR, Kelly, stops, self-calibration |
| [yangyuan-zhen/PolyWeather](https://github.com/yangyuan-zhen/PolyWeather) | Python | 52 | DEB+EMOS+LGBM, 52 cities, METAR+TAF, Telegram bot, most comprehensive |
| [AadiXD200/polymarket-weather-bot](https://github.com/AadiXD200/polymarket-weather-bot) | Python | — | 5 NWP models, Student-t distribution, nowcasting, bias correction |
| [lamenting-hawthorn/openclaw-weather](https://github.com/lamenting-hawthorn/openclaw-weather) | Python | — | NOAA+NWS+Open-Meteo ensemble, Kelly, React dashboard, Telegram |
| [solship/polymarket-trading-bot](https://github.com/solship/polymarket-trading-bot) | TypeScript | — | NWS+Kelly, simulation mode, paid edition |
| [suislanchez/polymarket-kalshi-weather-bot](https://github.com/suislanchez/polymarket-kalshi-weather-bot) | TypeScript | — | Cross-platform (Kalshi+Polymarket), GFS ensemble, BTC microstructure |
| [guzus/dr-manhattan](https://github.com/guzus/dr-manhattan/issues/45) | — | — | Weather bot strategy (issue #45 documents $204→$24K run) |

---

## Academic Research

### Directly Relevant

1. **"Unravelling the Probabilistic Forest: Arbitrage in Prediction Markets"** (Saguillo et al., 2025, arXiv:2508.03474)
   - First large-scale arbitrage analysis on Polymarket
   - Identifies Market Rebalancing Arb (intra-market, ΣYES ≠ 1) and Combinatorial Arb (inter-market)
   - Found ~$40M profit extracted across both types during measurement period
   - 662 of 1,578 NegRisk markets had at least one arb opportunity
   - Sports and weather categories show different arb profiles

2. **"Arbitrage Trade in Prediction Markets"** (Kildal et al., 2016)
   - Cross-border prediction market arbitrage (Intrade vs. Ipredict, 2008 election)
   - Found avg 3–4% discrepancy between platforms; typically negated by transaction costs
   - Relevant to Polymarket-Kalshi cross-platform strategies

3. **"Turning the Heat on Financial Decisions"** (Costa Sperb et al., 2022, EJOR)
   - Higher temperatures → lower decision quality in prediction markets
   - Temperature-induced cognitive errors affect logic-based traders most
   - Suggests systematic edge exists when trading against heat-affected retail participants

### Meteorological Verification

4. **"Verification of GFS, NAM, and HRRR Near-Surface Forecasts"** (Gaudet, 2024, Weather & Forecasting)
   - GFS: slight cold bias <25°C, warm bias >30°C
   - HRRR: cool-to-warm bias transition
   - All models: overforecast wind speeds ≥18 m/s
   - Provides per-model bias corrections for temperature forecast calibration

---

## Key Infrastructure Details

### Free Data Sources

| Source | Data | Auth | Update Frequency |
|--------|------|------|-----------------|
| Open-Meteo | GFS, ECMWF, ICON, GEM, Météo-France; ensemble (31–51 members) | None | ~3.5h after model cycle |
| NOAA/NWS API | US station forecasts | None | Hourly |
| Aviation Weather (METAR) | Real-time airport observations | None | 30–60 min |
| Aviation Weather (TAF) | 24h airport forecasts | None | 6h |
| Polymarket Gamma API | Market metadata, condition IDs, token IDs | None | Real-time |
| Polymarket CLOB API | Prices, order book, order execution | L1/L2 auth | Real-time |

### Paid Data Sources

| Source | Data | Cost | Advantage |
|--------|------|------|-----------|
| Direct ECMWF API | Raw model output before Open-Meteo | Paid | 30–60 min latency advantage |
| Visual Crossing | Historical station temperatures | Free tier | Resolution verification |
| Jua (proprietary) | AI weather model | Not public | Potentially superior forecasts |

### Resolution Station Reference

| City | Station | ICAO | Wunderground URL Pattern |
|------|---------|------|--------------------------|
| NYC | LaGuardia | KLGA | wunderground.com/history/daily/us/ny/new-york-city/KLGA |
| Chicago | O'Hare | KORD | wunderground.com/history/daily/us/il/chicago/KORD |
| Dallas | Love Field | KDAL | wunderground.com/history/daily/us/tx/dallas/KDAL |
| LA | LAX/Burbank | KLAX/KBUR | Verify per market |
| London | London City | EGLC | wunderground.com/history/daily/gb/london/EGLC |
| Paris | Le Bourget | LFPB | wunderground.com/history/daily/fr/paris/LFPB |
| Tokyo | Haneda/Narita | RJTT/RJAA | Verify per market |
| Hong Kong | HK Intl | VHHH | wunderground.com/history/daily/hk/chek-lap-kok/VHHH |
| Seoul | Incheon | RKSI | Verify per market |
| Shanghai | Pudong | ZSPD | Verify per market |

---

## Summary: Edge Hierarchy

| Rank | Strategy | Edge Range | Competition | Complexity | Capital Needed |
|------|----------|------------|-------------|------------|---------------|
| 1 | Forecast-vs-Market Mispricing | 8–30% | Med-High | Medium | $1K–$50K |
| 2 | Model-Update Latency Arb | 5–40% | High | Hard | $5K+ |
| 3 | Tail-Buying (gopfan2) | 3–15% | Medium | Easy | $500+ |
| 4 | METAR Nowcasting | 15–40% | Med-High | Medium | $1K+ |
| 5 | Airport vs. City Station Bias | 5–20% | Medium | Easy-Med | $1K+ |
| 6 | ΣYES < $1.00 Arb | 2–5% | High (major), Low (thin) | Medium | $5K+ |
| 7 | Ensemble Spread Exploitation | 5–20% | Medium | Medium | $1K+ |
| 8 | Cross-Platform (Kalshi) Arb | 2–10% | Low | Hard | $10K+ |
| 9 | Truncation Boundary Edge | 1–5% | Low-Med | Easy | $500+ |
| 10 | NegRisk Laddering | Capital efficiency | Low | Easy | $1K+ |

**Overall assessment:** The most robust edge comes from combining Strategies 1+5+6+9 (model probability + station bias + truncation + nowcasting) into a unified pipeline. The gopfan2 tail-buying approach proves that even without a sophisticated model, systematic rule-based trading at scale generates substantial returns. The competition is increasing but markets remain inefficient enough for well-calibrated participants to extract 6-figure annual profits.
