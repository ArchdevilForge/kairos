"""Rolling Z-score tracker for dynamic anomaly thresholds.

Replaces fixed-threshold comparisons with rolling-window statistics.
Each tracker maintains a deque of recent values and computes mean/std on
demand, so detectors can emit when abs(z_score) > threshold instead of
using hard-coded cutoffs.
"""

from __future__ import annotations

import collections
import math
import threading
from typing import Sequence


class ZScoreTracker:
    """Thread-safe rolling-window Z-score tracker.

    Usage::
        zt = ZScoreTracker(window=100)
        zt.add(42.0)
        zt.add(43.0)
        if abs(zt.zscore(44.0)) > 2.5:
            ...  # anomaly
    """

    def __init__(self, window: int = 100, min_samples: int = 10) -> None:
        if window < 1:
            raise ValueError(f"window must be >= 1, got {window}")
        if min_samples < 2:
            min_samples = 2
        self._window = window
        self._min_samples = min_samples
        self._values: collections.deque[float] = collections.deque(maxlen=window)
        self._lock = threading.RLock()

    def add(self, value: float) -> None:
        with self._lock:
            self._values.append(value)

    def zscore(self, value: float) -> float | None:
        with self._lock:
            n = len(self._values)
            if n < self._min_samples:
                return None
            avg = sum(self._values) / n
            variance = sum((v - avg) ** 2 for v in self._values) / (n - 1)
            if variance == 0:
                return 0.0
            return (value - avg) / math.sqrt(variance)

    def add_and_score(self, value: float) -> float | None:
        with self._lock:
            score = self.zscore(value)
            self._values.append(value)
            return score

    def reset(self) -> None:
        with self._lock:
            self._values.clear()
