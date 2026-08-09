"""EMOS (Ensemble Model Output Statistics) calibration module.

Implements Gaussian EMOS (Nonhomogeneous Gaussian Regression) for calibrating
ensemble temperature forecasts. Fixes ensemble underdispersion and bias.

Reference:
- Gneiting et al. (2005) "Calibrated Probabilistic Forecasting Using Ensemble
  Model Output Statistics and Minimum CRPS Estimation"
- polymarket-tmax-lab implementation

The EMOS model produces a predictive Gaussian distribution:
  N(location, scale²)
where:
  location = a + b * ensemble_mean
  scale² = c + d * ensemble_variance

Coefficients (a, b, c, d) are trained on historical data by minimizing CRPS.
"""

from __future__ import annotations

import json
from dataclasses import dataclass, field
from math import erf, sqrt
from pathlib import Path

import structlog

log = structlog.get_logger()

# Default coefficients (will be overridden by training)
DEFAULT_COEFFS = {
    "a": 0.0,  # intercept
    "b": 1.0,  # slope for ensemble mean
    "c": 4.0,  # intercept for scale²
    "d": 0.5,  # slope for ensemble variance
}


@dataclass
class EMOSResult:
    """Calibrated forecast result."""

    location: float  # Calibrated mean (°C)
    scale: float  # Calibrated std dev (°C)
    coeffs: dict[str, float]  # Coefficients used
    raw_mean: float  # Original ensemble mean
    raw_std: float  # Original ensemble std


@dataclass
class EMOSTrainingData:
    """Historical data for EMOS training."""

    ensemble_means: list[float] = field(default_factory=list)
    ensemble_vars: list[float] = field(default_factory=list)
    observations: list[float] = field(default_factory=list)


class EMOSCalibrator:
    """Gaussian EMOS calibrator for temperature forecasts.

    Usage:
        calibrator = EMOSCalibrator()
        # Train on historical data
        calibrator.train(historical_data)
        # Calibrate new forecasts
        result = calibrator.calibrate(ensemble_members)
    """

    def __init__(self, coeffs: dict[str, float] | None = None, city: str = ""):
        self.coeffs = coeffs or DEFAULT_COEFFS.copy()
        self.city = city
        self._trained = False

    def calibrate(self, ensemble_members: list[float]) -> EMOSResult:
        """Calibrate ensemble forecast using EMOS.

        Args:
            ensemble_members: List of ensemble member temperature values (°C)

        Returns:
            EMOSResult with calibrated location and scale
        """
        if not ensemble_members:
            return EMOSResult(
                location=0.0,
                scale=5.0,
                coeffs=self.coeffs,
                raw_mean=0.0,
                raw_std=5.0,
            )

        arr = __import__('numpy').array(ensemble_members)
        raw_mean = float(__import__('numpy').mean(arr))
        raw_var = float(__import__('numpy').var(arr)) if len(arr) > 1 else 4.0

        a = self.coeffs["a"]
        b = self.coeffs["b"]
        c = self.coeffs["c"]
        d = self.coeffs["d"]

        # EMOS formulas
        location = a + b * raw_mean
        scale_sq = c + d * raw_var
        scale = max(0.5, sqrt(max(0.01, scale_sq)))  # Floor at 0.5°C

        return EMOSResult(
            location=location,
            scale=scale,
            coeffs=self.coeffs,
            raw_mean=raw_mean,
            raw_std=sqrt(raw_var),
        )

    def calibrate_probability(
        self,
        ensemble_members: list[float],
        temp_low_c: float,
        temp_high_c: float,
        temp_unit: str = "C",
    ) -> float:
        """Calculate calibrated probability for a temperature bucket.

        Args:
            ensemble_members: Ensemble member values
            temp_low_c: Bucket lower bound (°C)
            temp_high: Bucket upper bound (°C or °F depending on temp_unit)
            temp_unit: "C" or "F"

        Returns:
            Calibrated probability [0, 1]
        """
        result = self.calibrate(ensemble_members)
        mu = result.location
        sigma = result.scale

        # Convert bucket bounds to Celsius if needed
        if temp_unit == "F":
            # Bucket bounds are in °F, convert to °C for comparison
            low_c = (temp_low_c - 32) / 1.8
            high_c = (temp_high_c - 32) / 1.8
        else:
            low_c = temp_low_c
            high_c = temp_high_c

        # Calculate probability using Gaussian CDF
        TAIL_BOUND = 999.0

        if high_c >= TAIL_BOUND:
            # Right tail: P(X >= low)
            z = (low_c - mu) / sigma
            p = 0.5 * (1.0 - erf(z / sqrt(2)))
        elif low_c <= -TAIL_BOUND:
            # Left tail: P(X <= high)
            z = (high_c - mu) / sigma
            p = 0.5 * (1.0 + erf(z / sqrt(2)))
        else:
            # Interval: P(low <= X <= high)
            # For 2°F buckets, approximate as continuous
            z_low = (low_c - mu) / sigma
            z_high = (high_c - mu) / sigma
            p = 0.5 * (erf(z_high / sqrt(2)) - erf(z_low / sqrt(2)))

        return max(0.0, min(1.0, p))

    def train(self, data: EMOSTrainingData) -> dict[str, float]:
        """Train EMOS coefficients by minimizing CRPS.

        Uses scipy.optimize.minimize if available, otherwise falls back to
        a simple grid search.

        Args:
            data: Training data with ensemble means, variances, and observations

        Returns:
            Trained coefficients
        """
        if len(data.observations) < 10:
            log.warning("emos_insufficient_data", n=len(data.observations))
            return self.coeffs

        import numpy as np
        means = np.array(data.ensemble_means)
        vars_ = np.array(data.ensemble_vars)
        obs = np.array(data.observations)

        try:
            from scipy.optimize import minimize

            def crps_loss(params):
                a, b, c, d = params
                loc = a + b * means
                scale_sq = c + d * vars_
                scale = np.sqrt(np.maximum(0.01, scale_sq))

                # CRPS for Gaussian distribution
                z = (obs - loc) / scale
                crps = scale * (z * (2 * norm_cdf(z) - 1) + 2 * norm_pdf(z) - 1 / sqrt(np.pi))
                return float(np.mean(crps))

            from scipy.stats import norm as norm_dist

            norm_cdf = norm_dist.cdf
            norm_pdf = norm_dist.pdf

            # Initial guess
            x0 = [0.0, 1.0, 4.0, 0.5]
            result = minimize(crps_loss, x0, method="Nelder-Mead")

            if result.success:
                self.coeffs = {
                    "a": float(result.x[0]),
                    "b": float(result.x[1]),
                    "c": max(0.01, float(result.x[2])),
                    "d": max(0.01, float(result.x[3])),
                }
                self._trained = True
                log.info(
                    "emos_trained",
                    city=self.city,
                    n=len(data.observations),
                    coeffs=self.coeffs,
                    crps=float(result.fun),
                )
            else:
                log.warning("emos_optimization_failed", message=result.message)

        except ImportError:
            # Fallback: simple grid search
            log.info("emos_fallback_grid_search")
            self._train_grid_search(means, vars_, obs)

        return self.coeffs

    def _train_grid_search(
        self,
        means: 'np.ndarray',
        vars_: 'np.ndarray',
        obs: 'np.ndarray',
    ) -> None:
        """Simple grid search for EMOS coefficients."""
        import numpy as np
        best_crps = float("inf")
        best_coeffs = self.coeffs.copy()

        for a in [-1.0, 0.0, 1.0]:
            for b in [0.8, 0.9, 1.0, 1.1, 1.2]:
                for c in [1.0, 2.0, 4.0, 8.0]:
                    for d in [0.2, 0.5, 1.0, 2.0]:
                        loc = a + b * means
                        scale_sq = c + d * vars_
                        scale = np.sqrt(np.maximum(0.01, scale_sq))

                        z = (obs - loc) / scale
                        # Approximate CRPS
                        crps = float(np.mean(np.abs(obs - loc)))
                        if crps < best_crps:
                            best_crps = crps
                            best_coeffs = {"a": a, "b": b, "c": c, "d": d}

        self.coeffs = best_coeffs
        self._trained = True

    def save(self, path: Path) -> None:
        """Save coefficients to JSON file."""
        data = {
            "city": self.city,
            "coeffs": self.coeffs,
            "trained": self._trained,
        }
        path.write_text(json.dumps(data, indent=2))

    @classmethod
    def load(cls, path: Path) -> EMOSCalibrator:
        """Load coefficients from JSON file."""
        data = json.loads(path.read_text())
        calibrator = cls(coeffs=data["coeffs"], city=data.get("city", ""))
        calibrator._trained = data.get("trained", True)
        return calibrator


def bucket_probability_emos(
    calibrator: EMOSCalibrator,
    ensemble_members: list[float],
    temp_low_c: float,
    temp_high_c: float,
    temp_unit: str = "C",
) -> float:
    """Calculate calibrated bucket probability using EMOS.

    This is the main entry point for strategy code.

    Args:
        calibrator: Trained EMOS calibrator
        ensemble_members: Raw ensemble member values
        temp_low_c: Bucket lower bound
        temp_high_c: Bucket upper bound
        temp_unit: "C" or "F"

    Returns:
        Calibrated probability [0, 1]
    """
    if not ensemble_members:
        return 0.0

    return calibrator.calibrate_probability(
        ensemble_members,
        temp_low_c,
        temp_high_c,
        temp_unit,
    )


def train_from_csv(csv_path: Path, city: str = "") -> EMOSCalibrator:
    """Train EMOS calibrator from a CSV file.

    Expected CSV columns: date, ensemble_mean, ensemble_var, observation
    """
    import csv

    data = EMOSTrainingData()
    with open(csv_path) as f:
        reader = csv.DictReader(f)
        for row in reader:
            data.ensemble_means.append(float(row["ensemble_mean"]))
            data.ensemble_vars.append(float(row["ensemble_var"]))
            data.observations.append(float(row["observation"]))

    calibrator = EMOSCalibrator(city=city)
    calibrator.train(data)
    return calibrator
