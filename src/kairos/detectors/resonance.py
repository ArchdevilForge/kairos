"""Multi-dimension resonance scorer with Signal Quality Score.

Signal Quality Score (0-100):
  - Extremity (0-40): max Z-score across dimensions
  - Resonance (0-30): 2→10, 3→20, 4+→30
  - Direction (0-20): all dimensions point same way
  - Context (0-10): funding + OI combo

  Push if score ≥ min_score (default 55). Tune per config.
"""

from __future__ import annotations

import logging
import time
import threading
from dataclasses import dataclass, field
from typing import Any, Callable

from kairos.detectors.base import AnomalyEvent

logger = logging.getLogger(__name__)


@dataclass
class ResonanceEvent:
    """A multi-dimension resonance-scored alert."""

    symbol: str
    signal_score: float
    dimension_count: int
    dimensions: dict[str, AnomalyEvent]
    timestamp: float = field(default_factory=time.time)

    def to_alert_event(self) -> AnomalyEvent:
        data: dict[str, Any] = {
            "signal_score": self.signal_score,
            "dimension_count": self.dimension_count,
            "dimensions": list(self.dimensions.keys()),
        }
        for dim, ev in self.dimensions.items():
            data[f"{dim}_data"] = ev.data
        return AnomalyEvent(
            symbol=self.symbol,
            event_type="resonance",
            severity="HIGH",
            data=data,
            timestamp=self.timestamp,
        )


class ResonanceScorer:
    """Aggregates AnomalyEvents from detectors. Pushes when score ≥ min_score."""

    def __init__(self, config: dict | None = None) -> None:
        cfg = config or {}
        rs = cfg.get("resonanceScorer", cfg)
        self.enabled = bool(rs.get("enabled", True))
        self.window_seconds = float(rs.get("windowSeconds", 300))
        self.min_dimensions = int(rs.get("minDimensions", 2))
        self.min_score = float(rs.get("minScore", 55))
        self.cooldown_seconds = float(rs.get("cooldownSeconds", 600))

        self._windows: dict[str, dict[str, AnomalyEvent]] = {}
        self._last_emission: dict[str, float] = {}
        self._callbacks: list[Callable[[ResonanceEvent], None]] = []
        self._lock = threading.RLock()

    def on_event(self, event: AnomalyEvent) -> None:
        if not self.enabled or event.event_type == "resonance":
            return
        with self._lock:
            now = event.timestamp or time.time()
            self._prune_windows(now)
            symbol = event.symbol
            if symbol not in self._windows:
                self._windows[symbol] = {}
            existing = self._windows[symbol].get(event.event_type)
            if existing and _extremity_z(existing.data) >= _extremity_z(event.data):
                return
            self._windows[symbol][event.event_type] = event
            self._evaluate(symbol, now)

    def set_callback(self, callback: Callable[[ResonanceEvent], None]) -> None:
        self._callbacks.append(callback)

    def _prune_windows(self, now: float) -> None:
        cutoff = now - self.window_seconds
        expired = [sym for sym, dims in self._windows.items()
                   if all(e.timestamp < cutoff for e in dims.values())]
        for sym in expired:
            del self._windows[sym]

    def _evaluate(self, symbol: str, now: float) -> None:
        dims = self._windows.get(symbol)
        if not dims or len(dims) < self.min_dimensions:
            return
        if now - self._last_emission.get(symbol, 0.0) < self.cooldown_seconds:
            return

        score = _signal_quality_score(dims)
        if score < self.min_score:
            return

        r = ResonanceEvent(symbol=symbol, signal_score=round(score),
                           dimension_count=len(dims), dimensions=dict(dims),
                           timestamp=now)
        self._last_emission[symbol] = now
        for cb in self._callbacks:
            try:
                cb(r)
            except Exception as exc:
                logger.error("ResonanceScorer callback error: %s", exc)

    def reset(self) -> None:
        with self._lock:
            self._windows.clear()
            self._last_emission.clear()


# ── Signal Quality Score ──────────────────────────────────────────

def _signal_quality_score(dims: dict[str, AnomalyEvent]) -> float:
    max_z = max((_extremity_z(e.data) for e in dims.values()), default=0.0)
    if max_z >= 5.0:
        extremity = 40
    elif max_z >= 4.0:
        extremity = 30
    elif max_z >= 3.0:
        extremity = 20
    elif max_z >= 2.5:
        extremity = 10
    else:
        max_sev = max((_sev_score(e.severity) for e in dims.values()), default=1)
        extremity = (max_sev - 1) * 10

    n = len(dims)
    resonance = min(n, 4) * 10 - 10

    directions = [_direction_bias(e.event_type, e.data) for e in dims.values()]
    non_neutral = [d for d in directions if d != 0]
    if len(non_neutral) >= 2 and all(d > 0 for d in non_neutral):
        direction_score = 20
    elif len(non_neutral) >= 2 and all(d < 0 for d in non_neutral):
        direction_score = 20
    elif len(non_neutral) >= 2:
        has_funding = "funding_rate_anomaly" in dims
        has_ls = "long_short_ratio" in dims
        has_liq = "liquidation" in dims
        if has_funding and has_ls:
            f_dir = _direction_bias("funding_rate_anomaly", dims["funding_rate_anomaly"].data)
            ls_dir = _direction_bias("long_short_ratio", dims["long_short_ratio"].data)
            direction_score = 10 if (f_dir != 0 and ls_dir != 0 and f_dir == ls_dir) else 0
        elif has_liq:
            direction_score = 10
        else:
            direction_score = 0
    else:
        direction_score = 10

    ctx_bonus = 0
    if "funding_rate_anomaly" in dims and "open_interest_change" in dims:
        ctx_bonus = 5  # funding + OI = crowded trade confirmation

    return min(extremity + resonance + direction_score + ctx_bonus, 100.0)


def _extremity_z(data: dict) -> float:
    z = _get_float(data, "zscore")
    if z is not None and z > 0:
        return abs(z)
    rate = abs(_get_float_or_zero(data, "funding_rate"))
    if rate >= 0.002:
        return 6.0
    if rate >= 0.001:
        return 4.0
    if rate >= 0.0005:
        return 2.5
    millions = _get_float_or_zero(data, "total_liquidation_millions")
    if millions >= 20:
        return 6.0
    if millions >= 10:
        return 4.0
    if millions >= 5:
        return 3.0
    oi = abs(_get_float_or_zero(data, "change_pct"))
    if oi >= 30:
        return 5.0
    if oi >= 15:
        return 3.0
    vel = abs(_get_float_or_zero(data, "change_pct"))
    if vel >= 3:
        return 4.0
    if vel >= 1.5:
        return 2.5
    vol = _get_float_or_zero(data, "ratio")
    if vol >= 15:
        return 5.0
    if vol >= 10:
        return 3.0
    ratio = _get_float(data, "ls_ratio")
    if ratio and abs(ratio - 1) > 3:
        return 5.0
    if data.get("long_rate", 0) >= 85:
        return 4.0
    if data.get("long_rate", 0) >= 80:
        return 2.5
    return 0.0


def _direction_bias(event_type: str, data: dict) -> int:
    if event_type == "price_velocity":
        pct = _get_float_or_zero(data, "change_pct")
        return 1 if pct > 0 else -1
    if event_type == "volume_spike":
        return 0
    if event_type == "open_interest_change":
        return 0
    if event_type == "funding_rate_anomaly":
        rate = _get_float_or_zero(data, "funding_rate")
        return -1 if rate > 0 else 1
    if event_type == "long_short_ratio":
        long_r = _get_float_or_zero(data, "long_rate")
        return -1 if long_r > 60 else (1 if long_r < 40 else 0)
    if event_type == "liquidation":
        long_pct = _get_float_or_zero(data, "long_liquidation_pct")
        return -1 if long_pct > 60 else (1 if long_pct < 40 else 0)
    return 0


def _get_float(data: dict, key: str, default: float | None = None) -> float | None:
    v = data.get(key)
    if v is None:
        return default
    try:
        return float(v)
    except (TypeError, ValueError):
        return default


def _get_float_or_zero(data: dict, key: str) -> float:
    v = data.get(key)
    if v is None:
        return 0.0
    try:
        return float(v)
    except (TypeError, ValueError):
        return 0.0


def _sev_score(s: str) -> int:
    return {"LOW": 1, "MEDIUM": 2, "HIGH": 3}.get(s.strip().upper(), 1)
