"""EMOS training script.

Collects historical ensemble forecasts and observations,
then trains EMOS calibration coefficients.

Usage:
    python -m pm_bot.scripts.train_emos --city "New York" --days 90
    python -m pm_bot.scripts.train_emos --all-cities --days 60
"""

from __future__ import annotations

import asyncio
import json
from datetime import datetime, timedelta
from pathlib import Path

import httpx
import numpy as np
import structlog

from pm_bot.core.emos import EMOSCalibrator, EMOSTrainingData
from pm_bot.models.config import CITY_COORDS

log = structlog.get_logger()

OPEN_METEO_BASE = "https://api.open-meteo.com/v1"
ENSEMBLE_BASE = "https://ensemble-api.open-meteo.com/v1/ensemble"
ARCHIVE_BASE = "https://archive-api.open-meteo.com/v1/archive"

# Data storage
DATA_DIR = Path("data/emos")


async def collect_training_data(
    client: httpx.AsyncClient,
    city: str,
    days: int = 90,
    model: str = "gfs_seamless",
) -> EMOSTrainingData:
    """Collect historical ensemble forecasts and observations for EMOS training.

    Args:
        client: HTTP client
        city: City name
        days: Number of days of history
        model: Weather model to use

    Returns:
        Training data with ensemble means, variances, and observations
    """
    coords = CITY_COORDS.get(city)
    if not coords:
        raise ValueError(f"Unknown city: {city}")

    lat, lon = coords
    data = EMOSTrainingData()

    end_date = datetime.now() - timedelta(days=1)  # Yesterday
    start_date = end_date - timedelta(days=days)

    log.info(
        "collecting_training_data",
        city=city,
        start=start_date.strftime("%Y-%m-%d"),
        end=end_date.strftime("%Y-%m-%d"),
        model=model,
    )

    # Fetch historical observations
    observations = await _fetch_observations(client, lat, lon, start_date, end_date)

    # Fetch historical ensemble forecasts (one day at a time for accuracy)
    current = start_date
    while current <= end_date:
        date_str = current.strftime("%Y-%m-%d")

        # Fetch ensemble forecast for this date
        ensemble = await _fetch_historical_ensemble(
            client, lat, lon, date_str, model
        )

        if ensemble and date_str in observations:
            obs = observations[date_str]
            arr = np.array(ensemble)
            data.ensemble_means.append(float(np.mean(arr)))
            data.ensemble_vars.append(float(np.var(arr)))
            data.observations.append(obs)

        current += timedelta(days=1)

    log.info(
        "training_data_collected",
        city=city,
        samples=len(data.observations),
    )

    return data


async def _fetch_observations(
    client: httpx.AsyncClient,
    lat: float,
    lon: float,
    start: datetime,
    end: datetime,
) -> dict[str, float]:
    """Fetch historical observations from Open-Meteo archive API."""
    params = {
        "latitude": lat,
        "longitude": lon,
        "start_date": start.strftime("%Y-%m-%d"),
        "end_date": end.strftime("%Y-%m-%d"),
        "daily": "temperature_2m_max",
        "timezone": "auto",
    }

    try:
        resp = await client.get(ARCHIVE_BASE, params=params)
        resp.raise_for_status()
        data = resp.json()

        daily = data.get("daily", {})
        dates = daily.get("time", [])
        temps = daily.get("temperature_2m_max", [])

        result = {}
        for date, temp in zip(dates, temps):
            if temp is not None:
                result[date] = float(temp)

        return result
    except httpx.HTTPError as e:
        log.error("observation_fetch_failed", error=str(e))
        return {}


async def _fetch_historical_ensemble(
    client: httpx.AsyncClient,
    lat: float,
    lon: float,
    date: str,
    model: str,
) -> list[float] | None:
    """Fetch ensemble forecast for a historical date.

    Uses the historical-forecast-api endpoint which provides
    forecasts as they were issued on that date.

    Note: Historical forecast API only provides deterministic forecast,
    not ensemble members. We estimate ensemble from deterministic +
    typical model spread.
    """
    params = {
        "latitude": lat,
        "longitude": lon,
        "start_date": date,
        "end_date": date,
        "daily": "temperature_2m_max",
        "timezone": "auto",
        "models": model,
    }

    try:
        # Use historical forecast API
        url = "https://historical-forecast-api.open-meteo.com/v1/forecast"
        resp = await client.get(url, params=params)
        resp.raise_for_status()
        data = resp.json()

        daily = data.get("daily", {})
        temps = daily.get("temperature_2m_max", [])

        if not temps or temps[0] is None:
            return None

        # Get deterministic forecast
        det_temp = float(temps[0])

        # Generate synthetic ensemble from deterministic + typical spread
        # GFS typical spread is ~2-3°C for day 1-2 forecasts
        import numpy as np
        np.random.seed(hash(date) % 2**32)
        spread = 2.5  # Typical GFS spread
        ensemble = [det_temp + np.random.normal(0, spread) for _ in range(31)]

        return ensemble

    except httpx.HTTPError as e:
        log.debug("historical_ensemble_failed", date=date, error=str(e))
        return None


async def train_city(
    city: str,
    days: int = 90,
    save: bool = True,
) -> EMOSCalibrator:
    """Train EMOS calibrator for a single city.

    Args:
        city: City name
        days: Days of history to use
        save: Whether to save trained coefficients

    Returns:
        Trained EMOS calibrator
    """
    DATA_DIR.mkdir(parents=True, exist_ok=True)

    async with httpx.AsyncClient(timeout=30.0) as client:
        data = await collect_training_data(client, city, days)

        if len(data.observations) < 10:
            log.warning("insufficient_data", city=city, n=len(data.observations))
            return EMOSCalibrator(city=city)

        calibrator = EMOSCalibrator(city=city)
        calibrator.train(data)

        if save:
            path = DATA_DIR / f"emos_{city.lower().replace(' ', '_')}.json"
            calibrator.save(path)
            log.info("calibrator_saved", city=city, path=str(path))

        return calibrator


async def train_all_cities(
    days: int = 60,
    cities: list[str] | None = None,
) -> dict[str, EMOSCalibrator]:
    """Train EMOS calibrators for multiple cities.

    Args:
        days: Days of history to use
        cities: Specific cities (default: all)

    Returns:
        Dict of city -> calibrator
    """
    target_cities = cities or list(CITY_COORDS.keys())
    calibrators: dict[str, EMOSCalibrator] = {}

    for city in target_cities:
        if city not in CITY_COORDS:
            continue

        log.info("training_city", city=city)
        try:
            calibrator = await train_city(city, days)
            calibrators[city] = calibrator
        except Exception as e:
            log.error("training_failed", city=city, error=str(e))

    return calibrators


def main():
    """CLI entry point for EMOS training."""
    import argparse

    parser = argparse.ArgumentParser(description="Train EMOS calibrators")
    parser.add_argument("--city", type=str, help="City to train")
    parser.add_argument("--all-cities", action="store_true", help="Train all cities")
    parser.add_argument("--days", type=int, default=90, help="Days of history")
    args = parser.parse_args()

    if args.all_cities:
        calibrators = asyncio.run(train_all_cities(days=args.days))
        print(f"Trained calibrators for {len(calibrators)} cities")
    elif args.city:
        calibrator = asyncio.run(train_city(args.city, days=args.days))
        print(f"Trained calibrator for {args.city}: {calibrator.coeffs}")
    else:
        parser.print_help()


if __name__ == "__main__":
    main()
