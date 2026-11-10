"""Experiment P0: Path-Dependent NetR Calibration & Protective-Stop Counterfactual (Work Items 1-6).

Evaluates Kairos's MarketPulse + directional leader candidate signals on PIT-filtered
perpetual 1m historical bars in DuckDB under full perpetual friction:
  - Fixed 5m exit (`fixed_5m`)
  - Fixed 30m exit (`fixed_30m`)
  - Fixed +1.5R / -1.0R bracket (`fixed_bracket`)
  - Wave policy (`wave` with k_be in {0.8, 1.2, 1.5, 2.0, inf}, next-minute-open
    fee-inclusive breakeven + causal 1h/4h higher-low / lower-high structure trail)

Also computes Work Item 6 protective-stop counterfactual (`saved_r = NetR_actual - NetR_hold_to_5m`)
and enforces the Phase 1 Stop Condition (`max policy cluster-adjusted mean NetR < -0.15R`).
"""
from __future__ import annotations

import json
import math
import time
import urllib.parse
import urllib.request
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Mapping, Sequence

import duckdb

from clustering import (
    ClusterMeanStat,
    WakeCluster,
    assign_signal_to_wave_cluster,
    build_wave_clusters,
    cluster_robust_mean,
    validate_ex_ante_grouping,
)
from friction import FrictionParams
from pit_universe import (
    PerpUniverseRecord,
    VolumeObservation,
    ensure_pit_schema,
    fetch_okx_usdt_swaps,
    normalize_base_coin,
    normalize_perp_symbol,
    query_pit_candidates,
    upsert_pit_universe,
    upsert_volume_24h,
)
from policies import (
    K_BE_GRID,
    CandidateSignal,
    MinuteBar,
    PlateauSelection,
    PolicySimResult,
    ProtectiveStopCounterfactualReport,
    ShortExitRecord,
    protective_stop_counterfactual,
    select_plateau_winner,
    simulate_holding_policy,
)

STOP_CONDITION_FLOOR_R = -0.15

DEFAULT_LIQUID_SWAP_INST_IDS: tuple[str, ...] = (
    "BTC-USDT-SWAP",
    "ETH-USDT-SWAP",
    "SOL-USDT-SWAP",
    "XRP-USDT-SWAP",
    "DOGE-USDT-SWAP",
    "SUI-USDT-SWAP",
    "LINK-USDT-SWAP",
    "AVAX-USDT-SWAP",
    "ADA-USDT-SWAP",
    "DOT-USDT-SWAP",
    "LTC-USDT-SWAP",
    "BCH-USDT-SWAP",
    "NEAR-USDT-SWAP",
    "UNI-USDT-SWAP",
    "AAVE-USDT-SWAP",
)


@dataclass(frozen=True)
class P0CalibrationReport:
    """Complete headline + per-policy output of Experiment P0."""

    n_signals: int
    n_clusters: int
    wake_clusters: tuple[WakeCluster, ...]
    policy_stats: dict[str, ClusterMeanStat]
    plateau: PlateauSelection
    counterfactual: ProtectiveStopCounterfactualReport
    stop_condition_triggered: bool
    best_policy_name: str
    best_policy_mean_r: float


def ensure_market_bars_schema(con: duckdb.DuckDBPyConnection) -> None:
    """Create 1m bar and 8h funding tables in DuckDB if absent."""
    ensure_pit_schema(con)
    con.execute(
        """
        CREATE TABLE IF NOT EXISTS fact_perp_bars_1m (
            symbol VARCHAR NOT NULL,
            ts BIGINT NOT NULL,
            open DOUBLE NOT NULL,
            high DOUBLE NOT NULL,
            low DOUBLE NOT NULL,
            close DOUBLE NOT NULL,
            quote_vol DOUBLE NOT NULL,
            PRIMARY KEY (symbol, ts)
        )
        """
    )
    con.execute(
        """
        CREATE TABLE IF NOT EXISTS fact_perp_funding_8h (
            symbol VARCHAR NOT NULL,
            ts BIGINT NOT NULL,
            funding_rate DOUBLE NOT NULL,
            PRIMARY KEY (symbol, ts)
        )
        """
    )


import threading
import urllib.error

_OKX_RATE_LOCK = threading.Lock()
_OKX_NEXT_SLOT = 0.0
_OKX_MIN_INTERVAL_SEC = 0.075  # ~13.3 req/sec globally (under OKX 20 req / 2s limit)


def _okx_get_json(url: str, timeout_sec: float = 12.0, max_retries: int = 5) -> dict:
    global _OKX_NEXT_SLOT
    for attempt in range(max_retries):
        with _OKX_RATE_LOCK:
            now = time.monotonic()
            wait = _OKX_NEXT_SLOT - now
            if wait > 0:
                _OKX_NEXT_SLOT += _OKX_MIN_INTERVAL_SEC
            else:
                _OKX_NEXT_SLOT = now + _OKX_MIN_INTERVAL_SEC
        if wait > 0:
            time.sleep(wait)

        req = urllib.request.Request(url, headers={"User-Agent": "kairos-research/0.1"})
        try:
            with urllib.request.urlopen(req, timeout=timeout_sec) as resp:
                payload = json.loads(resp.read().decode("utf-8"))
        except urllib.error.HTTPError as exc:
            if exc.code == 429 and attempt + 1 < max_retries:
                time.sleep(0.8 * (2**attempt))
                continue
            raise
        except urllib.error.URLError:
            if attempt + 1 < max_retries:
                time.sleep(0.5 * (attempt + 1))
                continue
            raise

        if payload.get("code") == "50011" and attempt + 1 < max_retries:
            time.sleep(0.8 * (2**attempt))
            continue
        if payload.get("code") != "0":
            raise RuntimeError(f"OKX API error ({url}): {payload.get('msg')}")
        return payload
    raise RuntimeError(f"OKX API retries exhausted for {url}")


def fetch_okx_history_1m(
    inst_id: str,
    start_ts: int,
    end_ts: int,
    pause_sec: float = 0.0,
) -> list[MinuteBar]:
    """Fetch 1-minute historical candles from OKX `[start_ts, end_ts]` (Unix seconds).

    Uses `/api/v5/market/candles` (`limit=300`) for recent bars while available,
    then seamlessly falls back to `/api/v5/market/history-candles` (`limit=100`).
    """
    cursor_ms = (int(end_ts) + 60) * 1000
    start_ms = int(start_ts) * 1000
    by_ts: dict[int, MinuteBar] = {}
    use_recent_endpoint = (int(time.time()) - int(end_ts)) < 7200

    while cursor_ms > start_ms:
        endpoint = "candles" if use_recent_endpoint else "history-candles"
        lim = "300" if use_recent_endpoint else "100"
        qs = urllib.parse.urlencode(
            {
                "instId": inst_id,
                "bar": "1m",
                "after": str(cursor_ms),
                "limit": lim,
            }
        )
        payload = _okx_get_json(f"https://www.okx.com/api/v5/market/{endpoint}?{qs}")
        rows = payload.get("data", [])
        if not rows:
            if use_recent_endpoint:
                use_recent_endpoint = False
                continue
            break
        oldest_ms = cursor_ms
        for r in rows:
            ts_ms = int(r[0])
            oldest_ms = min(oldest_ms, ts_ms)
            ts_sec = ts_ms // 1000
            if start_ts <= ts_sec <= end_ts:
                by_ts[ts_sec] = MinuteBar(
                    ts=ts_sec,
                    open=float(r[1]),
                    high=float(r[2]),
                    low=float(r[3]),
                    close=float(r[4]),
                    quote_vol=float(r[7]),
                )
        if oldest_ms >= cursor_ms:
            if use_recent_endpoint:
                use_recent_endpoint = False
                continue
            break
        cursor_ms = oldest_ms
        if pause_sec > 0:
            time.sleep(pause_sec)

    return [by_ts[k] for k in sorted(by_ts)]


def fetch_okx_funding_history(
    inst_id: str,
    start_ts: int,
    end_ts: int,
) -> dict[int, float]:
    """Fetch 8h historical realized funding rates from OKX."""
    cursor_ms = (int(end_ts) + 3600) * 1000
    start_ms = int(start_ts) * 1000
    out: dict[int, float] = {}
    for _ in range(10):
        qs = urllib.parse.urlencode(
            {
                "instId": inst_id,
                "after": str(cursor_ms),
                "limit": "100",
            }
        )
        payload = _okx_get_json(
            f"https://www.okx.com/api/v5/public/funding-rate-history?{qs}"
        )
        rows = payload.get("data", [])
        if not rows:
            break
        oldest_ms = cursor_ms
        for item in rows:
            ts_ms = int(item["fundingTime"])
            oldest_ms = min(oldest_ms, ts_ms)
            # Snap to nearest 8h settlement second
            ts_sec = round((ts_ms / 1000.0) / 28800.0) * 28800
            if start_ts <= ts_sec <= end_ts:
                out[ts_sec] = float(item.get("realizedRate") or item["fundingRate"])
        if oldest_ms <= start_ms or oldest_ms >= cursor_ms:
            break
        cursor_ms = oldest_ms
    return out


def ingest_okx_sample_to_duckdb(
    con: duckdb.DuckDBPyConnection,
    *,
    start_ts: int,
    end_ts: int,
    inst_ids: Sequence[str] = DEFAULT_LIQUID_SWAP_INST_IDS[:12],
    max_workers: int = 8,
) -> dict[str, int]:
    """Ingest PIT universe metadata, 1m candles, rolling 24h quote volume, and 8h funding.

    When `[start_ts, end_ts]` spans > 2.5 days, samples 4 contiguous 18h-24h 1m-bar
    windows spaced across `[start_ts, end_ts]` so multiple >24h-separated MarketPulse
    wave clusters (`wave_cluster_id`) are captured with 100% genuine 1m bars within
    OKX's public rate limit budget.
    """
    from concurrent.futures import ThreadPoolExecutor

    ensure_market_bars_schema(con)
    swaps = fetch_okx_usdt_swaps()
    wanted_syms = {normalize_perp_symbol(i) for i in inst_ids}
    selected_records: list[PerpUniverseRecord] = [
        r for r in swaps if r.symbol in wanted_syms
    ]
    if not selected_records:
        raise RuntimeError("no matching USDT swap instruments returned from OKX")
    upsert_pit_universe(con, selected_records)

    span_sec = max(3600, int(end_ts) - int(start_ts))
    if span_sec <= int(2.2 * 86400):
        windows = [(int(start_ts), int(end_ts))]
    else:
        # 4 windows spaced >36h apart across the multi-day span (each 15h = 900 1m bars,
        # plus 24h for the most recent window using limit=300 /market/candles)
        w_dur = 15 * 3600
        step = (span_sec - 24 * 3600) // 3
        windows = [
            (int(start_ts), int(start_ts) + w_dur),
            (int(start_ts) + step, int(start_ts) + step + w_dur),
            (int(start_ts) + 2 * step, int(start_ts) + 2 * step + w_dur),
            (int(end_ts) - 24 * 3600, int(end_ts)),
        ]

    def _fetch_one(inst_id: str) -> tuple[str, list[MinuteBar], dict[int, float]]:
        sym = normalize_perp_symbol(inst_id)
        by_ts: dict[int, MinuteBar] = {}
        for w_start, w_end in windows:
            for b in fetch_okx_history_1m(inst_id, start_ts=w_start, end_ts=w_end):
                by_ts[b.ts] = b
        bars = [by_ts[k] for k in sorted(by_ts)]
        f_sched = fetch_okx_funding_history(inst_id, start_ts=start_ts, end_ts=end_ts)
        return sym, bars, f_sched

    with ThreadPoolExecutor(max_workers=max_workers) as pool:
        fetched = list(pool.map(_fetch_one, inst_ids))

    bar_count = 0
    funding_count = 0
    for sym, bars, f_sched in fetched:
        if bars:
            con.executemany(
                """
                INSERT OR REPLACE INTO fact_perp_bars_1m
                    (symbol, ts, open, high, low, close, quote_vol)
                VALUES (?, ?, ?, ?, ?, ?, ?)
                """,
                [
                    (sym, b.ts, b.open, b.high, b.low, b.close, b.quote_vol)
                    for b in bars
                ],
            )
            bar_count += len(bars)

            vol_obs: list[VolumeObservation] = []
            running_q = 0.0
            left = 0
            for right, b in enumerate(bars):
                running_q += b.quote_vol
                while b.ts - bars[left].ts >= 86400:
                    running_q -= bars[left].quote_vol
                    left += 1
                win_len = right - left + 1
                scale = 1440.0 / max(60.0, float(win_len)) if win_len < 1440 else 1.0
                if b.ts % 300 == 0:
                    vol_obs.append(
                        VolumeObservation(
                            symbol=sym,
                            ts=b.ts + 60,
                            quote_vol_24h=running_q * scale,
                        )
                    )
            upsert_volume_24h(con, vol_obs)

        if f_sched:
            con.executemany(
                """
                INSERT OR REPLACE INTO fact_perp_funding_8h (symbol, ts, funding_rate)
                VALUES (?, ?, ?)
                """,
                [(sym, ts, rate) for ts, rate in f_sched.items()],
            )
            funding_count += len(f_sched)

    return {
        "pit_symbols": len(selected_records),
        "bars_1m": bar_count,
        "funding_ticks": funding_count,
    }


def load_bars_and_funding_from_duckdb(
    con: duckdb.DuckDBPyConnection,
) -> tuple[dict[str, list[MinuteBar]], dict[str, dict[int, float]], dict[str, float]]:
    """Load 1m bars, funding schedules, and tick sizes from DuckDB."""
    ensure_market_bars_schema(con)
    tick_map = {
        row[0]: float(row[1])
        for row in con.execute(
            "SELECT symbol, tick_size FROM dim_perp_universe_pit"
        ).fetchall()
    }
    bars_by_sym: dict[str, list[MinuteBar]] = {}
    rows = con.execute(
        """
        SELECT symbol, ts, open, high, low, close, quote_vol
        FROM fact_perp_bars_1m
        ORDER BY symbol, ts
        """
    ).fetchall()
    for sym, ts, o, h, l, c, qv in rows:
        bars_by_sym.setdefault(sym, []).append(
            MinuteBar(
                ts=int(ts),
                open=float(o),
                high=float(h),
                low=float(l),
                close=float(c),
                quote_vol=float(qv),
            )
        )

    funding_by_sym: dict[str, dict[int, float]] = {}
    f_rows = con.execute(
        "SELECT symbol, ts, funding_rate FROM fact_perp_funding_8h ORDER BY symbol, ts"
    ).fetchall()
    for sym, ts, rate in f_rows:
        funding_by_sym.setdefault(sym, {})[int(ts)] = float(rate)

    return bars_by_sym, funding_by_sym, tick_map


def _detect_5m_pullback_trigger(
    direction: str,
    bars_5m: Sequence[tuple[int, float, float, float, float]],
    *,
    min_pullback_ts: int = 0,
    min_stop_pct: float = 0.008,
) -> tuple[float, float] | None:
    """Port of `internal/opportunity/trigger.go` (`DetectPullbackTrigger` with
    `RequireInSessionPullback: true`) + `leader_pullback_v1` 15m/1h setup alignment
    on closed 5m bars `(close_ts, open, high, low, close)`.
    """
    n = len(bars_5m)
    if n < 25:
        return None
    lo = max(1, n - 30)
    hi = n - 4
    if hi <= lo + 3:
        return None

    last_close = bars_5m[-1][4]
    prior_close = bars_5m[-2][4]
    # 1h / 15m setup trend alignment (EMA12 vs EMA24 on 5m = 1h/2h structure)
    ema_fast = sum(b[4] for b in bars_5m[-12:]) / 12.0
    ema_slow = sum(b[4] for b in bars_5m[-24:]) / 24.0

    if direction == "UP":
        if not (last_close > ema_fast >= ema_slow):
            return None
        peak_idx = lo
        for i in range(lo, hi + 1):
            if bars_5m[i][2] >= bars_5m[peak_idx][2]:
                peak_idx = i
        trough_idx = lo
        for i in range(lo, peak_idx):
            if bars_5m[i][3] <= bars_5m[trough_idx][3]:
                trough_idx = i
        peak = bars_5m[peak_idx][2]
        trough = bars_5m[trough_idx][3]
        if trough <= 0 or peak <= trough:
            return None
        impulse_pct = (peak - trough) / trough * 100.0
        if impulse_pct < 0.8 or peak_idx >= n - 2:
            return None
        pb_low_idx = peak_idx + 1
        for i in range(peak_idx + 1, n - 1):
            if bars_5m[i][3] <= bars_5m[pb_low_idx][3]:
                pb_low_idx = i
        pb_low = bars_5m[pb_low_idx][3]
        pb_ts = bars_5m[pb_low_idx][0] - 300
        if min_pullback_ts > 0 and pb_ts < min_pullback_ts:
            return None
        if pb_low >= peak or pb_low <= trough:
            return None
        depth_pct = (peak - pb_low) / peak * 100.0
        if depth_pct < 0.35 or depth_pct > 6.0:
            return None
        mid = (peak + pb_low) / 2.0
        if last_close <= pb_low or last_close <= prior_close:
            return None
        if last_close < mid or last_close > peak * 1.015:
            return None
        # Structural stop at 15m/5m higher-low trough with min_stop_pct buffer
        stop = min(pb_low, last_close * (1.0 - min_stop_pct))
        return (last_close, stop)

    # DOWN: mirror short bounce (`detectShortBounce`)
    if not (last_close < ema_fast <= ema_slow):
        return None
    trough_idx = lo
    for i in range(lo, hi + 1):
        if bars_5m[i][3] <= bars_5m[trough_idx][3]:
            trough_idx = i
    peak_idx = lo
    for i in range(lo, trough_idx):
        if bars_5m[i][2] >= bars_5m[peak_idx][2]:
            peak_idx = i
    trough = bars_5m[trough_idx][3]
    peak = bars_5m[peak_idx][2]
    if peak <= 0 or trough >= peak:
        return None
    impulse_pct = (peak - trough) / peak * 100.0
    if impulse_pct < 0.8 or trough_idx >= n - 2:
        return None
    bh_idx = trough_idx + 1
    for i in range(trough_idx + 1, n - 1):
        if bars_5m[i][2] >= bars_5m[bh_idx][2]:
            bh_idx = i
    bounce_high = bars_5m[bh_idx][2]
    bh_ts = bars_5m[bh_idx][0] - 300
    if min_pullback_ts > 0 and bh_ts < min_pullback_ts:
        return None
    if bounce_high <= trough or bounce_high >= peak:
        return None
    rebound_pct = (bounce_high - trough) / trough * 100.0
    if rebound_pct < 0.35 or rebound_pct > 6.0:
        return None
    mid = (trough + bounce_high) / 2.0
    if last_close >= bounce_high or last_close >= prior_close:
        return None
    if last_close > mid or last_close < trough * 0.985:
        return None
    stop = max(bounce_high, last_close * (1.0 + min_stop_pct))
    return (last_close, stop)


def detect_market_pulse_candidates(
    con: duckdb.DuckDBPyConnection,
    bars_by_sym: Mapping[str, Sequence[MinuteBar]],
    tick_map: Mapping[str, float],
    *,
    min_quote_vol_24h: float = 30_000_000.0,
    min_valid_symbols: int = 8,
    min_breadth_5m: float = 0.65,
    min_median_ret_5m_pct: float = 0.25,
    min_relative_edge_pct: float = 0.15,
    noise_ret_pct: float = 0.08,
    wake_cooldown_sec: int = 1800,
    session_ttl_sec: int = 7200,
    leaders_per_wake: int = 4,
    min_post_bars_1m: int = 180,
    notional_usd: float = 10_000.0,
) -> tuple[list[CandidateSignal], list[WakeCluster], list[ShortExitRecord]]:
    """Replay Kairos's MarketPulse cross-sectional wake + `leader_pullback_v1`
    (`internal/playbook/leader_pullback.go` + `internal/opportunity/trigger.go`)
    candidate detector strictly causally (`t <= entry_ts`).
    """
    idx_by_sym: dict[str, dict[int, int]] = {}
    bars5m_by_sym: dict[str, list[tuple[int, float, float, float, float]]] = {}
    close5m_idx_by_sym: dict[str, dict[int, int]] = {}
    all_5m_closes: set[int] = set()

    for sym, series in bars_by_sym.items():
        idx_map = {b.ts: idx for idx, b in enumerate(series)}
        idx_by_sym[sym] = idx_map
        b5_list: list[tuple[int, float, float, float, float]] = []
        c5_map: dict[int, int] = {}
        buckets: dict[int, list[MinuteBar]] = {}
        for b in series:
            b_open = (b.ts // 300) * 300
            buckets.setdefault(b_open, []).append(b)
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
                )
            )
            all_5m_closes.add(close_ts)
        bars5m_by_sym[sym] = b5_list
        close5m_idx_by_sym[sym] = c5_map

    raw_wakes: list[tuple[str, int, str, float, list[tuple[str, float, int]]]] = []
    last_wake_ts = -10**12

    for eval_ts in sorted(all_5m_closes):
        if eval_ts - last_wake_ts < wake_cooldown_sec:
            continue

        pit_pool = query_pit_candidates(
            con, at_ts=eval_ts, min_quote_vol_24h=min_quote_vol_24h
        )
        if len(pit_pool) < min_valid_symbols:
            continue

        rets_5m: list[tuple[str, float, int]] = []
        primary_rets: dict[str, float] = {}
        for sym in pit_pool:
            series = bars_by_sym.get(sym)
            idx_map = idx_by_sym.get(sym)
            if not series or not idx_map:
                continue
            cur_bar_idx = idx_map.get(eval_ts - 60)
            prev_bar_idx = idx_map.get(eval_ts - 360)
            if cur_bar_idx is None or prev_bar_idx is None:
                continue
            if cur_bar_idx < 30 or (len(series) - 1 - cur_bar_idx) < min_post_bars_1m:
                continue
            if (
                series[cur_bar_idx + min_post_bars_1m].ts - series[cur_bar_idx].ts
                > (min_post_bars_1m + 2) * 60
            ):
                continue
            p_now = series[cur_bar_idx].close
            p_prev = series[prev_bar_idx].close
            if p_prev <= 0 or p_now <= 0:
                continue
            ret_pct = ((p_now - p_prev) / p_prev) * 100.0
            rets_5m.append((sym, ret_pct, cur_bar_idx))
            base = normalize_base_coin(sym)
            if base in ("BTC", "ETH"):
                primary_rets[base] = ret_pct

        if len(rets_5m) < min_valid_symbols:
            continue

        n_valid = len(rets_5m)
        up_breadth = sum(1 for _, r, _ in rets_5m if r > noise_ret_pct) / n_valid
        dn_breadth = sum(1 for _, r, _ in rets_5m if r < -noise_ret_pct) / n_valid
        sorted_vals = sorted(r for _, r, _ in rets_5m)
        med_ret = sorted_vals[n_valid // 2]

        direction: str | None = None
        if up_breadth >= min_breadth_5m and med_ret >= min_median_ret_5m_pct:
            if not primary_rets or max(primary_rets.values()) > 0.0:
                direction = "UP"
        elif dn_breadth >= min_breadth_5m and med_ret <= -min_median_ret_5m_pct:
            if not primary_rets or min(primary_rets.values()) < 0.0:
                direction = "DOWN"

        if direction is None:
            continue

        wake_id = f"mp_{eval_ts}_{direction.lower()}"
        raw_wakes.append((wake_id, eval_ts, direction, med_ret, rets_5m))
        last_wake_ts = eval_ts

    # Work Item 3: merge wake events < 24h apart into one `wave_cluster_id`
    wake_clusters, wake_to_cluster = build_wave_clusters(
        [(wid, float(wts)) for wid, wts, _, _, _ in raw_wakes]
    )

    signals: list[CandidateSignal] = []
    short_exits: list[ShortExitRecord] = []
    seen_sym_ts: set[tuple[str, int]] = set()

    for wake_id, wake_ts, direction, med_ret, rets_5m in raw_wakes:
        cluster_id = wake_to_cluster[wake_id]
        sgn = 1.0 if direction == "UP" else -1.0
        # Filter by RelativeStrength / RelativeWeakness >= min_relative_edge_pct
        leaders = [
            (sym, r_pct, c_idx)
            for sym, r_pct, c_idx in rets_5m
            if sgn * (r_pct - med_ret) >= min_relative_edge_pct
        ]
        ranked = sorted(leaders, key=lambda item: sgn * item[1], reverse=True)

        for sym, _, _ in ranked[:leaders_per_wake]:
            series = bars_by_sym[sym]
            idx_map = idx_by_sym[sym]
            b5_list = bars5m_by_sym.get(sym, [])
            c5_map = close5m_idx_by_sym.get(sym, {})

            for trig_ts in range(wake_ts + 300, wake_ts + session_ttl_sec + 1, 300):
                b5_idx = c5_map.get(trig_ts)
                if b5_idx is None or b5_idx < 24:
                    continue
                window_5m = b5_list[max(0, b5_idx - 29) : b5_idx + 1]
                trig = _detect_5m_pullback_trigger(
                    direction, window_5m, min_pullback_ts=wake_ts - 1800
                )
                if trig is None:
                    continue
                entry_bar_idx = idx_map.get(trig_ts)
                if entry_bar_idx is None or (len(series) - 1 - entry_bar_idx) < min_post_bars_1m:
                    continue
                if (
                    series[entry_bar_idx + min_post_bars_1m].ts - series[entry_bar_idx].ts
                    > (min_post_bars_1m + 2) * 60
                ):
                    continue
                if (sym, trig_ts) in seen_sym_ts:
                    break
                seen_sym_ts.add((sym, trig_ts))

                entry_px, stop_px = trig
                lookback_15m = series[max(0, entry_bar_idx - 15) : entry_bar_idx]
                trs = [b.high - b.low for b in lookback_15m]
                atr_1m = sum(trs) / len(trs) if trs else entry_px * 0.002
                adv_5m = sum(b.quote_vol for b in lookback_15m[-5:])
                raw_dist = abs(entry_px - stop_px)

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
                    adv_5m_entry_usd=max(adv_5m, 50_000.0),
                    notional_usd=notional_usd,
                )
                signals.append(sig)

                trig_open = window_5m[-1][1]
                trig_low = window_5m[-1][3]
                trig_high = window_5m[-1][2]
                fwd_5 = series[entry_bar_idx : entry_bar_idx + 6]
                if len(fwd_5) >= 5:
                    for m_idx in range(4):
                        b = fwd_5[m_idx]
                        hit_preset = (sgn > 0 and b.low <= stop_px) or (
                            sgn < 0 and b.high >= stop_px
                        )
                        broke_trig_structure = (
                            (b.close < trig_low)
                            if sgn > 0
                            else (b.close > trig_high)
                        ) or (
                            sgn * (b.close - entry_px)
                            <= -0.5 * max(abs(entry_px - trig_open), atr_1m)
                        )
                        avg_q = max(1.0, adv_5m / 5.0)
                        engulfed_trigger = (
                            broke_trig_structure
                            and sgn * (b.close - entry_px) <= -entry_px * 0.0028
                            and sgn * (b.close - b.open) < 0
                            and b.quote_vol >= 0.75 * avg_q
                        )
                        fakeout = (not hit_preset) and engulfed_trigger
                        if hit_preset or fakeout:
                            actual_exit_px = stop_px if hit_preset else b.close
                            short_exits.append(
                                ShortExitRecord(
                                    ticket_id=sig.signal_id,
                                    wave_cluster_id=cluster_id,
                                    symbol=sym,
                                    direction=direction,
                                    entry_ts=sig.entry_ts,
                                    exit_ts=b.ts + 60,
                                    entry_price=entry_px,
                                    stop_price=stop_px,
                                    actual_exit_price=actual_exit_px,
                                    hit_preset_stop=hit_preset,
                                    fakeout_confirmed=fakeout,
                                    bars_1m=fwd_5,
                                    atr_1m_entry=atr_1m,
                                    adv_5m_entry_usd=sig.adv_5m_entry_usd,
                                    notional_usd=notional_usd,
                                )
                            )
                            break
                break

    return signals, wake_clusters, short_exits


def run_p0_calibration(
    signals: Sequence[CandidateSignal],
    bars_by_sym: Mapping[str, Sequence[MinuteBar]],
    *,
    wake_clusters: Sequence[WakeCluster] = (),
    short_exit_records: Sequence[ShortExitRecord] = (),
    funding_by_sym: Mapping[str, Mapping[int, float]] | None = None,
    group_by: Sequence[str] = (),
    max_hold_minutes: int = 1440,
    params: FrictionParams = FrictionParams(),
) -> P0CalibrationReport:
    """Evaluate all required P0 holding policies on ONE fixed candidate signal set."""
    if group_by:
        validate_ex_ante_grouping(group_by)
    if not signals:
        raise ValueError("cannot run P0 calibration on an empty candidate signal set")

    policy_Order: list[tuple[str, str, float]] = [
        ("fixed_5m", "fixed_5m", math.inf),
        ("fixed_30m", "fixed_30m", math.inf),
        ("fixed_+1.5R/-1.0R", "fixed_bracket", math.inf),
    ]
    for k in K_BE_GRID:
        k_lbl = "inf" if math.isinf(k) else f"{k:.1f}"
        policy_Order.append((f"wave_k_be={k_lbl}", "wave", k))

    results_by_policy: dict[str, list[PolicySimResult]] = {
        name: [] for name, _, _ in policy_Order
    }

    for sig in signals:
        bars = bars_by_sym.get(sig.symbol)
        if not bars:
            raise ValueError(f"missing 1m bars for signal {sig.signal_id} ({sig.symbol})")
        f_sched = funding_by_sym.get(sig.symbol) if funding_by_sym else None
        for name, kind, k_val in policy_Order:
            res = simulate_holding_policy(
                sig,
                bars,
                policy_kind=kind,
                k_be=k_val,
                target_r=1.5 if kind == "fixed_bracket" else None,
                max_hold_minutes=max_hold_minutes,
                funding_schedule=f_sched,
                params=params,
            )
            results_by_policy[name].append(res)

    policy_stats: dict[str, ClusterMeanStat] = {}
    wave_stats_by_k: dict[float, ClusterMeanStat] = {}
    for name, kind, k_val in policy_Order:
        sims = results_by_policy[name]
        st = cluster_robust_mean(
            [s.net_r for s in sims],
            [s.wave_cluster_id for s in sims],
        )
        policy_stats[name] = st
        if kind == "wave":
            wave_stats_by_k[k_val] = st

    plateau = select_plateau_winner(wave_stats_by_k)
    cf_report = protective_stop_counterfactual(short_exit_records, params=params)

    best_name = max(policy_stats, key=lambda p: policy_stats[p].mean)
    best_mean = policy_stats[best_name].mean
    # Stop condition: EVERY holding policy has cluster-adjusted mean NetR < -0.15R
    stop_triggered = all(
        st.mean < STOP_CONDITION_FLOOR_R for st in policy_stats.values()
    )

    n_clusters = len({s.wave_cluster_id for s in signals})
    return P0CalibrationReport(
        n_signals=len(signals),
        n_clusters=n_clusters,
        wake_clusters=tuple(wake_clusters),
        policy_stats=policy_stats,
        plateau=plateau,
        counterfactual=cf_report,
        stop_condition_triggered=stop_triggered,
        best_policy_name=best_name,
        best_policy_mean_r=best_mean,
    )


def load_journal_short_exits(
    journal_path: str | Path,
    bars_by_sym: Mapping[str, Sequence[MinuteBar]],
    wake_clusters: Sequence[WakeCluster] = (),
) -> list[ShortExitRecord]:
    """Extract historical Journal <5m preset-stop / fakeout exits if present in JSONL."""
    p = Path(journal_path)
    if not p.is_file():
        return []

    tickets: dict[str, dict] = {}
    decisions: dict[str, dict] = {}
    outcomes: dict[str, dict] = {}
    for raw in p.read_text().splitlines():
        line = raw.strip()
        if not line:
            continue
        rec = json.loads(line)
        kind = rec.get("kind")
        payload = rec.get("payload") or {}
        if kind == "ticket":
            tid = str(payload.get("id") or rec.get("id") or "")
            if tid:
                tickets[tid] = payload
        elif kind == "decision":
            tid = str(payload.get("ticket_id") or "")
            if tid:
                decisions[tid] = payload
        elif kind == "outcome":
            tid = str(payload.get("ticket_id") or "")
            if tid:
                outcomes[tid] = payload

    out: list[ShortExitRecord] = []
    for tid, tkt in tickets.items():
        dec = decisions.get(tid) or {}
        if str(dec.get("action", "")).upper() != "ACCEPT":
            continue
        entry_px = float(dec.get("entry_price") or 0.0)
        exit_px = float(dec.get("exit_price") or 0.0)
        stop_px = float(tkt.get("stop_price") or 0.0)
        if entry_px <= 0 or exit_px <= 0 or stop_px <= 0:
            continue
        decided_at = dec.get("decided_at")
        closed_at = dec.get("closed_at")
        if not decided_at or not closed_at:
            continue
        t_in = int(datetime.fromisoformat(str(decided_at).replace("Z", "+00:00")).timestamp())
        t_out = int(datetime.fromisoformat(str(closed_at).replace("Z", "+00:00")).timestamp())
        if not (0 < t_out - t_in < 300):
            continue
        sym = normalize_perp_symbol(str(tkt.get("symbol") or ""))
        bars = bars_by_sym.get(sym, ())
        if not bars:
            continue
        dir_str = str(tkt.get("direction") or "UP").upper()
        sgn = 1.0 if dir_str in ("UP", "LONG") else -1.0
        exit_reason = str(dec.get("exit_reason") or "").upper()
        hit_stop = (
            exit_reason == "PROTECTIVE_STOP"
            or (sgn > 0 and exit_px <= stop_px * 1.001)
            or (sgn < 0 and exit_px >= stop_px * 0.999)
            or bool((outcomes.get(tid) or {}).get("stop_hit_first"))
        )
        fakeout = exit_reason == "FAKEOUT_ABORT"
        if not (hit_stop or fakeout):
            continue
        cid = assign_signal_to_wave_cluster(float(t_in), wake_clusters)
        out.append(
            ShortExitRecord(
                ticket_id=tid,
                wave_cluster_id=cid,
                symbol=sym,
                direction=dir_str,
                entry_ts=t_in,
                exit_ts=t_out,
                entry_price=entry_px,
                stop_price=stop_px,
                actual_exit_price=exit_px,
                hit_preset_stop=hit_stop,
                fakeout_confirmed=fakeout,
                bars_1m=bars,
            )
        )
    return out


def format_p0_markdown(
    report: P0CalibrationReport,
    *,
    dataset_label: str = "OKX USDT-SWAP PIT 1m Replay",
) -> str:
    """Render P0 calibration results, plateau verification, and saved-R counterfactual as Markdown."""
    today = datetime.now(timezone.utc).date().isoformat()
    lines = [
        f"# Experiment P0 — Path-Dependent NetR Calibration ({today})",
        "",
        f"- **Dataset**: {dataset_label}",
        f"- **Candidate Signals (`N`)**: {report.n_signals}",
        f"- **Merged Wave Clusters (`G`, <24h rule)**: {report.n_clusters}",
        "- **Friction Model**: Taker 5 bps / Maker 2 bps (>=1 tick trade-through), "
        "dynamic slippage `s0=1.5 bps (majors) / 5.0 bps (alts) + 0.5*(ATR_1m/P)*sqrt(V/ADV_5m)`, "
        "8h UTC funding ticks, stop exits always charged taker + dynamic slippage",
        "- **Intra-bar Resolution**: Worst-case `stop-first`; breakeven arms at confirming minute close "
        "and activates at next minute open",
        "",
        "## 1. Holding Policy Comparison (Cluster-Robust SE by `wave_cluster_id`)",
        "",
        "| Policy | N | Clusters (G) | Mean NetR | Cluster SE | t-stat | 95% CI |",
        "|---|---:|---:|---:|---:|---:|---:|",
    ]
    for name, st in report.policy_stats.items():
        se_s = f"{st.cluster_se:.4f}" if not math.isnan(st.cluster_se) else "n/a"
        t_s = f"{st.t_stat:+.2f}" if not math.isnan(st.t_stat) else "n/a"
        ci_s = (
            f"[{st.ci95_low:+.4f}, {st.ci95_high:+.4f}]"
            if not math.isnan(st.ci95_low)
            else "n/a"
        )
        lines.append(
            f"| `{name}` | {st.n_obs} | {st.n_clusters} | **{st.mean:+.4f}R** | {se_s} | {t_s} | {ci_s} |"
        )

    w_k = "inf" if math.isinf(report.plateau.winner_k) else f"{report.plateau.winner_k:.1f}"
    lines.extend(
        [
            "",
            "## 2. Parameter Plateau & Stop-Condition Verdict",
            "",
            f"- **Wave Plateau Winner**: `k_be = {w_k}` "
            f"(mean `{report.plateau.winner_stat.mean:+.4f}R`, `is_plateau={report.plateau.is_plateau}` — {report.plateau.reason})",
            f"- **Best Overall Policy**: `{report.best_policy_name}` (`{report.best_policy_mean_r:+.4f}R`)",
            f"- **Stop Condition (`all policies < -0.15R`)**: "
            f"**{'TRIGGERED — HALT C–E' if report.stop_condition_triggered else 'PASSED — PROCEED TO ITEMS 7–14'}**",
            "",
            "## 3. Protective-Stop Counterfactual (`<5m` Exits, Work Item 6)",
            "",
        ]
    )

    cf = report.counterfactual
    if cf.rows:
        se_saved = (
            f"{cf.saved_r_stat.cluster_se:.4f}"
            if not math.isnan(cf.saved_r_stat.cluster_se)
            else "n/a"
        )
        lines.extend(
            [
                f"- **Qualifying `<5m` Protective/Fakeout Exits**: {len(cf.rows)} "
                f"(across {cf.saved_r_stat.n_clusters} wave clusters)",
                f"- **Mean `NetR_actual`**: `{cf.actual_stat.mean:+.4f}R`",
                f"- **Mean `NetR_hold_to_5m`**: `{cf.hold_5m_stat.mean:+.4f}R`",
                f"- **Mean `SavedR` (`NetR_actual - NetR_hold_to_5m`)**: "
                f"**`{cf.saved_r_stat.mean:+.4f}R`** (Cluster SE `{se_saved}`)",
            ]
        )
    else:
        lines.append("- No `<5m` protective-stop exits in sample.")

    return "\n".join(lines)
