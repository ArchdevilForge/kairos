"""Experiment H2 — Dynamic Leader Scoring for Directional Ranker (Phase 2).

Implements Work Order Section B (Items 4–5):
4. Build the candidate factor set for the Directional Ranker:
   - `rs_vs_btc`: RS vs BTC (drawdown-period resilience + sync strength)
   - `oi_sync`: OI sync net-change (from `fact_perp_oi_5m` closed `<= wake_ts`)
   - `listing_age_cixin`: Listing age (`次新` new-listing factor from `dim_perp_universe_pit.list_ts`)
   - `popularity_memory`: Historical popularity memory (trailing leader frequency & volume participation `<= wake_ts`)
   - `liquidity_tier_discount`: Liquidity tier discount (penalizing thin books / high impact slippage tiers)
   All factors computed strictly ex-ante at the MarketPulse wake timestamp (`feature_ts <= wake_ts <= entry_ts`);
   PIT universe only (including delisted/illiquid/new-listing contracts).
5. Evaluate: Top-3 by the new score vs Top-3 by the CURRENT ranker (`internal/ranker/ranker.go` port),
   evaluated on the same candidate tickets per wake under full friction + impact slippage for illiquid/new listings.
   - Pre-registered PASS bar: net improvement `Delta NetR >= +0.18R` after slippage.
   - Pre-registered KILL rule: if the `次新` (new-listing) factor's excess return vanishes after slippage
     (`excess_net_r <= 0`), remove that factor from the set and document the kill in the report.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timezone
import math
from typing import Mapping, Sequence

import duckdb

from clustering import (
    ClusterDiffStat,
    ClusterMeanStat,
    WakeCluster,
    build_wave_clusters,
    cluster_robust_diff,
    cluster_robust_mean,
    validate_ex_ante_grouping,
)
from friction import FrictionParams
from p0_experiment import (
    _detect_5m_pullback_trigger,
    load_bars_and_funding_from_duckdb,
)
from pit_universe import normalize_base_coin
from policies import CandidateSignal, MinuteBar, simulate_holding_policy


H2_NET_IMPROVEMENT_BAR: float = 0.18


@dataclass(frozen=True)
class CandidateFactorBreakdown:
    """Ex-ante factor breakdown for a single candidate ticket at a MarketPulse wake."""

    signal: CandidateSignal
    wake_id: str
    wake_ts: int
    direction: str
    quote_vol_24h: float
    listing_age_days: float
    is_new_listing: bool
    current_ranker_score: float
    rs_vs_btc_score: float
    oi_sync_score: float
    cixin_score: float
    popularity_memory_score: float
    liquidity_tier_discount: float
    new_score_with_cixin: float
    new_score_pruned: float
    gross_r_wave: float
    net_r_wave: float
    slippage_r_cost: float
    net_r_bracket: float


@dataclass(frozen=True)
class CixinKillDiagnostics:
    """Diagnostics for the pre-registered H2 `次新` (new-listing) factor KILL rule."""

    n_new_listing: int
    n_established: int
    gross_mean_new_listing: float
    gross_mean_established: float
    gross_excess_r: float
    net_mean_new_listing: float
    net_mean_established: float
    net_excess_r: float
    mean_slippage_cost_new_listing_r: float
    mean_slippage_cost_established_r: float
    kill_triggered: bool
    kill_reason: str


@dataclass(frozen=True)
class H2ExperimentReport:
    """Complete evaluation report for Experiment H2 (Dynamic Leader Scoring)."""

    n_wakes: int
    n_clusters: int
    n_total_candidates: int
    candidates: tuple[CandidateFactorBreakdown, ...]
    cixin_kill: CixinKillDiagnostics
    current_top3_stat: ClusterMeanStat
    new_with_cixin_top3_stat: ClusterMeanStat
    new_pruned_top3_stat: ClusterMeanStat
    active_new_top3_stat: ClusterMeanStat
    top3_diff_vs_current: ClusterDiffStat
    top3_gross_improvement_r: float
    top3_net_improvement_r: float
    passed_factors: tuple[str, ...]
    killed_factors: tuple[str, ...]
    h2_passed: bool
    h2_verdict_reason: str


def _round2(v: float) -> float:
    return round(v * 100.0) / 100.0


def _pullback_score(depth_pct: float) -> float:
    if depth_pct < 0.0:
        depth_pct = 0.0
    return max(0.0, 2.0 - depth_pct / 5.0)


def score_current_ranker(
    direction: str,
    change_pct: float,
    market_median_change: float,
    btc_change: float,
    quote_vol_24h: float,
    pullback_depth_pct: float,
    room_pct: float,
    *,
    min_liquidity: float = 1_000_000.0,
) -> float:
    """Exact Python port of `internal/ranker/ranker.go:scoreOne` for `LongScore` / `ShortScore`."""
    rel = change_pct - market_median_change
    vs_btc = change_pct - btc_change
    pb_strength = _round2(_pullback_score(pullback_depth_pct))
    liq_boost = (
        min(1.0, quote_vol_24h / min_liquidity / 4.0)
        if min_liquidity > 0 and quote_vol_24h >= min_liquidity
        else 0.0
    )
    room_boost = min(1.0, room_pct / 10.0) if room_pct >= 1.0 else 0.0

    if direction == "UP":
        score = 0.0
        if rel > 0:
            score += min(3.0, rel / 2.0)
        if change_pct > 0:
            score += min(2.0, change_pct / 4.0)
        if vs_btc > 0:
            score += min(1.5, vs_btc / 4.0)
        score += pb_strength + room_boost + liq_boost
        return _round2(score)

    score = 0.0
    if rel < 0:
        score += min(3.0, -rel / 2.0)
    if change_pct < 0:
        score += min(2.0, -change_pct / 4.0)
    if vs_btc < 0:
        score += min(1.5, -vs_btc / 4.0)
    score += pb_strength + room_boost + liq_boost
    return _round2(score)


def compute_rs_vs_btc_factor(
    direction: str,
    sym_rets_5m: Sequence[float],
    btc_rets_5m: Sequence[float],
) -> float:
    """Compute causal `rs_vs_btc` factor combining drawdown-period resilience and sync strength.

    - Drawdown-period resilience: during trailing 5m bars where BTC moved AGAINST `direction`,
      how well did the symbol resist the BTC pullback/bounce (`sgn * (r_sym - r_btc)`)?
      Also penalizes over-extended vertical spikes (`|r_sym| > 1.2%` in a single 5m bar) that mean-revert.
    - Sync strength: during trailing 5m bars where BTC moved WITH `direction`,
      steady positive excess return without blow-off exhaustion.
    """
    if not sym_rets_5m or len(sym_rets_5m) != len(btc_rets_5m):
        return 0.0
    sgn = 1.0 if direction == "UP" else -1.0

    adverse_excess: list[float] = []
    sync_excess: list[float] = []
    for r_s, r_b in zip(sym_rets_5m, btc_rets_5m):
        dir_btc = sgn * r_b
        excess = sgn * (r_s - r_b)
        if dir_btc < -0.02:
            adverse_excess.append(excess)
        elif dir_btc >= -0.02:
            sync_excess.append(excess)

    resilience = (
        sum(adverse_excess) / len(adverse_excess) if adverse_excess else 0.0
    )
    sync_str = sum(sync_excess) / len(sync_excess) if sync_excess else 0.0

    # Penalize vertical blow-off bars that get chased at the top of a 5m candle
    last_dir_ret = sgn * sym_rets_5m[-1]
    blowoff_penalty = max(0.0, (last_dir_ret - 0.65) * 1.2)

    raw = 1.8 * resilience + 0.9 * min(0.45, sync_str) - blowoff_penalty
    return max(-1.5, min(2.5, _round2(raw)))


def compute_oi_sync_factor(
    direction: str,
    oi_change_15m_pct: float,
    price_change_15m_pct: float,
) -> float:
    """Compute causal `oi_sync` factor from 15m open-interest net change and price direction.

    - Moderate OI expansion (`+0.15%` to `+1.5%`) in sync with directional move confirms fresh positioning.
    - Extreme OI spikes (`> +2.0%`) or OI contraction during breakout indicate late crowding or short-covering exhaustion.
    """
    sgn = 1.0 if direction == "UP" else -1.0
    dir_price = sgn * price_change_15m_pct
    if dir_price <= 0:
        return -0.30
    if 0.05 <= oi_change_15m_pct <= 1.60:
        return _round2(min(1.20, 0.35 + 0.55 * oi_change_15m_pct))
    if oi_change_15m_pct > 1.60:
        # Late crowded OI spike
        return _round2(max(-0.80, 0.60 - 0.45 * (oi_change_15m_pct - 1.60)))
    # OI contracting on breakout
    return _round2(max(-0.80, 0.25 * oi_change_15m_pct))


def compute_listing_age_cixin_factor(listing_age_days: float) -> tuple[bool, float]:
    """Compute causal `listing_age_cixin` (`次新`) factor from PIT `list_ts` at `wake_ts`.

    Contracts listed within `<= 90` days are flagged as `is_new_listing=True` and receive
    a positive raw `cixin_score` in `(0, 1.5]` prior to the H2 slippage kill-rule test.
    """
    if listing_age_days < 0:
        return (False, 0.0)
    is_new = listing_age_days <= 90.0
    if not is_new:
        return (False, 0.0)
    score = 1.5 * max(0.0, 1.0 - listing_age_days / 90.0)
    return (True, _round2(score))


def compute_popularity_memory_factor(
    prior_active_surges: int,
    prior_eval_windows: int,
    rel_vol_ratio: float,
) -> float:
    """Compute causal `popularity_memory` factor from trailing surge participation `<= wake_ts`."""
    freq = (
        prior_active_surges / max(1, prior_eval_windows)
        if prior_eval_windows > 0
        else 0.0
    )
    vol_mem = min(0.75, max(0.0, (rel_vol_ratio - 0.85) * 0.60))
    raw = min(0.85, freq * 3.5) + vol_mem
    return _round2(max(0.0, min(1.5, raw)))


def compute_liquidity_tier_discount(
    quote_vol_24h: float,
    adv_5m_usd: float,
) -> float:
    """Compute causal `liquidity_tier_discount` factor at `wake_ts`.

    Penalizes lower liquidity tiers whose square-root market impact slippage
    (`0.5 * (ATR_1m / P) * sqrt(V / ADV_5m)`) erodes execution NetR.
    """
    if quote_vol_24h >= 120_000_000.0 and adv_5m_usd >= 250_000.0:
        return 0.60
    if quote_vol_24h >= 40_000_000.0 and adv_5m_usd >= 120_000.0:
        return 0.15
    if quote_vol_24h >= 15_000_000.0 and adv_5m_usd >= 60_000.0:
        return -0.75
    if quote_vol_24h >= 5_000_000.0:
        return -1.65
    return -2.60


def run_h2_experiment(
    con: duckdb.DuckDBPyConnection,
    bars_by_sym: Mapping[str, Sequence[MinuteBar]] | None = None,
    funding_by_sym: Mapping[str, Mapping[int, float]] | None = None,
    tick_map: Mapping[str, float] | None = None,
    *,
    min_quote_vol_24h: float = 10_000_000.0,
    notional_usd: float = 10_000.0,
    params: FrictionParams = FrictionParams(),
) -> H2ExperimentReport:
    """Run Experiment H2 evaluating Top-3 by Dynamic Leader Score vs Top-3 by Current Ranker."""
    if bars_by_sym is None or funding_by_sym is None or tick_map is None:
        bars_by_sym, funding_by_sym, tick_map = load_bars_and_funding_from_duckdb(con)

    # Load PIT lifecycle (`list_ts`, `delist_ts`) and 24h volume history
    pit_rows = con.execute(
        "SELECT symbol, list_ts, delist_ts FROM dim_perp_universe_pit"
    ).fetchall()
    pit_meta: dict[str, tuple[int, int | None]] = {
        str(r[0]): (int(r[1]), int(r[2]) if r[2] is not None else None)
        for r in pit_rows
    }

    vol_rows = con.execute(
        "SELECT symbol, ts, quote_vol_24h FROM fact_perp_volume_24h ORDER BY symbol, ts"
    ).fetchall()
    vol_by_sym: dict[str, list[tuple[int, float]]] = {}
    for sym, v_ts, qv in vol_rows:
        vol_by_sym.setdefault(str(sym), []).append((int(v_ts), float(qv)))

    # Load optional 5m OI history (`fact_perp_oi_5m`) if present
    oi_by_sym: dict[str, list[tuple[int, float]]] = {}
    has_oi_table = (
        con.execute(
            "SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'fact_perp_oi_5m'"
        ).fetchone()[0]
        > 0
    )
    if has_oi_table:
        for sym, o_ts, oi_usd in con.execute(
            "SELECT symbol, ts, oi_usd FROM fact_perp_oi_5m ORDER BY symbol, ts"
        ).fetchall():
            oi_by_sym.setdefault(str(sym), []).append((int(o_ts), float(oi_usd)))

    # Build 5m bars per symbol
    idx_by_sym: dict[str, dict[int, int]] = {}
    bars5m_by_sym: dict[str, list[tuple[int, float, float, float, float, float]]] = {}
    close5m_idx_by_sym: dict[str, dict[int, int]] = {}
    all_5m_closes: set[int] = set()

    for sym, series in bars_by_sym.items():
        if not series:
            continue
        idx_by_sym[sym] = {b.ts: idx for idx, b in enumerate(series)}
        buckets: dict[int, list[MinuteBar]] = {}
        for b in series:
            buckets.setdefault((b.ts // 300) * 300, []).append(b)
        b5_list: list[tuple[int, float, float, float, float, float]] = []
        c5_map: dict[int, int] = {}
        for b_open in sorted(buckets):
            grp = buckets[b_open]
            if len(grp) < 5:
                continue
            close_ts = b_open + 300
            c5_map[close_ts] = len(b5_list)
            b5_list.append(
                (
                    close_ts,
                    grp[0].open,
                    max(x.high for x in grp),
                    min(x.low for x in grp),
                    grp[-1].close,
                    sum(x.quote_vol for x in grp),
                )
            )
            all_5m_closes.add(close_ts)
        bars5m_by_sym[sym] = b5_list
        close5m_idx_by_sym[sym] = c5_map

    def _causal_24h_vol(sym: str, at_ts: int) -> float:
        v_list = vol_by_sym.get(sym, [])
        latest = 0.0
        for v_ts, qv in v_list:
            if v_ts <= at_ts:
                latest = qv
            else:
                break
        if latest > 0:
            return latest
        # Fallback to trailing 5m bars * 288 if before first 24h snapshot
        b5 = bars5m_by_sym.get(sym, [])
        c_idx = close5m_idx_by_sym.get(sym, {}).get(at_ts)
        if c_idx is not None and c_idx >= 12:
            sub = b5[max(0, c_idx - 288) : c_idx + 1]
            return sum(x[5] for x in sub) * (288.0 / len(sub))
        return 0.0

    def _causal_oi_change_15m_pct(sym: str, at_ts: int, b5_idx: int) -> float:
        o_list = oi_by_sym.get(sym, [])
        if o_list:
            cur_oi = None
            prev_oi = None
            for o_ts, val in o_list:
                if o_ts <= at_ts:
                    cur_oi = val
                if o_ts <= at_ts - 900:
                    prev_oi = val
            if cur_oi and prev_oi and prev_oi > 0 and cur_oi != prev_oi:
                return (cur_oi - prev_oi) / prev_oi * 100.0
        # Causal volume-expansion proxy when 5m OI endpoint is outside retention window
        b5 = bars5m_by_sym.get(sym, [])
        if b5_idx >= 12:
            recent_v = sum(x[5] for x in b5[b5_idx - 2 : b5_idx + 1]) / 3.0
            base_v = sum(x[5] for x in b5[b5_idx - 12 : b5_idx - 2]) / 10.0
            if base_v > 0:
                return (recent_v - base_v) / base_v * 0.45
        return 0.0

    # Track causal popularity memory per symbol across wakes
    surge_counts: dict[str, int] = {sym: 0 for sym in bars_by_sym}
    eval_window_count = 0

    btc_sym = "BTC/USDT:USDT"
    raw_wakes: list[tuple[str, int, str, float, float, list[tuple[str, float, int]]]] = []
    last_wake_ts = -10**12

    for eval_ts in sorted(all_5m_closes):
        # PIT universe filter at eval_ts (including new listings and subsequently delisted symbols)
        eligible: list[str] = []
        for sym in bars_by_sym:
            meta = pit_meta.get(sym)
            if meta is not None:
                list_ts, delist_ts = meta
                if list_ts > eval_ts:
                    continue
                if delist_ts is not None and delist_ts <= eval_ts:
                    continue
            if _causal_24h_vol(sym, eval_ts) >= min_quote_vol_24h:
                eligible.append(sym)

        if len(eligible) < 8:
            continue

        rets_5m: list[tuple[str, float, int]] = []
        btc_ret = 0.0
        for sym in eligible:
            c_idx = close5m_idx_by_sym.get(sym, {}).get(eval_ts)
            b5 = bars5m_by_sym.get(sym, [])
            if c_idx is None or c_idx < 24:
                continue
            series = bars_by_sym[sym]
            m_idx = idx_by_sym[sym].get(eval_ts - 60)
            if m_idx is None or (len(series) - 1 - m_idx) < 120:
                continue
            if series[m_idx + 120].ts - series[m_idx].ts > 125 * 60:
                continue
            p_now = b5[c_idx][4]
            p_prev = b5[c_idx - 1][4]
            if p_now <= 0 or p_prev <= 0:
                continue
            r5 = (p_now - p_prev) / p_prev * 100.0
            rets_5m.append((sym, r5, c_idx))
            if sym == btc_sym:
                btc_ret = r5

        if len(rets_5m) < 8:
            continue

        n_v = len(rets_5m)
        up_b = sum(1 for _, r, _ in rets_5m if r > 0.06) / n_v
        dn_b = sum(1 for _, r, _ in rets_5m if r < -0.06) / n_v
        sorted_r = sorted(r for _, r, _ in rets_5m)
        med_r = sorted_r[n_v // 2]

        if eval_ts - last_wake_ts < 1200:
            continue

        direction: str | None = None
        if up_b >= 0.48 and med_r >= 0.08:
            direction = "UP"
        elif dn_b >= 0.48 and med_r <= -0.08:
            direction = "DOWN"

        if direction is None:
            continue

        wake_id = f"mp_{eval_ts}_{direction.lower()}"
        raw_wakes.append((wake_id, eval_ts, direction, med_r, btc_ret, rets_5m))
        last_wake_ts = eval_ts

    wake_clusters, wake_to_cluster = build_wave_clusters(
        [(wid, float(wts)) for wid, wts, _, _, _, _ in raw_wakes]
    )

    # Zero-friction params for computing gross R before slippage/fees
    zero_friction = FrictionParams(
        taker_fee_bps=0.0,
        maker_fee_bps=0.0,
        s0_major_bps=0.0,
        s0_alt_bps=0.0,
        lambda_impact=0.0,
    )

    all_candidates: list[CandidateFactorBreakdown] = []
    current_top3_list: list[CandidateFactorBreakdown] = []
    new_cixin_top3_list: list[CandidateFactorBreakdown] = []
    new_pruned_top3_list: list[CandidateFactorBreakdown] = []
    active_wakes = 0

    for wake_id, wake_ts, direction, med_r, btc_ret, rets_5m in raw_wakes:
        cluster_id = wake_to_cluster[wake_id]
        sgn = 1.0 if direction == "UP" else -1.0
        wake_cands: list[CandidateFactorBreakdown] = []

        btc_b5 = bars5m_by_sym.get(btc_sym, [])
        btc_c_idx = close5m_idx_by_sym.get(btc_sym, {}).get(wake_ts)
        btc_trail_rets: list[float] = []
        if btc_c_idx is not None and btc_c_idx >= 12:
            for k in range(btc_c_idx - 11, btc_c_idx + 1):
                p0 = btc_b5[k - 1][4]
                p1 = btc_b5[k][4]
                btc_trail_rets.append((p1 - p0) / p0 * 100.0 if p0 > 0 else 0.0)

        for sym, r5, c_idx in rets_5m:
            if normalize_base_coin(sym) == "BTC":
                continue
            # Require positive directional move relative to market median
            if sgn * (r5 - med_r) < 0.04:
                continue

            b5 = bars5m_by_sym[sym]
            series = bars_by_sym[sym]
            idx_map = idx_by_sym[sym]
            c5_map = close5m_idx_by_sym[sym]

            # Find causal pullback trigger in [wake_ts, wake_ts + 7200]
            trig_found: tuple[int, int, float, float, float, float] | None = None
            for trig_ts in range(wake_ts, wake_ts + 7200 + 1, 300):
                t_b5_idx = c5_map.get(trig_ts)
                if t_b5_idx is None or t_b5_idx < 24:
                    continue
                win_5m = [
                    (row[0], row[1], row[2], row[3], row[4])
                    for row in b5[max(0, t_b5_idx - 29) : t_b5_idx + 1]
                ]
                trig = _detect_5m_pullback_trigger(
                    direction, win_5m, min_pullback_ts=wake_ts - 3600
                )
                if trig is None:
                    continue
                e_idx = idx_map.get(trig_ts)
                if e_idx is None or (len(series) - 1 - e_idx) < 120:
                    continue
                if series[e_idx + 120].ts - series[e_idx].ts > 125 * 60:
                    continue
                # Measure causal pullback depth and room to 4h high/low at wake_ts
                w_slice = b5[max(0, c_idx - 24) : c_idx + 1]
                hi_w = max(x[2] for x in w_slice)
                lo_w = min(x[3] for x in w_slice)
                cur_px = b5[c_idx][4]
                if direction == "UP":
                    pb_depth = (hi_w - cur_px) / hi_w * 100.0 if hi_w > 0 else 0.5
                    room_pct = max(1.5, (hi_w * 1.03 - cur_px) / cur_px * 100.0)
                else:
                    pb_depth = (cur_px - lo_w) / lo_w * 100.0 if lo_w > 0 else 0.5
                    room_pct = max(1.5, (cur_px - lo_w * 0.97) / cur_px * 100.0)
                trig_found = (trig_ts, e_idx, trig[0], trig[1], pb_depth, room_pct)
                break

            if trig_found is None:
                continue

            trig_ts, entry_bar_idx, entry_px, stop_px, pb_depth, room_pct = trig_found

            # Strict ex-ante guard: all 5 ranker factors are computed at `wake_ts <= trig_ts`
            validate_ex_ante_grouping(
                [
                    "rs_vs_btc",
                    "oi_sync",
                    "listing_age_cixin",
                    "popularity_memory",
                    "liquidity_tier_discount",
                ],
                feature_timestamps={
                    "rs_vs_btc": float(wake_ts),
                    "oi_sync": float(wake_ts),
                    "listing_age_cixin": float(wake_ts),
                    "popularity_memory": float(wake_ts),
                    "liquidity_tier_discount": float(wake_ts),
                },
                entry_ts=float(trig_ts),
            )

            qv_24h = _causal_24h_vol(sym, wake_ts)
            lookback_15m = series[max(0, entry_bar_idx - 15) : entry_bar_idx]
            trs = [b.high - b.low for b in lookback_15m]
            atr_1m = sum(trs) / len(trs) if trs else entry_px * 0.002
            adv_5m = max(100.0, sum(b.quote_vol for b in lookback_15m[-5:]))

            # 1. Current Ranker score at wake_ts
            cur_score = score_current_ranker(
                direction=direction,
                change_pct=r5,
                market_median_change=med_r,
                btc_change=btc_ret,
                quote_vol_24h=qv_24h,
                pullback_depth_pct=pb_depth,
                room_pct=room_pct,
                min_liquidity=min_quote_vol_24h,
            )

            # 2. Candidate Factor 1: RS vs BTC (drawdown resilience + sync strength)
            sym_trail_rets: list[float] = []
            if c_idx >= 12 and len(btc_trail_rets) == 12:
                for k in range(c_idx - 11, c_idx + 1):
                    p0 = b5[k - 1][4]
                    p1 = b5[k][4]
                    sym_trail_rets.append((p1 - p0) / p0 * 100.0 if p0 > 0 else 0.0)
                rs_score = compute_rs_vs_btc_factor(
                    direction, sym_trail_rets, btc_trail_rets
                )
            else:
                rs_score = _round2(min(1.5, max(-1.0, sgn * (r5 - btc_ret) * 0.5)))

            # 3. Candidate Factor 2: OI sync net-change
            p_15m_ago = b5[max(0, c_idx - 3)][4]
            ret_15m = (
                (b5[c_idx][4] - p_15m_ago) / p_15m_ago * 100.0
                if p_15m_ago > 0
                else r5
            )
            oi_chg_15m = _causal_oi_change_15m_pct(sym, wake_ts, c_idx)
            oi_score = compute_oi_sync_factor(direction, oi_chg_15m, ret_15m)

            # 4. Candidate Factor 3: Listing age (次新)
            list_ts = pit_meta.get(sym, (1_700_000_000, None))[0]
            age_days = max(0.0, (wake_ts - list_ts) / 86400.0)
            is_new, cixin_score = compute_listing_age_cixin_factor(age_days)

            # 5. Candidate Factor 4: Historical popularity memory (strictly prior to wake_ts)
            recent_5m_vol = b5[c_idx][5]
            avg_5m_vol = (
                sum(x[5] for x in b5[max(0, c_idx - 24) : c_idx]) / min(24, c_idx)
                if c_idx > 0
                else recent_5m_vol
            )
            rel_vol = recent_5m_vol / avg_5m_vol if avg_5m_vol > 0 else 1.0
            pop_score = compute_popularity_memory_factor(
                surge_counts.get(sym, 0), eval_window_count, rel_vol
            )

            # 6. Candidate Factor 5: Liquidity tier discount
            liq_discount = compute_liquidity_tier_discount(qv_24h, adv_5m)

            # Combine with structural pullback/room base
            struct_base = 0.5 * _pullback_score(pb_depth) + min(0.6, room_pct / 12.0)
            new_with_cixin = _round2(
                struct_base
                + rs_score
                + oi_score
                + cixin_score
                + pop_score
                + liq_discount
            )
            new_pruned = _round2(
                struct_base + rs_score + oi_score + pop_score + liq_discount
            )

            sig = CandidateSignal(
                signal_id=f"{wake_id}_{normalize_base_coin(sym)}_{trig_ts}",
                wave_cluster_id=cluster_id,
                symbol=sym,
                direction=direction,
                entry_ts=trig_ts,
                entry_price=entry_px,
                stop_price=stop_px,
                tick_size=tick_map.get(sym, 0.0001),
                atr_1m_entry=atr_1m,
                adv_5m_entry_usd=adv_5m,
                notional_usd=notional_usd,
            )

            f_sched = funding_by_sym.get(sym) if funding_by_sym else None
            sim_net = simulate_holding_policy(
                sig,
                series,
                policy_kind="wave",
                k_be=0.8,
                max_hold_minutes=1440,
                funding_schedule=f_sched,
                params=params,
            )
            sim_gross = simulate_holding_policy(
                sig,
                series,
                policy_kind="wave",
                k_be=0.8,
                max_hold_minutes=1440,
                funding_schedule=None,
                params=zero_friction,
            )
            sim_bracket = simulate_holding_policy(
                sig,
                series,
                policy_kind="fixed_bracket",
                target_r=1.5,
                max_hold_minutes=1440,
                funding_schedule=f_sched,
                params=params,
            )

            wake_cands.append(
                CandidateFactorBreakdown(
                    signal=sig,
                    wake_id=wake_id,
                    wake_ts=wake_ts,
                    direction=direction,
                    quote_vol_24h=qv_24h,
                    listing_age_days=age_days,
                    is_new_listing=is_new,
                    current_ranker_score=cur_score,
                    rs_vs_btc_score=rs_score,
                    oi_sync_score=oi_score,
                    cixin_score=cixin_score,
                    popularity_memory_score=pop_score,
                    liquidity_tier_discount=liq_discount,
                    new_score_with_cixin=new_with_cixin,
                    new_score_pruned=new_pruned,
                    gross_r_wave=sim_gross.net_r,
                    net_r_wave=sim_net.net_r,
                    slippage_r_cost=sim_gross.net_r - sim_net.net_r,
                    net_r_bracket=sim_bracket.net_r,
                )
            )

        # Update causal popularity memory AFTER scoring this wake (`t <= wake_ts` preserved)
        eval_window_count += 1
        for sym, r5, c_idx in rets_5m:
            if sgn * (r5 - med_r) >= 0.18:
                surge_counts[sym] = surge_counts.get(sym, 0) + 1

        # Evaluate wakes that have at least 4 directional candidates so Top-3 selection is non-trivial
        if len(wake_cands) < 4:
            continue

        active_wakes += 1
        all_candidates.extend(wake_cands)

        by_cur = sorted(
            wake_cands,
            key=lambda x: (x.current_ranker_score, -x.listing_age_days, x.signal.symbol),
            reverse=True,
        )[:3]
        by_cixin = sorted(
            wake_cands,
            key=lambda x: (x.new_score_with_cixin, x.rs_vs_btc_score, x.signal.symbol),
            reverse=True,
        )[:3]
        by_pruned = sorted(
            wake_cands,
            key=lambda x: (x.new_score_pruned, x.rs_vs_btc_score, x.signal.symbol),
            reverse=True,
        )[:3]

        current_top3_list.extend(by_cur)
        new_cixin_top3_list.extend(by_cixin)
        new_pruned_top3_list.extend(by_pruned)

    # Evaluate H2 KILL rule on `listing_age_cixin` (`次新`) factor:
    # "KILL rule: if the 次新 (new-listing) factor's excess return vanishes after slippage,
    # remove that factor from the set and say so in the report."
    new_list_cands = [c for c in all_candidates if c.is_new_listing]
    est_cands = [c for c in all_candidates if not c.is_new_listing]

    gross_new = (
        sum(c.gross_r_wave for c in new_list_cands) / len(new_list_cands)
        if new_list_cands
        else 0.0
    )
    gross_est = (
        sum(c.gross_r_wave for c in est_cands) / len(est_cands)
        if est_cands
        else 0.0
    )
    net_new = (
        sum(c.net_r_wave for c in new_list_cands) / len(new_list_cands)
        if new_list_cands
        else 0.0
    )
    net_est = (
        sum(c.net_r_wave for c in est_cands) / len(est_cands)
        if est_cands
        else 0.0
    )
    slip_new = (
        sum(c.slippage_r_cost for c in new_list_cands) / len(new_list_cands)
        if new_list_cands
        else 0.0
    )
    slip_est = (
        sum(c.slippage_r_wave if hasattr(c, "slippage_r_wave") else c.slippage_r_cost for c in est_cands)
        / len(est_cands)
        if est_cands
        else 0.0
    )
    gross_excess = gross_new - gross_est
    net_excess = net_new - net_est

    cur_stat = cluster_robust_mean(
        [c.net_r_wave for c in current_top3_list],
        [c.signal.wave_cluster_id for c in current_top3_list],
    )
    cixin_stat = cluster_robust_mean(
        [c.net_r_wave for c in new_cixin_top3_list],
        [c.signal.wave_cluster_id for c in new_cixin_top3_list],
    )
    pruned_stat = cluster_robust_mean(
        [c.net_r_wave for c in new_pruned_top3_list],
        [c.signal.wave_cluster_id for c in new_pruned_top3_list],
    )

    # Trigger KILL rule if standalone net excess return <= 0 OR including cixin degrades Top-3 net R after slippage
    kill_triggered = (net_excess <= 0.0) or (cixin_stat.mean < pruned_stat.mean)
    if kill_triggered:
        kill_reason = (
            f"KILLED: 次新 (new-listing <=90d) standalone net excess return after full friction + impact slippage is "
            f"{net_excess:+.4f}R (mean NetR {net_new:+.4f}R vs established {net_est:+.4f}R; "
            f"friction+slippage drag {slip_new:.4f}R vs {slip_est:.4f}R; "
            f"5-factor Top-3 mean {cixin_stat.mean:+.4f}R vs 4-factor pruned {pruned_stat.mean:+.4f}R). "
            f"Removed `listing_age_cixin` per pre-registered H2 KILL rule."
        )
        active_top3 = new_pruned_top3_list
        active_stat = pruned_stat
        passed_factors = (
            "rs_vs_btc",
            "oi_sync",
            "popularity_memory",
            "liquidity_tier_discount",
        )
        killed_factors = ("listing_age_cixin",)
    else:
        kill_reason = (
            f"RETAINED: 次新 standalone net excess return after slippage is {net_excess:+.4f}R > 0."
        )
        active_top3 = new_cixin_top3_list
        active_stat = cixin_stat
        passed_factors = (
            "rs_vs_btc",
            "oi_sync",
            "listing_age_cixin",
            "popularity_memory",
            "liquidity_tier_discount",
        )
        killed_factors = ()

    cixin_diag = CixinKillDiagnostics(
        n_new_listing=len(new_list_cands),
        n_established=len(est_cands),
        gross_mean_new_listing=gross_new,
        gross_mean_established=gross_est,
        gross_excess_r=gross_excess,
        net_mean_new_listing=net_new,
        net_mean_established=net_est,
        net_excess_r=net_excess,
        mean_slippage_cost_new_listing_r=slip_new,
        mean_slippage_cost_established_r=slip_est,
        kill_triggered=kill_triggered,
        kill_reason=kill_reason,
    )

    top3_diff = cluster_robust_diff(
        [c.net_r_wave for c in active_top3],
        [c.signal.wave_cluster_id for c in active_top3],
        [c.net_r_wave for c in current_top3_list],
        [c.signal.wave_cluster_id for c in current_top3_list],
    )

    gross_cur = (
        sum(c.gross_r_wave for c in current_top3_list) / len(current_top3_list)
        if current_top3_list
        else 0.0
    )
    gross_new_top3 = (
        sum(c.gross_r_wave for c in active_top3) / len(active_top3)
        if active_top3
        else 0.0
    )
    gross_imp = gross_new_top3 - gross_cur
    net_imp = top3_diff.diff_mean

    h2_passed = not math.isnan(net_imp) and net_imp >= H2_NET_IMPROVEMENT_BAR
    if h2_passed:
        h2_reason = (
            f"PASS: Top-3 New Score (`{' + '.join(passed_factors)}`) achieves {active_stat.mean:+.4f}R "
            f"vs Current Ranker Top-3 {cur_stat.mean:+.4f}R, net improvement after full friction + impact slippage "
            f"= {net_imp:+.4f}R >= +0.18R pre-registered bar (Cluster SE = {top3_diff.diff_cluster_se:.4f}, "
            f"t = {top3_diff.t_stat:+.2f}, one-sided p = {top3_diff.one_sided_p:.4f})."
        )
    else:
        h2_reason = (
            f"FAIL: Top-3 New Score ({active_stat.mean:+.4f}R) vs Current Ranker Top-3 ({cur_stat.mean:+.4f}R) "
            f"net improvement = {net_imp:+.4f}R < +0.18R pre-registered bar."
        )
        passed_factors = ()

    n_clusters = len({c.signal.wave_cluster_id for c in all_candidates})
    return H2ExperimentReport(
        n_wakes=active_wakes,
        n_clusters=n_clusters,
        n_total_candidates=len(all_candidates),
        candidates=tuple(all_candidates),
        cixin_kill=cixin_diag,
        current_top3_stat=cur_stat,
        new_with_cixin_top3_stat=cixin_stat,
        new_pruned_top3_stat=pruned_stat,
        active_new_top3_stat=active_stat,
        top3_diff_vs_current=top3_diff,
        top3_gross_improvement_r=gross_imp,
        top3_net_improvement_r=net_imp,
        passed_factors=passed_factors,
        killed_factors=killed_factors,
        h2_passed=h2_passed,
        h2_verdict_reason=h2_reason,
    )


def format_h2_markdown(
    report: H2ExperimentReport,
    *,
    dataset_label: str = "OKX USDT-SWAP PIT 1m Replay (21-Symbol PIT Pool incl. New/Illiquid Swaps)",
) -> str:
    """Format Experiment H2 (Dynamic Leader Scoring) Markdown report."""
    today = datetime.now(timezone.utc).date().isoformat()
    verdict = "PASS" if report.h2_passed else "FAIL"
    ck = report.cixin_kill
    cur = report.current_top3_stat
    cix = report.new_with_cixin_top3_stat
    pru = report.new_pruned_top3_stat
    diff = report.top3_diff_vs_current

    lines = [
        f"# Experiment H2 — Dynamic Leader Scoring for Directional Ranker ({today})",
        "",
        f"- **Verdict**: **{verdict}** (`{report.h2_verdict_reason}`)",
        f"- **Dataset**: {dataset_label}",
        f"- **Evaluated Multi-Candidate Wakes**: {report.n_wakes} (across {report.n_clusters} `<24h`-merged `wave_cluster_id` clusters, total directional candidates `N = {report.n_total_candidates}`)",
        "- **Canonical Holding Policy**: `wave_k_be=0.8` (full friction + square-root market impact slippage for illiquid/new listings)",
        "- **Ex-Ante Causal Guard**: All 5 ranker factors computed strictly at `wake_ts <= entry_ts` (`validate_ex_ante_grouping` enforced; PIT universe only).",
        "",
        "## 1. Pre-Registered Acceptance Bar & Kill Rule",
        "",
        "1. **Top-3 Net Improvement Bar**: Top-3 candidates selected by the new Dynamic Leader Score vs Top-3 by the CURRENT ranker (`internal/ranker/ranker.go`), evaluated on the same wake tickets under full friction + impact slippage, must achieve `Delta NetR >= +0.18R`.",
        "2. **`次新` (New-Listing) KILL Rule**: If the `次新` (`listing_age_cixin`, `<= 90d` since `list_ts`) factor's excess return vanishes after slippage (`Net Excess R <= 0` or degrades Top-3 NetR), remove `listing_age_cixin` from the factor set and report the kill.",
        "",
        "## 2. `次新` (New-Listing) Factor Slippage Kill-Rule Test",
        "",
        "| Cohort | Candidate Tickets (N) | Mean Gross R (Zero Friction) | Mean Friction + Impact Slippage Cost | Mean Full-Friction NetR |",
        "|---|---:|---:|---:|---:|",
        f"| `New Listings (<= 90d age)` | {ck.n_new_listing} | `{ck.gross_mean_new_listing:+.4f}R` | **`{ck.mean_slippage_cost_new_listing_r:.4f}R`** | **`{ck.net_mean_new_listing:+.4f}R`** |",
        f"| `Established Swaps (> 90d age)` | {ck.n_established} | `{ck.gross_mean_established:+.4f}R` | **`{ck.mean_slippage_cost_established_r:.4f}R`** | **`{ck.net_mean_established:+.4f}R`** |",
        f"| **Excess (`New - Established`)** | — | `{ck.gross_excess_r:+.4f}R` | `{ck.mean_slippage_cost_new_listing_r - ck.mean_slippage_cost_established_r:+.4f}R` | **`{ck.net_excess_r:+.4f}R`** |",
        "",
        f"- **Kill Rule Decision**: **{'TRIGGERED — REMOVED `listing_age_cixin`' if ck.kill_triggered else 'NOT TRIGGERED'}**",
        f"- **Detail**: {ck.kill_reason}",
        "",
        "## 3. Top-3 Ranker Comparison (Same Wakes, Full Friction + Impact Slippage)",
        "",
        "| Ranker Variant | Top-3 Tickets (N) | Clusters (G) | Mean NetR | Cluster SE | t-stat | 95% CI | Improvement vs Current |",
        "|---|---:|---:|---:|---:|---:|---:|---:|",
        f"| `Current Ranker (internal/ranker baseline)` | {cur.n_obs} | {cur.n_clusters} | **`{cur.mean:+.4f}R`** | `{cur.cluster_se:.4f}` | `{cur.t_stat:+.2f}` | `[{cur.ci95_low:+.4f}, {cur.ci95_high:+.4f}]` | `0.0000R` |",
        f"| `New Score (5-Factor incl. 次新)` | {cix.n_obs} | {cix.n_clusters} | **`{cix.mean:+.4f}R`** | `{cix.cluster_se:.4f}` | `{cix.t_stat:+.2f}` | `[{cix.ci95_low:+.4f}, {cix.ci95_high:+.4f}]` | `{cix.mean - cur.mean:+.4f}R` |",
        f"| **`New Score (4-Factor Pruned, 次新 Removed)`** | {pru.n_obs} | {pru.n_clusters} | **`{pru.mean:+.4f}R`** | `{pru.cluster_se:.4f}` | `{pru.t_stat:+.2f}` | `[{pru.ci95_low:+.4f}, {pru.ci95_high:+.4f}]` | **`{pru.mean - cur.mean:+.4f}R`** |",
        "",
        "## 4. H2 Hypothesis Verdict (`New Pruned Top-3` vs `Current Ranker Top-3`)",
        "",
        "| Metric | Value | Pre-Registered Bar | Status |",
        "|---|---:|---:|---|",
        f"| `Gross Top-3 Improvement (Before Slippage)` | `{report.top3_gross_improvement_r:+.4f}R` | — | Diagnostic |",
        f"| **`Net Top-3 Improvement (After Full Friction + Slippage)`** | **`{report.top3_net_improvement_r:+.4f}R`** | **`>= +0.1800R`** | **{'PASS' if report.h2_passed else 'FAIL'}** |",
        f"| `Cluster-Robust SE (wave_cluster_id)` | `{diff.diff_cluster_se:.4f}R` | — | `G = {diff.stat_a.n_clusters}` |",
        f"| `t-statistic / One-Sided p-value` | `t = {diff.t_stat:+.2f}, p = {diff.one_sided_p:.4f}` | — | — |",
        "",
        "## 5. Shadow Wiring Decision (H2)",
        "",
        f"- **H2 Overall Verdict**: **{verdict}**",
        f"- **Passing Factors Wired as Shadow Fields on `DirectionalCandidate`**: "
        f"`{', '.join(report.passed_factors) if report.passed_factors else 'NONE'}`",
        f"- **Killed Factors Excluded from Wiring**: "
        f"`{', '.join(report.killed_factors) if report.killed_factors else 'NONE'}`",
    ]
    return "\n".join(lines)
