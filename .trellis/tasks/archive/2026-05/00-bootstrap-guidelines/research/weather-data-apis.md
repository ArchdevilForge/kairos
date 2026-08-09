# Research: Weather Data APIs

- **Query**: Free APIs for temperature forecasts (Open-Meteo, NOAA, Wunderground, METAR)
- **Scope**: External
- **Date**: 2026-05-04

## Findings

### 1. Open-Meteo API (RECOMMENDED - Primary Source)

Open-Meteo is an open-source weather API offering free access for non-commercial use. **No API key required.**

**Base URLs by endpoint type:**

| API | Base URL | Purpose |
|---|---|---|
| Forecast API | `https://api.open-meteo.com/v1/forecast` | Current + up to 16-day forecast |
| Historical Weather API | `https://archive-api.open-meteo.com/v1/archive` | 80+ years of historical data, 10km resolution |
| Historical Forecast API | `https://historical-forecast-api.open-meteo.com/v1/forecast` | Archived past forecasts (2-5 years) |
| Ensemble API | `https://ensemble-api.open-meteo.com/v1/ensemble` | Ensemble member data (GFS 31-member) |
| Previous Runs API | `https://previous-runs-api.open-meteo.com/v1/forecast` | Past model runs for forecast comparison |
| Single Runs API | `https://single-runs-api.open-meteo.com/v1/forecast` | Individual model run access |

**Forecast API key parameters:**

| Parameter | Type | Required | Default | Description |
|---|---|---|---|---|
| `latitude` | float | Yes | - | WGS84 latitude |
| `longitude` | float | Yes | - | WGS84 longitude |
| `hourly` | string[] | No | - | Hourly weather variables |
| `daily` | string[] | No | - | Daily weather aggregations |
| `current` | string[] | No | - | Current conditions |
| `past_days` | int (0-92) | No | 0 | Past days to include |
| `forecast_days` | int (0-16) | No | 7 | Forecast days (up to 16) |
| `timezone` | string | No | GMT | Timezone (use `auto` for local) |
| `temperature_unit` | string | No | celsius | `celsius` or `fahrenheit` |
| `models` | string | No | best | Weather model selection |

**Key temperature variables:**

| Variable | Description | Available In |
|---|---|---|
| `temperature_2m` | Air temperature at 2m above ground (hourly) | Forecast, Historical |
| `temperature_2m_max` | Maximum daily temperature at 2m | Daily aggregation |
| `temperature_2m_min` | Minimum daily temperature at 2m | Daily aggregation |
| `temperature_2m_mean` | Mean daily temperature at 2m | Daily (historical) |
| `apparent_temperature` | Feels-like temperature | Forecast |
| `dewpoint_2m` | Dew point at 2m | Forecast |

**Example calls:**
```bash
# Current + hourly forecast for NYC (Central Park area)
curl "https://api.open-meteo.com/v1/forecast?latitude=40.7128&longitude=-74.0060&current=temperature_2m&hourly=temperature_2m,relative_humidity_2m,wind_speed_10m&temperature_unit=fahrenheit&timezone=auto&forecast_days=7"

# Daily high/low for 16-day forecast
curl "https://api.open-meteo.com/v1/forecast?latitude=40.7128&longitude=-74.0060&daily=temperature_2m_max,temperature_2m_min&temperature_unit=fahrenheit&timezone=auto&forecast_days=16"

# Historical data for a specific date range
curl "https://archive-api.open-meteo.com/v1/archive?latitude=40.7128&longitude=-74.0060&start_date=2025-05-01&end_date=2025-05-31&daily=temperature_2m_max,temperature_2m_min&temperature_unit=fahrenheit&timezone=auto"

# GFS ensemble (31 members) for probabilistic forecasting
curl "https://ensemble-api.open-meteo.com/v1/ensemble?latitude=40.7128&longitude=-74.0060&hourly=temperature_2m&temperature_unit=fahrenheit&timezone=auto&models=gfs_seamless"
```

**Weather models available:**
- `gfs_seamless` -- GFS (Global Forecast System, US)
- `ecmwf_ifs025` -- ECMWF IFS 0.25 degree (European, considered best for medium-range)
- `ecmwf_aifs` -- ECMWF AI forecast
- `jma_seamless` -- Japan Meteorological Agency
- `metno_seamless` -- MET Norway
- `ukmo_seamless` -- UK Met Office
- `cfs_seamless` -- Climate Forecast System
- `gfs_hrrr` -- HRRR (High-Resolution Rapid Refresh, US, hourly updates, dominant inside 18hr window)

**Open-Meteo SDK (FlatBuffers):**
- GitHub: https://github.com/open-meteo/sdk
- Provides FlatBuffers schemas for efficient binary data transfer
- Add `&format=flatbuffers` to any URL for binary response

### 2. NOAA / NWS API (US Government - Free)

The National Weather Service API provides free access to forecasts, alerts, and observations for US locations.

**Base URL:** `https://api.weather.gov`

**Key endpoints:**

| Endpoint | Description |
|---|---|
| `GET /points/{lat},{lon}` | Get gridpoint metadata for a location |
| `GET /gridpoints/{wfo}/{x},{y}/forecast` | 12h period forecast (7 days) |
| `GET /gridpoints/{wfo}/{x},{y}/forecast/hourly` | Hourly forecast (7 days) |
| `GET /gridpoints/{wfo}/{x},{y}` | Raw gridded forecast data |
| `GET /stations` | List observation stations |
| `GET /stations/{stationId}/observations` | Recent observations from a station |

**Rate limits:** Undocumented but "generous" for typical use. Retry after 5 seconds if rate-limited.

**Example workflow:**
```bash
# Step 1: Get gridpoint info for NYC
curl -H "Accept: application/json" "https://api.weather.gov/points/40.7128,-74.0060"
# Returns: forecast, forecastHourly, forecastGridData URLs, observationStations URL

# Step 2: Fetch hourly forecast
curl "https://api.weather.gov/gridpoints/OKX/33,35/forecast/hourly"

# Step 3: Get current observations from nearest station
curl "https://api.weather.gov/stations/KLGA/observations"
```

**Notes:**
- Resolution: ~2.5km x 2.5km grid
- No API key required
- Must set `Accept: application/json` header (or `application/vnd.noaa.dwml+xml` for legacy XML)
- OpenAPI spec available: `https://api.weather.gov/openapi.json` or `.yaml`
- No geocoding -- requires lat/lon input
- Station observations show max/min temperatures (bug: 24h max/min may be null outside Central Time)

### 3. METAR Data Sources (Airport Station Observations)

METAR (Meteorological Aerodrome Report) is the standard format for hourly airport weather observations. This is the actual data source used for Polymarket weather market resolution.

#### 3a. Aviation Weather Center (AWC) API (Official US Government - Free)

**Base URL:** `https://aviationweather.gov/api/data`

**Key endpoints:**

| Endpoint | Description | Format |
|---|---|---|
| `GET /metar?ids={ICAO}&format=json` | Latest METAR for an airport | JSON, GeoJSON, CSV, XML |
| `GET /taf?ids={ICAO}&format=json` | Latest TAF (terminal forecast) | JSON, GeoJSON, XML |
| `GET /station?ids={ICAO}&format=json` | Station info | JSON, GeoJSON, XML |

**Example:**
```bash
# Latest METAR for LaGuardia Airport (KLGA) - NYC resolution station
curl "https://aviationweather.gov/api/data/metar?ids=KLGA&format=json"

# Latest METAR for JFK
curl "https://aviationweather.gov/api/data/metar?ids=KJFK&format=json"
```

**Coverage:** Worldwide, all ICAO-coded airports. Historical data: up to previous 15 days.

#### 3b. MetarCentral API (Free - No API Key Required for Basic)

**Base URL:** `https://www.metarcentral.com/api`

**Endpoints:**

| Endpoint | Description |
|---|---|
| `GET /api/weather/{icao}` | Current METAR, decoded weather, flight rules |
| `GET /api/search?q={query}` | Search airports by ICAO, IATA, name, city |
| `GET /api/airports` | List all airports with coordinates |
| `GET /api/airports/metar-status` | Airports with current METAR status by region |

**Tiers:**
- Anonymous: No API key, results capped at 10 per request
- Free API key: Full results (up to 1,000), free forever

#### 3c. CheckWX API (Free tier available)

**Website:** https://checkwxapi.com/

- Free: 3,000 requests/day, requires API key (no credit card)
- Endpoints: METAR by ICAO, nearest, within radius, by country/state, history
- Fully decoded JSON with temperature in Celsius and Fahrenheit

#### 3d. AviationWX.org (Free tier)

**Base URL:** `https://api.aviationwx.org/v1`

**Endpoints:**

| Endpoint | Description |
|---|---|
| `GET /airports` | List all airports |
| `GET /airports/{id}/weather` | Current weather for airport |
| `GET /airports/{id}/weather/history` | 24-hour rolling weather history |
| `GET /weather/bulk?airports=a,b,c` | Weather for up to 10 airports |

**Rate limits:**
- Anonymous: 50/min, 500/hr, 2,000/day
- With API key: 500/min, 5,000/hr, 50,000/day

#### 3e. SkyLink METAR API (via RapidAPI)

**Website:** https://skylinkapi.com

- 1,000 free requests/month via RapidAPI
- Endpoints: `GET /v2/weather/metar/{icao}`, `GET /v2/weather/taf/{icao}`
- Returns raw text + fully decoded JSON
- Global coverage, 100+ countries

### 4. Weather Underground (Wunderground)

**Status: LARGELY DEPRECATED / RESTRICTED**

Weather Underground's API has been significantly restricted since being acquired by The Weather Company (IBM).

**Current state:**
- The old WUnderground API (`api.wunderground.com`) is no longer publicly available for new registrations
- Historical data access requires a WU/TWC API key, which is **only available to users who own weather stations submitting data to WU**
- The WUnderground website (`wunderground.com/history/`) has a historical data archive searchable by city/airport/date, but there is no official API
- Polymarket weather markets resolve against Wunderground data (specifically airport station readings), but there is no programmatic API for that data anymore

**Workarounds for WU data:**
- Scrape `https://www.wunderground.com/history/daily/{airport_code}/date/{year}-{month}-{day}` (fragile, not recommended)
- Use Visual Crossing Weather API (https://www.visualcrossing.com/weather-api) which provides WUnderground-compatible historical data with a free tier
- Use NOAA/NWS station observations as a close substitute
- Some open-source bots (alteregoeth-ai/weatherbot) use Visual Crossing as a WU replacement

**Visual Crossing Weather API (WU alternative):**
- Free tier available with API key
- Provides historical daily/hourly weather data
- Used by several Polymarket weather bots as a resolution data source
- URL: https://www.visualcrossing.com/weather-api

### API Comparison Matrix

| API | Auth Required | Forecast | Historical | Real-time Obs | Multi-model | Cost | Best For |
|---|---|---|---|---|---|---|---|
| Open-Meteo Forecast | No | 16-day | 92 days past | No | Yes (GFS,ECMWF,HRRR,etc.) | Free | Primary forecast source |
| Open-Meteo Ensemble | No | 16-day | No | No | Yes (31+ members) | Free | Probabilistic forecasting |
| Open-Meteo Historical | No | No | 80+ years | No | No | Free | Historical backtesting |
| NWS API | No | 7-day | No | Yes (US stations) | No | Free | US forecasts + observations |
| AWC METAR | No | No | 15 days | Yes (global airports) | No | Free | Airport station readings (resolution data) |
| MetarCentral | Optional | No | No | Yes (global airports) | No | Free | Decoded METAR |
| CheckWX | Yes (free key) | No | No | Yes (global airports) | No | Free (3K/day) | Decoded METAR + history |
| Visual Crossing | Yes (free key) | 15-day | Yes | Yes | No | Free tier | Historical/resolution data |

### Related Specs

- None yet (new project)

## Caveats / Not Found

- Wunderground API is effectively deprecated for public use; no new API keys are being issued except to PWS owners
- Polymarket resolves against Wunderground data but the exact resolution mechanism (UMA oracle -> WU scrape) is not fully documented
- The Open-Meteo ensemble API returns individual member forecasts which can be counted to create probability distributions across temperature buckets -- this is the core mechanism used by most open-source bots
- Open-Meteo uses airport ICAO coordinates for accurate station-level forecasts (important for matching resolution locations)
- NWS API requires `Accept: application/json` header or returns HTML error
- METAR temperatures are always in Celsius in raw format; decoded APIs may offer Fahrenheit conversion
