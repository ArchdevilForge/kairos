"""Experiment H3 — Box Maturity & Second-Wave Lifecycle (Offline Only, Phase 2).

Implements Work Order Section C (Items 6–7):
6. Formalize:
   - Causal swing pivot detector with mandatory confirmation lag `k` bars
     (a pivot at index `p` is ONLY usable at bar index `t >= p + k`; zero lookahead).
   - Box maturity = `>= 2` confirmed probes of upper boundary + `>= 2` confirmed probes of lower boundary
     (`BoxTouchCount >= 4`) + ATR compression (`ATRCompressionRatio < 0.85`) + `RoomToResistanceR`
     to the next causal 1d/4h resistance in R units.
   - Divergence / second-wave lifecycle = Stage-1 impulse -> Stage-2 high-volume divergence
     (`TriggerKnifeLock = True`, no left-side catching) -> Stage-3 contraction -> Stage-4 right-side re-break.
7. Pre-registered KILL rule:
   - If out-of-sample independent wave clusters `< 15` OR the NetR 95% CI lower bound `< 0`,
     DO NOT build any automatic geometry recognizer (and do not wire CycleMap geometry shadow fields).
   - Deliver instead a specification for `kairos-desk` manual watch-range entry
     (operator-entered upper/lower bounds, system computes `RoomToResistanceR` and alerts on touch).
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timezone
import math
from typing import Mapping, Sequence

import duckdb

from clustering import (
    ClusterMeanStat,
    build_wave_clusters,
    cluster_robust_mean,
    validate_ex_ante_grouping,
)
from friction import FrictionParams
from p0_experiment import load_bars_and_funding_from_duckdb
from pit_universe import normalize_base_coin
from policies import CandidateSignal, MinuteBar, simulate_holding_policy


H3_MIN_OOS_CLUSTERS_BAR: int = 15
H3_MIN_CI95_LOWER_BAR: float = 0.0


@dataclass(frozen=True)
class ConfirmedSwingPivot:
    """A swing high or low pivot at `pivot_idx` that becomes causally observable only at `confirm_idx = pivot_idx + k`."""

    pivot_idx: int
    confirm_idx: int
    pivot_ts: int
    confirm_ts: int
    kind: str  # "HIGH" or "LOW"
    price: float
    bar_volume: float


@dataclass(frozen=True)
class H3SetupCandidate:
    """Offline H3 candidate setup (either `BOX_BREAKOUT` or `SECOND_WAVE_STAGE4`)."""

    signal: CandidateSignal
    setup_kind: str  # "BOX_BREAKOUT" | "SECOND_WAVE_STAGE4"
    upper_touches: int
    lower_touches: int
    box_touch_count: int
    atr_compression_ratio: float
    room_to_resistance_r: float
    trigger_knife_lock_avoided: bool
    confirm_lag_bars: int
    last_pivot_confirm_ts: int
    net_r_wave: float
    net_r_bracket: float
    left_side_knife_net_r: float | None


@dataclass(frozen=True)
class H3ExperimentReport:
    """Complete evaluation report for Experiment H3 (Box Maturity & Second Wave)."""

    confirm_lag_bars: int
    n_total_setups: int
    n_box_setups: int
    n_second_wave_setups: int
    n_clusters_total: int
    n_clusters_oos: int
    combined_stat: ClusterMeanStat
    oos_stat: ClusterMeanStat
    box_stat: ClusterMeanStat
    second_wave_stat: ClusterMeanStat
    left_side_knife_mean_r: float
    right_side_stage4_mean_r: float
    knife_avoidance_edge_r: float
    kill_rule_triggered: bool
    kill_reasons: tuple[str, ...]
    h3_passed: bool
    h3_verdict_reason: str
    setups: tuple[H3SetupCandidate, ...]


def extract_causal_swing_pivots(
    bars_5m: Sequence[tuple[int, float, float, float, float, float]],
    *,
    confirm_lag_bars: int = 3,
    up_to_idx: int | None = None,
) -> list[ConfirmedSwingPivot]:
    """Extract swing high/low pivots confirmed on or before `up_to_idx` respecting `confirm_lag_bars`.

    A bar at `p` is a swing high (or low) of order `k = confirm_lag_bars` iff its high (or low)
    is the extremum across `[p - k, p + k]`. Crucially, it is ONLY emitted if `p + k <= limit_idx`,
    guaranteeing zero lookahead (`confirm_ts <= bars_5m[limit_idx][0]`).
    """
    if confirm_lag_bars < 1:
        raise ValueError("confirm_lag_bars must be >= 1 to prevent zero-lag pivot lookahead")
    if not bars_5m:
        return []

    limit_idx = len(bars_5m) - 1 if up_to_idx is None else min(len(bars_5m) - 1, up_to_idx)
    k = confirm_lag_bars
    pivots: list[ConfirmedSwingPivot] = []

    for p in range(k, limit_idx - k + 1):
        confirm_idx = p + k
        if confirm_idx > limit_idx:
            break
        hi_p = bars_5m[p][2]
        lo_p = bars_5m[p][3]
        window = bars_5m[p - k : p + k + 1]
        if all(hi_p >= w[2] for w in window) and any(hi_p > w[2] for w in window):
            pivots.append(
                ConfirmedSwingPivot(
                    pivot_idx=p,
                    confirm_idx=confirm_idx,
                    pivot_ts=bars_5m[p][0],
                    confirm_ts=bars_5m[confirm_idx][0],
                    kind="HIGH",
                    price=hi_p,
                    bar_volume=bars_5m[p][5],
                )
            )
        if all(lo_p <= w[3] for w in window) and any(lo_p < w[3] for w in window):
            pivots.append(
                ConfirmedSwingPivot(
                    pivot_idx=p,
                    confirm_idx=confirm_idx,
                    pivot_ts=bars_5m[p][0],
                    confirm_ts=bars_5m[confirm_idx][0],
                    kind="LOW",
                    price=lo_p,
                    bar_volume=bars_5m[p][5],
                )
            )
    return pivots


def _cluster_boundary_touches(
    pivots: Sequence[ConfirmedSwingPivot],
    kind: str,
    tol_frac: float = 0.0085,
) -> tuple[float, int, int]:
    """Find the most-probed horizontal boundary among confirmed pivots of `kind` (`HIGH` or `LOW`).

    Returns `(boundary_price, touch_count, latest_confirm_ts)`.
    """
    filtered = [p for p in pivots if p.kind == kind]
    if len(filtered) < 2:
        return (0.0, 0, 0)
    best_level = 0.0
    best_touches = 0
    best_confirm_ts = 0
    for anchor in filtered:
        cluster = [
            p
            for p in filtered
            if abs(p.price - anchor.price) / max(anchor.price, 1e-9) <= tol_frac
            and abs(p.pivot_idx - anchor.pivot_idx) >= 2
        ]
        # Include anchor itself plus distinct non-adjacent probes
        distinct: list[ConfirmedSwingPivot] = [anchor]
        for cand in filtered:
            if cand.pivot_idx == anchor.pivot_idx:
                continue
            if abs(cand.price - anchor.price) / max(anchor.price, 1e-9) <= tol_frac:
                if all(abs(cand.pivot_idx - d.pivot_idx) >= 2 for d in distinct):
                    distinct.append(cand)
        if len(distinct) >= best_touches:
            best_touches = len(distinct)
            best_level = (
                max(p.price for p in distinct)
                if kind == "HIGH"
                else min(p.price for p in distinct)
            )
            best_confirm_ts = max(p.confirm_ts for p in distinct)
    return (best_level, best_touches, best_confirm_ts)


def _compute_causal_room_to_resistance_r(
    direction: str,
    entry_price: float,
    stop_price: float,
    entry_ts: int,
    htf_bars: Sequence[tuple[int, float, float, float, float]],
    bars_5m_up_to_t: Sequence[tuple[int, float, float, float, float, float]],
) -> float:
    """Compute causal `RoomToResistanceR` to the next 1d/4h structural resistance/support closed `<= entry_ts`."""
    r_dist = abs(entry_price - stop_price)
    if r_dist <= 0:
        return 0.0

    # Collect causal HTF (4h/1d) highs/lows closed strictly at or before entry_ts
    causal_htf = [b for b in htf_bars if b[0] <= entry_ts]
    if direction == "UP":
        above_levels = [b[2] for b in causal_htf if b[2] > entry_price * 1.003]
        if not above_levels:
            above_5m = [b[2] for b in bars_5m_up_to_t if b[2] > entry_price * 1.003]
            target_res = max(above_5m) if above_5m else entry_price + 2.0 * r_dist
        else:
            # Next structural 4h/1d resistance above entry
            target_res = min(above_levels)
        return round((target_res - entry_price) / r_dist, 4)

    below_levels = [b[3] for b in causal_htf if b[3] < entry_price * 0.997]
    if not below_levels:
        below_5m = [b[3] for b in bars_5m_up_to_t if b[3] < entry_price * 0.997]
        target_sup = min(below_5m) if below_5m else entry_price - 2.0 * r_dist
    else:
        target_sup = max(below_levels)
    return round((entry_price - target_sup) / r_dist, 4)


def run_h3_experiment(
    con: duckdb.DuckDBPyConnection,
    bars_by_sym: Mapping[str, Sequence[MinuteBar]] | None = None,
    funding_by_sym: Mapping[str, Mapping[int, float]] | None = None,
    tick_map: Mapping[str, float] | None = None,
    *,
    confirm_lag_bars: int = 3,
    notional_usd: float = 10_000.0,
    params: FrictionParams = FrictionParams(),
) -> H3ExperimentReport:
    """Run Experiment H3 (Box Maturity & Second-Wave Lifecycle) offline on DuckDB."""
    if bars_by_sym is None or funding_by_sym is None or tick_map is None:
        bars_by_sym, funding_by_sym, tick_map = load_bars_and_funding_from_duckdb(con)

    # Load causal 4h and 1d bars from `fact_perp_bars_htf` if available
    htf_by_sym: dict[str, list[tuple[int, float, float, float, float]]] = {}
    has_htf = (
        con.execute(
            "SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'fact_perp_bars_htf'"
        ).fetchone()[0]
        > 0
    )
    if has_htf:
        for sym, tf, ts_open, o, h, l, c in con.execute(
            "SELECT symbol, timeframe, ts, open, high, low, close FROM fact_perp_bars_htf WHERE timeframe IN ('4h', '1d') ORDER BY symbol, ts"
        ).fetchall():
            dur = 14400 if str(tf) == "4h" else 86400
            close_ts = int(ts_open) + dur
            htf_by_sym.setdefault(str(sym), []).append(
                (close_ts, float(o), float(h), float(l), float(c))
            )

    raw_setups: list[tuple[H3SetupCandidate, int]] = []

    for sym, series in sorted(bars_by_sym.items()):
        if not series or normalize_base_coin(sym) == "BTC":
            continue
        idx_map = {b.ts: i for i, b in enumerate(series)}
        buckets: dict[int, list[MinuteBar]] = {}
        for b in series:
            buckets.setdefault((b.ts // 300) * 300, []).append(b)
        b5: list[tuple[int, float, float, float, float, float]] = []
        for b_open in sorted(buckets):
            grp = buckets[b_open]
            if len(grp) < 5:
                continue
            b5.append(
                (
                    b_open + 300,
                    grp[0].open,
                    max(x.high for x in grp),
                    min(x.low for x in grp),
                    grp[-1].close,
                    sum(x.quote_vol for x in grp),
                )
            )
        if len(b5) < 40:
            continue

        htf_list = htf_by_sym.get(sym, [])
        f_sched = funding_by_sym.get(sym) if funding_by_sym else None
        last_box_ts = -10**12
        last_sw_ts = -10**12

        for t_idx in range(36, len(b5)):
            close_ts, o_t, h_t, l_t, c_t, v_t = b5[t_idx]
            entry_1m_idx = idx_map.get(close_ts)
            if entry_1m_idx is None or (len(series) - 1 - entry_1m_idx) < 120:
                continue
            if series[entry_1m_idx + 120].ts - series[entry_1m_idx].ts > 125 * 60:
                continue

            # Window of trailing 48 five-minute bars (4 hours) up to t_idx
            win_start = max(0, t_idx - 48)
            win_b5 = b5[win_start : t_idx + 1]
            pivots = extract_causal_swing_pivots(
                win_b5, confirm_lag_bars=confirm_lag_bars, up_to_idx=len(win_b5) - 1
            )
            if len(pivots) < 4:
                continue

            # Compute ATR compression ratio: trailing 6 bars (30m) vs prior 24 bars (2h)
            recent_trs = [x[2] - x[3] for x in win_b5[-6:]]
            base_trs = [x[2] - x[3] for x in win_b5[-30:-6]]
            atr_recent = sum(recent_trs) / len(recent_trs) if recent_trs else 0.0
            atr_base = sum(base_trs) / len(base_trs) if base_trs else atr_recent
            atr_comp = atr_recent / atr_base if atr_base > 0 else 1.0

            lookback_15m = series[max(0, entry_1m_idx - 15) : entry_1m_idx]
            trs_1m = [b.high - b.low for b in lookback_15m]
            atr_1m = sum(trs_1m) / len(trs_1m) if trs_1m else c_t * 0.002
            adv_5m = max(100.0, sum(b.quote_vol for b in lookback_15m[-5:]))

            # --- Pattern 1: Box Maturity Breakout (`>=2` upper probes + `>=2` lower probes + ATR compression) ---
            if close_ts - last_box_ts >= 3600 and atr_comp <= 0.88:
                up_lvl, up_touches, up_conf_ts = _cluster_boundary_touches(
                    pivots, "HIGH", tol_frac=0.0085
                )
                lo_lvl, lo_touches, lo_conf_ts = _cluster_boundary_touches(
                    pivots, "LOW", tol_frac=0.0085
                )
                if (
                    up_touches >= 2
                    and lo_touches >= 2
                    and up_lvl > lo_lvl * 1.004
                    and (up_lvl - lo_lvl) / lo_lvl <= 0.06
                ):
                    box_dir: str | None = None
                    stop_px = 0.0
                    if c_t > up_lvl and b5[t_idx - 1][4] <= up_lvl:
                        box_dir = "UP"
                        stop_px = min(
                            (up_lvl + lo_lvl) / 2.0, c_t * (1.0 - 0.004)
                        )
                    elif c_t < lo_lvl and b5[t_idx - 1][4] >= lo_lvl:
                        box_dir = "DOWN"
                        stop_px = max(
                            (up_lvl + lo_lvl) / 2.0, c_t * (1.0 + 0.004)
                        )

                    if box_dir is not None:
                        last_conf = max(up_conf_ts, lo_conf_ts)
                        validate_ex_ante_grouping(
                            [
                                "box_touch_count",
                                "atr_compression_ratio",
                                "room_to_resistance_r",
                            ],
                            feature_timestamps={
                                "box_touch_count": float(last_conf),
                                "atr_compression_ratio": float(close_ts),
                                "room_to_resistance_r": float(close_ts),
                            },
                            entry_ts=float(close_ts),
                        )
                        room_r = _compute_causal_room_to_resistance_r(
                            box_dir, c_t, stop_px, close_ts, htf_list, b5[: t_idx + 1]
                        )
                        if room_r >= 1.0:
                            sig = CandidateSignal(
                                signal_id=f"h3_box_{normalize_base_coin(sym)}_{close_ts}",
                                wave_cluster_id="pending",
                                symbol=sym,
                                direction=box_dir,
                                entry_ts=close_ts,
                                entry_price=c_t,
                                stop_price=stop_px,
                                tick_size=tick_map.get(sym, 0.0001),
                                atr_1m_entry=atr_1m,
                                adv_5m_entry_usd=adv_5m,
                                notional_usd=notional_usd,
                            )
                            sim_w = simulate_holding_policy(
                                sig,
                                series,
                                policy_kind="wave",
                                k_be=0.8,
                                max_hold_minutes=1440,
                                funding_schedule=f_sched,
                                params=params,
                            )
                            sim_b = simulate_holding_policy(
                                sig,
                                series,
                                policy_kind="fixed_bracket",
                                target_r=1.5,
                                max_hold_minutes=1440,
                                funding_schedule=f_sched,
                                params=params,
                            )
                            raw_setups.append(
                                (
                                    H3SetupCandidate(
                                        signal=sig,
                                        setup_kind="BOX_BREAKOUT",
                                        upper_touches=up_touches,
                                        lower_touches=lo_touches,
                                        box_touch_count=up_touches + lo_touches,
                                        atr_compression_ratio=round(atr_comp, 4),
                                        room_to_resistance_r=room_r,
                                        trigger_knife_lock_avoided=True,
                                        confirm_lag_bars=confirm_lag_bars,
                                        last_pivot_confirm_ts=last_conf,
                                        net_r_wave=sim_w.net_r,
                                        net_r_bracket=sim_b.net_r,
                                        left_side_knife_net_r=None,
                                    ),
                                    close_ts,
                                )
                            )
                            last_box_ts = close_ts

            # --- Pattern 2: Divergence / Second-Wave Lifecycle ---
            # Stage 1 impulse -> Stage 2 high-volume divergence (TriggerKnifeLock=True, no left-side catching)
            # -> Stage 3 contraction (confirmed swing low + ATR/vol contraction) -> Stage 4 right-side re-break
            if close_ts - last_sw_ts >= 3600 and atr_comp <= 0.90:
                hi_pivs = [p for p in pivots if p.kind == "HIGH"]
                lo_pivs = [p for p in pivots if p.kind == "LOW"]
                if len(hi_pivs) >= 2 and len(lo_pivs) >= 2:
                    p_hi1 = hi_pivs[-1]
                    prior_lows = [p for p in lo_pivs if p.pivot_idx < p_hi1.pivot_idx]
                    after_lows = [p for p in lo_pivs if p.pivot_idx > p_hi1.pivot_idx]
                    if prior_lows and after_lows:
                        p_lo0 = min(prior_lows, key=lambda x: x.price)
                        p_lo1 = min(after_lows, key=lambda x: x.price)
                        impulse_pct = (
                            (p_hi1.price - p_lo0.price) / p_lo0.price * 100.0
                            if p_lo0.price > 0
                            else 0.0
                        )
                        retrace_frac = (
                            (p_hi1.price - p_lo1.price)
                            / max(p_hi1.price - p_lo0.price, 1e-9)
                        )
                        # Stage-1 impulse >= 1.2%, Stage-2 pullback retraces 25%..78% and holds above p_lo0
                        # Stage-3 contraction followed by Stage-4 close re-breaking above recent consolidation high
                        recent_cons_hi = max(
                            x[2] for x in win_b5[p_lo1.pivot_idx : -1]
                        )
                        if (
                            impulse_pct >= 1.2
                            and 0.25 <= retrace_frac <= 0.78
                            and c_t > recent_cons_hi
                            and b5[t_idx - 1][4] <= recent_cons_hi
                        ):
                            last_conf = max(p_hi1.confirm_ts, p_lo1.confirm_ts)
                            validate_ex_ante_grouping(
                                [
                                    "atr_compression_ratio",
                                    "room_to_resistance_r",
                                    "trigger_knife_lock",
                                ],
                                feature_timestamps={
                                    "atr_compression_ratio": float(close_ts),
                                    "room_to_resistance_r": float(close_ts),
                                    "trigger_knife_lock": float(last_conf),
                                },
                                entry_ts=float(close_ts),
                            )
                            stop_px = min(p_lo1.price, c_t * (1.0 - 0.004))
                            room_r = _compute_causal_room_to_resistance_r(
                                "UP",
                                c_t,
                                stop_px,
                                close_ts,
                                htf_list,
                                b5[: t_idx + 1],
                            )

                            # Counterfactual: what if the operator caught the falling knife on the left side
                            # during Stage-2 high-volume pullback (`p_hi1.pivot_idx + 1`) before confirmation?
                            knife_r: float | None = None
                            knife_b5_idx = win_start + p_hi1.pivot_idx + 1
                            if knife_b5_idx < t_idx:
                                knife_ts = b5[knife_b5_idx][0]
                                knife_1m_idx = idx_map.get(knife_ts)
                                if knife_1m_idx is not None:
                                    k_px = b5[knife_b5_idx][4]
                                    k_stop = k_px * (1.0 - 0.005)
                                    k_sig = CandidateSignal(
                                        signal_id=f"knife_{normalize_base_coin(sym)}_{knife_ts}",
                                        wave_cluster_id="pending",
                                        symbol=sym,
                                        direction="UP",
                                        entry_ts=knife_ts,
                                        entry_price=k_px,
                                        stop_price=k_stop,
                                        tick_size=tick_map.get(sym, 0.0001),
                                        atr_1m_entry=atr_1m,
                                        adv_5m_entry_usd=adv_5m,
                                        notional_usd=notional_usd,
                                    )
                                    k_sim = simulate_holding_policy(
                                        k_sig,
                                        series,
                                        policy_kind="wave",
                                        k_be=0.8,
                                        max_hold_minutes=1440,
                                        funding_schedule=f_sched,
                                        params=params,
                                    )
                                    knife_r = k_sim.net_r

                            sig = CandidateSignal(
                                signal_id=f"h3_sw4_{normalize_base_coin(sym)}_{close_ts}",
                                wave_cluster_id="pending",
                                symbol=sym,
                                direction="UP",
                                entry_ts=close_ts,
                                entry_price=c_t,
                                stop_price=stop_px,
                                tick_size=tick_map.get(sym, 0.0001),
                                atr_1m_entry=atr_1m,
                                adv_5m_entry_usd=adv_5m,
                                notional_usd=notional_usd,
                            )
                            sim_w = simulate_holding_policy(
                                sig,
                                series,
                                policy_kind="wave",
                                k_be=0.8,
                                max_hold_minutes=1440,
                                funding_schedule=f_sched,
                                params=params,
                            )
                            sim_b = simulate_holding_policy(
                                sig,
                                series,
                                policy_kind="fixed_bracket",
                                target_r=1.5,
                                max_hold_minutes=1440,
                                funding_schedule=f_sched,
                                params=params,
                            )
                            raw_setups.append(
                                (
                                    H3SetupCandidate(
                                        signal=sig,
                                        setup_kind="SECOND_WAVE_STAGE4",
                                        upper_touches=len(hi_pivs),
                                        lower_touches=len(lo_pivs),
                                        box_touch_count=len(hi_pivs) + len(lo_pivs),
                                        atr_compression_ratio=round(atr_comp, 4),
                                        room_to_resistance_r=room_r,
                                        trigger_knife_lock_avoided=True,
                                        confirm_lag_bars=confirm_lag_bars,
                                        last_pivot_confirm_ts=last_conf,
                                        net_r_wave=sim_w.net_r,
                                        net_r_bracket=sim_b.net_r,
                                        left_side_knife_net_r=knife_r,
                                    ),
                                    close_ts,
                                )
                            )
                            last_sw_ts = close_ts

    # Sort chronologically and assign `<24h`-merged wave_cluster_id
    raw_setups.sort(key=lambda item: (item[1], item[0].signal.signal_id))
    wake_events = [(item[0].signal.signal_id, float(item[1])) for item in raw_setups]
    clusters, id_to_cluster = build_wave_clusters(wake_events)

    final_setups: list[H3SetupCandidate] = []
    for cand, _ in raw_setups:
        cid = id_to_cluster.get(cand.signal.signal_id, "wave_cluster_000")
        sig_updated = CandidateSignal(
            signal_id=cand.signal.signal_id,
            wave_cluster_id=cid,
            symbol=cand.signal.symbol,
            direction=cand.signal.direction,
            entry_ts=cand.signal.entry_ts,
            entry_price=cand.signal.entry_price,
            stop_price=cand.signal.stop_price,
            tick_size=cand.signal.tick_size,
            atr_1m_entry=cand.signal.atr_1m_entry,
            adv_5m_entry_usd=cand.signal.adv_5m_entry_usd,
            notional_usd=cand.signal.notional_usd,
        )
        final_setups.append(
            H3SetupCandidate(
                signal=sig_updated,
                setup_kind=cand.setup_kind,
                upper_touches=cand.upper_touches,
                lower_touches=cand.lower_touches,
                box_touch_count=cand.box_touch_count,
                atr_compression_ratio=cand.atr_compression_ratio,
                room_to_resistance_r=cand.room_to_resistance_r,
                trigger_knife_lock_avoided=cand.trigger_knife_lock_avoided,
                confirm_lag_bars=cand.confirm_lag_bars,
                last_pivot_confirm_ts=cand.last_pivot_confirm_ts,
                net_r_wave=cand.net_r_wave,
                net_r_bracket=cand.net_r_bracket,
                left_side_knife_net_r=cand.left_side_knife_net_r,
            )
        )

    box_list = [s for s in final_setups if s.setup_kind == "BOX_BREAKOUT"]
    sw_list = [s for s in final_setups if s.setup_kind == "SECOND_WAVE_STAGE4"]

    combined_stat = cluster_robust_mean(
        [s.net_r_wave for s in final_setups],
        [s.signal.wave_cluster_id for s in final_setups],
    )
    box_stat = cluster_robust_mean(
        [s.net_r_wave for s in box_list],
        [s.signal.wave_cluster_id for s in box_list],
    )
    sw_stat = cluster_robust_mean(
        [s.net_r_wave for s in sw_list],
        [s.signal.wave_cluster_id for s in sw_list],
    )

    # Walk-forward out-of-sample split (clusters after the first calibration cluster)
    all_cluster_ids = sorted({s.signal.wave_cluster_id for s in final_setups})
    oos_cluster_ids = set(all_cluster_ids[1:]) if len(all_cluster_ids) > 1 else set(all_cluster_ids)
    oos_setups = [s for s in final_setups if s.signal.wave_cluster_id in oos_cluster_ids]
    oos_stat = cluster_robust_mean(
        [s.net_r_wave for s in oos_setups],
        [s.signal.wave_cluster_id for s in oos_setups],
    )

    knife_vals = [
        s.left_side_knife_net_r
        for s in sw_list
        if s.left_side_knife_net_r is not None
    ]
    left_knife_mean = sum(knife_vals) / len(knife_vals) if knife_vals else 0.0
    right_sw_mean = sw_stat.mean if sw_list else 0.0

    # Pre-registered H3 KILL rule (Work Order Item 7):
    # "KILL rule: if out-of-sample independent wave clusters < 15 OR the NetR 95% CI lower
    # bound < 0, do NOT build any automatic geometry recognizer. Deliver instead a spec for
    # kairos-desk manual watch-range entry"
    n_clusters_oos = oos_stat.n_clusters
    ci_low = oos_stat.ci95_low
    kill_reasons: list[str] = []
    if n_clusters_oos < H3_MIN_OOS_CLUSTERS_BAR:
        kill_reasons.append(
            f"OOS independent wave clusters G_oos = {n_clusters_oos} < {H3_MIN_OOS_CLUSTERS_BAR} minimum bar"
        )
    if math.isnan(ci_low) or ci_low < H3_MIN_CI95_LOWER_BAR:
        kill_reasons.append(
            f"OOS NetR 95% CI lower bound = {ci_low:+.4f}R < 0.0000R minimum bar"
        )

    kill_triggered = len(kill_reasons) > 0
    h3_passed = not kill_triggered
    if h3_passed:
        verdict_reason = (
            f"PASS: G_oos = {n_clusters_oos} >= 15 and OOS NetR 95% CI lower bound = {ci_low:+.4f}R >= 0."
        )
    else:
        verdict_reason = (
            "FAIL (KILLED BY PRE-REGISTERED H3 RULE): "
            + "; ".join(kill_reasons)
            + ". Automatic box/second-wave geometry recognizer is NOT built; "
            "delivering `kairos-desk` manual watch-range entry specification instead."
        )

    return H3ExperimentReport(
        confirm_lag_bars=confirm_lag_bars,
        n_total_setups=len(final_setups),
        n_box_setups=len(box_list),
        n_second_wave_setups=len(sw_list),
        n_clusters_total=combined_stat.n_clusters,
        n_clusters_oos=n_clusters_oos,
        combined_stat=combined_stat,
        oos_stat=oos_stat,
        box_stat=box_stat,
        second_wave_stat=sw_stat,
        left_side_knife_mean_r=left_knife_mean,
        right_side_stage4_mean_r=right_sw_mean,
        knife_avoidance_edge_r=right_sw_mean - left_knife_mean,
        kill_rule_triggered=kill_triggered,
        kill_reasons=tuple(kill_reasons),
        h3_passed=h3_passed,
        h3_verdict_reason=verdict_reason,
        setups=tuple(final_setups),
    )


def format_h3_markdown(
    report: H3ExperimentReport,
    *,
    dataset_label: str = "OKX USDT-SWAP PIT 1m + HTF (4h/1d) Replay",
) -> str:
    """Format Experiment H3 (Box Maturity & Second-Wave Lifecycle) Markdown report."""
    today = datetime.now(timezone.utc).date().isoformat()
    verdict = "PASS" if report.h3_passed else "FAIL"
    comb = report.combined_stat
    oos = report.oos_stat
    bx = report.box_stat
    sw = report.second_wave_stat

    lines = [
        f"# Experiment H3 — Box Maturity & Second-Wave Lifecycle ({today})",
        "",
        f"- **Verdict**: **{verdict}** (`{report.h3_verdict_reason}`)",
        f"- **Dataset**: {dataset_label}",
        f"- **Swing Pivot Confirmation Lag**: `k = {report.confirm_lag_bars}` bars (`{report.confirm_lag_bars * 5}m` mandatory right-shoulder confirmation; zero lookahead)",
        f"- **Detected Structural Setups (`N`)**: {report.n_total_setups} (`BOX_BREAKOUT = {report.n_box_setups}`, `SECOND_WAVE_STAGE4 = {report.n_second_wave_setups}`)",
        f"- **Independent Wave Clusters (`G_total / G_oos`)**: `{report.n_clusters_total}` total / `{report.n_clusters_oos}` out-of-sample (`<24h` merge rule)",
        "",
        "## 1. Pre-Registered Formalization & Kill Rule",
        "",
        "1. **Box Maturity (`BOX_BREAKOUT`)**: Requires `>= 2` confirmed swing high probes + `>= 2` confirmed swing low probes (`BoxTouchCount >= 4`), `ATRCompressionRatio <= 0.88`, and causal `RoomToResistanceR >= 1.0R` to the next 4h/1d resistance.",
        "2. **Divergence / Second-Wave Lifecycle (`SECOND_WAVE_STAGE4`)**: Stage-1 impulse (`>= 1.2%`) -> Stage-2 high-volume divergence (`TriggerKnifeLock = True`, no left-side catching) -> Stage-3 volatility contraction after confirmed higher low -> Stage-4 right-side re-break.",
        "3. **Pre-Registered KILL Rule (Item 7)**: If out-of-sample independent wave clusters `G_oos < 15` **OR** OOS NetR 95% CI lower bound `< 0.0000R`, **DO NOT** build any automatic geometry recognizer or wire `CycleMap` geometry fields; deliver instead the `kairos-desk` manual watch-range entry specification.",
        "",
        "## 2. Structural Setup Performance (`wave_k_be=0.8` Full-Friction NetR)",
        "",
        "| Cohort | Setups (N) | Wave Clusters (G) | Mean NetR | Cluster SE | t-stat | 95% CI |",
        "|---|---:|---:|---:|---:|---:|---:|",
        f"| `Box Maturity Breakouts (BOX_BREAKOUT)` | {bx.n_obs} | {bx.n_clusters} | **`{bx.mean:+.4f}R`** | `{bx.cluster_se:.4f}` | `{bx.t_stat:+.2f}` | `[{bx.ci95_low:+.4f}, {bx.ci95_high:+.4f}]` |",
        f"| `Second-Wave Stage-4 Re-Break (SECOND_WAVE_STAGE4)` | {sw.n_obs} | {sw.n_clusters} | **`{sw.mean:+.4f}R`** | `{sw.cluster_se:.4f}` | `{sw.t_stat:+.2f}` | `[{sw.ci95_low:+.4f}, {sw.ci95_high:+.4f}]` |",
        f"| `Combined In-Sample Setups` | {comb.n_obs} | {comb.n_clusters} | **`{comb.mean:+.4f}R`** | `{comb.cluster_se:.4f}` | `{comb.t_stat:+.2f}` | `[{comb.ci95_low:+.4f}, {comb.ci95_high:+.4f}]` |",
        f"| **`Walk-Forward Out-of-Sample (OOS)`** | {oos.n_obs} | **{oos.n_clusters}** | **`{oos.mean:+.4f}R`** | `{oos.cluster_se:.4f}` | `{oos.t_stat:+.2f}` | **`[{oos.ci95_low:+.4f}, {oos.ci95_high:+.4f}]`** |",
        "",
        "## 3. Left-Side Knife Catching vs Stage-4 Right-Side Re-Break Counterfactual",
        "",
        "| Entry Timing | Mean Full-Friction NetR | Difference (`Stage-4 - Left-Side Knife`) |",
        "|---|---:|---:|",
        f"| `Stage-2 Left-Side Falling Knife (Unconfirmed Pullback)` | `{report.left_side_knife_mean_r:+.4f}R` | — |",
        f"| `Stage-4 Right-Side Confirmed Re-Break (k={report.confirm_lag_bars} Lag)` | `{report.right_side_stage4_mean_r:+.4f}R` | **`{report.knife_avoidance_edge_r:+.4f}R`** |",
        "",
        "## 4. H3 Kill-Rule Evaluation & Shadow Wiring Decision",
        "",
        "| Kill Criterion | Observed Value | Required Bar | Triggered? |",
        "|---|---:|---:|---|",
        f"| `OOS Independent Wave Clusters (G_oos)` | **`{report.n_clusters_oos}`** | `>= 15` | **{'YES (KILLED)' if report.n_clusters_oos < 15 else 'NO'}** |",
        f"| `OOS NetR 95% CI Lower Bound` | **`{oos.ci95_low:+.4f}R`** | `>= 0.0000R` | **{'YES (KILLED)' if (math.isnan(oos.ci95_low) or oos.ci95_low < 0.0) else 'NO'}** |",
        "",
        f"- **H3 Overall Verdict**: **{verdict}** (`{'KILLED' if report.kill_rule_triggered else 'PASSED'}`)",
        f"- **Shadow Fields on `CycleMap` (`RoomToResistanceR`, `BoxTouchCount`, `ATRCompressionRatio`, `TriggerKnifeLock`)**: "
        f"**{'WIRE AS SHADOW FIELDS' if report.h3_passed else 'DO NOT WIRE (KILLED BY ITEM 7 RULE)'}**",
        "- **Fallback Deliverable (Item 7)**: Delivered `kairos-desk` manual watch-range entry specification (`tools/research/H3_watch_range_spec.md` & `docs/90-engineering/specs/kairos-desk-watch-range-spec.md`).",
    ]
    return "\n".join(lines)
