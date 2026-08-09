from __future__ import annotations

import json
import sqlite3
from pathlib import Path

import httpx
import structlog

from pm_bot.core.db import DEFAULT_DB_PATH
from pm_bot.models.config import CITY_COORDS
from pm_bot.models.market import ForecastResult

log = structlog.get_logger()

HISTORICAL_FORECAST_URL = "https://historical-forecast-api.open-meteo.com/v1/forecast"
ARCHIVE_URL = "https://archive-api.open-meteo.com/v1/archive"

_BT_SCHEMA = """
CREATE TABLE IF NOT EXISTS backtest_forecasts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    city TEXT NOT NULL,
    date TEXT NOT NULL,
    model TEXT NOT NULL,
    temp_high_c REAL NOT NULL,
    members_json TEXT,
    fetched_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(city, date, model)
);

CREATE TABLE IF NOT EXISTS backtest_observations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    city TEXT NOT NULL,
    date TEXT NOT NULL,
    temp_high_c REAL NOT NULL,
    fetched_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(city, date)
);

CREATE TABLE IF NOT EXISTS backtest_prices (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id TEXT NOT NULL,
    bucket_key TEXT NOT NULL,
    yes_price REAL NOT NULL,
    no_price REAL NOT NULL,
    fetched_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(event_id, bucket_key)
);
"""


class HistoricalDataFetcher:
    def __init__(self, db_path: Path | None = None) -> None:
        self.db_path = db_path or DEFAULT_DB_PATH
        self.db_path.parent.mkdir(parents=True, exist_ok=True)
        self._conn: sqlite3.Connection | None = None

    def _get_conn(self) -> sqlite3.Connection:
        if self._conn is not None:
            return self._conn
        self._conn = sqlite3.connect(str(self.db_path))
        self._conn.row_factory = sqlite3.Row
        self._conn.execute("PRAGMA journal_mode=WAL")
        self._conn.executescript(_BT_SCHEMA)
        self._conn.commit()
        return self._conn

    async def fetch_historical_forecasts(
        self,
        client: httpx.AsyncClient,
        city: str,
        date: str,
        model: str = "gfs_seamless",
    ) -> ForecastResult | None:
        conn = self._get_conn()
        row = conn.execute(
            "SELECT * FROM backtest_forecasts WHERE city = ? AND date = ? AND model = ?",
            (city, date, model),
        ).fetchone()
        if row:
            members = json.loads(row["members_json"]) if row["members_json"] else []
            return ForecastResult(
                city=city,
                date=date,
                model=model,
                temp_high_c=row["temp_high_c"],
                members=members,
            )

        coords = CITY_COORDS.get(city)
        if not coords:
            log.warning("unknown_city_for_backtest", city=city)
            return None

        lat, lon = coords
        try:
            resp = await client.get(
                HISTORICAL_FORECAST_URL,
                params={
                    "latitude": lat,
                    "longitude": lon,
                    "start_date": date,
                    "end_date": date,
                    "daily": "temperature_2m_max",
                    "models": model,
                    "timezone": "auto",
                },
                timeout=30.0,
            )
            resp.raise_for_status()
            data = resp.json()
        except httpx.HTTPError as e:
            log.warning("historical_forecast_fetch_failed", city=city, date=date, error=str(e))
            return None

        daily = data.get("daily", {})
        temps = daily.get("temperature_2m_max", [])
        temp_high = float(temps[0]) if temps and isinstance(temps[0], (int, float)) else None
        if temp_high is None:
            return None

        ensemble_members: list[float] = []
        for i in range(1, 36):
            key = f"temperature_2m_max_member{i:02d}"
            m_data = daily.get(key, [])
            if m_data and isinstance(m_data[0], (int, float)):
                ensemble_members.append(float(m_data[0]))

        result = ForecastResult(
            city=city,
            date=date,
            model=model,
            temp_high_c=temp_high,
            members=ensemble_members,
        )

        conn.execute(
            "INSERT OR IGNORE INTO backtest_forecasts (city, date, model, temp_high_c, members_json) VALUES (?, ?, ?, ?, ?)",
            (city, date, model, temp_high, json.dumps(ensemble_members)),
        )
        conn.commit()
        return result

    async def fetch_historical_observations(
        self,
        client: httpx.AsyncClient,
        city: str,
        date: str,
    ) -> float | None:
        conn = self._get_conn()
        row = conn.execute(
            "SELECT * FROM backtest_observations WHERE city = ? AND date = ?",
            (city, date),
        ).fetchone()
        if row:
            return float(row["temp_high_c"])

        coords = CITY_COORDS.get(city)
        if not coords:
            return None

        lat, lon = coords
        try:
            resp = await client.get(
                ARCHIVE_URL,
                params={
                    "latitude": lat,
                    "longitude": lon,
                    "start_date": date,
                    "end_date": date,
                    "daily": "temperature_2m_max",
                    "timezone": "auto",
                },
                timeout=30.0,
            )
            resp.raise_for_status()
            data = resp.json()
        except httpx.HTTPError as e:
            log.warning("historical_obs_fetch_failed", city=city, date=date, error=str(e))
            return None

        daily = data.get("daily", {})
        temps = daily.get("temperature_2m_max", [])
        temp_high = float(temps[0]) if temps and isinstance(temps[0], (int, float)) else None
        if temp_high is None:
            return None

        conn.execute(
            "INSERT OR IGNORE INTO backtest_observations (city, date, temp_high_c) VALUES (?, ?, ?)",
            (city, date, temp_high),
        )
        conn.commit()
        return temp_high

    async def fetch_historical_market_prices(
        self,
        client: httpx.AsyncClient,
        event_id: str,
    ) -> dict[str, dict[str, float]]:
        conn = self._get_conn()
        rows = conn.execute(
            "SELECT * FROM backtest_prices WHERE event_id = ?",
            (event_id,),
        ).fetchall()
        if rows:
            return {r["bucket_key"]: {"yes": r["yes_price"], "no": r["no_price"]} for r in rows}

        clob_base = "https://clob.polymarket.com"
        try:
            resp = await client.get(
                f"{clob_base}/prices-history",
                params={"market": event_id, "fidelity": "day"},
                timeout=15.0,
            )
            resp.raise_for_status()
            data = resp.json()
        except httpx.HTTPError as e:
            log.warning("historical_prices_fetch_failed", event_id=event_id, error=str(e))
            return {}

        history = data.get("history", [])
        if not history:
            return {}

        latest = history[-1] if isinstance(history, list) else {}
        prices: dict[str, dict[str, float]] = {}

        for key, val in latest.items():
            if key.startswith("price_") or key in ("yes_price", "no_price"):
                bucket_key = key
                yes_p = float(val) if isinstance(val, (int, float)) else 0.0
                prices[bucket_key] = {"yes": yes_p, "no": 1.0 - yes_p}
                conn.execute(
                    "INSERT OR IGNORE INTO backtest_prices (event_id, bucket_key, yes_price, no_price) VALUES (?, ?, ?, ?)",
                    (event_id, bucket_key, yes_p, 1.0 - yes_p),
                )

        conn.commit()
        return prices

    def close(self) -> None:
        if self._conn:
            self._conn.close()
            self._conn = None
