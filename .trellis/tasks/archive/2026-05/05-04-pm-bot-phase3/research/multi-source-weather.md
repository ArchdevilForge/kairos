# Research: Multi-Source Weather Data Aggregation

- **Query**: Free weather APIs beyond Open-Meteo, METAR data, forecast aggregation methods, edge calculation with multiple sources, historical accuracy comparison
- **Scope**: External + Internal (existing codebase)
- **Date**: 2026-05-04

---

## 1. Free Weather APIs Beyond Open-Meteo

### 1a. NOAA / NWS API (weather.gov) — US Stations, Free, No Key

**Base URL:** `https://api.weather.gov`

| Feature | Detail |
|---|---|
| Auth | No API key; User-Agent header required |
| Forecast range | 7-day hourly + 12h-period daily |
| Resolution | ~2.5km x 2.5km grid |
| Real-time obs | Yes (US observation stations via `/stations/{id}/observations`) |
| Rate limit | Undocumented but "generous"; retry after 5s if limited |
| Formats | GeoJSON (default), JSON-LD, DWML, CAP, ATOM |

**Key endpoints:**

```
GET /points/{lat},{lon}                          → gridpoint metadata + station URLs
GET /gridpoints/{wfo}/{x},{y}/forecast            → 12h period forecast (7 days)
GET /gridpoints/{wfo}/{x},{y}/forecast/hourly     → hourly forecast (7 days)
GET /gridpoints/{wfo}/{x},{y}                     → raw gridded data
GET /stations/{stationId}/observations/latest     → latest observation
GET /stations/{stationId}/observations            → observation history
```

**Usage example:**

```python
import httpx

async def fetch_nws_forecast(client, lat, lon):
    # Step 1: resolve gridpoint
    resp = await client.get(
        f"https://api.weather.gov/points/{lat},{lon}",
        headers={"User-Agent": "pm-bot/1.0"}
    )
    grid = resp.json()["properties"]
    # Step 2: get hourly forecast
    hourly_url = grid["forecastHourly"]
    resp2 = await client.get(hourly_url, headers={"User-Agent": "pm-bot/1.0"})
    return resp2.json()
```

**Known issues:**
- Station observations show null 24h max/min temps outside Central Time (MADIS bug)
- Observations may be delayed up to 20 min from MADIS QC processing
- No geocoding — requires lat/lon input
- US-only coverage

### 1b. OpenWeatherMap — Free Tier

**Base URL:** `https://api.openweathermap.org/data/3.0/onecall` (One Call 3.0)

| Feature | Free Tier | Startup ($40/mo) |
|---|---|---|
| API calls | 1,000/day free (One Call 3.0 pay-per-call) | 10M/month |
| Current weather | ✅ | ✅ |
| 5-day / 3-hour forecast | ✅ | ✅ |
| 16-day daily forecast | ❌ (paid only) | ✅ |
| Hourly 4-day forecast | ❌ (paid only) | ✅ |
| Historical data | ❌ | ❌ (paid only) |
| Air pollution | ✅ | ✅ |
| Geocoding | ✅ | ✅ |
| Weather maps | ✅ | ✅ |
| Update frequency | Every 2 hours | Every 1 hour |
| Uptime | 95% | 99.5% |

**One Call 3.0 (pay-per-call):** First 1,000 calls/day free, then ~$0.0015/call.
- Includes: current, 1-min to 8-day forecast, 47+ years history, government alerts, AI summary
- Single endpoint returns all data

**Accuracy vs Open-Meteo:**
- OpenWeatherMap uses a blend of models + station data; updates less frequently on free tier (2h vs 1h)
- Open-Meteo uses raw NWP model output (GFS, ECMWF, HRRR, etc.) directly, providing deterministic multi-model access
- Open-Meteo provides ensemble data (31 GFS members) which OpenWeatherMap does not expose
- For prediction markets: Open-Meteo's direct model access + ensemble is superior for edge calculation; OWM is better for current conditions due to station integration

**Key limitation for PM bot:** Free tier 1,000 calls/day is sufficient for ~40 cities at hourly refresh. Paid tiers needed for historical backtesting.

### 1c. WeatherAPI.com — Free Tier

**Base URL:** `https://api.weatherapi.com/v1`

| Feature | Free ($0) | Starter ($7/mo) | Pro+ ($25/mo) |
|---|---|---|---|
| API calls | 100K/month | 3M/month | 5M/month |
| Forecast range | 3-day | 7-day | 300-day ahead |
| Historical | Past 1 day | Past 7 days | Past 365 days |
| Real-time | ✅ | ✅ | ✅ |
| Hourly + daily | ✅ | ✅ | ✅ |
| Air quality | Limited | ✅ | ✅ |
| Weather alerts | Limited | ✅ | ✅ |
| Astronomical | ✅ | ✅ | ✅ |
| Uptime | 95.5% | 99% | 99% |
| Commercial use | ✅ (link-back required) | ✅ | ✅ |

**Unique features:**
- Airport code / METAR lookup: `GET /current.json?q=KLGA` returns decoded METAR
- IP-based location lookup
- Future weather up to 365 days ahead (Pro+ tier)
- Marine weather + tide data (Starter+)

**Forecast quality:**
- Uses "in-house" application blending GFS, JMA, ECMWF, NASA satellite data
- Updates every 4-6 hours (slower than Open-Meteo's per-model updates)
- Historical data is **archived forecast data, not actuals** — important distinction for backtesting

### 1d. Visual Crossing — Historical + Forecast

**Base URL:** `https://weather.visualcrossing.com/VisualCrossingWebServices/rest/services/timeline/{location}`

| Feature | Free/Metered | Professional |
|---|---|---|
| Records/day | 1,000 free; then $0.0001/record | 10M/month included |
| Forecast | 15-day | 15-day |
| Historical | 50+ years | 50+ years |
| Historical forecast | ✅ | ✅ |
| Real-time | ✅ | ✅ |
| Formats | JSON, CSV | JSON, CSV |
| API key | Required (free) | Required |

**Key advantage:** Used by multiple Polymarket weather bots as a Wunderground replacement for resolution data. Provides actual observed historical data (not forecast archives). The "historical forecast" feature lets you see what forecasts were issued on a given past date — valuable for backtesting edge calculation.

**Resolution data alignment:**
- Visual Crossing sources include NOAA ISD (Integrated Surface Database) which includes airport station data
- Station IDs in responses match ICAO codes (e.g., KLGA, KJFK)
- This is the closest free API to Wunderground historical data used for Polymarket resolution

### 1e. Other APIs Used in Prediction Market Bots

| API | Use Case | Cost | Notes |
|---|---|---|---|
| **Tomorrow.io** (was Climacell) | High-resolution forecasts, minutely updates | Free: 500/day; Paid: $79/mo | Good for hyper-local, but expensive for scale |
| **Pirate Weather** | Dark Sky API replacement | Free: 10K/month; $0.0001/call after | API-compatible with defunct Dark Sky |
| **AccuWeather** | Enterprise forecasts | Free: 50/day; limited | Low free tier, good accuracy reputation |
| **Meteostat** | Historical climate data | Free (CC BY-NC) | Python library + API; 40K+ stations |
| **Open-Meteo Historical Forecast** | Past forecast verification | Free | `https://historical-forecast-api.open-meteo.com/v1/forecast` — shows what each model predicted on a given date |

---

## 2. Airport METAR Data

### 2a. Aviation Weather Center (AWC) API — Primary Source

**Base URL:** `https://aviationweather.gov/api/data`

**Key endpoints (redesigned 2025):**

| Endpoint | Description | Formats | Update Freq |
|---|---|---|---|
| `GET /metar?ids={ICAO}&format=json` | Latest METAR | JSON, GeoJSON, CSV, XML | ~1 min |
| `GET /taf?ids={ICAO}&format=json` | Terminal Aerodrome Forecast | JSON, GeoJSON, XML | ~10 min |
| `GET /station?ids={ICAO}&format=json` | Station metadata | JSON, GeoJSON, XML | Daily |
| `GET /metar?ids={ICAO}&format=json&hours=24` | Last 24h METAR history | Same | On demand |

**Cache files (bulk download, updated every minute):**

| File | URL |
|---|---|
| All METARs (CSV) | `https://aviationweather.gov/data/cache/metars.cache.csv.gz` |
| All METARs (XML) | `https://aviationweather.gov/data/cache/metars.cache.xml.gz` |
| All TAFs (XML) | `https://aviationweather.gov/api/data/tafs.cache.xml.gz` |
| Stations (JSON) | `https://aviationweather.gov/data/cache/stations.cache.json.gz` |

**Rate limits:** 100 requests/minute max; 1 request/min per thread; max 400 entries per query.

**JSON response structure (METAR):**

```json
{
  "rawText": "KLGA 041951Z 19010KT 10SM FEW040 OVC070 12/08 A2992",
  "stationId": "KLGA",
  "observationTime": "2026-05-04T19:51:00Z",
  "temp": 12.0,
  "dewpoint": 8.0,
  "windSpeed": 10,
  "windDir": 190,
  "visibility": 10.0,
  "altimeter": 29.92,
  "clouds": [...],
  "flightCat": "VFR"
}
```

### 2b. How METAR Temperature Relates to Polymarket Resolution

**Resolution mechanism:**
- Polymarket weather markets resolve against **Weather Underground** data, which sources from the same airport stations (ICAO) that produce METARs
- WU displays the daily max/min temperature for a given airport station
- The METAR itself reports **instantaneous** temperature at observation time (typically hourly)
- To derive daily max/min: aggregate all METAR observations for a station over the UTC day (or local day, depending on market terms)
- Some markets use **"high temperature"** which corresponds to the max of all hourly readings for that station on that date

**Temperature extraction from raw METAR:**

```
KLGA 041951Z 19010KT 10SM FEW040 OVC070 12/08 A2992
                                    ^^ ^^
                                    |  dewpoint (°C)
                                    temperature (°C)
```

- Temperature is always in **Celsius** in raw METAR
- `12/08` = temperature 12°C, dewpoint 8°C
- `M05/M08` = negative temperatures: -5°C / -8°C
- `M05/` = missing dewpoint

**Update frequency and latency:**
- METARs are issued roughly every hour (spec: every 30 min for US, hourly elsewhere)
- Special METARs (SPECI) issued for significant changes
- AWC API shows observations with up to 20 min latency due to MADIS QC processing
- Cache files updated once per minute — sufficient for near-real-time monitoring

### 2c. Other METAR Sources

| Source | API Key | Rate Limit | Decoded JSON |
|---|---|---|---|
| AWC API | No | 100/min | ✅ |
| MetarCentral | Optional (free) | 10/request (anon) | ✅ |
| CheckWX | Yes (free) | 3K/day | ✅ |
| AviationWX.org | Optional | 50/min (anon) | ✅ |
| WeatherAPI.com | Yes (free) | 100K/mo | ✅ (via airport code query) |

---

## 3. Forecast Aggregation Methods

### 3a. Simple Averaging

The most basic approach: average temperature forecasts from all sources.

```python
import numpy as np

def simple_average(temps: list[float]) -> float:
    """Average temperature from multiple sources."""
    return float(np.mean(temps))
```

**Pros:** Easy, no historical data needed
**Cons:** Doesn't account for source accuracy differences; equally weights a bad source with a good one

### 3b. Inverse-Error Weighting (Weight by Historical Accuracy)

Weight each source inversely proportional to its historical RMSE.

```python
def inverse_error_weighted(temps: list[float], rmses: list[float]) -> float:
    """Weight forecasts by inverse of each source's historical RMSE."""
    weights = [1.0 / (rmse ** 2) for rmse in rmses]
    total_w = sum(weights)
    weighted_temp = sum(t * w for t, w in zip(temps, weights)) / total_w
    return weighted_temp

# Example: 3 sources with known RMSE
# Open-Meteo GFS: RMSE 1.8°C, temp=25.0
# NOAA NWS: RMSE 2.1°C, temp=24.5
# OpenWeatherMap: RMSE 2.5°C, temp=26.0
# Result: closer to Open-Meteo's 25.0 (lowest error → highest weight)
```

**Pros:** Better than simple averaging; accounts for known accuracy differences
**Cons:** Requires historical RMSE data per source; RMSE varies by lead time and geography

### 3c. Bayesian Model Averaging (BMA)

BMA produces a probabilistic forecast that is a weighted average of the predictive distributions from each source, where weights reflect each model's posterior probability of being the "best" model.

```python
from scipy import stats

def bma_temperature(
    means: list[float],
    stds: list[float],
    weights: list[float]
) -> tuple[float, float]:
    """
    Bayesian Model Averaging for temperature forecasts.
    
    Returns (bma_mean, bma_variance) for the BMA predictive distribution.
    
    BMA pdf = Σ(w_k * f_k(y)), where:
    - w_k = posterior weight for model k (Σ w_k = 1)
    - f_k = normal pdf N(mean_k, std_k²) for model k
    
    BMA mean = Σ(w_k * mean_k)
    BMA variance = Σ(w_k * (std_k² + mean_k²)) - (Σ w_k * mean_k)²
    """
    bma_mean = sum(w * m for w, m in zip(weights, means))
    bma_var = sum(w * (s**2 + m**2) for w, m, s in zip(weights, means, stds)) - bma_mean**2
    return bma_mean, bma_var

def bma_bucket_probability(
    means: list[float],
    stds: list[float],
    weights: list[float],
    temp_low: float,
    temp_high: float
) -> float:
    """
    Probability that BMA temperature falls in [temp_low, temp_high].
    """
    prob = 0.0
    for m, s, w in zip(means, stds, weights):
        p_low = stats.norm.cdf(temp_low, loc=m, scale=s)
        p_high = stats.norm.cdf(temp_high, loc=m, scale=s)
        prob += w * (p_high - p_low)
    return prob

# Determine BMA weights from historical performance (EM algorithm)
# Simplified: use inverse-RMSE² as initial weights, then normalize
def bma_weights_from_rmse(rmses: list[float]) -> list[float]:
    """Approximate BMA weights from RMSE (proper BMA uses EM)."""
    inv_sq = [1.0 / (r ** 2) for r in rmses]
    total = sum(inv_sq)
    return [w / total for w in inv_sq]
```

**Proper BMA weight estimation (EM algorithm):**

```python
def em_bma_weights(
    observed: list[float],       # actual temps for N days
    forecasts: list[list[float]], # K models × N days
    max_iter: int = 100,
    tol: float = 1e-6
) -> list[float]:
    """
    Estimate BMA weights using EM algorithm.
    K = number of models, N = number of observation days.
    """
    K = len(forecasts)
    N = len(observed)
    
    # Initialize weights uniformly
    w = [1.0 / K] * K
    
    for _ in range(max_iter):
        # E-step: compute posterior model probabilities
        g = []
        for k in range(K):
            resid = [observed[i] - forecasts[k][i] for i in range(N)]
            sigma_k = max(float(np.std(resid)), 0.1)
            ll = [stats.norm.pdf(observed[i], loc=forecasts[k][i], scale=sigma_k)
                  for i in range(N)]
            g.append([w[k] * l for l in ll])
        
        # Normalize posteriors
        g_norm = []
        for i in range(N):
            row_sum = sum(g[k][i] for k in range(K))
            for k in range(K):
                g[k][i] = g[k][i] / max(row_sum, 1e-12)
        
        # M-step: update weights
        w_new = [sum(g[k]) / N for k in range(K)]
        
        # Check convergence
        if max(abs(w_new[k] - w[k]) for k in range(K)) < tol:
            break
        w = w_new
    
    return w
```

### 3d. Practical Implementation Pattern for PM Bot

```python
from dataclasses import dataclass
from scipy import stats

@dataclass
class SourceForecast:
    source: str
    temp_high_c: float
    std_c: float           # uncertainty from ensemble or historical error
    weight: float = 1.0     # BMA weight or inverse-error weight

class MultiSourceAggregator:
    """Aggregate forecasts from multiple weather sources."""
    
    def __init__(self, source_weights: dict[str, float] | None = None):
        self.source_weights = source_weights or {}
    
    def consensus_probability(
        self,
        forecasts: list[SourceForecast],
        temp_low_c: float,
        temp_high_c: float
    ) -> tuple[float, float]:
        """
        Compute consensus probability and confidence.
        Returns (probability, confidence_score).
        """
        if not forecasts:
            return 0.5, 0.0
        
        # Normalize weights
        total_w = sum(f.weight for f in forecasts)
        probs = []
        for f in forecasts:
            w = f.weight / total_w
            p_low = stats.norm.cdf(temp_low_c, loc=f.temp_high_c, scale=max(f.std_c, 0.5))
            p_high = stats.norm.cdf(temp_high_c, loc=f.temp_high_c, scale=max(f.std_c, 0.5))
            probs.append(w * (p_high - p_low))
        
        consensus_p = sum(probs)
        
        # Confidence: measure of agreement between sources
        # High agreement → high confidence; disagreement → low confidence
        means = [f.temp_high_c for f in forecasts]
        spread = float(np.std(means)) if len(means) > 1 else 0.0
        confidence = max(0.0, 1.0 - spread / 5.0)  # 5°C spread → 0 confidence
        
        return consensus_p, confidence
```

---

## 4. Edge Calculation with Multiple Sources

### 4a. When 3+ Sources Agree → Higher Edge

When multiple independent sources converge on the same temperature bucket, the edge (forecast probability - market price) is more reliable:

```python
def multi_source_edge(
    forecasts: list[SourceForecast],
    market_yes_price: float,
    temp_low_c: float,
    temp_high_c: float
) -> tuple[float, float]:
    """
    Compute edge with multiple source confirmation.
    Returns (edge, adjusted_edge).
    """
    consensus_p, confidence = MultiSourceAggregator().consensus_probability(
        forecasts, temp_low_c, temp_high_c
    )
    
    raw_edge = consensus_p - market_yes_price
    
    # Scale edge by confidence (source agreement)
    # If 3/3 sources agree → confidence ~1.0 → full edge
    # If 2/3 agree, 1 outlier → confidence ~0.5 → halved edge
    adjusted_edge = raw_edge * confidence
    
    return raw_edge, adjusted_edge
```

### 4b. Source Disagreement as Uncertainty Signal → Reduce Position

When sources disagree significantly, position sizing should be reduced:

```python
def disagreement_adjusted_size(
    base_size: float,
    forecasts: list[SourceForecast],
    max_spread_c: float = 3.0
) -> float:
    """
    Reduce position size when sources disagree.
    
    max_spread_c: temperature spread (°C) at which size goes to 0.
    """
    if len(forecasts) < 2:
        return base_size * 0.5  # single source = less confident
    
    temps = [f.temp_high_c for f in forecasts]
    spread = max(temps) - min(temps)
    
    if spread >= max_spread_c:
        return 0.0  # too much disagreement, skip
    
    # Linear scaling: 0°C spread → full size, max_spread → 0
    scale = 1.0 - (spread / max_spread_c)
    return base_size * scale
```

### 4c. Consensus Probability from Multiple Forecasts

The consensus probability combines BMA-weighted individual source probabilities:

```python
def compute_consensus(
    sources: list[dict],  # [{name, temp, std, weight}]
    bucket_low: float,
    bucket_high: float
) -> dict:
    """
    Full consensus computation from multiple sources.
    """
    # 1. Individual probabilities per source
    individual_probs = []
    for s in sources:
        p = (stats.norm.cdf(bucket_high, s['temp'], s['std'])
             - stats.norm.cdf(bucket_low, s['temp'], s['std']))
        individual_probs.append(p)
    
    # 2. BMA-weighted consensus probability
    total_w = sum(s['weight'] for s in sources)
    consensus = sum(
        s['weight'] / total_w * p
        for s, p in zip(sources, individual_probs)
    )
    
    # 3. Confidence interval from source spread
    temps = [s['temp'] for s in sources]
    temp_std = float(np.std(temps)) if len(temps) > 1 else 0.0
    ci_low = max(0.0, consensus - temp_std / 2.0)
    ci_high = min(1.0, consensus + temp_std / 2.0)
    
    # 4. Directional consensus
    n_above = sum(1 for t in temps if t > bucket_high)
    n_below = sum(1 for t in temps if t < bucket_low)
    n_in = len(temps) - n_above - n_below
    
    return {
        'consensus_probability': consensus,
        'confidence_interval': (ci_low, ci_high),
        'source_agreement': f"{n_in}/{len(sources)} in bucket",
        'spread_c': temp_std,
        'individual_probs': dict(zip([s['name'] for s in sources], individual_probs)),
    }
```

### 4d. Confidence Intervals from Multi-Source Spread

```python
def spread_confidence_interval(
    forecasts: list[SourceForecast],
    level: float = 0.95
) -> tuple[float, float]:
    """
    Compute confidence interval for the true temperature
    using the spread across sources.
    
    Uses the BMA predictive distribution.
    """
    means = [f.temp_high_c for f in forecasts]
    stds = [f.std_c for f in forecasts]
    weights = [f.weight for f in forecasts]
    total_w = sum(weights)
    weights = [w / total_w for w in weights]
    
    bma_mean, bma_var = bma_temperature(means, stds, weights)
    bma_std = bma_var ** 0.5
    
    alpha = (1 - level) / 2
    ci_low = bma_mean + stats.norm.ppf(alpha) * bma_std
    ci_high = bma_mean + stats.norm.ppf(1 - alpha) * bma_std
    
    return ci_low, ci_high
```

---

## 5. Historical Accuracy Comparison

### 5a. Known Studies and Benchmarks

**Open-Meteo Model Accuracy (from their documentation):**

Open-Meteo does not publish a formal benchmark page (the `/docs/benchmark` URL returns 404). However, the underlying model accuracy is well-studied:

| Model | Source | Typical 1-3 Day RMSE (°C) | Notes |
|---|---|---|---|
| ECMWF IFS HRES | ECMWF | 1.0–1.5 | Gold standard for medium-range; best for day 3–7 |
| GFS (NCEP) | NOAA | 1.5–2.0 | Good for day 1–3; widely available |
| HRRR | NOAA | 0.8–1.2 (day 1) | Best for <18hr US forecasts; hourly updates |
| ICON (DWD) | Germany | 1.0–1.5 | Excellent for Europe |
| UKMO | UK Met Office | 1.2–1.8 | Strong for UK/Europe |
| JMA | Japan | 1.5–2.0 | Best for East Asia |
| NBM (National Blend) | NOAA | 0.8–1.3 | Multi-model blend; best overall US accuracy |

**Key finding:** ECMWF is consistently ranked #1 globally for medium-range (day 3-15) temperature forecasts. GFS/HRRR are competitive for short-range (day 1-2) US forecasts.

### 5b. Open-Meteo vs NOAA vs Other: Relative Accuracy

**For US temperature markets (the bulk of Polymarket weather):**

1. **Day-of (0-24hr):** HRRR > NBM > GFS ≈ NWS API > OpenWeatherMap
   - HRRR updates hourly and has 3km resolution over CONUS
   - NWS API uses NBM blend which is statistically best

2. **Day 1-3:** ECMWF > GFS > NWS API > OpenWeatherMap ≈ WeatherAPI
   - ECMWF available via Open-Meteo (`ecmwf_ifs025` model)
   - Open-Meteo's `best_match` automatically selects the best model per location

3. **Day 3-7:** ECMWF >> GFS > all others
   - ECMWF's advantage grows with lead time

**Geographic accuracy differences:**
- **US CONUS:** Best served by HRRR + GFS + NBM (Open-Meteo and NWS API both have these)
- **Europe:** Best served by ECMWF + ICON + UKMO (Open-Meteo has all; NWS doesn't cover)
- **East Asia:** Best served by JMA + KMA (Open-Meteo has both)
- **Southern Hemisphere:** ECMWF + GFS (Open-Meteo; limited local models)

### 5c. Source-Specific Accuracy for Polymarket Cities

Based on the project's `CITY_COORDS` in `pm_bot/models/config.py`:

| City | Best Primary Source | Best Secondary Source | Notes |
|---|---|---|---|
| NYC | Open-Meteo (HRRR+GFS) | NWS API + AWC METAR (KLGA) | Both free; METAR for resolution |
| London | Open-Meteo (ECMWF+UKMO) | Visual Crossing | No NWS; UKMO native |
| Hong Kong | Open-Meteo (ECMWF+GFS) | WeatherAPI | Limited local models |
| Miami | Open-Meteo (HRRR+GFS) | NWS API + AWC METAR (KMIA) | HRRR covers Florida |
| Dallas | Open-Meteo (HRRR+GFS) | NWS API + AWC METAR (KDFW) | HRRR covers Texas |
| Atlanta | Open-Meteo (HRRR+GFS) | NWS API + AWC METAR (KATL) | HRRR covers Georgia |
| Seoul | Open-Meteo (ECMWF+KMA) | WeatherAPI | KMA native model |
| Tokyo | Open-Meteo (ECMWF+JMA) | WeatherAPI | JMA native model |
| LA | Open-Meteo (HRRR+GFS) | NWS API + AWC METAR (KLAX) | HRRR covers California |
| Chicago | Open-Meteo (HRRR+GFS) | NWS API + AWC METAR (KORD) | HRRR covers Illinois |
| Paris | Open-Meteo (ECMWF+AROME) | Visual Crossing | Météo-France native |
| São Paulo | Open-Meteo (ECMWF+GFS) | WeatherAPI | Limited local models |
| Buenos Aires | Open-Meteo (ECMWF+GFS) | WeatherAPI | Limited local models |

### 5d. Recommended Multi-Source Stack for PM Bot

For **US cities** (8 of 18 default cities):
1. **Primary:** Open-Meteo (HRRR for day-of, GFS for day 1-3, ECMWF for day 3+)
2. **Secondary:** NWS API (hourly forecast + station observations)
3. **Resolution source:** AWC METAR (hourly airport observations for confirmation)

For **non-US cities**:
1. **Primary:** Open-Meteo (ECMWF + local model: UKMO, JMA, KMA, etc.)
2. **Secondary:** Visual Crossing (free tier, good historical + forecast)
3. **Tertiary:** WeatherAPI.com (free tier, 3-day forecast)

---

## Files Found in Codebase

| File Path | Description |
|---|---|
| `pm_bot/core/weather.py` | Current Open-Meteo implementation (single source) |
| `pm_bot/core/polymarket.py` | Market fetching, airport code extraction |
| `pm_bot/models/market.py` | `ForecastResult`, `TemperatureBucket` data models |
| `pm_bot/models/config.py` | `CITY_COORDS`, strategy defaults, cache TTL |
| `pm_bot/core/parser.py` | Temperature bucket parsing from market questions |
| `pm_bot/cli/trade.py` | `fetch_forecast_at()` — duplicate of weather.py logic |
| `.trellis/tasks/00-bootstrap-guidelines/research/weather-data-apis.md` | Prior API research (Open-Meteo, NOAA, METAR, WU) |

## Related Spec Documents

- `.trellis/tasks/00-bootstrap-guidelines/research/weather-data-apis.md` — prior research covering Open-Meteo, NOAA NWS, METAR sources, WU status
- `pm_bot/models/market.py` — `ForecastResult` model has `members` field for ensemble data but no multi-source fields yet

## Not Found

- No formal published study directly comparing Open-Meteo vs NOAA vs other APIs' temperature forecast RMSE for the same locations (Open-Meteo's benchmark page is 404)
- No open-source Python library specifically for multi-source weather forecast aggregation (BMA implementations exist in academic packages but not as weather-specific tools)
- Polymarket's exact UMA oracle resolution mechanism (which WU endpoint or page is scraped) is not documented publicly
- Historical RMSE per model per city would need to be computed from Open-Meteo's Historical Forecast API + actual observations
