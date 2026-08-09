"""Fetch real historical market data from Polymarket and Open-Meteo APIs.

Data sources (all free, no auth required):
- Gamma API: resolved weather events + winning outcomes (single pagination pass)
- Open-Meteo previous-runs: what forecasts actually said on a given date (batch per city)
"""

from __future__ import annotations

import json
import re
import sqlite3
from dataclasses import dataclass, field
from datetime import datetime, timedelta, timezone
from pathlib import Path
from time import monotonic
from typing import Any

import httpx
import structlog

from pm_bot.core.db import DEFAULT_DB_PATH
from pm_bot.models.config import CITY_COORDS, resolve_city_alias
from pm_bot.models.market import ForecastResult

log = structlog.get_logger()

GAMMA_BASE = "https://gamma-api.polymarket.com"
CLOB_BASE = "https://clob.polymarket.com"
PREVIOUS_RUNS_URL = "https://previous-runs-api.open-meteo.com/v1/forecast"

CLOB_PRICES_URL = "https://clob.polymarket.com/prices-history"

_DEFAULT_STD_C = 2.5

_ERA5_TMAX_BIAS_C: dict[str, float] = {
    "New York": 1.0,
    "London": 0.8,
    "Denver": 1.2,
    "Helsinki": 1.0,
    "Paris": 1.0,
    "Tokyo": 0.7,
    "Chicago": 1.0,
    "Austin": 1.2,
    "Seoul": 0.8,
    "Hong Kong": 0.5,
    "Warsaw": 1.0,
    "Lagos": 0.5,
    "Taipei": 0.6,
    "Miami": 0.6,
    "Dallas": 1.2,
    "Atlanta": 1.0,
    "São Paulo": 0.5,
    "Sao Paulo": 0.5,
    "Buenos Aires": 0.8,
    "Jeddah": 0.5,
    "Ankara": 1.2,
    "Shanghai": 0.8,
}

_SEASONAL_STD_FACTOR: dict[str, list[float]] = {
    "New York": [0.8, 0.7, 0.8, 1.0, 1.1, 1.2, 1.3, 1.3, 1.1, 1.0, 0.8, 0.7],
    "London": [0.7, 0.7, 0.7, 0.8, 0.9, 1.0, 1.0, 1.0, 0.9, 0.8, 0.7, 0.7],
    "Tokyo": [0.8, 0.8, 0.9, 1.0, 1.0, 1.1, 1.2, 1.3, 1.2, 1.0, 0.8, 0.7],
    "Chicago": [0.9, 0.8, 0.9, 1.1, 1.2, 1.3, 1.3, 1.2, 1.1, 1.0, 0.9, 0.8],
    "Miami": [0.9, 0.9, 1.0, 1.0, 1.1, 1.0, 1.0, 1.0, 1.1, 1.1, 1.0, 0.9],
    "Seoul": [0.8, 0.8, 0.9, 1.0, 1.0, 1.1, 1.3, 1.3, 1.1, 1.0, 0.8, 0.7],
    "Hong Kong": [1.0, 1.0, 1.1, 1.2, 1.2, 1.1, 1.1, 1.1, 1.1, 1.1, 1.0, 1.0],
    "Lagos": [0.9, 1.0, 1.1, 1.1, 1.0, 0.9, 0.9, 0.9, 0.9, 1.0, 1.0, 1.0],
    "Paris": [0.7, 0.7, 0.8, 0.9, 1.0, 1.1, 1.1, 1.1, 1.0, 0.8, 0.7, 0.7],
    "Denver": [0.9, 0.9, 1.1, 1.2, 1.3, 1.4, 1.3, 1.2, 1.1, 1.0, 0.9, 0.8],
    "Helsinki": [0.7, 0.8, 0.9, 1.0, 1.1, 1.2, 1.1, 1.1, 1.0, 0.9, 0.7, 0.7],
    "Warsaw": [0.8, 0.8, 0.9, 1.0, 1.1, 1.2, 1.2, 1.2, 1.0, 0.9, 0.8, 0.7],
    "Taipei": [1.0, 1.0, 1.1, 1.1, 1.1, 1.1, 1.1, 1.1, 1.1, 1.1, 1.0, 1.0],
    "Austin": [0.8, 0.8, 0.9, 1.0, 1.1, 1.2, 1.3, 1.3, 1.2, 1.0, 0.8, 0.7],
    "Dallas": [0.8, 0.8, 0.9, 1.0, 1.1, 1.2, 1.3, 1.3, 1.2, 1.0, 0.8, 0.7],
    "Atlanta": [0.8, 0.8, 0.9, 1.0, 1.1, 1.2, 1.2, 1.2, 1.1, 1.0, 0.8, 0.8],
    "Beijing": [0.9, 0.9, 1.0, 1.1, 1.2, 1.2, 1.3, 1.3, 1.2, 1.0, 0.9, 0.8],
    "Madrid": [0.8, 0.8, 0.9, 1.0, 1.1, 1.2, 1.3, 1.3, 1.2, 1.0, 0.8, 0.7],
    "Wellington": [1.0, 1.0, 1.0, 1.0, 1.1, 1.1, 1.1, 1.1, 1.0, 1.0, 1.0, 1.0],
    "Milan": [0.7, 0.7, 0.8, 0.9, 1.0, 1.1, 1.2, 1.2, 1.0, 0.9, 0.8, 0.7],
    "Wuhan": [0.9, 0.9, 1.0, 1.1, 1.2, 1.2, 1.3, 1.3, 1.2, 1.0, 0.9, 0.8],
    "Munich": [0.7, 0.7, 0.8, 0.9, 1.0, 1.1, 1.2, 1.2, 1.0, 0.9, 0.8, 0.7],
    "Moscow": [0.8, 0.8, 0.9, 1.0, 1.1, 1.2, 1.2, 1.1, 1.0, 0.9, 0.8, 0.7],
    "San Francisco": [0.9, 0.9, 1.0, 1.0, 1.1, 1.1, 1.1, 1.1, 1.1, 1.0, 0.9, 0.9],
    "Istanbul": [0.8, 0.8, 0.9, 1.0, 1.1, 1.2, 1.3, 1.3, 1.2, 1.0, 0.8, 0.7],
    "Jakarta": [1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0],
    "Mexico City": [0.9, 0.9, 1.0, 1.0, 1.1, 1.1, 1.1, 1.1, 1.0, 1.0, 0.9, 0.9],
    "Amsterdam": [0.7, 0.7, 0.8, 0.9, 1.0, 1.1, 1.1, 1.1, 1.0, 0.9, 0.8, 0.7],
    "Busan": [0.8, 0.8, 0.9, 1.0, 1.0, 1.1, 1.2, 1.3, 1.2, 1.0, 0.9, 0.8],
    "Seattle": [0.8, 0.8, 0.9, 1.0, 1.1, 1.2, 1.3, 1.3, 1.2, 1.0, 0.8, 0.8],
    "Toronto": [0.9, 0.8, 0.9, 1.1, 1.2, 1.3, 1.3, 1.2, 1.1, 1.0, 0.9, 0.8],
    "Cape Town": [1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0],
}

_ENSEMBLE_UNDERDISPERSION_FACTOR = 1.15

_CITY_STD_C: dict[str, float] = {
    "New York": 2.5,
    "London": 2.0,
    "Denver": 3.5,
    "Helsinki": 3.5,
    "Paris": 2.5,
    "Tokyo": 2.0,
    "Chicago": 3.0,
    "Austin": 2.0,
    "Seoul": 2.5,
    "Hong Kong": 1.0,
    "Warsaw": 2.5,
    "Lagos": 1.0,
    "Taipei": 1.5,
    "Miami": 1.5,
    "Shanghai": 2.0,
    "Buenos Aires": 2.0,
    "São Paulo": 2.0,
    "Beijing": 2.5,
    "Madrid": 2.5,
    "Wellington": 2.0,
    "Milan": 2.5,
    "Wuhan": 2.5,
    "Munich": 3.0,
    "Moscow": 3.5,
    "San Francisco": 2.0,
    "Istanbul": 3.0,
    "Jakarta": 1.0,
    "Mexico City": 2.0,
    "Amsterdam": 2.0,
    "Busan": 2.5,
    "Seattle": 2.5,
    "Toronto": 3.0,
    "Cape Town": 2.0,
}

_MIN_REQUEST_INTERVAL = 0.12
_MAX_RETRIES = 3
_RETRY_BACKOFF_BASE = 1.0

WEATHER_SERIES_SLUGS: list[str] = [
    "nyc-daily-weather",
    "london-daily-weather",
    "denver-daily-weather",
    "helsinki-daily-weather",
    "paris-daily-weather",
    "tokyo-daily-weather",
    "chicago-daily-weather",
    "austin-daily-weather",
    "seoul-daily-weather",
    "hong-kong-daily-weather",
    "warsaw-daily-weather",
    "lagos-daily-weather",
    "taipei-daily-weather",
    "miami-daily-weather",
    "shanghai-daily-weather",
    "beijing-daily-weather",
    "madrid-daily-weather",
    "wellington-daily-weather",
    "milan-daily-weather",
    "wuhan-daily-weather",
    "munich-daily-weather",
    "moscow-daily-weather",
    "san-francisco-daily-weather",
    "istanbul-daily-weather",
    "jakarta-daily-weather",
    "mexico-city-daily-weather",
    "amsterdam-daily-weather",
    "busan-daily-weather",
    "seattle-daily-weather",
    "toronto-daily-weather",
    "cape-town-daily-weather",
    "atlanta-daily-weather",
    "dallas-daily-weather",
    "los-angeles-daily-weather",
    "sao-paulo-daily-weather",
    "buenos-aires-daily-weather",
]

SERIES_SLUG_TO_CITY: dict[str, str] = {
    "nyc-daily-weather": "New York",
    "london-daily-weather": "London",
    "denver-daily-weather": "Denver",
    "helsinki-daily-weather": "Helsinki",
    "paris-daily-weather": "Paris",
    "tokyo-daily-weather": "Tokyo",
    "chicago-daily-weather": "Chicago",
    "austin-daily-weather": "Austin",
    "seoul-daily-weather": "Seoul",
    "hong-kong-daily-weather": "Hong Kong",
    "warsaw-daily-weather": "Warsaw",
    "lagos-daily-weather": "Lagos",
    "taipei-daily-weather": "Taipei",
    "miami-daily-weather": "Miami",
    "shanghai-daily-weather": "Shanghai",
    "beijing-daily-weather": "Beijing",
    "madrid-daily-weather": "Madrid",
    "wellington-daily-weather": "Wellington",
    "milan-daily-weather": "Milan",
    "wuhan-daily-weather": "Wuhan",
    "munich-daily-weather": "Munich",
    "moscow-daily-weather": "Moscow",
    "san-francisco-daily-weather": "San Francisco",
    "istanbul-daily-weather": "Istanbul",
    "jakarta-daily-weather": "Jakarta",
    "mexico-city-daily-weather": "Mexico City",
    "amsterdam-daily-weather": "Amsterdam",
    "busan-daily-weather": "Busan",
    "seattle-daily-weather": "Seattle",
    "toronto-daily-weather": "Toronto",
    "cape-town-daily-weather": "Cape Town",
    "atlanta-daily-weather": "Atlanta",
    "dallas-daily-weather": "Dallas",
    "los-angeles-daily-weather": "Los Angeles",
    "sao-paulo-daily-weather": "São Paulo",
    "buenos-aires-daily-weather": "Buenos Aires",
}


@dataclass
class PricePoint:
    timestamp: float
    price: float


@dataclass
class ResolvedMarket:
    question: str
    token_id: str
    outcome: str
    winning: bool
    yes_price: float = 0.0
    no_price: float = 0.0
    price_history: list[PricePoint] = field(default_factory=list)
    price_source: str = ""  # "clob", "dune", "gamma_active", or "forecast"


@dataclass
class ResolvedEvent:
    event_id: str
    title: str
    slug: str
    city: str
    target_date: str
    measure_type: str = "high"
    markets: list[ResolvedMarket] = field(default_factory=list)


_REAL_DATA_SCHEMA = """
CREATE TABLE IF NOT EXISTS bt_resolved_events (
    event_id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    slug TEXT NOT NULL,
    city TEXT NOT NULL,
    measure_type TEXT NOT NULL DEFAULT 'high',
    target_date TEXT NOT NULL,
    raw_json TEXT NOT NULL,
    fetched_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS bt_price_history (
    token_id TEXT NOT NULL,
    ts REAL NOT NULL,
    price REAL NOT NULL,
    source TEXT NOT NULL DEFAULT 'clob',
    fetched_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (token_id, ts, source)
);

CREATE TABLE IF NOT EXISTS bt_previous_forecasts (
    city TEXT NOT NULL,
    date TEXT NOT NULL,
    model TEXT NOT NULL,
    temp_high_c REAL NOT NULL,
    members_json TEXT,
    fetched_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(city, date, model)
);

CREATE TABLE IF NOT EXISTS bt_active_prices (
    token_id TEXT PRIMARY KEY,
    yes_price REAL NOT NULL,
    fetched_at TEXT NOT NULL DEFAULT (datetime('now'))
);
"""


class _RateLimiter:
    def __init__(self, min_interval: float = _MIN_REQUEST_INTERVAL) -> None:
        self._min_interval = min_interval
        self._last: float = 0.0

    async def wait(self) -> None:
        now = monotonic()
        elapsed = now - self._last
        if elapsed < self._min_interval:
            import asyncio

            await asyncio.sleep(self._min_interval - elapsed)
        self._last = monotonic()


_CITY_PATTERNS: list[str] = [
    "New York",
    "NYC",
    "Los Angeles",
    "LA",
    "Chicago",
    "Miami",
    "Dallas",
    "Atlanta",
    "London",
    "Paris",
    "Hong Kong",
    "Seoul",
    "Tokyo",
    "Shanghai",
    "Buenos Aires",
    "Jeddah",
    "Ankara",
    "Lagos",
    "São Paulo",
    "Sao Paulo",
]

_CITY_CANONICAL: dict[str, str] = {
    "NYC": "New York",
    "LA": "Los Angeles",
    "New York's Central Park": "New York",
}


def _is_weather_title(title: str) -> bool:
    t = title.lower()
    return any(
        k in t
        for k in [
            "temperature",
            "high temp",
            "low temp",
            "highest temp",
            "lowest temp",
        ]
    )


def _synthesize_ensemble(
    center: float,
    city: str = "",
    date_iso: str = "",
    n: int = 51,
) -> list[float]:
    base_std = _CITY_STD_C.get(city, _DEFAULT_STD_C)

    season_factor = 1.0
    if city in _SEASONAL_STD_FACTOR and date_iso:
        try:
            month = int(date_iso[5:7])
            season_factor = _SEASONAL_STD_FACTOR[city][month - 1]
        except (ValueError, IndexError):
            pass

    std = base_std * season_factor * _ENSEMBLE_UNDERDISPERSION_FACTOR

    import hashlib

    seed = int(hashlib.md5(f"{center}:{city}:{date_iso}".encode()).hexdigest()[:8], 16)
    import random

    rng = random.Random(seed)
    return [center + rng.gauss(0, std) for _ in range(n)]


def _extract_city(title: str) -> str | None:
    for c in _CITY_PATTERNS:
        if c.lower() in title.lower():
            return resolve_city_alias(_CITY_CANONICAL.get(c, c))
    return None


def _extract_date_iso(title: str) -> str:
    m = re.search(r"(\d{4}-\d{2}-\d{2})", title)
    if m:
        return m.group(1)
    m = re.search(r"(\w+\s+\d{1,2},?\s+\d{4})", title)
    if m:
        try:
            dt = datetime.strptime(m.group(1).replace(",", ""), "%B %d %Y")
            return dt.date().isoformat()
        except ValueError:
            pass
    m = re.search(r"on\s+(\w+\s+\d{1,2})", title, re.I)
    if m:
        try:
            year = datetime.now(timezone.utc).year
            dt = datetime.strptime(m.group(1), "%B %d")
            return dt.replace(year=year).date().isoformat()
        except ValueError:
            pass
    return ""


def _parse_flexible_date(date_str: str, title: str = "") -> Any | None:
    """Parse a date from various sources: ISO string, title, or endDate.

    Handles formats: '2026-01-22', 'Jan 22', 'November 15th, 2021',
    'April 23', etc.
    """
    import re as _re

    if date_str:
        for fmt in ("%Y-%m-%d", "%B %d", "%b %d"):
            try:
                dt = datetime.strptime(date_str, fmt)
                if "%Y" not in fmt:
                    iso = _extract_date_iso(title)
                    if iso:
                        try:
                            year = datetime.strptime(iso[:4], "%Y").year
                            dt = dt.replace(year=year)
                        except ValueError:
                            dt = dt.replace(year=datetime.now(timezone.utc).year)
                return dt.date()
            except ValueError:
                continue

    if title:
        patterns = [
            r"(\w+ \d{1,2}(?:st|nd|rd|th)?,?\s*\d{4})",
            r"(\d{4}-\d{2}-\d{2})",
            r"(\w+ \d{1,2}(?:st|nd|rd|th)?)",
        ]
        for pat in patterns:
            m = _re.search(pat, title)
            if m:
                raw = m.group(1)
                for fmt in (
                    "%B %d, %Y",
                    "%B %dth, %Y",
                    "%B %dst, %Y",
                    "%B %dnd, %Y",
                    "%B %drd, %Y",
                    "%Y-%m-%d",
                    "%B %d",
                ):
                    cleaned = _re.sub(r"(st|nd|rd|th)", "", raw)
                    for cf in ("%B %d, %Y", "%B %d %Y", "%Y-%m-%d", "%B %d"):
                        try:
                            dt = datetime.strptime(cleaned, cf)
                            if dt.year < 2020:
                                continue
                            return dt.date()
                        except ValueError:
                            continue
    return None


def _is_high_temp_market(title: str) -> bool:
    tl = title.lower()
    return not ("lowest" in tl or "low temperature" in tl or "low temp" in tl)


class RealDataFetcher:
    # Threshold for determining if a market resolved to YES based on outcomePrices.
    # 0.90 is slightly more lenient than the theoretical 1.0 to handle edge cases
    # where Polymarket markets settle near but not exactly at 1.0 (e.g. 0.95-0.98).
    RESOLVED_THRESHOLD: float = 0.90

    def __init__(self, db_path: Path | None = None) -> None:
        self.db_path = db_path or DEFAULT_DB_PATH
        self.db_path.parent.mkdir(parents=True, exist_ok=True)
        self._conn: sqlite3.Connection | None = None
        self._limiter = _RateLimiter()
        self._forecast_cache: dict[str, ForecastResult] = {}

    def _get_conn(self) -> sqlite3.Connection:
        if self._conn is not None:
            return self._conn
        self._conn = sqlite3.connect(str(self.db_path))
        self._conn.row_factory = sqlite3.Row
        self._conn.execute("PRAGMA journal_mode=WAL")
        self._conn.executescript(_REAL_DATA_SCHEMA)
        self._conn.commit()
        return self._conn

    async def _get_with_retry(
        self,
        client: httpx.AsyncClient,
        url: str,
        params: dict | None = None,
        timeout: float = 30.0,
    ) -> Any:
        for attempt in range(_MAX_RETRIES):
            try:
                await self._limiter.wait()
                log.debug("api_request", url=url, params=params, attempt=attempt)
                resp = await client.get(url, params=params, timeout=timeout)
                if resp.status_code == 429:
                    wait = _RETRY_BACKOFF_BASE * (2**attempt) * 2
                    log.warning("rate_limited", url=url, wait_s=wait)
                    import asyncio

                    await asyncio.sleep(wait)
                    continue
                resp.raise_for_status()
                return resp.json()
            except httpx.HTTPError as e:
                wait = _RETRY_BACKOFF_BASE * (2**attempt)
                log.warning("api_error", url=url, error=str(e), attempt=attempt, wait_s=wait)
                if attempt < _MAX_RETRIES - 1:
                    import asyncio

                    await asyncio.sleep(wait)
        log.error("api_persistent_failure", url=url)
        return None

    async def fetch_resolved_weather_events(
        self,
        client: httpx.AsyncClient,
        days: int = 30,
    ) -> list[ResolvedEvent]:
        """Fetch resolved weather events via series_slug endpoints.

        Gamma API's closed=true index is broken for new weather markets.
        Instead, query each city's series directly for reliable results.
        """
        conn = self._get_conn()
        cutoff = (datetime.now(timezone.utc) - timedelta(days=days)).date()
        events: list[ResolvedEvent] = []

        for slug in WEATHER_SERIES_SLUGS:
            city_name = SERIES_SLUG_TO_CITY.get(slug, "")
            if not city_name:
                continue

            page = 0
            while True:
                data = await self._get_with_retry(
                    client,
                    f"{GAMMA_BASE}/events",
                    params={
                        "series_slug": slug,
                        "limit": 100,
                        "order": "end_date",
                        "ascending": False,
                        "offset": page * 100,
                    },
                )
                if not data or not isinstance(data, list):
                    break

                for ev in data:
                    if not ev.get("closed", False):
                        continue

                    title = ev.get("title", "")
                    ev_date = _parse_flexible_date("", title)
                    if ev_date is None:
                        raw_date = ev.get("endDate", "")
                        if raw_date:
                            ev_date = _parse_flexible_date(raw_date[:10], title)
                    if ev_date is None:
                        continue

                    if ev_date > datetime.now(timezone.utc).date():
                        continue
                    if ev_date < cutoff:
                        break

                    if not _is_high_temp_market(title):
                        continue

                    raw_markets = ev.get("markets", [])

                    resolved_markets: list[ResolvedMarket] = []
                    for m in raw_markets:
                        rm = self._parse_resolved_market(m)
                        if rm is not None:
                            resolved_markets.append(rm)

                    if not resolved_markets:
                        continue

                    winning_markets = [m for m in resolved_markets if m.winning]
                    if not winning_markets:
                        continue

                    ev_id = f"{city_name}-{ev_date.isoformat()}-high"
                    resolved = ResolvedEvent(
                        event_id=ev_id,
                        title=title,
                        slug=ev.get("slug", ""),
                        city=city_name,
                        measure_type="high",
                        target_date=ev_date.isoformat(),
                        markets=resolved_markets,
                    )
                    events.append(resolved)

                    try:
                        conn.execute(
                            "INSERT OR IGNORE INTO bt_resolved_events (event_id, title, city, target_date, measure_type, slug) VALUES (?, ?, ?, ?, ?, ?)",
                            (ev_id, title, city_name, ev_date.isoformat(), "high", ev.get("slug", "")),
                        )
                    except sqlite3.Error:
                        pass

                if len(data) < 100:
                    break
                page += 1

        conn.commit()
        log.info("resolved_events_fetched", count=len(events), days=days)
        return events

    async def fetch_clob_price_at(
        self,
        client: httpx.AsyncClient,
        token_id: str,
        target_ts: float,
        window_hours: float = 24.0,
        fidelity: int = 60,
    ) -> float | None:
        """Fetch CLOB market price closest to target_ts for a given token.

        Per GitHub issue #216, resolved markets require startTs/endTs
        (not interval=max) and fidelity<=60 for data to be returned.
        Chunked into 15-day windows to avoid "interval too long" errors.
        """
        window_s = int(window_hours * 3600)
        start_ts = max(0, int(target_ts - window_s))
        end_ts = int(target_ts + window_s)

        chunk_s = 15 * 86400
        all_history: list[dict] = []

        chunk_start = start_ts
        while chunk_start < end_ts:
            chunk_end = min(chunk_start + chunk_s, end_ts)
            data = await self._get_with_retry(
                client,
                CLOB_PRICES_URL,
                params={
                    "market": token_id,
                    "startTs": chunk_start,
                    "endTs": chunk_end,
                    "fidelity": fidelity,
                },
            )
            if data and "history" in data and data["history"]:
                all_history.extend(data["history"])
            chunk_start = chunk_end

        if not all_history:
            return None

        for pt in all_history:
            try:
                self._get_conn().execute(
                    "INSERT OR IGNORE INTO bt_price_history (token_id, ts, price) VALUES (?, ?, ?)",
                    (token_id, pt["t"], float(pt["p"])),
                )
            except Exception:
                pass
        self._get_conn().commit()

        closest = min(all_history, key=lambda p: abs(p["t"] - target_ts))
        return float(closest["p"])

    async def enrich_events_with_clob_prices(
        self,
        client: httpx.AsyncClient,
        events: list[ResolvedEvent],
        hours_before_settlement: float = 24.0,
        max_concurrent: int = 10,
    ) -> None:
        """Enrich ResolvedEvent markets with CLOB T-24h prices.

        For each market's YES token, fetches the price at
        (settlement_time - hours_before_settlement). Falls back to
        forecast-derived probability if CLOB data unavailable.

        Checks SQLite cache first; only fetches from API if not cached.
        Uses concurrent requests for speed.
        Per GitHub issue #216, fidelity=60 (1h) is required for
        resolved markets; fidelity=120+ returns empty data.
        """
        import asyncio

        sem = asyncio.Semaphore(max_concurrent)
        tasks: list[asyncio.Task] = []

        async def _fetch_one(ev: ResolvedEvent, m: ResolvedMarket) -> None:
            async with sem:
                if not m.token_id or len(m.token_id) < 20:
                    return
                raw_date = ev.target_date
                try:
                    dt = datetime.strptime(raw_date, "%Y-%m-%d")
                except ValueError:
                    return
                from pm_bot.core.observation import CITY_TZ
                from zoneinfo import ZoneInfo

                tz_name = CITY_TZ.get(ev.city, "UTC")
                tz = ZoneInfo(tz_name)
                local_midnight = dt.replace(tzinfo=tz)
                settlement_utc = local_midnight.timestamp()
                target_ts = settlement_utc - (hours_before_settlement * 3600)

                cached = self._get_cached_clob_price(m.token_id, target_ts)
                if cached is not None:
                    if 0.01 <= cached <= 0.99:
                        m.yes_price = cached
                        m.no_price = 1.0 - cached
                        m.price_source = "clob"
                    return

                price = await self.fetch_clob_price_at(client, m.token_id, target_ts, fidelity=60)
                if price is not None and 0.01 <= price <= 0.99:
                    m.yes_price = price
                    m.no_price = 1.0 - price
                    m.price_source = "clob"

        for ev in events:
            for m in ev.markets:
                tasks.append(asyncio.create_task(_fetch_one(ev, m)))

        if tasks:
            await asyncio.gather(*tasks)

    def _get_cached_clob_price(self, token_id: str, target_ts: float, tolerance_s: float = 3600.0) -> float | None:
        """Try to get a cached CLOB price within tolerance of target_ts."""
        try:
            row = (
                self._get_conn()
                .execute(
                    "SELECT ts, price FROM bt_price_history WHERE token_id = ? AND ABS(ts - ?) <= ? ORDER BY ABS(ts - ?) LIMIT 1",
                    (token_id, target_ts, tolerance_s, target_ts),
                )
                .fetchone()
            )
            if row:
                return float(row[1])
        except Exception:
            pass
        return None

    def _get_cached_dune_price(self, token_id: str, target_ts: float, tolerance_s: float = 7200.0) -> float | None:
        """Try to get a cached Dune price within tolerance of target_ts."""
        try:
            row = (
                self._get_conn()
                .execute(
                    "SELECT ts, price FROM bt_price_history WHERE token_id = ? AND source = 'dune' AND ABS(ts - ?) <= ? ORDER BY ABS(ts - ?) LIMIT 1",
                    (token_id, target_ts, tolerance_s, target_ts),
                )
                .fetchone()
            )
            if row:
                return float(row[1])
        except Exception:
            pass
        return None

    async def fetch_dune_prices(
        self,
        client: httpx.AsyncClient,
        condition_id: str,
        hours_before: int = 24,
        dune_api_key: str = "",
    ) -> dict[str, float]:
        """Fetch hourly prices from Dune Analytics for a given condition_id.

        Queries the polymarket_polygon.market_prices_hourly table.
        Returns {token_id: price} mapping for the closest hour to settlement.

        Dune API docs: https://docs.dune.com/api-reference/
        """
        if not dune_api_key:
            return {}

        dune_url = "https://api.dune.com/api/v1/query/4829597/results"
        headers = {"X-DUNE-API-KEY": dune_api_key}

        # Query for prices near settlement time
        params: dict[str, str | int] = {
            "filters": f"condition_id='{condition_id}'",
            "limit": 100,
        }

        try:
            await self._limiter.wait()
            resp = await client.get(dune_url, params=params, headers=headers, timeout=30.0)
            if resp.status_code == 429:
                log.warning("dune_rate_limited", condition_id=condition_id)
                return {}
            resp.raise_for_status()
            data = resp.json()
        except httpx.HTTPError as e:
            log.warning("dune_fetch_failed", condition_id=condition_id, error=str(e))
            return {}

        rows = data.get("result", {}).get("rows", [])
        if not rows:
            return {}

        result: dict[str, float] = {}
        conn = self._get_conn()

        for row in rows:
            token_id = row.get("token_id", "")
            price = row.get("price")
            hour_ts = row.get("hour")
            if not token_id or price is None:
                continue

            price_f = float(price)
            ts_f = float(hour_ts) if hour_ts else 0.0

            if not (0.01 <= price_f <= 0.99):
                continue

            result[token_id] = price_f

            try:
                conn.execute(
                    "INSERT OR IGNORE INTO bt_price_history (token_id, ts, price, source) VALUES (?, ?, ?, 'dune')",
                    (token_id, ts_f, price_f),
                )
            except sqlite3.Error:
                pass

        conn.commit()
        log.info("dune_prices_fetched", condition_id=condition_id, count=len(result))
        return result

    async def enrich_events_with_dune_prices(
        self,
        client: httpx.AsyncClient,
        events: list[ResolvedEvent],
        dune_api_key: str = "",
        hours_before_settlement: float = 24.0,
    ) -> None:
        """Enrich ResolvedEvent markets with Dune hourly prices.

        Only fills in markets that still have 0/1 prices (i.e. no CLOB data).
        Priority: CLOB T-24h → Dune hourly → Gamma active → forecast.
        """
        import asyncio

        if not dune_api_key:
            return

        sem = asyncio.Semaphore(5)
        tasks: list[asyncio.Task] = []

        async def _fetch_one(ev: ResolvedEvent) -> None:
            async with sem:
                # Find condition_id from markets (use first market's token_id prefix or event metadata)
                # Dune uses condition_id which maps to event, not individual tokens
                condition_id = ev.event_id  # fallback
                prices = await self.fetch_dune_prices(
                    client,
                    condition_id,
                    hours_before=int(hours_before_settlement),
                    dune_api_key=dune_api_key,
                )
                for m in ev.markets:
                    if m.yes_price > 0.005 and m.yes_price < 0.995:
                        continue  # Already has CLOB price
                    dune_price = prices.get(m.token_id)
                    if dune_price is not None and 0.01 <= dune_price <= 0.99:
                        m.yes_price = dune_price
                        m.no_price = 1.0 - dune_price
                        m.price_source = "dune"

        for ev in events:
            # Only fetch Dune prices for events with markets missing CLOB data
            needs_dune = any(not (m.yes_price > 0.005 and m.yes_price < 0.995) for m in ev.markets)
            if needs_dune:
                tasks.append(asyncio.create_task(_fetch_one(ev)))

        if tasks:
            await asyncio.gather(*tasks)

    def _parse_resolved_market(self, m: dict) -> ResolvedMarket | None:
        """Parse a single market (bucket) from Gamma /markets endpoint.

        Weather market questions contain °F or °C. Each market IS a bucket.
        outcomePrices: ["0.95", "0.05"] means Yes=0.95, No=0.05.
        The winning bucket has Yes price ~1.0.
        """
        question = m.get("question", "")
        has_temp = any(c in question for c in ("°F", "°f", "°C", "°c"))
        if not has_temp:
            return None

        clob_token_ids_raw = m.get("clobTokenIds", "")
        token_ids: list[str] = []
        if isinstance(clob_token_ids_raw, str) and clob_token_ids_raw:
            try:
                token_ids = json.loads(clob_token_ids_raw)
            except json.JSONDecodeError:
                token_ids = []
        elif isinstance(clob_token_ids_raw, list):
            token_ids = [str(t) for t in clob_token_ids_raw]

        if not token_ids:
            yes_token = str(m.get("id", ""))
            if not yes_token:
                return None
        else:
            yes_token = token_ids[0]

        outcomes_raw = m.get("outcomes", "")
        prices_raw = m.get("outcomePrices", "")

        outcome_list: list[str] = []
        price_list: list[float] = []

        if isinstance(outcomes_raw, str):
            outcome_list = [o.strip().strip('"') for o in outcomes_raw.strip("[]").split(",")]
        if isinstance(prices_raw, str):
            for p in prices_raw.strip("[]").split(","):
                try:
                    price_list.append(float(p.strip().strip('"')))
                except ValueError:
                    price_list.append(0.0)

        yes_price = 0.0
        no_price = 0.0
        winning = False
        for o_str, p_val in zip(outcome_list, price_list):
            if o_str.upper() == "YES":
                yes_price = p_val
                if p_val >= self.RESOLVED_THRESHOLD:
                    winning = True
            elif o_str.upper() == "NO":
                no_price = p_val

        return ResolvedMarket(
            question=question,
            token_id=yes_token,
            outcome="Yes" if winning else "No",
            winning=winning,
            yes_price=yes_price,
            no_price=no_price,
        )

    async def fetch_active_market_prices(
        self,
        client: httpx.AsyncClient,
        days: int = 30,
    ) -> dict[str, float]:
        """Fetch current Gamma outcomePrices for active (unsettled) weather markets.

        For active markets, Gamma's outcomePrices reflect real market prices.
        This is the most reliable source for backtest entry prices since
        CLOB prices-history may return empty data for resolved markets.
        Returns {token_id: yes_price} mapping.
        """
        result: dict[str, float] = {}
        conn = self._get_conn()

        for slug in WEATHER_SERIES_SLUGS:
            page = 0
            while True:
                data = await self._get_with_retry(
                    client,
                    f"{GAMMA_BASE}/events",
                    params={
                        "series_slug": slug,
                        "limit": 100,
                        "order": "end_date",
                        "ascending": False,
                        "offset": page * 100,
                    },
                )
                if not data or not isinstance(data, list):
                    break

                for ev in data:
                    if ev.get("closed", False):
                        continue

                    raw_markets = ev.get("markets", [])
                    for m in raw_markets:
                        question = m.get("question", "")
                        has_temp = any(c in question for c in ("°F", "°f", "°C", "°c"))
                        if not has_temp:
                            continue

                        clob_token_ids_raw = m.get("clobTokenIds", "")
                        token_ids: list[str] = []
                        if isinstance(clob_token_ids_raw, str) and clob_token_ids_raw:
                            try:
                                token_ids = json.loads(clob_token_ids_raw)
                            except json.JSONDecodeError:
                                pass
                        elif isinstance(clob_token_ids_raw, list):
                            token_ids = [str(t) for t in clob_token_ids_raw]
                        if not token_ids:
                            continue

                        yes_token = token_ids[0]

                        prices_raw = m.get("outcomePrices", "")
                        price_list: list[float] = []
                        if isinstance(prices_raw, str):
                            for p in prices_raw.strip("[]").split(","):
                                try:
                                    price_list.append(float(p.strip().strip('"')))
                                except ValueError:
                                    price_list.append(0.0)

                        if price_list and 0.01 <= price_list[0] <= 0.99:
                            result[yes_token] = price_list[0]
                            try:
                                conn.execute(
                                    "INSERT OR IGNORE INTO bt_active_prices (token_id, yes_price, fetched_at) VALUES (?, ?, datetime('now'))",
                                    (yes_token, price_list[0]),
                                )
                            except sqlite3.Error:
                                pass

                if len(data) < 100:
                    break
                page += 1

        conn.commit()
        log.info("active_prices_fetched", count=len(result))
        return result

    def get_active_price(self, token_id: str) -> float | None:
        """Get cached active market price for a token."""
        conn = self._get_conn()
        row = conn.execute(
            "SELECT yes_price FROM bt_active_prices WHERE token_id = ? ORDER BY fetched_at DESC LIMIT 1",
            (token_id,),
        ).fetchone()
        if row:
            return float(row["yes_price"])
        return None

    async def prefetch_forecasts(
        self,
        client: httpx.AsyncClient,
        cities: list[str],
        days: int,
    ) -> None:
        """Batch-fetch historical forecasts for all cities via previous-runs API.

        One API call per city covers the entire date range — much faster than
        per-event fetching.
        """
        conn = self._get_conn()
        model = "gfs_seamless"

        for city in cities:
            canonical = resolve_city_alias(city)
            coords = CITY_COORDS.get(canonical) or CITY_COORDS.get(city)
            if not coords:
                log.warning("unknown_city_coords", city=city)
                continue

            lat, lon = coords

            try:
                resp = await client.get(
                    PREVIOUS_RUNS_URL,
                    params={
                        "latitude": lat,
                        "longitude": lon,
                        "daily": "temperature_2m_max",
                        "models": model,
                        "past_days": min(days, 365),
                        "timezone": "auto",
                    },
                    timeout=30.0,
                )
                resp.raise_for_status()
                data = resp.json()
            except httpx.HTTPError as e:
                log.warning("previous_runs_fetch_failed", city=city, error=str(e))
                continue

            daily = data.get("daily", {})
            times = daily.get("time", [])
            temps = daily.get("temperature_2m_max", [])

            for i, t in enumerate(times):
                if i >= len(temps) or not isinstance(temps[i], (int, float)):
                    continue

                cache_key = f"{canonical}:{t}:high"
                if cache_key in self._forecast_cache:
                    continue

                members: list[float] = []
                for mi in range(1, 36):
                    key = f"temperature_2m_max_member{mi:02d}"
                    m_data = daily.get(key, [])
                    if i < len(m_data) and isinstance(m_data[i], (int, float)):
                        members.append(float(m_data[i]))

                if not members:
                    members = _synthesize_ensemble(float(temps[i]), city=canonical, date_iso=t)

                fr = ForecastResult(
                    city=canonical,
                    date=t,
                    model=model,
                    temp_high_c=float(temps[i]),
                    measure_type="high",
                    members=members,
                )
                self._forecast_cache[cache_key] = fr

                try:
                    conn.execute(
                        "INSERT OR IGNORE INTO bt_previous_forecasts (city, date, model, temp_high_c, members_json) VALUES (?, ?, ?, ?, ?)",
                        (canonical, t, model, float(temps[i]), json.dumps(members)),
                    )
                except sqlite3.Error:
                    pass

            conn.commit()

        log.info("forecasts_prefetched", cities=len(cities), cached=len(self._forecast_cache))

    def get_cached_forecast(self, city: str, date: str) -> ForecastResult | None:
        canonical = resolve_city_alias(city)
        cache_key = f"{canonical}:{date}:high"
        if cache_key in self._forecast_cache:
            return self._forecast_cache[cache_key]

        conn = self._get_conn()
        model = "gfs_seamless"
        row = conn.execute(
            "SELECT * FROM bt_previous_forecasts WHERE city = ? AND date = ? AND model = ?",
            (canonical, date, model),
        ).fetchone()
        if row:
            cached_members = json.loads(row["members_json"]) if row["members_json"] else []
            fr = ForecastResult(
                city=canonical,
                date=date,
                model=model,
                temp_high_c=row["temp_high_c"],
                measure_type="high",
                members=cached_members,
            )
            self._forecast_cache[cache_key] = fr
            return fr
        return None

    async def fetch_market_prices(
        self,
        client: httpx.AsyncClient,
        token_ids: list[str],
        start_ts: float,
        end_ts: float,
    ) -> dict[str, list[PricePoint]]:
        conn = self._get_conn()
        result: dict[str, list[PricePoint]] = {}

        uncached: list[str] = []
        for tid in token_ids:
            rows = conn.execute(
                "SELECT ts, price FROM bt_price_history WHERE token_id = ? AND ts >= ? AND ts <= ? ORDER BY ts",
                (tid, start_ts, end_ts),
            ).fetchall()
            if rows:
                result[tid] = [PricePoint(timestamp=r["ts"], price=r["price"]) for r in rows]
            else:
                uncached.append(tid)

        if not uncached:
            return result

        for batch_start in range(0, len(uncached), 20):
            batch = uncached[batch_start : batch_start + 20]
            for tid in batch:
                data = await self._get_with_retry(
                    client,
                    f"{CLOB_BASE}/prices-history",
                    params={"market": tid, "startTs": int(start_ts), "endTs": int(end_ts)},
                )
                if data is None or not isinstance(data, dict):
                    continue
                history = data.get("history", [])
                points: list[PricePoint] = []
                for h in history:
                    if isinstance(h, dict):
                        t = h.get("t", 0)
                        p = h.get("p", 0.0)
                        if t and p is not None:
                            try:
                                points.append(PricePoint(timestamp=float(t), price=float(p)))
                            except (ValueError, TypeError):
                                continue
                if points:
                    result[tid] = points
                    for pt in points:
                        try:
                            conn.execute(
                                "INSERT OR IGNORE INTO bt_price_history (token_id, ts, price) VALUES (?, ?, ?)",
                                (tid, pt.timestamp, pt.price),
                            )
                        except sqlite3.Error:
                            pass

        conn.commit()
        return result

    ARCHIVE_URL = "https://archive-api.open-meteo.com/v1/archive"

    async def fetch_active_weather_events(
        self,
        client: httpx.AsyncClient,
        days: int = 30,
    ) -> list[ResolvedEvent]:
        """Fetch active (unsettled) weather events via series_slug endpoints.

        Builds ResolvedEvent structures with winning=False (unsettled).
        Gamma outcomePrices provide current market prices.
        Combined with fetch_actual_temps() for settlement simulation.
        """
        conn = self._get_conn()
        cutoff = (datetime.now(timezone.utc) - timedelta(days=days)).date()
        today = datetime.now(timezone.utc).date()
        events: list[ResolvedEvent] = []

        for slug in WEATHER_SERIES_SLUGS:
            city_name = SERIES_SLUG_TO_CITY.get(slug, "")
            if not city_name:
                continue

            page = 0
            while True:
                data = await self._get_with_retry(
                    client,
                    f"{GAMMA_BASE}/events",
                    params={
                        "series_slug": slug,
                        "limit": 100,
                        "order": "end_date",
                        "ascending": False,
                        "offset": page * 100,
                    },
                )
                if not data or not isinstance(data, list):
                    break

                for ev in data:
                    if ev.get("closed", False):
                        continue

                    title = ev.get("title", "")
                    ev_date = _parse_flexible_date("", title)
                    if ev_date is None:
                        raw_date = ev.get("endDate", "")
                        if raw_date:
                            ev_date = _parse_flexible_date(raw_date[:10], title)
                    if ev_date is None:
                        continue

                    if ev_date > today:
                        continue
                    if ev_date < cutoff:
                        break

                    if not _is_high_temp_market(title):
                        continue

                    raw_markets = ev.get("markets", [])
                    resolved_markets: list[ResolvedMarket] = []
                    for m in raw_markets:
                        rm = self._parse_resolved_market(m)
                        if rm is not None:
                            rm.winning = False
                            resolved_markets.append(rm)

                    if not resolved_markets:
                        continue

                    ev_id = f"{city_name}-{ev_date.isoformat()}-high-active"
                    resolved = ResolvedEvent(
                        event_id=ev_id,
                        title=title,
                        slug=ev.get("slug", ""),
                        city=city_name,
                        measure_type="high",
                        target_date=ev_date.isoformat(),
                        markets=resolved_markets,
                    )
                    events.append(resolved)

                    try:
                        conn.execute(
                            "INSERT OR IGNORE INTO bt_resolved_events (event_id, title, city, target_date, measure_type, slug) VALUES (?, ?, ?, ?, ?, ?)",
                            (ev_id, title, city_name, ev_date.isoformat(), "high", ev.get("slug", "")),
                        )
                    except sqlite3.Error:
                        pass

                if len(data) < 100:
                    break
                page += 1

        conn.commit()
        log.info("active_events_fetched", count=len(events), days=days)
        return events

    async def fetch_actual_temps(
        self,
        client: httpx.AsyncClient,
        events: list[ResolvedEvent],
    ) -> dict[str, float]:
        """Fetch actual max temperatures from Open-Meteo archive API.

        Returns {(city, date_iso): actual_max_temp_celsius}.
        Used for simulating settlement of active (unsettled) markets.

        Applies ERA5 Tmax cold-bias correction per city (ERA5 systematically
        underestimates daily max temperature vs airport ASOS stations that
        Polymarket settles on). Bias values range from +0.5 to +1.2°C.
        """
        from pm_bot.models.config import CITY_COORDS

        city_dates: dict[str, set[str]] = {}
        for ev in events:
            city_dates.setdefault(ev.city, set()).add(ev.target_date)

        result: dict[str, float] = {}

        for city, dates in city_dates.items():
            coords = CITY_COORDS.get(city)
            if not coords:
                continue

            date_list = sorted(dates)
            params = {
                "latitude": coords[0],
                "longitude": coords[1],
                "start_date": date_list[0],
                "end_date": date_list[-1],
                "daily": "temperature_2m_max",
                "timezone": "auto",
            }

            try:
                data = await self._get_with_retry(client, self.ARCHIVE_URL, params=params)
            except Exception:
                log.warning("archive_api_error", city=city)
                continue

            if not data or "daily" not in data:
                continue

            daily = data["daily"]
            times = daily.get("time", [])
            temps = daily.get("temperature_2m_max", [])

            for t, temp in zip(times, temps):
                if temp is not None:
                    bias = _ERA5_TMAX_BIAS_C.get(city, 0.8)
                    corrected = float(temp) + bias
                    key = f"{city}|{t}"
                    result[key] = corrected

        log.info("actual_temps_fetched", cities=len(city_dates), dates_with_temp=len(result))
        return result

    def close(self) -> None:
        if self._conn:
            self._conn.close()
            self._conn = None
