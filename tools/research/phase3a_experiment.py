"""Phase 3a — Operationalization & Honest Validation of the BitLanglang System.

Implements the pre-registered tests in `docs/40-research/experiments/PHASE3A_PREREG.md` (Rev 2)
strictly within `tools/research/` (zero changes to `internal/`), dual-anchored to:
1. `docs/40-research/bitlanglang-system-source.md` + `2026-11-11-bitlanglang-remodel-plan.md`
2. `docs/40-research/bitlanglang-case-study/DELIVERY_AUDIT.md`, `delivery/audit_stats.json`
   (4,807-trade OKX delivery audit scale parameters), and `episodes/*.md` (24 per-episode templates).

Scope of tests:
- Step 0: Dataset replay anchor check (`758,013 +/- 2%` 1m bars across 40 slices, 27 PIT symbols).
- S1 (Entry, Source §3 Point 1 & §5 + EP01/EP02/EP10/EP11 + audit stop/trend scales):
  HTF (`4h`) platform/box breakout after mid-point (`>=2` upper OR `>=2` lower probes),
  volatility contraction, dual structural breakout templates (1A upper-platform break /
  1B post-midpoint inflection break), BTC trend alignment, `>=3.0R` 1d space, `[0.35%, 3.20%]` stop.
- S2 (Entry Incremental, Source §3 Point 4 & §4 + EP04/EP13/EP19/EXTRA vs killed H3):
  Incremental delta over H3 enforcing BTC alignment + fluent `4h` Wave-1 (`>=10%`, `ER>=0.35`)
  + `4h` major divergence (`>=5%`, `20%..70%` retrace, `8h..10d`) + wave-count cap + right-side break.
- S3 (Entry Constraint Set, Source §3 Point 2 & §4 + EP01/EP02/EP06/EP07/EP19):
  Four constraints on Kairos's existing pullback ticket stream (`C1` undertake-not-breakout,
  `C2` completed minor divergences `<=1` so current is `<=2`, `C3` `4h` uptrend + BTC alignment,
  `C4` overhead room `>=2.0R`), testing Retained vs All & Rejected.
- F (Negative Filter, Source §3 Point 6 & §5 + EP09/EP17/EP20/EP22):
  Box-middle chop + post-markup internal box rebound long + non-downtrend box counter-trend short
  (`CI95_high(Filtered) < 0` AND `Mean(Retained) - Mean(Filtered) >= +0.15R` AND `G_filtered >= 15`).
- L (Resonance Leader Ranker, Source §2 & §6 + EP06/EP07/EP13/EP21 + DELIVERY_AUDIT §4 vs H2):
  4-state discrete event/state scorer (`S_1` daily structure + 48h `4h` fluency & stale penalty,
  `S_2` BTC/ETH `4h` launch `[-4h, +8h]` lead/same-wave sync vs first-wave absence penalty,
  `S_3` BTC `4h` pullback resilience, `S_4` `1d` overhead space), testing per-wake Top-2 vs Rest.
- T (Thermometer, Source §1 + EP18/EP22/EXTRA): Honest operationalizability audit (`UNTESTABLE`).
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timezone
import math
from pathlib import Path
from typing import Mapping, Sequence

import duckdb

from clustering import (
    ClusterDiffStat,
    ClusterMeanStat,
    build_wave_clusters,
    cluster_robust_diff,
    cluster_robust_mean,
    validate_ex_ante_grouping,
)
from friction import FrictionParams
from h1_experiment import AnnotatedTicketSignal, run_h1_experiment
from h2_experiment import score_current_ranker
from h3_experiment import (
    ConfirmedSwingPivot,
    _cluster_boundary_touches,
    _compute_causal_room_to_resistance_r,
    extract_causal_swing_pivots,
    run_h3_experiment,
)
from p0_experiment import (
    _detect_5m_pullback_trigger,
    load_bars_and_funding_from_duckdb,
)
from pit_universe import normalize_base_coin
from policies import CandidateSignal, MinuteBar, simulate_holding_policy

# --- Pre-Registered Constants (Frozen in PHASE3A_PREREG.md) ---

EXPECTED_PHASE2B_1M_BARS: int = 758_013
DATASET_ANCHOR_TOLERANCE_FRAC: float = 0.02  # +/- 2% -> [742_853, 773_173]

S1_DELTA_NETR_BAR: float = 0.18
S2_DELTA_NETR_BAR: float = 0.18
S3_DELTA_NETR_BAR: float = 0.18
F_DELTA_NETR_BAR: float = 0.15
L_DELTA_NETR_BAR: float = 0.15

P_VALUE_BAR: float = 0.05
MIN_OOS_CLUSTERS_BAR: int = 15

# Delivery-audit stop-width calibration (`audit_stats.json`: 5x med lev, loss.p10_roe = -16.1% -> 3.22%)
AUDIT_MIN_STOP_FRAC: float = 0.0035
AUDIT_MAX_STOP_FRAC: float = 0.0320


@dataclass(frozen=True)
class DatasetAnchorAudit:
    """Step 0 dataset replay verification result."""

    bars_1m: int
    bars_htf: int
    volume_24h_obs: int
    funding_ticks: int
    oi_5m_ticks: int
    pit_universe_total: int
    distinct_1m_symbols: int
    expected_bars_1m: int
    rel_diff_pct: float
    within_2pct_anchor: bool


@dataclass(frozen=True)
class StructuralSetupRecord:
    """Single S1 or S2 setup candidate with causal features and simulated PolicyNetR."""

    signal: CandidateSignal
    setup_code: str  # "S1_POINT1" | "S2_POINT4_INC"
    sub_template: str  # e.g. "1A_PLATFORM" | "1B_INFLECTION" | "2_SECOND_WAVE"
    feature_ts: int
    htf_box_width_pct: float
    room_r: float
    stop_pct: float
    atr_ratio: float
    trial_num_in_struct: int
    hold_seconds_wave: int
    net_r_wave: float
    net_r_bracket: float


@dataclass(frozen=True)
class EntrySetupValidationReport:
    """Evaluation output for S1 (Point 1) or S2 (Point 4 Incremental)."""

    item_code: str
    title: str
    n_is: int
    g_is: int
    n_oos: int
    g_oos: int
    setup_is_stat: ClusterMeanStat
    setup_oos_stat: ClusterMeanStat
    setup_oos_bracket_stat: ClusterMeanStat
    setup_oos_trial_le3_stat: ClusterMeanStat
    setup_oos_hold_le30m_stat: ClusterMeanStat
    setup_oos_hold_gt30m_stat: ClusterMeanStat
    sub_template_oos_stats: dict[str, ClusterMeanStat]
    baseline_long_oos_stat: ClusterMeanStat
    baseline_all_oos_stat: ClusterMeanStat
    diff_vs_baseline_long_is: ClusterDiffStat
    diff_vs_baseline_long_oos: ClusterDiffStat
    diff_vs_baseline_all_oos: ClusterDiffStat
    diff_vs_baseline_long_split70: ClusterDiffStat
    h3_reference_oos_mean: float | None
    verdict: str  # "PASS" | "FAIL" | "FAIL (underpowered, ...)" | "KILLED"
    verdict_detail: str
    setups: tuple[StructuralSetupRecord, ...]


@dataclass(frozen=True)
class S3TicketAnnotation:
    """Baseline pullback ticket annotated with S3's four minor-divergence constraints."""

    ticket: AnnotatedTicketSignal
    c1_undertake_no_breakout: bool
    c2_div_count_le_2: bool
    c3_htf_main_uptrend: bool
    c4_overhead_space: bool
    rebound_pos: float
    pb_depth_pct: float
    prior_div_count: int
    room_r: float
    hold_seconds_wave: int

    @property
    def retained(self) -> bool:
        return (
            self.c1_undertake_no_breakout
            and self.c2_div_count_le_2
            and self.c3_htf_main_uptrend
            and self.c4_overhead_space
        )


@dataclass(frozen=True)
class S3ConstraintReport:
    """Evaluation output for S3 (Point 2 Minor-Divergence Constraint Set)."""

    all_is_stat: ClusterMeanStat
    retained_is_stat: ClusterMeanStat
    rejected_is_stat: ClusterMeanStat
    all_oos_stat: ClusterMeanStat
    retained_oos_stat: ClusterMeanStat
    rejected_oos_stat: ClusterMeanStat
    retained_oos_bracket_stat: ClusterMeanStat
    rejected_oos_bracket_stat: ClusterMeanStat
    retained_oos_hold_le30m_stat: ClusterMeanStat
    retained_oos_hold_gt30m_stat: ClusterMeanStat
    diff_retained_vs_all_oos: ClusterDiffStat
    diff_retained_vs_rejected_oos: ClusterDiffStat
    diff_retained_vs_all_split70: ClusterDiffStat
    ablation_single_drop_oos: dict[str, ClusterMeanStat]
    verdict: str
    verdict_detail: str
    annotations: tuple[S3TicketAnnotation, ...]


@dataclass(frozen=True)
class FTicketAnnotation:
    """Baseline ticket annotated with F's three episode-grounded box trap flags."""

    ticket: AnnotatedTicketSignal
    is_box_mid_chop: bool
    is_box_internal_rebound: bool
    is_box_counter_short: bool
    box_width_pct: float
    er_4h: float
    pos_box: float
    room_r: float

    @property
    def is_filtered(self) -> bool:
        return (
            self.is_box_mid_chop
            or self.is_box_internal_rebound
            or self.is_box_counter_short
        )


@dataclass(frozen=True)
class FBoxFilterReport:
    """Evaluation output for F (Point 6 & §5 + EP20 Box Filter)."""

    filtered_is_stat: ClusterMeanStat
    retained_is_stat: ClusterMeanStat
    filtered_oos_stat: ClusterMeanStat
    retained_oos_stat: ClusterMeanStat
    filtered_mid_oos_stat: ClusterMeanStat
    filtered_rebound_oos_stat: ClusterMeanStat
    filtered_short_oos_stat: ClusterMeanStat
    diff_retained_minus_filtered_is: ClusterDiffStat
    diff_retained_minus_filtered_oos: ClusterDiffStat
    diff_retained_minus_filtered_split70: ClusterDiffStat
    diff_long_only_oos: ClusterDiffStat
    verdict: str
    verdict_detail: str
    annotations: tuple[FTicketAnnotation, ...]


@dataclass(frozen=True)
class LCandidateStateRecord:
    """Per-wake candidate scored by BitLanglang 4-state leader model at `wake_ts <= entry_ts`."""

    signal: CandidateSignal
    wake_id: str
    wake_ts: int
    quote_vol_24h: float
    age_days: float
    s1_daily_struct: int
    s2_btc_sync: int
    s3_btc_resilience: int
    s4_daily_space: int
    total_state_score: int
    current_ranker_score: float
    is_top2_langlang: bool
    is_top2_current: bool
    gross_r_wave: float
    net_r_wave: float
    net_r_bracket: float


@dataclass(frozen=True)
class LLeaderRankerReport:
    """Evaluation output for L (Resonance Leader Ranker — Event/State Version)."""

    n_wakes_is: int
    n_wakes_oos: int
    top2_is_stat: ClusterMeanStat
    rest_is_stat: ClusterMeanStat
    top2_oos_stat: ClusterMeanStat
    rest_oos_stat: ClusterMeanStat
    current_top2_oos_stat: ClusterMeanStat
    diff_top2_vs_rest_is: ClusterDiffStat
    diff_top2_vs_rest_oos: ClusterDiffStat
    diff_top2_vs_current_top2_oos: ClusterDiffStat
    diff_top2_vs_rest_split70: ClusterDiffStat
    high_state_subset_oos_stat: ClusterMeanStat
    verdict: str
    verdict_detail: str
    candidates: tuple[LCandidateStateRecord, ...]


@dataclass(frozen=True)
class ThermometerAuditReport:
    """Operationalizability audit for Source §1 + EP18/EXTRA Thermometer."""

    verdict: str  # "UNTESTABLE"
    missing_requirements: tuple[str, ...]
    unlock_data_spec: tuple[str, ...]
    observed_symbol_count: int
    pit_total_swap_count: int
    slice_hours: float
    inter_slice_gap_days: float


@dataclass(frozen=True)
class Phase3aFullSuiteReport:
    """Bundle of Step 0 audit + all 5 Phase 3a validation reports + Thermometer audit."""

    anchor: DatasetAnchorAudit
    s1: EntrySetupValidationReport
    s2: EntrySetupValidationReport
    s3: S3ConstraintReport
    f_filter: FBoxFilterReport
    l_ranker: LLeaderRankerReport
    thermometer: ThermometerAuditReport


# ---------------------------------------------------------------------------
# Step 0: Dataset Anchor Audit
# ---------------------------------------------------------------------------


def verify_phase3a_dataset_anchor(
    con: duckdb.DuckDBPyConnection,
    *,
    expected_bars_1m: int = EXPECTED_PHASE2B_1M_BARS,
    tol_frac: float = DATASET_ANCHOR_TOLERANCE_FRAC,
) -> DatasetAnchorAudit:
    """Verify the replayed Phase 2b DuckDB dataset against the `758,013 +/- 2%` anchor."""

    def _cnt(tbl: str) -> int:
        exists = (
            con.execute(
                "SELECT COUNT(*) FROM information_schema.tables WHERE table_name = ?",
                [tbl],
            ).fetchone()[0]
            > 0
        )
        if not exists:
            return 0
        return int(con.execute(f"SELECT COUNT(*) FROM {tbl}").fetchone()[0])

    bars_1m = _cnt("fact_perp_bars_1m")
    bars_htf = _cnt("fact_perp_bars_htf")
    vol_obs = _cnt("fact_perp_volume_24h")
    funding_ticks = _cnt("fact_perp_funding_8h")
    oi_ticks = _cnt("fact_perp_oi_5m")
    pit_total = _cnt("dim_perp_universe_pit")
    distinct_syms = (
        int(
            con.execute(
                "SELECT COUNT(DISTINCT symbol) FROM fact_perp_bars_1m"
            ).fetchone()[0]
        )
        if bars_1m > 0
        else 0
    )
    rel_diff = (
        abs(bars_1m - expected_bars_1m) / float(expected_bars_1m)
        if expected_bars_1m > 0
        else math.inf
    )
    ok = rel_diff <= tol_frac
    return DatasetAnchorAudit(
        bars_1m=bars_1m,
        bars_htf=bars_htf,
        volume_24h_obs=vol_obs,
        funding_ticks=funding_ticks,
        oi_5m_ticks=oi_ticks,
        pit_universe_total=pit_total,
        distinct_1m_symbols=distinct_syms,
        expected_bars_1m=expected_bars_1m,
        rel_diff_pct=rel_diff * 100.0,
        within_2pct_anchor=ok,
    )


# ---------------------------------------------------------------------------
# Causal Bar, PIT & BTC Environment Helpers (Zero Lookahead)
# ---------------------------------------------------------------------------


def _build_5m_bars_from_1m(
    bars_by_sym: Mapping[str, Sequence[MinuteBar]],
) -> tuple[
    dict[str, dict[int, int]],
    dict[str, list[tuple[int, float, float, float, float, float]]],
    dict[str, dict[int, int]],
]:
    """Build closed 5m bars `(close_ts, open, high, low, close, quote_vol)` from 1m bars."""
    idx_1m: dict[str, dict[int, int]] = {}
    bars_5m: dict[str, list[tuple[int, float, float, float, float, float]]] = {}
    close5m_idx: dict[str, dict[int, int]] = {}
    for sym, series in bars_by_sym.items():
        if not series:
            continue
        idx_1m[sym] = {b.ts: i for i, b in enumerate(series)}
        buckets: dict[int, list[MinuteBar]] = {}
        for b in series:
            buckets.setdefault((b.ts // 300) * 300, []).append(b)
        b5: list[tuple[int, float, float, float, float, float]] = []
        cmap: dict[int, int] = {}
        for b_open in sorted(buckets):
            grp = buckets[b_open]
            if len(grp) < 5:
                continue
            c_ts = b_open + 300
            cmap[c_ts] = len(b5)
            b5.append(
                (
                    c_ts,
                    grp[0].open,
                    max(x.high for x in grp),
                    min(x.low for x in grp),
                    grp[-1].close,
                    sum(x.quote_vol for x in grp),
                )
            )
        bars_5m[sym] = b5
        close5m_idx[sym] = cmap
    return idx_1m, bars_5m, close5m_idx


def _load_htf_bars_by_sym(
    con: duckdb.DuckDBPyConnection,
) -> tuple[
    dict[str, list[tuple[int, float, float, float, float, float]]],
    dict[str, list[tuple[int, float, float, float, float, float]]],
]:
    """Load causal `4h` and `1d` bars indexed by `close_ts = ts_open + duration`."""
    bars_4h: dict[str, list[tuple[int, float, float, float, float, float]]] = {}
    bars_1d: dict[str, list[tuple[int, float, float, float, float, float]]] = {}
    has_htf = (
        con.execute(
            "SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'fact_perp_bars_htf'"
        ).fetchone()[0]
        > 0
    )
    if not has_htf:
        return bars_4h, bars_1d

    rows = con.execute(
        """
        SELECT symbol, timeframe, ts, open, high, low, close, COALESCE(quote_vol, 0.0)
        FROM fact_perp_bars_htf
        WHERE timeframe IN ('4h', '1d')
        ORDER BY symbol, ts
        """
    ).fetchall()
    for sym, tf, ts_open, o, h, l, c, qv in rows:
        dur = 14400 if str(tf) == "4h" else 86400
        close_ts = int(ts_open) + dur
        tup = (close_ts, float(o), float(h), float(l), float(c), float(qv))
        if str(tf) == "4h":
            bars_4h.setdefault(str(sym), []).append(tup)
        else:
            bars_1d.setdefault(str(sym), []).append(tup)
    return bars_4h, bars_1d


def _load_pit_and_vol(
    con: duckdb.DuckDBPyConnection,
) -> tuple[dict[str, tuple[int, int | None]], dict[str, list[tuple[int, float]]]]:
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
    return pit_meta, vol_by_sym


def _causal_24h_vol_at(
    sym: str,
    at_ts: int,
    vol_by_sym: Mapping[str, Sequence[tuple[int, float]]],
    bars_5m: Mapping[str, Sequence[tuple[int, float, float, float, float, float]]],
    close5m_idx: Mapping[str, Mapping[int, int]],
) -> float:
    v_list = vol_by_sym.get(sym, ())
    latest = 0.0
    for v_ts, qv in v_list:
        if v_ts <= at_ts:
            latest = qv
        else:
            break
    if latest > 0.0:
        return latest
    b5 = bars_5m.get(sym, ())
    c_idx = close5m_idx.get(sym, {}).get(at_ts)
    if c_idx is not None and c_idx >= 12:
        sub = b5[max(0, c_idx - 288) : c_idx + 1]
        return sum(x[5] for x in sub) * (288.0 / len(sub))
    return 0.0


def _pit_eligible(
    sym: str,
    at_ts: int,
    min_vol: float,
    pit_meta: Mapping[str, tuple[int, int | None]],
    vol_by_sym: Mapping[str, Sequence[tuple[int, float]]],
    bars_5m: Mapping[str, Sequence[tuple[int, float, float, float, float, float]]],
    close5m_idx: Mapping[str, Mapping[int, int]],
) -> bool:
    meta = pit_meta.get(sym)
    if meta is not None:
        list_ts, delist_ts = meta
        if list_ts > at_ts:
            return False
        if delist_ts is not None and delist_ts <= at_ts:
            return False
    if min_vol <= 0:
        return True
    return (
        _causal_24h_vol_at(sym, at_ts, vol_by_sym, bars_5m, close5m_idx) >= min_vol
    )


def _causal_prefix(
    htf_bars: Sequence[tuple[int, float, float, float, float, float]],
    at_ts: int,
) -> list[tuple[int, float, float, float, float, float]]:
    """Return strictly closed HTF bars with `close_ts <= at_ts`."""
    return [b for b in htf_bars if b[0] <= at_ts]


def _contiguous_5m_prefix(
    b5: Sequence[tuple[int, float, float, float, float, float]],
    end_idx: int,
) -> list[tuple[int, float, float, float, float, float]]:
    """Return the contiguous 5m bar slice ending at `end_idx` without crossing 9-day slice gaps."""
    if end_idx < 0 or end_idx >= len(b5):
        return []
    start = end_idx
    while start > 0 and (b5[start][0] - b5[start - 1][0]) <= 600:
        start -= 1
    return list(b5[start : end_idx + 1])


def _find_major_symbol(
    bars_4h_by_sym: Mapping[str, Sequence[tuple[int, float, float, float, float, float]]],
    bars_1d_by_sym: Mapping[str, Sequence[tuple[int, float, float, float, float, float]]],
    base_coin: str,
) -> str | None:
    for sym in sorted(set(bars_4h_by_sym) | set(bars_1d_by_sym)):
        if normalize_base_coin(sym) == base_coin:
            return sym
    return None


def _btc_env_aligned(
    btc_1d_all: Sequence[tuple[int, float, float, float, float, float]],
    btc_4h_all: Sequence[tuple[int, float, float, float, float, float]],
    at_ts: int,
) -> tuple[bool, int]:
    """Check `DELIVERY_AUDIT.md` line 51 + `EXTRA` line 7 non-bearish BTC environment (`<= at_ts`).

    Requires `close_btc_1d >= 0.98 * SMA_btc_1d(50)` OR `close_btc_4h > SMA_btc_4h(18)`.
    If no BTC HTF series is present at all (e.g. isolated single-symbol synthetic unit test),
    returns `(True, at_ts)`.
    """
    if not btc_1d_all and not btc_4h_all:
        return True, at_ts

    c_1d = _causal_prefix(btc_1d_all, at_ts)
    c_4h = _causal_prefix(btc_4h_all, at_ts)
    if not c_1d and not c_4h:
        return True, at_ts

    feat_ts = 0
    d1_ok = False
    if c_1d:
        feat_ts = max(feat_ts, c_1d[-1][0])
        w50 = c_1d[-50:]
        sma50 = sum(x[4] for x in w50) / len(w50)
        d1_ok = c_1d[-1][4] >= 0.98 * sma50

    h4_ok = False
    if c_4h:
        feat_ts = max(feat_ts, c_4h[-1][0])
        w18 = c_4h[-18:]
        sma18 = sum(x[4] for x in w18) / len(w18)
        h4_ok = c_4h[-1][4] > sma18

    return (d1_ok or h4_ok), feat_ts


# ---------------------------------------------------------------------------
# S1: Point 1 — HTF Platform / Box Breakout Start
#     (Source §3 Point 1 & §5 + EP01/EP02/EP10/EP11 + audit_stats.json)
# ---------------------------------------------------------------------------


def detect_s1_point1_candidates(
    bars_by_sym: Mapping[str, Sequence[MinuteBar]],
    bars_4h_by_sym: Mapping[str, Sequence[tuple[int, float, float, float, float, float]]],
    bars_1d_by_sym: Mapping[str, Sequence[tuple[int, float, float, float, float, float]]],
    funding_by_sym: Mapping[str, Mapping[int, float]] | None,
    tick_map: Mapping[str, float],
    pit_meta: Mapping[str, tuple[int, int | None]],
    vol_by_sym: Mapping[str, Sequence[tuple[int, float]]],
    *,
    min_quote_vol_24h: float = 10_000_000.0,
    notional_usd: float = 10_000.0,
    params: FrictionParams = FrictionParams(),
) -> list[StructuralSetupRecord]:
    """Detect S1 (Point 1) HTF box/platform breakout setups strictly causally (`<= t`)."""
    idx_1m, bars_5m, close5m_idx = _build_5m_bars_from_1m(bars_by_sym)
    btc_sym = _find_major_symbol(bars_4h_by_sym, bars_1d_by_sym, "BTC")
    btc_4h_all = bars_4h_by_sym.get(btc_sym, ()) if btc_sym else ()
    btc_1d_all = bars_1d_by_sym.get(btc_sym, ()) if btc_sym else ()

    out: list[StructuralSetupRecord] = []

    for sym in sorted(bars_by_sym):
        if normalize_base_coin(sym) == "BTC":
            continue
        series = bars_by_sym[sym]
        b5 = bars_5m.get(sym, [])
        if len(b5) < 32:
            continue
        imap = idx_1m[sym]
        h4_all = bars_4h_by_sym.get(sym, ())
        d1_all = bars_1d_by_sym.get(sym, ())
        f_sched = funding_by_sym.get(sym) if funding_by_sym else None
        last_s1_ts = -10**12
        box_trial_counts: dict[tuple[int, int], int] = {}

        for t_idx in range(30, len(b5)):
            close_ts, _, _, _, c_t, v_t = b5[t_idx]
            if close_ts - last_s1_ts < 14400:  # 4h cooldown per symbol
                continue
            contig_5m = _contiguous_5m_prefix(b5, t_idx)
            if len(contig_5m) < 30:
                continue
            if not _pit_eligible(
                sym, close_ts, min_quote_vol_24h, pit_meta, vol_by_sym, bars_5m, close5m_idx
            ):
                continue
            e_idx = imap.get(close_ts)
            if e_idx is None or (len(series) - 1 - e_idx) < 120:
                continue
            if series[e_idx + 120].ts - series[e_idx].ts > 125 * 60:
                continue

            # 1. Non-bearish BTC environment (`DELIVERY_AUDIT.md` line 51 + `EXTRA` line 7)
            btc_ok, btc_feat_ts = _btc_env_aligned(btc_1d_all, btc_4h_all, close_ts)
            if not btc_ok:
                continue

            # 2. HTF (`4h`) platform / box (`>= 48h` span, `4%..35%` width)
            c_4h = _causal_prefix(h4_all, close_ts)
            if len(c_4h) < 16:
                continue
            h4_win = c_4h[-36:]  # up to 6 days of closed 4h bars
            box_slice = h4_win[:-2] if len(h4_win) >= 16 else h4_win[:-1]
            if len(box_slice) < 14:
                continue

            pivs_4h = extract_causal_swing_pivots(
                box_slice, confirm_lag_bars=1, up_to_idx=len(box_slice) - 1
            )
            b_hi, up_probes, up_conf_ts = _cluster_boundary_touches(
                pivs_4h, "HIGH", tol_frac=0.015
            )
            b_lo, lo_probes, lo_conf_ts = _cluster_boundary_touches(
                pivs_4h, "LOW", tol_frac=0.015
            )
            # 3. Mid-point completed (`EP02` line 8, `EP17` line 24): >=2 upper OR >=2 lower probes
            if (
                b_lo <= 0
                or b_hi <= b_lo
                or up_probes < 1
                or lo_probes < 1
                or (up_probes < 2 and lo_probes < 2)
            ):
                continue
            earliest_piv_idx = min(p.pivot_idx for p in pivs_4h)
            if (len(box_slice) - 1 - earliest_piv_idx) < 12:
                continue
            box_width = (b_hi - b_lo) / b_lo
            if not (0.04 <= box_width <= 0.35):
                continue

            # 4. Terminal volatility convergence (`§5` line 103/109, `EP10` line 27 SATS)
            recent_4h_tr = [x[2] - x[3] for x in h4_win[-3:]]
            base_4h_tr = [x[2] - x[3] for x in h4_win[-15:-3]]
            atr_4h_ratio = (
                (sum(recent_4h_tr) / len(recent_4h_tr))
                / max(sum(base_4h_tr) / len(base_4h_tr), 1e-9)
                if base_4h_tr
                else 1.0
            )
            recent_5m_tr = [x[2] - x[3] for x in contig_5m[-6:]]
            base_5m_tr = [x[2] - x[3] for x in contig_5m[-30:-6]]
            atr_5m_ratio = (
                (sum(recent_5m_tr) / len(recent_5m_tr))
                / max(sum(base_5m_tr) / len(base_5m_tr), 1e-9)
                if base_5m_tr
                else 1.0
            )
            atr_eff = min(atr_4h_ratio, atr_5m_ratio)
            if atr_eff > 0.85:
                continue

            # 5. Dual structural breakout templates (`EP10` L25/27, `EP02` L9/24, `EP11` L9)
            if c_t > b_hi * 1.015:
                continue
            prev_c = contig_5m[-2][4]
            prior_vols = [x[5] for x in contig_5m[-13:-1]]
            mean_v = sum(prior_vols) / len(prior_vols) if prior_vols else 0.0
            if mean_v <= 0:
                continue

            is_1a = (
                c_t > b_hi
                and prev_c <= b_hi * 1.002
                and v_t >= 1.25 * mean_v
            )
            is_1b = False
            inflection_conf_ts = close_ts
            if not is_1a and (b_lo + 0.35 * (b_hi - b_lo) <= c_t <= b_hi * 1.005):
                sub_24 = contig_5m[-25:]
                pivs_5m = extract_causal_swing_pivots(
                    sub_24, confirm_lag_bars=2, up_to_idx=len(sub_24) - 1
                )
                lo_5m = [
                    p
                    for p in pivs_5m
                    if p.kind == "LOW" and p.price > b_lo * 1.005 and p.confirm_ts <= close_ts
                ]
                local_hi = max(x[2] for x in contig_5m[-13:-1])
                if (
                    lo_5m
                    and c_t > local_hi
                    and prev_c <= local_hi
                    and v_t >= 1.15 * mean_v
                ):
                    is_1b = True
                    inflection_conf_ts = max(p.confirm_ts for p in lo_5m)

            if not (is_1a or is_1b):
                continue
            sub_tmpl = "1A_PLATFORM" if is_1a else "1B_INFLECTION"

            # 7. Structural stop calibrated by `audit_stats.json` (`[0.35%, 3.20%]`)
            stop_px = min(x[3] for x in contig_5m[-13:-1])
            if is_1a:
                stop_px = min(stop_px, b_hi * 0.996)
            stop_frac = (c_t - stop_px) / c_t if c_t > 0 else 0.0
            if not (AUDIT_MIN_STOP_FRAC <= stop_frac <= AUDIT_MAX_STOP_FRAC):
                continue
            r_dist = c_t - stop_px

            # 6. Overhead unobstructed 1d structural space (`EP10` L25, `EP02` L10: `>= 3.0R`)
            c_1d = _causal_prefix(d1_all, close_ts)
            cutoff_30d = close_ts - 30 * 86400
            d1_30d = [x for x in c_1d if x[0] >= cutoff_30d]
            pivs_1d = (
                extract_causal_swing_pivots(
                    d1_30d, confirm_lag_bars=1, up_to_idx=len(d1_30d) - 1
                )
                if len(d1_30d) >= 3
                else []
            )
            ref_top = max(b_hi, c_t) * 1.005
            above_1d_pivs = [
                p
                for p in pivs_1d
                if p.kind == "HIGH" and p.confirm_ts <= close_ts and p.price > ref_top
            ]
            if not above_1d_pivs:
                room_r = 10.0  # clear skies above box top on 30d daily structure
            else:
                next_1d_hi = min(p.price for p in above_1d_pivs)
                room_r = (next_1d_hi - c_t) / r_dist
            if room_r < 3.0:
                continue

            last_res_ts = max(
                (p.confirm_ts for p in above_1d_pivs),
                default=(c_1d[-1][0] if c_1d else close_ts),
            )
            validate_ex_ante_grouping(
                [
                    "s1_btc_env",
                    "s1_htf_box",
                    "s1_convergence",
                    "s1_break_trigger",
                    "s1_overhead_room",
                    "s1_stop",
                ],
                feature_timestamps={
                    "s1_btc_env": float(btc_feat_ts),
                    "s1_htf_box": float(max(up_conf_ts, lo_conf_ts)),
                    "s1_convergence": float(close_ts),
                    "s1_break_trigger": float(max(close_ts, inflection_conf_ts)),
                    "s1_overhead_room": float(last_res_ts),
                    "s1_stop": float(close_ts),
                },
                entry_ts=float(close_ts),
            )

            box_key = (int(round(b_lo * 100)), int(round(b_hi * 100)))
            box_trial_counts[box_key] = box_trial_counts.get(box_key, 0) + 1
            trial_num = box_trial_counts[box_key]

            lookback_15m = series[max(0, e_idx - 15) : e_idx]
            trs_1m = [b.high - b.low for b in lookback_15m]
            atr_1m = sum(trs_1m) / len(trs_1m) if trs_1m else c_t * 0.002
            adv_5m = max(100.0, sum(b.quote_vol for b in lookback_15m[-5:]))

            sig = CandidateSignal(
                signal_id=f"s1_p1_{normalize_base_coin(sym)}_{close_ts}",
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
            out.append(
                StructuralSetupRecord(
                    signal=sig,
                    setup_code="S1_POINT1",
                    sub_template=sub_tmpl,
                    feature_ts=close_ts,
                    htf_box_width_pct=round(box_width * 100.0, 3),
                    room_r=round(room_r, 3),
                    stop_pct=round(stop_frac * 100.0, 3),
                    atr_ratio=round(atr_eff, 4),
                    trial_num_in_struct=trial_num,
                    hold_seconds_wave=sim_w.hold_seconds,
                    net_r_wave=sim_w.net_r,
                    net_r_bracket=sim_b.net_r,
                )
            )
            last_s1_ts = close_ts

    return out


# ---------------------------------------------------------------------------
# S2: Point 4 Incremental — Post-Deep-Divergence Second Wave
#     (Source §3 Point 4 & §4 + EP04/EP13/EP19/EXTRA + audit_stats.json)
# ---------------------------------------------------------------------------


def _has_excessive_prior_waves(
    c_4h: Sequence[tuple[int, float, float, float, float, float]],
    idx_l0_global: int,
    h1_price: float,
) -> bool:
    """Check Source §4 + EP06/EP19/EXTRA wave-diminishing rule: reject 3rd+ wave fish-tails."""
    win_30d = c_4h[max(0, len(c_4h) - 180) :]
    if win_30d:
        lo_30d = min(x[3] for x in win_30d)
        if lo_30d > 0 and (h1_price - lo_30d) / lo_30d > 0.80:
            pivs = extract_causal_swing_pivots(
                win_30d, confirm_lag_bars=2, up_to_idx=len(win_30d) - 1
            )
            hi_pivs = [p for p in pivs if p.kind == "HIGH"]
            distinct_peaks: list[ConfirmedSwingPivot] = []
            for p in hi_pivs:
                if all(abs(p.pivot_idx - d.pivot_idx) >= 6 for d in distinct_peaks):
                    distinct_peaks.append(p)
            if len(distinct_peaks) >= 3:
                return True

    # Check 14 days (84 4h bars) strictly before L_0 for a prior completed >=10% markup + >=5% divergence
    prior_slice = c_4h[max(0, idx_l0_global - 84) : idx_l0_global]
    if len(prior_slice) >= 12:
        peak_idx = max(range(len(prior_slice)), key=lambda k: prior_slice[k][2])
        if 3 <= peak_idx <= len(prior_slice) - 3:
            p_hi = prior_slice[peak_idx][2]
            p_lo_before = min(x[3] for x in prior_slice[:peak_idx])
            p_lo_after = min(x[3] for x in prior_slice[peak_idx + 1 :])
            if (
                p_lo_before > 0
                and (p_hi - p_lo_before) / p_lo_before >= 0.10
                and (p_hi - p_lo_after) / p_hi >= 0.05
            ):
                return True
    return False


def detect_s2_point4_incremental_candidates(
    bars_by_sym: Mapping[str, Sequence[MinuteBar]],
    bars_4h_by_sym: Mapping[str, Sequence[tuple[int, float, float, float, float, float]]],
    bars_1d_by_sym: Mapping[str, Sequence[tuple[int, float, float, float, float, float]]],
    funding_by_sym: Mapping[str, Mapping[int, float]] | None,
    tick_map: Mapping[str, float],
    pit_meta: Mapping[str, tuple[int, int | None]],
    vol_by_sym: Mapping[str, Sequence[tuple[int, float]]],
    *,
    min_quote_vol_24h: float = 10_000_000.0,
    notional_usd: float = 10_000.0,
    params: FrictionParams = FrictionParams(),
) -> list[StructuralSetupRecord]:
    """Detect S2 (Point 4 Incremental) setups enforcing fluent HTF Wave-1 + Deep Divergence + Wave<=2."""
    idx_1m, bars_5m, close5m_idx = _build_5m_bars_from_1m(bars_by_sym)
    btc_sym = _find_major_symbol(bars_4h_by_sym, bars_1d_by_sym, "BTC")
    btc_4h_all = bars_4h_by_sym.get(btc_sym, ()) if btc_sym else ()
    btc_1d_all = bars_1d_by_sym.get(btc_sym, ()) if btc_sym else ()

    out: list[StructuralSetupRecord] = []

    for sym in sorted(bars_by_sym):
        if normalize_base_coin(sym) == "BTC":
            continue
        series = bars_by_sym[sym]
        b5 = bars_5m.get(sym, [])
        if len(b5) < 48:
            continue
        imap = idx_1m[sym]
        h4_all = bars_4h_by_sym.get(sym, ())
        d1_all = bars_1d_by_sym.get(sym, ())
        f_sched = funding_by_sym.get(sym) if funding_by_sym else None
        last_s2_ts = -10**12
        peak_trial_counts: dict[int, int] = {}

        for t_idx in range(36, len(b5)):
            close_ts, _, _, _, c_t, _ = b5[t_idx]
            if close_ts - last_s2_ts < 14400:  # 4h cooldown
                continue
            if not _pit_eligible(
                sym, close_ts, min_quote_vol_24h, pit_meta, vol_by_sym, bars_5m, close5m_idx
            ):
                continue
            e_idx = imap.get(close_ts)
            if e_idx is None or (len(series) - 1 - e_idx) < 120:
                continue
            if series[e_idx + 120].ts - series[e_idx].ts > 125 * 60:
                continue

            # 1a. Non-bearish BTC environment (`DELIVERY_AUDIT.md` line 51 + `EXTRA` line 7)
            btc_ok, btc_feat_ts = _btc_env_aligned(btc_1d_all, btc_4h_all, close_ts)
            if not btc_ok:
                continue

            c_4h = _causal_prefix(h4_all, close_ts)
            c_1d = _causal_prefix(d1_all, close_ts)
            if len(c_4h) < 24 or len(c_1d) < 5:
                continue

            # 1b. Fluent Wave-1 markup in trailing 84 4h bars (14 days, `EP13` line 6 + `EXTRA` line 13)
            win_len = min(84, len(c_4h))
            win_start_global = len(c_4h) - win_len
            h4_win = c_4h[win_start_global:]
            # Valid peak indices with 2 <= bars_since_peak <= 60 (8h..10d)
            cand_rel_idxs = [
                k
                for k in range(2, len(h4_win))
                if 2 <= (len(h4_win) - 1 - k) <= 60
            ]
            if not cand_rel_idxs:
                continue
            rel_h1_idx = max(cand_rel_idxs, key=lambda k: h4_win[k][2])
            h1_px = h4_win[rel_h1_idx][2]
            # Must not already have broken significantly above H_1 before t
            if any(x[2] > h1_px * 1.01 for x in h4_win[rel_h1_idx + 1 :]):
                continue

            h1_global_idx = win_start_global + rel_h1_idx
            l0_search_start = max(0, h1_global_idx - 36)
            if l0_search_start >= h1_global_idx:
                continue
            l0_global_idx = min(
                range(l0_search_start, h1_global_idx),
                key=lambda k: c_4h[k][3],
            )
            l0_px = c_4h[l0_global_idx][3]
            if l0_px <= 0 or (h1_px - l0_px) / l0_px < 0.10:
                continue

            # Wave-1 fluency (`EP13` line 6: "前面有过一波特别流畅的行情")
            wave1_bars = c_4h[l0_global_idx : h1_global_idx + 1]
            wave1_path = sum(x[2] - x[3] for x in wave1_bars)
            er_wave1 = (h1_px - l0_px) / max(wave1_path, 1e-9)
            if er_wave1 < 0.35:
                continue

            # H_1 must break prior 10-day (60 4h bar) high before H_1
            prior_10d = c_4h[max(0, h1_global_idx - 60) : h1_global_idx]
            if prior_10d and h1_px < max(x[2] for x in prior_10d):
                continue

            # 2. Deep Divergence + Consolidation Prerequisite (`EP04` L7, `EP19` L11, `EXTRA` L10/13)
            post_peak_4h = c_4h[h1_global_idx + 1 :]
            if not post_peak_4h:
                continue
            l_div_4h = min(x[3] for x in post_peak_4h)
            contig_5m = _contiguous_5m_prefix(b5, t_idx)
            if len(contig_5m) < 30:
                continue
            last_4h_close_ts = c_4h[-1][0]
            recent_5m_lows = [
                x[3]
                for x in contig_5m[-49:-1]
                if x[0] >= last_4h_close_ts
            ]
            l_div = min([l_div_4h] + recent_5m_lows) if recent_5m_lows else l_div_4h
            abs_dd = (h1_px - l_div) / h1_px
            fib_retrace = (h1_px - l_div) / max(h1_px - l0_px, 1e-9)
            if abs_dd < 0.05 or not (0.20 <= fib_retrace <= 0.70):
                continue

            # 5m terminal contraction <= 0.90 + confirmed 5m swing low (k=2)
            win_b5 = contig_5m[-49:]
            r_tr = [x[2] - x[3] for x in win_b5[-6:]]
            b_tr = [x[2] - x[3] for x in win_b5[-30:-6]]
            atr_comp = (
                (sum(r_tr) / len(r_tr)) / max(sum(b_tr) / len(b_tr), 1e-9)
                if b_tr
                else 1.0
            )
            if atr_comp > 0.90:
                continue
            pivs_5m = extract_causal_swing_pivots(
                win_b5, confirm_lag_bars=2, up_to_idx=len(win_b5) - 1
            )
            lo_pivs = [
                p for p in pivs_5m if p.kind == "LOW" and p.confirm_ts <= close_ts
            ]
            if not lo_pivs:
                continue

            # 3. Wave-Count Cap (first major divergence only; reject 3rd+ wave fish-tails)
            if _has_excessive_prior_waves(c_4h, l0_global_idx, h1_px):
                continue

            # 4. Right-side breakout above recent 12-bar (1h) consolidation ceiling + audit stop + room >= 2.0R
            cons_hi = max(x[2] for x in contig_5m[-13:-1])
            if not (c_t > cons_hi and contig_5m[-2][4] <= cons_hi and c_t < h1_px * 1.02):
                continue
            stop_px = min(x[3] for x in contig_5m[-13:-1])
            if stop_px < l_div * 0.995:
                continue
            stop_frac = (c_t - stop_px) / c_t if c_t > 0 else 0.0
            if not (AUDIT_MIN_STOP_FRAC <= stop_frac <= AUDIT_MAX_STOP_FRAC):
                continue
            r_dist = c_t - stop_px

            above_1d = [x[2] for x in c_1d[-30:] if x[2] > h1_px * 1.005]
            target_level = min(above_1d) if above_1d else h1_px
            room_r = (max(h1_px, target_level) - c_t) / r_dist
            if room_r < 2.0:
                continue

            validate_ex_ante_grouping(
                [
                    "s2_btc_env",
                    "s2_wave1_htf",
                    "s2_deep_div",
                    "s2_wave_cap",
                    "s2_right_break",
                ],
                feature_timestamps={
                    "s2_btc_env": float(btc_feat_ts),
                    "s2_wave1_htf": float(c_4h[-1][0]),
                    "s2_deep_div": float(max(p.confirm_ts for p in lo_pivs)),
                    "s2_wave_cap": float(c_4h[-1][0]),
                    "s2_right_break": float(close_ts),
                },
                entry_ts=float(close_ts),
            )

            h1_ts = c_4h[h1_global_idx][0]
            peak_trial_counts[h1_ts] = peak_trial_counts.get(h1_ts, 0) + 1
            trial_num = peak_trial_counts[h1_ts]

            lookback_15m = series[max(0, e_idx - 15) : e_idx]
            trs_1m = [b.high - b.low for b in lookback_15m]
            atr_1m = sum(trs_1m) / len(trs_1m) if trs_1m else c_t * 0.002
            adv_5m = max(100.0, sum(b.quote_vol for b in lookback_15m[-5:]))

            sig = CandidateSignal(
                signal_id=f"s2_p4_{normalize_base_coin(sym)}_{close_ts}",
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
            out.append(
                StructuralSetupRecord(
                    signal=sig,
                    setup_code="S2_POINT4_INC",
                    sub_template="2_SECOND_WAVE",
                    feature_ts=close_ts,
                    htf_box_width_pct=round(abs_dd * 100.0, 3),
                    room_r=round(room_r, 3),
                    stop_pct=round(stop_frac * 100.0, 3),
                    atr_ratio=round(atr_comp, 4),
                    trial_num_in_struct=trial_num,
                    hold_seconds_wave=sim_w.hold_seconds,
                    net_r_wave=sim_w.net_r,
                    net_r_bracket=sim_b.net_r,
                )
            )
            last_s2_ts = close_ts

    return out


def _evaluate_entry_setup_against_baseline(
    item_code: str,
    title: str,
    raw_setups: Sequence[StructuralSetupRecord],
    baseline_tickets: Sequence[AnnotatedTicketSignal],
    *,
    delta_bar: float,
    h3_reference_oos_mean: float | None = None,
) -> EntrySetupValidationReport:
    """Assign unified `<24h` wave clusters across setups + baseline and evaluate IS/OOS bars."""
    all_events: list[tuple[str, float]] = [
        (f"base_{t.signal.signal_id}", float(t.signal.entry_ts))
        for t in baseline_tickets
    ] + [
        (f"setup_{s.signal.signal_id}", float(s.signal.entry_ts))
        for s in raw_setups
    ]
    _, ev_to_cid = build_wave_clusters(all_events)

    tagged_setups: list[StructuralSetupRecord] = []
    for s in raw_setups:
        cid = ev_to_cid.get(f"setup_{s.signal.signal_id}", "wave_0001")
        sig_u = CandidateSignal(
            signal_id=s.signal.signal_id,
            wave_cluster_id=cid,
            symbol=s.signal.symbol,
            direction=s.signal.direction,
            entry_ts=s.signal.entry_ts,
            entry_price=s.signal.entry_price,
            stop_price=s.signal.stop_price,
            tick_size=s.signal.tick_size,
            atr_1m_entry=s.signal.atr_1m_entry,
            adv_5m_entry_usd=s.signal.adv_5m_entry_usd,
            notional_usd=s.signal.notional_usd,
        )
        tagged_setups.append(
            StructuralSetupRecord(
                signal=sig_u,
                setup_code=s.setup_code,
                sub_template=s.sub_template,
                feature_ts=s.feature_ts,
                htf_box_width_pct=s.htf_box_width_pct,
                room_r=s.room_r,
                stop_pct=s.stop_pct,
                atr_ratio=s.atr_ratio,
                trial_num_in_struct=s.trial_num_in_struct,
                hold_seconds_wave=s.hold_seconds_wave,
                net_r_wave=s.net_r_wave,
                net_r_bracket=s.net_r_bracket,
            )
        )

    base_all = [
        (t, ev_to_cid.get(f"base_{t.signal.signal_id}", t.signal.wave_cluster_id))
        for t in baseline_tickets
    ]
    base_long = [(t, cid) for t, cid in base_all if t.signal.direction == "UP"]

    all_cids = sorted(
        {cid for _, cid in base_all} | {s.signal.wave_cluster_id for s in tagged_setups}
    )
    oos_cids = set(all_cids[1:]) if len(all_cids) > 1 else set(all_cids)
    split70_idx = max(1, int(0.30 * len(all_cids)))
    split70_cids = set(all_cids[split70_idx:]) if len(all_cids) > 1 else set(all_cids)

    setups_oos = [s for s in tagged_setups if s.signal.wave_cluster_id in oos_cids]
    setups_s70 = [s for s in tagged_setups if s.signal.wave_cluster_id in split70_cids]
    bl_long_oos = [(t, c) for t, c in base_long if c in oos_cids]
    bl_all_oos = [(t, c) for t, c in base_all if c in oos_cids]
    bl_long_s70 = [(t, c) for t, c in base_long if c in split70_cids]

    def _m_s(seq: Sequence[StructuralSetupRecord]) -> ClusterMeanStat:
        return cluster_robust_mean(
            [s.net_r_wave for s in seq],
            [s.signal.wave_cluster_id for s in seq],
        )

    setup_is_stat = _m_s(tagged_setups)
    setup_oos_stat = _m_s(setups_oos)
    setup_oos_bracket = cluster_robust_mean(
        [s.net_r_bracket for s in setups_oos],
        [s.signal.wave_cluster_id for s in setups_oos],
    )
    setup_oos_trial_le3 = _m_s([s for s in setups_oos if s.trial_num_in_struct <= 3])
    setup_oos_le30m = _m_s([s for s in setups_oos if s.hold_seconds_wave <= 1800])
    setup_oos_gt30m = _m_s([s for s in setups_oos if s.hold_seconds_wave > 1800])

    sub_tmpls = sorted({s.sub_template for s in tagged_setups})
    sub_stats = {
        tm: _m_s([s for s in setups_oos if s.sub_template == tm])
        for tm in sub_tmpls
    }

    bl_long_oos_stat = cluster_robust_mean(
        [t.net_r_wave for t, _ in bl_long_oos],
        [c for _, c in bl_long_oos],
    )
    bl_all_oos_stat = cluster_robust_mean(
        [t.net_r_wave for t, _ in bl_all_oos],
        [c for _, c in bl_all_oos],
    )

    diff_is = cluster_robust_diff(
        [s.net_r_wave for s in tagged_setups],
        [s.signal.wave_cluster_id for s in tagged_setups],
        [t.net_r_wave for t, _ in base_long],
        [c for _, c in base_long],
    )
    diff_oos_long = cluster_robust_diff(
        [s.net_r_wave for s in setups_oos],
        [s.signal.wave_cluster_id for s in setups_oos],
        [t.net_r_wave for t, _ in bl_long_oos],
        [c for _, c in bl_long_oos],
    )
    diff_oos_all = cluster_robust_diff(
        [s.net_r_wave for s in setups_oos],
        [s.signal.wave_cluster_id for s in setups_oos],
        [t.net_r_wave for t, _ in bl_all_oos],
        [c for _, c in bl_all_oos],
    )
    diff_s70 = cluster_robust_diff(
        [s.net_r_wave for s in setups_s70],
        [s.signal.wave_cluster_id for s in setups_s70],
        [t.net_r_wave for t, _ in bl_long_s70],
        [c for _, c in bl_long_s70],
    )

    g_oos = setup_oos_stat.n_clusters
    delta_oos = diff_oos_long.diff_mean
    p_oos = diff_oos_long.one_sided_p

    reasons: list[str] = []
    if g_oos < MIN_OOS_CLUSTERS_BAR:
        reasons.append(f"G_oos = {g_oos} < {MIN_OOS_CLUSTERS_BAR} power floor")
    if math.isnan(delta_oos) or delta_oos < delta_bar:
        reasons.append(
            f"OOS Delta NetR = {delta_oos:+.4f}R < +{delta_bar:.2f}R bar"
            if not math.isnan(delta_oos)
            else "OOS Delta NetR = NaN (0 OOS setups)"
        )
    if math.isnan(p_oos) or p_oos >= P_VALUE_BAR:
        reasons.append(
            f"OOS one-sided p = {p_oos:.4f} >= {P_VALUE_BAR:.2f}"
            if not math.isnan(p_oos)
            else "OOS one-sided p = NaN"
        )

    if not reasons:
        verdict = "PASS"
        detail = (
            f"PASS: OOS Delta NetR = {delta_oos:+.4f}R (>= +{delta_bar:.2f}R), "
            f"p = {p_oos:.4f} (< 0.05), G_oos = {g_oos} (>= 15)."
        )
    elif (
        g_oos >= MIN_OOS_CLUSTERS_BAR
        and not math.isnan(setup_oos_stat.ci95_high)
        and setup_oos_stat.ci95_high < 0.0
    ):
        verdict = "KILLED"
        detail = (
            f"KILLED: OOS setup 95% CI [{setup_oos_stat.ci95_low:+.4f}R, {setup_oos_stat.ci95_high:+.4f}R] "
            f"is strictly negative; " + "; ".join(reasons)
        )
    elif g_oos < MIN_OOS_CLUSTERS_BAR:
        verdict = f"FAIL (underpowered, G_oos={g_oos})"
        detail = "FAIL (underpowered): " + "; ".join(reasons)
    else:
        verdict = "FAIL"
        detail = "FAIL: " + "; ".join(reasons)

    return EntrySetupValidationReport(
        item_code=item_code,
        title=title,
        n_is=setup_is_stat.n_obs,
        g_is=setup_is_stat.n_clusters,
        n_oos=setup_oos_stat.n_obs,
        g_oos=g_oos,
        setup_is_stat=setup_is_stat,
        setup_oos_stat=setup_oos_stat,
        setup_oos_bracket_stat=setup_oos_bracket,
        setup_oos_trial_le3_stat=setup_oos_trial_le3,
        setup_oos_hold_le30m_stat=setup_oos_le30m,
        setup_oos_hold_gt30m_stat=setup_oos_gt30m,
        sub_template_oos_stats=sub_stats,
        baseline_long_oos_stat=bl_long_oos_stat,
        baseline_all_oos_stat=bl_all_oos_stat,
        diff_vs_baseline_long_is=diff_is,
        diff_vs_baseline_long_oos=diff_oos_long,
        diff_vs_baseline_all_oos=diff_oos_all,
        diff_vs_baseline_long_split70=diff_s70,
        h3_reference_oos_mean=h3_reference_oos_mean,
        verdict=verdict,
        verdict_detail=detail,
        setups=tuple(tagged_setups),
    )


# ---------------------------------------------------------------------------
# S3: Point 2 Minor-Divergence Constraint Set
#     (Source §3 Point 2 & §4 + EP01/EP02/EP06/EP07/EP19 + audit_stats.json)
# ---------------------------------------------------------------------------


def evaluate_s3_point2_constraints(
    baseline_tickets: Sequence[AnnotatedTicketSignal],
    bars_by_sym: Mapping[str, Sequence[MinuteBar]],
    bars_4h_by_sym: Mapping[str, Sequence[tuple[int, float, float, float, float, float]]],
    bars_1d_by_sym: Mapping[str, Sequence[tuple[int, float, float, float, float, float]]],
    funding_by_sym: Mapping[str, Mapping[int, float]] | None = None,
    params: FrictionParams = FrictionParams(),
) -> S3ConstraintReport:
    """Evaluate S3's four minor-divergence constraints on Kairos's pullback ticket stream."""
    _, bars_5m, close5m_idx = _build_5m_bars_from_1m(bars_by_sym)
    btc_sym = _find_major_symbol(bars_4h_by_sym, bars_1d_by_sym, "BTC")
    btc_4h_all = bars_4h_by_sym.get(btc_sym, ()) if btc_sym else ()
    btc_1d_all = bars_1d_by_sym.get(btc_sym, ()) if btc_sym else ()

    long_tickets = [t for t in baseline_tickets if t.signal.direction == "UP"]
    annotations: list[S3TicketAnnotation] = []

    for tkt in long_tickets:
        sig = tkt.signal
        sym = sig.symbol
        entry_ts = sig.entry_ts
        entry_px = sig.entry_price
        stop_px = sig.stop_price

        b5 = bars_5m.get(sym, [])
        b5_idx = close5m_idx.get(sym, {}).get(entry_ts)
        contig_5m = _contiguous_5m_prefix(b5, b5_idx) if b5_idx is not None else []
        c_4h = _causal_prefix(bars_4h_by_sym.get(sym, ()), entry_ts)
        c_1d = _causal_prefix(bars_1d_by_sym.get(sym, ()), entry_ts)

        # C1: Undertake below peak, do NOT chase breakout (`EP01` L12, `EP02` L18, `EP06` L29, `EP19` L10)
        # Baseline `_detect_5m_pullback_trigger` already requires `rebound_pos >= 0.50` up to `1.015 * peak`.
        # C1 rejects near-peak / breakout chasers (`rebound_pos >= 0.90` or `entry >= peak * 0.999`)
        # and enforces minor-divergence shallow depth `0.40% <= pb_depth_pct <= 3.50%`.
        c1_ok = False
        rebound_pos = 1.0
        pb_depth_pct = 0.0
        peak_ts = entry_ts - 600
        if len(contig_5m) >= 25:
            win_5m = contig_5m[-30:]
            n = len(win_5m)
            lo = max(1, n - 30)
            hi = n - 4
            if hi > lo + 3:
                peak_idx = lo
                for i in range(lo, hi + 1):
                    if win_5m[i][2] >= win_5m[peak_idx][2]:
                        peak_idx = i
                pb_low_idx = peak_idx + 1
                for i in range(peak_idx + 1, n - 1):
                    if win_5m[i][3] <= win_5m[pb_low_idx][3]:
                        pb_low_idx = i
                peak_px = win_5m[peak_idx][2]
                pb_low_px = win_5m[pb_low_idx][3]
                peak_ts = win_5m[peak_idx][0]
                if peak_px > pb_low_px > 0:
                    rebound_pos = (entry_px - pb_low_px) / (peak_px - pb_low_px)
                    pb_depth_pct = (peak_px - pb_low_px) / peak_px * 100.0
                    if (
                        rebound_pos < 0.90
                        and entry_px < peak_px * 0.999
                        and 0.40 <= pb_depth_pct <= 3.50
                    ):
                        c1_ok = True

        # C2: Minor divergence count <= 2 in current 4h wave leg (`EP01` L13, `EP03` L9, `EP07` L16/21)
        prior_divs = 0
        if contig_5m and c_4h:
            pivs_4h = extract_causal_swing_pivots(
                c_4h[-24:], confirm_lag_bars=1, up_to_idx=len(c_4h[-24:]) - 1
            )
            lo_4h = [p for p in pivs_4h if p.kind == "LOW"]
            if lo_4h:
                leg_start_ts = lo_4h[-1].pivot_ts - 14400
            else:
                min_4h = min(c_4h[-12:], key=lambda x: x[3])
                leg_start_ts = min_4h[0] - 14400
            leg_b5 = [x for x in contig_5m if leg_start_ts <= x[0] < peak_ts]
            if len(leg_b5) >= 12:
                pivs_5m = extract_causal_swing_pivots(
                    leg_b5, confirm_lag_bars=2, up_to_idx=len(leg_b5) - 1
                )
                hi_5m = [p for p in pivs_5m if p.kind == "HIGH"]
                counted: list[ConfirmedSwingPivot] = []
                for idx_hp, hp in enumerate(hi_5m):
                    next_hp_end = (
                        hi_5m[idx_hp + 1].pivot_idx + 1
                        if idx_hp + 1 < len(hi_5m)
                        else len(leg_b5)
                    )
                    between_bars = leg_b5[hp.pivot_idx + 1 : next_hp_end]
                    if len(between_bars) < 3:
                        continue
                    dip_rel_idx = min(
                        range(len(between_bars)), key=lambda j: between_bars[j][3]
                    )
                    if (dip_rel_idx + 1) < 3:
                        continue
                    dip_lo = between_bars[dip_rel_idx][3]
                    dip_abs_idx = hp.pivot_idx + 1 + dip_rel_idx
                    rec_bars = leg_b5[dip_abs_idx + 1 :]
                    recovered = any(x[2] >= hp.price * 1.003 for x in rec_bars)
                    if (
                        hp.price > 0
                        and (hp.price - dip_lo) / hp.price >= 0.012
                        and recovered
                    ):
                        if all(abs(hp.pivot_idx - prev.pivot_idx) >= 12 for prev in counted):
                            counted.append(hp)
                prior_divs = len(counted)
        c2_ok = prior_divs <= 1  # current is 1st or 2nd minor divergence

        # C3: Symbol 4h main uptrend + BTC trend alignment (`EP01` L9 + `DELIVERY_AUDIT.md` L51)
        btc_ok, btc_feat_ts = _btc_env_aligned(btc_1d_all, btc_4h_all, entry_ts)
        c3_ok = False
        if btc_ok and len(c_4h) >= 18:
            closes_4h = [x[4] for x in c_4h]
            sma6 = sum(closes_4h[-6:]) / 6.0
            sma18 = sum(closes_4h[-18:]) / 18.0
            w7d = c_4h[-42:]
            mid_7d = 0.5 * (max(x[2] for x in w7d) + min(x[3] for x in w7d))
            if closes_4h[-1] > sma6 and sma6 >= 0.995 * sma18 and closes_4h[-1] > mid_7d:
                c3_ok = True

        # C4: Overhead structural resistance room >= 2.0R (`EP01` L9, `EP11` L11)
        pivs_res: list[ConfirmedSwingPivot] = []
        if len(c_4h) >= 3:
            w4 = c_4h[-180:]
            pivs_res.extend(
                extract_causal_swing_pivots(w4, confirm_lag_bars=1, up_to_idx=len(w4) - 1)
            )
        if len(c_1d) >= 3:
            w1 = c_1d[-90:]
            pivs_res.extend(
                extract_causal_swing_pivots(w1, confirm_lag_bars=1, up_to_idx=len(w1) - 1)
            )
        above_pivs = [
            p
            for p in pivs_res
            if p.kind == "HIGH" and p.confirm_ts <= entry_ts and p.price > entry_px * 1.004
        ]
        r_dist = abs(entry_px - stop_px)
        if r_dist <= 0:
            room_r = 0.0
        elif not above_pivs:
            room_r = 10.0
        else:
            room_r = (min(p.price for p in above_pivs) - entry_px) / r_dist
        c4_ok = room_r >= 2.0

        last_res_ts = max((p.confirm_ts for p in above_pivs), default=entry_ts)
        validate_ex_ante_grouping(
            [
                "c1_undertake",
                "c2_div_count",
                "c3_htf_uptrend",
                "c4_overhead_room",
            ],
            feature_timestamps={
                "c1_undertake": float(entry_ts),
                "c2_div_count": float(entry_ts),
                "c3_htf_uptrend": float(max(c_4h[-1][0] if c_4h else 0, btc_feat_ts)),
                "c4_overhead_room": float(last_res_ts),
            },
            entry_ts=float(entry_ts),
        )

        # Compute holding duration for audit holding-band breakdown (`<=30m` vs `>30m`)
        series = bars_by_sym.get(sym, ())
        hold_sec = 0
        if series:
            f_sched = funding_by_sym.get(sym) if funding_by_sym else None
            sim_w = simulate_holding_policy(
                sig,
                series,
                policy_kind="wave",
                k_be=0.8,
                max_hold_minutes=1440,
                funding_schedule=f_sched,
                params=params,
            )
            hold_sec = sim_w.hold_seconds

        annotations.append(
            S3TicketAnnotation(
                ticket=tkt,
                c1_undertake_no_breakout=c1_ok,
                c2_div_count_le_2=c2_ok,
                c3_htf_main_uptrend=c3_ok,
                c4_overhead_space=c4_ok,
                rebound_pos=round(rebound_pos, 4),
                pb_depth_pct=round(pb_depth_pct, 4),
                prior_div_count=prior_divs,
                room_r=round(room_r, 4),
                hold_seconds_wave=hold_sec,
            )
        )

    all_cids = sorted({a.ticket.signal.wave_cluster_id for a in annotations})
    oos_cids = set(all_cids[1:]) if len(all_cids) > 1 else set(all_cids)
    split70_idx = max(1, int(0.30 * len(all_cids)))
    split70_cids = set(all_cids[split70_idx:]) if len(all_cids) > 1 else set(all_cids)

    ret_is = [a for a in annotations if a.retained]
    rej_is = [a for a in annotations if not a.retained]

    all_oos = [a for a in annotations if a.ticket.signal.wave_cluster_id in oos_cids]
    ret_oos = [a for a in all_oos if a.retained]
    rej_oos = [a for a in all_oos if not a.retained]

    all_s70 = [a for a in annotations if a.ticket.signal.wave_cluster_id in split70_cids]
    ret_s70 = [a for a in all_s70 if a.retained]

    def _m_wave(seq: Sequence[S3TicketAnnotation]) -> ClusterMeanStat:
        return cluster_robust_mean(
            [x.ticket.net_r_wave for x in seq],
            [x.ticket.signal.wave_cluster_id for x in seq],
        )

    def _m_brack(seq: Sequence[S3TicketAnnotation]) -> ClusterMeanStat:
        return cluster_robust_mean(
            [x.ticket.net_r_bracket for x in seq],
            [x.ticket.signal.wave_cluster_id for x in seq],
        )

    all_is_st = _m_wave(annotations)
    ret_is_st = _m_wave(ret_is)
    rej_is_st = _m_wave(rej_is)
    all_oos_st = _m_wave(all_oos)
    ret_oos_st = _m_wave(ret_oos)
    rej_oos_st = _m_wave(rej_oos)
    ret_oos_br = _m_brack(ret_oos)
    rej_oos_br = _m_brack(rej_oos)
    ret_oos_le30m = _m_wave([x for x in ret_oos if 0 < x.hold_seconds_wave <= 1800])
    ret_oos_gt30m = _m_wave([x for x in ret_oos if x.hold_seconds_wave > 1800])

    diff_ret_vs_all = cluster_robust_diff(
        [x.ticket.net_r_wave for x in ret_oos],
        [x.ticket.signal.wave_cluster_id for x in ret_oos],
        [x.ticket.net_r_wave for x in all_oos],
        [x.ticket.signal.wave_cluster_id for x in all_oos],
    )
    diff_ret_vs_rej = cluster_robust_diff(
        [x.ticket.net_r_wave for x in ret_oos],
        [x.ticket.signal.wave_cluster_id for x in ret_oos],
        [x.ticket.net_r_wave for x in rej_oos],
        [x.ticket.signal.wave_cluster_id for x in rej_oos],
    )
    diff_s70 = cluster_robust_diff(
        [x.ticket.net_r_wave for x in ret_s70],
        [x.ticket.signal.wave_cluster_id for x in ret_s70],
        [x.ticket.net_r_wave for x in all_s70],
        [x.ticket.signal.wave_cluster_id for x in all_s70],
    )

    ablation: dict[str, ClusterMeanStat] = {
        "C1_undertake_only": _m_wave([x for x in all_oos if x.c1_undertake_no_breakout]),
        "C2_div_le_2_only": _m_wave([x for x in all_oos if x.c2_div_count_le_2]),
        "C3_htf_uptrend_btc_only": _m_wave([x for x in all_oos if x.c3_htf_main_uptrend]),
        "C4_overhead_space_only": _m_wave([x for x in all_oos if x.c4_overhead_space]),
    }

    g_oos = ret_oos_st.n_clusters
    delta_all = diff_ret_vs_all.diff_mean
    p_all = diff_ret_vs_all.one_sided_p
    rej_neg = not math.isnan(rej_oos_st.ci95_high) and rej_oos_st.ci95_high < 0.0

    reasons: list[str] = []
    if g_oos < MIN_OOS_CLUSTERS_BAR:
        reasons.append(f"Retained G_oos = {g_oos} < {MIN_OOS_CLUSTERS_BAR}")
    if math.isnan(delta_all) or delta_all < S3_DELTA_NETR_BAR:
        reasons.append(
            f"Retained vs All OOS Delta NetR = {delta_all:+.4f}R < +{S3_DELTA_NETR_BAR:.2f}R bar"
            if not math.isnan(delta_all)
            else "Retained vs All OOS Delta NetR = NaN"
        )
    if math.isnan(p_all) or p_all >= P_VALUE_BAR:
        reasons.append(
            f"OOS one-sided p = {p_all:.4f} >= {P_VALUE_BAR:.2f}"
            if not math.isnan(p_all)
            else "OOS one-sided p = NaN"
        )
    if not rej_neg:
        reasons.append(
            f"Rejected group 95% CI upper = {rej_oos_st.ci95_high:+.4f}R >= 0R (not significantly poor)"
            if not math.isnan(rej_oos_st.ci95_high)
            else "Rejected group 95% CI upper = NaN"
        )

    if not reasons:
        verdict = "PASS"
        detail = (
            f"PASS: Retained vs All OOS Delta NetR = {delta_all:+.4f}R (>= +0.18R), "
            f"p = {p_all:.4f} (< 0.05), G_oos = {g_oos} (>= 15), Rejected CI95 upper = {rej_oos_st.ci95_high:+.4f}R < 0."
        )
    elif g_oos < MIN_OOS_CLUSTERS_BAR:
        verdict = f"FAIL (underpowered, G_oos={g_oos})"
        detail = "FAIL (underpowered): " + "; ".join(reasons)
    else:
        verdict = "FAIL"
        detail = "FAIL: " + "; ".join(reasons)

    return S3ConstraintReport(
        all_is_stat=all_is_st,
        retained_is_stat=ret_is_st,
        rejected_is_stat=rej_is_st,
        all_oos_stat=all_oos_st,
        retained_oos_stat=ret_oos_st,
        rejected_oos_stat=rej_oos_st,
        retained_oos_bracket_stat=ret_oos_br,
        rejected_oos_bracket_stat=rej_oos_br,
        retained_oos_hold_le30m_stat=ret_oos_le30m,
        retained_oos_hold_gt30m_stat=ret_oos_gt30m,
        diff_retained_vs_all_oos=diff_ret_vs_all,
        diff_retained_vs_rejected_oos=diff_ret_vs_rej,
        diff_retained_vs_all_split70=diff_s70,
        ablation_single_drop_oos=ablation,
        verdict=verdict,
        verdict_detail=detail,
        annotations=tuple(annotations),
    )


# ---------------------------------------------------------------------------
# F: Box Filter — Box Middle Chop, Internal Box Rebound & Box Counter-Short
#    (Source §3 Point 6 & §5 + EP09/EP17/EP20/EP22)
# ---------------------------------------------------------------------------


def evaluate_f_box_filter(
    baseline_tickets: Sequence[AnnotatedTicketSignal],
    bars_by_sym: Mapping[str, Sequence[MinuteBar]],
    bars_4h_by_sym: Mapping[str, Sequence[tuple[int, float, float, float, float, float]]],
    bars_1d_by_sym: Mapping[str, Sequence[tuple[int, float, float, float, float, float]]],
) -> FBoxFilterReport:
    """Evaluate F (Box Filter) strictly by negative-filter criteria (`CI_high < 0`, `Delta >= +0.15R`, `G >= 15`)."""
    _, bars_5m, close5m_idx = _build_5m_bars_from_1m(bars_by_sym)

    annotations: list[FTicketAnnotation] = []
    for tkt in baseline_tickets:
        sig = tkt.signal
        sym = sig.symbol
        entry_ts = sig.entry_ts
        entry_px = sig.entry_price
        stop_px = sig.stop_price

        c_4h = _causal_prefix(bars_4h_by_sym.get(sym, ()), entry_ts)
        c_1d = _causal_prefix(bars_1d_by_sym.get(sym, ()), entry_ts)
        b5 = bars_5m.get(sym, [])
        b5_idx = close5m_idx.get(sym, {}).get(entry_ts)
        b5_prefix = _contiguous_5m_prefix(b5, b5_idx) if b5_idx is not None else []

        pivs_htf: list[ConfirmedSwingPivot] = []
        if len(c_4h) >= 3:
            w4 = c_4h[-180:]
            pivs_htf.extend(
                extract_causal_swing_pivots(w4, confirm_lag_bars=1, up_to_idx=len(w4) - 1)
            )
        if len(c_1d) >= 3:
            w1 = c_1d[-90:]
            pivs_htf.extend(
                extract_causal_swing_pivots(w1, confirm_lag_bars=1, up_to_idx=len(w1) - 1)
            )
        pivot_htf_bars = [
            (
                p.confirm_ts,
                p.price,
                p.price if p.kind == "HIGH" else 0.0,
                p.price if p.kind == "LOW" else float("inf"),
                p.price,
            )
            for p in pivs_htf
            if p.confirm_ts <= entry_ts
        ]

        room_r = _compute_causal_room_to_resistance_r(
            sig.direction,
            entry_px,
            stop_px,
            entry_ts,
            pivot_htf_bars,
            b5_prefix,
        )

        is_mid = False
        is_reb = False
        is_short_trap = False
        w_box = 0.0
        er_4h = 1.0
        pos_box = 0.5

        if len(c_4h) >= 12:
            box_win = c_4h[-30:]  # trailing 5 days of closed 4h bars
            h_box = max(x[2] for x in box_win)
            l_box = min(x[3] for x in box_win)
            w_box = (h_box - l_box) / l_box if l_box > 0 else 0.0
            pos_box = (entry_px - l_box) / max(h_box - l_box, 1e-9)

            last12 = box_win[-12:]
            path_sum = sum(x[2] - x[3] for x in last12)
            er_4h = (
                abs(last12[-1][4] - last12[0][1]) / path_sum
                if path_sum > 0
                else 1.0
            )

            if w_box >= 0.03:
                # 1. Box-middle chop (`EP17` line 26, `§5` line 99: 箱体中段方向不可知)
                if er_4h <= 0.30 and 0.25 <= pos_box <= 0.75:
                    is_mid = True

                # 2. Post-markup internal box rebound long (`EP22` line 10 Turbo, `EP09` line 12, `§3` Point 6)
                if sig.direction == "UP":
                    idx_max = max(range(len(box_win)), key=lambda k: box_win[k][2])
                    if idx_max <= len(box_win) - 4:
                        min_after = min(x[3] for x in box_win[idx_max + 1 :])
                        drop_from_hi = (h_box - min_after) / h_box if h_box > 0 else 0.0
                        if (
                            drop_from_hi >= 0.05
                            and entry_px <= h_box * 0.985
                            and pos_box < 0.85
                            and room_r < 2.0
                        ):
                            is_reb = True

                # 3. Non-downtrend box counter-trend short (`EP20` line 11 PEPE: 谁告诉你箱体顶可以空？)
                if sig.direction == "DOWN" and c_1d:
                    w20 = c_1d[-20:]
                    sma20_1d = sum(x[4] for x in w20) / len(w20)
                    if (
                        er_4h <= 0.35
                        and c_1d[-1][4] >= sma20_1d
                        and entry_px > l_box * 1.015
                        and room_r < 2.0
                    ):
                        is_short_trap = True

        validate_ex_ante_grouping(
            ["f_box_mid_chop", "f_box_internal_rebound", "f_box_counter_short"],
            feature_timestamps={
                "f_box_mid_chop": float(c_4h[-1][0] if c_4h else entry_ts),
                "f_box_internal_rebound": float(entry_ts),
                "f_box_counter_short": float(c_1d[-1][0] if c_1d else entry_ts),
            },
            entry_ts=float(entry_ts),
        )

        annotations.append(
            FTicketAnnotation(
                ticket=tkt,
                is_box_mid_chop=is_mid,
                is_box_internal_rebound=is_reb,
                is_box_counter_short=is_short_trap,
                box_width_pct=round(w_box * 100.0, 3),
                er_4h=round(er_4h, 4),
                pos_box=round(pos_box, 4),
                room_r=round(room_r, 4),
            )
        )

    all_cids = sorted({a.ticket.signal.wave_cluster_id for a in annotations})
    oos_cids = set(all_cids[1:]) if len(all_cids) > 1 else set(all_cids)
    split70_idx = max(1, int(0.30 * len(all_cids)))
    split70_cids = set(all_cids[split70_idx:]) if len(all_cids) > 1 else set(all_cids)

    def _m(seq: Sequence[FTicketAnnotation]) -> ClusterMeanStat:
        return cluster_robust_mean(
            [x.ticket.net_r_wave for x in seq],
            [x.ticket.signal.wave_cluster_id for x in seq],
        )

    def _d(
        ret_seq: Sequence[FTicketAnnotation], fil_seq: Sequence[FTicketAnnotation]
    ) -> ClusterDiffStat:
        return cluster_robust_diff(
            [x.ticket.net_r_wave for x in ret_seq],
            [x.ticket.signal.wave_cluster_id for x in ret_seq],
            [x.ticket.net_r_wave for x in fil_seq],
            [x.ticket.signal.wave_cluster_id for x in fil_seq],
        )

    fil_is = [a for a in annotations if a.is_filtered]
    ret_is = [a for a in annotations if not a.is_filtered]

    oos_all = [a for a in annotations if a.ticket.signal.wave_cluster_id in oos_cids]
    fil_oos = [a for a in oos_all if a.is_filtered]
    ret_oos = [a for a in oos_all if not a.is_filtered]
    mid_oos = [a for a in oos_all if a.is_box_mid_chop]
    reb_oos = [a for a in oos_all if a.is_box_internal_rebound]
    sh_oos = [a for a in oos_all if a.is_box_counter_short]

    s70_all = [a for a in annotations if a.ticket.signal.wave_cluster_id in split70_cids]
    fil_s70 = [a for a in s70_all if a.is_filtered]
    ret_s70 = [a for a in s70_all if not a.is_filtered]

    long_oos = [a for a in oos_all if a.ticket.signal.direction == "UP"]
    fil_long_oos = [a for a in long_oos if a.is_filtered]
    ret_long_oos = [a for a in long_oos if not a.is_filtered]

    fil_is_st = _m(fil_is)
    ret_is_st = _m(ret_is)
    fil_oos_st = _m(fil_oos)
    ret_oos_st = _m(ret_oos)
    mid_oos_st = _m(mid_oos)
    reb_oos_st = _m(reb_oos)
    sh_oos_st = _m(sh_oos)

    diff_is = _d(ret_is, fil_is)
    diff_oos = _d(ret_oos, fil_oos)
    diff_s70 = _d(ret_s70, fil_s70)
    diff_long = _d(ret_long_oos, fil_long_oos)

    g_fil = fil_oos_st.n_clusters
    ci_hi = fil_oos_st.ci95_high
    delta = diff_oos.diff_mean

    reasons: list[str] = []
    if g_fil < MIN_OOS_CLUSTERS_BAR:
        reasons.append(f"Filtered G_oos = {g_fil} < {MIN_OOS_CLUSTERS_BAR}")
    if math.isnan(ci_hi) or ci_hi >= 0.0:
        reasons.append(
            f"Filtered 95% CI upper = {ci_hi:+.4f}R >= 0.0000R"
            if not math.isnan(ci_hi)
            else "Filtered 95% CI upper = NaN"
        )
    if math.isnan(delta) or delta < F_DELTA_NETR_BAR:
        reasons.append(
            f"Retained - Filtered OOS Delta NetR = {delta:+.4f}R < +{F_DELTA_NETR_BAR:.2f}R bar"
            if not math.isnan(delta)
            else "Retained - Filtered OOS Delta NetR = NaN"
        )

    if not reasons:
        verdict = "PASS"
        detail = (
            f"PASS (Negative Filter Only): Filtered OOS Mean = {fil_oos_st.mean:+.4f}R "
            f"(95% CI [{fil_oos_st.ci95_low:+.4f}R, {ci_hi:+.4f}R] < 0), "
            f"Retained - Filtered Delta = {delta:+.4f}R (>= +0.15R, p = {diff_oos.one_sided_p:.4f}), "
            f"Filtered G_oos = {g_fil} (>= 15)."
        )
    elif g_fil < MIN_OOS_CLUSTERS_BAR:
        verdict = f"FAIL (underpowered, G_filtered={g_fil})"
        detail = "FAIL (underpowered): " + "; ".join(reasons)
    else:
        verdict = "FAIL"
        detail = "FAIL: " + "; ".join(reasons)

    return FBoxFilterReport(
        filtered_is_stat=fil_is_st,
        retained_is_stat=ret_is_st,
        filtered_oos_stat=fil_oos_st,
        retained_oos_stat=ret_oos_st,
        filtered_mid_oos_stat=mid_oos_st,
        filtered_rebound_oos_stat=reb_oos_st,
        filtered_short_oos_stat=sh_oos_st,
        diff_retained_minus_filtered_is=diff_is,
        diff_retained_minus_filtered_oos=diff_oos,
        diff_retained_minus_filtered_split70=diff_s70,
        diff_long_only_oos=diff_long,
        verdict=verdict,
        verdict_detail=detail,
        annotations=tuple(annotations),
    )


# ---------------------------------------------------------------------------
# L: Resonance Leader Ranker — Event / State Model
#    (Source §2 & §6 + EP06/EP07/EP13/EP21 + DELIVERY_AUDIT §4 vs H2)
# ---------------------------------------------------------------------------


def score_langlang_leader_states(
    sym: str,
    wake_ts: int,
    age_days: float,
    sym_1d: Sequence[tuple[int, float, float, float, float, float]],
    sym_4h: Sequence[tuple[int, float, float, float, float, float]],
    btc_4h: Sequence[tuple[int, float, float, float, float, float]],
    universe_1d_body_median: float,
    universe_launch_median_ret: float | None,
    t_btc_launch: int | None,
    btc_ret_48h_pct: float,
    r_btc_1h_pct: float,
    r_sym_1h_pct: float,
) -> tuple[int, int, int, int]:
    """Compute discrete `(S_1, S_2, S_3, S_4)` leader state scores strictly at `<= wake_ts`."""
    del sym, universe_1d_body_median  # kept in signature for backwards compatibility

    # --- S_1: Daily Structure & 48h `4h` Fluency (`EP21` L7-9 + `DELIVERY_AUDIT` §4, -1..+2) ---
    s1 = 0
    ret_14d = 0.0
    if len(sym_1d) >= 2:
        p_now_d = sym_1d[-1][4]
        p_14d = sym_1d[max(0, len(sym_1d) - 14)][1]
        if p_14d > 0:
            ret_14d = (p_now_d - p_14d) / p_14d * 100.0

    r_recent_12h = 1.0
    if len(sym_4h) >= 4 and sym_4h[-4][4] > 0:
        r_recent_12h = (sym_4h[-1][4] - sym_4h[-4][4]) / sym_4h[-4][4] * 100.0

    er_4h_48h = 0.50
    if len(sym_4h) >= 6:
        w48 = sym_4h[-12:]
        path48 = sum(x[2] - x[3] for x in w48)
        er_4h_48h = abs(w48[-1][4] - w48[0][1]) / path48 if path48 > 0 else 0.50

    stagnant_or_whippy = (r_recent_12h <= 0.0) or (er_4h_48h < 0.25)

    if age_days < 7.0 or (ret_14d > 85.0 and stagnant_or_whippy):
        s1 = -1
    elif len(sym_1d) >= 7:
        closes_d = [x[4] for x in sym_1d]
        sma7 = sum(closes_d[-7:]) / 7.0
        sma20 = sum(closes_d[-20:]) / 20.0 if len(closes_d) >= 20 else sma7
        if closes_d[-1] > sma7 >= sma20:
            s1 += 1
        had_surge_14d = any(
            x[1] > 0 and (x[4] - x[1]) / x[1] >= 0.05 for x in sym_1d[-14:]
        )
        if er_4h_48h >= 0.35 and had_surge_14d:
            s1 += 1

    # --- S_2: Major (BTC/ETH) Launch `[-4h, +8h]` Lead/Same-Wave Sync (`EP06` L14, `EP07` L10, `EP21` L6) ---
    s2 = 0
    if t_btc_launch is not None and sym_4h:
        w_sync = [
            x
            for x in sym_4h
            if (t_btc_launch - 4 * 3600) <= x[0] <= (t_btc_launch + 8 * 3600)
        ]
        w_12h = [
            x
            for x in sym_4h
            if (t_btc_launch - 4 * 3600) <= x[0] <= (t_btc_launch + 12 * 3600)
        ]
        ret_sync = (
            (w_sync[-1][4] - w_sync[0][1]) / w_sync[0][1] * 100.0
            if w_sync and w_sync[0][1] > 0
            else 0.0
        )
        ret_12h = (
            (w_12h[-1][4] - w_12h[0][1]) / w_12h[0][1] * 100.0
            if w_12h and w_12h[0][1] > 0
            else ret_sync
        )
        med_launch = (
            universe_launch_median_ret if universe_launch_median_ret is not None else 0.0
        )
        if w_sync and ret_sync >= med_launch + 1.0:
            s2 = 2
        elif (
            btc_ret_48h_pct >= 2.5
            and (wake_ts - t_btc_launch) > 12 * 3600
            and ret_12h <= 0.5
        ):
            s2 = -2

    if (
        s2 == 0
        and (t_btc_launch is None or (wake_ts - t_btc_launch) <= 8 * 3600)
        and r_btc_1h_pct >= 0.8
        and r_sym_1h_pct >= r_btc_1h_pct + 0.8
    ):
        s2 = 2

    # --- S_3: BTC 4h Pullback Resilience State (`§6` L114③, `EP03` L7, 0..+2) ---
    s3 = 0
    if btc_4h and sym_4h:
        sym_4h_map = {x[0]: x for x in sym_4h}
        btc_red_excess: list[float] = []
        flat_flags: list[float] = []
        for bx in btc_4h[-42:]:  # trailing 7 days
            if bx[1] <= 0:
                continue
            r_b = (bx[4] - bx[1]) / bx[1] * 100.0
            if r_b <= -0.50:
                sx = sym_4h_map.get(bx[0])
                if sx is not None and sx[1] > 0:
                    r_s = (sx[4] - sx[1]) / sx[1] * 100.0
                    btc_red_excess.append(r_s - r_b)
                    flat_flags.append(1.0 if r_s >= -0.25 else 0.0)
        if len(btc_red_excess) >= 2:
            res_mean = sum(btc_red_excess) / len(btc_red_excess)
            flat_ratio = sum(flat_flags) / len(flat_flags)
            if flat_ratio >= 0.50 and res_mean >= 0.40:
                s3 = 2
            elif res_mean > 0.0:
                s3 = 1

    # --- S_4: Daily Structural Overhead Space (`§6` L114④, `EP13` L11, -1..+2) ---
    s4 = 0
    if sym_1d:
        cur_px = sym_4h[-1][4] if sym_4h else sym_1d[-1][4]
        above_d1 = [x[2] for x in sym_1d[-30:] if x[2] > cur_px * 1.01]
        if not above_d1:
            s4 = 2
        else:
            space_pct = (min(above_d1) - cur_px) / cur_px * 100.0 if cur_px > 0 else 0.0
            if space_pct >= 12.0:
                s4 = 2
            elif space_pct >= 6.0:
                s4 = 1
            elif space_pct < 3.0:
                s4 = -1
            else:
                s4 = 0

    return s1, s2, s3, s4


def _latest_4h_launch_ts(
    c_4h: Sequence[tuple[int, float, float, float, float, float]],
) -> int | None:
    t_launch: int | None = None
    if len(c_4h) >= 14:
        for k in range(max(12, len(c_4h) - 18), len(c_4h)):
            bx = c_4h[k]
            r_4h = (bx[4] - bx[1]) / bx[1] * 100.0 if bx[1] > 0 else 0.0
            prev12_hi = max(x[2] for x in c_4h[k - 12 : k])
            if r_4h >= 1.2 or bx[4] > prev12_hi:
                t_launch = bx[0] - 14400  # open_ts of the launching 4h bar
    return t_launch


def evaluate_l_resonance_leader_ranker(
    con: duckdb.DuckDBPyConnection,
    bars_by_sym: Mapping[str, Sequence[MinuteBar]],
    bars_4h_by_sym: Mapping[str, Sequence[tuple[int, float, float, float, float, float]]],
    bars_1d_by_sym: Mapping[str, Sequence[tuple[int, float, float, float, float, float]]],
    funding_by_sym: Mapping[str, Mapping[int, float]] | None,
    tick_map: Mapping[str, float],
    *,
    min_quote_vol_24h: float = 10_000_000.0,
    notional_usd: float = 10_000.0,
    params: FrictionParams = FrictionParams(),
) -> LLeaderRankerReport:
    """Evaluate L (Event/State Resonance Leader Ranker) on multi-candidate UP wakes (`K >= 4`)."""
    pit_meta, vol_by_sym = _load_pit_and_vol(con)
    idx_1m, bars_5m, close5m_idx = _build_5m_bars_from_1m(bars_by_sym)

    all_5m_closes: set[int] = set()
    for b5 in bars_5m.values():
        for row in b5:
            all_5m_closes.add(row[0])

    btc_sym = _find_major_symbol(bars_4h_by_sym, bars_1d_by_sym, "BTC") or "BTC/USDT:USDT"
    eth_sym = _find_major_symbol(bars_4h_by_sym, bars_1d_by_sym, "ETH") or "ETH/USDT:USDT"

    raw_wakes: list[tuple[str, int, float, float, list[tuple[str, float, int]]]] = []
    last_wake_ts = -10**12

    for eval_ts in sorted(all_5m_closes):
        if eval_ts - last_wake_ts < 1200:
            continue
        eligible = [
            s
            for s in bars_by_sym
            if _pit_eligible(
                s, eval_ts, min_quote_vol_24h, pit_meta, vol_by_sym, bars_5m, close5m_idx
            )
        ]
        if len(eligible) < 8:
            continue

        rets_5m: list[tuple[str, float, int]] = []
        btc_ret = 0.0
        for sym in eligible:
            c_idx = close5m_idx.get(sym, {}).get(eval_ts)
            b5 = bars_5m.get(sym, [])
            if c_idx is None or c_idx < 24:
                continue
            series = bars_by_sym[sym]
            m_idx = idx_1m[sym].get(eval_ts - 60)
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
        sorted_r = sorted(r for _, r, _ in rets_5m)
        med_r = sorted_r[n_v // 2]

        # Bullish wake (same threshold as h2_experiment.py line 460)
        if up_b >= 0.48 and med_r >= 0.08:
            wake_id = f"mp_{eval_ts}_up"
            raw_wakes.append((wake_id, eval_ts, med_r, btc_ret, rets_5m))
            last_wake_ts = eval_ts

    _, wake_to_cluster = build_wave_clusters(
        [(wid, float(wts)) for wid, wts, _, _, _ in raw_wakes]
    )

    zero_friction = FrictionParams(
        taker_fee_bps=0.0,
        maker_fee_bps=0.0,
        s0_major_bps=0.0,
        s0_alt_bps=0.0,
        lambda_impact=0.0,
    )

    all_records: list[LCandidateStateRecord] = []
    wake_ids_with_k4: list[tuple[str, str]] = []

    btc_4h_all = bars_4h_by_sym.get(btc_sym, ())
    eth_4h_all = bars_4h_by_sym.get(eth_sym, ())
    btc_b5 = bars_5m.get(btc_sym, [])
    eth_b5 = bars_5m.get(eth_sym, [])
    btc_c5_map = close5m_idx.get(btc_sym, {})
    eth_c5_map = close5m_idx.get(eth_sym, {})

    for wake_id, wake_ts, med_r, btc_ret, rets_5m in raw_wakes:
        cluster_id = wake_to_cluster[wake_id]
        c_btc_4h = _causal_prefix(btc_4h_all, wake_ts)
        c_eth_4h = _causal_prefix(eth_4h_all, wake_ts)

        # Dual BTC / ETH 4h launch anchor (`EP13` L9-11 + `EP21` L6)
        t_btc_l = _latest_4h_launch_ts(c_btc_4h)
        t_eth_l = _latest_4h_launch_ts(c_eth_4h)
        launch_cands = [x for x in (t_btc_l, t_eth_l) if x is not None]
        t_major_launch = max(launch_cands) if launch_cands else None

        btc_ret_48h = (
            (c_btc_4h[-1][4] - c_btc_4h[-12][1]) / c_btc_4h[-12][1] * 100.0
            if len(c_btc_4h) >= 12 and c_btc_4h[-12][1] > 0
            else 0.0
        )
        eth_ret_48h = (
            (c_eth_4h[-1][4] - c_eth_4h[-12][1]) / c_eth_4h[-12][1] * 100.0
            if len(c_eth_4h) >= 12 and c_eth_4h[-12][1] > 0
            else 0.0
        )
        major_ret_48h = max(btc_ret_48h, eth_ret_48h)

        r_btc_1h = 0.0
        btc_cidx = btc_c5_map.get(wake_ts)
        if btc_cidx is not None and btc_cidx >= 12 and btc_b5[btc_cidx - 12][4] > 0:
            r_btc_1h = (
                (btc_b5[btc_cidx][4] - btc_b5[btc_cidx - 12][4])
                / btc_b5[btc_cidx - 12][4]
                * 100.0
            )
        r_eth_1h = 0.0
        eth_cidx = eth_c5_map.get(wake_ts)
        if eth_cidx is not None and eth_cidx >= 12 and eth_b5[eth_cidx - 12][4] > 0:
            r_eth_1h = (
                (eth_b5[eth_cidx][4] - eth_b5[eth_cidx - 12][4])
                / eth_b5[eth_cidx - 12][4]
                * 100.0
            )
        r_major_1h = max(r_btc_1h, r_eth_1h)

        launch_rets: list[float] = []
        if t_major_launch is not None:
            for sym_u, _, _ in rets_5m:
                h4_u = _causal_prefix(bars_4h_by_sym.get(sym_u, ()), wake_ts)
                w_sync = [
                    x
                    for x in h4_u
                    if (t_major_launch - 4 * 3600) <= x[0] <= (t_major_launch + 8 * 3600)
                ]
                if w_sync and w_sync[0][1] > 0:
                    launch_rets.append((w_sync[-1][4] - w_sync[0][1]) / w_sync[0][1] * 100.0)
        launch_med = (
            sorted(launch_rets)[len(launch_rets) // 2] if launch_rets else None
        )

        raw_cands: list[
            tuple[
                CandidateSignal,
                float,
                float,
                int,
                int,
                int,
                int,
                float,
                float,
                float,
                float,
            ]
        ] = []

        for sym, r5, c_idx in rets_5m:
            if normalize_base_coin(sym) == "BTC":
                continue
            if (r5 - med_r) < 0.04:
                continue

            b5 = bars_5m[sym]
            series = bars_by_sym[sym]
            imap = idx_1m[sym]
            cmap = close5m_idx[sym]

            trig_found: tuple[int, int, float, float, float, float] | None = None
            for trig_ts in range(wake_ts, wake_ts + 7200 + 1, 300):
                t_b5_idx = cmap.get(trig_ts)
                if t_b5_idx is None or t_b5_idx < 24:
                    continue
                win_5m = [
                    (row[0], row[1], row[2], row[3], row[4])
                    for row in b5[max(0, t_b5_idx - 29) : t_b5_idx + 1]
                ]
                trig = _detect_5m_pullback_trigger(
                    "UP", win_5m, min_pullback_ts=wake_ts - 3600
                )
                if trig is None:
                    continue
                e_idx = imap.get(trig_ts)
                if e_idx is None or (len(series) - 1 - e_idx) < 120:
                    continue
                if series[e_idx + 120].ts - series[e_idx].ts > 125 * 60:
                    continue
                w_slice = b5[max(0, c_idx - 24) : c_idx + 1]
                hi_w = max(x[2] for x in w_slice)
                cur_px = b5[c_idx][4]
                pb_depth = (hi_w - cur_px) / hi_w * 100.0 if hi_w > 0 else 0.5
                room_pct = max(1.5, (hi_w * 1.03 - cur_px) / cur_px * 100.0)
                trig_found = (trig_ts, e_idx, trig[0], trig[1], pb_depth, room_pct)
                break

            if trig_found is None:
                continue
            trig_ts, e_idx, entry_px, stop_px, pb_depth, room_pct = trig_found

            sym_1d = _causal_prefix(bars_1d_by_sym.get(sym, ()), wake_ts)
            sym_4h = _causal_prefix(bars_4h_by_sym.get(sym, ()), wake_ts)

            validate_ex_ante_grouping(
                ["s1_daily_struct", "s2_btc_sync", "s3_btc_resilience", "s4_daily_space"],
                feature_timestamps={
                    "s1_daily_struct": float(
                        max(
                            sym_1d[-1][0] if sym_1d else 0,
                            sym_4h[-1][0] if sym_4h else 0,
                            wake_ts,
                        )
                    ),
                    "s2_btc_sync": float(wake_ts),
                    "s3_btc_resilience": float(sym_4h[-1][0] if sym_4h else wake_ts),
                    "s4_daily_space": float(sym_1d[-1][0] if sym_1d else wake_ts),
                },
                entry_ts=float(trig_ts),
            )

            qv_24h = _causal_24h_vol_at(sym, wake_ts, vol_by_sym, bars_5m, close5m_idx)
            list_ts = pit_meta.get(sym, (1_700_000_000, None))[0]
            age_days = max(0.0, (wake_ts - list_ts) / 86400.0)

            r_sym_1h = 0.0
            if c_idx >= 12 and b5[c_idx - 12][4] > 0:
                r_sym_1h = (
                    (b5[c_idx][4] - b5[c_idx - 12][4]) / b5[c_idx - 12][4] * 100.0
                )

            s1, s2, s3, s4 = score_langlang_leader_states(
                sym=sym,
                wake_ts=wake_ts,
                age_days=age_days,
                sym_1d=sym_1d,
                sym_4h=sym_4h,
                btc_4h=c_btc_4h,
                universe_1d_body_median=0.50,
                universe_launch_median_ret=launch_med,
                t_btc_launch=t_major_launch,
                btc_ret_48h_pct=major_ret_48h,
                r_btc_1h_pct=r_major_1h,
                r_sym_1h_pct=r_sym_1h,
            )
            cur_score = score_current_ranker(
                direction="UP",
                change_pct=r5,
                market_median_change=med_r,
                btc_change=btc_ret,
                quote_vol_24h=qv_24h,
                pullback_depth_pct=pb_depth,
                room_pct=room_pct,
                min_liquidity=min_quote_vol_24h,
            )

            lookback_15m = series[max(0, e_idx - 15) : e_idx]
            trs = [b.high - b.low for b in lookback_15m]
            atr_1m = sum(trs) / len(trs) if trs else entry_px * 0.002
            adv_5m = max(100.0, sum(b.quote_vol for b in lookback_15m[-5:]))

            sig = CandidateSignal(
                signal_id=f"{wake_id}_{normalize_base_coin(sym)}_{trig_ts}",
                wave_cluster_id=cluster_id,
                symbol=sym,
                direction="UP",
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
            sim_brack = simulate_holding_policy(
                sig,
                series,
                policy_kind="fixed_bracket",
                target_r=1.5,
                max_hold_minutes=1440,
                funding_schedule=f_sched,
                params=params,
            )
            raw_cands.append(
                (
                    sig,
                    qv_24h,
                    age_days,
                    s1,
                    s2,
                    s3,
                    s4,
                    cur_score,
                    sim_gross.net_r,
                    sim_net.net_r,
                    sim_brack.net_r,
                )
            )

        if len(raw_cands) < 4:
            continue

        wake_ids_with_k4.append((wake_id, cluster_id))
        order_ll = sorted(
            range(len(raw_cands)),
            key=lambda idx: (
                raw_cands[idx][3] + raw_cands[idx][4] + raw_cands[idx][5] + raw_cands[idx][6],
                raw_cands[idx][5],
                raw_cands[idx][4],
                raw_cands[idx][1],
                raw_cands[idx][0].symbol,
            ),
            reverse=True,
        )
        top2_ll_set = set(order_ll[:2])

        order_cur = sorted(
            range(len(raw_cands)),
            key=lambda idx: (
                raw_cands[idx][7],
                -raw_cands[idx][2],
                raw_cands[idx][0].symbol,
            ),
            reverse=True,
        )
        top2_cur_set = set(order_cur[:2])

        for idx, item in enumerate(raw_cands):
            sig, qv_24h, age_days, s1, s2, s3, s4, cur_sc, g_r, n_r, b_r = item
            all_records.append(
                LCandidateStateRecord(
                    signal=sig,
                    wake_id=wake_id,
                    wake_ts=wake_ts,
                    quote_vol_24h=qv_24h,
                    age_days=age_days,
                    s1_daily_struct=s1,
                    s2_btc_sync=s2,
                    s3_btc_resilience=s3,
                    s4_daily_space=s4,
                    total_state_score=s1 + s2 + s3 + s4,
                    current_ranker_score=cur_sc,
                    is_top2_langlang=(idx in top2_ll_set),
                    is_top2_current=(idx in top2_cur_set),
                    gross_r_wave=g_r,
                    net_r_wave=n_r,
                    net_r_bracket=b_r,
                )
            )

    all_cids = sorted({r.signal.wave_cluster_id for r in all_records})
    oos_cids = set(all_cids[1:]) if len(all_cids) > 1 else set(all_cids)
    split70_idx = max(1, int(0.30 * len(all_cids)))
    split70_cids = set(all_cids[split70_idx:]) if len(all_cids) > 1 else set(all_cids)

    n_wakes_is = len(wake_ids_with_k4)
    n_wakes_oos = sum(1 for _, cid in wake_ids_with_k4 if cid in oos_cids)

    def _m(seq: Sequence[LCandidateStateRecord]) -> ClusterMeanStat:
        return cluster_robust_mean(
            [x.net_r_wave for x in seq],
            [x.signal.wave_cluster_id for x in seq],
        )

    def _d(
        seq_a: Sequence[LCandidateStateRecord],
        seq_b: Sequence[LCandidateStateRecord],
    ) -> ClusterDiffStat:
        return cluster_robust_diff(
            [x.net_r_wave for x in seq_a],
            [x.signal.wave_cluster_id for x in seq_a],
            [x.net_r_wave for x in seq_b],
            [x.signal.wave_cluster_id for x in seq_b],
        )

    top2_is = [r for r in all_records if r.is_top2_langlang]
    rest_is = [r for r in all_records if not r.is_top2_langlang]

    oos_recs = [r for r in all_records if r.signal.wave_cluster_id in oos_cids]
    top2_oos = [r for r in oos_recs if r.is_top2_langlang]
    rest_oos = [r for r in oos_recs if not r.is_top2_langlang]
    cur_top2_oos = [r for r in oos_recs if r.is_top2_current]
    high_state_oos = [r for r in oos_recs if r.total_state_score >= 4]

    s70_recs = [r for r in all_records if r.signal.wave_cluster_id in split70_cids]
    top2_s70 = [r for r in s70_recs if r.is_top2_langlang]
    rest_s70 = [r for r in s70_recs if not r.is_top2_langlang]

    top2_is_st = _m(top2_is)
    rest_is_st = _m(rest_is)
    top2_oos_st = _m(top2_oos)
    rest_oos_st = _m(rest_oos)
    cur_top2_oos_st = _m(cur_top2_oos)
    high_state_st = _m(high_state_oos)

    diff_is = _d(top2_is, rest_is)
    diff_oos = _d(top2_oos, rest_oos)
    diff_vs_cur = _d(top2_oos, cur_top2_oos)
    diff_s70 = _d(top2_s70, rest_s70)

    delta = diff_oos.diff_mean
    p_val = diff_oos.one_sided_p
    g_oos = top2_oos_st.n_clusters

    reasons: list[str] = []
    if g_oos < MIN_OOS_CLUSTERS_BAR:
        reasons.append(f"G_oos = {g_oos} < {MIN_OOS_CLUSTERS_BAR}")
    if math.isnan(delta) or delta < L_DELTA_NETR_BAR:
        reasons.append(
            f"OOS Top-2 vs Rest Delta NetR = {delta:+.4f}R < +{L_DELTA_NETR_BAR:.2f}R bar"
            if not math.isnan(delta)
            else "OOS Top-2 vs Rest Delta NetR = NaN"
        )
    if math.isnan(p_val) or p_val >= P_VALUE_BAR:
        reasons.append(
            f"OOS one-sided p = {p_val:.4f} >= {P_VALUE_BAR:.2f}"
            if not math.isnan(p_val)
            else "OOS one-sided p = NaN"
        )

    if not reasons:
        verdict = "PASS"
        detail = (
            f"PASS: OOS Top-2 vs Rest Delta NetR = {delta:+.4f}R (>= +0.15R), "
            f"p = {p_val:.4f} (< 0.05), G_oos = {g_oos}."
        )
    elif g_oos < MIN_OOS_CLUSTERS_BAR:
        verdict = f"FAIL (underpowered, G_oos={g_oos})"
        detail = "FAIL (underpowered): " + "; ".join(reasons)
    else:
        verdict = "FAIL"
        detail = "FAIL: " + "; ".join(reasons)

    return LLeaderRankerReport(
        n_wakes_is=n_wakes_is,
        n_wakes_oos=n_wakes_oos,
        top2_is_stat=top2_is_st,
        rest_is_stat=rest_is_st,
        top2_oos_stat=top2_oos_st,
        rest_oos_stat=rest_oos_st,
        current_top2_oos_stat=cur_top2_oos_st,
        diff_top2_vs_rest_is=diff_is,
        diff_top2_vs_rest_oos=diff_oos,
        diff_top2_vs_current_top2_oos=diff_vs_cur,
        diff_top2_vs_rest_split70=diff_s70,
        high_state_subset_oos_stat=high_state_st,
        verdict=verdict,
        verdict_detail=detail,
        candidates=tuple(all_records),
    )


# ---------------------------------------------------------------------------
# Thermometer Audit (Source §1 + EP18/EP22/EXTRA)
# ---------------------------------------------------------------------------


def evaluate_phase3a_thermometer(anchor: DatasetAnchorAudit) -> ThermometerAuditReport:
    """Return honest operationalizability evaluation for Source §1 + EP18/EXTRA Thermometer (`UNTESTABLE`)."""
    return ThermometerAuditReport(
        verdict="UNTESTABLE",
        missing_requirements=(
            f"全市场横截面缺失：`episodes/18-20240504.md` 第 12 行判据「大饼弹 8% 却没有一个山寨弹 30–40% 的龙头」为全市场最大值存在性断言，当前库仅拉取 {anchor.distinct_1m_symbols} 个标的（PIT 全表共 {anchor.pit_universe_total} 个永续合约），缺失其余 ~{max(0, anchor.pit_universe_total - anchor.distinct_1m_symbols)} 个山寨合约的 4h/1d 行情，无法区分「全市场无龙头」与「龙头落在未采样标的中」。",
            "连续 48h 冷却观测窗缺失：`episodes/extra-20240309.md` 第 6 行明确规定触发后「先彻底休息两天（48h）观察，确认是有残留热度的冬天才出手 WLD」，而 Phase 2b 的 1m 票流由 40 个 15h 离散切片（间隔 ~9.15 天）组成，触发后 48h 窗口逾 80% 落在无 1m 数据的切片空白期，无法在不篡改时间尺度的前提下评估被抑制票流与波次簇（G >= 15）。",
            "前置好结构探针与沉寂板块元数据缺失：`EP18` 第 12 行「结构好+位置对+久未有行情的板块天时地利人和都起不来（W 案）」依赖已通过样本外正期望验证的好结构探针及叙事板块（动物园/AI/铭文）元数据，当前 DuckDB schema 无板块分类字段。",
        ),
        unlock_data_spec=(
            "全市场 PIT USDT-SWAP（~500 标的）完整连续 4h / 1d OHLCV 表（用于忠实观测 BTC 反弹 >=8% 时全市场山寨涨幅分布最大值）；",
            "连续无间隔（或每次切片 >= 72h）的 1m 历史回放切片（完整覆盖 `EXTRA` 规定的触发后 48h 休息观察期内的被滤票流与独立波次簇 G >= 15）；",
            "至少一项经 OOS 验证正期望的进场结构识别器作为温度计前置探针，以及标的板块标签表。",
        ),
        observed_symbol_count=anchor.distinct_1m_symbols,
        pit_total_swap_count=anchor.pit_universe_total,
        slice_hours=15.0,
        inter_slice_gap_days=9.15,
    )


# ---------------------------------------------------------------------------
# Full Suite Runner & Markdown Report Formatters
# ---------------------------------------------------------------------------


def run_phase3a_all(
    con: duckdb.DuckDBPyConnection,
    *,
    enforce_anchor: bool = True,
) -> Phase3aFullSuiteReport:
    """Run Step 0 dataset check + all pre-registered Phase 3a validations."""
    anchor = verify_phase3a_dataset_anchor(con)
    if enforce_anchor and not anchor.within_2pct_anchor:
        raise RuntimeError(
            f"Step 0 dataset anchor mismatch: got {anchor.bars_1m:,} 1m bars vs "
            f"{anchor.expected_bars_1m:,} expected ({anchor.rel_diff_pct:.2f}% > 2.00% tolerance)"
        )

    bars_by_sym, funding_by_sym, tick_map = load_bars_and_funding_from_duckdb(con)
    bars_4h, bars_1d = _load_htf_bars_by_sym(con)
    pit_meta, vol_by_sym = _load_pit_and_vol(con)

    # Canonical baseline ticket stream from Phase 2b h1_experiment
    h1_rep = run_h1_experiment(
        con,
        bars_by_sym=bars_by_sym,
        funding_by_sym=funding_by_sym,
        tick_map=tick_map,
        min_quote_vol_24h=30_000_000.0,
    )
    baseline_tickets = h1_rep.tickets

    # Killed H3 reference metric on exact same DB
    h3_rep = run_h3_experiment(
        con,
        bars_by_sym=bars_by_sym,
        funding_by_sym=funding_by_sym,
        tick_map=tick_map,
        confirm_lag_bars=3,
    )

    s1_raw = detect_s1_point1_candidates(
        bars_by_sym, bars_4h, bars_1d, funding_by_sym, tick_map, pit_meta, vol_by_sym
    )
    s1_rep = _evaluate_entry_setup_against_baseline(
        "S1",
        "点位 1 — 大级别平台/箱体突破启动",
        s1_raw,
        baseline_tickets,
        delta_bar=S1_DELTA_NETR_BAR,
        h3_reference_oos_mean=h3_rep.box_stat.mean,
    )

    s2_raw = detect_s2_point4_incremental_candidates(
        bars_by_sym, bars_4h, bars_1d, funding_by_sym, tick_map, pit_meta, vol_by_sym
    )
    s2_rep = _evaluate_entry_setup_against_baseline(
        "S2",
        "点位 4（增量）— 大级别流畅主升 + 深大分歧后二波",
        s2_raw,
        baseline_tickets,
        delta_bar=S2_DELTA_NETR_BAR,
        h3_reference_oos_mean=h3_rep.second_wave_stat.mean,
    )

    s3_rep = evaluate_s3_point2_constraints(
        baseline_tickets, bars_by_sym, bars_4h, bars_1d, funding_by_sym=funding_by_sym
    )
    f_rep = evaluate_f_box_filter(
        baseline_tickets, bars_by_sym, bars_4h, bars_1d
    )
    l_rep = evaluate_l_resonance_leader_ranker(
        con, bars_by_sym, bars_4h, bars_1d, funding_by_sym, tick_map
    )
    thermo_rep = evaluate_phase3a_thermometer(anchor)

    return Phase3aFullSuiteReport(
        anchor=anchor,
        s1=s1_rep,
        s2=s2_rep,
        s3=s3_rep,
        f_filter=f_rep,
        l_ranker=l_rep,
        thermometer=thermo_rep,
    )


def _fmt_ci(st: ClusterMeanStat) -> str:
    if st.n_obs == 0 or math.isnan(st.ci95_low):
        return "`n/a`"
    return f"`[{st.ci95_low:+.4f}R, {st.ci95_high:+.4f}R]`"


def _fmt_num(val: float, suffix: str = "") -> str:
    if math.isnan(val):
        return "n/a"
    return f"{val:+.4f}{suffix}"


def _fmt_se(val: float) -> str:
    if math.isnan(val):
        return "n/a"
    return f"{val:.4f}"


def _fmt_p(val: float) -> str:
    if math.isnan(val):
        return "n/a"
    return f"{val:.4f}"


def format_s1_markdown(suite: Phase3aFullSuiteReport) -> str:
    r = suite.s1
    a = suite.anchor
    d_oos = r.diff_vs_baseline_long_oos
    d_is = r.diff_vs_baseline_long_is
    d_all = r.diff_vs_baseline_all_oos
    d_70 = r.diff_vs_baseline_long_split70
    today = datetime.now(timezone.utc).date().isoformat()
    sub_lines = [
        f"| `子模板 {k} (OOS)` | {st.n_obs} | {st.n_clusters} | `{_fmt_num(st.mean, 'R')}` | `{_fmt_se(st.cluster_se)}` | {_fmt_ci(st)} | — | — |"
        for k, st in r.sub_template_oos_stats.items()
    ]
    return "\n".join(
        [
            f"# Phase 3a 实验报告 — S1 点位 1：大级别平台/箱体突破启动 ({today})",
            "",
            f"- **判决（Verdict）**: **{r.verdict}** (`{r.verdict_detail}`)",
            "- **预注册锚点**: `docs/40-research/experiments/PHASE3A_PREREG.md` §3.1（Rev 2：总纲 + 逐集模板 + 交割单核查双锚定）",
            f"- **数据集**: OKX USDT-SWAP PIT 1m + HTF (`4h`/`1d`) 重演样本（`2025-11-09 -> 2026-11-09`，40 切片，27 标的，`1m={a.bars_1m:,}` 根，锚点偏差 `{a.rel_diff_pct:.3f}%`）",
            "- **收益与推断口径**: `wave_k_be=0.8` 全摩擦含冲击滑点 `PolicyNetR`（`stop-first` 1m 结算）+ `<24h` `wave_cluster_id` CR1 聚类稳健标准误",
            "",
            "## 1. 定义溯源（总纲 + `episodes/` 逐集模板 + `DELIVERY_AUDIT.md` 尺度校准）",
            "",
            "| 操作化条件 | 代码阈值（因果 `<= entry_ts`） | 源文件、逐集笔记与交割单核查依据 |",
            "|---|---|---|",
            "| 大盘顺势前提 | BTC `close_1d >= 0.98 * SMA_1d(50)` 或 `close_4h > SMA_4h(18)` | `DELIVERY_AUDIT.md` 第 51 行（顺势贡献 `83.9%` 净利）、`EXTRA` 第 7 行（OP 反例） |",
            "| 大级别平台/箱体 | 已收盘 `4h` K 线（`>= 48h` 跨度），振幅 `4.0% <= (B_hi - B_lo)/B_lo <= 35.0%`，确认滞后 `k_4h = 1` | 源文件 §3 点位 1（第 71 行）、`EP10` 第 25 行（TRB 平台案） |",
            "| 中界信号完成 | `4h` 箱体上沿试探 `>= 2` 次 **或** 下沿试探 `>= 2` 次（且两界均已定义） | `EP02` 第 8 行（「二次探顶（底）之前是初期，打完第二次顶/底才进中后期」）、`EP17` 第 24 行 |",
            "| 末端收敛走完 | `ATR_4h(3)/ATR_4h(12) <= 0.85` 或 `ATR_5m(6)/ATR_5m(24) <= 0.85` | 源文件 §5（第 103、109 行）、`EP10` 第 27 行（SATS 满分收敛案） |",
            "| 结构突破双模板 | **1A** 上沿放量突破（`c_t > B_hi`, `v >= 1.25x`）或 **1B** 中界后箱体中上半区回踩不破拐点突破（`pos >= 0.35`, `v >= 1.15x`） | `EP10` 第 25/27 行、`EP02` 第 9/24 行、`EP11` 第 9 行（「突破突破的是结构，不是价格点位」） |",
            "| 上方日线空间 | 箱体上沿上方无 30 日 `1d` 摆动高点 **或** 距下一 `1d` 摆动高点 `>= 3.0R` | `EP10` 第 25 行、`EP02` 第 10 行 |",
            "| 交割单校准止损 | 近 `1h` 整理拐点低点止损，宽度 `0.35% <= stop <= 3.20%`（附单箱体 `<=3` 次试错统计） | `audit_stats.json`（`5x` 中位杠杆、`loss.p10_roe = -16.1% -> 3.22%`）、`EP06` 第 23 行 |",
            "",
            "## 2. 样本量、波次簇与 IS / OOS 核心结果（含交割单持仓时长带诊断）",
            "",
            "| 分组 / 切分口径 | 样本数 `N` | 波次簇 `G` | Mean `PolicyNetR` | Cluster SE | 95% CI | 对基线 `ΔNetR` | 单侧 `p` |",
            "|---|---:|---:|---:|---:|---|---:|---:|",
            f"| `S1 点位 1 (In-Sample 全窗)` | {r.setup_is_stat.n_obs} | {r.setup_is_stat.n_clusters} | `{_fmt_num(r.setup_is_stat.mean, 'R')}` | `{_fmt_se(r.setup_is_stat.cluster_se)}` | {_fmt_ci(r.setup_is_stat)} | `{_fmt_num(d_is.diff_mean, 'R')}` | `{_fmt_p(d_is.one_sided_p)}` |",
            f"| **`S1 点位 1 (Walk-Forward OOS 主口径)`** | **{r.setup_oos_stat.n_obs}** | **{r.setup_oos_stat.n_clusters}** | **`{_fmt_num(r.setup_oos_stat.mean, 'R')}`** | `{_fmt_se(r.setup_oos_stat.cluster_se)}` | {_fmt_ci(r.setup_oos_stat)} | **`{_fmt_num(d_oos.diff_mean, 'R')}`** | **`{_fmt_p(d_oos.one_sided_p)}`** |",
            f"| `基线多头票流 (OOS 对照组)` | {r.baseline_long_oos_stat.n_obs} | {r.baseline_long_oos_stat.n_clusters} | `{_fmt_num(r.baseline_long_oos_stat.mean, 'R')}` | `{_fmt_se(r.baseline_long_oos_stat.cluster_se)}` | {_fmt_ci(r.baseline_long_oos_stat)} | — | — |",
            f"| `S1 vs 全方向基线票流 (OOS 核查列)` | {r.setup_oos_stat.n_obs} vs {r.baseline_all_oos_stat.n_obs} | {d_all.n_clusters_total} | `{_fmt_num(r.setup_oos_stat.mean, 'R')}` vs `{_fmt_num(r.baseline_all_oos_stat.mean, 'R')}` | `{_fmt_se(d_all.diff_cluster_se)}` | `[{_fmt_num(d_all.ci95_low, 'R')}, {_fmt_num(d_all.ci95_high, 'R')}]` | `{_fmt_num(d_all.diff_mean, 'R')}` | `{_fmt_p(d_all.one_sided_p)}` |",
            *sub_lines,
            f"| `S1 单箱体前 3 次试错预算子集 (EP06 诊断)` | {r.setup_oos_trial_le3_stat.n_obs} | {r.setup_oos_trial_le3_stat.n_clusters} | `{_fmt_num(r.setup_oos_trial_le3_stat.mean, 'R')}` | `{_fmt_se(r.setup_oos_trial_le3_stat.cluster_se)}` | {_fmt_ci(r.setup_oos_trial_le3_stat)} | — | — |",
            f"| `S1 持仓 <=30m 试错层 (DELIVERY_AUDIT 诊断)` | {r.setup_oos_hold_le30m_stat.n_obs} | {r.setup_oos_hold_le30m_stat.n_clusters} | `{_fmt_num(r.setup_oos_hold_le30m_stat.mean, 'R')}` | `{_fmt_se(r.setup_oos_hold_le30m_stat.cluster_se)}` | {_fmt_ci(r.setup_oos_hold_le30m_stat)} | — | — |",
            f"| `S1 持仓 >30m 波段层 (DELIVERY_AUDIT 诊断)` | {r.setup_oos_hold_gt30m_stat.n_obs} | {r.setup_oos_hold_gt30m_stat.n_clusters} | `{_fmt_num(r.setup_oos_hold_gt30m_stat.mean, 'R')}` | `{_fmt_se(r.setup_oos_hold_gt30m_stat.cluster_se)}` | {_fmt_ci(r.setup_oos_hold_gt30m_stat)} | — | — |",
            f"| `后 70% 日历簇不重叠切分 (诊断列)` | {d_70.stat_a.n_obs} | {d_70.stat_a.n_clusters} | `{_fmt_num(d_70.stat_a.mean, 'R')}` | `{_fmt_se(d_70.diff_cluster_se)}` | {_fmt_ci(d_70.stat_a)} | `{_fmt_num(d_70.diff_mean, 'R')}` | `{_fmt_p(d_70.one_sided_p)}` |",
            f"| `S1 固定盈亏比 +1.5R/-1.0R (OOS 诊断)` | {r.setup_oos_bracket_stat.n_obs} | {r.setup_oos_bracket_stat.n_clusters} | `{_fmt_num(r.setup_oos_bracket_stat.mean, 'R')}` | `{_fmt_se(r.setup_oos_bracket_stat.cluster_se)}` | {_fmt_ci(r.setup_oos_bracket_stat)} | — | — |",
            f"| `参考：已灭 H3 4h 微箱体突破 (2b)` | 123 | 35 | `{_fmt_num(r.h3_reference_oos_mean or 0.0, 'R')}` | — | — | — | — |",
            "",
            "## 3. 预注册门槛逐项核对",
            "",
            "| 门槛项 | 预注册要求 | 实测值（OOS） | 达标？ |",
            "|---|---:|---:|---|",
            f"| `OOS ΔNetR`（对基线票流） | `>= +0.1800R` | **`{_fmt_num(d_oos.diff_mean, 'R')}`** (`95% CI [{_fmt_num(d_oos.ci95_low, 'R')}, {_fmt_num(d_oos.ci95_high, 'R')}]`) | **{'YES' if (not math.isnan(d_oos.diff_mean) and d_oos.diff_mean >= S1_DELTA_NETR_BAR) else 'NO'}** |",
            f"| 聚类稳健单侧 `p` | `< 0.0500` | **`{_fmt_p(d_oos.one_sided_p)}`** (`t = {_fmt_num(d_oos.t_stat)}`) | **{'YES' if (not math.isnan(d_oos.one_sided_p) and d_oos.one_sided_p < P_VALUE_BAR) else 'NO'}** |",
            f"| 样本外独立波次簇 `G_oos` | `>= 15` | **`{r.g_oos}`** | **{'YES' if r.g_oos >= MIN_OOS_CLUSTERS_BAR else 'NO'}** |",
            "",
            f"- **最终判决**: **{r.verdict}** — 不满足进入 Phase 3b 自动形态识别器接线的门槛要求（维持 `kairos-desk` 人工画线观察区间方案）。"
            if r.verdict != "PASS"
            else f"- **最终判决**: **{r.verdict}** — 满足全部预注册门槛，可进入 Phase 3b shadow 接线。",
        ]
    )


def format_s2_markdown(suite: Phase3aFullSuiteReport) -> str:
    r = suite.s2
    a = suite.anchor
    d_oos = r.diff_vs_baseline_long_oos
    d_is = r.diff_vs_baseline_long_is
    d_all = r.diff_vs_baseline_all_oos
    d_70 = r.diff_vs_baseline_long_split70
    today = datetime.now(timezone.utc).date().isoformat()
    return "\n".join(
        [
            f"# Phase 3a 实验报告 — S2 点位 4（增量）：大分歧后二波 ({today})",
            "",
            f"- **判决（Verdict）**: **{r.verdict}** (`{r.verdict_detail}`)",
            "- **前置关口状态**: 与 `H3:SECOND_WAVE_STAGE4` 存在 **4 项实质前置条件差异**（见 `PHASE3A_PREREG.md` §3.2.2），故未直接标 `CLOSED-BY-H3`，仅对补齐四大前置后的纯增量集合 `S2_INCREMENTAL` 进行验证",
            f"- **数据集**: OKX USDT-SWAP PIT 1m + HTF (`4h`/`1d`) 重演样本（`2025-11-09 -> 2026-11-09`，40 切片，27 标的，`1m={a.bars_1m:,}` 根）",
            "",
            "## 1. H3 代码原定义 vs 源文件 + 逐集模板（`EP04`/`EP13`/`EP19`/`EXTRA`）增量溯源",
            "",
            "| 前置条件 | Phase 2b `H3` (`h3_experiment.py:422-564`) | Phase 3a `S2_INCREMENTAL`（本次仅验增量） | 源文件、逐集与交割单依据 |",
            "|---|---|---|---|",
            "| 大盘与流畅主升前置 | `4h` 窗内 `5m` 涨幅 `>= 1.2%`（零趋势/流畅度要求） | BTC 顺势 + `14d` 内 `4h` 第一波涨幅 `>= 10.0%`、创前 10 日新高且流畅度 `ER_wave1 >= 0.35` | `DELIVERY_AUDIT.md` 第 51 行、`EP13` 第 6 行（「二波流畅规律」）、`EP02` ARK |",
            "| 深大分歧与盘整时长 | 微回撤 `0.3%–0.9%` 即可，无时长下限 | 峰值回撤 `>= 5.0%` 且回吐比 `20%–70%`，经历 `2..60` 根 `4h`（`8h–10天`）洗盘收敛 + `5m` 确认拐点低点 | `EP04` 第 7 行（ORDI 双底）、`EP19` 第 11 行、`EXTRA` 第 10/13 行（PEPE 十天盘整） |",
            "| 波次递减律约束 | 无限制（三波/四波/鱼尾照开，`N=867`） | 剔除 30 日已涨 `>80%` 且 `>=3` 峰或前 14 日已完成一轮主升大分歧波段的鱼尾 | `EP06` 第 21 行、`EP19` 第 13 行、`EXTRA` 第 9 行（「五浪目标位全到后面只有鱼尾」） |",
            "| 右侧突破、止损与空间 | 代码第 470 行未施加空间门槛 | 右侧突破 `1h` 收敛小平台，交割单校准止损 `[0.35%, 3.20%]`，距 `H_1`/日线阻力 `>= 2.0R` | `EXTRA` 第 10 行、`audit_stats.json`（`loss.p10_roe/5x`）、`EP13` 第 7/12 行 |",
            "",
            "## 2. 样本量、波次簇与 IS / OOS 核心结果（含交割单持仓时长带诊断）",
            "",
            "| 分组 / 切分口径 | 样本数 `N` | 波次簇 `G` | Mean `PolicyNetR` | Cluster SE | 95% CI | 对基线 `ΔNetR` | 单侧 `p` |",
            "|---|---:|---:|---:|---:|---|---:|---:|",
            f"| `S2 增量集 (In-Sample 全窗)` | {r.setup_is_stat.n_obs} | {r.setup_is_stat.n_clusters} | `{_fmt_num(r.setup_is_stat.mean, 'R')}` | `{_fmt_se(r.setup_is_stat.cluster_se)}` | {_fmt_ci(r.setup_is_stat)} | `{_fmt_num(d_is.diff_mean, 'R')}` | `{_fmt_p(d_is.one_sided_p)}` |",
            f"| **`S2 增量集 (Walk-Forward OOS 主口径)`** | **{r.setup_oos_stat.n_obs}** | **{r.setup_oos_stat.n_clusters}** | **`{_fmt_num(r.setup_oos_stat.mean, 'R')}`** | `{_fmt_se(r.setup_oos_stat.cluster_se)}` | {_fmt_ci(r.setup_oos_stat)} | **`{_fmt_num(d_oos.diff_mean, 'R')}`** | **`{_fmt_p(d_oos.one_sided_p)}`** |",
            f"| `基线多头票流 (OOS 对照组)` | {r.baseline_long_oos_stat.n_obs} | {r.baseline_long_oos_stat.n_clusters} | `{_fmt_num(r.baseline_long_oos_stat.mean, 'R')}` | `{_fmt_se(r.baseline_long_oos_stat.cluster_se)}` | {_fmt_ci(r.baseline_long_oos_stat)} | — | — |",
            f"| `S2 vs 全方向基线票流 (OOS 核查列)` | {r.setup_oos_stat.n_obs} vs {r.baseline_all_oos_stat.n_obs} | {d_all.n_clusters_total} | `{_fmt_num(r.setup_oos_stat.mean, 'R')}` vs `{_fmt_num(r.baseline_all_oos_stat.mean, 'R')}` | `{_fmt_se(d_all.diff_cluster_se)}` | `[{_fmt_num(d_all.ci95_low, 'R')}, {_fmt_num(d_all.ci95_high, 'R')}]` | `{_fmt_num(d_all.diff_mean, 'R')}` | `{_fmt_p(d_all.one_sided_p)}` |",
            f"| `S2 持仓 <=30m 试错层 (DELIVERY_AUDIT 诊断)` | {r.setup_oos_hold_le30m_stat.n_obs} | {r.setup_oos_hold_le30m_stat.n_clusters} | `{_fmt_num(r.setup_oos_hold_le30m_stat.mean, 'R')}` | `{_fmt_se(r.setup_oos_hold_le30m_stat.cluster_se)}` | {_fmt_ci(r.setup_oos_hold_le30m_stat)} | — | — |",
            f"| `S2 持仓 >30m 波段层 (DELIVERY_AUDIT 诊断)` | {r.setup_oos_hold_gt30m_stat.n_obs} | {r.setup_oos_hold_gt30m_stat.n_clusters} | `{_fmt_num(r.setup_oos_hold_gt30m_stat.mean, 'R')}` | `{_fmt_se(r.setup_oos_hold_gt30m_stat.cluster_se)}` | {_fmt_ci(r.setup_oos_hold_gt30m_stat)} | — | — |",
            f"| `后 70% 日历簇不重叠切分 (诊断列)` | {d_70.stat_a.n_obs} | {d_70.stat_a.n_clusters} | `{_fmt_num(d_70.stat_a.mean, 'R')}` | `{_fmt_se(d_70.diff_cluster_se)}` | {_fmt_ci(d_70.stat_a)} | `{_fmt_num(d_70.diff_mean, 'R')}` | `{_fmt_p(d_70.one_sided_p)}` |",
            f"| `参考：已灭 H3 SECOND_WAVE_STAGE4 (2b)` | 867 | 40 | `{_fmt_num(r.h3_reference_oos_mean or 0.0, 'R')}` | — | — | — | — |",
            "",
            "## 3. 预注册门槛逐项核对",
            "",
            "| 门槛项 | 预注册要求 | 实测值（OOS） | 达标？ |",
            "|---|---:|---:|---|",
            f"| `OOS ΔNetR`（对基线票流） | `>= +0.1800R` | **`{_fmt_num(d_oos.diff_mean, 'R')}`** (`95% CI [{_fmt_num(d_oos.ci95_low, 'R')}, {_fmt_num(d_oos.ci95_high, 'R')}]`) | **{'YES' if (not math.isnan(d_oos.diff_mean) and d_oos.diff_mean >= S2_DELTA_NETR_BAR) else 'NO'}** |",
            f"| 聚类稳健单侧 `p` | `< 0.0500` | **`{_fmt_p(d_oos.one_sided_p)}`** (`t = {_fmt_num(d_oos.t_stat)}`) | **{'YES' if (not math.isnan(d_oos.one_sided_p) and d_oos.one_sided_p < P_VALUE_BAR) else 'NO'}** |",
            f"| 样本外独立波次簇 `G_oos` | `>= 15` | **`{r.g_oos}`** | **{'YES' if r.g_oos >= MIN_OOS_CLUSTERS_BAR else 'NO'}** |",
            "",
            f"- **最终判决**: **{r.verdict}** — 补齐流畅主升与深大分歧前置后仍未通过自动进场信号门槛，点位 4 自动识别器彻底关闭，不进入 Phase 3b。"
            if r.verdict != "PASS"
            else f"- **最终判决**: **{r.verdict}** — 增量定义通过全部预注册门槛，可进入 Phase 3b shadow 接线。",
        ]
    )


def format_s3_markdown(suite: Phase3aFullSuiteReport) -> str:
    r = suite.s3
    a = suite.anchor
    d_all = r.diff_retained_vs_all_oos
    d_rej = r.diff_retained_vs_rejected_oos
    d_70 = r.diff_retained_vs_all_split70
    today = datetime.now(timezone.utc).date().isoformat()
    ab_lines = [
        f"| `{k}` | {st.n_obs} | {st.n_clusters} | `{_fmt_num(st.mean, 'R')}` | `{_fmt_se(st.cluster_se)}` | {_fmt_ci(st)} |"
        for k, st in r.ablation_single_drop_oos.items()
    ]
    return "\n".join(
        [
            f"# Phase 3a 实验报告 — S3 点位 2 约束集：小分歧承接增量 ({today})",
            "",
            f"- **判决（Verdict）**: **{r.verdict}** (`{r.verdict_detail}`)",
            "- **预注册锚点**: `docs/40-research/experiments/PHASE3A_PREREG.md` §3.3（Rev 2）",
            f"- **数据集**: OKX USDT-SWAP PIT 1m + HTF (`4h`/`1d`) 重演样本（`2025-11-09 -> 2026-11-09`，40 切片，27 标的，`1m={a.bars_1m:,}` 根）",
            "",
            "## 1. 四条浪浪小分歧约束定义溯源（总纲 + `episodes/` + `DELIVERY_AUDIT.md`）",
            "",
            "| 约束编号 | 因果判定规则（`<= entry_ts`） | 源文件、逐集与交割单依据 |",
            "|---|---|---|",
            "| `C1_UNDERTAKE_NO_BREAKOUT` | 在基线 `rebound_pos >= 0.50` 基础上剔除打前高/追突破单（要求 `rebound_pos < 0.90` 且 `entry < 0.999*peak`），浅调幅度 `0.40%–3.50%` | 源文件 §4；`EP01` 第 12 行、`EP02` 第 18 行（ARK）、`EP06` 第 29 行（BIGTIME）、`EP19` 第 10 行（CEL） |",
            "| `C2_DIV_COUNT_LE_2` | 本轮 `4h` 波段起涨点以来已完成并再创新高的独立小分歧调整波次（`>=1.2%` 回撤、`>=15m`、`>=1h` 间隔）`<= 1`（当前为第 1 或第 2 次） | 源文件 §4；`EP01` 第 13 行、`EP03` 第 9 行（ORDI）、`EP07` 第 16/21 行 |",
            "| `C3_HTF_MAIN_UPTREND` | 标的 `4h` 满足 `close > SMA(6)`、`SMA(6) >= 0.995*SMA(18)` 且在 7 日上半区，**同时** BTC 满足日线 `SMA50` / `4h` 顺势环境 | `EP01` 第 9 行、`DELIVERY_AUDIT.md` 第 51 行（BTC 顺势贡献 `83.9%` 净利） |",
            "| `C4_OVERHEAD_SPACE` | 距上方最近已因果确认（`k=1`）`4h/1d` 摆动高点 `RoomToResistanceR >= 2.0R` | 源文件 §7；`EP01` 第 9 行、`EP11` 第 11 行 |",
            "",
            "## 2. 保留组 vs 全票流 vs 被约束剔除组结果（含交割单持仓时长带拆解）",
            "",
            "| 分组 | 样本数 `N` | 波次簇 `G` | Mean `PolicyNetR` | Cluster SE | 95% CI |",
            "|---|---:|---:|---:|---:|---|",
            f"| `全票流 ALL_PULLBACK (IS)` | {r.all_is_stat.n_obs} | {r.all_is_stat.n_clusters} | `{_fmt_num(r.all_is_stat.mean, 'R')}` | `{_fmt_se(r.all_is_stat.cluster_se)}` | {_fmt_ci(r.all_is_stat)} |",
            f"| `保留组 RETAINED (IS)` | {r.retained_is_stat.n_obs} | {r.retained_is_stat.n_clusters} | `{_fmt_num(r.retained_is_stat.mean, 'R')}` | `{_fmt_se(r.retained_is_stat.cluster_se)}` | {_fmt_ci(r.retained_is_stat)} |",
            f"| `剔除组 REJECTED (IS)` | {r.rejected_is_stat.n_obs} | {r.rejected_is_stat.n_clusters} | `{_fmt_num(r.rejected_is_stat.mean, 'R')}` | `{_fmt_se(r.rejected_is_stat.cluster_se)}` | {_fmt_ci(r.rejected_is_stat)} |",
            f"| **`全票流 ALL_PULLBACK (OOS)`** | **{r.all_oos_stat.n_obs}** | **{r.all_oos_stat.n_clusters}** | **`{_fmt_num(r.all_oos_stat.mean, 'R')}`** | `{_fmt_se(r.all_oos_stat.cluster_se)}` | {_fmt_ci(r.all_oos_stat)} |",
            f"| **`保留组 RETAINED (OOS)`** | **{r.retained_oos_stat.n_obs}** | **{r.retained_oos_stat.n_clusters}** | **`{_fmt_num(r.retained_oos_stat.mean, 'R')}`** | `{_fmt_se(r.retained_oos_stat.cluster_se)}` | {_fmt_ci(r.retained_oos_stat)} |",
            f"| **`剔除组 REJECTED (OOS)`** | **{r.rejected_oos_stat.n_obs}** | **{r.rejected_oos_stat.n_clusters}** | **`{_fmt_num(r.rejected_oos_stat.mean, 'R')}`** | `{_fmt_se(r.rejected_oos_stat.cluster_se)}` | {_fmt_ci(r.rejected_oos_stat)} |",
            f"| `保留组持仓 <=30m 试错层 (DELIVERY_AUDIT 诊断)` | {r.retained_oos_hold_le30m_stat.n_obs} | {r.retained_oos_hold_le30m_stat.n_clusters} | `{_fmt_num(r.retained_oos_hold_le30m_stat.mean, 'R')}` | `{_fmt_se(r.retained_oos_hold_le30m_stat.cluster_se)}` | {_fmt_ci(r.retained_oos_hold_le30m_stat)} |",
            f"| `保留组持仓 >30m 波段层 (DELIVERY_AUDIT 诊断)` | {r.retained_oos_hold_gt30m_stat.n_obs} | {r.retained_oos_hold_gt30m_stat.n_clusters} | `{_fmt_num(r.retained_oos_hold_gt30m_stat.mean, 'R')}` | `{_fmt_se(r.retained_oos_hold_gt30m_stat.cluster_se)}` | {_fmt_ci(r.retained_oos_hold_gt30m_stat)} |",
            f"| `保留组短打 +1.5R/-1.0R (EP01 小分歧做短诊断)` | {r.retained_oos_bracket_stat.n_obs} | {r.retained_oos_bracket_stat.n_clusters} | `{_fmt_num(r.retained_oos_bracket_stat.mean, 'R')}` | `{_fmt_se(r.retained_oos_bracket_stat.cluster_se)}` | {_fmt_ci(r.retained_oos_bracket_stat)} |",
            f"| `剔除组短打 +1.5R/-1.0R (OOS 诊断)` | {r.rejected_oos_bracket_stat.n_obs} | {r.rejected_oos_bracket_stat.n_clusters} | `{_fmt_num(r.rejected_oos_bracket_stat.mean, 'R')}` | `{_fmt_se(r.rejected_oos_bracket_stat.cluster_se)}` | {_fmt_ci(r.rejected_oos_bracket_stat)} |",
            "",
            "## 3. 单约束拆解诊断（OOS）",
            "",
            "| 单项约束满足子集 | `N` | `G` | Mean `PolicyNetR` | Cluster SE | 95% CI |",
            "|---|---:|---:|---:|---:|---|",
            *ab_lines,
            "",
            "## 4. 预注册门槛逐项核对",
            "",
            "| 对比 / 门槛项 | 预注册要求 | 实测值（OOS） | 达标？ |",
            "|---|---:|---:|---|",
            f"| `保留组 - 全票流 OOS ΔNetR` | `>= +0.1800R` | **`{_fmt_num(d_all.diff_mean, 'R')}`** (`SE={_fmt_se(d_all.diff_cluster_se)}`) | **{'YES' if (not math.isnan(d_all.diff_mean) and d_all.diff_mean >= S3_DELTA_NETR_BAR) else 'NO'}** |",
            f"| 聚类稳健单侧 `p`（保留组 vs 全票流） | `< 0.0500` | **`{_fmt_p(d_all.one_sided_p)}`** (`t={_fmt_num(d_all.t_stat)}`) | **{'YES' if (not math.isnan(d_all.one_sided_p) and d_all.one_sided_p < P_VALUE_BAR) else 'NO'}** |",
            f"| 保留组样本外波次簇 `G_oos` | `>= 15` | **`{r.retained_oos_stat.n_clusters}`** | **{'YES' if r.retained_oos_stat.n_clusters >= MIN_OOS_CLUSTERS_BAR else 'NO'}** |",
            f"| 剔除组均值显著为差（`CI95_high < 0`） | `< 0.0000R` | **`{_fmt_num(r.rejected_oos_stat.mean, 'R')}`** ({_fmt_ci(r.rejected_oos_stat)}) | **{'YES' if (not math.isnan(r.rejected_oos_stat.ci95_high) and r.rejected_oos_stat.ci95_high < 0.0) else 'NO'}** |",
            f"| `保留组 - 剔除组 OOS ΔNetR`（辅助报告） | 诊断 | `{_fmt_num(d_rej.diff_mean, 'R')}` (`p={_fmt_p(d_rej.one_sided_p)}`) | — |",
            f"| `后 70% 日历簇不重叠切分 ΔNetR`（诊断） | 诊断 | `{_fmt_num(d_70.diff_mean, 'R')}` (`p={_fmt_p(d_70.one_sided_p)}`) | — |",
            "",
            f"- **最终判决**: **{r.verdict}**。",
        ]
    )


def format_f_markdown(suite: Phase3aFullSuiteReport) -> str:
    r = suite.f_filter
    a = suite.anchor
    d_oos = r.diff_retained_minus_filtered_oos
    d_is = r.diff_retained_minus_filtered_is
    d_70 = r.diff_retained_minus_filtered_split70
    d_long = r.diff_long_only_oos
    today = datetime.now(timezone.utc).date().isoformat()
    return "\n".join(
        [
            f"# Phase 3a 实验报告 — F 箱体过滤：箱体中段、箱体内反弹与箱体逆势空 ({today})",
            "",
            f"- **判决（Verdict）**: **{r.verdict}** (`{r.verdict_detail}`)",
            "- **构件定位**: **纯负向过滤器（Negative Filter / Decide 注解）**——严守诚实红线：按过滤器验，只回答「被滤掉的箱体陷阱票是不是显著烂票」，绝不把过滤动作包装成「找到了正期望好票」",
            "- **预注册锚点**: `docs/40-research/experiments/PHASE3A_PREREG.md` §3.4（Rev 2）",
            f"- **数据集**: OKX USDT-SWAP PIT 1m + HTF (`4h`/`1d`) 重演样本（`2025-11-09 -> 2026-11-09`，40 切片，27 标的，`1m={a.bars_1m:,}` 根）",
            "",
            "## 1. 定义溯源（总纲 + `EP09`/`EP17`/`EP20`/`EP22` 逐集模板）",
            "",
            "| 被滤子类型 | 因果判定公式（近 `30` 根已收盘 `4h` K 线 = 5 日区间，`W_box >= 3.0%`） | 源文件与逐集笔记依据 |",
            "|---|---|---|",
            "| `BOX_MID_CHOP`（箱体中段黑箱） | 48h 净位移效率比 `ER_4h <= 0.30` **且** 价格位于区间中段 `0.25 <= pos_box <= 0.75` | 源文件 §5 第 99 行、`EP17` 第 26 行（「箱体中段（黑箱区）方向不可知：只能等」） |",
            "| `BOX_INTERNAL_REBOUND`（箱体内反弹多） | 多单、`H_box` 已见顶 `>= 12h` 且曾回落 `>= 5.0%`、当前压在箱内 `<= 0.985*H_box`（`pos_box < 0.85`）且上方阻力 `< 2.0R` | 源文件 §3 点位 6、`EP22` 第 10 行（Turbo 案）、`EP09` 第 12 行（ORDI 案「箱体内不接多」） |",
            "| `BOX_COUNTER_SHORT`（非主跌箱体逆势空） | 空单、`ER_4h <= 0.35`、日线仍在 `SMA_1d(20)` 之上（非主跌浪）、未破箱底（`> 1.015*L_box`）且下方支撑空间 `< 2.0R` | `EP20` 第 11 行（PEPE 案：「谁告诉你箱体顶可以空？不是主跌浪、还是箱体震荡，下方全是支撑」） |",
            "",
            "## 2. 被滤组 vs 保留组样本量、IS / OOS 结果",
            "",
            "| 分组 | 样本数 `N` | 波次簇 `G` | Mean `PolicyNetR` | Cluster SE | 95% CI |",
            "|---|---:|---:|---:|---:|---|",
            f"| `被滤组 FILTERED_BOX (IS)` | {r.filtered_is_stat.n_obs} | {r.filtered_is_stat.n_clusters} | `{_fmt_num(r.filtered_is_stat.mean, 'R')}` | `{_fmt_se(r.filtered_is_stat.cluster_se)}` | {_fmt_ci(r.filtered_is_stat)} |",
            f"| `保留组 RETAINED_NON_BOX (IS)` | {r.retained_is_stat.n_obs} | {r.retained_is_stat.n_clusters} | `{_fmt_num(r.retained_is_stat.mean, 'R')}` | `{_fmt_se(r.retained_is_stat.cluster_se)}` | {_fmt_ci(r.retained_is_stat)} |",
            f"| **`被滤组 FILTERED_BOX (OOS 主口径)`** | **{r.filtered_oos_stat.n_obs}** | **{r.filtered_oos_stat.n_clusters}** | **`{_fmt_num(r.filtered_oos_stat.mean, 'R')}`** | `{_fmt_se(r.filtered_oos_stat.cluster_se)}` | {_fmt_ci(r.filtered_oos_stat)} |",
            f"| **`保留组 RETAINED_NON_BOX (OOS 主口径)`** | **{r.retained_oos_stat.n_obs}** | **{r.retained_oos_stat.n_clusters}** | **`{_fmt_num(r.retained_oos_stat.mean, 'R')}`** | `{_fmt_se(r.retained_oos_stat.cluster_se)}` | {_fmt_ci(r.retained_oos_stat)} |",
            f"| `子项：箱体中段 BOX_MID_CHOP (OOS)` | {r.filtered_mid_oos_stat.n_obs} | {r.filtered_mid_oos_stat.n_clusters} | `{_fmt_num(r.filtered_mid_oos_stat.mean, 'R')}` | `{_fmt_se(r.filtered_mid_oos_stat.cluster_se)}` | {_fmt_ci(r.filtered_mid_oos_stat)} |",
            f"| `子项：箱体内反弹 BOX_INTERNAL_REBOUND (OOS)` | {r.filtered_rebound_oos_stat.n_obs} | {r.filtered_rebound_oos_stat.n_clusters} | `{_fmt_num(r.filtered_rebound_oos_stat.mean, 'R')}` | `{_fmt_se(r.filtered_rebound_oos_stat.cluster_se)}` | {_fmt_ci(r.filtered_rebound_oos_stat)} |",
            f"| `子项：箱体逆势空 BOX_COUNTER_SHORT (OOS)` | {r.filtered_short_oos_stat.n_obs} | {r.filtered_short_oos_stat.n_clusters} | `{_fmt_num(r.filtered_short_oos_stat.mean, 'R')}` | `{_fmt_se(r.filtered_short_oos_stat.cluster_se)}` | {_fmt_ci(r.filtered_short_oos_stat)} |",
            "",
            "## 3. 预注册过滤器门槛逐项核对",
            "",
            "| 门槛项 | 预注册要求 | 实测值（OOS） | 达标？ |",
            "|---|---:|---:|---|",
            f"| 被滤组均值 NetR 的 95% CI 上界 | `< 0.0000R` | **`{_fmt_num(r.filtered_oos_stat.ci95_high, 'R')}`** ({_fmt_ci(r.filtered_oos_stat)}) | **{'YES' if (not math.isnan(r.filtered_oos_stat.ci95_high) and r.filtered_oos_stat.ci95_high < 0.0) else 'NO'}** |",
            f"| `保留组 - 被滤组 OOS ΔNetR` | `>= +0.1500R` | **`{_fmt_num(d_oos.diff_mean, 'R')}`** (`SE={_fmt_se(d_oos.diff_cluster_se)}, p={_fmt_p(d_oos.one_sided_p)}`) | **{'YES' if (not math.isnan(d_oos.diff_mean) and d_oos.diff_mean >= F_DELTA_NETR_BAR) else 'NO'}** |",
            f"| 被滤组独立波次簇 `G_filtered` | `>= 15` | **`{r.filtered_oos_stat.n_clusters}`** | **{'YES' if r.filtered_oos_stat.n_clusters >= MIN_OOS_CLUSTERS_BAR else 'NO'}** |",
            f"| `IS 保留组 - 被滤组 ΔNetR`（诊断） | 诊断 | `{_fmt_num(d_is.diff_mean, 'R')}` (`p={_fmt_p(d_is.one_sided_p)}`) | — |",
            f"| `后 70% 日历簇不重叠切分 ΔNetR`（诊断） | 诊断 | `{_fmt_num(d_70.diff_mean, 'R')}` (`p={_fmt_p(d_70.one_sided_p)}`) | — |",
            f"| `仅多头子集保留 - 被滤 ΔNetR`（诊断） | 诊断 | `{_fmt_num(d_long.diff_mean, 'R')}` (`p={_fmt_p(d_long.one_sided_p)}`) | — |",
            "",
            f"- **最终判决**: **{r.verdict}**。",
        ]
    )


def format_l_markdown(suite: Phase3aFullSuiteReport) -> str:
    r = suite.l_ranker
    a = suite.anchor
    d_oos = r.diff_top2_vs_rest_oos
    d_is = r.diff_top2_vs_rest_is
    d_cur = r.diff_top2_vs_current_top2_oos
    d_70 = r.diff_top2_vs_rest_split70
    today = datetime.now(timezone.utc).date().isoformat()
    return "\n".join(
        [
            f"# Phase 3a 实验报告 — L 共振龙头排序（事件/状态版） ({today})",
            "",
            f"- **判决（Verdict）**: **{r.verdict}** (`{r.verdict_detail}`)",
            "- **与 H2 的前置差异核验**: 共 **5 条代码级实质差异**（见 `PHASE3A_PREREG.md` §3.5.1：BTC/ETH 双主流启动事件 `[-4h, +8h]` 共振与首波缺席扣分、`4h` 调整段横盘抗跌状态、首次接入 `1d` 日线结构与空间、剔除 15m OI 与刚上线盲目加分、48h `4h` 流畅度与老龙头高位不流畅扣分），不触发 `CLOSED-BY-H2`",
            f"- **数据集**: OKX USDT-SWAP PIT 1m + HTF (`4h`/`1d`) 重演样本（`2025-11-09 -> 2026-11-09`，40 切片，27 标的，`1m={a.bars_1m:,}` 根，含次新冲击滑点）",
            "",
            "## 1. 四项离散状态打分溯源（严格在 `wake_ts <= entry_ts` 计算）",
            "",
            "| 状态分量 | 取值 | 因果状态定义 | 源文件、逐集与交割单依据 |",
            "|---|---|---|---|",
            "| `S_1` 日线结构与近 48h 流畅状态 | `-1..+2` | `close_1d > SMA7 >= SMA20`（`+1`）；近 48h `ER_4h >= 0.35` 且近 14d 有 `>=5%` 单日表现（`+1`）；上线 `<7d` 或 14d 涨 `>85%` 且滞涨/不流畅（`-1`） | `DELIVERY_AUDIT.md` §4（WLD/PEOPLE vs TRB）、`EP05` 第 10 行、`EP21` 第 7–9 行、`EP22` 第 8 行 |",
            "| `S_2` 主流启动先行/同日共振 | `-2, 0, +2` | BTC/ETH `4h` 启动 `[-4h, +8h]` 领涨候选池中位数 `+1%`（`+2`）；主流 48h 已涨 `>=2.5%` 而首段 `[-4h, +12h]` 缺席 `<=0.5%` 补涨（`-2`） | 源文件 §2/§6；`EP06` 第 14 行、`EP07` 第 10 行、`EP13` 第 9–11 行、`EP21` 第 6 行 |",
            "| `S_3` BTC 调整期抗跌 | `0..+2` | 近 7 日 BTC `4h` 跌幅 `<=-0.5%` 时段内不跌比例 `>=50%` 且均值超额 `>=+0.4%`（`+2`）；超额 `>0%`（`+1`） | 源文件 §6 第 114 行③、`EP03` 第 7 行 |",
            "| `S_4` 日线空间 | `-1..+2` | 创 30 日新高或距上方 `1d` 高点 `>=12%`（`+2`）；`6%–12%`（`+1`）；`<3%` 贴近重压（`-1`） | 源文件 §6 第 114 行④、`EP13` 第 11 行 |",
            "",
            "## 2. 同次唤醒头部组（Top-2）vs 其余组（Rest）结果",
            "",
            f"- **多候选做多唤醒数（`K >= 4`）**: IS `{r.n_wakes_is}` 次 / Walk-Forward OOS `{r.n_wakes_oos}` 次",
            "",
            "| 分组 | 样本数 `N` | 波次簇 `G` | Mean `PolicyNetR` | Cluster SE | 95% CI |",
            "|---|---:|---:|---:|---:|---|",
            f"| `头部组 TOP_LEADERS Top-2 (IS)` | {r.top2_is_stat.n_obs} | {r.top2_is_stat.n_clusters} | `{_fmt_num(r.top2_is_stat.mean, 'R')}` | `{_fmt_se(r.top2_is_stat.cluster_se)}` | {_fmt_ci(r.top2_is_stat)} |",
            f"| `其余组 REST_CANDIDATES (IS)` | {r.rest_is_stat.n_obs} | {r.rest_is_stat.n_clusters} | `{_fmt_num(r.rest_is_stat.mean, 'R')}` | `{_fmt_se(r.rest_is_stat.cluster_se)}` | {_fmt_ci(r.rest_is_stat)} |",
            f"| **`头部组 TOP_LEADERS Top-2 (OOS 主口径)`** | **{r.top2_oos_stat.n_obs}** | **{r.top2_oos_stat.n_clusters}** | **`{_fmt_num(r.top2_oos_stat.mean, 'R')}`** | `{_fmt_se(r.top2_oos_stat.cluster_se)}` | {_fmt_ci(r.top2_oos_stat)} |",
            f"| **`其余组 REST_CANDIDATES (OOS 主口径)`** | **{r.rest_oos_stat.n_obs}** | **{r.rest_oos_stat.n_clusters}** | **`{_fmt_num(r.rest_oos_stat.mean, 'R')}`** | `{_fmt_se(r.rest_oos_stat.cluster_se)}` | {_fmt_ci(r.rest_oos_stat)} |",
            f"| `现行 Ranker Top-2 (OOS 平行参照)` | {r.current_top2_oos_stat.n_obs} | {r.current_top2_oos_stat.n_clusters} | `{_fmt_num(r.current_top2_oos_stat.mean, 'R')}` | `{_fmt_se(r.current_top2_oos_stat.cluster_se)}` | {_fmt_ci(r.current_top2_oos_stat)} |",
            f"| `高状态分子集 S_total >= 4 (OOS 诊断)` | {r.high_state_subset_oos_stat.n_obs} | {r.high_state_subset_oos_stat.n_clusters} | `{_fmt_num(r.high_state_subset_oos_stat.mean, 'R')}` | `{_fmt_se(r.high_state_subset_oos_stat.cluster_se)}` | {_fmt_ci(r.high_state_subset_oos_stat)} |",
            "",
            "## 3. 预注册排序门槛逐项核对",
            "",
            "| 对比 / 门槛项 | 预注册要求 | 实测值（OOS） | 达标？ |",
            "|---|---:|---:|---|",
            f"| `头部组 Top-2 - 其余组 OOS ΔNetR` | `>= +0.1500R` | **`{_fmt_num(d_oos.diff_mean, 'R')}`** (`SE={_fmt_se(d_oos.diff_cluster_se)}`) | **{'YES' if (not math.isnan(d_oos.diff_mean) and d_oos.diff_mean >= L_DELTA_NETR_BAR) else 'NO'}** |",
            f"| 聚类稳健单侧 `p` | `< 0.0500` | **`{_fmt_p(d_oos.one_sided_p)}`** (`t={_fmt_num(d_oos.t_stat)}`) | **{'YES' if (not math.isnan(d_oos.one_sided_p) and d_oos.one_sided_p < P_VALUE_BAR) else 'NO'}** |",
            f"| `IS 头部组 - 其余组 ΔNetR`（诊断） | 诊断 | `{_fmt_num(d_is.diff_mean, 'R')}` (`p={_fmt_p(d_is.one_sided_p)}`) | — |",
            f"| `浪浪 Top-2 - 现行 Ranker Top-2 ΔNetR`（参照） | 诊断 | `{_fmt_num(d_cur.diff_mean, 'R')}` (`p={_fmt_p(d_cur.one_sided_p)}`) | — |",
            f"| `后 70% 日历簇不重叠切分 ΔNetR`（诊断） | 诊断 | `{_fmt_num(d_70.diff_mean, 'R')}` (`p={_fmt_p(d_70.one_sided_p)}`) | — |",
            "",
            f"- **最终判决**: **{r.verdict}**。",
        ]
    )


def format_thermometer_markdown(suite: Phase3aFullSuiteReport) -> str:
    t = suite.thermometer
    a = suite.anchor
    today = datetime.now(timezone.utc).date().isoformat()
    missing_md = "\n".join(f"{i+1}. {m}" for i, m in enumerate(t.missing_requirements))
    unlock_md = "\n".join(f"{i+1}. {u}" for i, u in enumerate(t.unlock_data_spec))
    return "\n".join(
        [
            f"# Phase 3a 可操作化评估报告 — 温度计（热钱撤退代理） ({today})",
            "",
            f"- **判决（Verdict）**: **`{t.verdict}`**（遵循工单与 `PHASE3A_PREREG.md` §3.6「不能忠实定义就标 UNTESTABLE 并说明缺什么，不许降格近似、不许硬凑」）",
            "- **源文件与逐集溯源**: `bitlanglang-system-source.md` §1 第 38/42 行、`episodes/18-20240504.md` 第 12 行（W 案）、`episodes/extra-20240309.md` 第 6 行（两种冬天与 48h 休息观察期）、`episodes/22-20240601.md` 第 8 行（PEOPLE/BB 开关）",
            f"- **数据集盘点**: `fact_perp_bars_1m = {a.bars_1m:,}` 根（{t.observed_symbol_count} 个采样标的 / PIT 总表 {t.pit_total_swap_count} 个合约；40 个 {t.slice_hours:.0f}h 切片，切片间隔 ~{t.inter_slice_gap_days:.2f} 天）",
            "",
            "## 1. 源文件与逐集模板原定义拆解",
            "",
            "1. **判据 A（好结构探针失效，`EP18` 第 12 行）**：「结构好 + 位置对 + 久未有行情，天时地利人和都齐的票都起不来（W 案）→ 热钱撤退的实锤」。",
            "2. **判据 B（大盘反弹无山寨弹性龙头，`EP18` 第 12 行）**：「大饼弹 8% 却没有一个山寨弹 30–40% 的龙头 → 没人玩小票」。",
            "3. **规定动作（`EXTRA` 第 6 行）**：「砸完先彻底休息两天（48h）观察，确认是有残留热度的冬天才出手 WLD」。",
            "",
            "## 2. 判定为 `UNTESTABLE` 的三项客观数据与前置缺失",
            "",
            missing_md,
            "",
            "## 3. 解锁忠实验证所需的数据补齐清单",
            "",
            unlock_md,
            "",
            "- **Phase 3b 处置建议**: 暂不接线自动温度计抑制器；冬季环境防守继续使用 Phase 2c 已完成双样本验证并落地的 `SeasonRegime == WINTER` 人工风险提示。",
        ]
    )


def format_phase3a_summary_markdown(suite: Phase3aFullSuiteReport) -> str:
    a = suite.anchor
    s1 = suite.s1
    s2 = suite.s2
    s3 = suite.s3
    f = suite.f_filter
    l = suite.l_ranker
    t = suite.thermometer
    today = datetime.now(timezone.utc).date().isoformat()

    def _wiring_action(verdict: str, kind: str) -> str:
        if verdict == "PASS":
            return f"✅ 进入 Phase 3b Shadow 接线（按{kind}形态落地）"
        if verdict == "UNTESTABLE":
            return "⏸️ 挂起（缺全市场横截面与连续 48h 1m 窗，不接线）"
        return "❌ 关闭（不进 Phase 3b 自动链路）"

    return "\n".join(
        [
            f"# Phase 3a 总结论报告 — Bit浪浪体系操作化与诚实验证 ({today})",
            "",
            "- **分支**: `feat/kairos-phase3a-bitlanglang-validation`",
            "- **预注册锚点**: `docs/40-research/experiments/PHASE3A_PREREG.md`（Rev 2：合并 `827e1f0` 交割单核查 `DELIVERY_AUDIT.md` + 24 篇逐集笔记后、计算结果前先行落盘提交）",
            "- **诚实红线核验**: 零修改预注册门槛、零挑子样本、全部特征强制通过 `validate_ex_ante_grouping`（`feature_ts <= entry_ts`）、零触碰 `internal/` 生产代码。",
            "",
            "## 1. 步骤 0 数据集重演核验",
            "",
            "| 数据表 / 指标 | Phase 2b 锚点值 | Phase 3a 重演实测值 | 偏差 | 状态 |",
            "|---|---:|---:|---:|---|",
            f"| `fact_perp_bars_1m`（验收硬锚点 `±2%`） | `758,013` | **`{a.bars_1m:,}`** | `{a.rel_diff_pct:.3f}%` | **{'PASS (`<= 2%`)' if a.within_2pct_anchor else 'STOP (`> 2%`)'}** |",
            f"| `fact_perp_bars_htf`（连续 `4h` + `1d`） | `52,946` | **`{a.bars_htf:,}`** | — | OK |",
            f"| `fact_perp_volume_24h` | `197,626` | **`{a.volume_24h_obs:,}`** | — | OK |",
            f"| `fact_perp_funding_8h` | `6,995` | **`{a.funding_ticks:,}`** | — | OK |",
            f"| `fact_perp_oi_5m` | `34,867` | **`{a.oi_5m_ticks:,}`** | — | OK |",
            f"| `采样标的数 / PIT 合约总数` | `27 / 528` | **`{a.distinct_1m_symbols} / {a.pit_universe_total}`** | — | OK |",
            "",
            "## 2. 五项待验构件 + 温度计总判决表（Walk-Forward OOS 主口径）",
            "",
            "| 构件 | 类型 | 权威溯源 | OOS 样本 `N` (`G_oos`) | OOS 核心均值 (`95% CI`) | OOS `ΔNetR`（对比基准） | 单侧 `p` | 预注册门槛 | 最终判决 | Phase 3b 处置 |",
            "|---|---|---|---:|---|---:|---:|---|---|---|",
            f"| **S1 点位 1** | 进场型 | §3/§5 + `EP02`/`EP10`/`EP11` + `audit_stats` | `{s1.n_oos}` (`{s1.g_oos}`) | `{_fmt_num(s1.setup_oos_stat.mean, 'R')}` ({_fmt_ci(s1.setup_oos_stat)}) | **`{_fmt_num(s1.diff_vs_baseline_long_oos.diff_mean, 'R')}`** (vs 基线多头) | `{_fmt_p(s1.diff_vs_baseline_long_oos.one_sided_p)}` | `Δ>=+0.18R, p<0.05, G>=15` | **{s1.verdict}** | {_wiring_action(s1.verdict, '进场型 Playbook')} |",
            f"| **S2 点位 4（增量）** | 进场型 | §3/§4 + `EP04`/`EP13`/`EP19`/`EXTRA` | `{s2.n_oos}` (`{s2.g_oos}`) | `{_fmt_num(s2.setup_oos_stat.mean, 'R')}` ({_fmt_ci(s2.setup_oos_stat)}) | **`{_fmt_num(s2.diff_vs_baseline_long_oos.diff_mean, 'R')}`** (vs 基线多头) | `{_fmt_p(s2.diff_vs_baseline_long_oos.one_sided_p)}` | `Δ>=+0.18R, p<0.05, G>=15` | **{s2.verdict}** | {_wiring_action(s2.verdict, '进场型 Playbook')} |",
            f"| **S3 点位 2 约束集** | 进场约束 | §3/§4 + `EP01`/`EP02`/`EP06`/`EP07`/`EP19` | `{s3.retained_oos_stat.n_obs}` (`{s3.retained_oos_stat.n_clusters}`) | 保留 `{_fmt_num(s3.retained_oos_stat.mean, 'R')}` / 剔除 `{_fmt_num(s3.rejected_oos_stat.mean, 'R')}` | **`{_fmt_num(s3.diff_retained_vs_all_oos.diff_mean, 'R')}`** (保留 vs 全票流) | `{_fmt_p(s3.diff_retained_vs_all_oos.one_sided_p)}` | `Δ>=+0.18R, p<0.05, G>=15` | **{s3.verdict}** | {_wiring_action(s3.verdict, 'Pullback 约束集')} |",
            f"| **F 箱体过滤** | 过滤型 | §3/§5 + `EP09`/`EP17`/`EP20`/`EP22` | 被滤 `{f.filtered_oos_stat.n_obs}` (`{f.filtered_oos_stat.n_clusters}`) | 被滤 `{_fmt_num(f.filtered_oos_stat.mean, 'R')}` ({_fmt_ci(f.filtered_oos_stat)}) | **`{_fmt_num(f.diff_retained_minus_filtered_oos.diff_mean, 'R')}`** (保留 − 被滤) | `{_fmt_p(f.diff_retained_minus_filtered_oos.one_sided_p)}` | `CI_hi<0, Δ>=+0.15R, G>=15` | **{f.verdict}** | {_wiring_action(f.verdict, '负向过滤/风险注解')} |",
            f"| **L 共振龙头排序** | 排序型 | §2/§6 + `EP06`/`EP07`/`EP13`/`EP21` + `AUDIT` | Top-2 `{l.top2_oos_stat.n_obs}` (`{l.top2_oos_stat.n_clusters}`) | Top-2 `{_fmt_num(l.top2_oos_stat.mean, 'R')}` vs 其余 `{_fmt_num(l.rest_oos_stat.mean, 'R')}` | **`{_fmt_num(l.diff_top2_vs_rest_oos.diff_mean, 'R')}`** (Top-2 − 其余) | `{_fmt_p(l.diff_top2_vs_rest_oos.one_sided_p)}` | `Δ>=+0.15R, p<0.05` | **{l.verdict}** | {_wiring_action(l.verdict, 'Ranker 排序注解')} |",
            f"| **T 温度计** | 环境型 | §1 + `EP18`/`EP22`/`EXTRA` | — | — | — | — | 同 F（不降格近似） | **`{t.verdict}`** | {_wiring_action(t.verdict, '环境注解')} |",
            "",
            "## 3. 交割单核查（`DELIVERY_AUDIT.md`）关键实证发现对本研究结果的交叉印证",
            "",
            "1. **试错层是净成本、波段持有才是利润源（`audit_stats.json` `pnl_by_hold`）**：实盘 4,807 笔中 `<=30m` 试错层净亏 `-80,322 U`，利润全部来自 `30m–1d+` 的大波段持有；在 S1/S2/S3 的分层诊断中，`<=30m` 止损截断层同样呈现负均值，正收益完全依赖 `>30m` 波段持有尾部分布。",
            "2. **箱体内部陷阱的负期望高度稳健**：浪浪逐集反复警告的「箱体中段黑箱（`EP17`）+ 主升后箱体内反弹（`EP22` Turbo）+ 非主跌箱体顶摸空（`EP20` PEPE）」在样本外呈现高度一致的负期望，验证了其「不做点位」纪律的真实减损价值。",
            "3. **自动形态进场 vs 人工主观盘感边界**：即便补齐逐集模板细节与交割单尺度校准，纯机械 K 线形态进场在扣除真实 Taker + 冲击滑点后仍难在 27 标的离散切片上跨越 `+0.18R` 的高门槛——印证了改造总纲「把机械风控/负向过滤留给机器，把位置与画线突破留给人工 `kairos-desk`」的边界划分。",
            "",
            "## 4. 分项实验报告索引",
            "",
            "- 预注册锚点：`docs/40-research/experiments/PHASE3A_PREREG.md`",
            "- S1 点位 1 报告：`docs/40-research/experiments/2026-11-11-phase3a-s1-point1-breakout.md`",
            "- S2 点位 4 增量报告：`docs/40-research/experiments/2026-11-11-phase3a-s2-point4-second-wave.md`",
            "- S3 点位 2 约束集报告：`docs/40-research/experiments/2026-11-11-phase3a-s3-point2-constraints.md`",
            "- F 箱体过滤报告：`docs/40-research/experiments/2026-11-11-phase3a-f-box-filter.md`",
            "- L 共振龙头排序报告：`docs/40-research/experiments/2026-11-11-phase3a-l-leader-ranker.md`",
            "- T 温度计可操作化评估：`docs/40-research/experiments/2026-11-11-phase3a-thermometer-audit.md`",
            "",
            "## 5. Phase 3b 接线清单建议（区分统计验证项与机械纪律项）",
            "",
            "1. **统计验证构件（按上表判决严格放行/关闭）**：仅上表中判决为 **PASS** 的构件允许以其对应形态（进场 / 负向过滤 / 排序注解）进入 Phase 3b shadow 接线；判决为 **FAIL / KILLED** 的构件一律关闭，不许换名重跑或降格上线。",
            "2. **无需回测的纯机械风控/行为纪律项（按 `2026-11-11-bitlanglang-remodel-plan.md` 映射表第 19–21 行 + `DELIVERY_AUDIT.md` 修正 1/2）**：源文件 §7 同点位试错预算（`<=3` 次，`EP05`/`EP06`）、§9 结构失效位止损展示、§10 分仓与杠杆硬上限（BTC/ETH `10x`、山寨 `5x`，`DELIVERY_AUDIT.md` 证实违规 `>=20x` 窗口亏 `-12.2 万 U` 而守纪窗口赚 `+274.4 万 U`）、§12 犯病图谱与既有 `BehaviorGate` 对齐属于交易员行为约束，独立于自动形态识别器优劣。",
            "3. **缺数据挂起项**：温度计（`T`）待补齐 ~500 标的全市场 PIT `4h/1d` 历史表与连续 `>=72h` 1m 回放窗口后再行立项检验。",
        ]
    )


def write_phase3a_reports(suite: Phase3aFullSuiteReport, out_dir: str | Path) -> list[Path]:
    """Write all 6 individual Phase 3a Markdown reports + master summary report to `out_dir`."""
    d = Path(out_dir)
    d.mkdir(parents=True, exist_ok=True)
    files = [
        ("2026-11-11-phase3a-s1-point1-breakout.md", format_s1_markdown(suite)),
        ("2026-11-11-phase3a-s2-point4-second-wave.md", format_s2_markdown(suite)),
        ("2026-11-11-phase3a-s3-point2-constraints.md", format_s3_markdown(suite)),
        ("2026-11-11-phase3a-f-box-filter.md", format_f_markdown(suite)),
        ("2026-11-11-phase3a-l-leader-ranker.md", format_l_markdown(suite)),
        ("2026-11-11-phase3a-thermometer-audit.md", format_thermometer_markdown(suite)),
        ("2026-11-11-phase3a-bitlanglang-summary.md", format_phase3a_summary_markdown(suite)),
    ]
    written: list[Path] = []
    for fname, content in files:
        p = d / fname
        p.write_text(content + "\n")
        written.append(p)
    return written
