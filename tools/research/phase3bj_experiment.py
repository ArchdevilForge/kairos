"""Phase 3b-J — Jev Blind Judgment Re-Validation Harness (Stage 1 Scaffold & Evaluator).

Implements `docs/40-research/experiments/PHASE3BJ_PREREG.md` strictly within
`tools/research/` and `docs/40-research/` (zero changes to `internal/`):
- Step A: Replays/verifies the Phase 2b/3a dataset anchor (`758,013 +/- 2%` 1m bars).
- Step B: Extracts P1 (`S3_POOL`, all candidates entering Phase 3a S3) and P2
  (`S1_SET`, all Phase 3a Point-1 breakout candidates) without inventing new
  definitions, generating strictly ex-ante context snapshots (`<= entry_ts`)
  enforced by `validate_ex_ante_grouping` and `assert_snapshot_ex_ante_clean`.
- Step C/D: Joins Jev judgment cache (`candidate_id -> p`) with Phase 3a's exact
  `PolicyNetR` (`wave_k_be=0.8`) outcomes and Walk-Forward OOS wave clusters,
  evaluating P1 and P2 independently at frozen `tau = 0.60` (plus descriptive
  `tau in {0.55, 0.65}`, P1 head-to-head vs S3 rule retained group, Brier score,
  5-bin reliability calibration, and acceptance rates).
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timezone
import hashlib
import json
import math
from pathlib import Path
from typing import Any, Mapping, Sequence

import duckdb

from clustering import (
    ClusterDiffStat,
    ClusterMeanStat,
    cluster_robust_diff,
    cluster_robust_mean,
    validate_ex_ante_grouping,
)
from friction import FrictionParams
from h1_experiment import (
    SEASON_WINTER,
    SYNC_LAGGING_CATCHUP,
    SYNC_NONE,
    SYNC_RESONANCE,
    AnnotatedTicketSignal,
    SeasonObservation,
    build_causal_season_timeline,
    run_h1_experiment,
)
from h3_experiment import (
    ConfirmedSwingPivot,
    _cluster_boundary_touches,
    extract_causal_swing_pivots,
)
from jev_client import (
    DEFAULT_SHUFFLE_SEED,
    JevClient,
    JevConfig,
    JevRunManifest,
    JudgmentCacheEntry,
    CandidatePriorityMeta,
    TEMPLATE_ID,
    assert_snapshot_ex_ante_clean,
    build_jev_payload,
    compute_template_spec_sha256,
    load_judgment_cache,
)
from p0_experiment import load_bars_and_funding_from_duckdb
from phase3a_experiment import (
    DatasetAnchorAudit,
    S1_DELTA_NETR_BAR,
    _build_5m_bars_from_1m,
    _causal_24h_vol_at,
    _causal_prefix,
    _contiguous_5m_prefix,
    _evaluate_entry_setup_against_baseline,
    _find_major_symbol,
    _latest_4h_launch_ts,
    _load_htf_bars_by_sym,
    _load_pit_and_vol,
    detect_s1_point1_candidates,
    evaluate_s3_point2_constraints,
    score_langlang_leader_states,
    verify_phase3a_dataset_anchor,
)
from pit_universe import normalize_base_coin
from policies import CandidateSignal, MinuteBar

# --- Frozen Pre-Registered Constants (`PHASE3BJ_PREREG.md`) ---

PRIMARY_TAU: float = 0.60
SENSITIVITY_TAUS: tuple[float, ...] = (0.55, 0.60, 0.65)

DELTA_NETR_BAR: float = 0.18
P_VALUE_BAR: float = 0.05
MIN_OOS_CLUSTERS_BAR: int = 15

POOL_P1_S3: str = "S3_POOL"
POOL_P2_S1: str = "S1_SET"

RELIABILITY_BIN_SPECS: tuple[tuple[str, float, float, bool], ...] = (
    ("[0.00, 0.40)", 0.00, 0.40, False),
    ("[0.40, 0.55)", 0.40, 0.55, False),
    ("[0.55, 0.60)", 0.55, 0.60, False),
    ("[0.60, 0.70)", 0.60, 0.70, False),
    ("[0.70, 1.00]", 0.70, 1.00, True),
)

STATIC_SECTOR_BUCKET_BY_COIN: dict[str, str] = {
    # MAJORS_PAYMENT
    "BTC": "MAJORS_PAYMENT",
    "ETH": "MAJORS_PAYMENT",
    "XRP": "MAJORS_PAYMENT",
    "LTC": "MAJORS_PAYMENT",
    "BCH": "MAJORS_PAYMENT",
    # ALT_SMART_L1
    "SOL": "ALT_SMART_L1",
    "SUI": "ALT_SMART_L1",
    "AVAX": "ALT_SMART_L1",
    "ADA": "ALT_SMART_L1",
    "DOT": "ALT_SMART_L1",
    "NEAR": "ALT_SMART_L1",
    # DEFI_INFRA
    "LINK": "DEFI_INFRA",
    "UNI": "DEFI_INFRA",
    "AAVE": "DEFI_INFRA",
    "LIT": "DEFI_INFRA",
    # MEME
    "DOGE": "MEME",
    # EMERGING_2025_2026
    "PIEVERSE": "EMERGING_2025_2026",
    "ZAMA": "EMERGING_2025_2026",
    "EDGE": "EMERGING_2025_2026",
    "UP": "EMERGING_2025_2026",
    "GRAM": "EMERGING_2025_2026",
    "CAP": "EMERGING_2025_2026",
    "VVV": "EMERGING_2025_2026",
    "AKE": "EMERGING_2025_2026",
    "PONS": "EMERGING_2025_2026",
    "DGAI": "EMERGING_2025_2026",
    "CT": "EMERGING_2025_2026",
}

KNOWN_TRAINING_OVERLAP_CAVEAT: str = (
    "已知局限先行声明（Pre-Registered Training-Window Overlap Caveat）："
    "Jev 的底层训练数据覆盖范围与知识截止时间不可审计；鉴于本评测窗口为 "
    "`2025-11-09` 至 `2026-11-09` 的公开永续合约行情，若 Jev 的训练窗与评测窗重叠，"
    "任何正向结果须强制降格为「判断一致性证据（Judgment Consistency Evidence）」，"
    "不得声称为「独立样本外证据（Independent Out-of-Sample Evidence）」。"
)


@dataclass(frozen=True)
class GroundTruthTicketOutcome:
    """Evaluation-only outcome & split metadata (NEVER stored in ex-ante snapshot JSONL)."""

    candidate_id: str
    source_pool: str  # "S3_POOL" | "S1_SET"
    signal_id: str
    symbol: str
    side: str
    entry_ts: int
    wave_cluster_id: str
    is_oos: bool
    net_r_wave: float
    net_r_bracket: float
    s3_rule_retained: bool  # Meaningful only for P1 ("S3_POOL")


@dataclass(frozen=True)
class ExtractedPopulationsBundle:
    """Step A + Step B output: anchor audit, clean ex-ante snapshots, and evaluation outcomes."""

    anchor: DatasetAnchorAudit
    snapshots: tuple[dict[str, Any], ...]
    outcomes_by_id: dict[str, GroundTruthTicketOutcome]
    priority_by_id: dict[str, CandidatePriorityMeta]
    baseline_long_oos_outcomes: tuple[tuple[float, str], ...]  # (net_r_wave, wave_cluster_id)


@dataclass(frozen=True)
class ReliabilityBinRow:
    """Single probability calibration bin summary."""

    label: str
    n_obs: int
    mean_pred_p: float
    empirical_pos_rate: float
    mean_net_r: float


@dataclass(frozen=True)
class CalibrationSummary:
    """Brier score and 5-bin reliability calibration summary."""

    n_obs: int
    base_pos_rate: float
    brier_score: float
    brier_climatology: float
    brier_skill_score: float
    ece: float
    bins: tuple[ReliabilityBinRow, ...]


@dataclass(frozen=True)
class ThresholdSensitivityRow:
    """Evaluation summary at threshold `tau` (`0.60` primary; `0.55`/`0.65` descriptive)."""

    tau: float
    is_primary: bool
    n_pool_oos: int
    n_accepted_oos: int
    accept_rate_oos: float
    g_accepted_oos: int
    accepted_oos_stat: ClusterMeanStat
    diff_vs_pool_oos: ClusterDiffStat
    n_pool_is: int
    n_accepted_is: int
    accept_rate_is: float


@dataclass(frozen=True)
class RuleOverlapBreakdown:
    """2x2 overlap table on P1 OOS between Jev (`p >= 0.60`) and S3 Fixed Rule (`RETAINED`)."""

    n_both: int
    mean_both: float
    n_jev_only: int
    mean_jev_only: float
    n_rule_only: int
    mean_rule_only: float
    n_neither: int
    mean_neither: float


@dataclass(frozen=True)
class PopulationEvaluationReport:
    """Independent evaluation report for a single population (`P1` or `P2`)."""

    pop_code: str  # "P1" | "P2"
    source_pool: str  # "S3_POOL" | "S1_SET"
    title: str
    mode: str  # "mock" | "live"
    n_total_is: int
    g_total_is: int
    n_total_oos: int
    g_total_oos: int
    n_judged_is: int
    n_judged_oos: int
    pool_is_stat: ClusterMeanStat
    pool_oos_stat: ClusterMeanStat
    accepted_is_stat: ClusterMeanStat
    accepted_oos_stat: ClusterMeanStat
    rejected_oos_stat: ClusterMeanStat
    accept_rate_is: float
    accept_rate_oos: float
    diff_accepted_vs_pool_is: ClusterDiffStat
    diff_accepted_vs_pool_oos: ClusterDiffStat
    sensitivity_rows: tuple[ThresholdSensitivityRow, ...]
    calibration_oos: CalibrationSummary
    calibration_is: CalibrationSummary
    # P1-only head-to-head vs S3 rule retained group (None for P2)
    rule_retained_oos_stat: ClusterMeanStat | None
    diff_jev_vs_rule_oos: ClusterDiffStat | None
    rule_overlap_oos: RuleOverlapBreakdown | None
    # P2-only reference vs Phase 3a Baseline Long OOS stream (None for P1)
    diff_jev_vs_baseline_long_oos: ClusterDiffStat | None
    verdict: str
    verdict_detail: str


@dataclass(frozen=True)
class Phase3bjSuiteEvaluation:
    """Full Stage 1 / Stage 2 evaluation bundle across independent P1 and P2."""

    anchor: DatasetAnchorAudit
    mode: str  # "mock" | "live"
    manifest: JevRunManifest | None
    p1: PopulationEvaluationReport
    p2: PopulationEvaluationReport


# ---------------------------------------------------------------------------
# Opaque Candidate ID & Static Profiles
# ---------------------------------------------------------------------------


def compute_opaque_candidate_id(
    source_pool: str,
    symbol: str,
    side: str,
    entry_ts: int,
    entry_price: float,
    stop_price: float,
) -> str:
    """Compute opaque SHA-256 hex `candidate_id` free of sequence numbers or pool names."""
    raw = (
        f"{source_pool}|{symbol}|{side}|{int(entry_ts)}|"
        f"{float(entry_price):.8f}|{float(stop_price):.8f}"
    )
    digest = hashlib.sha256(raw.encode("utf-8")).hexdigest()[:16]
    return f"cand_{digest}"


def classify_sector_bucket(symbol: str) -> str:
    coin = normalize_base_coin(symbol)
    return STATIC_SECTOR_BUCKET_BY_COIN.get(coin, "EMERGING_2025_2026")


def classify_age_tier(age_days: float) -> str:
    if age_days < 7.0:
        return "NEW_LT_7D"
    if age_days <= 90.0:
        return "CIXIN_7D_90D"
    if age_days <= 365.0:
        return "ESTABLISHED_90D_365D"
    return "MATURE_GT_365D"


def classify_liquidity_tier(quote_vol_24h_usd: float) -> str:
    if quote_vol_24h_usd >= 500_000_000.0:
        return "TIER_1_ULTRA_GT_500M"
    if quote_vol_24h_usd >= 100_000_000.0:
        return "TIER_2_HIGH_100M_500M"
    if quote_vol_24h_usd >= 30_000_000.0:
        return "TIER_3_CORE_30M_100M"
    if quote_vol_24h_usd >= 10_000_000.0:
        return "TIER_4_THIN_10M_30M"
    return "TIER_5_ILLIQUID_LT_10M"


# ---------------------------------------------------------------------------
# Faithful Causal CycleMap 3-Layer Classifier (Port of `internal/cycle`)
# ---------------------------------------------------------------------------


def _sma(vals: Sequence[float], period: int) -> float:
    if not vals or period <= 0:
        return 0.0
    sub = vals[-period:]
    return sum(sub) / float(len(sub))


def _slope(vals: Sequence[float]) -> float:
    n = len(vals)
    if n < 2:
        return 0.0
    x_mean = 0.5 * (n - 1)
    y_mean = sum(vals) / float(n)
    num = sum((i - x_mean) * (vals[i] - y_mean) for i in range(n))
    den = sum((i - x_mean) ** 2 for i in range(n))
    return num / den if den > 0 else 0.0


def _std_pop(vals: Sequence[float]) -> float:
    n = len(vals)
    if n == 0:
        return 0.0
    mu = sum(vals) / float(n)
    return math.sqrt(sum((x - mu) ** 2 for x in vals) / float(n))


def _classify_cycle_node_raw(
    bars: Sequence[tuple[int, float, float, float, float, float]],
) -> dict[str, Any]:
    """Port of `internal/cycle/direction.go` + `internal/cycle/phase.go` on closed bars."""
    closes = [x[4] for x in bars]
    highs = [x[2] for x in bars]
    lows = [x[3] for x in bars]

    if len(closes) < 40:
        return {
            "direction": "neutral",
            "phase": "winter",
            "confidence": 0.25,
        }

    price = closes[-1]
    mid = _sma(closes, 20)
    fast = _sma(closes, 10)
    fast_series = [
        _sma(closes[: len(closes) - i + 1], 10)
        for i in range(5, 0, -1)
        if (len(closes) - i + 1) >= 10
    ]
    fast_slope = _slope(fast_series)

    up, down = 0, 0
    if mid > 0:
        rel = (price - mid) / mid * 100.0
        if rel > 0.15:
            up += 1
        elif rel < -0.15:
            down += 1

    if fast > mid and fast_slope > 0:
        up += 1
    elif fast < mid and fast_slope < 0:
        down += 1

    h_recent = max(highs[-10:])
    h_prior = max(highs[-20:-10])
    l_recent = min(lows[-10:])
    l_prior = min(lows[-20:-10])
    if h_recent > h_prior and l_recent > l_prior:
        up += 1
    elif h_recent < h_prior and l_recent < l_prior:
        down += 1

    if len(closes) >= 41:
        win40_c = closes[-41:-1]
        if price > max(win40_c):
            up += 1
        elif price < min(win40_c):
            down += 1

    if len(closes) >= 15 and mid > 0:
        if price > mid and min(lows[-8:]) > min(lows[-15:-8]):
            up += 1
        if price < mid and max(highs[-8:]) < max(highs[-15:-8]):
            down += 1

    raw_conf = min(1.0, max(0.0, abs(up - down) / 5.0))
    if up >= down + 2:
        direction = "up"
        dir_conf = max(raw_conf, 0.4)
    elif down >= up + 2:
        direction = "down"
        dir_conf = max(raw_conf, 0.4)
    else:
        direction = "neutral"
        dir_conf = max(0.3, 1.0 - raw_conf)

    if direction == "neutral":
        return {
            "direction": "neutral",
            "phase": "winter",
            "confidence": round(0.5 * dir_conf + 0.3, 4),
        }

    p_base = closes[-21] if len(closes) >= 21 and closes[-21] > 0 else closes[0]
    chg = (closes[-1] - p_base) / p_base * 100.0 if p_base > 0 else 0.0
    rets = [
        (closes[i] - closes[i - 1]) / closes[i - 1]
        for i in range(len(closes) - 20, len(closes))
        if i > 0 and closes[i - 1] != 0
    ]
    vol = max(_std_pop(rets) * 100.0, 1e-9)
    strength = abs(chg / vol)

    p_10 = closes[-11] if len(closes) >= 11 and closes[-11] > 0 else closes[0]
    recent_chg = (closes[-1] - p_10) / p_10 * 100.0 if p_10 > 0 else 0.0
    prior_slice = closes[-20:-10]
    prior_chg = (
        (prior_slice[-1] - prior_slice[0]) / prior_slice[0] * 100.0
        if len(prior_slice) > 1 and prior_slice[0] != 0
        else 0.0
    )
    sign = 1.0 if direction == "up" else -1.0
    mom_recent = sign * recent_chg
    mom_prior = sign * prior_chg
    mom_delta = mom_recent - mom_prior

    crosses = 0
    if mid > 0 and len(closes) >= 15:
        for i in range(len(closes) - 14, len(closes)):
            a = closes[i - 1] - mid
            b = closes[i] - mid
            if a != 0 and b != 0 and (a > 0) != (b > 0):
                crosses += 1

    aligned = sign * chg
    if crosses >= 6 and strength < 1.2:
        phase = "winter"
        p_conf = 0.7
    elif aligned > 0 and strength >= 1.5 and mom_delta >= 0:
        phase = "summer"
        p_conf = min(1.0, 0.5 + strength / 6.0)
    elif aligned > 0 and strength >= 0.6 and mom_delta > 0 and mom_prior < mom_recent:
        phase = "spring"
        p_conf = min(1.0, 0.45 + strength / 5.0)
    elif aligned > 0 and strength >= 0.5 and mom_delta < 0:
        phase = "autumn"
        p_conf = min(1.0, 0.4 + strength / 6.0)
    elif aligned > 0 and strength >= 0.8:
        phase = "summer"
        p_conf = min(1.0, 0.4 + strength / 6.0)
    elif aligned > 0:
        phase = "spring"
        p_conf = min(1.0, 0.35 + strength / 5.0)
    else:
        phase = "winter"
        p_conf = 0.5

    return {
        "direction": direction,
        "phase": phase,
        "confidence": round(min(1.0, max(0.0, 0.5 * dir_conf + 0.5 * p_conf)), 4),
    }


def _build_contiguous_15m_from_5m(
    contig_5m: Sequence[tuple[int, float, float, float, float, float]],
) -> list[tuple[int, float, float, float, float, float]]:
    """Aggregate contiguous closed 5m bars (`close_ts <= entry_ts`) into closed 15m bars."""
    buckets: dict[int, list[tuple[int, float, float, float, float, float]]] = {}
    for row in contig_5m:
        open_5m = row[0] - 300
        open_15m = (open_5m // 900) * 900
        buckets.setdefault(open_15m, []).append(row)
    out: list[tuple[int, float, float, float, float, float]] = []
    for b_open in sorted(buckets):
        grp = buckets[b_open]
        if len(grp) < 3:
            continue
        grp_sorted = sorted(grp, key=lambda x: x[0])
        out.append(
            (
                b_open + 900,
                grp_sorted[0][1],
                max(x[2] for x in grp_sorted),
                min(x[3] for x in grp_sorted),
                grp_sorted[-1][4],
                sum(x[5] for x in grp_sorted),
            )
        )
    return out


def classify_cycle_map_ex_ante(
    c_1d: Sequence[tuple[int, float, float, float, float, float]],
    c_4h: Sequence[tuple[int, float, float, float, float, float]],
    contig_5m: Sequence[tuple[int, float, float, float, float, float]],
) -> dict[str, Any]:
    """Build 3-layer `CycleMap` (`context_1d`, `context_4h`, `setup_15m`, `trigger_5m`).

    Mirrors `internal/cycle/hierarchy.go:classifyHierarchy` strictly on `<= entry_ts` bars.
    """
    contig_15m = _build_contiguous_15m_from_5m(contig_5m)
    n_1d = _classify_cycle_node_raw(c_1d)
    n_4h = _classify_cycle_node_raw(c_4h)
    n_15m = _classify_cycle_node_raw(contig_15m)
    n_5m = _classify_cycle_node_raw(contig_5m)

    # Primary direction from context (`1d` preferred over `4h`, matching `contextPreference`)
    if n_1d["direction"] != "neutral":
        primary = n_1d["direction"]
    elif n_4h["direction"] != "neutral":
        primary = n_4h["direction"]
    else:
        primary = "neutral"

    context_nodes = [n_1d, n_4h]
    winterish = sum(
        1 for n in context_nodes if n["direction"] == "neutral" or n["phase"] == "winter"
    )
    context_no_trade = (winterish == len(context_nodes)) or (primary == "neutral")

    # Dominant lower-TF direction (`setup_15m` + `trigger_5m`)
    lower_nodes = [n_15m, n_5m]
    up_l = sum(1 for n in lower_nodes if n["direction"] == "up")
    dn_l = sum(1 for n in lower_nodes if n["direction"] == "down")
    ne_l = sum(1 for n in lower_nodes if n["direction"] == "neutral")
    if up_l > dn_l and up_l >= ne_l:
        lower_dir = "up"
    elif dn_l > up_l and dn_l >= ne_l:
        lower_dir = "down"
    else:
        lower_dir = "neutral"

    lower_mixed = (
        n_15m["direction"] != "neutral"
        and n_5m["direction"] != "neutral"
        and n_15m["direction"] != n_5m["direction"]
    )

    if context_no_trade:
        alignment = "no_trade"
        trade_class = "no_trade"
    elif lower_mixed or lower_dir == "neutral":
        alignment = "mixed"
        trade_class = "no_trade"
    elif lower_dir == primary:
        alignment = "full_alignment"
        trade_class = "aligned_long" if primary == "up" else "aligned_short"
    else:
        alignment = "counter_trend"
        trade_class = "counter_trend_long" if lower_dir == "up" else "counter_trend_short"

    return {
        "primary_direction": primary,
        "alignment": alignment,
        "trade_class": trade_class,
        "context_1d": n_1d,
        "context_4h": n_4h,
        "setup_15m": n_15m,
        "trigger_5m": n_5m,
    }


# ---------------------------------------------------------------------------
# Causal Cross-Sectional BTC Impulse & Snapshot Feature Extraction
# ---------------------------------------------------------------------------


def _detect_causal_btc_up_impulses(
    bars_5m: Mapping[str, Sequence[tuple[int, float, float, float, float, float]]],
    close5m_idx: Mapping[str, Mapping[int, int]],
    pit_meta: Mapping[str, tuple[int, int | None]],
    vol_by_sym: Mapping[str, Sequence[tuple[int, float]]],
    btc_sym: str,
    *,
    min_quote_vol_24h: float = 30_000_000.0,
    min_valid_symbols: int = 6,
) -> list[int]:
    """Detect causal BTC upward `market_impulse` timestamps strictly from closed `<= t` bars.

    Identical thresholds to `h1_experiment.py:486-508` (`btc_ret_5m >= 0.22%` or
    `btc_ret_15m >= 0.40%` with `up_breadth >= 0.55` and 30m cooldown), without
    referencing any post-`t` slice length so future bar mutations cannot alter past impulses.
    """
    all_closes: set[int] = set()
    for b5 in bars_5m.values():
        for row in b5:
            all_closes.add(row[0])

    impulses: list[int] = []
    for eval_ts in sorted(all_closes):
        rets: list[tuple[str, float]] = []
        btc_r5 = 0.0
        btc_r15 = 0.0
        for sym, b5 in bars_5m.items():
            meta = pit_meta.get(sym)
            if meta is not None:
                if meta[0] > eval_ts:
                    continue
                if meta[1] is not None and meta[1] <= eval_ts:
                    continue
            if (
                _causal_24h_vol_at(sym, eval_ts, vol_by_sym, bars_5m, close5m_idx)
                < min_quote_vol_24h
            ):
                continue
            c_idx = close5m_idx.get(sym, {}).get(eval_ts)
            if c_idx is None or c_idx < 6:
                continue
            # Ensure contiguous 5m bar step (no 9-day gap bridge)
            if b5[c_idx][0] - b5[c_idx - 1][0] > 600:
                continue
            p_now = b5[c_idx][4]
            p_prev = b5[c_idx - 1][4]
            if p_now <= 0 or p_prev <= 0:
                continue
            r5 = (p_now - p_prev) / p_prev * 100.0
            rets.append((sym, r5))
            if sym == btc_sym:
                btc_r5 = r5
                if c_idx >= 3 and (b5[c_idx][0] - b5[c_idx - 3][0]) <= 1200 and b5[c_idx - 3][4] > 0:
                    btc_r15 = (p_now - b5[c_idx - 3][4]) / b5[c_idx - 3][4] * 100.0

        if len(rets) < min_valid_symbols:
            continue
        up_b = sum(1 for _, r in rets if r > 0.06) / float(len(rets))
        if (btc_r5 >= 0.22 or btc_r15 >= 0.40) and up_b >= 0.55:
            if not impulses or eval_ts - impulses[-1] >= 1800:
                impulses.append(eval_ts)
    return impulses


def _classify_btc_sync_at(entry_ts: int, btc_up_impulses: Sequence[int]) -> str:
    prior = [t for t in btc_up_impulses if t <= entry_ts]
    if not prior:
        return SYNC_NONE
    recent_lag = float(entry_ts - prior[-1])
    if 0.0 <= recent_lag <= 900.0:
        return SYNC_RESONANCE
    for t_imp in reversed(prior):
        dt = float(entry_ts - t_imp)
        if 7200.0 <= dt <= 43200.0:
            return SYNC_LAGGING_CATCHUP
    return SYNC_NONE


def build_ex_ante_snapshot_for_signal(
    signal: CandidateSignal,
    source_pool: str,
    *,
    bars_5m_by_sym: Mapping[str, Sequence[tuple[int, float, float, float, float, float]]],
    close5m_idx_by_sym: Mapping[str, Mapping[int, int]],
    bars_4h_by_sym: Mapping[str, Sequence[tuple[int, float, float, float, float, float]]],
    bars_1d_by_sym: Mapping[str, Sequence[tuple[int, float, float, float, float, float]]],
    pit_meta: Mapping[str, tuple[int, int | None]],
    vol_by_sym: Mapping[str, Sequence[tuple[int, float]]],
    season_timeline: Mapping[int, SeasonObservation],
    btc_up_impulses: Sequence[int],
    btc_sym: str,
    eth_sym: str,
) -> dict[str, Any]:
    """Construct one strictly ex-ante context snapshot (`feature_ts <= entry_ts`) for `signal`.

    Uses identical feature formulas for both `S3_POOL` (P1) and `S1_SET` (P2) so
    that after `source_pool` and `candidate_id` are stripped, the resulting Jev
    payload is 100% blind to candidate construction origin and contains zero rule
    pass/fail booleans or post-entry fields.
    """
    sym = signal.symbol
    side = "LONG" if signal.direction == "UP" else "SHORT"
    entry_ts = int(signal.entry_ts)
    entry_px = float(signal.entry_price)
    stop_px = float(signal.stop_price)

    cand_id = compute_opaque_candidate_id(
        source_pool=source_pool,
        symbol=sym,
        side=side,
        entry_ts=entry_ts,
        entry_price=entry_px,
        stop_price=stop_px,
    )

    # Strictly causal bar prefixes (`close_ts <= entry_ts` ONLY)
    b5_all = [row for row in bars_5m_by_sym.get(sym, ()) if row[0] <= entry_ts]
    contig_5m = _contiguous_5m_prefix(b5_all, len(b5_all) - 1) if b5_all else []
    c_4h = _causal_prefix(bars_4h_by_sym.get(sym, ()), entry_ts)
    c_1d = _causal_prefix(bars_1d_by_sym.get(sym, ()), entry_ts)

    c_btc_4h = _causal_prefix(bars_4h_by_sym.get(btc_sym, ()), entry_ts)
    c_btc_1d = _causal_prefix(bars_1d_by_sym.get(btc_sym, ()), entry_ts)
    c_eth_4h = _causal_prefix(bars_4h_by_sym.get(eth_sym, ()), entry_ts)
    btc_b5_prefix = [row for row in bars_5m_by_sym.get(btc_sym, ()) if row[0] <= entry_ts]
    btc_contig_5m = (
        _contiguous_5m_prefix(btc_b5_prefix, len(btc_b5_prefix) - 1)
        if btc_b5_prefix
        else []
    )

    # 1. Trade geometry & causal overhead room in R (`<= entry_ts`)
    r_dist = abs(entry_px - stop_px)
    stop_dist_pct = (r_dist / entry_px * 100.0) if entry_px > 0 else 0.0

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
    if signal.direction == "UP":
        rel_pivs = [
            p
            for p in pivs_res
            if p.kind == "HIGH" and p.confirm_ts <= entry_ts and p.price > entry_px * 1.004
        ]
        if r_dist <= 0:
            room_r = 0.0
        elif not rel_pivs:
            room_r = 10.0
        else:
            room_r = min(20.0, (min(p.price for p in rel_pivs) - entry_px) / r_dist)
    else:
        rel_pivs = [
            p
            for p in pivs_res
            if p.kind == "LOW" and p.confirm_ts <= entry_ts and p.price < entry_px * 0.996
        ]
        if r_dist <= 0:
            room_r = 0.0
        elif not rel_pivs:
            room_r = 10.0
        else:
            room_r = min(20.0, (entry_px - max(p.price for p in rel_pivs)) / r_dist)
    room_feat_ts = max((p.confirm_ts for p in rel_pivs), default=(c_4h[-1][0] if c_4h else entry_ts))

    # 2. Symbol profile (PIT listing age + causal 24h liquidity tier)
    list_ts = pit_meta.get(sym, (1_700_000_000, None))[0]
    age_days = max(0.0, (entry_ts - list_ts) / 86400.0)
    qv_24h = _causal_24h_vol_at(
        sym, entry_ts, vol_by_sym, bars_5m_by_sym, close5m_idx_by_sym
    )

    # 3. CycleMap 3-layer state (`context_1d`, `context_4h`, `setup_15m`, `trigger_5m`)
    cycle_map_obj = classify_cycle_map_ex_ante(c_1d, c_4h, contig_5m)
    cycle_feat_ts = max(
        c_1d[-1][0] if c_1d else 0,
        c_4h[-1][0] if c_4h else 0,
        contig_5m[-1][0] if contig_5m else entry_ts,
    )

    # 4. Box state (5d `4h` box geometry, 48h `ER_4h`, 6d platform probes, ATR convergence)
    box_5d_width_pct = 0.0
    pos_in_5d_box = 0.5
    er_4h_48h = 0.5
    if len(c_4h) >= 4:
        box_win = c_4h[-30:]
        h_box = max(x[2] for x in box_win)
        l_box = min(x[3] for x in box_win)
        box_5d_width_pct = ((h_box - l_box) / l_box * 100.0) if l_box > 0 else 0.0
        pos_in_5d_box = (entry_px - l_box) / max(h_box - l_box, 1e-9)
        last12 = box_win[-12:]
        path12 = sum(x[2] - x[3] for x in last12)
        er_4h_48h = abs(last12[-1][4] - last12[0][1]) / path12 if path12 > 0 else 0.5

    htf_platform_width_pct = 0.0
    up_probes = 0
    lo_probes = 0
    box_probe_ts = c_4h[-1][0] if c_4h else entry_ts
    if len(c_4h) >= 16:
        h4_win = c_4h[-36:]
        box_slice = h4_win[:-2]
        if len(box_slice) >= 14:
            pivs_4h_box = extract_causal_swing_pivots(
                box_slice, confirm_lag_bars=1, up_to_idx=len(box_slice) - 1
            )
            b_hi, up_p, up_cts = _cluster_boundary_touches(pivs_4h_box, "HIGH", tol_frac=0.015)
            b_lo, lo_p, lo_cts = _cluster_boundary_touches(pivs_4h_box, "LOW", tol_frac=0.015)
            up_probes = int(up_p)
            lo_probes = int(lo_p)
            if b_lo > 0 and b_hi > b_lo:
                htf_platform_width_pct = (b_hi - b_lo) / b_lo * 100.0
            box_probe_ts = max(up_cts, lo_cts, c_4h[-1][0])

    atr_4h_ratio = 1.0
    if len(c_4h) >= 6:
        r_4h_tr = [x[2] - x[3] for x in c_4h[-3:]]
        b_4h_tr = [x[2] - x[3] for x in c_4h[-15:-3]]
        if b_4h_tr:
            atr_4h_ratio = (sum(r_4h_tr) / len(r_4h_tr)) / max(
                sum(b_4h_tr) / len(b_4h_tr), 1e-9
            )

    atr_5m_ratio = 1.0
    if len(contig_5m) >= 12:
        r_5m_tr = [x[2] - x[3] for x in contig_5m[-6:]]
        b_5m_tr = [x[2] - x[3] for x in contig_5m[-30:-6]]
        if b_5m_tr:
            atr_5m_ratio = (sum(r_5m_tr) / len(r_5m_tr)) / max(
                sum(b_5m_tr) / len(b_5m_tr), 1e-9
            )

    # 5. Divergence state (5m pullback depth/rebound, prior completed minor divs, 14d 4h drawdown, 5m vol surge)
    pb_depth_5m_pct = 0.0
    rebound_pos_5m = 1.0
    peak_ts_local = entry_ts - 600
    if len(contig_5m) >= 10:
        win_5m = contig_5m[-30:]
        n_w = len(win_5m)
        lo_i = max(1, n_w - 30)
        hi_i = max(lo_i, n_w - 4)
        if hi_i > lo_i + 1:
            peak_idx = lo_i
            for i in range(lo_i, hi_i + 1):
                if win_5m[i][2] >= win_5m[peak_idx][2]:
                    peak_idx = i
            pb_low_idx = peak_idx + 1
            for i in range(peak_idx + 1, n_w - 1):
                if win_5m[i][3] <= win_5m[pb_low_idx][3]:
                    pb_low_idx = i
            if pb_low_idx < n_w:
                peak_px = win_5m[peak_idx][2]
                pb_low_px = win_5m[pb_low_idx][3]
                peak_ts_local = win_5m[peak_idx][0]
                if peak_px > pb_low_px > 0:
                    pb_depth_5m_pct = (peak_px - pb_low_px) / peak_px * 100.0
                    rebound_pos_5m = (entry_px - pb_low_px) / (peak_px - pb_low_px)

    prior_minor_divs = 0
    if contig_5m and c_4h:
        pivs_4h_leg = extract_causal_swing_pivots(
            c_4h[-24:], confirm_lag_bars=1, up_to_idx=len(c_4h[-24:]) - 1
        )
        lo_4h = [p for p in pivs_4h_leg if p.kind == "LOW"]
        if lo_4h:
            leg_start_ts = lo_4h[-1].pivot_ts - 14400
        else:
            min_4h = min(c_4h[-12:], key=lambda x: x[3])
            leg_start_ts = min_4h[0] - 14400
        leg_b5 = [x for x in contig_5m if leg_start_ts <= x[0] < peak_ts_local]
        if len(leg_b5) >= 12:
            pivs_5m_leg = extract_causal_swing_pivots(
                leg_b5, confirm_lag_bars=2, up_to_idx=len(leg_b5) - 1
            )
            hi_5m = [p for p in pivs_5m_leg if p.kind == "HIGH"]
            counted_hp: list[ConfirmedSwingPivot] = []
            for idx_hp, hp in enumerate(hi_5m):
                next_end = (
                    hi_5m[idx_hp + 1].pivot_idx + 1
                    if idx_hp + 1 < len(hi_5m)
                    else len(leg_b5)
                )
                between = leg_b5[hp.pivot_idx + 1 : next_end]
                if len(between) < 3:
                    continue
                dip_rel = min(range(len(between)), key=lambda j: between[j][3])
                if (dip_rel + 1) < 3:
                    continue
                dip_lo = between[dip_rel][3]
                rec_bars = leg_b5[hp.pivot_idx + 1 + dip_rel + 1 :]
                recovered = any(x[2] >= hp.price * 1.003 for x in rec_bars)
                if hp.price > 0 and (hp.price - dip_lo) / hp.price >= 0.012 and recovered:
                    if all(abs(hp.pivot_idx - prev.pivot_idx) >= 12 for prev in counted_hp):
                        counted_hp.append(hp)
            prior_minor_divs = len(counted_hp)

    dd_from_14d_4h_high_pct = 0.0
    bars_4h_since_14d_high = 0
    if c_4h:
        w14d = c_4h[-84:]
        idx_hi14 = max(range(len(w14d)), key=lambda k: w14d[k][2])
        hi14_px = w14d[idx_hi14][2]
        if hi14_px > 0:
            dd_from_14d_4h_high_pct = max(0.0, (hi14_px - entry_px) / hi14_px * 100.0)
        bars_4h_since_14d_high = len(w14d) - 1 - idx_hi14

    vol_5m_surge_ratio = 1.0
    if len(contig_5m) >= 3:
        prior_v = [x[5] for x in contig_5m[-13:-1]]
        mean_v = sum(prior_v) / len(prior_v) if prior_v else 0.0
        if mean_v > 0:
            vol_5m_surge_ratio = contig_5m[-1][5] / mean_v

    # 6. BTC state (`<= entry_ts`)
    btc_1d_vs_sma50_pct = 0.0
    if c_btc_1d:
        sma50 = sum(x[4] for x in c_btc_1d[-50:]) / len(c_btc_1d[-50:])
        if sma50 > 0:
            btc_1d_vs_sma50_pct = (c_btc_1d[-1][4] / sma50 - 1.0) * 100.0

    btc_4h_vs_sma18_pct = 0.0
    if c_btc_4h:
        sma18 = sum(x[4] for x in c_btc_4h[-18:]) / len(c_btc_4h[-18:])
        if sma18 > 0:
            btc_4h_vs_sma18_pct = (c_btc_4h[-1][4] / sma18 - 1.0) * 100.0

    btc_ret_1h_pct = 0.0
    if len(btc_contig_5m) >= 13 and btc_contig_5m[-13][4] > 0:
        btc_ret_1h_pct = (
            (btc_contig_5m[-1][4] - btc_contig_5m[-13][4]) / btc_contig_5m[-13][4] * 100.0
        )

    btc_ret_48h_pct = 0.0
    if len(c_btc_4h) >= 12 and c_btc_4h[-12][1] > 0:
        btc_ret_48h_pct = (
            (c_btc_4h[-1][4] - c_btc_4h[-12][1]) / c_btc_4h[-12][1] * 100.0
        )

    btc_dist_to_4h_high_pct = 0.0
    if btc_contig_5m:
        w48_btc = btc_contig_5m[-48:]
        hi_btc_4h = max(x[2] for x in w48_btc)
        if hi_btc_4h > 0:
            btc_dist_to_4h_high_pct = max(
                0.0, (hi_btc_4h - btc_contig_5m[-1][4]) / hi_btc_4h * 100.0
            )

    btc_sync_mode = _classify_btc_sync_at(entry_ts, btc_up_impulses)

    t_btc_l = _latest_4h_launch_ts(c_btc_4h)
    t_eth_l = _latest_4h_launch_ts(c_eth_4h)
    launch_cands = [x for x in (t_btc_l, t_eth_l) if x is not None and x <= entry_ts]
    t_major_launch = max(launch_cands) if launch_cands else None
    hours_since_launch: float | None = (
        round(max(0.0, (entry_ts - t_major_launch) / 3600.0), 3)
        if t_major_launch is not None
        else None
    )

    r_sym_1h = 0.0
    if len(contig_5m) >= 13 and contig_5m[-13][4] > 0:
        r_sym_1h = (
            (contig_5m[-1][4] - contig_5m[-13][4]) / contig_5m[-13][4] * 100.0
        )
    _, _, s3_resilience, _ = score_langlang_leader_states(
        sym=sym,
        wake_ts=entry_ts,
        age_days=age_days,
        sym_1d=c_1d,
        sym_4h=c_4h,
        btc_4h=c_btc_4h,
        universe_1d_body_median=0.50,
        universe_launch_median_ret=None,
        t_btc_launch=t_major_launch,
        btc_ret_48h_pct=btc_ret_48h_pct,
        r_btc_1h_pct=btc_ret_1h_pct,
        r_sym_1h_pct=r_sym_1h,
    )
    btc_feat_ts = max(
        c_btc_1d[-1][0] if c_btc_1d else 0,
        c_btc_4h[-1][0] if c_btc_4h else 0,
        btc_contig_5m[-1][0] if btc_contig_5m else entry_ts,
    )

    # 7. Causal Season state (Phase 2c classifier `<= entry_ts`)
    season_obs = season_timeline.get(entry_ts)
    if season_obs is None:
        prior_s_ts = [ts_k for ts_k in season_timeline if ts_k <= entry_ts]
        if prior_s_ts:
            season_obs = season_timeline[max(prior_s_ts)]
    if season_obs is not None:
        season_state = {
            "season_regime": season_obs.season_regime,
            "breadth_fast": round(season_obs.breadth_fast, 4),
            "breadth_slow": round(season_obs.breadth_slow, 4),
            "breadth_momentum": round(season_obs.breadth_momentum, 4),
            "volume_ratio": round(season_obs.volume_ratio, 4),
            "crowding_pct": round(season_obs.crowding_pct, 4),
        }
        season_feat_ts = int(season_obs.ts)
    else:
        season_state = {
            "season_regime": SEASON_WINTER,
            "breadth_fast": 0.0,
            "breadth_slow": 0.0,
            "breadth_momentum": 0.0,
            "volume_ratio": 1.0,
            "crowding_pct": 0.5,
        }
        season_feat_ts = entry_ts

    # 8. Same-sector & cross-section 5m resonance counts (`<= entry_ts`)
    sec_bucket = classify_sector_bucket(sym)
    dir_sign = 1.0 if signal.direction == "UP" else -1.0
    same_sec_peers = 0
    same_sec_res = 0
    cross_sec_res = 0
    resonance_feat_ts = entry_ts

    for peer_sym, peer_b5 in bars_5m_by_sym.items():
        p_meta = pit_meta.get(peer_sym)
        if p_meta is not None:
            if p_meta[0] > entry_ts:
                continue
            if p_meta[1] is not None and p_meta[1] <= entry_ts:
                continue
        p_idx = close5m_idx_by_sym.get(peer_sym, {}).get(entry_ts)
        if p_idx is None or p_idx < 1:
            continue
        if peer_b5[p_idx][0] - peer_b5[p_idx - 1][0] > 600:
            continue
        p_now = peer_b5[p_idx][4]
        p_prev = peer_b5[p_idx - 1][4]
        if p_now <= 0 or p_prev <= 0:
            continue
        r5_peer = dir_sign * ((p_now - p_prev) / p_prev * 100.0)
        is_resonating = r5_peer > 0.06
        if is_resonating:
            cross_sec_res += 1
        if peer_sym != sym and classify_sector_bucket(peer_sym) == sec_bucket:
            same_sec_peers += 1
            if is_resonating:
                same_sec_res += 1

    # Mandatory Phase 3a `validate_ex_ante_grouping` check across all snapshot feature groups
    feature_ts_map: dict[str, float] = {
        "trade_geometry": float(max(entry_ts if contig_5m else 0, room_feat_ts)),
        "symbol_profile": float(entry_ts),
        "cycle_map": float(cycle_feat_ts),
        "box_state": float(max(box_probe_ts, contig_5m[-1][0] if contig_5m else entry_ts)),
        "divergence_state": float(contig_5m[-1][0] if contig_5m else entry_ts),
        "btc_state": float(btc_feat_ts),
        "season_state": float(season_feat_ts),
        "resonance_state": float(resonance_feat_ts),
    }
    validate_ex_ante_grouping(
        list(feature_ts_map.keys()),
        feature_timestamps=feature_ts_map,
        entry_ts=float(entry_ts),
    )
    max_feat_ts = int(max(feature_ts_map.values()))

    entry_iso = datetime.fromtimestamp(entry_ts, tz=timezone.utc).strftime(
        "%Y-%m-%dT%H:%M:%SZ"
    )
    atr_1m_pct = (signal.atr_1m_entry / entry_px * 100.0) if entry_px > 0 else 0.0

    snapshot: dict[str, Any] = {
        "candidate_id": cand_id,
        "source_pool": source_pool,
        "max_feature_ts": max_feat_ts,
        "symbol": sym,
        "side": side,
        "entry_ts": entry_ts,
        "entry_iso_utc": entry_iso,
        "trade_geometry": {
            "entry_price": round(entry_px, 8),
            "stop_price": round(stop_px, 8),
            "stop_distance_pct": round(stop_dist_pct, 4),
            "overhead_room_r": round(room_r, 4),
            "atr_1m_pct": round(atr_1m_pct, 4),
            "adv_5m_usd": round(float(signal.adv_5m_entry_usd), 2),
        },
        "symbol_profile": {
            "listing_age_days": round(age_days, 2),
            "age_tier": classify_age_tier(age_days),
            "quote_vol_24h_usd": round(qv_24h, 2),
            "liquidity_tier": classify_liquidity_tier(qv_24h),
        },
        "cycle_map": cycle_map_obj,
        "box_state": {
            "box_5d_width_pct": round(box_5d_width_pct, 4),
            "pos_in_5d_box": round(pos_in_5d_box, 4),
            "er_4h_48h": round(er_4h_48h, 4),
            "htf_platform_width_pct": round(htf_platform_width_pct, 4),
            "upper_probe_count": up_probes,
            "lower_probe_count": lo_probes,
            "atr_4h_convergence_ratio": round(atr_4h_ratio, 4),
            "atr_5m_convergence_ratio": round(atr_5m_ratio, 4),
        },
        "divergence_state": {
            "pullback_depth_5m_pct": round(pb_depth_5m_pct, 4),
            "rebound_position_5m": round(rebound_pos_5m, 4),
            "prior_completed_minor_divs_in_4h_leg": int(prior_minor_divs),
            "dd_from_14d_4h_high_pct": round(dd_from_14d_4h_high_pct, 4),
            "bars_4h_since_14d_high": int(bars_4h_since_14d_high),
            "vol_5m_surge_ratio": round(vol_5m_surge_ratio, 4),
        },
        "btc_state": {
            "btc_1d_vs_sma50_pct": round(btc_1d_vs_sma50_pct, 4),
            "btc_4h_vs_sma18_pct": round(btc_4h_vs_sma18_pct, 4),
            "btc_ret_1h_pct": round(btc_ret_1h_pct, 4),
            "btc_ret_48h_pct": round(btc_ret_48h_pct, 4),
            "btc_dist_to_4h_high_pct": round(btc_dist_to_4h_high_pct, 4),
            "btc_sync_mode": btc_sync_mode,
            "hours_since_major_4h_launch": hours_since_launch,
            "btc_pullback_resilience_score": int(s3_resilience),
        },
        "season_state": season_state,
        "resonance_state": {
            "sector_bucket": sec_bucket,
            "same_sector_active_peers": int(same_sec_peers),
            "same_sector_resonance_count_5m": int(same_sec_res),
            "cross_section_resonance_count_5m": int(cross_sec_res),
        },
    }
    assert_snapshot_ex_ante_clean(snapshot)
    # Also verify blind payload conversion passes every stripping assertion
    _ = build_jev_payload(snapshot)
    return snapshot


def extract_phase3bj_populations(
    con: duckdb.DuckDBPyConnection,
    *,
    enforce_anchor: bool = True,
    params: FrictionParams = FrictionParams(),
) -> ExtractedPopulationsBundle:
    """Run Step A anchor check and extract Step B snapshots + evaluation table for P1 and P2."""
    anchor = verify_phase3a_dataset_anchor(con)
    if enforce_anchor and not anchor.within_2pct_anchor:
        raise RuntimeError(
            f"Step 0 dataset anchor mismatch: got {anchor.bars_1m:,} 1m bars vs "
            f"{anchor.expected_bars_1m:,} expected ({anchor.rel_diff_pct:.2f}% > 2.00% tolerance)"
        )

    bars_by_sym, funding_by_sym, tick_map = load_bars_and_funding_from_duckdb(con)
    bars_4h, bars_1d = _load_htf_bars_by_sym(con)
    pit_meta, vol_by_sym = _load_pit_and_vol(con)
    _, bars_5m, close5m_idx = _build_5m_bars_from_1m(bars_by_sym)

    btc_sym = (
        _find_major_symbol(bars_4h, bars_1d, "BTC")
        or ("BTC/USDT:USDT" if "BTC/USDT:USDT" in bars_5m else next(iter(bars_5m), "BTC/USDT:USDT"))
    )
    eth_sym = _find_major_symbol(bars_4h, bars_1d, "ETH") or "ETH/USDT:USDT"

    season_timeline = build_causal_season_timeline(bars_5m, funding_by_sym)
    btc_up_impulses = _detect_causal_btc_up_impulses(
        bars_5m, close5m_idx, pit_meta, vol_by_sym, btc_sym
    )

    # Exact Phase 3a baseline stream + S3 constraint evaluation (P1)
    h1_rep = run_h1_experiment(
        con,
        bars_by_sym=bars_by_sym,
        funding_by_sym=funding_by_sym,
        tick_map=tick_map,
        min_quote_vol_24h=30_000_000.0,
        params=params,
    )
    baseline_tickets: Sequence[AnnotatedTicketSignal] = h1_rep.tickets

    s3_rep = evaluate_s3_point2_constraints(
        baseline_tickets,
        bars_by_sym,
        bars_4h,
        bars_1d,
        funding_by_sym=funding_by_sym,
        params=params,
    )
    p1_all_cids = sorted({a.ticket.signal.wave_cluster_id for a in s3_rep.annotations})
    p1_oos_cids = set(p1_all_cids[1:]) if len(p1_all_cids) > 1 else set(p1_all_cids)

    # Exact Phase 3a S1 Point-1 candidate detection + unified wave cluster assignment (P2)
    s1_raw = detect_s1_point1_candidates(
        bars_by_sym,
        bars_4h,
        bars_1d,
        funding_by_sym,
        tick_map,
        pit_meta,
        vol_by_sym,
        params=params,
    )
    s1_rep = _evaluate_entry_setup_against_baseline(
        "S1",
        "点位 1 — 大级别平台/箱体突破启动",
        s1_raw,
        baseline_tickets,
        delta_bar=S1_DELTA_NETR_BAR,
    )

    # Reconstruct exact unified cluster OOS set used in `_evaluate_entry_setup_against_baseline`
    from clustering import build_wave_clusters

    all_unified_events: list[tuple[str, float]] = [
        (f"base_{t.signal.signal_id}", float(t.signal.entry_ts))
        for t in baseline_tickets
    ] + [
        (f"setup_{s.signal.signal_id}", float(s.signal.entry_ts))
        for s in s1_raw
    ]
    _, ev_to_unified_cid = build_wave_clusters(all_unified_events)
    base_with_u_cid = [
        (t, ev_to_unified_cid.get(f"base_{t.signal.signal_id}", t.signal.wave_cluster_id))
        for t in baseline_tickets
    ]
    unified_cids = sorted(
        {cid for _, cid in base_with_u_cid}
        | {s.signal.wave_cluster_id for s in s1_rep.setups}
    )
    p2_oos_cids = set(unified_cids[1:]) if len(unified_cids) > 1 else set(unified_cids)
    baseline_long_oos_outcomes = tuple(
        (float(t.net_r_wave), cid)
        for t, cid in base_with_u_cid
        if t.signal.direction == "UP" and cid in p2_oos_cids
    )

    snapshots: list[dict[str, Any]] = []
    outcomes_by_id: dict[str, GroundTruthTicketOutcome] = {}
    priority_by_id: dict[str, CandidatePriorityMeta] = {}

    # Build P1 (`S3_POOL`) snapshots & outcomes
    for ann in s3_rep.annotations:
        sig = ann.ticket.signal
        snap = build_ex_ante_snapshot_for_signal(
            sig,
            POOL_P1_S3,
            bars_5m_by_sym=bars_5m,
            close5m_idx_by_sym=close5m_idx,
            bars_4h_by_sym=bars_4h,
            bars_1d_by_sym=bars_1d,
            pit_meta=pit_meta,
            vol_by_sym=vol_by_sym,
            season_timeline=season_timeline,
            btc_up_impulses=btc_up_impulses,
            btc_sym=btc_sym,
            eth_sym=eth_sym,
        )
        cid = str(snap["candidate_id"])
        is_oos = sig.wave_cluster_id in p1_oos_cids
        snapshots.append(snap)
        outcomes_by_id[cid] = GroundTruthTicketOutcome(
            candidate_id=cid,
            source_pool=POOL_P1_S3,
            signal_id=sig.signal_id,
            symbol=sig.symbol,
            side=str(snap["side"]),
            entry_ts=int(sig.entry_ts),
            wave_cluster_id=sig.wave_cluster_id,
            is_oos=is_oos,
            net_r_wave=float(ann.ticket.net_r_wave),
            net_r_bracket=float(ann.ticket.net_r_bracket),
            s3_rule_retained=bool(ann.retained),
        )
        priority_by_id[cid] = CandidatePriorityMeta(
            candidate_id=cid,
            priority_tier=1 if is_oos else 3,
            entry_ts=int(sig.entry_ts),
            symbol=sig.symbol,
        )

    # Build P2 (`S1_SET`) snapshots & outcomes
    for rec in s1_rep.setups:
        sig = rec.signal
        snap = build_ex_ante_snapshot_for_signal(
            sig,
            POOL_P2_S1,
            bars_5m_by_sym=bars_5m,
            close5m_idx_by_sym=close5m_idx,
            bars_4h_by_sym=bars_4h,
            bars_1d_by_sym=bars_1d,
            pit_meta=pit_meta,
            vol_by_sym=vol_by_sym,
            season_timeline=season_timeline,
            btc_up_impulses=btc_up_impulses,
            btc_sym=btc_sym,
            eth_sym=eth_sym,
        )
        cid = str(snap["candidate_id"])
        is_oos = sig.wave_cluster_id in p2_oos_cids
        snapshots.append(snap)
        outcomes_by_id[cid] = GroundTruthTicketOutcome(
            candidate_id=cid,
            source_pool=POOL_P2_S1,
            signal_id=sig.signal_id,
            symbol=sig.symbol,
            side=str(snap["side"]),
            entry_ts=int(sig.entry_ts),
            wave_cluster_id=sig.wave_cluster_id,
            is_oos=is_oos,
            net_r_wave=float(rec.net_r_wave),
            net_r_bracket=float(rec.net_r_bracket),
            s3_rule_retained=False,
        )
        priority_by_id[cid] = CandidatePriorityMeta(
            candidate_id=cid,
            priority_tier=2 if is_oos else 4,
            entry_ts=int(sig.entry_ts),
            symbol=sig.symbol,
        )

    return ExtractedPopulationsBundle(
        anchor=anchor,
        snapshots=tuple(snapshots),
        outcomes_by_id=outcomes_by_id,
        priority_by_id=priority_by_id,
        baseline_long_oos_outcomes=baseline_long_oos_outcomes,
    )


def write_snapshots_jsonl(
    snapshots: Sequence[Mapping[str, Any]],
    out_path: str | Path,
) -> Path:
    """Write validated ex-ante snapshots to JSONL (one JSON object per line)."""
    p = Path(out_path)
    p.parent.mkdir(parents=True, exist_ok=True)
    with p.open("w", encoding="utf-8") as fh:
        for snap in snapshots:
            assert_snapshot_ex_ante_clean(snap)
            fh.write(
                json.dumps(snap, ensure_ascii=False, separators=(",", ":")) + "\n"
            )
    return p


def load_snapshots_jsonl(path: str | Path) -> list[dict[str, Any]]:
    p = Path(path)
    out: list[dict[str, Any]] = []
    for raw_line in p.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line:
            continue
        snap = json.loads(line)
        assert_snapshot_ex_ante_clean(snap)
        out.append(snap)
    return out


# ---------------------------------------------------------------------------
# Calibration & Statistical Evaluation (`PHASE3BJ_PREREG.md` §4)
# ---------------------------------------------------------------------------


def compute_calibration_summary(
    probs_and_netr: Sequence[tuple[float, float]],
) -> CalibrationSummary:
    """Compute Brier Score, Climatology, BSS, ECE, and 5-bin reliability table."""
    n = len(probs_and_netr)
    if n == 0:
        empty_bins = tuple(
            ReliabilityBinRow(
                label=lbl,
                n_obs=0,
                mean_pred_p=math.nan,
                empirical_pos_rate=math.nan,
                mean_net_r=math.nan,
            )
            for lbl, _, _, _ in RELIABILITY_BIN_SPECS
        )
        return CalibrationSummary(
            n_obs=0,
            base_pos_rate=math.nan,
            brier_score=math.nan,
            brier_climatology=math.nan,
            brier_skill_score=math.nan,
            ece=math.nan,
            bins=empty_bins,
        )

    ys = [1.0 if r > 0.0 else 0.0 for _, r in probs_and_netr]
    ps = [float(p) for p, _ in probs_and_netr]

    base_rate = math.fsum(ys) / n
    brier = math.fsum((p - y) ** 2 for p, y in zip(ps, ys)) / n
    brier_clim = base_rate * (1.0 - base_rate)
    bss = (1.0 - brier / brier_clim) if brier_clim > 1e-12 else math.nan

    bin_rows: list[ReliabilityBinRow] = []
    ece_acc = 0.0
    for label, lo, hi, inclusive_hi in RELIABILITY_BIN_SPECS:
        in_bin = [
            (p, r, (1.0 if r > 0.0 else 0.0))
            for p, r in probs_and_netr
            if (lo <= p <= hi if inclusive_hi else lo <= p < hi)
        ]
        nb = len(in_bin)
        if nb == 0:
            bin_rows.append(
                ReliabilityBinRow(
                    label=label,
                    n_obs=0,
                    mean_pred_p=math.nan,
                    empirical_pos_rate=math.nan,
                    mean_net_r=math.nan,
                )
            )
        else:
            mp = math.fsum(x[0] for x in in_bin) / nb
            ey = math.fsum(x[2] for x in in_bin) / nb
            mr = math.fsum(x[1] for x in in_bin) / nb
            ece_acc += (nb / float(n)) * abs(mp - ey)
            bin_rows.append(
                ReliabilityBinRow(
                    label=label,
                    n_obs=nb,
                    mean_pred_p=mp,
                    empirical_pos_rate=ey,
                    mean_net_r=mr,
                )
            )

    return CalibrationSummary(
        n_obs=n,
        base_pos_rate=base_rate,
        brier_score=brier,
        brier_climatology=brier_clim,
        brier_skill_score=bss,
        ece=ece_acc,
        bins=tuple(bin_rows),
    )


def evaluate_population_judgments(
    pop_code: str,
    source_pool: str,
    title: str,
    outcomes: Sequence[GroundTruthTicketOutcome],
    cache_by_id: Mapping[str, JudgmentCacheEntry],
    *,
    mode: str,
    baseline_long_oos_outcomes: Sequence[tuple[float, str]] | None = None,
) -> PopulationEvaluationReport:
    """Evaluate a single population (`P1` or `P2`) independently per `PHASE3BJ_PREREG.md` §4."""
    is_all = list(outcomes)
    oos_all = [x for x in outcomes if x.is_oos]

    judged_is = [
        (x, cache_by_id[x.candidate_id].p_netr_gt_0)
        for x in is_all
        if x.candidate_id in cache_by_id
    ]
    judged_oos = [
        (x, cache_by_id[x.candidate_id].p_netr_gt_0)
        for x in oos_all
        if x.candidate_id in cache_by_id
    ]

    pool_is_stat = cluster_robust_mean(
        [x.net_r_wave for x in is_all],
        [x.wave_cluster_id for x in is_all],
    )
    pool_oos_stat = cluster_robust_mean(
        [x.net_r_wave for x in oos_all],
        [x.wave_cluster_id for x in oos_all],
    )

    acc_is = [x for x, p in judged_is if p >= PRIMARY_TAU]
    acc_oos = [x for x, p in judged_oos if p >= PRIMARY_TAU]
    rej_oos = [x for x, p in judged_oos if p < PRIMARY_TAU]

    acc_is_stat = cluster_robust_mean(
        [x.net_r_wave for x in acc_is],
        [x.wave_cluster_id for x in acc_is],
    )
    acc_oos_stat = cluster_robust_mean(
        [x.net_r_wave for x in acc_oos],
        [x.wave_cluster_id for x in acc_oos],
    )
    rej_oos_stat = cluster_robust_mean(
        [x.net_r_wave for x in rej_oos],
        [x.wave_cluster_id for x in rej_oos],
    )

    diff_is = cluster_robust_diff(
        [x.net_r_wave for x in acc_is],
        [x.wave_cluster_id for x in acc_is],
        [x.net_r_wave for x in is_all],
        [x.wave_cluster_id for x in is_all],
    )
    diff_oos = cluster_robust_diff(
        [x.net_r_wave for x in acc_oos],
        [x.wave_cluster_id for x in acc_oos],
        [x.net_r_wave for x in oos_all],
        [x.wave_cluster_id for x in oos_all],
    )

    sens_rows: list[ThresholdSensitivityRow] = []
    for tau in SENSITIVITY_TAUS:
        sub_oos = [x for x, p in judged_oos if p >= tau]
        sub_is = [x for x, p in judged_is if p >= tau]
        st_oos = cluster_robust_mean(
            [x.net_r_wave for x in sub_oos],
            [x.wave_cluster_id for x in sub_oos],
        )
        df_oos = cluster_robust_diff(
            [x.net_r_wave for x in sub_oos],
            [x.wave_cluster_id for x in sub_oos],
            [x.net_r_wave for x in oos_all],
            [x.wave_cluster_id for x in oos_all],
        )
        sens_rows.append(
            ThresholdSensitivityRow(
                tau=tau,
                is_primary=math.isclose(tau, PRIMARY_TAU, abs_tol=1e-9),
                n_pool_oos=len(judged_oos),
                n_accepted_oos=len(sub_oos),
                accept_rate_oos=(len(sub_oos) / len(judged_oos)) if judged_oos else 0.0,
                g_accepted_oos=st_oos.n_clusters,
                accepted_oos_stat=st_oos,
                diff_vs_pool_oos=df_oos,
                n_pool_is=len(judged_is),
                n_accepted_is=len(sub_is),
                accept_rate_is=(len(sub_is) / len(judged_is)) if judged_is else 0.0,
            )
        )

    cal_oos = compute_calibration_summary([(p, x.net_r_wave) for x, p in judged_oos])
    cal_is = compute_calibration_summary([(p, x.net_r_wave) for x, p in judged_is])

    rule_ret_stat: ClusterMeanStat | None = None
    diff_jev_vs_rule: ClusterDiffStat | None = None
    overlap_oos: RuleOverlapBreakdown | None = None

    if source_pool == POOL_P1_S3:
        rule_oos = [x for x in oos_all if x.s3_rule_retained]
        rule_ret_stat = cluster_robust_mean(
            [x.net_r_wave for x in rule_oos],
            [x.wave_cluster_id for x in rule_oos],
        )
        diff_jev_vs_rule = cluster_robust_diff(
            [x.net_r_wave for x in acc_oos],
            [x.wave_cluster_id for x in acc_oos],
            [x.net_r_wave for x in rule_oos],
            [x.wave_cluster_id for x in rule_oos],
        )
        both = [x.net_r_wave for x, p in judged_oos if p >= PRIMARY_TAU and x.s3_rule_retained]
        j_only = [x.net_r_wave for x, p in judged_oos if p >= PRIMARY_TAU and not x.s3_rule_retained]
        r_only = [x.net_r_wave for x, p in judged_oos if p < PRIMARY_TAU and x.s3_rule_retained]
        neither = [x.net_r_wave for x, p in judged_oos if p < PRIMARY_TAU and not x.s3_rule_retained]
        overlap_oos = RuleOverlapBreakdown(
            n_both=len(both),
            mean_both=(math.fsum(both) / len(both)) if both else math.nan,
            n_jev_only=len(j_only),
            mean_jev_only=(math.fsum(j_only) / len(j_only)) if j_only else math.nan,
            n_rule_only=len(r_only),
            mean_rule_only=(math.fsum(r_only) / len(r_only)) if r_only else math.nan,
            n_neither=len(neither),
            mean_neither=(math.fsum(neither) / len(neither)) if neither else math.nan,
        )

    diff_vs_bl_long: ClusterDiffStat | None = None
    if source_pool == POOL_P2_S1 and baseline_long_oos_outcomes is not None:
        diff_vs_bl_long = cluster_robust_diff(
            [x.net_r_wave for x in acc_oos],
            [x.wave_cluster_id for x in acc_oos],
            [v for v, _ in baseline_long_oos_outcomes],
            [c for _, c in baseline_long_oos_outcomes],
        )

    # Primary verdict determination (`PHASE3BJ_PREREG.md` §4.2 — identical bar to Phase 3a)
    g_oos = acc_oos_stat.n_clusters
    delta_oos = diff_oos.diff_mean
    p_oos = diff_oos.one_sided_p

    reasons: list[str] = []
    if g_oos < MIN_OOS_CLUSTERS_BAR:
        reasons.append(f"G_oos = {g_oos} < {MIN_OOS_CLUSTERS_BAR}")
    if math.isnan(delta_oos) or delta_oos < DELTA_NETR_BAR:
        reasons.append(
            f"OOS Delta NetR = {delta_oos:+.4f}R < +{DELTA_NETR_BAR:.2f}R bar"
            if not math.isnan(delta_oos)
            else "OOS Delta NetR = NaN (0 accepted OOS tickets)"
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
            f"PASS: OOS Delta NetR = {delta_oos:+.4f}R (>= +0.18R), "
            f"p = {p_oos:.4f} (< 0.05), G_oos = {g_oos} (>= 15)."
        )
    elif (
        g_oos >= MIN_OOS_CLUSTERS_BAR
        and not math.isnan(acc_oos_stat.ci95_high)
        and acc_oos_stat.ci95_high < 0.0
    ):
        verdict = "KILLED"
        detail = (
            f"KILLED: Accepted OOS 95% CI [{acc_oos_stat.ci95_low:+.4f}R, "
            f"{acc_oos_stat.ci95_high:+.4f}R] is strictly negative; "
            + "; ".join(reasons)
        )
    elif g_oos < MIN_OOS_CLUSTERS_BAR:
        verdict = f"FAIL (underpowered, G_oos={g_oos})"
        detail = "FAIL (underpowered): " + "; ".join(reasons)
    else:
        verdict = "FAIL"
        detail = "FAIL: " + "; ".join(reasons)

    if mode == "mock":
        verdict = f"MOCK-DRYRUN [{verdict}]"
        detail = f"[NON-EVIDENTIARY MOCK CACHE] {detail}"

    return PopulationEvaluationReport(
        pop_code=pop_code,
        source_pool=source_pool,
        title=title,
        mode=mode,
        n_total_is=pool_is_stat.n_obs,
        g_total_is=pool_is_stat.n_clusters,
        n_total_oos=pool_oos_stat.n_obs,
        g_total_oos=pool_oos_stat.n_clusters,
        n_judged_is=len(judged_is),
        n_judged_oos=len(judged_oos),
        pool_is_stat=pool_is_stat,
        pool_oos_stat=pool_oos_stat,
        accepted_is_stat=acc_is_stat,
        accepted_oos_stat=acc_oos_stat,
        rejected_oos_stat=rej_oos_stat,
        accept_rate_is=(len(acc_is) / len(judged_is)) if judged_is else 0.0,
        accept_rate_oos=(len(acc_oos) / len(judged_oos)) if judged_oos else 0.0,
        diff_accepted_vs_pool_is=diff_is,
        diff_accepted_vs_pool_oos=diff_oos,
        sensitivity_rows=tuple(sens_rows),
        calibration_oos=cal_oos,
        calibration_is=cal_is,
        rule_retained_oos_stat=rule_ret_stat,
        diff_jev_vs_rule_oos=diff_jev_vs_rule,
        rule_overlap_oos=overlap_oos,
        diff_jev_vs_baseline_long_oos=diff_vs_bl_long,
        verdict=verdict,
        verdict_detail=detail,
    )


def evaluate_phase3bj_suite(
    bundle: ExtractedPopulationsBundle,
    cache_by_id: Mapping[str, JudgmentCacheEntry],
    *,
    manifest: JevRunManifest | None = None,
) -> Phase3bjSuiteEvaluation:
    """Evaluate P1 and P2 independently from cached Jev judgments (never pooling P1+P2)."""
    modes_seen = {e.mode for e in cache_by_id.values()}
    if len(modes_seen) > 1:
        raise ValueError(f"refusing to evaluate mixed judgment cache modes: {sorted(modes_seen)}")
    mode = next(iter(modes_seen), manifest.mode if manifest else "mock")

    p1_outcomes = [
        o for o in bundle.outcomes_by_id.values() if o.source_pool == POOL_P1_S3
    ]
    p2_outcomes = [
        o for o in bundle.outcomes_by_id.values() if o.source_pool == POOL_P2_S1
    ]

    p1_rep = evaluate_population_judgments(
        "P1",
        POOL_P1_S3,
        "主总体 P1 — S3 全候选池（同池对照 S3 小分歧约束集）",
        p1_outcomes,
        cache_by_id,
        mode=mode,
    )
    p2_rep = evaluate_population_judgments(
        "P2",
        POOL_P2_S1,
        "次总体 P2 — S1 点位 1 大级别平台/箱体突破全候选集",
        p2_outcomes,
        cache_by_id,
        mode=mode,
        baseline_long_oos_outcomes=bundle.baseline_long_oos_outcomes,
    )
    return Phase3bjSuiteEvaluation(
        anchor=bundle.anchor,
        mode=mode,
        manifest=manifest,
        p1=p1_rep,
        p2=p2_rep,
    )


def run_phase3bj_pipeline(
    con: duckdb.DuckDBPyConnection,
    *,
    work_dir: str | Path,
    mock: bool = True,
    shuffle_seed: int = DEFAULT_SHUFFLE_SEED,
    enforce_anchor: bool = True,
    client: JevClient | None = None,
) -> tuple[ExtractedPopulationsBundle, Phase3bjSuiteEvaluation, Path, Path, Path]:
    """End-to-end runner: Step A anchor -> Step B snapshots -> Step D client -> Evaluation.

    Returns `(bundle, eval_suite, snapshots_path, cache_path, manifest_path)`.
    """
    wdir = Path(work_dir)
    wdir.mkdir(parents=True, exist_ok=True)
    snapshots_path = wdir / "phase3bj_ex_ante_snapshots.jsonl"
    mode_tag = "mock" if mock else "live"
    cache_path = wdir / f"phase3bj_jev_cache_{mode_tag}.jsonl"
    manifest_path = wdir / f"phase3bj_jev_manifest_{mode_tag}.json"

    bundle = extract_phase3bj_populations(con, enforce_anchor=enforce_anchor)
    write_snapshots_jsonl(bundle.snapshots, snapshots_path)

    active_client = client if client is not None else JevClient(JevConfig.from_env(), mock=mock)
    cache_map, manifest = active_client.run_batch(
        bundle.snapshots,
        cache_path,
        manifest_path=manifest_path,
        priority_by_id=bundle.priority_by_id,
        shuffle_seed=shuffle_seed,
    )
    loaded_cache = load_judgment_cache(cache_path, expected_mode=active_client.mode)
    assert len(loaded_cache) == len(cache_map)

    suite_eval = evaluate_phase3bj_suite(bundle, loaded_cache, manifest=manifest)
    return bundle, suite_eval, snapshots_path, cache_path, manifest_path


# ---------------------------------------------------------------------------
# Markdown Report & Stage 2 Blank Template Formatters
# ---------------------------------------------------------------------------


def _fmt_r(val: float) -> str:
    if math.isnan(val):
        return "n/a"
    return f"{val:+.4f}R"


def _fmt_f(val: float, digits: int = 4) -> str:
    if math.isnan(val):
        return "n/a"
    return f"{val:.{digits}f}"


def _fmt_pct(val: float) -> str:
    if math.isnan(val):
        return "n/a"
    return f"{val * 100.0:.2f}%"


def _fmt_ci_r(st: ClusterMeanStat) -> str:
    if st.n_obs == 0 or math.isnan(st.ci95_low):
        return "`n/a`"
    return f"`[{st.ci95_low:+.4f}R, {st.ci95_high:+.4f}R]`"


def _format_population_section_md(rep: PopulationEvaluationReport) -> list[str]:
    d_oos = rep.diff_accepted_vs_pool_oos
    d_is = rep.diff_accepted_vs_pool_is
    lines: list[str] = [
        f"## {rep.title}",
        "",
        f"- **主判决（`τ = 0.60`, Walk-Forward OOS）**: **{rep.verdict}** (`{rep.verdict_detail}`)",
        f"- **全候选池规模**: IS `{rep.n_total_is}` 张 (`G = {rep.g_total_is}`) / Walk-Forward OOS `{rep.n_total_oos}` 张 (`G = {rep.g_total_oos}`)",
        f"- **已判覆盖数**: IS `{rep.n_judged_is}` / `{rep.n_total_is}`；OOS `{rep.n_judged_oos}` / `{rep.n_total_oos}`",
        f"- **主口径放行率（`τ = 0.60`）**: OOS **`{_fmt_pct(rep.accept_rate_oos)}`** (`{rep.accepted_oos_stat.n_obs} / {rep.n_judged_oos}`)；IS `{_fmt_pct(rep.accept_rate_is)}` (`{rep.accepted_is_stat.n_obs} / {rep.n_judged_is}`)",
        "",
        "### 核心组间对比（`wave_k_be=0.8` 全摩擦 `PolicyNetR` + `CR1` 波次聚类稳健推断）",
        "",
        "| 分组 / 口径 | `N` | `G` | Mean `PolicyNetR` | Cluster SE | 95% CI | vs 同池基线 `ΔNetR` | 单侧 `p` |",
        "|---|---:|---:|---:|---:|---|---:|---:|",
        f"| `同总体全候选基线 ALL (OOS)` | {rep.pool_oos_stat.n_obs} | {rep.pool_oos_stat.n_clusters} | `{_fmt_r(rep.pool_oos_stat.mean)}` | `{_fmt_f(rep.pool_oos_stat.cluster_se)}` | {_fmt_ci_r(rep.pool_oos_stat)} | — | — |",
        f"| **`Jev 放行组 p >= 0.60 (OOS 主口径)`** | **{rep.accepted_oos_stat.n_obs}** | **{rep.accepted_oos_stat.n_clusters}** | **`{_fmt_r(rep.accepted_oos_stat.mean)}`** | `{_fmt_f(rep.accepted_oos_stat.cluster_se)}` | {_fmt_ci_r(rep.accepted_oos_stat)} | **`{_fmt_r(d_oos.diff_mean)}`** | **`{_fmt_f(d_oos.one_sided_p)}`** |",
        f"| `Jev 拒绝组 p < 0.60 (OOS 诊断)` | {rep.rejected_oos_stat.n_obs} | {rep.rejected_oos_stat.n_clusters} | `{_fmt_r(rep.rejected_oos_stat.mean)}` | `{_fmt_f(rep.rejected_oos_stat.cluster_se)}` | {_fmt_ci_r(rep.rejected_oos_stat)} | — | — |",
        f"| `同总体全候选基线 ALL (IS 诊断)` | {rep.pool_is_stat.n_obs} | {rep.pool_is_stat.n_clusters} | `{_fmt_r(rep.pool_is_stat.mean)}` | `{_fmt_f(rep.pool_is_stat.cluster_se)}` | {_fmt_ci_r(rep.pool_is_stat)} | — | — |",
        f"| `Jev 放行组 p >= 0.60 (IS 诊断)` | {rep.accepted_is_stat.n_obs} | {rep.accepted_is_stat.n_clusters} | `{_fmt_r(rep.accepted_is_stat.mean)}` | `{_fmt_f(rep.accepted_is_stat.cluster_se)}` | {_fmt_ci_r(rep.accepted_is_stat)} | `{_fmt_r(d_is.diff_mean)}` | `{_fmt_f(d_is.one_sided_p)}` |",
    ]

    if rep.diff_jev_vs_baseline_long_oos is not None:
        dbl = rep.diff_jev_vs_baseline_long_oos
        lines.append(
            f"| `附报参照：Jev 放行组 vs 3a 基线多头流 (OOS)` | {rep.accepted_oos_stat.n_obs} vs {dbl.stat_b.n_obs} | {dbl.n_clusters_total} | `{_fmt_r(rep.accepted_oos_stat.mean)}` vs `{_fmt_r(dbl.stat_b.mean)}` | `{_fmt_f(dbl.diff_cluster_se)}` | `[{_fmt_r(dbl.ci95_low)}, {_fmt_r(dbl.ci95_high)}]` | `{_fmt_r(dbl.diff_mean)}` | `{_fmt_f(dbl.one_sided_p)}` |"
        )

    lines.extend(
        [
            "",
            "### 阈值 `τ` 敏感性附报（仅 `τ = 0.60` 绑定主判决；`0.55 / 0.65` 只描述不改判）",
            "",
            "| 阈值 `τ` | 口径定位 | OOS 放行数 `N` | OOS 放行率 | `G_oos` | OOS Mean `PolicyNetR` | vs 全池 `ΔNetR` | 单侧 `p` |",
            "|---:|---|---:|---:|---:|---:|---:|---:|",
        ]
    )
    for srow in rep.sensitivity_rows:
        role = "**主口径（写死）**" if srow.is_primary else "描述性附报（不改判）"
        lines.append(
            f"| `{srow.tau:.2f}` | {role} | {srow.n_accepted_oos} | `{_fmt_pct(srow.accept_rate_oos)}` | {srow.g_accepted_oos} | `{_fmt_r(srow.accepted_oos_stat.mean)}` | `{_fmt_r(srow.diff_vs_pool_oos.diff_mean)}` | `{_fmt_f(srow.diff_vs_pool_oos.one_sided_p)}` |"
        )

    if (
        rep.rule_retained_oos_stat is not None
        and rep.diff_jev_vs_rule_oos is not None
        and rep.rule_overlap_oos is not None
    ):
        rr = rep.rule_retained_oos_stat
        dj = rep.diff_jev_vs_rule_oos
        ov = rep.rule_overlap_oos
        lines.extend(
            [
                "",
                "### 次要诊断 A：Jev 放行组 vs Phase 3a S3 规则保留组头对头对照（仅 P1，只描述不改判）",
                "",
                "| 对照项（Walk-Forward OOS） | `N` | `G` | Mean `PolicyNetR` | Cluster SE | 95% CI | 头对头 `ΔNetR (Jev - Rule)` | 单侧 `p` |",
                "|---|---:|---:|---:|---:|---|---:|---:|",
                f"| `Jev 盲判放行组 (p >= 0.60)` | {rep.accepted_oos_stat.n_obs} | {rep.accepted_oos_stat.n_clusters} | `{_fmt_r(rep.accepted_oos_stat.mean)}` | `{_fmt_f(rep.accepted_oos_stat.cluster_se)}` | {_fmt_ci_r(rep.accepted_oos_stat)} | `{_fmt_r(dj.diff_mean)}` | `{_fmt_f(dj.one_sided_p)}` |",
                f"| `S3 固定规则保留组 (C1∧C2∧C3∧C4)` | {rr.n_obs} | {rr.n_clusters} | `{_fmt_r(rr.mean)}` | `{_fmt_f(rr.cluster_se)}` | {_fmt_ci_r(rr)} | — | — |",
                "",
                f"- **2×2 交叉列联（OOS）**：`Jev ∩ Rule` = `{ov.n_both}` 张（均值 `{_fmt_r(ov.mean_both)}`）；`Jev \\ Rule` = `{ov.n_jev_only}` 张（均值 `{_fmt_r(ov.mean_jev_only)}`）；`Rule \\ Jev` = `{ov.n_rule_only}` 张（均值 `{_fmt_r(ov.mean_rule_only)}`）；`双拒` = `{ov.n_neither}` 张（均值 `{_fmt_r(ov.mean_neither)}`）。",
            ]
        )

    cal = rep.calibration_oos
    lines.extend(
        [
            "",
            "### 次要诊断 B：Jev 概率校准（OOS，目标事件 `y = 1{PolicyNetR > 0}`，只描述不改判）",
            "",
            f"- **样本基准正净值率 `P(NetR > 0)`**: `{_fmt_pct(cal.base_pos_rate)}` (`N = {cal.n_obs}`)",
            f"- **Brier Score**: `{_fmt_f(cal.brier_score)}`（无技巧气候学基准 `BS_clim = {_fmt_f(cal.brier_climatology)}`，`Brier Skill Score = {_fmt_f(cal.brier_skill_score)}`，`ECE = {_fmt_f(cal.ece)}`）",
            "",
            "| 预测概率分箱 | 票数 `n` | 平均预测概率 `mean(p)` | 实际 `P(NetR > 0)` | 箱内 Mean `PolicyNetR` |",
            "|---|---:|---:|---:|---:|",
        ]
    )
    for brow in cal.bins:
        lines.append(
            f"| `{brow.label}` | {brow.n_obs} | `{_fmt_f(brow.mean_pred_p)}` | `{_fmt_f(brow.empirical_pos_rate)}` | `{_fmt_r(brow.mean_net_r)}` |"
        )
    lines.append("")
    return lines


def format_phase3bj_evaluation_markdown(suite: Phase3bjSuiteEvaluation) -> str:
    """Format complete Phase 3b-J evaluation report (clearly badged when `mode == 'mock'`)."""
    a = suite.anchor
    today = datetime.now(timezone.utc).date().isoformat()
    is_mock = suite.mode == "mock"
    header_title = (
        f"# Phase 3b-J Stage 1 端到端 Mock 干跑报告（NON-EVIDENTIARY DRY-RUN） ({today})"
        if is_mock
        else f"# Phase 3b-J 实验报告 — Jev 盲判重验 ({today})"
    )
    banner_lines = (
        [
            "> **⚠️ STAGE 1 MOCK DRY-RUN 声明（非实证结论）**：本报表由 `--mock` 模式（基于 `HMAC-SHA256(candidate_id)` 的确定性伪概率哈希，未调用任何外部模型或规则替代品）自动生成，仅用于证明「数据集重演 → 事前盲判快照 → 盲判载荷剥离 → 幂等缓存客户端 → CR1 聚类统计与校准评测器」端到端全链路畅通。真实结论待 Stage 2 闸门开启并注入真实 `JEV_BASE_URL` / `JEV_API_KEY` 后生成。",
            "",
        ]
        if is_mock
        else []
    )

    manifest_lines: list[str] = []
    if suite.manifest is not None:
        m = suite.manifest
        manifest_lines = [
            f"- **运行模式 / 端点宿主**: `{m.mode}` / `{m.endpoint_host}`",
            f"- **盲判模板版本与指纹**: `{m.template_id}` (`sha256={m.template_spec_sha256[:16]}...`)",
            f"- **随机提交序种子（`shuffle_seed`）**: `{m.shuffle_seed}`（提交序 `sha256={m.submitted_order_sha256[:16]}...`）",
            f"- **调用统计**: 总输入 `{m.total_candidates_input}` 张，缓存命中 `{m.cached_hits}` 张，本次新判 `{m.new_calls_completed}` 张，预算截断 `{m.truncated_by_budget}` 张（上限 `{m.max_calls}`）",
        ]

    lines = [
        header_title,
        "",
        *banner_lines,
        "- **分支**: `feat/kairos-phase3bj-jev-blind-judgment`",
        "- **预注册锚点**: `docs/40-research/experiments/PHASE3BJ_PREREG.md`（与 Phase 3a 完全同一道门槛：`OOS ΔNetR >= +0.18R`、单侧 `p < 0.05`、`G_oos >= 15`；主口径 `τ = 0.60`；P1/P2 独立判定）",
        *manifest_lines,
        "",
        f"> {KNOWN_TRAINING_OVERLAP_CAVEAT}",
        "",
        "## 1. 步骤 A 数据集重演核验（硬锚点 `758,013 ± 2%`）",
        "",
        "| 数据表 / 指标 | Phase 2b/3a 锚点值 | 本次重演实测值 | 偏差 | 状态 |",
        "|---|---:|---:|---:|---|",
        f"| `fact_perp_bars_1m` | `758,013` | **`{a.bars_1m:,}`** | `{a.rel_diff_pct:.3f}%` | **{'PASS (`<= 2%`)' if a.within_2pct_anchor else 'STOP (`> 2%`)'}** |",
        f"| `fact_perp_bars_htf` (`4h` + `1d`) | `53,122` (3a) | **`{a.bars_htf:,}`** | — | OK |",
        f"| `fact_perp_volume_24h` | `197,776` (3a) | **`{a.volume_24h_obs:,}`** | — | OK |",
        f"| `fact_perp_funding_8h` | `6,995` | **`{a.funding_ticks:,}`** | — | OK |",
        f"| `fact_perp_oi_5m` | `30,157` (3a) | **`{a.oi_5m_ticks:,}`** | — | OK |",
        f"| `采样标的数 / PIT 合约总数` | `27 / 528` | **`{a.distinct_1m_symbols} / {a.pit_universe_total}`** | — | OK |",
        "",
        "## 2. 总判决矩阵（Walk-Forward OOS 主口径，`τ = 0.60`，P1/P2 独立判定）",
        "",
        "| 总体 | 定义溯源 | OOS 基线 `N` (`G`) | Jev 放行 `N` (`G_oos`) | 放行率 | 放行组 OOS Mean (`95% CI`) | vs 同池基线 `ΔNetR` | 单侧 `p` | 判决 |",
        "|---|---|---:|---:|---:|---|---:|---:|---|",
        f"| **P1 (`S3_POOL`)** | 3a S3 全候选池 | `{suite.p1.n_total_oos}` (`{suite.p1.g_total_oos}`) | `{suite.p1.accepted_oos_stat.n_obs}` (`{suite.p1.accepted_oos_stat.n_clusters}`) | `{_fmt_pct(suite.p1.accept_rate_oos)}` | `{_fmt_r(suite.p1.accepted_oos_stat.mean)}` ({_fmt_ci_r(suite.p1.accepted_oos_stat)}) | **`{_fmt_r(suite.p1.diff_accepted_vs_pool_oos.diff_mean)}`** | `{_fmt_f(suite.p1.diff_accepted_vs_pool_oos.one_sided_p)}` | **{suite.p1.verdict}** |",
        f"| **P2 (`S1_SET`)** | 3a S1 全候选票 | `{suite.p2.n_total_oos}` (`{suite.p2.g_total_oos}`) | `{suite.p2.accepted_oos_stat.n_obs}` (`{suite.p2.accepted_oos_stat.n_clusters}`) | `{_fmt_pct(suite.p2.accept_rate_oos)}` | `{_fmt_r(suite.p2.accepted_oos_stat.mean)}` ({_fmt_ci_r(suite.p2.accepted_oos_stat)}) | **`{_fmt_r(suite.p2.diff_accepted_vs_pool_oos.diff_mean)}`** | `{_fmt_f(suite.p2.diff_accepted_vs_pool_oos.one_sided_p)}` | **{suite.p2.verdict}** |",
        "",
        *_format_population_section_md(suite.p1),
        *_format_population_section_md(suite.p2),
    ]
    return "\n".join(lines).rstrip()


def format_phase3bj_stage2_template_markdown(bundle: ExtractedPopulationsBundle) -> str:
    """Format the official Stage 2 report template with live Jev cells left blank (`TBD`)."""
    a = bundle.anchor
    p1_all = [o for o in bundle.outcomes_by_id.values() if o.source_pool == POOL_P1_S3]
    p2_all = [o for o in bundle.outcomes_by_id.values() if o.source_pool == POOL_P2_S1]
    p1_oos = [o for o in p1_all if o.is_oos]
    p2_oos = [o for o in p2_all if o.is_oos]
    p1_rule_oos = [o for o in p1_oos if o.s3_rule_retained]

    p1_oos_st = cluster_robust_mean(
        [o.net_r_wave for o in p1_oos], [o.wave_cluster_id for o in p1_oos]
    )
    p2_oos_st = cluster_robust_mean(
        [o.net_r_wave for o in p2_oos], [o.wave_cluster_id for o in p2_oos]
    )
    p1_rule_st = cluster_robust_mean(
        [o.net_r_wave for o in p1_rule_oos], [o.wave_cluster_id for o in p1_rule_oos]
    )

    spec_sha = compute_template_spec_sha256()
    return "\n".join(
        [
            "# Phase 3b-J 实验报告模板 — Jev 盲判重验（Stage 2 待填实测值）",
            "",
            "- **状态**: **TEMPLATE（Stage 1 先行落盘，Jev 盲判实测数值留空待 Stage 2 闸门开启后由评测器自动填充）**",
            "- **分支**: `feat/kairos-phase3bj-jev-blind-judgment`",
            "- **预注册锚点**: `docs/40-research/experiments/PHASE3BJ_PREREG.md`",
            f"- **盲判载荷模板版本**: `{TEMPLATE_ID}` (`spec_sha256 = {spec_sha}`)",
            "- **Stage 2 运行清单元数据**: `endpoint_host = <TBD_STAGE2>` | `shuffle_seed = 20261111` | `submitted_order_sha256 = <TBD_STAGE2>`",
            "",
            f"> {KNOWN_TRAINING_OVERLAP_CAVEAT}",
            "",
            "## 1. 步骤 A 数据集重演核验（Stage 1 已锁定）",
            "",
            "| 数据表 / 指标 | Phase 2b/3a 锚点值 | Stage 1 重演实测值 | 偏差 | 状态 |",
            "|---|---:|---:|---:|---|",
            f"| `fact_perp_bars_1m`（硬锚点 `±2%`） | `758,013` | **`{a.bars_1m:,}`** | `{a.rel_diff_pct:.3f}%` | **{'PASS (`<= 2%`)' if a.within_2pct_anchor else 'STOP (`> 2%`)'}** |",
            f"| `fact_perp_bars_htf` (`4h` + `1d`) | `53,122` (3a) | **`{a.bars_htf:,}`** | — | OK |",
            f"| `fact_perp_volume_24h` | `197,776` (3a) | **`{a.volume_24h_obs:,}`** | — | OK |",
            f"| `fact_perp_funding_8h` | `6,995` | **`{a.funding_ticks:,}`** | — | OK |",
            f"| `fact_perp_oi_5m` | `30,157` (3a) | **`{a.oi_5m_ticks:,}`** | — | OK |",
            f"| `采样标的数 / PIT 合约总数` | `27 / 528` | **`{a.distinct_1m_symbols} / {a.pit_universe_total}`** | — | OK |",
            "",
            "## 2. 主判决总表（Walk-Forward OOS，`τ = 0.60` 写死，P1 / P2 独立判定）",
            "",
            "| 总体 | 全样本 IS+OOS `N` | OOS 基线 `N` (`G`) | OOS 基线 Mean `PolicyNetR` | Jev 放行 `N` (`G_oos`) | OOS 放行率 | Jev 放行 Mean (`95% CI`) | OOS `ΔNetR` (vs 同池基线) | 单侧 `p` | 最终判决 |",
            "|---|---:|---:|---:|---:|---:|---|---:|---:|---|",
            f"| **P1 (`S3_POOL`)** | `{len(p1_all)}` | `{p1_oos_st.n_obs}` (`{p1_oos_st.n_clusters}`) | `{_fmt_r(p1_oos_st.mean)}` | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* |",
            f"| **P2 (`S1_SET`)** | `{len(p2_all)}` | `{p2_oos_st.n_obs}` (`{p2_oos_st.n_clusters}`) | `{_fmt_r(p2_oos_st.mean)}` | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* |",
            "",
            "## 3. 阈值敏感性附报表（`τ ∈ {0.55, 0.60, 0.65}`；`0.55 / 0.65` 只描述不改判）",
            "",
            "| 总体 | 阈值 `τ` | 定位 | OOS 放行数 `N` | OOS 放行率 | `G_oos` | OOS Mean `PolicyNetR` | vs 同池 `ΔNetR` | 单侧 `p` |",
            "|---|---:|---|---:|---:|---:|---:|---:|---:|",
            "| **P1 (`S3_POOL`)** | `0.55` | 描述性附报 | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* |",
            "| **P1 (`S3_POOL`)** | **`0.60`** | **主口径（写死）** | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* |",
            "| **P1 (`S3_POOL`)** | `0.65` | 描述性附报 | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* |",
            "| **P2 (`S1_SET`)** | `0.55` | 描述性附报 | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* |",
            "| **P2 (`S1_SET`)** | **`0.60`** | **主口径（写死）** | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* |",
            "| **P2 (`S1_SET`)** | `0.65` | 描述性附报 | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* |",
            "",
            "## 4. 次要诊断 A：P1 头对头对照（Jev 放行组 vs Phase 3a S3 规则保留组，OOS）",
            "",
            "| 对照组（P1 Walk-Forward OOS） | `N` | `G` | Mean `PolicyNetR` | Cluster SE | 95% CI | 头对头 `ΔNetR (Jev - Rule)` | 单侧 `p` |",
            "|---|---:|---:|---:|---:|---|---:|---:|",
            "| `Jev 盲判放行组 (p >= 0.60)` | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* |",
            f"| `S3 固定规则保留组 (C1∧C2∧C3∧C4)` | `{p1_rule_st.n_obs}` | `{p1_rule_st.n_clusters}` | `{_fmt_r(p1_rule_st.mean)}` | `{_fmt_f(p1_rule_st.cluster_se)}` | {_fmt_ci_r(p1_rule_st)} | — | — |",
            "",
            "- **2×2 交叉列联（OOS）**: `Jev ∩ Rule` = *TBD* | `Jev \\ Rule` = *TBD* | `Rule \\ Jev` = *TBD* | `双拒` = *TBD*",
            "",
            "## 5. 次要诊断 B：Jev 概率校准（Brier 分数与 5 档可靠性分箱，OOS）",
            "",
            "| 总体 | OOS `N` | 基准 `P(NetR > 0)` | Brier Score | `BS_clim` | Brier Skill Score | `ECE` |",
            "|---|---:|---:|---:|---:|---:|---:|",
            f"| **P1 (`S3_POOL`)** | `{p1_oos_st.n_obs}` | `{_fmt_pct(sum(1 for o in p1_oos if o.net_r_wave > 0) / max(1, len(p1_oos)))}` | *TBD* | *TBD* | *TBD* | *TBD* |",
            f"| **P2 (`S1_SET`)** | `{p2_oos_st.n_obs}` | `{_fmt_pct(sum(1 for o in p2_oos if o.net_r_wave > 0) / max(1, len(p2_oos)))}` | *TBD* | *TBD* | *TBD* | *TBD* |",
            "",
            "| 概率分箱 | P1 `n` | P1 `mean(p)` | P1 实际 `P(NetR>0)` | P1 Mean `NetR` | P2 `n` | P2 `mean(p)` | P2 实际 `P(NetR>0)` | P2 Mean `NetR` |",
            "|---|---:|---:|---:|---:|---:|---:|---:|---:|",
            "| `[0.00, 0.40)` | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* |",
            "| `[0.40, 0.55)` | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* |",
            "| `[0.55, 0.60)` | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* |",
            "| `[0.60, 0.70)` | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* |",
            "| `[0.70, 1.00]` | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* |",
            "",
            "## 6. Stage 2 开工闸门清单（Gate Checklist）",
            "",
            "1. 主会话提供真实 Jev HTTP 端点并通过环境变量注入 `JEV_BASE_URL`（绝不写入仓库文件）。",
            "2. 主会话在 Stage 2 开工时单独提供 `JEV_API_KEY` 并仅通过进程环境变量注入（日志/异常/缓存/报告全程脱敏）。",
            "3. 核对 `PHASE3BJ_PREREG.md` 附录 A 载荷模板指纹 `spec_sha256` 未发生任何改动。",
            "4. 运行 `uv run python research.py phase3bj --db ../../data/phase2b.duckdb --work-dir ../../data/phase3bj --out-report ../../docs/40-research/experiments/2026-11-11-phase3bj-jev-blind-judgment.md`（不带 `--mock`，自动创建全新 `live` 缓存并与 Stage 1 `mock` 缓存物理隔离）。",
        ]
    )
