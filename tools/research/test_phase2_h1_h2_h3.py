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


def test_h2_ranker_factors_and_cixin_age_classification():
    from h2_experiment import (
        compute_liquidity_tier_discount,
        compute_listing_age_cixin_factor,
        compute_oi_sync_factor,
        compute_popularity_memory_factor,
        compute_rs_vs_btc_factor,
        score_current_ranker,
    )

    cur = score_current_ranker(
        direction="UP",
        change_pct=2.0,
        market_median_change=0.4,
        btc_change=0.5,
        quote_vol_24h=50_000_000.0,
        pullback_depth_pct=1.0,
        room_pct=5.0,
        min_liquidity=10_000_000.0,
    )
    assert cur > 3.0

    # Resilient leader during BTC pullbacks scores higher than a fragile laggard
    btc_rets = [-0.20, -0.15, 0.25, -0.18, 0.30, 0.20]
    resilient_sym = [0.05, 0.02, 0.35, 0.04, 0.40, 0.30]
    fragile_sym = [-0.60, -0.45, 0.10, -0.50, 0.15, 0.10]
    assert compute_rs_vs_btc_factor("UP", resilient_sym, btc_rets) > compute_rs_vs_btc_factor(
        "UP", fragile_sym, btc_rets
    )

    # OI sync factor rewards moderate OI expansion with price direction
    assert compute_oi_sync_factor("UP", 0.65, 1.2) > compute_oi_sync_factor("UP", -0.50, 1.2)

    # Listing age (次新) factor flags contracts <= 90 days old
    is_new, sc_new = compute_listing_age_cixin_factor(30.0)
    assert is_new is True and sc_new > 0.5
    is_old, sc_old = compute_listing_age_cixin_factor(365.0)
    assert is_old is False and sc_old == 0.0

    # Popularity memory and liquidity tier discount
    assert compute_popularity_memory_factor(5, 20, 1.8) > compute_popularity_memory_factor(0, 20, 0.8)
    assert compute_liquidity_tier_discount(150_000_000.0, 500_000.0) > compute_liquidity_tier_discount(
        3_000_000.0, 20_000.0
    )


def test_h3_swing_pivot_confirmation_lag_and_box_touches():
    from h3_experiment import (
        _cluster_boundary_touches,
        _compute_causal_room_to_resistance_r,
        extract_causal_swing_pivots,
    )

    # Zero-lag must be rejected
    with pytest.raises(ValueError, match="confirm_lag_bars must be >= 1"):
        extract_causal_swing_pivots([], confirm_lag_bars=0)

    # Build 20 five-minute bars with a peak at index 5 and second peak at index 13
    bars_5m: list[tuple[int, float, float, float, float, float]] = []
    highs = [
        100.0,
        101.0,
        102.0,
        103.0,
        104.0,
        110.0,  # idx 5: swing high #1
        104.0,
        102.0,
        98.0,   # idx 8: swing low #1
        102.0,
        104.0,
        105.0,
        106.0,
        110.2,  # idx 13: swing high #2
        105.0,
        103.0,
        98.1,   # idx 16: swing low #2
        102.0,
        104.0,
        105.0,
    ]
    for i, hi in enumerate(highs):
        ts = 1_700_000_300 + i * 300
        lo = hi - 4.0
        bars_5m.append((ts, hi - 2.0, hi, lo, hi - 1.5, 100_000.0))

    # At index 7 (only 2 bars after index 5), with k=3 lag, pivot at index 5 is NOT yet confirmed!
    pivs_at_7 = extract_causal_swing_pivots(bars_5m, confirm_lag_bars=3, up_to_idx=7)
    assert all(p.pivot_idx != 5 for p in pivs_at_7)

    # At index 8 (3 bars after index 5), pivot at index 5 IS confirmed with confirm_idx == 8
    pivs_at_8 = extract_causal_swing_pivots(bars_5m, confirm_lag_bars=3, up_to_idx=8)
    high_pivs_8 = [p for p in pivs_at_8 if p.kind == "HIGH" and p.pivot_idx == 5]
    assert len(high_pivs_8) == 1
    assert high_pivs_8[0].confirm_idx == 8
    assert high_pivs_8[0].confirm_ts == bars_5m[8][0]

    # At index 19, both upper probes (idx 5 & 13) and both lower probes (idx 8 & 16) are confirmed
    pivs_all = extract_causal_swing_pivots(bars_5m, confirm_lag_bars=3, up_to_idx=19)
    up_lvl, up_touches, _ = _cluster_boundary_touches(pivs_all, "HIGH", tol_frac=0.0085)
    lo_lvl, lo_touches, _ = _cluster_boundary_touches(pivs_all, "LOW", tol_frac=0.0085)
    assert up_touches >= 2 and math.isclose(up_lvl, 110.2, rel_tol=1e-4)
    assert lo_touches >= 2 and math.isclose(lo_lvl, 94.0, rel_tol=1e-4)

    # RoomToResistanceR calculation
    htf = [(1_700_000_000, 100.0, 120.0, 90.0, 108.0)]
    room_r = _compute_causal_room_to_resistance_r(
        "UP", 111.0, 108.0, 1_700_006_000, htf, bars_5m
    )
    assert math.isclose(room_r, (120.0 - 111.0) / (111.0 - 108.0), rel_tol=1e-4)
