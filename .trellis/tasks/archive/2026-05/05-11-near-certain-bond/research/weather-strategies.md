# Polymarket Weather Market Research — Near-Certain Bond + Staged Entry

## 1. Near-Certain Bond Strategy

**Core Idea**: Buy YES on buckets priced 95-99¢ where model confirms >99% probability. Collect 1-5¢ per trade as "daily yield". Compound across many markets.

**Why it works**:
- Market often slightly underprices near-certain outcomes (crowd overvalues uncertainty)
- Model confirmation (GFS/ECMWF agreement at 24h out) gives >99% confidence
- Multiple cities = multiple daily "bond coupons"
- 5 markets × 3¢ profit × $100 each = $15/day → $450/month

**Risk**: Extreme weather event shifts temperature out of bucket. Rare but devastating at scale.

**Validation**: Multiple high-PNL wallets (7k+ trades, stable upward curve) use this grind approach. Some report $1450+/day.

## 2. Staged Entry Timing

**Core Idea**: Don't enter all at once. Build position as forecast converges.

| Stage | Timing | Position | Rationale |
|-------|--------|----------|-----------|
| 1 | 48-24h out | 30% | Early, high uncertainty, best prices |
| 2 | 24-8h out | 60% | Forecast converging, moderate prices |
| 3 | 8-0h out | 100% | Forecast locked, prices reflect consensus |

**Implementation**: Track `hours_to_resolution` for each event. Adjust position size multiplier based on stage.

**Edge**: Early entry catches stale liquidity. Late entry has higher confidence. Blending reduces timing risk.

## 3. Total Cost Constraint for Laddering

**Core Idea**: When laddering 3-5 adjacent buckets, total cost must be <80-90¢. This guarantees positive ROI if any bucket hits (pays $1).

**Example**: Forecast 31°C
- Buy 30°C (27¢) + 31°C (35¢) + 32°C (17¢) = 79¢ total
- Hit any one → $1 payout → net 26.5% ROI
- Miss all → lose 79¢

**Key**: Select buckets where sum of YES prices < 0.90. Current `LadderingStrategy` doesn't enforce this constraint.

## 4. Resolution Source Matching

**Polymarket rules**: Temperature markets resolve based on specific airport weather stations, NOT city centers.

**Examples**:
- NYC → Central Park (not JFK/LGA)
- Seoul → Incheon RKSI
- Paris → CDG (not Orly)
- London → Heathrow EGLL

**Impact**: City center vs airport can differ 1-3°C. Must match forecast data to correct station.

**Implementation**: Parse resolution source from market description. Map ICAO codes to cities. Use station-specific bias correction (already have `station_bias.py`).

## 5. City Variance Filtering

**Stable cities** (preferred for laddering): London, NYC, Miami, Seoul, LA, Paris
**High variance** (avoid during transition seasons): Chicago (spring), Shanghai (certain periods)

**Implementation**: Track historical forecast error per city. Filter markets by variance score.

## Sources

- PolymarketWeather.com blog (multiple articles)
- Polymarket resolution docs
- X traders: gopfan2, neobrother, automatedAItradingbot
- Polymarket weather market pages (live data)
