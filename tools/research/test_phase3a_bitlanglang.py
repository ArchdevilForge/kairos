"""Unit tests for Phase 3a BitLanglang operationalization and honest validation harness."""

from __future__ import annotations

import math

import duckdb
import pytest

from h1_experiment import AnnotatedTicketSignal
from h3_experiment import _cluster_boundary_touches, extract_causal_swing_pivots
from p0_experiment import ensure_market_bars_schema
from phase3a_experiment import (
    EXPECTED_PHASE2B_1M_BARS,
    DatasetAnchorAudit,
    _btc_env_aligned,
    _has_excessive_prior_waves,
    detect_s1_point1_candidates,
    evaluate_f_box_filter,
    evaluate_phase3a_thermometer,
    evaluate_s3_point2_constraints,
    run_phase3a_all,
    score_langlang_leader_states,
    verify_phase3a_dataset_anchor,
)
from policies import CandidateSignal, MinuteBar


def _make_minute_bars_aligned_5m(
    start_ts_aligned_300: int,
    specs_5m: list[tuple[float, float, float, float, float]],
) -> list[MinuteBar]:
    """Create 5x 1m bars per 5m spec so `_build_5m_bars_from_1m` closes at `b_open + 300`."""
    out: list[MinuteBar] = []
    for b5_i, (o, h, l, c, qv_per_1m) in enumerate(specs_5m):
        b_open = start_ts_aligned_300 + b5_i * 300
        for m in range(5):
            out.append(
                MinuteBar(
                    ts=b_open + m * 60,
                    open=o if m == 0 else (o + c) * 0.5,
                    high=h if m == 2 else max(o, c),
                    low=l if m == 1 else min(o, c),
                    close=c if m == 4 else (o + c) * 0.5,
                    quote_vol=qv_per_1m,
                )
            )
    return out


def test_step0_dataset_anchor_tolerance_and_guard():
    con = duckdb.connect(":memory:")
    ensure_market_bars_schema(con)
    audit = verify_phase3a_dataset_anchor(con)
    assert audit.bars_1m == 0
    assert audit.expected_bars_1m == EXPECTED_PHASE2B_1M_BARS
    assert not audit.within_2pct_anchor
    with pytest.raises(RuntimeError, match="Step 0 dataset anchor mismatch"):
        run_phase3a_all(con, enforce_anchor=True)


def test_delivery_audit_btc_env_alignment_blocks_deep_bear():
    base_ts = 1_704_067_200
    # Bearish BTC: 1d close well below 0.98 * SMA50 and 4h close below SMA18
    btc_1d_bear = [
        (base_ts + d * 86_400, 100.0 - d, 101.0 - d, 98.0 - d, 99.0 - d, 1e9)
        for d in range(30)
    ]
    btc_4h_bear = [
        (base_ts + 29 * 86_400 + k * 14_400, 72.0 - k * 0.5, 72.2 - k * 0.5, 71.0 - k * 0.5, 71.2 - k * 0.5, 2e8)
        for k in range(20)
    ]
    ok_bear, _ = _btc_env_aligned(btc_1d_bear, btc_4h_bear, btc_4h_bear[-1][0])
    assert ok_bear is False

    # Bullish BTC: 1d close above SMA50
    btc_1d_bull = [
        (base_ts + d * 86_400, 70.0 + d, 72.0 + d, 69.5 + d, 71.5 + d, 1e9)
        for d in range(30)
    ]
    ok_bull, _ = _btc_env_aligned(btc_1d_bull, btc_4h_bear, btc_1d_bull[-1][0])
    assert ok_bull is True


def test_htf_midpoint_boundary_probes_cluster_accurately():
    base_ts = 1_704_067_200
    bars_4h: list[tuple[int, float, float, float, float, float]] = []
    pattern = [
        (103.0, 105.0, 102.0, 104.0),
        (104.0, 110.0, 103.5, 106.0),  # upper pivot 1 (110.0)
        (106.0, 106.5, 103.0, 104.0),
        (104.0, 104.5, 100.0, 102.0),  # lower pivot 1 (100.0)
        (102.0, 106.0, 101.5, 105.5),
        (105.5, 109.8, 104.5, 107.0),  # upper pivot 2 (109.8, within 1.5% of 110)
        (107.0, 107.2, 103.0, 103.5),
        (103.5, 104.0, 100.2, 102.5),  # lower pivot 2 (100.2, within 1.5% of 100)
        (102.5, 106.0, 102.0, 105.0),
    ]
    for i, (o, h, l, c) in enumerate(pattern):
        bars_4h.append((base_ts + (i + 1) * 14_400, o, h, l, c, 5_000_000.0))

    pivs = extract_causal_swing_pivots(bars_4h, confirm_lag_bars=1, up_to_idx=len(bars_4h) - 1)
    b_hi, up_probes, _ = _cluster_boundary_touches(pivs, "HIGH", tol_frac=0.015)
    b_lo, lo_probes, _ = _cluster_boundary_touches(pivs, "LOW", tol_frac=0.015)
    assert up_probes == 2
    assert lo_probes == 2
    assert math.isclose(b_hi, 109.9, rel_tol=0.01)
    assert math.isclose(b_lo, 100.1, rel_tol=0.01)


def test_s1_point1_breakout_detector_has_zero_future_lookahead():
    sym = "SOL/USDT:USDT"
    base_4h_ts = 1_704_067_200
    # 22 closed 4h bars forming a 100..108 box with 2 upper probes and 1 lower probe
    # (testing EP02 line 8: >=2 upper OR >=2 lower probes suffices for midpoint)
    box_seq = [
        (103.0, 105.0, 102.0, 104.0),
        (104.0, 108.0, 103.0, 106.0),  # upper 1 (108.0)
        (106.0, 106.2, 102.0, 103.0),
        (103.0, 104.0, 100.0, 102.0),  # lower 1 (100.0)
        (102.0, 105.5, 101.5, 105.0),
        (105.0, 107.9, 104.0, 106.5),  # upper 2 (107.9)
        (106.5, 106.8, 103.5, 104.5),
        (104.5, 105.8, 103.8, 105.2),
        (105.2, 106.5, 104.2, 105.8),
        (105.8, 107.2, 104.5, 106.0),
        (106.0, 107.4, 104.8, 106.5),
        (106.5, 107.5, 105.0, 106.8),
        (106.8, 107.6, 105.4, 107.0),
        (107.0, 107.7, 105.8, 107.2),
        (107.2, 107.75, 106.4, 107.3),
        (107.3, 107.80, 106.8, 107.4),
        (107.4, 107.82, 107.1, 107.5),
        (107.5, 107.84, 107.3, 107.6),
        (107.6, 107.85, 107.45, 107.68),
        (107.68, 107.86, 107.50, 107.72),
        (107.72, 107.88, 107.55, 107.76),
        (107.76, 107.90, 107.60, 107.80),
    ]
    bars_4h = [
        (base_4h_ts + (i + 1) * 14_400, o, h, l, c, 8_000_000.0)
        for i, (o, h, l, c) in enumerate(box_seq)
    ]
    last_4h_close = bars_4h[-1][0]
    bars_1d = [
        (base_4h_ts - 86_400 * (10 - d), 102.0, 107.5, 99.5, 104.0, 50_000_000.0)
        for d in range(10)
    ]

    prefix_5m: list[tuple[float, float, float, float, float]] = []
    for b5_i in range(32):
        if b5_i < 25:
            prefix_5m.append((107.2, 107.85, 106.6, 107.5, 100_000.0))
        elif b5_i < 31:
            prefix_5m.append((107.55, 107.80, 107.25, 107.70, 100_000.0))
        else:
            prefix_5m.append((107.75, 108.45, 107.70, 108.35, 250_000.0))

    future_bull = [
        (108.5 + k * 0.4, 109.0 + k * 0.4, 108.4 + k * 0.4, 108.8 + k * 0.4, 200_000.0)
        for k in range(33)
    ]
    future_crash = [
        (108.0 - k * 1.2, 108.1 - k * 1.2, 106.5 - k * 1.2, 106.8 - k * 1.2, 200_000.0)
        for k in range(33)
    ]

    bars_1m_bull = _make_minute_bars_aligned_5m(last_4h_close, prefix_5m + future_bull)
    bars_1m_crash = _make_minute_bars_aligned_5m(last_4h_close, prefix_5m + future_crash)
    bars_4h_future_shock = bars_4h + [(last_4h_close + 14_400, 108.0, 150.0, 40.0, 50.0, 99e7)]

    vol_map = {sym: [(last_4h_close, 80_000_000.0)]}
    pit = {sym: (base_4h_ts - 86_400 * 120, None)}
    ticks = {sym: 0.01}

    res_bull = detect_s1_point1_candidates(
        {sym: bars_1m_bull}, {sym: bars_4h}, {sym: bars_1d}, {}, ticks, pit, vol_map
    )
    res_crash = detect_s1_point1_candidates(
        {sym: bars_1m_crash},
        {sym: bars_4h_future_shock},
        {sym: bars_1d},
        {},
        ticks,
        pit,
        vol_map,
    )
    assert len(res_bull) >= 1 and len(res_crash) >= 1
    c_bull = res_bull[0]
    c_crash = res_crash[0]
    assert c_bull.signal.entry_ts == c_crash.signal.entry_ts
    assert c_bull.signal.entry_price == c_crash.signal.entry_price
    assert c_bull.signal.stop_price == c_crash.signal.stop_price
    assert c_bull.htf_box_width_pct == c_crash.htf_box_width_pct
    assert c_bull.room_r == c_crash.room_r
    assert c_bull.atr_ratio == c_crash.atr_ratio
    assert c_bull.net_r_wave > c_crash.net_r_wave


def test_s2_wave_diminishing_cap_rejects_prior_completed_major_wave():
    base_ts = 1_704_067_200
    closes = (
        [50.0, 50.5, 51.0, 54.0, 56.0, 58.0, 55.5, 54.5, 54.0, 54.5, 55.0, 55.5, 56.0]
        + [57.0, 62.0, 68.0, 63.0, 64.0]
    )
    bars_4h = [
        (base_ts + (idx + 1) * 14_400, c * 0.99, c * 1.01, c * 0.985, c, 5e6)
        for idx, c in enumerate(closes)
    ]
    assert _has_excessive_prior_waves(bars_4h, idx_l0_global=12, h1_price=68.0) is True


def test_s3_constraints_and_f_box_filter_including_ep20_short_trap():
    sym = "ETH/USDT:USDT"
    base_ts = 1_704_067_200
    bars_4h_chop: list[tuple[int, float, float, float, float, float]] = []
    for i in range(36):
        mid = 2050.0 + (20.0 if i % 2 == 0 else -20.0)
        bars_4h_chop.append(
            (
                base_ts + (i + 1) * 14_400,
                mid - 10,
                2100.0 if i % 5 == 0 else mid + 15,
                2000.0 if i % 5 == 2 else mid - 15,
                mid,
                2e7,
            )
        )
    last_4h = bars_4h_chop[-1][0]
    # 1d above SMA20 (non-downtrend, EP20 PEPE box-top short trap context)
    bars_1d = [
        (base_ts + d * 86_400, 2010.0, 2110.0, 1995.0, 2060.0 if d == 5 else 2030.0, 1e8)
        for d in range(6)
    ]

    specs_5m = [(2049.0, 2053.0, 2047.0, 2050.0, 200_000.0) for _ in range(35)]
    bars_1m = _make_minute_bars_aligned_5m(last_4h, specs_5m)
    entry_ts = last_4h + 34 * 300

    sig_long = CandidateSignal(
        signal_id="tkt_eth_chop_long",
        wave_cluster_id="wave_0002",
        symbol=sym,
        direction="UP",
        entry_ts=entry_ts,
        entry_price=2050.0,
        stop_price=2030.0,
        tick_size=0.01,
        atr_1m_entry=4.0,
        adv_5m_entry_usd=1_000_000.0,
    )
    sig_short = CandidateSignal(
        signal_id="tkt_eth_box_top_short",
        wave_cluster_id="wave_0002",
        symbol=sym,
        direction="DOWN",
        entry_ts=entry_ts,
        entry_price=2092.0,  # near box top (pos_box = 0.92 > 0.75, so NOT mid_chop!)
        stop_price=2135.0,
        tick_size=0.01,
        atr_1m_entry=4.0,
        adv_5m_entry_usd=1_000_000.0,
    )
    tkt_long = AnnotatedTicketSignal(
        signal=sig_long,
        feature_ts=entry_ts,
        season_regime="WINTER",
        season_regime_oos="WINTER",
        breadth_fast=0.35,
        breadth_slow=0.40,
        volume_ratio=0.80,
        btc_sync_mode="NONE",
        btc_impulse_lag_sec=None,
        btc_near_high=False,
        crowding_pct=0.50,
        net_r_wave=-0.85,
        net_r_bracket=-1.0,
        net_r_30m=-0.40,
        stop_hit=True,
    )
    tkt_short = AnnotatedTicketSignal(
        signal=sig_short,
        feature_ts=entry_ts,
        season_regime="WINTER",
        season_regime_oos="WINTER",
        breadth_fast=0.35,
        breadth_slow=0.40,
        volume_ratio=0.80,
        btc_sync_mode="NONE",
        btc_impulse_lag_sec=None,
        btc_near_high=False,
        crowding_pct=0.50,
        net_r_wave=-0.95,
        net_r_bracket=-1.0,
        net_r_30m=-0.50,
        stop_hit=True,
    )

    f_rep = evaluate_f_box_filter(
        [tkt_long, tkt_short], {sym: bars_1m}, {sym: bars_4h_chop}, {sym: bars_1d}
    )
    assert len(f_rep.annotations) == 2
    assert f_rep.annotations[0].is_box_mid_chop is True
    assert f_rep.annotations[1].is_box_mid_chop is False
    assert f_rep.annotations[1].is_box_counter_short is True
    assert f_rep.annotations[1].is_filtered is True

    s3_rep = evaluate_s3_point2_constraints(
        [tkt_long], {sym: bars_1m}, {sym: bars_4h_chop}, {sym: bars_1d}
    )
    assert len(s3_rep.annotations) == 1
    assert s3_rep.annotations[0].c3_htf_main_uptrend is False
    assert s3_rep.annotations[0].retained is False


def test_l_leader_state_scorer_rewards_early_sync_fluency_and_penalizes_laggard():
    wake_ts = 1_705_000_000
    t_launch = wake_ts - 14_400 * 4  # Major launched 16h ago
    btc_4h = [
        (t_launch - 14_400 * (6 - k), 40_000.0, 40_200.0, 39_400.0, 39_600.0, 1e8)
        for k in range(6)
    ] + [
        (t_launch, 39_600.0, 40_500.0, 39_550.0, 40_400.0, 2e8),
        (t_launch + 14_400, 40_400.0, 41_200.0, 40_300.0, 41_000.0, 2e8),
        (t_launch + 28_800, 41_000.0, 41_100.0, 40_600.0, 40_700.0, 1.5e8),
        (t_launch + 43_200, 40_700.0, 41_300.0, 40_650.0, 41_200.0, 2e8),
        (t_launch + 57_600, 41_200.0, 41_600.0, 41_150.0, 41_500.0, 2e8),
    ]

    leader_1d = [
        (
            wake_ts - 86_400 * (25 - d),
            10.0 + d * 0.2,
            10.85 + d * 0.2 if d == 20 else 10.3 + d * 0.2,
            9.95 + d * 0.2,
            10.80 + d * 0.2 if d == 20 else 10.25 + d * 0.2,
            5e7,
        )
        for d in range(25)
    ]
    # Leader started slightly early at `t_launch - 4h` (EP06/EP07 lead sync) with fluent 4h bars
    leader_4h = [
        (t_launch - 14_400 * (6 - k), 14.0, 14.15, 14.0, 14.1, 1e7) for k in range(5)
    ] + [
        (t_launch - 14_400, 14.1, 14.55, 14.08, 14.5, 1.8e7),  # -4h early start!
        (t_launch, 14.5, 15.0, 14.48, 14.95, 2e7),
        (t_launch + 14_400, 14.95, 15.4, 14.90, 15.35, 2.5e7),
        (t_launch + 28_800, 15.35, 15.5, 15.30, 15.42, 1.5e7),
        (t_launch + 43_200, 15.42, 15.8, 15.40, 15.75, 2e7),
        (t_launch + 57_600, 15.75, 16.05, 15.72, 16.0, 2e7),
    ]

    laggard_1d = [
        (
            wake_ts - 86_400 * (25 - d),
            20.0 - d * 0.1,
            17.80 if d == 24 else 20.2 - d * 0.1,
            19.5 - d * 0.1,
            19.8 - d * 0.1,
            2e7,
        )
        for d in range(25)
    ]
    laggard_4h = [
        (t_launch - 14_400 * (6 - k), 17.5, 17.6, 17.1, 17.2, 5e6) for k in range(6)
    ] + [
        (t_launch, 17.2, 17.25, 17.15, 17.21, 5e6),
        (t_launch + 14_400, 17.21, 17.25, 17.18, 17.22, 5e6),
        (t_launch + 28_800, 17.22, 17.24, 16.80, 16.90, 5e6),
        (t_launch + 43_200, 16.90, 17.15, 16.88, 17.10, 5e6),
        (t_launch + 57_600, 17.10, 17.50, 17.08, 17.45, 8e6),
    ]

    s_lead = score_langlang_leader_states(
        sym="PEOPLE/USDT:USDT",
        wake_ts=wake_ts,
        age_days=60.0,
        sym_1d=leader_1d,
        sym_4h=leader_4h,
        btc_4h=btc_4h,
        universe_1d_body_median=0.45,
        universe_launch_median_ret=2.0,
        t_btc_launch=t_launch,
        btc_ret_48h_pct=4.5,
        r_btc_1h_pct=1.2,
        r_sym_1h_pct=2.8,
    )
    s_lag = score_langlang_leader_states(
        sym="NEW/USDT:USDT",
        wake_ts=wake_ts,
        age_days=3.0,
        sym_1d=laggard_1d,
        sym_4h=laggard_4h,
        btc_4h=btc_4h,
        universe_1d_body_median=0.45,
        universe_launch_median_ret=2.0,
        t_btc_launch=t_launch,
        btc_ret_48h_pct=4.5,
        r_btc_1h_pct=1.2,
        r_sym_1h_pct=2.5,
    )
    assert sum(s_lead) >= 6
    assert sum(s_lag) <= 0
    assert s_lead[0] == 2
    assert s_lead[1] == 2
    assert s_lag[1] == -2


def test_thermometer_audit_is_untestable_without_approximation():
    anchor = DatasetAnchorAudit(
        bars_1m=758_013,
        bars_htf=53_122,
        volume_24h_obs=197_776,
        funding_ticks=6_995,
        oi_5m_ticks=30_157,
        pit_universe_total=528,
        distinct_1m_symbols=27,
        expected_bars_1m=758_013,
        rel_diff_pct=0.0,
        within_2pct_anchor=True,
    )
    rep = evaluate_phase3a_thermometer(anchor)
    assert rep.verdict == "UNTESTABLE"
    assert len(rep.missing_requirements) == 3
    assert len(rep.unlock_data_spec) == 3
