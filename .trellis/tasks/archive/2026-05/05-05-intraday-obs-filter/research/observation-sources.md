# Real-Time Temperature Observation Sources for Polymarket Weather Trading

Research date: 2026-05-05

---

## 1. METAR API (Aviation Weather Center / NOAA)

### Primary API: AWC Data API

- **Base URL**: `https://aviationweather.gov/api/data/metar`
- **Cost**: **Free** — no API key required
- **Formats**: JSON, CSV, XML, GeoJSON, Raw text, IWXXM
- **Rate limit**: 100 requests/minute; no more than 1 request/min per thread
- **Update frequency**: **Once per minute** (cache files updated every 60 seconds)
- **Historical depth**: Up to 15 days
- **Max results per query**: 400 entries

### Example Request

```
GET https://aviationweather.gov/api/data/metar?ids=KLGA&format=json&taf=false
```

Returns current METAR for LaGuardia Airport in JSON with decoded fields.

### Cache Files (Bulk Download)

- `https://aviationweather.gov/data/cache/metars.cache.csv.gz` — all current METARs (CSV), updated every minute
- `https://aviationweather.gov/data/cache/metars.cache.xml.gz` — all current METARs (XML), updated every minute

These are recommended for bulk queries instead of per-station API calls.

### Latency

- METAR observations are issued **once per hour** at approximately :52-:55 past the hour (routine METARs)
- **SPECI** (special) METARs are issued when conditions change rapidly
- The AWC cache updates every 1 minute, so worst-case latency from observation to API availability is **~1-2 minutes**
- Total observation latency: **~5-60 minutes** depending on when in the hour the observation was taken

### Temperature in METAR

METAR reports temperature in **whole degrees Celsius** in the body (e.g., `04/M08` = 4°C / -8°C dewpoint). US stations include **tenths of a degree** in the remarks section (e.g., `T00441078` = 4.4°C temp, -7.8°C dewpoint).

### Temperature Precision

- **Body**: whole degrees °C only (rounded)
- **Remarks (US stations)**: tenths of °C
- Polymarket resolves in **whole degrees Fahrenheit** — the METAR body precision (1°C ≈ 1.8°F) is insufficient for precise resolution; remarks data (0.1°C ≈ 0.18°F) is better but still rounds to whole °F

### Key Limitation

METAR observations are **hourly** — they capture a snapshot, not the continuous max. A short-lived temperature spike between METAR reports could be missed. However, SPECI reports capture significant changes.

---

## 2. Open-Meteo Observations

### Does Open-Meteo Have Real Observations?

**No.** Open-Meteo does **not** provide actual ground-station observed temperature data. Their APIs are:

| API | What It Provides | Real Observations? |
|-----|------------------|--------------------|
| `/v1/forecast` | Weather model forecasts + "current conditions" | **No** — "current conditions" are based on 15-min weather model data (HRRR/ICON-D2/AROME), not station observations |
| Historical API | Reanalysis/era5-derived data | **No** — model-derived, not raw observations |
| Ensemble API | Model ensemble forecasts | **No** |

### The "current" Parameter

The `current=temperature_2m` parameter on the forecast API returns **model-estimated current temperature**, not an actual station observation. Per the docs:

> "Current conditions are based on 15-minutely weather model data."

This is useful as a rough estimate but **cannot be trusted** for Polymarket resolution, which uses actual Wunderground station observations.

### Verdict

Open-Meteo is **not suitable** as a primary observation source for a trading bot. It could serve as a supplementary forecast/estimate but never as the definitive observation source.

---

## 3. Weather Underground API

### Current Status

Weather Underground **shut down their free API** years ago. The current situation:

- **Free API**: Only available to **active PWS (Personal Weather Station) owners/uploaders** — you must operate a weather station to get a free key
- **Paid API**: Previously offered at various tiers, but the API program has been significantly restricted
- **No public pricing page** accessible — the old `wunderground.com/weather/api/d/pricing.htm` redirects to a login page

### Alternative: Scraping Wunderground History Page

Since Polymarket resolves from the Wunderground **History** tab (e.g., `https://www.wunderground.com/history/daily/us/ny/new-york-city/KLGA`), one approach is scraping this page. However:

- The History page data is **not finalized until the next day** typically
- Wunderground has anti-bot protections
- Data appears to come from the same airport ASOS stations that METAR reports from

### Wunderground Data Sources

Per Wunderground's own documentation:
- ~2,000 ASOS stations at US airports (FAA-maintained, hourly or more frequent)
- 250,000+ PWS stations (quality-controlled, updates as often as every 2.5 seconds)
- 26,000+ MADIS stations (NOAA-managed)
- International: ~6,000 airport stations (1, 3, or 6 hour intervals)

### For Trading Bot

**Wunderground is not directly usable** via API without a PWS owner key. The resolution source is Wunderground's History page, but that data isn't finalized until after the day ends. For real-time intraday observation, you need the **underlying ASOS/METAR data** directly.

---

## 4. Peak Temperature Timing

### When Does the Daily High Occur?

Based on meteorological research:

| Climate Type | Typical Peak Time (Local) | Notes |
|-------------|--------------------------|-------|
| Continental (inland US) | **3:00 PM - 5:00 PM** | Largest diurnal range; thermal lag 3-5 hrs after solar noon |
| Desert (arid) | **4:00 PM - 5:30 PM** | Greatest thermal lag due to dry soil heating |
| Maritime/coastal | **1:00 PM - 3:00 PM** | Smaller range; ocean moderates lag |
| Tropical humid | **2:00 PM - 4:00 PM** | Smaller diurnal range (~4-7°C), cloud cover can shift earlier |
| Urban heat islands | **4:00 PM - 6:00 PM** | Concrete/asphalt retains heat longer |

### Key Research Findings

1. **Seidel et al. (2005, JGR)**: Surface diurnal cycle peaks "a few hours after local noon" — mean peak around **15:00 LST (3 PM local solar time)** over land.

2. **Gough (2022, "Diurnal Extrema Timing")**: Introduced DET (Diurnal Extrema Timing) as a climate parameter. Found that the timing of daily maximum temperature is **the most vulnerable to climate change** among temperature indices. Average timing around **3-4 PM local** with seasonal variation.

3. **ERA5 analysis (2023, Climate Dynamics)**: Global analysis shows surface air temperature maximum around **1500 LST (3 PM)** over land, with amplitude 3-4°C. Over ocean, much weaker (~0.7-0.8°C amplitude).

4. **Australian Bureau of Meteorology**: "The highest temperature recorded at stations between 9am and 3pm is a good guide to the maximum temperature recorded for a day." This suggests **by 3 PM local, you can be reasonably confident** the daily high has been observed (at least for Australian stations).

5. **Wikipedia (Diurnal temperature variation)**: "Equilibrium is usually reached from 3–5 p.m., but this may be affected by a variety of factors such as large bodies of water, soil type and cover, wind, cloud cover/water vapor, and moisture on the ground."

### When Is It Safe to Assume the Daily High Has Been Reached?

| Time (Local) | Confidence Level | Reasoning |
|-------------|-----------------|-----------|
| 2:00 PM | **Low** (~60-70%) | Peak hasn't occurred yet in most continental/desert climates |
| 3:00 PM | **Medium** (~75-85%) | Peak typically near for most climates; maritime stations likely peaked |
| 4:00 PM | **Medium-High** (~85-92%) | Most locations have peaked; desert/urban may still be rising |
| 5:00 PM | **High** (~92-97%) | Almost all locations have peaked; rare late spikes possible |
| 6:00 PM | **Very High** (~97%+) | Exceptionally rare to see new daily max after 6 PM local |

### Exceptions & Edge Cases

- **Cold fronts**: Can cause late-day temperature spikes before a sharp drop
- **Foehn/Chinook winds**: Can cause rapid warming at any time of day
- **Cloud burst clearing**: Sudden clearing after overcast can cause late-afternoon surge
- **Tropical convection**: Afternoon thunderstorms can suppress the peak early

### Practical Rule for Trading

**After 5:00 PM local time, the daily high is almost certainly set.** For US stations (most common on Polymarket), this means:
- EST: high reached by ~5 PM EST = 22:00 UTC
- CST: high reached by ~5 PM CST = 23:00 UTC  
- MST: high reached by ~5 PM MST = 00:00 UTC (next day)
- PST: high reached by ~5 PM PST = 01:00 UTC (next day)

---

## 5. Polymarket Market Timing

### Resolution Source

**Weather Underground History tab** for specific airport stations.

Example: NYC markets use **LaGuardia Airport (KLGA)**:
> "The resolution source for this market will be information from Wunderground, specifically the highest temperature recorded for all times on this day by the Forecast for the LaGuardia Station once information is finalized."

URL: `https://www.wunderground.com/history/daily/us/ny/new-york-city/KLGA`

### Trading Day Window

Per wethr.net analysis of platform differences:

| Platform | Time Standard | Trading Day Window |
|----------|--------------|-------------------|
| **Polymarket** | **Local Time (clock time)** | **12:00 AM – 11:59 PM local time, year-round** |
| Kalshi | Local Standard Time | Shifts during DST |
| Robinhood | Local Time | 12:00 AM – 11:59 PM |
| IBKR | Local Time | 12:00 AM – 11:59 PM |

### endDate Field

The API returns `endDate` as `YYYY-MM-DDT12:00:00Z` (noon UTC) for weather markets. Per GitHub issue #331 on `Polymarket/clob-client`, this is **misleading** — it does not represent the actual market end time. The market remains open until resolution.

### Resolution Timing

Markets resolve **after all data for the date has been finalized**. The rules state:
> "This market can not resolve to 'Yes' until all data for this date has been finalized."

In practice, this means resolution typically happens **the next day** (often early morning UTC) after Wunderground finalizes the historical data.

### Temperature Precision

Polymarket resolves in **whole degrees Fahrenheit**. The resolution source measures "temperatures to whole degrees Fahrenheit (e.g., 21°F)."

### Market Structure

Each market resolves to a **temperature range** (e.g., "70-71°F"), not a single degree. Typical ranges cover 2°F bins with "or below" / "or above" anchors.

---

## 6. Python Libraries for METAR Data

### avwx-engine (Recommended)

| Attribute | Value |
|-----------|-------|
| PyPI package | `avwx-engine` |
| Import | `import avwx` |
| Python | ≥3.10 |
| License | MIT |
| Stars | 117 |
| Latest release | 1.9.8 (2026-02-16) |
| Status | Production/Stable |

```python
from avwx import Metar

metar = avwx.Metar('KLGA')
metar.update()
# Access parsed data:
metar.raw                          # Raw METAR string
metar.data.temperature             # Parsed temperature
metar.data.dewpoint                # Parsed dewpoint
metar.data.flight_rules            # VFR/MVFR/IFR/LIFR
metar.station.name                 # Station metadata
```

**Key features**:
- Fetches from NOAA AWC and other localized sources automatically
- Parses all METAR elements including remarks (tenths-of-degree temperatures for US stations)
- Calculates flight rules, translations, speech summaries
- Supports METAR, TAF, PIREP, AIRMET/SIGMET, NOTAM, NBM, GFS
- Auto-selects best regional source per station

### python-metar (Alternative)

| Attribute | Value |
|-----------|-------|
| PyPI package | `metar` |
| Import | `from metar import Metar` |
| Python | ≥3.10 |
| Stars | 292 |
| Status | Production/Stable |

```python
from metar import Metar

obs = Metar.Metar('KLGA 051651Z 33021G25KT 10SM FEW060 18/M08 A3054 RMK AO2 T01781078')
obs.temp        # Temperature object (°C)
obs.temp.value("F")  # In Fahrenheit
obs.dewpt       # Dewpoint object
obs.max_temp_24hr  # 24-hour max temp from remarks
obs.min_temp_24hr  # 24-hour min temp from remarks
```

**Key features**:
- Pure parser (does not fetch — you provide the raw METAR string)
- Parses US remark groups including min/max temperature
- Well-established (original code from 2004)
- More basic than avwx but sufficient for parsing

### Direct AWC API (No Library Needed)

```python
import requests

resp = requests.get(
    "https://aviationweather.gov/api/data/metar",
    params={"ids": "KLGA", "format": "json", "taf": "false"}
)
data = resp.json()
# data[0]["rawOb"]  — raw METAR string
# data[0]["temp"]   — temperature in °C (float)
# data[0]["dewpt"]  — dewpoint in °C (float)
# data[0]["obsTime"] — observation time
```

The AWC API returns **pre-decoded JSON** with temperature as a numeric field, so you don't strictly need a parsing library.

---

## 7. Practical Recommendation Summary

### Best Source for Trading Bot (Sub-5min Latency)

| Rank | Source | Latency | Cost | Pros | Cons |
|------|--------|---------|------|------|------|
| **1** | **AWC METAR API** | 1-60 min (hourly obs cycle) | Free | Official, pre-decoded JSON, global airport coverage | Hourly snapshot, can miss spikes between reports |
| **2** | **AWC Cache CSV** | ~1 min | Free | Bulk download all stations, fastest access | Same hourly observation limitation |
| **3** | **MetarCentral API** | 5 min | Free (1K req/day) | No auth needed, 10K+ airports | Just a wrapper around AWC, adds latency |
| ❌ | Open-Meteo "current" | ~1 hr | Free | Easy API | Model data, not real observations — **unusable** |
| ❌ | Wunderground API | N/A | PWS owners only | Would match resolution source | **Not publicly available** |

### Recommended Architecture

1. **Primary**: Poll AWC METAR API every 5 minutes for target ICAO stations
   - `https://aviationweather.gov/api/data/metar?ids=KLGA,KJFK,KORD&format=json`
   - Track the max temperature seen across all METARs for the current local day
   
2. **Enhancement**: Parse remarks for US stations to get 0.1°C precision
   - `T01781078` → 17.8°C temp, -7.8°C dewpoint (more precise than body)

3. **Confidence Filter**: Only consider the daily high "locked in" after 5 PM local time
   - Before 5 PM: METAR observations provide an evolving estimate of the daily high
   - After 5 PM: with high confidence, the max METAR temp ≈ the daily high
   - After 6 PM: very high confidence

4. **Resolution Alignment**: Remember that Polymarket resolves from **Wunderground**, which reports slightly differently from raw METAR
   - Wunderground may include 6-hour max data from NWS that METAR doesn't capture
   - Wunderground rounds to whole °F; so does Polymarket
   - Occasional 1°F discrepancy between METAR-derived max and Wunderground max

---

## 8. Additional Data Sources Worth Noting

### Iowa Environmental Mesonet (IEM)

- **ASOS 1-minute data**: `https://mesonet.agron.iastate.edu/cgi-bin/request/asos1min.py`
- Provides 1-minute ASOS observations with ~24 hour delay (NCEI archival)
- **Not real-time** but excellent for historical analysis/backtesting

### NCEI ISD (Integrated Surface Dataset)

- REST API: `https://www.ncei.noaa.gov/access/services/data/v1`
- Historical hourly observations globally
- Free, but not real-time (days to weeks lag)

### MADIS 1-Minute ASOS (OMO)

- One Minute Observations via MADIS
- US-only, CONUS coverage
- **Not easily accessible via public API** — requires LDM or special access
- Would be ideal for sub-hourly observation but access is limited

