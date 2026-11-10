"""Path-dependent NetR holding policy simulator & protective-stop counterfactual (Work Items 4, 5, 6).

Implements causal 1-minute bar replay with:
- Strict intra-bar worst-case rule: if one 1m bar touches BOTH active stop and
  target (or breakeven arming level), settle as `stop-first`.
- Next-minute-open breakeven activation: when confirming minute closes at >= k_be R,
  the fee-inclusive breakeven stop takes effect at the NEXT minute open.
- Causal 1h / 4h higher-low (long) / lower-high (short) swing structure trailing
  using ONLY bars whose close_ts <= current minute open_ts.
- Flat parameter plateau selection across k_be in {0.8, 1.2, 1.5, 2.0, inf}.
- Protective-stop counterfactual ("saved R" = NetR_actual - NetR_hold_to_5m) for
  <5m preset-stop / fakeout exits.
"""
from __future__ import annotations

import math
from dataclasses import dataclass
from typing import Mapping, Sequence

from clustering import ClusterMeanStat, cluster_robust_mean
from friction import (
    CostBreakdown,
    FrictionParams,
    direction_sign,
    evaluate_trade_net_r,
    limit_order_fills,
)

K_BE_GRID: tuple[float, ...] = (0.8, 1.2, 1.5, 2.0, math.inf)


@dataclass(frozen=True)
class MinuteBar:
    """One 1-minute OHLCV bar (`ts` = bar open Unix seconds; closes at `ts + 60`)."""

    ts: int
    open: float
    high: float
    low: float
    close: float
    quote_vol: float = 0.0


@dataclass(frozen=True)
class CandidateSignal:
    """Ex-ante candidate signal frozen at `entry_ts` (`t <= entry_ts`)."""

    signal_id: str
    wave_cluster_id: str
    symbol: str
    direction: str  # "UP" or "DOWN"
    entry_ts: int
    entry_price: float
    stop_price: float
    tick_size: float = 0.01
    atr_1m_entry: float = 0.0
    adv_5m_entry_usd: float = 0.0
    notional_usd: float = 10_000.0


@dataclass(frozen=True)
class PolicySimResult:
    """Outcome of simulating one holding policy on one candidate signal."""

    policy_name: str
    signal_id: str
    wave_cluster_id: str
    symbol: str
    exit_ts: int
    exit_price: float
    exit_kind: str  # "initial_stop" | "breakeven_stop" | "trail_stop" | "target" | "time_exit"
    hold_seconds: int
    cost: CostBreakdown

    @property
    def net_r(self) -> float:
        return self.cost.net_r


@dataclass(frozen=True)
class PlateauSelection:
    """Result of evaluating the k_be grid for a flat parameter plateau."""

    stats_by_k: dict[float, ClusterMeanStat]
    winner_k: float
    winner_stat: ClusterMeanStat
    is_plateau: bool
    reason: str


@dataclass(frozen=True)
class ShortExitRecord:
    """Historical Journal <5m exit input for protective-stop counterfactual (Work Item 6)."""

    ticket_id: str
    wave_cluster_id: str
    symbol: str
    direction: str
    entry_ts: int
    exit_ts: int
    entry_price: float
    stop_price: float
    actual_exit_price: float
    hit_preset_stop: bool
    fakeout_confirmed: bool
    bars_1m: Sequence[MinuteBar]
    atr_1m_entry: float = 0.0
    adv_5m_entry_usd: float = 0.0
    notional_usd: float = 10_000.0
    funding_schedule: Mapping[int, float] | None = None


@dataclass(frozen=True)
class SavedRRow:
    """Per-trade counterfactual comparison for a <5m protective stop / fakeout exit."""

    ticket_id: str
    wave_cluster_id: str
    symbol: str
    hold_seconds: int
    net_r_actual: float
    net_r_hold_5m: float
    saved_r: float


@dataclass(frozen=True)
class ProtectiveStopCounterfactualReport:
    """Aggregate saved-R evidence report for Work Item 6."""

    rows: tuple[SavedRRow, ...]
    actual_stat: ClusterMeanStat
    hold_5m_stat: ClusterMeanStat
    saved_r_stat: ClusterMeanStat


def _aggregate_htf_bars(
    bars_1m: Sequence[MinuteBar], period_sec: int
) -> list[tuple[int, float, float, float, float]]:
    """Aggregate 1m bars into closed higher-timeframe bars `(close_ts, open, high, low, close)`."""
    if not bars_1m:
        return []
    buckets: dict[int, list[MinuteBar]] = {}
    for b in bars_1m:
        b_open = (b.ts // period_sec) * period_sec
        buckets.setdefault(b_open, []).append(b)
    out: list[tuple[int, float, float, float, float]] = []
    for b_open in sorted(buckets):
        grp = buckets[b_open]
        # Only emit complete buckets or buckets whose window end <= last 1m close
        close_ts = b_open + period_sec
        if grp[-1].ts + 60 < close_ts:
            continue
        o = grp[0].open
        h = max(x.high for x in grp)
        l = min(x.low for x in grp)
        c = grp[-1].close
        out.append((close_ts, o, h, l, c))
    return out


def _build_causal_structure_trail_schedule(
    bars_1m: Sequence[MinuteBar],
    direction: str,
    entry_price: float,
    initial_stop: float,
) -> dict[int, float]:
    """Precompute causal 1h/4h swing higher-low (long) / lower-high (short) trail updates.

    A 3-bar swing pivot at index `k` (`bars[k-1], bars[k], bars[k+1]`) is ONLY known
    once `bars[k+1]` has closed at `close_ts(k+1)`. Therefore it can tighten `active_stop`
    only for 1m bars with `open_ts >= close_ts(k+1)` (strictly zero lookahead).
    """
    sgn = direction_sign(direction)
    r_price = abs(entry_price - initial_stop)
    updates: dict[int, float] = {}

    for period_sec in (900, 3600, 14400):  # 15m / 1h / 4h swing structure
        htf = _aggregate_htf_bars(bars_1m, period_sec)
        if len(htf) < 3:
            continue
        prior_pivot: float | None = initial_stop
        max_fav_seen = entry_price
        for k in range(1, len(htf) - 1):
            _, _, prev_h, prev_l, _ = htf[k - 1]
            _, _, cur_h, cur_l, _ = htf[k]
            confirm_close_ts, _, next_h, next_l, _ = htf[k + 1]

            if sgn > 0:
                max_fav_seen = max(max_fav_seen, prev_h, cur_h, next_h)
                is_pivot_low = cur_l <= prev_l and cur_l <= next_l
                # Require wave expansion (>= +1R move seen) and a higher low above prior structure
                if (
                    is_pivot_low
                    and max_fav_seen >= entry_price + r_price
                    and prior_pivot is not None
                    and cur_l > prior_pivot
                ):
                    prior_pivot = cur_l
                    prev_sched = updates.get(confirm_close_ts, -math.inf)
                    updates[confirm_close_ts] = max(prev_sched, cur_l)
            else:
                max_fav_seen = min(max_fav_seen, prev_l, cur_l, next_l)
                is_pivot_high = cur_h >= prev_h and cur_h >= next_h
                if (
                    is_pivot_high
                    and max_fav_seen <= entry_price - r_price
                    and prior_pivot is not None
                    and cur_h < prior_pivot
                ):
                    prior_pivot = cur_h
                    prev_sched = updates.get(confirm_close_ts, math.inf)
                    updates[confirm_close_ts] = min(prev_sched, cur_h)

    return updates


def _trailing_atr_adv(
    bars_1m: Sequence[MinuteBar], idx: int, fallback_atr: float, fallback_adv5m: float
) -> tuple[float, float]:
    """Estimate causal 1m ATR and 5m ADV up to `bars_1m[idx]`."""
    start = max(0, idx - 14)
    window = bars_1m[start : idx + 1]
    if not window:
        return fallback_atr, fallback_adv5m
    trs: list[float] = []
    for j, b in enumerate(window):
        if j == 0:
            trs.append(b.high - b.low)
        else:
            prev_c = window[j - 1].close
            trs.append(max(b.high - b.low, abs(b.high - prev_c), abs(b.low - prev_c)))
    atr = sum(trs) / len(trs) if trs else fallback_atr
    v_win = bars_1m[max(0, idx - 11) : idx + 1]
    q_sum = sum(x.quote_vol for x in v_win)
    adv5m = (q_sum / len(v_win)) * 5.0 if v_win and q_sum > 0 else fallback_adv5m
    return (atr if atr > 0 else fallback_atr), (adv5m if adv5m > 0 else fallback_adv5m)


def simulate_holding_policy(
    signal: CandidateSignal,
    bars_1m: Sequence[MinuteBar],
    *,
    policy_kind: str,
    k_be: float = math.inf,
    target_r: float | None = None,
    max_hold_minutes: int = 4320,  # 72h (3 days) default wave horizon
    use_limit_target: bool = False,
    funding_schedule: Mapping[int, float] | None = None,
    params: FrictionParams = FrictionParams(),
) -> PolicySimResult:
    """Simulate a single holding policy on causal post-entry 1m bars (`bar.ts >= signal.entry_ts`).

    Supported `policy_kind`:
        - `"fixed_5m"`: preset -1R stop + forced close at +5m
        - `"fixed_30m"`: preset -1R stop + forced close at +30m
        - `"fixed_bracket"`: preset -1R stop + fixed `+target_r` (default +1.5R), intra-bar stop-first
        - `"wave"`: preset -1R stop + next-minute-open fee-inclusive BE at `k_be` R + 1h/4h HL/LH trail
    """
    raw_fwd = [b for b in bars_1m if b.ts >= signal.entry_ts]
    if not raw_fwd:
        raise ValueError(f"no post-entry 1m bars for signal {signal.signal_id}")
    fwd: list[MinuteBar] = [raw_fwd[0]]
    for b in raw_fwd[1:]:
        if b.ts - fwd[-1].ts > 300:
            break
        fwd.append(b)

    sgn = direction_sign(signal.direction)
    entry = signal.entry_price
    r_price = abs(entry - signal.stop_price)
    if r_price <= 0:
        raise ValueError(f"zero stop distance on signal {signal.signal_id}")

    if policy_kind == "fixed_5m":
        horizon_min = 5
        target_px = None
        policy_name = "fixed_5m"
    elif policy_kind == "fixed_30m":
        horizon_min = 30
        target_px = None
        policy_name = "fixed_30m"
    elif policy_kind == "fixed_bracket":
        tr = 1.5 if target_r is None else float(target_r)
        horizon_min = max_hold_minutes
        target_px = entry + sgn * tr * r_price
        policy_name = f"fixed_+{tr:.1f}R/-1.0R"
    elif policy_kind == "wave":
        horizon_min = max_hold_minutes
        target_px = None
        k_label = "inf" if math.isinf(k_be) else f"{k_be:.1f}"
        policy_name = f"wave_k_be={k_label}"
    else:
        raise ValueError(f"unknown policy_kind: {policy_kind!r}")

    # Fee-inclusive breakeven price: offsets nominal open+close taker fees;
    # stop-market execution still incurs s_open + s_close dynamic slippage + funding.
    round_trip_fee_frac = 2.0 * params.taker_fee_frac
    be_price = entry * (1.0 + sgn * round_trip_fee_frac)

    active_stop = signal.stop_price
    stop_state = "initial_stop"
    be_armed_next_open = False
    be_applied = False

    trail_schedule: dict[int, float] = {}
    if policy_kind == "wave":
        trail_schedule = _build_causal_structure_trail_schedule(
            fwd[:horizon_min], signal.direction, entry, signal.stop_price
        )
    sorted_trail_times = sorted(trail_schedule)
    trail_ptr = 0

    exit_idx = min(len(fwd), horizon_min) - 1
    exit_price = fwd[exit_idx].close
    exit_ts = fwd[exit_idx].ts + 60
    exit_kind = "time_exit"
    is_stop_exit = False
    close_is_maker = False

    limit_slice = fwd[:horizon_min]
    for i, bar in enumerate(limit_slice):
        # 1. Apply scheduled next-minute-open stop updates BEFORE inspecting bar [open, close]
        if policy_kind == "wave":
            if be_armed_next_open and not be_applied:
                if sgn > 0 and be_price > active_stop:
                    active_stop = be_price
                    stop_state = "breakeven_stop"
                elif sgn < 0 and be_price < active_stop:
                    active_stop = be_price
                    stop_state = "breakeven_stop"
                be_applied = True

            while trail_ptr < len(sorted_trail_times) and sorted_trail_times[trail_ptr] <= bar.ts:
                cand_stop = trail_schedule[sorted_trail_times[trail_ptr]]
                if sgn > 0 and cand_stop > active_stop:
                    active_stop = cand_stop
                    stop_state = "trail_stop"
                elif sgn < 0 and cand_stop < active_stop:
                    active_stop = cand_stop
                    stop_state = "trail_stop"
                trail_ptr += 1

        # 2. Check gap-through at minute open
        gap_stop = (sgn > 0 and bar.open <= active_stop) or (
            sgn < 0 and bar.open >= active_stop
        )
        if gap_stop:
            exit_idx = i
            exit_price = bar.open
            exit_ts = bar.ts
            exit_kind = stop_state
            is_stop_exit = True
            break

        # 3. Intra-bar check: WORST CASE STOP-FIRST (Work Item 4)
        hit_stop = (sgn > 0 and bar.low <= active_stop) or (
            sgn < 0 and bar.high >= active_stop
        )
        hit_target = False
        if target_px is not None:
            if use_limit_target:
                hit_target = limit_order_fills(
                    is_buy=(sgn < 0),
                    limit_price=target_px,
                    bar_low=bar.low,
                    bar_high=bar.high,
                    tick_size=signal.tick_size,
                )
            else:
                hit_target = (sgn > 0 and bar.high >= target_px) or (
                    sgn < 0 and bar.low <= target_px
                )

        if hit_stop:
            # Even if hit_target or k_be threshold was also touched on this 1m bar,
            # settle strictly as stop-first!
            exit_idx = i
            exit_price = active_stop
            exit_ts = bar.ts + 60
            exit_kind = stop_state
            is_stop_exit = True
            break

        if hit_target and target_px is not None:
            exit_idx = i
            exit_price = target_px
            exit_ts = bar.ts + 60
            exit_kind = "target"
            is_stop_exit = False
            close_is_maker = use_limit_target
            break

        # 4. At confirming minute close: check if wave breakeven arms for NEXT minute open
        if (
            policy_kind == "wave"
            and not math.isinf(k_be)
            and not be_applied
            and not be_armed_next_open
        ):
            fav_close_r = sgn * (bar.close - entry) / r_price
            if fav_close_r >= k_be:
                be_armed_next_open = True

    atr_close, adv_close = _trailing_atr_adv(
        fwd, exit_idx, signal.atr_1m_entry, signal.adv_5m_entry_usd
    )
    f_sched = dict(funding_schedule) if funding_schedule is not None else None
    cost = evaluate_trade_net_r(
        symbol=signal.symbol,
        direction=signal.direction,
        entry_price=entry,
        exit_price=exit_price,
        stop_price=signal.stop_price,
        entry_ts=signal.entry_ts,
        exit_ts=max(signal.entry_ts + 1, exit_ts),
        notional_usd=signal.notional_usd,
        open_is_maker=False,
        close_is_maker=close_is_maker,
        is_stop_exit=is_stop_exit,
        atr_1m_open=signal.atr_1m_entry,
        adv_5m_open_usd=signal.adv_5m_entry_usd,
        atr_1m_close=atr_close,
        adv_5m_close_usd=adv_close,
        funding_schedule=f_sched,
        params=params,
    )
    return PolicySimResult(
        policy_name=policy_name,
        signal_id=signal.signal_id,
        wave_cluster_id=signal.wave_cluster_id,
        symbol=signal.symbol,
        exit_ts=exit_ts,
        exit_price=exit_price,
        exit_kind=exit_kind,
        hold_seconds=max(0, exit_ts - signal.entry_ts),
        cost=cost,
    )


def select_plateau_winner(
    stats_by_k: Mapping[float, ClusterMeanStat],
    max_neighbor_drop_r: float = 0.15,
) -> PlateauSelection:
    """Select the winning `k_be` sitting on a flat parameter plateau (Work Item 5).

    Rejects isolated parameter spikes whose adjacent `k_be` grid points collapse
    by more than `max_neighbor_drop_r` (and more than 1 cluster SE).
    """
    ordered_keys = sorted(stats_by_k.keys(), key=lambda x: (math.isinf(x), x))
    if not ordered_keys:
        raise ValueError("stats_by_k cannot be empty")

    plateau_candidates: list[float] = []
    for idx, k in enumerate(ordered_keys):
        st = stats_by_k[k]
        neighbors: list[ClusterMeanStat] = []
        if idx > 0:
            neighbors.append(stats_by_k[ordered_keys[idx - 1]])
        if idx + 1 < len(ordered_keys):
            neighbors.append(stats_by_k[ordered_keys[idx + 1]])
        if not neighbors:
            plateau_candidates.append(k)
            continue

        se_tol = st.cluster_se if not math.isnan(st.cluster_se) else 0.0
        tol = max(max_neighbor_drop_r, se_tol)
        # Flat plateau requirement: at least one neighbor within `tol` AND average neighbor
        # does not drop by more than `1.5 * tol` (prevents needle spikes).
        best_neighbor_gap = min(st.mean - nb.mean for nb in neighbors)
        avg_neighbor_gap = sum(st.mean - nb.mean for nb in neighbors) / len(neighbors)
        if best_neighbor_gap <= tol and avg_neighbor_gap <= 1.5 * tol:
            plateau_candidates.append(k)

    if plateau_candidates:
        winner = max(plateau_candidates, key=lambda k: stats_by_k[k].mean)
        return PlateauSelection(
            stats_by_k=dict(stats_by_k),
            winner_k=winner,
            winner_stat=stats_by_k[winner],
            is_plateau=True,
            reason=f"selected flat plateau optimum k_be={winner} (plateau grid={plateau_candidates})",
        )

    # All points had steep drops (isolated spike warning): choose highest neighbor-smoothed score
    smoothed_scores: dict[float, float] = {}
    for idx, k in enumerate(ordered_keys):
        vals = [stats_by_k[k].mean]
        if idx > 0:
            vals.append(stats_by_k[ordered_keys[idx - 1]].mean)
        if idx + 1 < len(ordered_keys):
            vals.append(stats_by_k[ordered_keys[idx + 1]].mean)
        smoothed_scores[k] = sum(vals) / len(vals)
    winner = max(ordered_keys, key=lambda k: smoothed_scores[k])
    return PlateauSelection(
        stats_by_k=dict(stats_by_k),
        winner_k=winner,
        winner_stat=stats_by_k[winner],
        is_plateau=False,
        reason=f"isolated spike rejected; fell back to neighbor-smoothed k_be={winner}",
    )


def protective_stop_counterfactual(
    records: Sequence[ShortExitRecord],
    params: FrictionParams = FrictionParams(),
) -> ProtectiveStopCounterfactualReport:
    """Compute `saved_r = NetR_actual - NetR_hold_to_5m` for <5m preset-stop / fakeout exits (Item 6)."""
    rows: list[SavedRRow] = []
    for rec in records:
        hold_sec = rec.exit_ts - rec.entry_ts
        if hold_sec <= 0 or hold_sec >= 300:
            continue
        if not (rec.hit_preset_stop or rec.fakeout_confirmed):
            continue
        fwd = [b for b in rec.bars_1m if b.ts >= rec.entry_ts]
        if len(fwd) < 5:
            continue

        bar_5m = fwd[4]  # 5th 1-minute bar closes at entry_ts + 300s
        f_sched = dict(rec.funding_schedule) if rec.funding_schedule is not None else None

        actual_cost = evaluate_trade_net_r(
            symbol=rec.symbol,
            direction=rec.direction,
            entry_price=rec.entry_price,
            exit_price=rec.actual_exit_price,
            stop_price=rec.stop_price,
            entry_ts=rec.entry_ts,
            exit_ts=rec.exit_ts,
            notional_usd=rec.notional_usd,
            is_stop_exit=True,
            atr_1m_open=rec.atr_1m_entry,
            adv_5m_open_usd=rec.adv_5m_entry_usd,
            funding_schedule=f_sched,
            params=params,
        )
        hold5m_cost = evaluate_trade_net_r(
            symbol=rec.symbol,
            direction=rec.direction,
            entry_price=rec.entry_price,
            exit_price=bar_5m.close,
            stop_price=rec.stop_price,
            entry_ts=rec.entry_ts,
            exit_ts=bar_5m.ts + 60,
            notional_usd=rec.notional_usd,
            is_stop_exit=False,
            atr_1m_open=rec.atr_1m_entry,
            adv_5m_open_usd=rec.adv_5m_entry_usd,
            funding_schedule=f_sched,
            params=params,
        )
        rows.append(
            SavedRRow(
                ticket_id=rec.ticket_id,
                wave_cluster_id=rec.wave_cluster_id,
                symbol=rec.symbol,
                hold_seconds=hold_sec,
                net_r_actual=actual_cost.net_r,
                net_r_hold_5m=hold5m_cost.net_r,
                saved_r=actual_cost.net_r - hold5m_cost.net_r,
            )
        )

    cids = [r.wave_cluster_id for r in rows]
    actual_stat = cluster_robust_mean([r.net_r_actual for r in rows], cids)
    hold_stat = cluster_robust_mean([r.net_r_hold_5m for r in rows], cids)
    saved_stat = cluster_robust_mean([r.saved_r for r in rows], cids)
    return ProtectiveStopCounterfactualReport(
        rows=tuple(rows),
        actual_stat=actual_stat,
        hold_5m_stat=hold_stat,
        saved_r_stat=saved_stat,
    )
