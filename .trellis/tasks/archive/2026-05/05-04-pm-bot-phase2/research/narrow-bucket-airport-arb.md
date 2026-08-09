# Research: Narrow Bucket Buy NO & Airport Arbitrage Strategies

- **Query**: "Narrow bucket buy NO" and "airport arbitrage" strategies for Polymarket weather markets; config.toml patterns for Python CLI tools
- **Scope**: External + Internal
- **Date**: 2026-05-04

## Findings

---

## 1. Narrow Bucket Buy NO — What It Means

### Definition

"Narrow bucket buy NO" is a strategy that exploits the systematic overpricing of narrow (1°F or 1°C) temperature buckets near the center of the forecast distribution. In Polymarket's negative-risk multi-outcome markets, each bucket is a separate tradeable contract. When buckets are narrow (1°F), even a small uncertainty in the forecast creates significant probability mass spread across many adjacent buckets — meaning the "most likely" single bucket is still unlikely in absolute terms.

### Why Narrow Buckets Are Overpriced

1. **Retail concentration on modal outcomes**: When a forecast says "68°F", retail traders pile into the 68–69°F bucket. But with 1°F buckets and a forecast σ of ~2°F, the modal bucket only has ~20% probability — yet the market often prices it at 35–50¢ (implied 35–50%). The bucket is overpriced relative to its true probability.

2. **Neglected probability mass in adjacent buckets**: A 2°F forecast uncertainty means 3–5 adjacent buckets each carry 10–20% probability. Retail ignores these, concentrating capital on the single most-likely bucket, inflating its price.

3. **Favorite-longshot bias in reverse**: High-priced buckets ($0.40–$0.80) are systematically overpriced because retail treats the forecast as more certain than it is. Low-priced tail buckets ($0.05–$0.15) are systematically underpriced.

### How to Detect Narrow Bucket Overpricing

- **Sum-of-YES check**: In a well-calibrated market with 1°F buckets, the sum of all YES prices should equal ~$1.00 (since exactly one bucket wins). If ΣYES > $1.00, some buckets are overpriced — typically the center ones. If ΣYES < $1.00, some are underpriced — typically the tails.
- **Model vs. market comparison**: Use ensemble forecast to compute P(bucket) for each bucket. If the center bucket's YES price exceeds model_prob by >8%, it's a buy-NO candidate.
- **Bucket width filter**: Only 1°F or 1°C buckets are narrow enough for this edge to work. 2°F or 2°C buckets are wide enough that the center bucket often IS the dominant probability mass, reducing the overpricing effect.

### The Edge

- **Buy NO on center buckets**: If a 1°F bucket is priced at $0.45 YES ($0.55 NO), but the model gives it only 20% probability, buying NO at $0.55 gives an edge of (0.80 × $0.55) − (0.20 × $0.45) = $0.44 − $0.09 = **$0.35 expected profit per $0.55 risked**.
- **gopfan2 variant**: Buy NO on any bucket where YES ≥ $0.45 (i.e., NO ≤ $0.55). The assumption is that high-priced buckets are systematically overpriced. This is the "no_min: 0.45" parameter in our existing `Gopfan2Strategy`.
- **Combined approach**: Buy YES on tails (YES ≤ $0.15) AND buy NO on center (YES ≥ $0.45) simultaneously. This captures both sides of the favorite-longshot bias.

### Existing Implementation in This Project

The `Gopfan2Strategy` in `pm_bot/strategies/base.py` already implements this:
- `yes_max = 0.15` — buy YES on tail buckets priced ≤ $0.15
- `no_min = 0.45` — buy NO on center buckets where NO price ≥ $0.45 (i.e., YES ≥ $0.55)
- EV calculation assumes 80% win rate as a heuristic

### Key Parameters for Config

```toml
[strategies.narrow_bucket]
yes_max = 0.15          # Buy YES on buckets priced ≤ this
no_min = 0.45           # Buy NO on buckets where NO ≥ this
min_edge = 0.08         # Minimum model-vs-market edge to trade
min_volume = 2000       # Minimum market volume in USD
bucket_width_max = 2    # Only apply to buckets ≤ this width (°F or °C)
```

---

## 2. Airport Arbitrage — What It Means

### Definition

"Airport arbitrage" exploits the systematic temperature difference between the airport weather station where Polymarket markets resolve and the city-center readings that retail traders reference. Polymarket temperature markets resolve at specific airport ASOS/AWOS stations (identified by ICAO codes), not at city-center locations. Retail traders using weather apps see city-center temperatures, which can differ from airport readings by **3–8°F**.

### How the Arbitrage Works

1. **Retail anchors to city-center**: A retail trader checks their phone weather app ("New York: 88°F") and buys YES on the 86–90°F bucket for NYC.
2. **Market resolves at airport**: NYC resolves at LaGuardia (KLGA), which on a summer sea-breeze day might read 83°F.
3. **The gap is the edge**: The 83–84°F bucket was cheap (retail didn't buy it); the 86–90°F bucket was expensive (retail overbought it). A trader who knows the airport delta buys NO on the overpriced warm bucket and/or YES on the correctly-priced cooler bucket.

### Why It's Predictable

The airport-vs-city delta is not random noise — it has a **predictable sign and magnitude** based on local geography and synoptic weather pattern:

| City | Airport (ICAO) | Delta Direction | Magnitude | Mechanism |
|------|----------------|-----------------|-----------|-----------|
| New York | KLGA (LaGuardia) | Airport cooler | 3–6°F | Sea breeze off Flushing Bay in summer; waterfront location |
| Paris | LFPB (Le Bourget) | Airport cooler | 2–3°C | Less urban heat island than city center; suburban plain NE of city |
| London | EGLC (City) or EGLL (Heathrow) | Airport cooler | 2–4°C | Urban heat island in Westminster vs. suburban airports |
| Los Angeles | KLAX or KBUR (verify per market) | Variable | 12–18°F range | Marine layer: LAX can be socked in (cool) while Burbank clears (hot) |
| Tokyo | RJTT (Haneda) or RJAA (Narita) | Airport cooler (Narita) | 1–3°C | Narita 65km NE, less heat island; Haneda bay-adjacent |
| Shanghai | ZSPD (Pudong) or ZSSS (Hongqiao) | Variable | 2–4°C | Pudong coastal (sea breeze), Hongqiao inland (warmer) |
| Seoul | RKSI (Incheon) | Airport cooler | 2–4°C | Incheon on Yeongjong Island, marine influence |
| Beijing | ZBAA (Capital) | Variable | 2–4°C | Dust/haze can suppress temps at ZBAA; NE of center |
| Hong Kong | VHHH (HK Intl) | Airport slightly cooler | 1–2°C | Reclaimed land offshore; stagnant heat events |
| Dallas | KDAL (Love Field) | Verify per market | — | Not DFW; Love Field is closer to city center |
| Chicago | KORD (O'Hare) | Airport cooler | 2–4°F | O'Hare NW suburbs, less heat island than Loop |

### How to Detect Airport Arbitrage Opportunities

1. **Identify the resolution station**: Parse the market description for ICAO code or station name.
2. **Compute the forecast for the airport coordinates** (NOT city center): This is the most critical step. Using city-center lat/lon in the forecast API introduces the 3–8°F error that causes losses.
3. **Compute the forecast for city center** (what retail sees): This is what drives market pricing.
4. **Delta = airport_forecast − city_center_forecast**: If delta is negative (airport cooler), warm buckets are overpriced. If delta is positive, cool buckets are overpriced.
5. **Filter by atmospheric regime**: The delta is pattern-dependent. Sea-breeze events in NYC only occur with onshore flow. Marine layer in LA requires specific conditions. The delta must be validated against the current synoptic pattern.

### Station Mapping (Critical — Must Be Per-Market, Not Per-City)

Polymarket has changed resolution stations between market series. The same city can resolve at different airports in different markets. **Every trade must verify the station from the market's own rules.**

Known stations from polymarketweather.com:

| City | Primary Station | Backup/Alternative | Notes |
|------|----------------|-------------------|-------|
| NYC | KLGA | — | LaGuardia, NOT JFK or Newark |
| LA | KLAX or KBUR | Per-market | Verify every time |
| London | EGLC or EGLL | Per-market | City vs Heathrow varies |
| Paris | LFPB | — | Le Bourget, NOT CDG or Orly |
| Tokyo | RJTT or RJAA | Per-market | Haneda vs Narita |
| Hong Kong | VHHH | — | Unambiguous |
| Shanghai | ZSPD or ZSSS | Per-market | Pudong vs Hongqiao |
| Beijing | ZBAA | — | Capital Intl |
| Seoul | RKSI | — | Incheon |
| Dallas | KDAL | — | Love Field, NOT DFW |
| Chicago | KORD | — | O'Hare |

### Airport Coordinates for Forecast Queries

| ICAO | Airport | Lat | Lon |
|------|---------|-----|-----|
| KLGA | LaGuardia | 40.7772 | -73.8726 |
| KORD | O'Hare | 41.9742 | -87.9073 |
| KMIA | Miami Intl | 25.7953 | -80.2902 |
| KDAL | Dallas Love | 32.8471 | -96.8518 |
| KSEA | Sea-Tac | 47.4502 | -122.3088 |
| KATL | Hartsfield | 33.6407 | -84.4277 |
| KLAX | LAX | 33.9425 | -118.4081 |
| KBUR | Burbank | 34.2007 | -118.3586 |
| EGLC | London City | 51.5048 | 0.0495 |
| EGLL | Heathrow | 51.4775 | -0.4614 |
| LFPB | Le Bourget | 48.9694 | 2.4414 |
| RJTT | Haneda | 35.5522 | 139.7796 |
| RJAA | Narita | 35.7647 | 140.3864 |
| VHHH | HK Intl | 22.3080 | 113.9185 |
| ZSPD | Pudong | 31.1443 | 121.8083 |
| ZSSS | Hongqiao | 31.1979 | 121.3375 |
| ZBAA | Beijing Capital | 40.0801 | 116.5845 |
| RKSI | Incheon | 37.4602 | 126.4407 |
| RCTP | Taoyuan | 25.0777 | 121.2328 |
| ZHHH | Wuhan Tianhe | 30.7838 | 114.2081 |

### Current Project Bug: CITY_COORDS Uses City Centers

The existing `pm_bot/models/config.py` has `CITY_COORDS` using city-center coordinates (e.g., NYC: 40.7128, -74.006), which is LaGuardia-correct for lat but incorrect for lon (KLGA is at -73.8726). This introduces forecast errors of 2–5°F on narrow bucket markets. The coordinates need to be replaced with airport station coordinates.

---

## 3. How Existing Bots Implement These Strategies

### alteregoeth-ai/weatherbot (Python, 258★)

**Strategy**: EV + fractional Kelly + stop-loss
- Fetches ECMWF + HRRR via Open-Meteo **at airport station coordinates**
- Gets real-time METAR observations from Aviation Weather Center
- Matches forecast to temperature bucket
- Calculates Expected Value — only enters if positive
- Sizes with fractional Kelly (0.25x)
- 20-city support
- Uses `config.json` for parameters

**Airport arbitrage implementation**: Uses airport station coordinates explicitly in all forecast queries. The README explicitly states: "Most bots use city center coordinates. That's wrong."

**Config.json structure**:
```json
{
  "balance": 10000.0,
  "max_bet": 20.0,
  "min_ev": 0.05,
  "max_price": 0.45,
  "min_volume": 2000,
  "min_hours": 2.0,
  "max_hours": 72.0,
  "kelly_fraction": 0.25,
  "max_slippage": 0.03,
  "scan_interval": 3600,
  "calibration_min": 30,
  "vc_key": "YOUR_VISUAL_CROSSING_KEY"
}
```

### GuillermoEguilaz/Polymarket-Weather-Bot (TypeScript, 301★)

**Strategy**: NWS forecast → bucket matching → price threshold
- Uses NWS observations and forecast data
- Entry threshold: YES < $0.15, exit threshold: YES > $0.45
- Three execution modes: signal, paper, live
- `.env` for configuration (not config.toml)
- 6 US cities

**Narrow bucket implementation**: The ENTRY_THRESHOLD=0.15 and EXIT_THRESHOLD=0.45 directly mirror the gopfan2 strategy.

### polymarketweather.com (Commercial, closed-source)

**Strategy**: 4-model ensemble + calibration + behavioral edge
- ECMWF IFS (weight 0.35), GEFS 31-member (0.25), UKMO (0.20), NWS (0.20)
- Each deterministic forecast treated as Gaussian with σ derived from forecast horizon (0.8°F at 6h to 5.5°F at 10d)
- Normal CDF evaluated over market bucket → P(bucket)
- Bayesian adjustment with NOAA NCEI 10-year base rates
- Edge = model_prob − market_price; only trades if ≥ 8% AND z-score ≥ 1.5
- Fractional Kelly at 15% of full Kelly, hard cap $100/trade, 5% bankroll max
- WebSocket for real-time price streaming

**Airport arbitrage implementation**: Verifies resolution station from market rules before every deployment. Uses airport coordinates for forecast queries. Key quote: "using city-centre coordinates introduces 3–8°F error on 1–2°F bucket markets — a guaranteed loss on the wrong side."

### suislanchez/polymarket-kalshi-weather-bot (Python/TS, 291★)

**Strategy**: GFS 31-member ensemble counting + Kelly + BTC microstructure
- Multi-platform: Kalshi + Polymarket simultaneously
- Ensemble counting: 28/31 members above 70°F = 90% probability
- Edge threshold: 8%
- React dashboard for monitoring

### Summary Table

| Bot | Narrow Bucket | Airport Arb | Config Format | Language |
|-----|--------------|-------------|---------------|----------|
| alteregoeth/weatherbot | EV + Kelly + $0.45 max | Airport coords in forecasts | config.json | Python |
| GuillermoEguilaz | $0.15 entry / $0.45 exit | Not implemented | .env | TypeScript |
| polymarketweather.com | 8% min edge + z≥1.5 | Station verification per trade | Internal | Unknown |
| suislanchez | Ensemble counting | Not explicit | TypeScript config | TS + Python |

---

## 4. Python config.toml Libraries Comparison

### tomllib (stdlib, Python 3.11+)

- **Status**: Standard library since Python 3.11
- **Capabilities**: Read-only (parse TOML → dict)
- **Pros**: Zero dependencies, guaranteed available, fast, well-tested
- **Cons**: Cannot write TOML; no style preservation; no environment variable merging
- **API**:
  ```python
  import tomllib
  with open("config.toml", "rb") as f:
      data = tomllib.load(f)
  # Returns plain dict
  ```
- **Best for**: Reading config files that are hand-edited; when zero dependencies matter

### tomlkit (python-poetry/tomlkit, 822★)

- **Status**: Third-party, active maintenance, TOML 1.1.0 compliant
- **Capabilities**: Read AND write TOML with **style preservation** (comments, whitespace, ordering)
- **Pros**: Round-trip editing; preserves comments and formatting; used by Poetry
- **Cons**: Third-party dependency; slightly slower than tomllib for read-only
- **API**:
  ```python
  import tomlkit
  doc = tomlkit.parse(toml_string)
  doc["section"]["key"] = "new value"
  tomlkit.dumps(doc)  # preserves original formatting
  ```
- **Best for**: CLI tools that need to write/edit config (e.g., `pm-bot config set edge_min 0.10`)

### dynaconf (dynaconf/dynaconf)

- **Status**: Third-party, mature, full config management framework
- **Capabilities**: Multi-format (TOML/YAML/JSON/INI), layered environments (dev/prod), env var overrides, secrets management, validation, CLI
- **Pros**: Complete config management solution; built-in validation; env var merging; Flask/Django extensions
- **Cons**: Heavy dependency; vendored libraries; overkill for a single-purpose CLI tool; opinionated structure
- **API**:
  ```python
  from dynaconf import Dynaconf
  settings = Dynaconf(settings_files=["config.toml", ".secrets.toml"])
  settings.edge_min  # dot-notation access
  ```
- **Best for**: Web applications with complex multi-environment config; NOT ideal for a focused trading CLI

### Recommendation for This Project

**Use `tomllib` for reading + `tomlkit` for writing/editing.** This combination:
1. Zero-cost reading via stdlib (no dependency for the hot path)
2. Style-preserving writes for `pm-bot config edit` commands
3. No framework lock-in; simple dict-based config passed to strategies
4. Fallback to `tomllib` on Python 3.11+, with `tomli` as backport for 3.10

**Do NOT use dynaconf** — it's overkill for a trading bot with ~20 config parameters and no web framework.

---

## 5. Example config.toml for Strategy Parameters

```toml
# pm-bot configuration
# Schema version for future migrations
schema_version = 1

[bot]
# General bot settings
scan_interval_seconds = 120       # How often to scan markets
max_open_positions = 20           # Concurrent position limit
circuit_breaker_daily_pct = -10  # Halt if daily P&L < this %
paper_trading = true              # Start in simulation mode
bankroll_usd = 1000.0            # Starting bankroll for sizing

[strategies.narrow_bucket]
enabled = true
# Buy YES on tail buckets priced ≤ this
yes_max = 0.15
# Buy NO on center buckets where NO ≥ this
no_min = 0.45
# Minimum model-vs-market edge to trade
min_edge = 0.08
# Only apply to buckets ≤ this width
bucket_width_max = 2
# Assume this win rate for EV calculation
assumed_win_rate = 0.80

[strategies.airport_arb]
enabled = true
# Minimum airport-vs-city delta (°F) to trade
min_delta_f = 3.0
# Which atmospheric regimes to trade
# Options: "sea_breeze", "marine_layer", "heat_island", "all"
regimes = ["sea_breeze", "heat_island"]
# Cities where airport arbitrage is enabled
cities = ["NYC", "Paris", "London", "Los_Angeles", "Tokyo", "Seoul"]
# Minimum edge after applying delta
min_edge = 0.08

[strategies.ladder]
enabled = true
edge_min = 0.08
spread = 1.0

[strategies.sum_arb]
enabled = true
gap_min = 0.02

[sizing]
kelly_fraction = 0.15            # Fractional Kelly (15% of full)
max_position_usd = 100           # Hard cap per position
max_bankroll_pct = 0.05          # Max 5% of bankroll per trade
min_position_usd = 1.0           # Minimum bet size

[risk]
max_slippage = 0.03              # Skip markets with spread > this
min_hours_to_resolution = 2.0    # Skip markets resolving sooner
max_hours_to_resolution = 72.0   # Skip markets resolving later
min_volume_usd = 2000            # Minimum market volume

[forecast]
# Default model for deterministic forecast
default_model = "gfs_seamless"
# Ensemble spread multiplier (1.15 = correct for underdispersion)
ensemble_spread_multiplier = 1.15
# Cache TTL in seconds for different data types
[sources.open_meteo]
base_url = "https://api.open-meteo.com/v1"
ensemble_url = "https://ensemble-api.open-meteo.com/v1/ensemble"
# No auth required

[sources.polymarket_gamma]
base_url = "https://gamma-api.polymarket.com"
# No auth required

[sources.polymarket_clob]
base_url = "https://clob.polymarket.com"
# Auth via wallet (trading only)

[sources.visual_crossing]
# Free API key for historical resolution data
api_key = ""                      # Set in .secrets.toml or env var

[sources.nws]
base_url = "https://api.weather.gov"
# No auth required, but set User-Agent header

# Airport station coordinates (CRITICAL: must match market resolution source)
[stations.KLGA]
name = "LaGuardia"
city = "NYC"
lat = 40.7772
lon = -73.8726

[stations.KORD]
name = "O'Hare"
city = "Chicago"
lat = 41.9742
lon = -87.9073

[stations.KMIA]
name = "Miami Intl"
city = "Miami"
lat = 25.7953
lon = -80.2902

[stations.KDAL]
name = "Dallas Love Field"
city = "Dallas"
lat = 32.8471
lon = -96.8518

[stations.EGLC]
name = "London City"
city = "London"
lat = 51.5048
lon = 0.0495

[stations.EGLL]
name = "Heathrow"
city = "London"
lat = 51.4775
lon = -0.4614

[stations.LFPB]
name = "Le Bourget"
city = "Paris"
lat = 48.9694
lon = 2.4414

[stations.RJTT]
name = "Haneda"
city = "Tokyo"
lat = 35.5522
lon = 139.7796

[stations.RJAA]
name = "Narita"
city = "Tokyo"
lat = 35.7647
lon = 140.3864

[stations.VHHH]
name = "HK Intl"
city = "Hong Kong"
lat = 22.3080
lon = 113.9185

[stations.RKSI]
name = "Incheon"
city = "Seoul"
lat = 37.4602
lon = 126.4407

[stations.ZSPD]
name = "Pudong"
city = "Shanghai"
lat = 31.1443
lon = 121.8083

[stations.ZBAA]
name = "Beijing Capital"
city = "Beijing"
lat = 40.0801
lon = 116.5845
```

Secrets should be in a separate `.secrets.toml` (gitignored):

```toml
# .secrets.toml — DO NOT COMMIT
[sources.visual_crossing]
api_key = "YOUR_KEY_HERE"

[polymarket]
private_key = "0x..."
proxy_wallet_address = "0x..."
```

---

## 6. Open-Source Implementations on GitHub

### Directly Relevant (Weather Trading Bots)

| Repository | Stars | Language | Strategy | Airport Arb? |
|-----------|-------|----------|----------|-------------|
| alteregoeth-ai/weatherbot | 258 | Python | EV + Kelly + stop | Yes (airport coords) |
| GuillermoEguilaz/Polymarket-Weather-Bot | 301 | TypeScript | NWS + threshold | No |
| suislanchez/polymarket-kalshi-weather-bot | 291 | TS + Python | Ensemble counting | No |
| MoonsatProtocol/Polymarket-Weather-Bot | 104 | TypeScript | Kelly + NWS | No |
| hcharper/polyBot-Weather | 35 | Python | Basic | Unknown |
| idlepraxis/polymarket-weather-bot | 22 | Python | Basic | Unknown |
| RiekertQuant/polymarket-weather-bot-poc | 21 | Python | Paper trading | Unknown |
| yangyuan-zhen/PolyWeather | 58 | Python | Quant analysis | Unknown |
| ScouterInfinite/polymarket-trading-bot-copytrading | 137 | TypeScript | Copy trading | N/A |

### Notable (Advanced/Research)

| Repository | Stars | Language | Strategy | Airport Arb? |
|-----------|-------|----------|----------|-------------|
| santox422/weather-edge | 3 | TS + Python | 5-stage + Black-Litterman | Partial |
| openclaw/simmer | Unknown | Python | Bonding + tail | Unknown |

### None Implement "Airport Arbitrage" as a Named Strategy

After searching GitHub and polymarketweather.com extensively, **no open-source bot implements "airport arbitrage" as a distinct, named strategy**. The concept is discussed extensively on polymarketweather.com as a "structural edge" (edge type #2: airport-vs-city discrepancy), but implementations either:
1. Use airport coordinates for forecasts (alteregoeth/weatherbot) — which avoids the error but doesn't actively exploit the delta
2. Don't address it at all

This means building an explicit "airport arbitrage" strategy would be novel among open-source bots.

---

## Internal Code References

| File Path | Description |
|-----------|-------------|
| `pm_bot/models/config.py` | City coordinates (currently city-center, needs airport coords), strategy defaults |
| `pm_bot/strategies/base.py` | Gopfan2Strategy (narrow bucket), SumArbStrategy, LadderStrategy implementations |
| `pm_bot/core/weather.py` | Forecast fetching + `bucket_probability_numpy()` |
| `pm_bot/core/polymarket.py` | Market discovery, `_extract_airport()`, bucket parsing |
| `.trellis/tasks/00-bootstrap-guidelines/research/polymarket-weather-markets-api.md` | API architecture research |
| `.trellis/tasks/00-bootstrap-guidelines/research/existing-polymarket-weather-bots.md` | Bot comparison table |

### Related Spec Documents

- `.trellis/spec/` — No existing spec for weather strategies

### Not Found

- No open-source implementation of explicit "airport arbitrage" strategy
- No Python weather bot using `config.toml` (all use `config.json` or `.env`)
- No historical dataset of airport-vs-city temperature deltas published publicly
