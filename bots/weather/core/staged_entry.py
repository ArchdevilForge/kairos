"""Staged Entry — time-based position scaling.

All strategies can call ``apply_staged_entry()`` to reduce position size
when far from resolution, then scale up as forecast confidence grows.

Usage:
    from pm_bot.core.staged_entry import apply_staged_entry_for_event

    recs = apply_staged_entry_for_event(recs, event.date)
"""

from __future__ import annotations

from datetime import datetime, timezone
from typing import Sequence

from pm_bot.models.market import Recommendation

# Default stage thresholds and multipliers
_DEFAULT_STAGES: list[tuple[float, float]] = [
    (48.0, 0.0),  # > 48h: skip
    (24.0, 0.3),  # 48-24h: 30%
    (8.0, 0.6),  # 24-8h: 60%
    (0.0, 1.0),  # < 8h: full
]


def estimate_hours_to_resolution(date_str: str | None) -> float | None:
    """Estimate hours until weather market resolution.

    Weather markets in this project usually resolve around midnight US/Eastern.
    We approximate that as 05:00 UTC on the resolution date.
    """

    if not date_str:
        return None

    try:
        resolution = (
            datetime.strptime(date_str, "%Y-%m-%d")
            .replace(hour=5, minute=0, second=0, microsecond=0)
            .replace(tzinfo=timezone.utc)
        )
        now = datetime.now(timezone.utc)
        delta = (resolution - now).total_seconds() / 3600.0
        return max(0.0, delta)
    except (ValueError, TypeError):
        return None


def get_position_multiplier(
    hours_to_resolution: float,
    stages: Sequence[tuple[float, float]] | None = None,
) -> float:
    """Return position size multiplier based on hours until resolution.

    Args:
        hours_to_resolution: Hours until market resolution.
        stages: Optional custom stage list. Each tuple is (threshold_hours, multiplier).
                Must be sorted descending by threshold. Default stages:
                >48h → 0.0, 48-24h → 0.3, 24-8h → 0.6, <8h → 1.0

    Returns:
        Multiplier in [0.0, 1.0].
    """
    stage_list = list(stages) if stages is not None else _DEFAULT_STAGES

    for threshold, multiplier in stage_list:
        if hours_to_resolution > threshold:
            return multiplier

    return stage_list[-1][1] if stage_list else 1.0


def apply_staged_entry(
    recs: list[Recommendation],
    hours_to_resolution: float,
    stages: Sequence[tuple[float, float]] | None = None,
) -> list[Recommendation]:
    """Scale recommendation sizes by time-to-resolution multiplier.

    Filters out recommendations with zero multiplier.

    Args:
        recs: Recommendations from a strategy.
        hours_to_resolution: Hours until market resolution.
        stages: Optional custom stage list.

    Returns:
        Filtered and scaled recommendations.
    """
    multiplier = get_position_multiplier(hours_to_resolution, stages)
    if multiplier <= 0.0:
        return []

    result: list[Recommendation] = []
    for rec in recs:
        result.append(
            Recommendation(
                strategy=rec.strategy,
                event=rec.event,
                bucket=rec.bucket,
                direction=rec.direction,
                edge=rec.edge,
                reasoning=rec.reasoning,
                size_usd=rec.size_usd * multiplier,
                kelly_fraction=rec.kelly_fraction,
            )
        )
    return result


def apply_staged_entry_for_event(
    recs: list[Recommendation],
    event_date: str | None,
    stages: Sequence[tuple[float, float]] | None = None,
) -> list[Recommendation]:
    """Convenience wrapper: estimate hours from event date, then stage size."""
    hours = estimate_hours_to_resolution(event_date)
    if hours is None:
        return recs
    return apply_staged_entry(recs, hours, stages=stages)
