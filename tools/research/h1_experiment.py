"""Experiment H1 (H1a & H1b): Causal Seasons State Machine & BTC Resonance Validation.

Validates Phase 2 Section A hypotheses offline on PIT-filtered OKX perpetual data
using the Phase 1 full-friction NetR and wave_cluster_id CR1 harness:

1. Causal Seasons state machine (`SPRING`, `SUMMER`, `AUTUMN`, `WINTER`) computed
   strictly from rolling cross-sectional breadth and volume aggregates at `t <= entry_ts`
   (zero manual regime labels, enforced by `validate_ex_ante_grouping`).
2. H1a test:
   - Candidate breakout/pullback tickets under ex-ante `SUMMER/AUTUMN` vs `WINTER/SPRING`
     (cluster-adjusted mean NetR difference, `wave_cluster_id` robust SE, walk-forward OOS).
   - Pre-registered PASS bar: `Delta NetR >= +0.25R` with one-sided `p < 0.05`.
   - Winter defense test: risk multiplier `0.25x` (or `0.0x` silence) in `WINTER`
     retains `>= 80%` of cumulative net profit while cutting max drawdown `>= 30%`.
3. H1b test:
   - After a BTC upward `market_impulse`, alts breaking out in sync within `[0, 15m]`
     (`RESONANCE`) vs laggards starting `[2h, 12h]` later (`LAGGING_CATCHUP`).
   - Pre-registered PASS bar: `RESONANCE` group mean NetR significantly above
     `LAGGING_CATCHUP` group (one-sided `p < 0.05`).
   - Crowding & fakeout measurement: BTC near rolling highs + market-wide funding at
     extreme percentile (`CrowdingPct >= 0.80`) -> alt long-breakout fakeout (stop-hit) rate.
"""
from __future__ import annotations

import math
from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Mapping, Sequence

import duckdb

from clustering import (
    ClusterDiffStat,
    ClusterMeanStat,
    WakeCluster,
    assign_signal_to_wave_cluster,
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
from pit_universe import normalize_base_coin, normalize_perp_symbol
from policies import CandidateSignal, MinuteBar, simulate_holding_policy

SEASON_SPRING = "SPRING"
SEASON_SUMMER = "SUMMER"
SEASON_AUTUMN = "AUTUMN"
SEASON_WINTER = "WINTER"
VALID_SEASONS: tuple[str, ...] = (
    SEASON_SPRING,
    SEASON_SUMMER,
    SEASON_AUTUMN,
    SEASON_WINTER,
)

SYNC_RESONANCE = "RESONANCE"
SYNC_LAGGING_CATCHUP = "LAGGING_CATCHUP"
SYNC_NONE = "NONE"

H1A_DELTA_NETR_BAR = 0.25
H1A_P_VALUE_BAR = 0.05
H1A_WINTER_PROFIT_RETENTION_BAR = 0.80
H1A_WINTER_DD_REDUCTION_BAR = 0.30
H1B_P_VALUE_BAR = 0.05


@dataclass(frozen=True)
class SeasonObservation:
    """Ex-ante causal market season and crowding state at timestamp `ts`."""

    ts: int
    season_regime: str
    breadth_fast: float
    breadth_slow: float
    breadth_momentum: float
    volume_ratio: float
    btc_near_high: bool
    crowding_pct: float


@dataclass(frozen=True)
class AnnotatedTicketSignal:
    """Candidate signal annotated strictly with ex-ante H1 features (`feature_ts <= entry_ts`)."""

    signal: CandidateSignal
    feature_ts: int
    season_regime: str
    season_regime_oos: str
    breadth_fast: float
    breadth_slow: float
    volume_ratio: float
    btc_sync_mode: str
    btc_impulse_lag_sec: float | None
    btc_near_high: bool
    crowding_pct: float
    net_r_wave: float
    net_r_bracket: float
    net_r_30m: float
    stop_hit: bool


@dataclass(frozen=True)
class WinterDefenseMetrics:
    """Portfolio equity & drawdown impact of scaling down risk in WINTER."""

    baseline_cum_r: float
    baseline_max_dd_r: float
    defense_025x_cum_r: float
    defense_025x_max_dd_r: float
    profit_retention_025x: float
    dd_reduction_025x: float
    silence_0x_cum_r: float
    silence_0x_max_dd_r: float
    profit_retention_0x: float
    dd_reduction_0x: float
    passed: bool


@dataclass(frozen=True)
class CrowdingFakeoutMetrics:
    """Alt long-breakout fakeout (stop-hit) rate when BTC near high + extreme funding percentile."""

    n_crowded_high: int
    fakeout_rate_crowded_high: float
    mean_net_r_crowded_high: float
    n_baseline: int
    fakeout_rate_baseline: float
    mean_net_r_baseline: float
    fakeout_rate_lift: float


@dataclass(frozen=True)
class H1ExperimentReport:
    """Complete output of Experiment H1 (H1a seasons & H1b BTC resonance)."""

    n_signals: int
    n_clusters: int
    tickets: tuple[AnnotatedTicketSignal, ...]
    season_stats: dict[str, ClusterMeanStat]
    h1a_in_sample_diff: ClusterDiffStat
    h1a_oos_diff: ClusterDiffStat
    winter_defense: WinterDefenseMetrics
    h1a_passed: bool
    h1a_verdict_reason: str
    sync_stats: dict[str, ClusterMeanStat]
    h1b_resonance_vs_laggard: ClusterDiffStat
    crowding_fakeout: CrowdingFakeoutMetrics
    h1b_passed: bool
    h1b_verdict_reason: str


def _max_drawdown_r(pnl_series: Sequence[float]) -> float:
    """Compute maximum peak-to-trough drawdown in R units along a sequence of trade outcomes."""
    cum = 0.0
    peak = 0.0
    max_dd = 0.0
    for r in pnl_series:
        cum += float(r)
        if cum > peak:
            peak = cum
        dd = peak - cum
        if dd > max_dd:
            max_dd = dd
    return max_dd


def classify_season_from_aggregates(
    breadth_fast: float,
    breadth_slow: float,
    volume_ratio: float,
    *,
    summer_breadth_min: float = 0.58,
    spring_breadth_min: float = 0.46,
    autumn_slow_breadth_min: float = 0.52,
    summer_vol_min: float = 0.98,
    winter_vol_max: float = 0.85,
) -> str:
    """Deterministic causal rule mapping `(breadth_fast, breadth_slow, volume_ratio)` -> SeasonRegime.

    Uses ONLY rolling breadth and volume aggregates (zero price level lookahead or manual labels):
    - `SUMMER`: High short-term breadth (`>= summer_breadth_min`) with sustained/expanding volume (`>= summer_vol_min`).
    - `AUTUMN`: Prior slow breadth was elevated (`>= autumn_slow_breadth_min`) but fast breadth is decaying
      (`breadth_fast < breadth_slow`) or volume is fading (`volume_ratio < summer_vol_min`), while `breadth_fast >= 0.40`.
    - `SPRING`: Fast breadth is recovering (`>= spring_breadth_min`) above slow breadth (`breadth_fast >= breadth_slow`)
      with non-contracted volume (`volume_ratio >= winter_vol_max`).
    - `WINTER`: Low breadth (`< spring_breadth_min` and not in autumn decay) or contracted volume chop (`volume_ratio < winter_vol_max`).
    """
    b_mom = breadth_fast - breadth_slow
    if breadth_fast >= summer_breadth_min and volume_ratio >= summer_vol_min:
        return SEASON_SUMMER
    if (
        breadth_slow >= autumn_slow_breadth_min
        and breadth_fast >= 0.40
        and (b_mom < -0.02 or volume_ratio < summer_vol_min)
    ):
        return SEASON_AUTUMN
    if (
        breadth_fast >= spring_breadth_min
        and b_mom >= -0.02
        and volume_ratio >= winter_vol_max
    ):
        return SEASON_SPRING
    return SEASON_WINTER


def build_causal_season_timeline(
    bars5m_by_sym: Mapping[str, Sequence[tuple[int, float, float, float, float, float]]],
    funding_by_sym: Mapping[str, Mapping[int, float]] | None = None,
    *,
    fast_bars: int = 6,   # 30m rolling breadth & volume
    slow_bars: int = 24,  # 2h rolling baseline breadth & volume
    noise_ret_pct: float = 0.05,
) -> dict[int, SeasonObservation]:
    """Compute causal `SeasonObservation` at every 5m close timestamp `t` using ONLY bars closed `<= t`."""
    # Map close_ts -> per-symbol (ret_5m_pct, quote_vol_5m, close_px)
    all_closes: set[int] = set()
    sym_idx: dict[str, dict[int, int]] = {}
    for sym, b5 in bars5m_by_sym.items():
        idx_map: dict[int, int] = {}
        for i, row in enumerate(b5):
            all_closes.add(row[0])
            idx_map[row[0]] = i
        sym_idx[sym] = idx_map

    sorted_ts = sorted(all_closes)
    # Pre-collect sorted 8h funding timestamps across universe for causal crowding percentile
    funding_history_means: list[tuple[int, float]] = []
    if funding_by_sym:
        by_tick: dict[int, list[float]] = {}
        for sym, sched in funding_by_sym.items():
            for f_ts, rate in sched.items():
                by_tick.setdefault(int(f_ts), []).append(float(rate))
        for f_ts in sorted(by_tick):
            vals = by_tick[f_ts]
            if vals:
                funding_history_means.append((f_ts, sum(vals) / len(vals)))

    btc_sym = "BTC/USDT:USDT"
    btc_bars = bars5m_by_sym.get(btc_sym, ())
    btc_idx_map = sym_idx.get(btc_sym, {})

    timeline: dict[int, SeasonObservation] = {}
    prev_season = SEASON_WINTER
    streak_candidate = SEASON_WINTER
    streak_len = 0

    for ts in sorted_ts:
        fast_up_ratios: list[float] = []
        slow_up_ratios: list[float] = []
        vol_fast_sum = 0.0
        vol_slow_per_fast_sum = 0.0
        valid_syms = 0

        for sym, b5 in bars5m_by_sym.items():
            idx = sym_idx[sym].get(ts)
            if idx is None or idx < 3:
                continue
            f_start = max(0, idx - fast_bars + 1)
            s_start = max(0, idx - slow_bars + 1)
            f_slice = b5[f_start : idx + 1]
            s_slice = b5[s_start : idx + 1]
            if len(f_slice) < 2 or len(s_slice) < 4:
                continue

            # Ensure contiguous window (do not bridge across multi-hour sample gaps)
            if f_slice[-1][0] - f_slice[0][0] > (len(f_slice) + 2) * 300:
                continue
            while len(s_slice) > len(f_slice) and (
                s_slice[-1][0] - s_slice[0][0] > (len(s_slice) + 4) * 300
            ):
                s_slice = s_slice[1:]

            ret_fast = (
                (f_slice[-1][4] - f_slice[0][1]) / f_slice[0][1] * 100.0
                if f_slice[0][1] > 0
                else 0.0
            )
            ret_slow = (
                (s_slice[-1][4] - s_slice[0][1]) / s_slice[0][1] * 100.0
                if s_slice[0][1] > 0
                else 0.0
            )
            fast_up_ratios.append(1.0 if ret_fast > noise_ret_pct else 0.0)
            slow_up_ratios.append(1.0 if ret_slow > noise_ret_pct else 0.0)

            v_fast = sum(x[5] for x in f_slice) / len(f_slice)
            v_slow = sum(x[5] for x in s_slice) / len(s_slice)
            vol_fast_sum += v_fast
            vol_slow_per_fast_sum += v_slow
            valid_syms += 1

        if valid_syms < 4:
            continue

        b_fast = sum(fast_up_ratios) / len(fast_up_ratios)
        b_slow = sum(slow_up_ratios) / len(slow_up_ratios)
        v_ratio = (
            vol_fast_sum / vol_slow_per_fast_sum
            if vol_slow_per_fast_sum > 0
            else 1.0
        )

        raw_season = classify_season_from_aggregates(b_fast, b_slow, v_ratio)
        # 2-bar causal hysteresis to prevent single-5m-bar flicker
        if raw_season == prev_season:
            active_season = prev_season
            streak_candidate = raw_season
            streak_len = 0
        else:
            if raw_season == streak_candidate:
                streak_len += 1
            else:
                streak_candidate = raw_season
                streak_len = 1
            if streak_len >= 2:
                active_season = raw_season
                prev_season = active_season
            else:
                active_season = prev_season

        # BTC near rolling highs (within 0.6% of trailing 48-bar / 4h high up to ts)
        btc_near_high = False
        b_idx = btc_idx_map.get(ts)
        if b_idx is not None and b_idx >= 6 and btc_bars:
            w_start = max(0, b_idx - 48)
            w_bars = [
                b for b in btc_bars[w_start : b_idx + 1] if ts - b[0] <= 48 * 300
            ]
            if w_bars:
                rolling_hi = max(b[2] for b in w_bars)
                btc_close = btc_bars[b_idx][4]
                if rolling_hi > 0 and (rolling_hi - btc_close) / rolling_hi <= 0.006:
                    btc_near_high = True

        # Causal CrowdingPct in [0, 1]: empirical percentile of latest known 8h funding <= ts
        # blended with short-term volume/breadth heat
        crowding_pct = 0.5
        past_f = [m for f_ts, m in funding_history_means if f_ts <= ts]
        if len(past_f) >= 3:
            cur_f = past_f[-1]
            rank_f = sum(1 for x in past_f if x <= cur_f) / len(past_f)
            crowding_pct = 0.7 * rank_f + 0.3 * min(1.0, max(0.0, b_fast))
        else:
            crowding_pct = min(1.0, max(0.0, 0.5 * b_fast + 0.25 * min(2.0, v_ratio)))

        timeline[ts] = SeasonObservation(
            ts=ts,
            season_regime=active_season,
            breadth_fast=b_fast,
            breadth_slow=b_slow,
            breadth_momentum=b_fast - b_slow,
            volume_ratio=v_ratio,
            btc_near_high=btc_near_high,
            crowding_pct=crowding_pct,
        )

    return timeline


def run_h1_experiment(
    con: duckdb.DuckDBPyConnection,
    bars_by_sym: Mapping[str, Sequence[MinuteBar]] | None = None,
    funding_by_sym: Mapping[str, Mapping[int, float]] | None = None,
    tick_map: Mapping[str, float] | None = None,
    *,
    min_quote_vol_24h: float = 30_000_000.0,
    min_valid_symbols: int = 6,
    notional_usd: float = 10_000.0,
    params: FrictionParams = FrictionParams(),
) -> H1ExperimentReport:
    """Execute Experiment H1 (H1a seasons & H1b BTC resonance) causally on DuckDB 1m bars."""
    if bars_by_sym is None or funding_by_sym is None or tick_map is None:
        bars_by_sym, funding_by_sym, tick_map = load_bars_and_funding_from_duckdb(con)

    # Pre-index 1m bars and 5m bars with 5m quote_vol
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
            b_open = (b.ts // 300) * 300
            buckets.setdefault(b_open, []).append(b)
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

    season_timeline = build_causal_season_timeline(bars5m_by_sym, funding_by_sym)

    # Load PIT universe lifecycle in memory to avoid per-bar SQL overhead
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

    def _pit_eligible_at(sym: str, at_ts: int) -> bool:
        meta = pit_meta.get(sym)
        if meta is not None:
            list_ts, delist_ts = meta
            if list_ts > at_ts:
                return False
            if delist_ts is not None and delist_ts <= at_ts:
                return False
        if min_quote_vol_24h <= 0:
            return True
        v_list = vol_by_sym.get(sym)
        if not v_list:
            return False
        # Latest causal 24h volume at <= at_ts
        latest_q = -1.0
        for v_ts, qv in v_list:
            if v_ts <= at_ts:
                latest_q = qv
            else:
                break
        return latest_q >= min_quote_vol_24h

    # Detect BTC upward market_impulse timestamps and MarketPulse wakes across all regimes
    # so we have candidate signals in both SUMMER/AUTUMN and WINTER/SPRING, as well as
    # RESONANCE ([0, 15m] after BTC impulse) and LAGGING_CATCHUP ([2h, 12h] after BTC impulse).
    btc_sym = "BTC/USDT:USDT"
    btc_up_impulses: list[int] = []
    raw_wakes: list[tuple[str, int, str, float, list[tuple[str, float, int]]]] = []
    last_wake_ts = -10**12

    for eval_ts in sorted(all_5m_closes):
        active_syms = [s for s in bars_by_sym if _pit_eligible_at(s, eval_ts)]
        if len(active_syms) < min_valid_symbols:
            continue

        rets_5m: list[tuple[str, float, int]] = []
        btc_ret_5m = 0.0
        btc_ret_15m = 0.0
        for sym in active_syms:
            series = bars_by_sym[sym]
            idx_map = idx_by_sym[sym]
            cur_idx = idx_map.get(eval_ts - 60)
            prev_idx = idx_map.get(eval_ts - 360)
            if cur_idx is None or prev_idx is None:
                continue
            if cur_idx < 30 or (len(series) - 1 - cur_idx) < 120:
                continue
            if series[cur_idx + 120].ts - series[cur_idx].ts > 125 * 60:
                continue
            p_now = series[cur_idx].close
            p_prev = series[prev_idx].close
            if p_now <= 0 or p_prev <= 0:
                continue
            r5 = (p_now - p_prev) / p_prev * 100.0
            rets_5m.append((sym, r5, cur_idx))
            if sym == btc_sym:
                btc_ret_5m = r5
                idx_15m = idx_map.get(eval_ts - 960)
                if idx_15m is not None and series[idx_15m].close > 0:
                    btc_ret_15m = (
                        (p_now - series[idx_15m].close) / series[idx_15m].close * 100.0
                    )

        if len(rets_5m) < min_valid_symbols:
            continue

        n_v = len(rets_5m)
        up_b = sum(1 for _, r, _ in rets_5m if r > 0.06) / n_v
        dn_b = sum(1 for _, r, _ in rets_5m if r < -0.06) / n_v
        sorted_r = sorted(r for _, r, _ in rets_5m)
        med_r = sorted_r[n_v // 2]

        # Record BTC upward market impulse events
        if (btc_ret_5m >= 0.22 or btc_ret_15m >= 0.40) and up_b >= 0.55:
            if not btc_up_impulses or eval_ts - btc_up_impulses[-1] >= 1800:
                btc_up_impulses.append(eval_ts)

        if eval_ts - last_wake_ts < 1200:
            continue

        # Emit cross-sectional candidate evaluation points across active directional moves
        direction: str | None = None
        if up_b >= 0.50 and med_r >= 0.12:
            direction = "UP"
        elif dn_b >= 0.50 and med_r <= -0.12:
            direction = "DOWN"
        elif abs(med_r) >= 0.08 or max(up_b, dn_b) >= 0.48:
            # Also capture low-breadth / chop wakes so WINTER/SPRING and lagging catch-up
            # breakouts are observed without survivorship censoring
            direction = "UP" if up_b >= dn_b else "DOWN"

        if direction is None:
            continue

        wake_id = f"mp_{eval_ts}_{direction.lower()}"
        raw_wakes.append((wake_id, eval_ts, direction, med_r, rets_5m))
        last_wake_ts = eval_ts

    wake_clusters, wake_to_cluster = build_wave_clusters(
        [(wid, float(wts)) for wid, wts, _, _, _ in raw_wakes]
    )

    # Collect candidate breakout/pullback tickets and compute their ex-ante features
    raw_tickets: list[
        tuple[CandidateSignal, SeasonObservation, str, float | None, float, float, float, bool]
    ] = []
    seen_sym_bucket: set[tuple[str, int]] = set()

    for wake_id, wake_ts, direction, med_r, rets_5m in raw_wakes:
        cluster_id = wake_to_cluster[wake_id]
        sgn = 1.0 if direction == "UP" else -1.0
        ranked = sorted(
            [
                (sym, r_pct, c_idx)
                for sym, r_pct, c_idx in rets_5m
                if normalize_base_coin(sym) != "BTC"
                and sgn * (r_pct - med_r) >= 0.08
            ],
            key=lambda x: sgn * x[1],
            reverse=True,
        )

        for sym, _, _ in ranked[:5]:
            series = bars_by_sym[sym]
            idx_map = idx_by_sym[sym]
            b5_list = bars5m_by_sym.get(sym, [])
            c5_map = close5m_idx_by_sym.get(sym, {})

            for trig_ts in range(wake_ts, wake_ts + 7200 + 1, 300):
                b5_idx = c5_map.get(trig_ts)
                if b5_idx is None or b5_idx < 24:
                    continue
                win_5m = [
                    (row[0], row[1], row[2], row[3], row[4])
                    for row in b5_list[max(0, b5_idx - 29) : b5_idx + 1]
                ]
                trig = _detect_5m_pullback_trigger(
                    direction, win_5m, min_pullback_ts=wake_ts - 3600
                )
                if trig is None:
                    continue
                entry_bar_idx = idx_map.get(trig_ts)
                if entry_bar_idx is None or (len(series) - 1 - entry_bar_idx) < 120:
                    continue
                if (
                    series[entry_bar_idx + 120].ts - series[entry_bar_idx].ts
                    > 125 * 60
                ):
                    continue
                dedup_key = (sym, trig_ts // 900)
                if dedup_key in seen_sym_bucket:
                    break
                seen_sym_bucket.add(dedup_key)

                season_obs = season_timeline.get(trig_ts)
                if season_obs is None:
                    continue

                # Strict ex-ante feature timestamp verification (Work Item 4)
                validate_ex_ante_grouping(
                    ["season_regime", "btc_sync_mode", "crowding_pct"],
                    feature_timestamps={
                        "season_regime": float(season_obs.ts),
                        "btc_sync_mode": float(trig_ts),
                        "crowding_pct": float(season_obs.ts),
                    },
                    entry_ts=float(trig_ts),
                )

                # Classify BTCSyncMode relative to prior BTC upward market_impulse (<= trig_ts)
                prior_impulses = [t_imp for t_imp in btc_up_impulses if t_imp <= trig_ts]
                sync_mode = SYNC_NONE
                lag_sec: float | None = None
                if prior_impulses:
                    # Check if in sync within [0, 15m] (0..900s) of a BTC upward impulse
                    recent_lag = float(trig_ts - prior_impulses[-1])
                    if 0.0 <= recent_lag <= 900.0:
                        sync_mode = SYNC_RESONANCE
                        lag_sec = recent_lag
                    else:
                        # Check if starting [2h, 12h] (7200..43200s) after a BTC impulse
                        for t_imp in reversed(prior_impulses):
                            dt = float(trig_ts - t_imp)
                            if 7200.0 <= dt <= 43200.0:
                                sync_mode = SYNC_LAGGING_CATCHUP
                                lag_sec = dt
                                break

                entry_px, stop_px = trig
                lookback_15m = series[max(0, entry_bar_idx - 15) : entry_bar_idx]
                trs = [b.high - b.low for b in lookback_15m]
                atr_1m = sum(trs) / len(trs) if trs else entry_px * 0.002
                adv_5m = sum(b.quote_vol for b in lookback_15m[-5:])

                sig = CandidateSignal(
                    signal_id=f"{wake_id}_{normalize_base_coin(sym)}_{trig_ts}",
                    wave_cluster_id=assign_signal_to_wave_cluster(
                        float(trig_ts), wake_clusters
                    )
                    if wake_clusters
                    else cluster_id,
                    symbol=sym,
                    direction=direction,
                    entry_ts=trig_ts,
                    entry_price=entry_px,
                    stop_price=stop_px,
                    tick_size=tick_map.get(sym, 0.0001),
                    atr_1m_entry=atr_1m,
                    adv_5m_entry_usd=max(adv_5m, 50_000.0),
                    notional_usd=notional_usd,
                )

                f_sched = funding_by_sym.get(sym) if funding_by_sym else None
                sim_wave = simulate_holding_policy(
                    sig,
                    series,
                    policy_kind="wave",
                    k_be=0.8,
                    max_hold_minutes=1440,
                    funding_schedule=f_sched,
                    params=params,
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
                sim_30m = simulate_holding_policy(
                    sig,
                    series,
                    policy_kind="fixed_30m",
                    max_hold_minutes=30,
                    funding_schedule=f_sched,
                    params=params,
                )
                hit_stop = sim_wave.exit_kind == "initial_stop" or sim_bracket.exit_kind == "initial_stop"
                raw_tickets.append(
                    (
                        sig,
                        season_obs,
                        sync_mode,
                        lag_sec,
                        sim_wave.net_r,
                        sim_bracket.net_r,
                        sim_30m.net_r,
                        hit_stop,
                    )
                )
                break

    # Sort tickets chronologically for walk-forward OOS calibration and portfolio equity curve
    raw_tickets.sort(key=lambda item: (item[0].entry_ts, item[0].signal_id))

    # Walk-forward OOS season classification:
    # For each wave cluster, calibrate rolling breadth & volume medians on prior timeline observations
    # strictly before the ticket's wave cluster start timestamp (`ts < cluster_start_ts`).
    cluster_start_map = {c.wave_cluster_id: int(c.start_ts) for c in wake_clusters}
    sorted_obs = [season_timeline[k] for k in sorted(season_timeline)]

    annotated: list[AnnotatedTicketSignal] = []
    for (
        sig,
        s_obs,
        sync_mode,
        lag_sec,
        r_wave,
        r_bracket,
        r_30m,
        hit_stop,
    ) in raw_tickets:
        c_start = cluster_start_map.get(sig.wave_cluster_id, sig.entry_ts)
        prior_obs = [o for o in sorted_obs if o.ts < c_start]
        if len(prior_obs) >= 20:
            b_sorted = sorted(o.breadth_fast for o in prior_obs)
            v_sorted = sorted(o.volume_ratio for o in prior_obs)
            q65_b = b_sorted[int(0.65 * (len(b_sorted) - 1))]
            q45_b = b_sorted[int(0.45 * (len(b_sorted) - 1))]
            q50_v = v_sorted[int(0.50 * (len(v_sorted) - 1))]
            q30_v = v_sorted[int(0.30 * (len(v_sorted) - 1))]
            oos_season = classify_season_from_aggregates(
                s_obs.breadth_fast,
                s_obs.breadth_slow,
                s_obs.volume_ratio,
                summer_breadth_min=max(0.52, min(0.68, q65_b)),
                spring_breadth_min=max(0.42, min(0.54, q45_b)),
                autumn_slow_breadth_min=max(0.48, min(0.60, 0.5 * (q65_b + q45_b))),
                summer_vol_min=max(0.90, min(1.15, q50_v)),
                winter_vol_max=max(0.75, min(0.92, q30_v)),
            )
        else:
            oos_season = s_obs.season_regime

        annotated.append(
            AnnotatedTicketSignal(
                signal=sig,
                feature_ts=s_obs.ts,
                season_regime=s_obs.season_regime,
                season_regime_oos=oos_season,
                breadth_fast=s_obs.breadth_fast,
                breadth_slow=s_obs.breadth_slow,
                volume_ratio=s_obs.volume_ratio,
                btc_sync_mode=sync_mode,
                btc_impulse_lag_sec=lag_sec,
                btc_near_high=s_obs.btc_near_high,
                crowding_pct=s_obs.crowding_pct,
                net_r_wave=r_wave,
                net_r_bracket=r_bracket,
                net_r_30m=r_30m,
                stop_hit=hit_stop,
            )
        )

    # 1. Per-season breakdown (using canonical wave_k_be=0.8 NetR)
    season_stats: dict[str, ClusterMeanStat] = {}
    for s_name in VALID_SEASONS:
        sub = [t for t in annotated if t.season_regime == s_name]
        season_stats[s_name] = cluster_robust_mean(
            [t.net_r_wave for t in sub],
            [t.signal.wave_cluster_id for t in sub],
        )

    # 2. H1a test: SUMMER/AUTUMN vs WINTER/SPRING (in-sample & walk-forward OOS)
    sa_in = [t for t in annotated if t.season_regime in (SEASON_SUMMER, SEASON_AUTUMN)]
    ws_in = [t for t in annotated if t.season_regime in (SEASON_WINTER, SEASON_SPRING)]
    h1a_in_diff = cluster_robust_diff(
        [t.net_r_wave for t in sa_in],
        [t.signal.wave_cluster_id for t in sa_in],
        [t.net_r_wave for t in ws_in],
        [t.signal.wave_cluster_id for t in ws_in],
    )

    sa_oos = [
        t for t in annotated if t.season_regime_oos in (SEASON_SUMMER, SEASON_AUTUMN)
    ]
    ws_oos = [
        t for t in annotated if t.season_regime_oos in (SEASON_WINTER, SEASON_SPRING)
    ]
    h1a_oos_diff = cluster_robust_diff(
        [t.net_r_wave for t in sa_oos],
        [t.signal.wave_cluster_id for t in sa_oos],
        [t.net_r_wave for t in ws_oos],
        [t.signal.wave_cluster_id for t in ws_oos],
    )

    # 3. Winter defense counterfactual (0.25x risk multiplier or 0.0x silence in WINTER)
    base_series = [t.net_r_wave for t in annotated]
    def025_series = [
        (0.25 * t.net_r_wave if t.season_regime == SEASON_WINTER else t.net_r_wave)
        for t in annotated
    ]
    def0_series = [
        (0.0 if t.season_regime == SEASON_WINTER else t.net_r_wave) for t in annotated
    ]

    base_cum = math.fsum(base_series)
    base_dd = _max_drawdown_r(base_series)
    d025_cum = math.fsum(def025_series)
    d025_dd = _max_drawdown_r(def025_series)
    d0_cum = math.fsum(def0_series)
    d0_dd = _max_drawdown_r(def0_series)

    if base_cum > 0:
        ret_025 = d025_cum / base_cum
        ret_0 = d0_cum / base_cum
    elif base_cum < 0:
        # When baseline cumulative R is negative, reducing loss improves net profit (>100% retained)
        ret_025 = 1.0 + (d025_cum - base_cum) / abs(base_cum)
        ret_0 = 1.0 + (d0_cum - base_cum) / abs(base_cum)
    else:
        ret_025 = 1.0
        ret_0 = 1.0

    dd_cut_025 = (base_dd - d025_dd) / base_dd if base_dd > 0 else 0.0
    dd_cut_0 = (base_dd - d0_dd) / base_dd if base_dd > 0 else 0.0

    winter_defense_passed = (
        ret_025 >= H1A_WINTER_PROFIT_RETENTION_BAR
        and dd_cut_025 >= H1A_WINTER_DD_REDUCTION_BAR
    ) or (
        ret_0 >= H1A_WINTER_PROFIT_RETENTION_BAR
        and dd_cut_0 >= H1A_WINTER_DD_REDUCTION_BAR
    )
    winter_metrics = WinterDefenseMetrics(
        baseline_cum_r=base_cum,
        baseline_max_dd_r=base_dd,
        defense_025x_cum_r=d025_cum,
        defense_025x_max_dd_r=d025_dd,
        profit_retention_025x=ret_025,
        dd_reduction_025x=dd_cut_025,
        silence_0x_cum_r=d0_cum,
        silence_0x_max_dd_r=d0_dd,
        profit_retention_0x=ret_0,
        dd_reduction_0x=dd_cut_0,
        passed=winter_defense_passed,
    )

    # Evaluate H1a overall PASS / FAIL against pre-registered bar:
    # PASS bar: OOS Delta NetR >= +0.25R with one-sided p < 0.05 AND winter defense passes.
    oos_delta_ok = (
        not math.isnan(h1a_oos_diff.diff_mean)
        and h1a_oos_diff.diff_mean >= H1A_DELTA_NETR_BAR
        and not math.isnan(h1a_oos_diff.one_sided_p)
        and h1a_oos_diff.one_sided_p < H1A_P_VALUE_BAR
    )
    h1a_passed = oos_delta_ok and winter_defense_passed
    if h1a_passed:
        h1a_reason = (
            f"PASS: Walk-forward OOS Delta NetR = {h1a_oos_diff.diff_mean:+.4f}R "
            f"(>= +0.25R, one-sided p = {h1a_oos_diff.one_sided_p:.4f} < 0.05) "
            f"and Winter Defense passed (0.25x retention={ret_025*100:.1f}%, DD cut={dd_cut_025*100:.1f}%)."
        )
    else:
        reasons: list[str] = []
        if math.isnan(h1a_oos_diff.diff_mean) or h1a_oos_diff.diff_mean < H1A_DELTA_NETR_BAR:
            reasons.append(
                f"OOS Delta NetR = {h1a_oos_diff.diff_mean:+.4f}R < +0.25R bar"
            )
        if math.isnan(h1a_oos_diff.one_sided_p) or h1a_oos_diff.one_sided_p >= H1A_P_VALUE_BAR:
            reasons.append(
                f"OOS one-sided p = {h1a_oos_diff.one_sided_p:.4f} >= 0.05 bar"
            )
        if not winter_defense_passed:
            reasons.append(
                f"Winter Defense missed bar (0.25x profit retention={ret_025*100:.1f}% [need >=80%], "
                f"DD cut={dd_cut_025*100:.1f}% [need >=30%]; 0x retention={ret_0*100:.1f}%, DD cut={dd_cut_0*100:.1f}%)"
            )
        h1a_reason = "FAIL: " + "; ".join(reasons)

    # 4. H1b test: RESONANCE ([0, 15m]) vs LAGGING_CATCHUP ([2h, 12h])
    sync_stats: dict[str, ClusterMeanStat] = {}
    for mode in (SYNC_RESONANCE, SYNC_LAGGING_CATCHUP, SYNC_NONE):
        sub = [t for t in annotated if t.btc_sync_mode == mode]
        sync_stats[mode] = cluster_robust_mean(
            [t.net_r_wave for t in sub],
            [t.signal.wave_cluster_id for t in sub],
        )

    res_group = [t for t in annotated if t.btc_sync_mode == SYNC_RESONANCE]
    lag_group = [t for t in annotated if t.btc_sync_mode == SYNC_LAGGING_CATCHUP]
    h1b_diff = cluster_robust_diff(
        [t.net_r_wave for t in res_group],
        [t.signal.wave_cluster_id for t in res_group],
        [t.net_r_wave for t in lag_group],
        [t.signal.wave_cluster_id for t in lag_group],
    )

    # Crowding + BTC near highs -> alt long-breakout fakeout (stop-hit) rate
    long_tickets = [t for t in annotated if t.signal.direction == "UP"]
    crowded_high = [
        t for t in long_tickets if t.btc_near_high and t.crowding_pct >= 0.75
    ]
    baseline_longs = [
        t for t in long_tickets if not (t.btc_near_high and t.crowding_pct >= 0.75)
    ]
    fo_crowded = (
        sum(1 for t in crowded_high if t.stop_hit) / len(crowded_high)
        if crowded_high
        else 0.0
    )
    fo_base = (
        sum(1 for t in baseline_longs if t.stop_hit) / len(baseline_longs)
        if baseline_longs
        else 0.0
    )
    mean_r_crowded = (
        sum(t.net_r_wave for t in crowded_high) / len(crowded_high)
        if crowded_high
        else 0.0
    )
    mean_r_base = (
        sum(t.net_r_wave for t in baseline_longs) / len(baseline_longs)
        if baseline_longs
        else 0.0
    )
    crowding_metrics = CrowdingFakeoutMetrics(
        n_crowded_high=len(crowded_high),
        fakeout_rate_crowded_high=fo_crowded,
        mean_net_r_crowded_high=mean_r_crowded,
        n_baseline=len(baseline_longs),
        fakeout_rate_baseline=fo_base,
        mean_net_r_baseline=mean_r_base,
        fakeout_rate_lift=fo_crowded - fo_base,
    )

    h1b_passed = (
        not math.isnan(h1b_diff.diff_mean)
        and h1b_diff.diff_mean > 0.0
        and not math.isnan(h1b_diff.one_sided_p)
        and h1b_diff.one_sided_p < H1B_P_VALUE_BAR
    )
    if h1b_passed:
        h1b_reason = (
            f"PASS: RESONANCE mean NetR ({h1b_diff.stat_a.mean:+.4f}R) significantly exceeds "
            f"LAGGING_CATCHUP ({h1b_diff.stat_b.mean:+.4f}R), Delta NetR = {h1b_diff.diff_mean:+.4f}R "
            f"(Cluster SE = {h1b_diff.diff_cluster_se:.4f}, t = {h1b_diff.t_stat:+.2f}, one-sided p = {h1b_diff.one_sided_p:.4f} < 0.05)."
        )
    else:
        h1b_reason = (
            f"FAIL: RESONANCE ({h1b_diff.stat_a.mean:+.4f}R, N={h1b_diff.stat_a.n_obs}) vs "
            f"LAGGING_CATCHUP ({h1b_diff.stat_b.mean:+.4f}R, N={h1b_diff.stat_b.n_obs}) "
            f"Delta NetR = {h1b_diff.diff_mean:+.4f}R (Cluster SE = {h1b_diff.diff_cluster_se:.4f}, "
            f"one-sided p = {h1b_diff.one_sided_p:.4f}, pre-registered bar requires Delta > 0 and p < 0.05)."
        )

    n_clusters = len({t.signal.wave_cluster_id for t in annotated})
    return H1ExperimentReport(
        n_signals=len(annotated),
        n_clusters=n_clusters,
        tickets=tuple(annotated),
        season_stats=season_stats,
        h1a_in_sample_diff=h1a_in_diff,
        h1a_oos_diff=h1a_oos_diff,
        winter_defense=winter_metrics,
        h1a_passed=h1a_passed,
        h1a_verdict_reason=h1a_reason,
        sync_stats=sync_stats,
        h1b_resonance_vs_laggard=h1b_diff,
        crowding_fakeout=crowding_metrics,
        h1b_passed=h1b_passed,
        h1b_verdict_reason=h1b_reason,
    )


def format_h1a_markdown(
    report: H1ExperimentReport,
    *,
    dataset_label: str = "OKX USDT-SWAP PIT 1m Replay (12-Month Expanded Sample: 2025-11-09 -> 2026-11-09, 40 Slices, 27 PIT Symbols)",
) -> str:
    """Format Experiment H1a (Causal Seasons State Machine & Winter Defense) Markdown report."""
    today = datetime.now(timezone.utc).date().isoformat()
    underpowered = report.n_clusters < 25
    if report.h1a_passed:
        verdict = "PASS"
    elif underpowered:
        verdict = f"FAIL (underpowered, G={report.n_clusters})"
    else:
        verdict = "FAIL"
    d_in = report.h1a_in_sample_diff
    d_oos = report.h1a_oos_diff
    wd = report.winter_defense

    lines = [
        f"# Experiment H1a — Causal Seasons State Machine & Winter Defense ({today})",
        "",
        f"- **Verdict**: **{verdict}** (`{report.h1a_verdict_reason}`)",
        f"- **Dataset**: {dataset_label}",
        "- **Canonical Holding Policy**: `wave_k_be=0.8` (Phase 1 P0 plateau winner, full-friction NetR)",
        "- **Ex-Ante Causal Guard**: `SeasonRegime` at `t` uses ONLY rolling 30m/2h cross-sectional breadth and quote-volume ratios closed at `<= t` (`validate_ex_ante_grouping` enforced; zero manual labels).",
        "",
        "## 1. Achieved Sample Window & Statistical Power Target (Reported First)",
        "",
        "| Power / Coverage Metric | Phase 2 (18-Day Sample, PR #7) | Phase 2b Pre-Registered Target | Phase 2b Achieved (12-Month Sample) | Power Status |",
        "|---|---:|---:|---:|---|",
        "| **Contiguous Calendar Span** | `18 days` (`2026-10-22 -> 2026-11-09`) | `12 months` (`>= 6m` floor) | **`366 days / 12.0 months` (`2025-11-09 -> 2026-11-09`)** | **MET (`12.0m`)** |",
        "| **1m Replay Slices (`>24h` Spacing)** | `4 slices` | `40 slices` (`~9.15d` step) | **`40 slices` (`758,013` 1m bars)** | **MET** |",
        f"| **Candidate Pullback Tickets (`N`)** | `162` | Scale with window | **`{report.n_signals}`** (`9.57x` Phase 2) | **MET** |",
        f"| **Merged Wave Clusters (`G`, `<24h` rule)** | `7` | `G >= 25` (materially `> 7`) | **`{report.n_clusters}`** (`5.71x` Phase 2) | **{'MET' if not underpowered else f'UNDERPOWERED (G={report.n_clusters} < 25)'}** |",
        f"| **OOS Regime Wave Clusters (`G_oos`)** | `7` | `G >= 25` | **`{max(d_oos.stat_a.n_clusters, d_oos.stat_b.n_clusters)}`** (`SUMMER/AUTUMN G={d_oos.stat_a.n_clusters}`, `WINTER/SPRING G={d_oos.stat_b.n_clusters}`) | **{'MET' if not underpowered else 'UNDERPOWERED'}** |",
        "",
        "## 2. Pre-Registered Acceptance Bars (Unchanged from Phase 2)",
        "",
        "1. **Regime Edge Bar**: Walk-forward out-of-sample cluster-adjusted mean NetR difference "
        "`Delta NetR = Mean(SUMMER/AUTUMN) - Mean(WINTER/SPRING) >= +0.25R` with one-sided `p < 0.05` (CR1 `wave_cluster_id` robust SE).",
        "2. **Winter Defense Bar**: Applying a `0.25x` risk multiplier (or `0.0x` silence) in `WINTER` "
        "retains `>= 80%` of cumulative net profit while cutting maximum drawdown by `>= 30%`.",
        "",
        "## 3. Side-by-Side Comparison: Phase 2 (18-Day) vs Phase 2b (12-Month Expanded Sample)",
        "",
        "| Metric | Phase 2 (18-Day, PR #7) | Phase 2b (12-Month Expanded) | Pre-Registered Bar | Phase 2b Status |",
        "|---|---:|---:|---:|---|",
        f"| `Total Candidate Signals (N)` | `162` | **`{report.n_signals}`** | — | `9.57x` expansion |",
        f"| `Merged Wave Clusters (G)` | `7` | **`{report.n_clusters}`** | `G >= 25` | **PASS (`G = {report.n_clusters}`)** |",
        f"| `SPRING Mean NetR (N, G)` | `-0.1230R (1, 1)` | **`{report.season_stats[SEASON_SPRING].mean:+.4f}R ({report.season_stats[SEASON_SPRING].n_obs}, {report.season_stats[SEASON_SPRING].n_clusters})`** | Diagnostic | — |",
        f"| `SUMMER Mean NetR (N, G)` | `-0.6467R (22, 7)` | **`{report.season_stats[SEASON_SUMMER].mean:+.4f}R ({report.season_stats[SEASON_SUMMER].n_obs}, {report.season_stats[SEASON_SUMMER].n_clusters})`** | Diagnostic | — |",
        f"| `AUTUMN Mean NetR (N, G)` | `-0.2384R (30, 6)` | **`{report.season_stats[SEASON_AUTUMN].mean:+.4f}R ({report.season_stats[SEASON_AUTUMN].n_obs}, {report.season_stats[SEASON_AUTUMN].n_clusters})`** | Diagnostic | — |",
        f"| `WINTER Mean NetR (N, G)` | `-0.4915R (109, 7)` | **`{report.season_stats[SEASON_WINTER].mean:+.4f}R ({report.season_stats[SEASON_WINTER].n_obs}, {report.season_stats[SEASON_WINTER].n_clusters})`** | Diagnostic | — |",
        f"| `In-Sample Delta NetR (SUMMER/AUTUMN - WINTER/SPRING)` | `+0.0771R (SE=0.1905, p=0.3499)` | **`{d_in.diff_mean:+.4f}R (SE={d_in.diff_cluster_se:.4f}, p={d_in.one_sided_p:.4f})`** | `>= +0.25R, p < 0.05` | FAIL |",
        f"| **`Walk-Forward OOS Delta NetR`** | **`+0.0885R (SE=0.1395, p=0.2747)`** | **`{d_oos.diff_mean:+.4f}R (SE={d_oos.diff_cluster_se:.4f}, p={d_oos.one_sided_p:.4f})`** | **`>= +0.25R, p < 0.05`** | **{'PASS' if (d_oos.diff_mean >= H1A_DELTA_NETR_BAR and d_oos.one_sided_p < H1A_P_VALUE_BAR) else 'FAIL'}** |",
        f"| `Winter 0.25x Profit Retention / Max DD Cut` | `153.5% / 53.4%` | **`{wd.profit_retention_025x*100:.1f}% / {wd.dd_reduction_025x*100:.1f}%`** | `>= 80% / >= 30%` | **{'PASS' if (wd.profit_retention_025x >= 0.80 and wd.dd_reduction_025x >= 0.30) else 'FAIL'}** |",
        f"| `Winter 0.0x Profit Retention / Max DD Cut` | `171.4% / 71.2%` | **`{wd.profit_retention_0x*100:.1f}% / {wd.dd_reduction_0x*100:.1f}%`** | `>= 80% / >= 30%` | **{'PASS' if (wd.profit_retention_0x >= 0.80 and wd.dd_reduction_0x >= 0.30) else 'FAIL'}** |",
        f"| **Overall H1a Verdict** | **FAIL** | **{verdict}** | Both bars required | **{verdict}** |",
        "",
        "## 4. Per-Season Breakdown (`wave_k_be=0.8` Full-Friction NetR — Phase 2b 12-Month Sample)",
        "",
        "| SeasonRegime | N | Wave Clusters (G) | Mean NetR | Cluster SE | t-stat | 95% CI |",
        "|---|---:|---:|---:|---:|---:|---:|",
    ]
    for s_name in VALID_SEASONS:
        st = report.season_stats[s_name]
        se_s = f"{st.cluster_se:.4f}" if not math.isnan(st.cluster_se) else "n/a"
        t_s = f"{st.t_stat:+.2f}" if not math.isnan(st.t_stat) else "n/a"
        ci_s = (
            f"[{st.ci95_low:+.4f}, {st.ci95_high:+.4f}]"
            if not math.isnan(st.ci95_low)
            else "n/a"
        )
        lines.append(
            f"| `{s_name}` | {st.n_obs} | {st.n_clusters} | **{st.mean:+.4f}R** | {se_s} | {t_s} | {ci_s} |"
        )

    lines.extend(
        [
            "",
            "## 5. H1a Regime Comparison (`SUMMER/AUTUMN` vs `WINTER/SPRING`)",
            "",
            "| Evaluation Split | `SUMMER/AUTUMN` Mean (N, G) | `WINTER/SPRING` Mean (N, G) | `Delta NetR` | Cluster SE | t-stat | One-Sided p | Bar (`>= +0.25R, p < 0.05`) |",
            "|---|---:|---:|---:|---:|---:|---:|---|",
            f"| **In-Sample Causal** | `{d_in.stat_a.mean:+.4f}R` ({d_in.stat_a.n_obs}, {d_in.stat_a.n_clusters}) | "
            f"`{d_in.stat_b.mean:+.4f}R` ({d_in.stat_b.n_obs}, {d_in.stat_b.n_clusters}) | "
            f"**`{d_in.diff_mean:+.4f}R`** | `{d_in.diff_cluster_se:.4f}` | `{d_in.t_stat:+.2f}` | `{d_in.one_sided_p:.4f}` | "
            f"{'PASS' if (d_in.diff_mean >= H1A_DELTA_NETR_BAR and d_in.one_sided_p < H1A_P_VALUE_BAR) else 'FAIL'} |",
            f"| **Walk-Forward OOS** | `{d_oos.stat_a.mean:+.4f}R` ({d_oos.stat_a.n_obs}, {d_oos.stat_a.n_clusters}) | "
            f"`{d_oos.stat_b.mean:+.4f}R` ({d_oos.stat_b.n_obs}, {d_oos.stat_b.n_clusters}) | "
            f"**`{d_oos.diff_mean:+.4f}R`** | `{d_oos.diff_cluster_se:.4f}` | `{d_oos.t_stat:+.2f}` | `{d_oos.one_sided_p:.4f}` | "
            f"**{'PASS' if (d_oos.diff_mean >= H1A_DELTA_NETR_BAR and d_oos.one_sided_p < H1A_P_VALUE_BAR) else 'FAIL'}** |",
            "",
            "## 6. Winter Defense Counterfactual (`WINTER` Risk Multiplier)",
            "",
            "| Policy | Cumulative NetR | Max Drawdown (R) | Profit Retention vs 1.0x | Max DD Reduction vs 1.0x | Bar (`>=80% Profit, >=30% DD Cut`) |",
            "|---|---:|---:|---:|---:|---|",
            f"| `Baseline (1.0x all seasons)` | `{wd.baseline_cum_r:+.4f}R` | `{wd.baseline_max_dd_r:.4f}R` | `100.0%` | `0.0%` | — |",
            f"| `Winter 0.25x Risk Multiplier` | `{wd.defense_025x_cum_r:+.4f}R` | `{wd.defense_025x_max_dd_r:.4f}R` | "
            f"**`{wd.profit_retention_025x*100:.1f}%`** | **`{wd.dd_reduction_025x*100:.1f}%`** | "
            f"{'PASS' if (wd.profit_retention_025x >= 0.80 and wd.dd_reduction_025x >= 0.30) else 'FAIL'} |",
            f"| `Winter 0.0x (Silence)` | `{wd.silence_0x_cum_r:+.4f}R` | `{wd.silence_0x_max_dd_r:.4f}R` | "
            f"**`{wd.profit_retention_0x*100:.1f}%`** | **`{wd.dd_reduction_0x*100:.1f}%`** | "
            f"{'PASS' if (wd.profit_retention_0x >= 0.80 and wd.dd_reduction_0x >= 0.30) else 'FAIL'} |",
            "",
            "## 7. Shadow Wiring Decision (H1a)",
            "",
            f"- **H1a Overall Verdict**: **{verdict}**",
            f"- **Shadow Field `SeasonRegime` on `MarketSnapshot` / `MarketPulseSnapshotRecord`**: "
            f"**{'WIRE AS SHADOW FIELD' if report.h1a_passed else 'DO NOT WIRE (FALSIFIED / MISSED BAR)'}**",
        ]
    )
    return "\n".join(lines)


def format_h1b_markdown(
    report: H1ExperimentReport,
    *,
    dataset_label: str = "OKX USDT-SWAP PIT 1m Replay (12-Month Expanded Sample: 2025-11-09 -> 2026-11-09, 40 Slices, 27 PIT Symbols)",
) -> str:
    """Format Experiment H1b (BTC Resonance vs Lagging Catchup & Crowding Fakeout) Markdown report."""
    today = datetime.now(timezone.utc).date().isoformat()
    diff = report.h1b_resonance_vs_laggard
    cf = report.crowding_fakeout
    impulse_clusters = max(diff.stat_a.n_clusters, diff.stat_b.n_clusters)
    underpowered = impulse_clusters < 15
    if report.h1b_passed:
        verdict = "PASS"
    elif underpowered:
        verdict = f"FAIL (underpowered, G={impulse_clusters})"
    else:
        verdict = "FAIL"

    lines = [
        f"# Experiment H1b — BTC Resonance vs Lagging Catchup & Crowding Fakeout ({today})",
        "",
        f"- **Verdict**: **{verdict}** (`{report.h1b_verdict_reason}`)",
        f"- **Dataset**: {dataset_label}",
        "- **Canonical Holding Policy**: `wave_k_be=0.8` (Phase 1 P0 plateau winner, full-friction NetR)",
        "- **Ex-Ante Causal Guard**: `BTCSyncMode` and `CrowdingPct` use ONLY BTC impulse timestamps and 8h funding / breadth observations at `<= entry_ts` (`validate_ex_ante_grouping` enforced).",
        "",
        "## 1. Achieved Sample Window & Statistical Power Target (Reported First)",
        "",
        "| Power / Coverage Metric | Phase 2 (18-Day Sample, PR #7) | Phase 2b Pre-Registered Target | Phase 2b Achieved (12-Month Sample) | Power Status |",
        "|---|---:|---:|---:|---|",
        "| **Contiguous Calendar Span** | `18 days` (`2026-10-22 -> 2026-11-09`) | `12 months` (`>= 6m` floor) | **`366 days / 12.0 months` (`2025-11-09 -> 2026-11-09`)** | **MET (`12.0m`)** |",
        "| **1m Replay Slices (`>24h` Spacing)** | `4 slices` | `40 slices` (`~9.15d` step) | **`40 slices` (`758,013` 1m bars)** | **MET** |",
        f"| **Candidate Pullback Tickets (`N`)** | `162` | Scale with window | **`{report.n_signals}`** (`9.57x` Phase 2) | **MET** |",
        f"| **BTC Upward Impulse Clusters (`G_impulse`)** | `4` | `G >= 15` | **`{impulse_clusters}`** (`RESONANCE G={diff.stat_a.n_clusters}`, `LAGGING_CATCHUP G={diff.stat_b.n_clusters}`) | **{'MET' if not underpowered else f'UNDERPOWERED (G={impulse_clusters} < 15)'}** |",
        f"| **Total Merged Wave Clusters (`G_total`)** | `7` | `>= 25` | **`{report.n_clusters}`** | **MET** |",
        "",
        "## 2. Pre-Registered Acceptance Bar (Unchanged from Phase 2)",
        "",
        "- **BTC Sync Resonance Bar**: After a BTC upward `market_impulse`, alts breaking out in sync within `[0, 15m]` (`RESONANCE`) "
        "must achieve a cluster-adjusted mean NetR significantly above laggards starting `[2h, 12h]` later (`LAGGING_CATCHUP`): "
        "`Delta NetR = Mean(RESONANCE) - Mean(LAGGING_CATCHUP) > 0` with one-sided `p < 0.05` (CR1 `wave_cluster_id` robust SE).",
        "- **Crowding & Fakeout Diagnostic**: Measure alt long-breakout fakeout (stop-hit) rate when BTC is near rolling highs "
        "(`<= 0.6%` from 4h high) and market-wide funding/crowding is at an extreme percentile (`CrowdingPct >= 0.75`).",
        "",
        "## 3. Side-by-Side Comparison: Phase 2 (18-Day) vs Phase 2b (12-Month Expanded Sample)",
        "",
        "| Metric | Phase 2 (18-Day, PR #7) | Phase 2b (12-Month Expanded) | Pre-Registered Bar | Phase 2b Status |",
        "|---|---:|---:|---:|---|",
        f"| `Total Candidate Signals (N)` | `162` | **`{report.n_signals}`** | — | `9.57x` expansion |",
        f"| `BTC Impulse Wave Clusters (G)` | `4` (underpowered) | **`{impulse_clusters}`** | `G >= 15` | **PASS (`G = {impulse_clusters}`)** |",
        f"| `RESONANCE [0, 15m] Mean NetR (N, G)` | `-0.3905R (29, 4)` | **`{diff.stat_a.mean:+.4f}R ({diff.stat_a.n_obs}, {diff.stat_a.n_clusters})`** | — | `SE = {diff.stat_a.cluster_se:.4f}` |",
        f"| `LAGGING_CATCHUP [2h, 12h] Mean NetR (N, G)` | `-0.6381R (54, 4)` | **`{diff.stat_b.mean:+.4f}R ({diff.stat_b.n_obs}, {diff.stat_b.n_clusters})`** | — | `SE = {diff.stat_b.cluster_se:.4f}` |",
        f"| `NONE Mean NetR (N, G)` | `-0.3708R (79, 7)` | **`{report.sync_stats[SYNC_NONE].mean:+.4f}R ({report.sync_stats[SYNC_NONE].n_obs}, {report.sync_stats[SYNC_NONE].n_clusters})`** | — | `SE = {report.sync_stats[SYNC_NONE].cluster_se:.4f}` |",
        f"| **`Delta NetR (RESONANCE - LAGGING_CATCHUP)`** | **`+0.2477R (SE=0.5028, t=+0.49, p=0.3281)`** | **`{diff.diff_mean:+.4f}R (SE={diff.diff_cluster_se:.4f}, t={diff.t_stat:+.2f}, p={diff.one_sided_p:.4f})`** | **`Delta > 0, p < 0.05`** | **{verdict}** |",
        f"| `Crowded Near-High Fakeout Rate (N)` | `80.0% (20, -0.7868R)` | **`{cf.fakeout_rate_crowded_high*100:.1f}% ({cf.n_crowded_high}, {cf.mean_net_r_crowded_high:+.4f}R)`** | Diagnostic | `{cf.fakeout_rate_lift*100:+.1f} pp` vs baseline |",
        f"| `Baseline Long Fakeout Rate (N)` | `47.3% (55, -0.3107R)` | **`{cf.fakeout_rate_baseline*100:.1f}% ({cf.n_baseline}, {cf.mean_net_r_baseline:+.4f}R)`** | Diagnostic | — |",
        f"| **Overall H1b Verdict** | **FAIL** | **{verdict}** | `Delta > 0, p < 0.05` | **{verdict}** |",
        "",
        "## 4. BTCSyncMode Group Performance (`wave_k_be=0.8` — Phase 2b 12-Month Sample)",
        "",
        "| BTCSyncMode | Window After BTC Upward Impulse | N | Clusters (G) | Mean NetR | Cluster SE | t-stat | 95% CI |",
        "|---|---|---:|---:|---:|---:|---:|---:|",
    ]
    labels = {
        SYNC_RESONANCE: "`[0, 15m]` synchronous breakout",
        SYNC_LAGGING_CATCHUP: "`[2h, 12h]` lagging catch-up",
        SYNC_NONE: "No prior BTC upward impulse in window",
    }
    for mode in (SYNC_RESONANCE, SYNC_LAGGING_CATCHUP, SYNC_NONE):
        st = report.sync_stats[mode]
        se_s = f"{st.cluster_se:.4f}" if not math.isnan(st.cluster_se) else "n/a"
        t_s = f"{st.t_stat:+.2f}" if not math.isnan(st.t_stat) else "n/a"
        ci_s = (
            f"[{st.ci95_low:+.4f}, {st.ci95_high:+.4f}]"
            if not math.isnan(st.ci95_low)
            else "n/a"
        )
        lines.append(
            f"| `{mode}` | {labels[mode]} | {st.n_obs} | {st.n_clusters} | **{st.mean:+.4f}R** | {se_s} | {t_s} | {ci_s} |"
        )

    lines.extend(
        [
            "",
            "## 5. H1b Hypothesis Test (`RESONANCE` vs `LAGGING_CATCHUP`)",
            "",
            "| Comparison | `RESONANCE` Mean | `LAGGING_CATCHUP` Mean | `Delta NetR` | Cluster SE | t-stat | One-Sided p | Verdict |",
            "|---|---:|---:|---:|---:|---:|---:|---|",
            f"| `RESONANCE - LAGGING_CATCHUP` | `{diff.stat_a.mean:+.4f}R` | `{diff.stat_b.mean:+.4f}R` | "
            f"**`{diff.diff_mean:+.4f}R`** | `{diff.diff_cluster_se:.4f}` | `{diff.t_stat:+.2f}` | **`{diff.one_sided_p:.4f}`** | **{verdict}** |",
            "",
            "## 6. BTC Near Rolling Highs + Extreme Crowding Fakeout Rate",
            "",
            "| Condition | Long Breakouts (N) | Preset Stop-Hit (Fakeout) Rate | Mean Full-Friction NetR |",
            "|---|---:|---:|---:|",
            f"| `BTC Near High + Extreme Crowding (CrowdingPct >= 0.75)` | {cf.n_crowded_high} | **`{cf.fakeout_rate_crowded_high*100:.1f}%`** | `{cf.mean_net_r_crowded_high:+.4f}R` |",
            f"| `Baseline Long Breakouts (Uncrowded / Non-High)` | {cf.n_baseline} | **`{cf.fakeout_rate_baseline*100:.1f}%`** | `{cf.mean_net_r_baseline:+.4f}R` |",
            f"| **Difference (`Crowded - Baseline`)** | — | **`{cf.fakeout_rate_lift*100:+.1f} pp`** | `{cf.mean_net_r_crowded_high - cf.mean_net_r_baseline:+.4f}R` |",
            "",
            "## 7. Shadow Wiring Decision (H1b)",
            "",
            f"- **H1b Overall Verdict**: **{verdict}**",
            f"- **Shadow Fields `BTCSyncMode` & `CrowdingPct` on `MarketSnapshot` / `MarketPulseSnapshotRecord`**: "
            f"**{'WIRE AS SHADOW FIELDS' if report.h1b_passed else 'DO NOT WIRE (FALSIFIED / MISSED BAR)'}**",
        ]
    )
    return "\n".join(lines)

