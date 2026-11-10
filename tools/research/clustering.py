"""Wave-cluster assignment, cluster-robust SEs, and ex-ante feature guard (Work Items 3 & 4).

- Merges MarketPulse market-wide wake events <24h apart into one `wave_cluster_id`.
- Computes CR1 cluster-robust standard errors grouped by `wave_cluster_id`
  (naive i.i.d. standard errors are forbidden because same-wave alt trades co-move).
- Enforces the ex-ante feature rule (`t <= entry_ts`; realized holding duration
  and other ex-post stopping times are rejected as grouping keys).
"""
from __future__ import annotations

import math
from dataclasses import dataclass
from typing import Mapping, Sequence

WAVE_MERGE_GAP_SEC = 24 * 3600  # <24h strictly merges into the same wave cluster

FORBIDDEN_EX_POST_COLUMNS: frozenset[str] = frozenset(
    {
        "holding_duration",
        "holding_seconds",
        "hold_seconds",
        "hold_secs",
        "hold_minutes",
        "hold_mins",
        "holding_hours",
        "realized_duration",
        "exit_ts",
        "closed_at",
        "path_end_unix",
        "exit_reason",
        "exit_price",
        "realized_r",
        "net_r",
        "mechanical_r",
        "mfe",
        "mae",
        "stop_hit_first",
        "target_hit_first",
    }
)


class ExAnteViolationError(ValueError):
    """Raised when an experiment attempts to group or filter on ex-post information."""


@dataclass(frozen=True)
class WakeCluster:
    """Merged MarketPulse wave cluster spanning one or more wake events <24h apart."""

    wave_cluster_id: str
    start_ts: float
    last_wake_ts: float
    event_ids: tuple[str, ...]


@dataclass(frozen=True)
class ClusterMeanStat:
    """Cluster-robust summary statistic for NetR grouped by `wave_cluster_id`."""

    n_obs: int
    n_clusters: int
    mean: float
    cluster_se: float
    t_stat: float
    ci95_low: float
    ci95_high: float


def build_wave_clusters(
    wake_events: Sequence[tuple[str, float]],
    merge_gap_sec: float = WAVE_MERGE_GAP_SEC,
) -> tuple[list[WakeCluster], dict[str, str]]:
    """Merge MarketPulse wake events `< merge_gap_sec` (default 24h) apart into one cluster.

    Args:
        wake_events: Sequence of `(event_id, event_ts_unix_sec)`.
        merge_gap_sec: Strict gap threshold in seconds (`gap < merge_gap_sec` merges).

    Returns:
        `(clusters, event_to_cluster_id)`.
    """
    if not wake_events:
        return [], {}

    ordered = sorted(wake_events, key=lambda item: (float(item[1]), str(item[0])))
    clusters: list[WakeCluster] = []
    event_map: dict[str, str] = {}

    cur_events = [str(ordered[0][0])]
    cur_start = float(ordered[0][1])
    cur_last = cur_start
    cluster_idx = 1

    for ev_id_raw, ts_raw in ordered[1:]:
        ev_id = str(ev_id_raw)
        ts = float(ts_raw)
        # Strictly < 24h apart merges into the current wave cluster.
        if ts - cur_last < merge_gap_sec:
            cur_events.append(ev_id)
            cur_last = ts
        else:
            cid = f"wave_{cluster_idx:04d}"
            clusters.append(
                WakeCluster(
                    wave_cluster_id=cid,
                    start_ts=cur_start,
                    last_wake_ts=cur_last,
                    event_ids=tuple(cur_events),
                )
            )
            for eid in cur_events:
                event_map[eid] = cid
            cluster_idx += 1
            cur_events = [ev_id]
            cur_start = ts
            cur_last = ts

    cid = f"wave_{cluster_idx:04d}"
    clusters.append(
        WakeCluster(
            wave_cluster_id=cid,
            start_ts=cur_start,
            last_wake_ts=cur_last,
            event_ids=tuple(cur_events),
        )
    )
    for eid in cur_events:
        event_map[eid] = cid

    return clusters, event_map


def assign_signal_to_wave_cluster(
    signal_ts: float,
    clusters: Sequence[WakeCluster],
    max_wake_lag_sec: float = WAVE_MERGE_GAP_SEC,
) -> str:
    """Assign a candidate signal at `signal_ts` causally to its active `wave_cluster_id`."""
    best: WakeCluster | None = None
    for c in clusters:
        if c.start_ts <= signal_ts and signal_ts - c.last_wake_ts < max_wake_lag_sec:
            best = c
    if best is not None:
        return best.wave_cluster_id
    # Fallback deterministic 24h bucket when no prior pulse wake is within 24h.
    bucket = int(signal_ts // max_wake_lag_sec)
    return f"wave_unanchored_{bucket}"


def _t_crit_95(df: int) -> float:
    """Two-sided 95% Student-t critical value for `df = G - 1` degrees of freedom."""
    table = {
        1: 12.7062,
        2: 4.3027,
        3: 3.1824,
        4: 2.7764,
        5: 2.5706,
        6: 2.4469,
        7: 2.3646,
        8: 2.3060,
        9: 2.2622,
        10: 2.2281,
        12: 2.1788,
        15: 2.1314,
        20: 2.0860,
        25: 2.0595,
        30: 2.0423,
        40: 2.0211,
        60: 2.0003,
        120: 1.9799,
    }
    if df <= 0:
        return math.nan
    if df in table:
        return table[df]
    if df > 120:
        # Cornhill / Peizer-Pratt normal + 1/df correction
        z = 1.959963984540054
        return z + (z**3 + z) / (4.0 * df)
    keys = sorted(table)
    lo = max(k for k in keys if k < df)
    hi = min(k for k in keys if k > df)
    w = (df - lo) / (hi - lo)
    return table[lo] * (1.0 - w) + table[hi] * w


def cluster_robust_mean(
    values: Sequence[float],
    cluster_ids: Sequence[str],
) -> ClusterMeanStat:
    """Compute sample mean and CR1 cluster-robust standard error grouped by `wave_cluster_id`.

    For OLS on a constant `y_gi = mu + u_gi` across `G` clusters (`N` total trades):
        Var_CR1(y_bar) = (G / (G - 1)) * (1 / N^2) * sum_g ( sum_{i in g} (y_gi - y_bar) )^2
    """
    n = len(values)
    if n != len(cluster_ids):
        raise ValueError(
            f"values length ({n}) != cluster_ids length ({len(cluster_ids)})"
        )
    if n == 0:
        return ClusterMeanStat(
            n_obs=0,
            n_clusters=0,
            mean=0.0,
            cluster_se=math.nan,
            t_stat=math.nan,
            ci95_low=math.nan,
            ci95_high=math.nan,
        )
    if any(not str(cid).strip() for cid in cluster_ids):
        raise ValueError("every observation must carry a non-empty wave_cluster_id")

    y_bar = math.fsum(values) / n
    by_cluster: dict[str, float] = {}
    for val, cid in zip(values, cluster_ids):
        key = str(cid)
        by_cluster[key] = by_cluster.get(key, 0.0) + (float(val) - y_bar)

    g = len(by_cluster)
    if g < 2:
        return ClusterMeanStat(
            n_obs=n,
            n_clusters=g,
            mean=y_bar,
            cluster_se=math.nan,
            t_stat=math.nan,
            ci95_low=math.nan,
            ci95_high=math.nan,
        )

    meat = math.fsum(u_sum * u_sum for u_sum in by_cluster.values())
    var_hat = (g / (g - 1.0)) * meat / (n * n)
    se = math.sqrt(max(0.0, var_hat))
    t_stat = y_bar / se if se > 0 else math.copysign(math.inf, y_bar) if y_bar != 0 else 0.0
    t_crit = _t_crit_95(g - 1)
    ci_low = y_bar - t_crit * se
    ci_high = y_bar + t_crit * se
    return ClusterMeanStat(
        n_obs=n,
        n_clusters=g,
        mean=y_bar,
        cluster_se=se,
        t_stat=t_stat,
        ci95_low=ci_low,
        ci95_high=ci_high,
    )


def validate_ex_ante_grouping(
    group_columns: Sequence[str],
    *,
    feature_timestamps: Mapping[str, float] | None = None,
    entry_ts: float | None = None,
) -> None:
    """Enforce Work Item 4: experiment grouping may ONLY use features known at `t <= entry_ts`.

    Raises `ExAnteViolationError` if any column is an ex-post stopping time / outcome
    (e.g. holding duration) or carries a feature timestamp strictly after `entry_ts`.
    """
    for col in group_columns:
        norm = col.strip().lower()
        if norm in FORBIDDEN_EX_POST_COLUMNS or "hold" in norm and "dur" in norm:
            raise ExAnteViolationError(
                f"forbidden ex-post grouping column {col!r}: holding duration / realized outcomes "
                "are stopping times, not ex-ante features"
            )
        if feature_timestamps is not None and entry_ts is not None and col in feature_timestamps:
            fts = float(feature_timestamps[col])
            if fts > float(entry_ts):
                raise ExAnteViolationError(
                    f"feature {col!r} timestamp ({fts}) > entry_ts ({entry_ts}): lookahead forbidden"
                )
