"""Multi-model ensemble for weather forecasts.

Combines forecasts from multiple models to improve accuracy:
- GFS (NOAA) - US model, good for Americas
- ECMWF IFS - European model, generally most accurate
- GEM (Canada) - Good for North America
- ICON (Germany) - Good for Europe
- JMA (Japan) - Good for Asia

Uses weighted averaging based on historical performance.

Reference:
- polymarketweather.com: "4-model meteorological ensemble"
- degendoppler.com: "14-Model Ensemble Forecasts"
"""

from __future__ import annotations

import asyncio
from dataclasses import dataclass, field

import httpx
import numpy as np
import structlog

from pm_bot.models.config import CITY_COORDS
from pm_bot.models.market import ForecastResult

log = structlog.get_logger()

OPEN_METEO_BASE = "https://api.open-meteo.com/v1"
ENSEMBLE_BASE = "https://ensemble-api.open-meteo.com/v1/ensemble"

# Models to use (Open-Meteo supported)
ENSEMBLE_MODELS = [
    "gfs_seamless",  # GFS (31 members)
    "ecmwf_ifs025",  # ECMWF IFS (51 members)
    "icon_global",  # ICON (40 members)
    "gem_global",  # GEM (20 members)
]

# Default weights (can be trained)
DEFAULT_WEIGHTS = {
    "gfs_seamless": 0.30,
    "ecmwf_ifs025": 0.35,
    "icon_global": 0.20,
    "gem_global": 0.15,
}


@dataclass
class ModelForecast:
    """Single model forecast."""

    model: str
    mean_c: float
    std_c: float
    members: list[float]
    weight: float = 1.0


@dataclass
class EnsembleForecast:
    """Combined multi-model forecast."""

    city: str
    date: str
    models: list[ModelForecast]
    weighted_mean: float
    weighted_std: float
    combined_members: list[float]
    agreement_score: float  # 0-1, higher = models agree more

    def to_forecast_result(self) -> ForecastResult:
        """Convert to standard ForecastResult."""
        return ForecastResult(
            city=self.city,
            date=self.date,
            model="ensemble",
            temp_high_c=self.weighted_mean,
            measure_type="high",
            members=self.combined_members,
            std=self.weighted_std,
        )


class MultiModelEnsemble:
    """Fetch and combine forecasts from multiple weather models.

    Usage:
        ensemble = MultiModelEnsemble()
        result = await ensemble.fetch_forecast(client, "New York", "2026-05-15")
    """

    def __init__(
        self,
        models: list[str] | None = None,
        weights: dict[str, float] | None = None,
    ):
        self.models = models or ENSEMBLE_MODELS
        self.weights = weights or DEFAULT_WEIGHTS.copy()

    async def fetch_forecast(
        self,
        client: httpx.AsyncClient,
        city: str,
        date: str = "",
    ) -> EnsembleForecast | None:
        """Fetch and combine forecasts from multiple models.

        Args:
            client: HTTP client
            city: City name
            date: Target date (optional)

        Returns:
            Combined ensemble forecast, or None if all models fail
        """
        coords = CITY_COORDS.get(city)
        if not coords:
            log.warning("unknown_city", city=city)
            return None

        lat, lon = coords

        # Fetch all models concurrently
        tasks = [
            self._fetch_model(client, model, lat, lon)
            for model in self.models
        ]
        results = await asyncio.gather(*tasks, return_exceptions=True)

        # Collect successful forecasts
        forecasts: list[ModelForecast] = []
        for model, result in zip(self.models, results):
            if isinstance(result, Exception):
                log.warning("model_fetch_failed", model=model, error=str(result))
                continue
            if result is not None:
                forecasts.append(result)

        if not forecasts:
            log.error("all_models_failed", city=city)
            return None

        # Calculate weighted statistics
        weight_sum = sum(f.weight for f in forecasts)
        if weight_sum <= 0:
            weight_sum = 1.0

        weighted_mean = sum(f.mean_c * f.weight for f in forecasts) / weight_sum
        weighted_var = sum((f.std_c**2 + (f.mean_c - weighted_mean) ** 2) * f.weight for f in forecasts) / weight_sum
        weighted_std = max(0.5, np.sqrt(weighted_var))

        # Combine members (weighted sampling)
        combined_members = self._combine_members(forecasts)

        # Agreement score: penalize spread between models
        means = [f.mean_c for f in forecasts]
        spread = max(means) - min(means) if len(means) > 1 else 0
        agreement = max(0, 1 - spread / 5.0)  # 5°C spread = 0 agreement

        result = EnsembleForecast(
            city=city,
            date=date,
            models=forecasts,
            weighted_mean=weighted_mean,
            weighted_std=weighted_std,
            combined_members=combined_members,
            agreement_score=agreement,
        )

        log.info(
            "ensemble_forecast",
            city=city,
            n_models=len(forecasts),
            mean=f"{weighted_mean:.1f}",
            std=f"{weighted_std:.1f}",
            agreement=f"{agreement:.2f}",
        )

        return result

    async def _fetch_model(
        self,
        client: httpx.AsyncClient,
        model: str,
        lat: float,
        lon: float,
    ) -> ModelForecast | None:
        """Fetch ensemble members from a single model."""
        params = {
            "latitude": lat,
            "longitude": lon,
            "daily": "temperature_2m_max",
            "forecast_days": 3,
            "timezone": "auto",
            "models": model,
        }

        try:
            resp = await client.get(ENSEMBLE_BASE, params=params)
            resp.raise_for_status()
            data = resp.json()
        except httpx.HTTPError as e:
            log.debug("model_fetch_error", model=model, error=str(e))
            return None

        daily = data.get("daily", {})
        members: list[float] = []

        # Extract all member data
        for key, values in daily.items():
            if key.startswith("temperature_2m_max_member"):
                if values and isinstance(values[0], (int, float)):
                    members.append(float(values[0]))

        if not members:
            return None

        arr = np.array(members)
        weight = self.weights.get(model, 0.1)

        return ModelForecast(
            model=model,
            mean_c=float(np.mean(arr)),
            std_c=float(np.std(arr)) if len(arr) > 1 else 2.0,
            members=members,
            weight=weight,
        )

    def _combine_members(
        self,
        forecasts: list[ModelForecast],
        target_size: int = 51,
    ) -> list[float]:
        """Combine members from multiple models using weighted sampling.

        Resamples each model's members proportionally to weight,
        then pools them.
        """
        combined: list[float] = []
        weight_sum = sum(f.weight for f in forecasts)

        for f in forecasts:
            # Sample proportional to weight
            n_samples = max(1, int(target_size * f.weight / weight_sum))
            if f.members:
                samples = np.random.choice(f.members, size=n_samples, replace=True)
                combined.extend(samples.tolist())

        # Shuffle to avoid ordering bias
        np.random.shuffle(combined)
        return combined[:target_size]


# Convenience function
async def fetch_ensemble_forecast(
    client: httpx.AsyncClient,
    city: str,
    date: str = "",
) -> ForecastResult | None:
    """Fetch ensemble forecast for a city.

    Returns standard ForecastResult with combined ensemble members.
    """
    ensemble = MultiModelEnsemble()
    result = await ensemble.fetch_forecast(client, city, date)
    if result:
        return result.to_forecast_result()
    return None
