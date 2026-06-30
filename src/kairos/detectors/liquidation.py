"""Liquidation anomaly detector using CoinGlass data.

Monitors per-symbol liquidation volumes (total, long, short) via periodic
CoinGlass polling and emits anomalies on:
  1. Absolute liquidation USD exceeding a threshold
  2. Z-score spike relative to the symbol's own history
  3. Long/short liquidation imbalance (one side dominating)
"""

from __future__ import annotations

import logging
import threading
import time
from typing import Any

from kairos.detectors.base import AnomalyEvent, BaseDetector
from kairos.utils.zscore import ZScoreTracker

logger = logging.getLogger(__name__)


class LiquidationDetector(BaseDetector):
    """Detects anomalous liquidation events from CoinGlass polling.

    Poll periodically via ``on_liquidation_snapshot(symbol, timestamp, data)``
    where *data* is the dict from ``_liquidation_today_context()``.
    """

    def __init__(self, config: dict) -> None:
        super().__init__(config)
        liq = config.get("liquidation", config)

        self.enabled = bool(liq.get("enabled", True))
        self.poll_interval_s = float(liq.get("pollIntervalSeconds", 300))
        self.abs_threshold_usd = float(liq.get("absThresholdUsd", 1_000_000))  # $1M+
        self.zscore_threshold = float(liq.get("zscoreThreshold", 2.5))
        self.zscore_window = int(liq.get("zscoreWindow", 48))  # ~4h at 5min polls
        self.imbalance_threshold = float(liq.get("imbalanceThreshold", 0.80))  # 80% one side
        self.min_notify_seconds = float(
            _parse_liq_seconds(liq.get("minNotifyInterval", "30m"), 1800)
        )

        # symbol -> ZScoreTracker for liquidation USD
        self._zscore: dict[str, ZScoreTracker] = {}
        # symbol -> last (timestamp, total_liq, long_liq, short_liq)
        self._last: dict[str, tuple[float, float, float, float]] = {}
        # key -> cooldown timestamp
        self._last_notify: dict[str, float] = {}
        self._lock = threading.RLock()

    def on_liquidation_snapshot(
        self,
        symbol: str,
        timestamp: float,
        total_liq_usd: float,
        long_liq_usd: float,
        short_liq_usd: float,
    ) -> None:
        """Process one liquidation data snapshot from polling."""
        if not self.enabled:
            return
        if total_liq_usd is None or total_liq_usd <= 0:
            return

        with self._lock:
            if symbol not in self._zscore:
                self._zscore[symbol] = ZScoreTracker(
                    window=self.zscore_window, min_samples=5
                )

            zt = self._zscore[symbol]
            prev = self._last.get(symbol)

            # 1. Absolute threshold
            if total_liq_usd >= self.abs_threshold_usd:
                label = "liq_absolute"
                if self._can_notify(symbol, "absolute", timestamp):
                    self._emit_anomaly(
                        symbol, timestamp, total_liq_usd, long_liq_usd, short_liq_usd,
                        label, total_liq_usd, None,
                    )

            # 2. Z-score spike
            zs = zt.add_and_score(total_liq_usd)
            if zs is not None and abs(zs) >= self.zscore_threshold:
                label = "liq_zscore"
                prev_liq = prev[1] if prev else None
                if self._can_notify(symbol, "zscore", timestamp):
                    self._emit_anomaly(
                        symbol, timestamp, total_liq_usd, long_liq_usd, short_liq_usd,
                        label, total_liq_usd, zs,
                    )

            # 3. Long/short imbalance (one side dominating)
            if total_liq_usd > 0:
                long_ratio = long_liq_usd / total_liq_usd if total_liq_usd > 0 else 0.5
                short_ratio = short_liq_usd / total_liq_usd if total_liq_usd > 0 else 0.5
                if long_ratio >= self.imbalance_threshold:
                    label = "liq_long_dominated"
                    if self._can_notify(symbol, "imbalance", timestamp):
                        self._emit_anomaly(
                            symbol, timestamp, total_liq_usd, long_liq_usd, short_liq_usd,
                            label, long_ratio, None,
                        )
                elif short_ratio >= self.imbalance_threshold:
                    label = "liq_short_dominated"
                    if self._can_notify(symbol, "imbalance", timestamp):
                        self._emit_anomaly(
                            symbol, timestamp, total_liq_usd, long_liq_usd, short_liq_usd,
                            label, short_ratio, None,
                        )

            self._last[symbol] = (timestamp, total_liq_usd, long_liq_usd, short_liq_usd)

    def _emit_anomaly(
        self,
        symbol: str,
        timestamp: float,
        total_liq_usd: float,
        long_liq_usd: float,
        short_liq_usd: float,
        reason: str,
        trigger_value: float | None,
        zscore: float | None,
    ) -> None:
        total_m = total_liq_usd / 1_000_000
        if total_liq_usd >= self.abs_threshold_usd * 5 or (
            zscore is not None and abs(zscore) >= 4.0
        ):
            severity = "HIGH"
        elif total_liq_usd >= self.abs_threshold_usd * 2 or (
            zscore is not None and abs(zscore) >= 3.2
        ):
            severity = "MEDIUM"
        else:
            severity = "LOW"

        long_pct = round(long_liq_usd / total_liq_usd * 100, 1) if total_liq_usd > 0 else 50.0
        short_pct = round(short_liq_usd / total_liq_usd * 100, 1) if total_liq_usd > 0 else 50.0

        self._emit(
            AnomalyEvent(
                symbol=symbol,
                event_type="liquidation",
                severity=severity,
                data={
                    "total_liquidation_usd": round(total_liq_usd, 2),
                    "total_liquidation_millions": round(total_m, 2),
                    "long_liquidation_usd": round(long_liq_usd, 2),
                    "short_liquidation_usd": round(short_liq_usd, 2),
                    "long_liquidation_pct": long_pct,
                    "short_liquidation_pct": short_pct,
                    "reason": reason,
                    "trigger_value": round(trigger_value, 4) if trigger_value is not None else None,
                    "zscore": round(zscore, 4) if zscore is not None else None,
                    "threshold_abs_usd": self.abs_threshold_usd,
                    "threshold_zscore": self.zscore_threshold,
                    "threshold_imbalance": self.imbalance_threshold,
                },
                timestamp=timestamp,
            )
        )

    def _can_notify(self, symbol: str, label: str, now: float) -> bool:
        key = f"{symbol}__liq__{label}"
        last = self._last_notify.get(key, 0.0)
        if now - last < self.min_notify_seconds:
            return False
        self._last_notify[key] = now
        return True

    def update_config(self, config: dict) -> None:
        with self._lock:
            super().update_config(config)
            updated = LiquidationDetector(config)
            self.enabled = updated.enabled
            self.poll_interval_s = updated.poll_interval_s
            self.abs_threshold_usd = updated.abs_threshold_usd
            self.zscore_threshold = updated.zscore_threshold
            self.zscore_window = updated.zscore_window
            self.imbalance_threshold = updated.imbalance_threshold
            self.min_notify_seconds = updated.min_notify_seconds


def _parse_liq_seconds(value: Any, default: float) -> float:
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
