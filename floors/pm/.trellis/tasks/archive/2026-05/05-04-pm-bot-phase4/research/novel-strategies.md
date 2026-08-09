# Novel & Less-Explored Strategies for Polymarket Weather Markets

**Research Date**: 2026-05-04  
**Status**: Completed  
**Scope**: Strategies beyond gopfan2 tail-buying, narrow bucket NO, airport arb, sum arb, and ladder

---

## Executive Summary

The well-known strategies (ensemble-vs-market mispricing, laddering, sum arbitrage, airport coordinate arb, tail NO buying) are increasingly crowded. Multiple open-source bots (alteregoeth-ai/weatherbot, openclaw-weather, PolyWeather, WeatherBot.fi, PolymarketWeather.com) implement variants of the same core logic: pull ensemble → count members → compare to market price → trade on edge ≥ 8%. This research identifies **8 novel strategy categories** with varying edge, competition, and complexity profiles that fewer bots are currently exploiting.

---

## Strategy 1: Resolution Source Divergence (WU vs NWS vs METAR)

### Description
Polymarket resolves against **Weather Underground (WU) History tab** data. Kalshi resolves against **NWS Climate Report (CLI)**. These sources frequently disagree by 1-3°F for the same station on the same day. The discrepancies arise from:

1. **6-hour max capture**: NWS CLI incorporates 6-hour maximum temperature reports and Daily Summary Messages (DSMs) that capture short-lived temperature spikes between hourly METARs. WU does **not** use these products — it only uses standard hourly METARs and SPECI reports.
2. **DST time-window offset**: NWS uses Local Standard Time (LST) year-round. WU uses local clock time. During DST months, the 12:00–1:00 AM window belongs to different calendar days depending on the platform.
3. **Celsius round-trip errors**: 5-minute ASOS data goes through a F→C→round C→F→round F pipeline, introducing ±1°F ambiguity. The CLI uses proper 2-minute averaging; WU uses rounded METAR data.

### Key Insight
If you can model **which direction** the WU-vs-NWS discrepancy will fall on a given day, you have a systematic edge on Polymarket markets that bots using NWS-derived forecasts miss. Specifically:
- **Fast-moving cold fronts**: NWS CLI captures brief temperature extremes that WU misses → NWS reports a higher high → Polymarket (WU) may report 1-2°F lower
- **Late-night DST boundary shifts**: during summer, a temperature reading at 12:30 AM local belongs to today on NWS but yesterday on WU
- **Afternoon spikes between METAR hours**: if a spike occurs between hourly observations and isn't significant enough for a SPECI, WU misses it

### Implementation
- Build a parallel model for both WU-likely resolution and NWS-likely resolution
- When the two models predict different buckets, trade toward the WU model (since Polymarket resolves on WU)
- Most bots use NWS/ECMWF forecasts → they predict NWS-resolution outcomes → they're systematically wrong on WU-resolution days

### Assessment
| Metric | Value |
|--------|-------|
| Expected gross edge | 2-5% (1-2°F resolution difference on borderline days) |
| Competition level | Very low — most bots don't distinguish resolution sources |
| Implementation complexity | High — requires modeling both WU and NWS resolution logic |
| Capital requirements | Low — same as standard weather trading |
| Risk level | Medium — discrepancy direction must be modeled correctly |
| Best conditions | DST transition days, fast-moving fronts, cities where NWS/WU discrepancy history shows 1°F+ gaps |

### Sources
- wethr.net platform comparison guide documents NWS vs WU differences extensively
- Interactive Brokers research by Patrick Brown documents the three-source divergence (NWS Time Series, CLI, METAR)
- minuteTemp.com documents the rounding pipeline and why displayed temps differ from official records
- Iowa Environmental Mesonet (IEM) confirms no near-real-time source has correct 2-minute averaging

---

## Strategy 2: Cross-Market Temperature Correlation (High + Low Pairs)

### Description
For the same city, Polymarket often runs both **daily high** and **daily low** temperature markets. These are physically correlated: if the high is extreme, the low is more likely to be extreme too (heat wave persists, cold front drives both down). The correlation structure is:

- **Heat waves**: both high and low are elevated → low doesn't drop much overnight
- **Cold fronts**: both high and low are suppressed
- **Clear dry days**: high is elevated but low drops → negative correlation (diurnal range expands)
- **Cloudy/rainy days**: high is suppressed but low stays elevated → negative correlation (diurnal range compresses)

### Key Insight
If the market prices high and low buckets independently (which they do — they're separate neg_risk events), you can exploit the conditional dependence. Example:
- Forecast says extreme heat wave: high 95°F with 80% confidence
- The low temperature is unlikely to drop below 75°F because the heat wave persists
- Market for low < 70°F might be priced at 15¢ when conditional probability given the high is more like 5%

### Implementation
- Model joint distribution of (high, low) for each city using historical data
- When you identify edge on a high-temperature market, calculate the conditional probability shift for the low-temperature market
- Trade the low market if its price hasn't adjusted for the high's predicted extreme

### Assessment
| Metric | Value |
|--------|-------|
| Expected gross edge | 3-8% (conditional shift on correlated market) |
| Competition level | Low — no known bot explicitly models high-low joint distributions |
| Implementation complexity | Medium — requires bivariate temperature model |
| Capital requirements | Low-medium — two positions per city per day |
| Risk level | Medium — correlation isn't perfect; diurnal range can surprise |
| Best conditions | Heat waves, cold outbreaks, persistent blocking patterns |

---

## Strategy 3: Same-City Autocorrelation / Weather Persistence

### Description
Weather exhibits strong autocorrelation: if today's high is 95°F, tomorrow's high is more likely to be 93°F than 75°F. Heat waves and cold spells persist for 3-7 days. Most bots treat each day independently — they forecast tomorrow using only the latest model run, without conditioning on the already-resolved or nearly-resolved temperature of the previous day.

### Key Insight
Once today's high is known (or nearly known after 2-3pm local), you have information about tomorrow's high that the ensemble models alone don't fully capture:
- **Persistence signal**: if today exceeded the forecast by 3°F, the model may be biased cold for this regime → adjust tomorrow's forecast upward
- **Regime identification**: if we're in a blocking pattern (verified by today's extreme), the model may underweight the persistence
- **Adaptive Kalman filter**: build a station-specific bias tracker that updates with each day's resolved temperature

### Implementation
- Track forecast-vs-actual bias for each station over a rolling window
- When a station shows persistent bias in one direction (e.g., ECMWF underforecasts by 2°F during heat waves at KLGA), apply a conditional correction
- This is different from the generic ×1.15 underdispersion multiplier — it's a **regime-conditional** adjustment

### Assessment
| Metric | Value |
|--------|-------|
| Expected gross edge | 1-4% (persistence bias correction) |
| Competition level | Low-Medium — some bots (PolyWeather's DEB system) do bias correction, but most don't |
| Implementation complexity | Medium — requires tracking historical forecast errors per station per regime |
| Capital requirements | Low |
| Risk level | Low-Medium — persistence can break suddenly (front passage) |
| Best conditions | Multi-day heat waves, blocking highs, extended cold spells |

---

## Strategy 4: METAR Real-Time Observation Edge

### Description
Most bots use **forecast models** (ECMWF, GFS, HRRR) as their primary data source. Very few systematically incorporate **real-time METAR observations** from the resolution station during the same-day window. METAR observations are available roughly hourly (at the :51-:54 mark past each hour) and provide ground-truth temperature readings.

### Key Insight
For same-day markets (resolving within 6-12 hours), the most valuable information isn't the forecast — it's what the thermometer is **actually reading** right now. Specifically:
- By 10am local, you can see if the station is running ahead of or behind the forecast
- If the 10am reading is 2°F above the HRRR prediction, the afternoon high is likely to exceed the forecast
- This gives you a window of 2-6 hours where you have superior information to bots that only re-run ensemble models

### Implementation
- Poll METAR data for the resolution station every 30 minutes
- Compare observed temperature trajectory to forecast trajectory
- If observations are systematically running above/below forecast, adjust probability estimates accordingly
- Trade in the final 6-12 hour window when observation edge is highest

### Assessment
| Metric | Value |
|--------|-------|
| Expected gross edge | 3-10% (observation-vs-forecast divergence on same-day) |
| Competition level | Low — alteregoeth-ai/weatherbot fetches METAR but doesn't seem to use it for same-day probability adjustment; most bots focus on forecasts only |
| Implementation complexity | Low — METAR API is free and simple |
| Capital requirements | Low — but position must be entered same-day, limiting time |
| Risk level | Low-Medium — observations are ground truth; main risk is that the high hasn't occurred yet |
| Best conditions | Same-day markets where the morning trajectory diverges from forecast; frontal passages where the temperature trend is clear |

---

## Strategy 5: Maker-Side Weather Market Making

### Description
Polymarket weather markets charge a **5% taker fee** (peaking at $1.25 per $50 position at the 50¢ midpoint). Maker fees are **zero**, and makers receive a **25% rebate** from the taker fee pool. The weather market is the **quietest LP environment** on the platform — daily volume is thin, and two-sided quotes from a patient maker can collect a disproportionate share of the ~$5M/month general liquidity rewards pool.

### Key Insight
Weather markets have specific properties that make market-making particularly attractive:
- **Known resolution time**: markets resolve daily → no long-dated uncertainty
- **Model-driven price discovery**: you can quote both sides based on your own probability model
- **Thin existing liquidity**: most market-makers avoid weather → wider spreads → more rebate capture
- **Retail arrives in bursts**: after NHC advisories, during extreme weather events → you get taker flow

### Implementation
- Run a probability model (same as a directional bot)
- Quote both YES and NO on the 2-3 most likely buckets, 1-2¢ inside the current spread
- Refresh quotes every time the model updates (GFS/ECMWF/HRRR run completes)
- Capture the spread + maker rebate on every fill
- Requires cancel/replace cycle under 200ms to avoid adverse selection

### Assessment
| Metric | Value |
|--------|-------|
| Expected gross edge | 1-3% spread capture + 0.25-0.5% rebate (combined ~1.5-3.5%) |
| Competition level | Low — very few dedicated weather market makers |
| Implementation complexity | Medium-High — requires real-time WebSocket monitoring, fast cancel/replace, inventory management |
| Capital requirements | Medium — need inventory across multiple buckets |
| Risk level | Medium — adverse selection from faster bots, inventory risk from wrong-direction moves |
| Best conditions | High-volume cities (NYC, London, Tokyo), periods around model update times |

### Sources
- Polymarket fee documentation: weather taker fee 5%, maker fee 0%, maker rebate 25%
- PolymarketWeather.com notes that maker orders work on 1-2% edges vs 3%+ needed for takers
- After the 500ms taker delay removal and dynamic fee introduction (Feb 2026), the platform explicitly favors makers

---

## Strategy 6: Model Update Window Timing

### Description
Weather models update on fixed schedules:
- **GFS**: 00Z, 06Z, 12Z, 18Z → data available ~4-5 hours after init (e.g., 00Z available ~04-05Z)
- **ECMWF IFS HRES**: 00Z, 12Z → data available ~6-7 hours after init (via Open-Meteo, now without additional delay since Oct 2025 open-data transition)
- **HRRR**: Every hour → data available ~1.5-2 hours after init
- **Open-Meteo**: Adds ~10 minutes processing delay on top of model availability

### Key Insight
When a model run shifts the forecast meaningfully, there's a window where Polymarket prices haven't yet adjusted. The window duration depends on how many bots are watching and how fast they execute. However:
- **ECMWF 00Z run** (init 00Z, available ~06-07Z): this is the most impactful update for medium-range. Many bots poll on 30-60 minute intervals → window of 5-15 minutes before price convergence
- **HRRR hourly updates**: most bots don't track HRRR → 15-30 minute window
- **ECMWF direct API** (paid, ~€800/year) delivers data 1-2 hours before Open-Meteo makes it available → premium timing edge

### Implementation
- Subscribe to model update notifications (Open-Meteo metadata API, or direct ECMWF API)
- On model update, immediately recalculate probability distributions
- Compare to market prices and execute within the convergence window
- The key is **speed of execution**, not just accuracy of forecast

### Assessment
| Metric | Value |
|--------|-------|
| Expected gross edge | 2-5% (model update window, compressed by competition) |
| Competition level | Medium-High — most sophisticated bots already track model updates |
| Implementation complexity | Medium — requires real-time model ingestion pipeline |
| Capital requirements | Low |
| Risk level | Low — you're trading on updated information |
| Best conditions | ECMWF 00Z/12Z runs that shift forecast by 2°F+, HRRR updates during same-day markets |

### Sources
- Open-Meteo model updates documentation: explicit timing for each model
- ECMWF dissemination schedule: HRES available by ~06:55Z for 00Z run
- Pirate Weather documentation: ECMWF IFS 8-hour delay, HRRR 1.75-hour delay
- alteregoeth-ai/weatherbot: uses ECMWF via Open-Meteo, scans hourly — misses HRRR hourly updates

---

## Strategy 7: Neg_risk Bucket Sum Deviation Exploitation

### Description
Polymarket temperature markets are **neg_risk events** — mutually exclusive outcomes where exactly one bucket resolves YES. The sum of all YES prices should equal ~$1.00. The academic literature (AFT 2025 conference paper, arxiv 2508.03474) documents that this constraint is systematically violated, enabling $29M in NegRisk rebalancing profits over one year.

For temperature markets specifically:
- Retail flow concentrates on 1-2 favorite buckets → complementary buckets trade thin
- Tail buckets (extreme temperature outcomes) are often priced too cheaply (favorite-longshot bias in reverse)
- The sum of YES prices across all buckets can deviate from $1.00 by 2-5%

### Key Insight
Two distinct approaches:

**A. Underpriced sum (Sum < $1.00) — True Arbitrage**  
Buy YES on all buckets using **standard** (not NegRisk) market orders. Pay < $1.00, receive $1.00 at resolution. This is risk-free. However:
- Must use standard markets (NOT NegRisk) — NegRisk costs exactly $1.00, eliminating the profit
- Occurs most often on thin international markets with few participants
- Execution risk: must fill all buckets simultaneously

**B. Overpriced sum (Sum > $1.00) — Fade Favorites**  
When sum > $1.00, buy NO on the most overpriced buckets. NegRisk makes this capital-efficient: 1 NO on bucket A = 1 YES on every other bucket. This is NOT risk-free but has structural edge because:
- ~65% of large-field multi-outcome markets trade over-summed
- Retail systematically overprices favorites and underprices tails
- Buying NO on 3-4 overpriced favorites using NegRisk costs only 1 unit of collateral

### Implementation
- Continuously monitor sum of YES prices across all buckets in a temperature event
- When sum < 0.98 after fees: execute standard-market buy-all arbitrage
- When sum > 1.03: buy NO on the 2-3 most overpriced buckets via NegRisk
- Size with quarter-Kelly across the basket

### Assessment
| Metric | Value |
|--------|-------|
| Expected gross edge (underpriced) | 2-5% risk-free; 4-8% with fee optimization |
| Expected gross edge (overpriced) | 3-5% statistical edge (not risk-free) |
| Competition level | Medium — sum arb is well-known but execution on weather markets is less competitive than politics |
| Implementation complexity | Medium — requires monitoring all buckets simultaneously, batch execution |
| Capital requirements | Low-Medium |
| Risk level | Low (underpriced arb), Medium (overpriced fade) |
| Best conditions | Thin international markets, new market openings, post-resolution rebalancing |

### Sources
- AFT 2025 paper (arxiv 2508.03474): $39.6M total extraction, NegRisk rebalancing = $29M
- NegRisk documentation: 1 NO → 1 YES on every other bucket conversion
- Navnoor Bawa analysis: 73% of profits from NegRisk rebalancing despite 8.6% of opportunities
- PolymarketWeather.com: "sum drops below $1.00 occasionally on thin international markets"

---

## Strategy 8: Resolution Station Change Detection

### Description
Polymarket has **changed resolution stations mid-cycle** for at least one city (Paris switched from LFPG/CDG to LFPB/Le Bourget after the April 2026 sensor manipulation incident). The market description specifies the resolution station, but not all bots verify it on every trade.

### Key Insight
- Using city-center coordinates introduces 3-8°F error on 1-2°F bucket markets
- Some cities have per-market station variation (London: Heathrow EGLL or City EGLC; Tokyo: Haneda RJTT or Narita RJAA; LA: "per-market, verify")
- When Polymarket changes a station, there's a brief window where bots using the old station coordinates are systematically wrong
- Station mismatch is "the single most common cause of unexpected losses" (PolymarketWeather.com)

### Implementation
- On every trade cycle, re-read the market description to extract the resolution station
- Verify the ICAO code against your forecast coordinate mapping
- Alert on station changes — these create systematic mispricing from bots using stale coordinates
- After a station change, the new station may have different microclimate characteristics → recalculate bias corrections from scratch

### Assessment
| Metric | Value |
|--------|-------|
| Expected gross edge | 5-15% during station-change windows (3-8°F coordinate error) |
| Competition level | Very low — most bots hard-code station coordinates and update infrequently |
| Implementation complexity | Low — API call to market description + coordinate lookup |
| Capital requirements | Low |
| Risk level | Low — you're just using the correct station |
| Best conditions | After Polymarket changes a resolution station (rare but impactful) |

### Sources
- PolymarketWeather.com: "markets for the same city don't always resolve at the same station, and Polymarket has occasionally changed the resolution station mid-cycle"
- Paris CDG → Le Bourget switch documented by fibo-crypto.fr
- alteregoeth-ai/weatherbot: hard-codes station coordinates per city

---

## Strategy 9: Precipitation-Temperature Correlation

### Description
Polymarket sometimes runs precipitation markets alongside temperature markets for the same city. Rain/cloud cover directly suppresses the daily high temperature. If precipitation is forecast with high confidence, the temperature high is more likely to be below the ensemble mean.

### Key Insight
The temperature model uncertainty should **conditionally narrow** when precipitation is certain:
- Rain day → temperature distribution shifts left and compresses
- Clear sky → temperature distribution shifts right and widens
- The correlation is stronger for high temperature than low temperature

### Implementation
- Build conditional probability model: P(high in bucket | precipitation=yes) vs P(high in bucket | precipitation=no)
- When precipitation market has high-confidence resolution, adjust temperature bucket probabilities
- Trade temperature markets where the conditional shift creates edge vs unconditional model

### Assessment
| Metric | Value |
|--------|-------|
| Expected gross edge | 2-6% (conditional shift on rainy/clear days) |
| Competition level | Very low — no known bot models precip-temp correlation |
| Implementation complexity | Medium — requires joint model |
| Capital requirements | Low |
| Risk level | Low-Medium |
| Best conditions | Days with high-confidence precipitation forecast, coastal cities where marine layers suppress highs |

---

## Strategy 10: Weekend Effect / Reduced Bot Activity

### Description
Several sources suggest that bot activity may decrease on weekends, creating wider spreads and more mispricing. If fewer bots are actively updating prices on Saturdays and Sundays, the manual/retail-driven prices may lag further behind model updates.

### Key Insight
- Weekend GFS/ECMWF updates happen on the same schedule
- But human traders who manually trigger bot runs may be less active
- Server maintenance windows sometimes occur on weekends
- New market openings may happen on weekdays when Polymarket staff is available

### Implementation
- Compare weekend vs weekday spreads, edge frequency, and price convergence speed
- If weekend markets are systematically less efficient, concentrate capital deployment on weekends
- Alternatively, run market-making on weekends when spreads are widest

### Assessment
| Metric | Value |
|--------|-------|
| Expected gross edge | 1-3% (wider spreads on weekends) |
| Competition level | Low — if confirmed, weekend is structurally less competitive |
| Implementation complexity | Low — just shift trading schedule |
| Capital requirements | Low |
| Risk level | Low |
| Best conditions | Weekends, especially Sunday when markets may be thinnest |

**Note**: This strategy needs empirical validation. No definitive source confirms weekend effects in Polymarket weather.

---

## Strategy 11: New Market Opening Inefficiency

### Description
Temperature markets open 5-7 days before the target date. In the first 24-72 hours, "prices are wide and sparse, volume is very thin" (PolymarketWeather.com). New cities appear without announcement. The initial pricing is set by early participants who may use less sophisticated methods.

### Key Insight
- Best window is often 48-96 hours after launch, once initial pricing chaos has settled but before the market becomes efficient
- New city markets may have no historical calibration data → bots with per-city bias correction have no advantage
- Initial prices may be set by Polymarket's automated market-making algorithm, which doesn't use weather models → inherently inefficient

### Implementation
- Monitor Gamma API for new market creation events
- On new market detection, immediately calculate model-based probabilities
- Compare to initial prices — expect larger edges than on established markets
- Trade early but with reduced position size (thinner liquidity = more slippage)

### Assessment
| Metric | Value |
|--------|-------|
| Expected gross edge | 5-15% (early-stage mispricing) |
| Competition level | Low-Medium — some bots scan for new markets but many skip thin ones |
| Implementation complexity | Low — just watch for new markets |
| Capital requirements | Low (thin liquidity limits position size anyway) |
| Risk level | Medium — less liquidity = harder to exit, new cities may have unusual resolution rules |
| Best conditions | New city launches, first 48-96 hours |

---

## Strategy 12: EMOS/Quantile Regression Forest Calibration

### Description
The standard approach in weather bots is either: (a) count ensemble members per bucket, or (b) fit a Gaussian distribution with spread multiplier. The meteorological literature has developed significantly more sophisticated post-processing methods:

- **EMOS (Ensemble Model Output Statistics)**: fits a Gaussian where the mean is a bias-corrected weighted average of ensemble members and the variance is a linear function of ensemble variance. Published by Gneiting et al. (2005), it consistently outperforms raw ensemble counting.
- **BMA (Bayesian Model Averaging)**: combines predictive distributions from multiple models with weights proportional to each model's skill. Raftery et al. (2005) showed BMA produces much better calibration than raw ensembles.
- **QRF (Quantile Regression Forests)**: non-parametric method that can capture non-Gaussian features. Taillardat et al. showed QRF outperforms EMOS for Météo-France PEARP ensemble.

### Key Insight
Most bots use the simplest possible probability estimation (ensemble counting + ×1.15 spread multiplier). The literature shows that:
1. Raw ensembles are systematically **underdispersive** — the ×1.15 multiplier is a crude fix
2. Station-specific EMOS coefficients can reduce CRPS by 15-25% vs raw ensemble
3. The spread-skill relationship varies by station, season, and forecast horizon
4. Winter forecasts have **twice the error rate** of summer forecasts at most stations (ECMWF evaluation)

### Implementation
- Train EMOS models per station using historical forecast-vs-observation data
- Use rolling 60-day training window, updated monthly
- For multi-model ensembles, use BMA weights calibrated by Brier score per city
- Apply season-specific and horizon-specific variance adjustments

### Assessment
| Metric | Value |
|--------|-------|
| Expected gross edge | 2-8% (better calibration → more accurate probability → better bucket selection) |
| Competition level | Low — PolyWeather uses EMOS/LGBM but most bots use simple ensemble counting |
| Implementation complexity | High — requires historical data pipeline, model training, station-specific coefficients |
| Capital requirements | Low-Medium |
| Risk level | Low — better calibration reduces both systematic and random errors |
| Best conditions | Winter markets (where model errors are largest and calibration matters most), longer-horizon markets (3-5 days out where underdispersion is worst) |

### Sources
- Gneiting et al. (2005): EMOS for ensemble calibration
- Raftery et al. (2005): BMA for ensemble postprocessing  
- Veenhuis (2013): Spread calibration of ensemble MOS forecasts
- Taillardat et al.: QRF vs EMOS comparison
- ECMWF evaluation reports: winter error rates ~2x summer for 2m temperature

---

## Strategy 13: Physical Oracle Attack Monitoring

### Description
The April 2026 Paris CDG sensor manipulation incident (physical heating of Météo France station LFPG, netting ~$34K) exposed a structural vulnerability: Polymarket resolves on a single physical sensor with no redundancy.

### Key Insight
While we don't recommend executing physical oracle attacks (they're criminal), there are legitimate defensive/offensive strategies:
1. **Anomaly detection**: monitor surrounding stations for temperature divergence — if the resolution station spikes 3°C above all nearby stations, a manipulation event may be underway
2. **Fade suspicious spikes**: if you detect an anomalous reading, trade in the direction the pre-spike temperature would have resolved — the spike may be corrected before resolution, or Polymarket may switch stations (as they did for Paris)
3. **Station diversification**: maintain probability models for nearby alternate stations in case Polymarket switches resolution source

### Assessment
| Metric | Value |
|--------|-------|
| Expected gross edge | N/A (defensive strategy, occasional offensive opportunity) |
| Competition level | Very low — no known bot monitors for oracle manipulation |
| Implementation complexity | Medium — requires multi-station monitoring and anomaly detection |
| Capital requirements | Low |
| Risk level | High — if manipulation succeeds, you lose; if it's corrected, you win |
| Best conditions | Cities with single-sensor resolution, airports with accessible weather stations |

---

## Prediction Market Alpha Persistence: Academic Literature

### Key Findings

1. **Iowa Electronic Markets (Berg et al., 2008)**: Prediction markets outperform polls 74% of the time, even 100+ days before elections. However, Page & Clemen (2013) found that long-term markets exhibit systematic bias: low-likelihood events are overpriced, high-likelihood events underpriced.

2. **Time discounting bias (Page & Clemen, 2013)**: Prices of long-term prediction markets are systematically biased toward 50%. Miscalibration increases with time to expiration. The bias can be exploited: buy events priced above 60% that are far from expiration.

3. **Convergence of forecasts (arxiv 2402.16345)**: If agents with correct forecasts exist, market prices converge to true conditional expectations. The wealth-weighted average of beliefs drives convergence. This implies that as long as some bot has the correct forecast, the market will eventually converge — but the speed of convergence determines edge persistence.

4. **Polymarket-specific (Clinton & Huang, 2025)**: In the 2024 presidential election, only 67% of Polymarket markets predicted outcomes better than chance, vs 93% for PredictIt. Arbitrage opportunities peaked in the final two weeks. Price changes were weakly correlated or negatively autocorrelated — suggesting markets are NOT fully efficient.

5. **Weather market alpha persistence**: The daily resolution cycle creates rapid feedback — mispricings are corrected within 24-48 hours. However, new markets open daily, creating a continuous stream of opportunities. The edge persists because:
   - Retail participants continuously enter with inferior information
   - Model updates create temporary mispricings
   - Different resolution sources create systematic 1-2°F discrepancies
   - The market is too small for institutional market-makers to enter

6. **Edge decay rate**: Based on the observed compression of common strategies (gopfan2, sum arb), edges that are easily codified compress within 3-6 months of becoming public. Strategies that require custom data pipelines, station-specific calibration, or resolution-source expertise persist longer because they have higher barriers to entry.

---

## Priority Ranking

| Priority | Strategy | Edge | Competition | Complexity | Why |
|----------|----------|------|-------------|------------|-----|
| 1 | Resolution Source Divergence | 2-5% | Very Low | High | Structural; most bots don't distinguish WU vs NWS |
| 2 | METAR Same-Day Observation Edge | 3-10% | Low | Low | Ground truth beats forecast on same-day; easy to implement |
| 3 | Neg_risk Sum Deviation | 2-8% | Medium | Medium | Well-documented edge; weather markets less arb'd than politics |
| 4 | EMOS/QRF Calibration | 2-8% | Low | High | Better probability estimation → compounding advantage |
| 5 | Maker-Side Market Making | 1-3.5% | Low | Medium-High | Structural fee advantage; weather is quietest LP environment |
| 6 | Cross-Market High-Low Correlation | 3-8% | Low | Medium | Exploits independence assumption across correlated markets |
| 7 | Station Change Detection | 5-15% | Very Low | Low | Rare but huge edge when it occurs |
| 8 | Model Update Window Timing | 2-5% | Medium-High | Medium | Edge exists but compressed by competition |
| 9 | Same-City Autocorrelation | 1-4% | Low-Medium | Medium | Persistence bias is known but regime-conditional version isn't |
| 10 | New Market Opening Inefficiency | 5-15% | Low-Medium | Low | Large edges but thin liquidity limits position size |
| 11 | Precipitation-Temperature Correlation | 2-6% | Very Low | Medium | Novel cross-market signal |
| 12 | Weekend Effect | 1-3% | Unknown | Low | Needs empirical validation |
| 13 | Oracle Attack Monitoring | Variable | Very Low | Medium | Defensive; occasional offensive opportunity |

---

## Sources & References

### Polymarket-Specific
- PolymarketWeather.com: Comprehensive bot guides, market mechanics, resolution details
- alteregoeth-ai/weatherbot (GitHub): Open-source Python bot with METAR/ECMWF/HRRR
- openclaw-weather (GitHub): NOAA + Open-Meteo ensemble, 100% win rate claimed
- yangyuan-zhen/PolyWeather (GitHub): DEB blending, EMOS/LGBM, 52-city coverage, TAF integration
- WeatherBot.fi: Claude AI-powered, 4-model ensemble, 5-layer exit system
- solship/Polymarket-Weather-Trading-Bot: Kelly-driven NWS bot

### Resolution Mechanics
- wethr.net: Platform comparison (Kalshi=NWS CLI vs Polymarket=WU), NWS data guide
- minuteTemp.com: ASOS temperature rounding pipeline, precision validation
- IEM (Iowa Environmental Mesonet): ASOS data quality, 2-minute averaging vs 5-minute
- Interactive Brokers / Patrick Brown: NWS Time Series vs CLI vs METAR divergence

### Neg_risk & Market Structure
- Polymarket docs: Neg_risk overview, conversion mechanics, augmented neg_risk
- AFT 2025 (arxiv 2508.03474): $39.6M arbitrage extraction analysis
- Navnoor Bawa: NegRisk rebalancing breakdown ($29M from 662 opportunities)
- Polymarket fee docs: Weather = 5% taker, 0% maker, 25% rebate

### Meteorological Calibration
- Gneiting et al. (2005): EMOS — Gaussian predictive PDF with spread-skill relationship
- Raftery et al. (2005): BMA — Bayesian model averaging for ensemble calibration
- Veenhuis (2013): Spread calibration of ensemble MOS forecasts (NAEFS)
- Taillardat et al.: QRF vs EMOS for Météo-France PEARP ensemble
- ECMWF evaluation reports (2023-2025): Seasonal bias patterns, winter vs summer error rates

### Prediction Market Efficiency
- Berg, Nelson & Rietz (2008): IEM accuracy in the long run — 74% vs polls
- Page & Clemen (2013): Miscalibration with time to expiration, favorite-longshot bias
- Angelini & De Angelis (2022): In-play prediction market inefficiency
- Clinton & Huang (2025): $2.4B in 2024 election markets — only 67% Polymarket accuracy
- Beygelzimer, Langford & Pennock (2012): Kelly bettors and market convergence
