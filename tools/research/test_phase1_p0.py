"""Unit & integration tests for Phase 1 Work Items 1-6 (tools/research)."""
from __future__ import annotations

import math
from pathlib import Path

import duckdb
import pytest

from clustering import (
    ExAnteViolationError,
    assign_signal_to_wave_cluster,
    build_wave_clusters,
    cluster_robust_mean,
    validate_ex_ante_grouping,
)
from friction import (
    FrictionParams,
    compute_funding_usd,
    dynamic_slippage_frac,
    evaluate_trade_net_r,
    limit_order_fills,
)
from p0_experiment import (
    detect_market_pulse_candidates,
    ensure_market_bars_schema,
    load_bars_and_funding_from_duckdb,
    load_journal_short_exits,
    run_p0_calibration,
)
from pit_universe import (
    PerpUniverseRecord,
    VolumeObservation,
    load_pit_jsonl,
    query_pit_candidates,
    upsert_pit_universe,
    upsert_volume_24h,
)
from policies import (
    CandidateSignal,
    MinuteBar,
    ShortExitRecord,
    protective_stop_counterfactual,
    select_plateau_winner,
    simulate_holding_policy,
)


def test_pit_universe_excludes_prelist_postdelist_and_low_volume(tmp_path: Path):
    """Work Item 1: dim_perp_universe_pit filters strictly at timestamp t + 24h volume floor."""
    con = duckdb.connect()
    upsert_pit_universe(
        con,
        [
            PerpUniverseRecord("BTC-USDT-SWAP", list_ts=1000, delist_ts=None, tick_size=0.1),
            PerpUniverseRecord("NEWCOIN/USDT:USDT", list_ts=5000, delist_ts=None, tick_size=0.001),
            PerpUniverseRecord("DEADCOINUSDT", list_ts=1000, delist_ts=4000, tick_size=0.01),
            PerpUniverseRecord("ILLIQUID/USDT:USDT", list_ts=1000, delist_ts=None, tick_size=0.01),
        ],
    )
    upsert_volume_24h(
        con,
        [
            VolumeObservation("BTC/USDT:USDT", ts=2000, quote_vol_24h=500_000_000.0),
            VolumeObservation("NEWCOIN/USDT:USDT", ts=5000, quote_vol_24h=80_000_000.0),
            VolumeObservation("DEADCOIN/USDT:USDT", ts=2000, quote_vol_24h=60_000_000.0),
            VolumeObservation("ILLIQUID/USDT:USDT", ts=2000, quote_vol_24h=5_000_000.0),
            # Future volume spike on ILLIQUID at ts=6000 must NOT leak into t=3000 query!
            VolumeObservation("ILLIQUID/USDT:USDT", ts=6000, quote_vol_24h=90_000_000.0),
        ],
    )

    # At t=3000: BTC and DEADCOIN are active & liquid; NEWCOIN not listed yet; ILLIQUID < 30M
    pool_3000 = query_pit_candidates(con, at_ts=3000, min_quote_vol_24h=30_000_000.0)
    assert pool_3000 == ["BTC/USDT:USDT", "DEADCOIN/USDT:USDT"]

    # At t=4000: DEADCOIN delisted (`delist_ts > at_ts` required)
    pool_4000 = query_pit_candidates(con, at_ts=4000, min_quote_vol_24h=30_000_000.0)
    assert pool_4000 == ["BTC/USDT:USDT"]

    # At t=5500: NEWCOIN is now listed & liquid; ILLIQUID still <30M causally
    pool_5500 = query_pit_candidates(con, at_ts=5500, min_quote_vol_24h=30_000_000.0)
    assert pool_5500 == ["BTC/USDT:USDT", "NEWCOIN/USDT:USDT"]

    # JSONL loader round-trip
    jf = tmp_path / "pit.jsonl"
    jf.write_text('{"symbol":"SOL-USDT-SWAP","list_ts":1500,"tick_size":0.01}\n')
    assert load_pit_jsonl(con, jf) == 1
    assert "SOL/USDT:USDT" in query_pit_candidates(con, at_ts=2000, min_quote_vol_24h=0)
    con.close()


def test_full_friction_cost_model_and_breakeven_landing_range():
    """Work Item 2: fees, >=1 tick maker fill, dynamic slippage, 8h funding, and BE stop R range."""
    params = FrictionParams()  # defaults: taker=5bps, maker=2bps, s0_major=1.5bps, s0_alt=5bps

    # Limit order fills ONLY when price trades through by >= 1 tick
    assert not limit_order_fills(
        is_buy=True, limit_price=100.00, bar_low=100.00, bar_high=101.00, tick_size=0.01
    )
    assert limit_order_fills(
        is_buy=True, limit_price=100.00, bar_low=99.99, bar_high=101.00, tick_size=0.01
    )
    assert not limit_order_fills(
        is_buy=False, limit_price=100.00, bar_low=99.00, bar_high=100.00, tick_size=0.01
    )
    assert limit_order_fills(
        is_buy=False, limit_price=100.00, bar_low=99.00, bar_high=100.01, tick_size=0.01
    )

    # Dynamic slippage: major s0=1.5bps vs alt s0=5.0bps + impact
    s_btc = dynamic_slippage_frac("BTC/USDT:USDT", price=60_000.0)
    s_alt = dynamic_slippage_frac("SOL/USDT:USDT", price=150.0)
    assert math.isclose(s_btc, 1.5e-4)
    assert math.isclose(s_alt, 5.0e-4)
    s_alt_impact = dynamic_slippage_frac(
        "SOL/USDT:USDT",
        price=100.0,
        atr_1m=0.30,
        notional_usd=10_000.0,
        adv_5m_usd=400_000.0,
        params=params,
    )
    assert s_alt_impact > s_alt

    # 8h UTC funding ticks (28,800s boundaries)
    f_usd = compute_funding_usd(
        direction="LONG",
        notional_usd=10_000.0,
        entry_ts=28_700,
        exit_ts=57_700,  # crosses 28,800 and 57,600 (2 ticks)
        funding_schedule={28_800: 0.0001, 57_600: 0.0002},
    )
    assert math.isclose(f_usd, 10_000.0 * (0.0001 + 0.0002))

    # A "breakeven" stop on an altcoin (1.5% stop distance) realistically lands in [-0.15R, -0.08R]
    be_trade = evaluate_trade_net_r(
        symbol="SOL/USDT:USDT",
        direction="LONG",
        entry_price=100.0,
        exit_price=100.0,  # nominal breakeven stop trigger
        stop_price=98.50,  # 1.5% structural stop
        entry_ts=1_000,
        exit_ts=4_600,
        notional_usd=10_000.0,
        close_is_maker=True,  # must be ignored because is_stop_exit=True!
        is_stop_exit=True,
        atr_1m_open=0.20,
        adv_5m_open_usd=500_000.0,
    )
    assert -0.15 <= be_trade.net_r <= -0.08
    # Verify close_is_maker was overridden to taker (5 bps = $5.00 on $10,000)
    assert math.isclose(be_trade.fee_close_usd, 5.0)


def test_wave_cluster_merging_and_cluster_robust_se():
    """Work Item 3: merge MarketPulse wakes <24h apart and compute CR1 cluster-robust SE."""
    wakes = [
        ("ev1", 100_000.0),
        ("ev2", 100_000.0 + 6 * 3600),   # +6h (<24h) -> same cluster wave_0001
        ("ev3", 100_000.0 + 29 * 3600),  # +23h after ev2 (<24h) -> STILL wave_0001
        ("ev4", 100_000.0 + 54 * 3600),  # +25h after ev3 (>=24h) -> new cluster wave_0002
    ]
    clusters, ev_map = build_wave_clusters(wakes)
    assert len(clusters) == 2
    assert ev_map["ev1"] == "wave_0001"
    assert ev_map["ev2"] == "wave_0001"
    assert ev_map["ev3"] == "wave_0001"
    assert ev_map["ev4"] == "wave_0002"
    assert assign_signal_to_wave_cluster(100_000.0 + 7 * 3600, clusters) == "wave_0001"
    assert assign_signal_to_wave_cluster(100_000.0 + 55 * 3600, clusters) == "wave_0002"

    # Correlated alt trades within the same wave cluster MUST inflate CR1 SE above naive i.i.d. SE
    # Suppose 3 clusters of 5 identical trades each: [+1, +1, +1, +1, +1], [-1, ...], [0, ...]
    vals = [1.0] * 5 + [-1.0] * 5 + [0.0] * 5
    cids = ["wave_0001"] * 5 + ["wave_0002"] * 5 + ["wave_0003"] * 5
    stat = cluster_robust_mean(vals, cids)
    naive_iid_se = math.sqrt(sum(v * v for v in vals) / (15 * 14))
    assert stat.n_obs == 15
    assert stat.n_clusters == 3
    assert math.isclose(stat.mean, 0.0)
    assert stat.cluster_se > 2.0 * naive_iid_se


def test_ex_ante_feature_guard_and_intrabar_stop_first():
    """Work Item 4: forbid ex-post holding duration grouping and resolve intra-bar ties stop-first."""
    with pytest.raises(ExAnteViolationError):
        validate_ex_ante_grouping(["symbol", "holding_duration"])
    with pytest.raises(ExAnteViolationError):
        validate_ex_ante_grouping(["hold_minutes"])
    with pytest.raises(ExAnteViolationError):
        validate_ex_ante_grouping(
            ["future_vwap"],
            feature_timestamps={"future_vwap": 2_060.0},
            entry_ts=2_000.0,
        )
    # Valid ex-ante features pass
    validate_ex_ante_grouping(
        ["symbol", "breadth_5m"],
        feature_timestamps={"breadth_5m": 2_000.0},
        entry_ts=2_000.0,
    )

    # Intra-bar conflict: single 1m bar touches BOTH -1R stop (99.0) AND +1.5R target (101.5)
    sig = CandidateSignal(
        signal_id="sig_conflict",
        wave_cluster_id="wave_0001",
        symbol="SOL/USDT:USDT",
        direction="UP",
        entry_ts=1_000,
        entry_price=100.0,
        stop_price=99.0,
    )
    bars = [
        MinuteBar(ts=1_000, open=100.0, high=102.0, low=98.8, close=101.8, quote_vol=100_000.0)
    ]
    res = simulate_holding_policy(sig, bars, policy_kind="fixed_bracket", target_r=1.5)
    assert res.exit_kind == "initial_stop"
    assert res.net_r < -0.95


def test_next_minute_open_breakeven_and_plateau_selection():
    """Work Item 5: next-minute-open breakeven timing, 1h structure trail, and plateau selector."""
    sig = CandidateSignal(
        signal_id="sig_wave",
        wave_cluster_id="wave_0001",
        symbol="ETH/USDT:USDT",
        direction="UP",
        entry_ts=0,
        entry_price=100.0,
        stop_price=99.0,  # 1R = $1.00
    )
    # Minute 0 closes at 101.3 (+1.3R >= k_be=1.2) -> arms BE for Minute 1 open (ts=60).
    # Minute 1 drops back to 100.05 (hits fee-inclusive BE at 100.10, NOT initial stop 99.0).
    bars = [
        MinuteBar(ts=0, open=100.0, high=101.4, low=99.8, close=101.3, quote_vol=500_000.0),
        MinuteBar(ts=60, open=101.2, high=101.2, low=100.0, close=100.05, quote_vol=500_000.0),
        MinuteBar(ts=120, open=100.05, high=100.1, low=98.5, close=98.6, quote_vol=500_000.0),
    ]
    res_be = simulate_holding_policy(sig, bars, policy_kind="wave", k_be=1.2)
    res_nobe = simulate_holding_policy(sig, bars, policy_kind="wave", k_be=math.inf)
    assert res_be.exit_kind == "breakeven_stop"
    assert res_be.exit_ts == 120  # exited during minute 1 (ts=60..120), saving minute 2's -1R stop!
    assert res_nobe.exit_kind == "initial_stop"
    assert res_be.net_r > res_nobe.net_r + 0.75

    # Plateau selection: reject isolated needle spike at k=0.8 if neighbors collapse
    from clustering import ClusterMeanStat

    spike_grid = {
        0.8: ClusterMeanStat(20, 5, mean=0.45, cluster_se=0.05, t_stat=9.0, ci95_low=0.3, ci95_high=0.6),
        1.2: ClusterMeanStat(20, 5, mean=0.05, cluster_se=0.05, t_stat=1.0, ci95_low=-0.1, ci95_high=0.2),
        1.5: ClusterMeanStat(20, 5, mean=0.32, cluster_se=0.05, t_stat=6.4, ci95_low=0.18, ci95_high=0.46),
        2.0: ClusterMeanStat(20, 5, mean=0.30, cluster_se=0.05, t_stat=6.0, ci95_low=0.16, ci95_high=0.44),
        math.inf: ClusterMeanStat(20, 5, mean=0.26, cluster_se=0.05, t_stat=5.2, ci95_low=0.12, ci95_high=0.40),
    }
    sel = select_plateau_winner(spike_grid, max_neighbor_drop_r=0.12)
    assert sel.is_plateau
    assert sel.winner_k == 1.5  # sits on flat [1.5, 2.0, inf] plateau instead of isolated 0.8 spike!


def test_protective_stop_counterfactual_saved_r_and_journal_loader(tmp_path: Path):
    """Work Item 6: saved_r = NetR_actual - NetR_hold_to_5m on <5m preset-stop / fakeout exits."""
    bars = [
        MinuteBar(ts=1_000, open=100.0, high=100.1, low=98.9, close=99.0, quote_vol=200_000.0),
        MinuteBar(ts=1_060, open=99.0, high=99.1, low=98.2, close=98.3, quote_vol=200_000.0),
        MinuteBar(ts=1_120, open=98.3, high=98.4, low=97.6, close=97.8, quote_vol=200_000.0),
        MinuteBar(ts=1_180, open=97.8, high=97.9, low=97.2, close=97.4, quote_vol=200_000.0),
        MinuteBar(ts=1_240, open=97.4, high=97.5, low=96.8, close=97.0, quote_vol=200_000.0),
    ]
    rec1 = ShortExitRecord(
        ticket_id="t1",
        wave_cluster_id="wave_0001",
        symbol="SOL/USDT:USDT",
        direction="UP",
        entry_ts=1_000,
        exit_ts=1_060,  # 60s (<5m) preset stop hit at 99.0
        entry_price=100.0,
        stop_price=99.0,
        actual_exit_price=99.0,
        hit_preset_stop=True,
        fakeout_confirmed=False,
        bars_1m=bars,
    )
    rec2 = ShortExitRecord(
        ticket_id="t2",
        wave_cluster_id="wave_0002",
        symbol="SOL/USDT:USDT",
        direction="UP",
        entry_ts=1_000,
        exit_ts=1_120,  # 120s (<5m) fakeout abort at 99.2 before 5m collapse to 97.0
        entry_price=100.0,
        stop_price=99.0,
        actual_exit_price=99.2,
        hit_preset_stop=False,
        fakeout_confirmed=True,
        bars_1m=bars,
    )
    rep = protective_stop_counterfactual([rec1, rec2])
    assert len(rep.rows) == 2
    assert rep.saved_r_stat.mean > 1.5  # Stopping at -1R saved ~2R vs holding to 97.0 (-3R)

    # Also verify historical Journal JSONL loader
    jfile = tmp_path / "journal.jsonl"
    jfile.write_text(
        '{"id":"t1","kind":"ticket","payload":{"id":"t1","symbol":"SOL/USDT:USDT","direction":"UP","stop_price":99.0}}\n'
        '{"id":"d1","kind":"decision","payload":{"ticket_id":"t1","action":"ACCEPT","entry_price":100.0,"exit_price":99.0,'
        '"decided_at":"1970-01-01T00:16:40Z","closed_at":"1970-01-01T00:17:40Z","exit_reason":"PROTECTIVE_STOP"}}\n'
    )
    loaded = load_journal_short_exits(jfile, {"SOL/USDT:USDT": bars})
    assert len(loaded) == 1
    assert loaded[0].ticket_id == "t1"
    assert loaded[0].hit_preset_stop is True


def test_p0_stop_condition_triggers_when_all_policies_below_minus_015r():
    """Stop condition check: triggers if every holding policy has mean NetR < -0.15R."""
    con = duckdb.connect()
    ensure_market_bars_schema(con)
    # Build two losing signals in two separate wave clusters
    bars = [
        MinuteBar(ts=t * 60, open=100.0 - 0.05 * t, high=100.0 - 0.05 * t + 0.02, low=100.0 - 0.05 * t - 0.3, close=100.0 - 0.05 * (t + 1), quote_vol=300_000.0)
        for t in range(60)
    ]
    sigs = [
        CandidateSignal("s1", "wave_0001", "SOL/USDT:USDT", "UP", 0, 100.0, 99.0),
        CandidateSignal("s2", "wave_0002", "SOL/USDT:USDT", "UP", 60, 99.95, 98.95),
    ]
    rep = run_p0_calibration(sigs, {"SOL/USDT:USDT": bars}, max_hold_minutes=45)
    assert rep.stop_condition_triggered is True
    con.close()
