"""Long/Short Ratio anomaly detector using CoinGlass data.

Polls CoinGlass for per-symbol long/short ratio snapshots and emits anomalies
when the ratio reaches extreme levels (Z-score or absolute threshold) or
when it shifts rapidly.
"""

from __future__ import annotations

import logging
import threading
import time
from typing import Any

from kairos.detectors.base import AnomalyEvent, BaseDetector
from kairos.utils.zscore import ZScoreTracker

logger = logging.getLogger(__name__)


class LongShortRatioDetector(BaseDetector):
    """Detects anomalous long/short ratio levels and shifts.

    Two trigger modes (any match → anomaly):
      1. Absolute: long_rate exceeds abs_threshold (e.g. >80% or <20%)
      2. Z-score:  abs(z_score(long_rate)) > zscore_threshold
      3. Velocity: ratio shifted by more than velocity_threshold_pct within one poll interval

    Poll periodically via ``on_ls_snapshot(symbol, timestamp, long_rate, short_rate)``.
    """

    def __init__(self, config: dict) -> None:
        super().__init__(config)
        ls = config.get("longShortRatio", config)

        self.enabled = bool(ls.get("enabled", True))
        self.poll_interval_s = float(ls.get("pollIntervalSeconds", 300))
        self.abs_threshold = float(ls.get("absThreshold", 80.0))  # long_rate %
        self.zscore_threshold = float(ls.get("zscoreThreshold", 2.5))
        self.zscore_window = int(ls.get("zscoreWindow", 48))  # ~4h at 5min polls
        self.velocity_threshold_pct = float(ls.get("velocityThresholdPct", 15.0))  # % change
        self.min_notify_seconds = float(
            _parse_seconds_config(ls.get("minNotifyInterval", "30m"), 1800)
        )

        # symbol -> ZScoreTracker for long_rate %
        self._zscore: dict[str, ZScoreTracker] = {}
        # symbol -> last (timestamp, long_rate, short_rate)
        self._last: dict[str, tuple[float, float, float]] = {}
        # symbol_event_type -> cooldown timestamp
        self._last_notify: dict[str, float] = {}
        self._lock = threading.RLock()

    def on_ls_snapshot(
        self,
        symbol: str,
        timestamp: float,
        long_rate: float,
        short_rate: float,
    ) -> None:
        """Process one long/short ratio snapshot from polling."""
        if not self.enabled:
            return
        if long_rate is None or long_rate < 0 or long_rate > 100:
            return

        with self._lock:
            if symbol not in self._zscore:
                self._zscore[symbol] = ZScoreTracker(window=self.zscore_window, min_samples=5)

            zt = self._zscore[symbol]
            prev = self._last.get(symbol)

            # 1. Absolute threshold
            if long_rate >= self.abs_threshold or long_rate <= (100.0 - self.abs_threshold):
                label = "long_excessive" if long_rate >= self.abs_threshold else "short_excessive"
                if self._can_notify(symbol, label, timestamp):
                    self._emit_anomaly(symbol, timestamp, long_rate, short_rate, label, long_rate, None)
                    # still record for zscore but skip velocity if we just emitted
                    zt.add(long_rate)
                    self._last[symbol] = (timestamp, long_rate, short_rate)
                    return

            # 2. Velocity check (rapid shift vs previous poll)
            if prev is not None:
                prev_long, prev_short = prev[1], prev[2]
                if prev_long > 0:
                    shift_pct = abs(long_rate - prev_long) / prev_long * 100
                    if shift_pct >= self.velocity_threshold_pct:
                        label = "ls_velocity"
                        if self._can_notify(symbol, label, timestamp):
                            self._emit_anomaly(symbol, timestamp, long_rate, short_rate, label, shift_pct, None)

            # 3. Z-score against own history
            zs = zt.add_and_score(long_rate)
            if zs is not None and abs(zs) >= self.zscore_threshold:
                label = "ls_zscore"
                if self._can_notify(symbol, label, timestamp):
                    self._emit_anomaly(symbol, timestamp, long_rate, short_rate, label, long_rate, zs)

            self._last[symbol] = (timestamp, long_rate, short_rate)

    def _emit_anomaly(
        self,
        symbol: str,
        timestamp: float,
        long_rate: float,
        short_rate: float,
        reason: str,
        value: float | None,
        zscore: float | None,
    ) -> None:
        abs_val = abs(value) if value is not None else 0.0
        if abs_val >= 80.0 or (zscore is not None and abs(zscore) >= 4.0):
            severity = "HIGH"
        elif abs_val >= 60.0 or (zscore is not None and abs(zscore) >= 3.0):
            severity = "MEDIUM"
        else:
            severity = "LOW"

        self._emit(
            AnomalyEvent(
                symbol=symbol,
                event_type="long_short_ratio",
                severity=severity,
                data={
                    "long_rate": round(long_rate, 2),
                    "short_rate": round(short_rate, 2),
                    "ls_ratio": round(long_rate / short_rate, 4) if short_rate > 0 else None,
                    "reason": reason,
                    "trigger_value": round(value, 4) if value is not None else None,
                    "zscore": round(zscore, 4) if zscore is not None else None,
                    "threshold_abs": self.abs_threshold,
                    "threshold_zscore": self.zscore_threshold,
                    "threshold_velocity_pct": self.velocity_threshold_pct,
                },
                timestamp=timestamp,
            )
        )

    def _can_notify(self, symbol: str, label: str, now: float) -> bool:
        key = f"{symbol}__ls__{label}"
        last = self._last_notify.get(key, 0.0)
        if now - last < self.min_notify_seconds:
            return False
        self._last_notify[key] = now
        return True

    def update_config(self, config: dict) -> None:
        with self._lock:
            super().update_config(config)
            updated = LongShortRatioDetector(config)
            self.enabled = updated.enabled
            self.poll_interval_s = updated.poll_interval_s
            self.abs_threshold = updated.abs_threshold
            self.zscore_threshold = updated.zscore_threshold
            self.zscore_window = updated.zscore_window
            self.velocity_threshold_pct = updated.velocity_threshold_pct
            self.min_notify_seconds = updated.min_notify_seconds


def _parse_seconds_config(value: Any, default: float) -> float:
    """Parse a duration value (int/float/str like '30m') to seconds."""
    if isinstance(value, (int, float)):
        return float(value)
    s = str(value).strip().lower()
    if s.endswith("s"):
        return float(s[:-1])
    if s.endswith("m"):
        return float(s[:-1]) * 60
    if s.endswith("h"):
        return float(s[:-1]) * 3600
    return default
