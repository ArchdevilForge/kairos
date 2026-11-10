"""Unit tests for Phase 2 research harness (H1 seasons/resonance, H2 leader scoring, H3 box/second-wave)."""

from __future__ import annotations

import math

import pytest

from clustering import cluster_robust_diff, student_t_one_sided_p
from h1_experiment import (
    SEASON_AUTUMN,
    SEASON_SPRING,
    SEASON_SUMMER,
    SEASON_WINTER,
    build_causal_season_timeline,
    classify_season_from_aggregates,
)


def test_cluster_robust_diff_and_one_sided_p():
    # Group A across 4 clusters with clear positive mean ~ +0.50R
    vals_a = [0.55, 0.45, 0.60, 0.50, 0.48, 0.52, 0.47, 0.53]
    clus_a = ["c1", "c1", "c2", "c2", "c3", "c3", "c4", "c4"]
    # Group B across 4 clusters with mean ~ -0.10R
    vals_b = [-0.12, -0.08, -0.15, -0.05, -0.11, -0.09, -0.14, -0.06]
    clus_b = ["c1", "c1", "c2", "c2", "c3", "c3", "c4", "c4"]

    diff = cluster_robust_diff(vals_a, clus_a, vals_b, clus_b)
    assert math.isclose(diff.stat_a.mean, 0.5125, rel_tol=1e-6)
    assert math.isclose(diff.stat_b.mean, -0.10, rel_tol=1e-6)
    assert math.isclose(diff.diff_mean, 0.6125, rel_tol=1e-6)
    assert diff.diff_cluster_se > 0
    assert diff.t_stat > 5.0
    assert diff.one_sided_p < 0.01

    # Symmetric check at t=0
    assert math.isclose(student_t_one_sided_p(0.0, 5.0), 0.5, rel_tol=1e-6)


def test_classify_season_from_aggregates_covers_all_four_regimes():
    assert classify_season_from_aggregates(0.68, 0.60, 1.15) == SEASON_SUMMER
    assert classify_season_from_aggregates(0.48, 0.58, 0.90) == SEASON_AUTUMN
    assert classify_season_from_aggregates(0.52, 0.48, 0.95) == SEASON_SPRING
    assert classify_season_from_aggregates(0.30, 0.35, 0.70) == SEASON_WINTER


def test_causal_season_timeline_has_zero_future_lookahead():
    # Construct synthetic 5m bars for 5 symbols across 40 bars
    bars_full: dict[str, list[tuple[int, float, float, float, float, float]]] = {}
    for s_idx, sym in enumerate(
        [
            "BTC/USDT:USDT",
            "ETH/USDT:USDT",
            "SOL/USDT:USDT",
            "SUI/USDT:USDT",
            "DOGE/USDT:USDT",
        ]
    ):
        seq: list[tuple[int, float, float, float, float, float]] = []
        px = 100.0 + s_idx * 10.0
        for i in range(40):
            ts = 1_700_000_300 + i * 300
            # First 25 bars flat/down, last 15 bars strong breakout
            drift = -0.001 if i < 25 else 0.005
            open_px = px
            close_px = px * (1.0 + drift)
            high_px = max(open_px, close_px) * 1.001
            low_px = min(open_px, close_px) * 0.999
            qvol = 100_000.0 if i < 25 else 350_000.0
            seq.append((ts, open_px, high_px, low_px, close_px, qvol))
            px = close_px
        bars_full[sym] = seq

    cutoff_ts = 1_700_000_300 + 24 * 300
    bars_truncated = {
        sym: [b for b in seq if b[0] <= cutoff_ts] for sym, seq in bars_full.items()
    }

    tl_full = build_causal_season_timeline(bars_full)
    tl_trunc = build_causal_season_timeline(bars_truncated)

    assert cutoff_ts in tl_full and cutoff_ts in tl_trunc
    for ts, obs_trunc in tl_trunc.items():
        obs_full = tl_full[ts]
        assert obs_trunc.season_regime == obs_full.season_regime
        assert math.isclose(obs_trunc.breadth_fast, obs_full.breadth_fast, abs_tol=1e-12)
        assert math.isclose(obs_trunc.volume_ratio, obs_full.volume_ratio, abs_tol=1e-12)
