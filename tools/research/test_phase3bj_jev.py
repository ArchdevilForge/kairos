"""Unit tests for Phase 3b-J Jev blind judgment scaffold, snapshot hygiene, and evaluator."""

from __future__ import annotations

import copy
import json
import logging
import math
from pathlib import Path
from typing import Any, Mapping

import duckdb
import pytest

from clustering import ExAnteViolationError
from h1_experiment import AnnotatedTicketSignal, SeasonObservation
from jev_client import (
    CandidatePriorityMeta,
    EXPECTED_CONTEXT_TOP_KEYS,
    FROZEN_PROMPT_TEMPLATE,
    JevClient,
    JevConfig,
    JudgmentCacheEntry,
    RedactingLogFilter,
    TEMPLATE_ID,
    assert_blind_payload_clean,
    assert_snapshot_ex_ante_clean,
    build_jev_payload,
    compute_template_spec_sha256,
    load_judgment_cache,
    mock_jev_probability,
    redact_sensitive_text,
    select_and_shuffle_pending,
)
from p0_experiment import ensure_market_bars_schema
from phase3a_experiment import (
    _build_5m_bars_from_1m,
    evaluate_s3_point2_constraints,
)
from phase3bj_experiment import (
    GroundTruthTicketOutcome,
    POOL_P1_S3,
    POOL_P2_S1,
    PRIMARY_TAU,
    build_ex_ante_snapshot_for_signal,
    compute_calibration_summary,
    evaluate_population_judgments,
    extract_phase3bj_populations,
    format_phase3bj_evaluation_markdown,
    format_phase3bj_stage2_template_markdown,
    run_phase3bj_pipeline,
    write_snapshots_jsonl,
)
from policies import CandidateSignal, MinuteBar, simulate_holding_policy


def _make_minute_bars_aligned_5m(
    start_ts_aligned_300: int,
    specs_5m: list[tuple[float, float, float, float, float]],
) -> list[MinuteBar]:
    """Create 5x 1m bars per 5m bar spec so `_build_5m_bars_from_1m` closes at `b_open + 300`."""
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


def _build_synthetic_world(
    future_bull: bool,
) -> tuple[
    CandidateSignal,
    dict[str, list[MinuteBar]],
    dict[str, list[tuple[int, float, float, float, float, float]]],
    dict[str, dict[int, int]],
    dict[str, list[tuple[int, float, float, float, float, float]]],
    dict[str, list[tuple[int, float, float, float, float, float]]],
    dict[str, tuple[int, int | None]],
    dict[str, list[tuple[int, float]]],
    dict[int, SeasonObservation],
    list[int],
]:
    """Create two worlds identical up to `entry_ts` and opposite after `entry_ts`."""
    sym = "SOL/USDT:USDT"
    btc = "BTC/USDT:USDT"
    eth = "ETH/USDT:USDT"
    peer = "SUI/USDT:USDT"
    base_ts = 1_765_008_000  # divisible by 86400, 14400, 300

    # 45 closed 5m bars up to entry_ts (`base_ts + 45 * 300`)
    entry_ts = base_ts + 45 * 300
    prefix_specs = [
        (100.0 + 0.1 * i, 100.6 + 0.1 * i, 99.7 + 0.1 * i, 100.3 + 0.1 * i, 150_000.0)
        for i in range(45)
    ]
    if future_bull:
        post_specs = [
            (105.0 + 0.8 * k, 106.0 + 0.8 * k, 104.9 + 0.8 * k, 105.7 + 0.8 * k, 300_000.0)
            for k in range(30)
        ]
    else:
        post_specs = [
            (104.5 - 1.2 * k, 104.6 - 1.2 * k, 102.5 - 1.2 * k, 102.8 - 1.2 * k, 300_000.0)
            for k in range(30)
        ]

    bars_1m: dict[str, list[MinuteBar]] = {
        sym: _make_minute_bars_aligned_5m(base_ts, prefix_specs + post_specs),
        btc: _make_minute_bars_aligned_5m(base_ts, prefix_specs + post_specs),
        eth: _make_minute_bars_aligned_5m(base_ts, prefix_specs + post_specs),
        peer: _make_minute_bars_aligned_5m(base_ts, prefix_specs + post_specs),
    }
    _, bars_5m, close5m_idx = _build_5m_bars_from_1m(bars_1m)

    # 45 closed 4h bars strictly `<= base_ts`, plus post-entry 4h bars `> entry_ts`
    pre_4h = [
        (base_ts - (45 - k) * 14_400, 90.0 + k * 0.2, 91.5 + k * 0.2, 89.5 + k * 0.2, 90.8 + k * 0.2, 2e7)
        for k in range(1, 46)
    ]
    post_4h = [
        (
            entry_ts + 14_400 * (k + 1),
            150.0 if future_bull else 40.0,
            180.0 if future_bull else 42.0,
            145.0 if future_bull else 20.0,
            175.0 if future_bull else 22.0,
            9e8,
        )
        for k in range(5)
    ]
    bars_4h = {s: list(pre_4h + post_4h) for s in (sym, btc, eth, peer)}

    pre_1d = [
        (base_ts - (55 - d) * 86_400, 70.0 + d * 0.5, 72.0 + d * 0.5, 69.0 + d * 0.5, 71.2 + d * 0.5, 1e8)
        for d in range(1, 56)
    ]
    post_1d = [
        (
            entry_ts + 86_400 * (d + 1),
            200.0 if future_bull else 10.0,
            220.0 if future_bull else 12.0,
            195.0 if future_bull else 5.0,
            215.0 if future_bull else 6.0,
            9e8,
        )
        for d in range(3)
    ]
    bars_1d = {s: list(pre_1d + post_1d) for s in (sym, btc, eth, peer)}

    pit_meta = {s: (1_700_000_000, None) for s in (sym, btc, eth, peer)}
    vol_by_sym = {
        s: [
            (entry_ts - 300, 120_000_000.0),
            (entry_ts + 600, 999_000_000.0 if future_bull else 1_000_000.0),
        ]
        for s in (sym, btc, eth, peer)
    }
    season_timeline = {
        entry_ts: SeasonObservation(
            ts=entry_ts,
            season_regime="SUMMER",
            breadth_fast=0.62,
            breadth_slow=0.58,
            breadth_momentum=0.04,
            volume_ratio=1.25,
            btc_near_high=True,
            crowding_pct=0.52,
        ),
        entry_ts + 300: SeasonObservation(
            ts=entry_ts + 300,
            season_regime="SUMMER" if future_bull else "WINTER",
            breadth_fast=0.95 if future_bull else 0.05,
            breadth_slow=0.90 if future_bull else 0.10,
            breadth_momentum=0.30 if future_bull else -0.40,
            volume_ratio=3.0 if future_bull else 0.3,
            btc_near_high=future_bull,
            crowding_pct=0.95 if future_bull else 0.05,
        ),
    }
    btc_impulses = [entry_ts - 600] + ([entry_ts + 300] if future_bull else [])

    entry_px = bars_5m[sym][44][4]
    sig = CandidateSignal(
        signal_id="test_sig_001",
        wave_cluster_id="wave_0002",
        symbol=sym,
        direction="UP",
        entry_ts=entry_ts,
        entry_price=entry_px,
        stop_price=entry_px * 0.98,
        atr_1m_entry=0.25,
        adv_5m_entry_usd=750_000.0,
    )
    return (
        sig,
        bars_1m,
        bars_5m,
        close5m_idx,
        bars_4h,
        bars_1d,
        pit_meta,
        vol_by_sym,
        season_timeline,
        btc_impulses,
    )


def test_step_a_anchor_check_blocks_out_of_tolerance_db():
    con = duckdb.connect(":memory:")
    ensure_market_bars_schema(con)
    with pytest.raises(RuntimeError, match="Step 0 dataset anchor mismatch"):
        extract_phase3bj_populations(con, enforce_anchor=True)
    con.close()


def test_snapshot_ex_ante_invariance_under_future_bar_mutation():
    bull = _build_synthetic_world(future_bull=True)
    bear = _build_synthetic_world(future_bull=False)

    sig_b, bars1_b, b5_b, idx_b, b4_b, b1d_b, pit_b, vol_b, sea_b, imp_b = bull
    sig_c, bars1_c, b5_c, idx_c, b4_c, b1d_c, pit_c, vol_c, sea_c, imp_c = bear

    res_bull = simulate_holding_policy(
        sig_b, bars1_b[sig_b.symbol], policy_kind="wave", k_be=0.8, max_hold_minutes=1440
    )
    res_bear = simulate_holding_policy(
        sig_c, bars1_c[sig_c.symbol], policy_kind="wave", k_be=0.8, max_hold_minutes=1440
    )
    assert res_bull.net_r > 0.5
    assert res_bear.net_r < -0.5

    snap_bull = build_ex_ante_snapshot_for_signal(
        sig_b,
        POOL_P1_S3,
        bars_5m_by_sym=b5_b,
        close5m_idx_by_sym=idx_b,
        bars_4h_by_sym=b4_b,
        bars_1d_by_sym=b1d_b,
        pit_meta=pit_b,
        vol_by_sym=vol_b,
        season_timeline=sea_b,
        btc_up_impulses=imp_b,
        btc_sym="BTC/USDT:USDT",
        eth_sym="ETH/USDT:USDT",
    )
    snap_bear = build_ex_ante_snapshot_for_signal(
        sig_c,
        POOL_P1_S3,
        bars_5m_by_sym=b5_c,
        close5m_idx_by_sym=idx_c,
        bars_4h_by_sym=b4_c,
        bars_1d_by_sym=b1d_c,
        pit_meta=pit_c,
        vol_by_sym=vol_c,
        season_timeline=sea_c,
        btc_up_impulses=imp_c,
        btc_sym="BTC/USDT:USDT",
        eth_sym="ETH/USDT:USDT",
    )

    assert snap_bull["max_feature_ts"] <= snap_bull["entry_ts"]
    assert json.dumps(snap_bull, sort_keys=True) == json.dumps(snap_bear, sort_keys=True)
    assert json.dumps(build_jev_payload(snap_bull), sort_keys=True) == json.dumps(
        build_jev_payload(snap_bear), sort_keys=True
    )


@pytest.mark.parametrize(
    "forbidden_key",
    [
        "net_r",
        "net_r_wave",
        "net_r_bracket",
        "gross_r",
        "realized_r",
        "mfe",
        "mae",
        "exit_ts",
        "exit_price",
        "exit_reason",
        "hold_seconds",
        "future_bars",
        "win",
        "outcome",
        "verdict",
        "retained",
        "is_filtered",
        "rule_pass",
        "s3_rule_retained",
        "c1_ok",
        "c2_div_count_le_2",
        "is_box_mid_chop",
        "is_top2_langlang",
        "wave_cluster_id",
        "is_oos",
    ],
)
def test_snapshot_rejects_every_post_entry_and_rule_verdict_key_even_nested(
    forbidden_key: str,
    tmp_path: Path,
):
    sig, _, b5, idx, b4, b1d, pit, vol, sea, imp = _build_synthetic_world(future_bull=True)
    clean_snap = build_ex_ante_snapshot_for_signal(
        sig,
        POOL_P1_S3,
        bars_5m_by_sym=b5,
        close5m_idx_by_sym=idx,
        bars_4h_by_sym=b4,
        bars_1d_by_sym=b1d,
        pit_meta=pit,
        vol_by_sym=vol,
        season_timeline=sea,
        btc_up_impulses=imp,
        btc_sym="BTC/USDT:USDT",
        eth_sym="ETH/USDT:USDT",
    )

    poisoned_top = copy.deepcopy(clean_snap)
    poisoned_top[forbidden_key] = 1
    with pytest.raises(ExAnteViolationError):
        assert_snapshot_ex_ante_clean(poisoned_top)
    with pytest.raises(ExAnteViolationError):
        write_snapshots_jsonl([poisoned_top], tmp_path / "bad.jsonl")

    poisoned_nested = copy.deepcopy(clean_snap)
    poisoned_nested["box_state"][forbidden_key] = False
    with pytest.raises(ExAnteViolationError):
        assert_snapshot_ex_ante_clean(poisoned_nested)

    future_ts_snap = copy.deepcopy(clean_snap)
    future_ts_snap["max_feature_ts"] = clean_snap["entry_ts"] + 60
    with pytest.raises(ExAnteViolationError, match="max_feature_ts"):
        assert_snapshot_ex_ante_clean(future_ts_snap)


def test_blind_payload_strips_source_pool_sequence_and_matches_prereg_appendix_a():
    sig, _, b5, idx, b4, b1d, pit, vol, sea, imp = _build_synthetic_world(future_bull=True)
    snap_p1 = build_ex_ante_snapshot_for_signal(
        sig,
        POOL_P1_S3,
        bars_5m_by_sym=b5,
        close5m_idx_by_sym=idx,
        bars_4h_by_sym=b4,
        bars_1d_by_sym=b1d,
        pit_meta=pit,
        vol_by_sym=vol,
        season_timeline=sea,
        btc_up_impulses=imp,
        btc_sym="BTC/USDT:USDT",
        eth_sym="ETH/USDT:USDT",
    )
    snap_p2 = build_ex_ante_snapshot_for_signal(
        sig,
        POOL_P2_S1,
        bars_5m_by_sym=b5,
        close5m_idx_by_sym=idx,
        bars_4h_by_sym=b4,
        bars_1d_by_sym=b1d,
        pit_meta=pit,
        vol_by_sym=vol,
        season_timeline=sea,
        btc_up_impulses=imp,
        btc_sym="BTC/USDT:USDT",
        eth_sym="ETH/USDT:USDT",
    )

    assert snap_p1["source_pool"] == "S3_POOL"
    assert snap_p2["source_pool"] == "S1_SET"
    assert snap_p1["candidate_id"] != snap_p2["candidate_id"]
    assert snap_p1["candidate_id"].startswith("cand_")

    payload_p1 = build_jev_payload(snap_p1)
    payload_p2 = build_jev_payload(snap_p2)

    # Identical signal context under S3_POOL vs S1_SET yields 100% identical blind payload
    assert payload_p1 == payload_p2
    assert tuple(payload_p1["context"].keys()) == EXPECTED_CONTEXT_TOP_KEYS
    assert payload_p1["schema_version"] == TEMPLATE_ID

    serialized = json.dumps(payload_p1, ensure_ascii=False)
    for forbidden_tok in ("source_pool", "candidate_id", "S3_POOL", "S1_SET", "cand_"):
        assert forbidden_tok not in serialized

    # Verify frozen prompt template matches PHASE3BJ_PREREG.md Appendix A.3 verbatim
    prereg_path = (
        Path(__file__).resolve().parents[2]
        / "docs"
        / "40-research"
        / "experiments"
        / "PHASE3BJ_PREREG.md"
    )
    prereg_text = prereg_path.read_text(encoding="utf-8")
    assert TEMPLATE_ID in prereg_text
    assert FROZEN_PROMPT_TEMPLATE in prereg_text
    assert len(compute_template_spec_sha256()) == 64

    leaked = copy.deepcopy(payload_p1)
    leaked["context"]["source_pool"] = "S3_POOL"
    with pytest.raises(ExAnteViolationError):
        assert_blind_payload_clean(leaked)


def test_jev_client_env_gate_and_secret_redaction_in_logs_repr_and_errors(
    caplog: pytest.LogCaptureFixture,
):
    secret_key = "jev_live_sk_SUPER_SECRET_99887766"
    base_url = f"https://user:{secret_key}@jev.internal.example/v1/judge?api_key={secret_key}"

    # Missing env vars block live mode immediately (Stage 2 gate closed)
    with pytest.raises(ValueError, match="JEV_BASE_URL"):
        JevClient(JevConfig.from_env(environ={}), mock=False)
    with pytest.raises(ValueError, match="JEV_API_KEY"):
        JevClient(
            JevConfig.from_env(environ={"JEV_BASE_URL": "https://jev.example/v1"}),
            mock=False,
        )
    with pytest.raises(ValueError, match="exceeds pre-registered cap"):
        JevClient(
            JevConfig(
                base_url="https://jev.example/v1",
                api_key=secret_key,
                max_calls=5001,
            ),
            mock=False,
        )

    cfg = JevConfig.from_env(
        environ={"JEV_BASE_URL": base_url, "JEV_API_KEY": secret_key},
        max_retries=1,
        min_interval_sec=0.0,
    )
    assert secret_key not in repr(cfg)
    assert secret_key not in str(cfg)
    assert cfg.endpoint_host_label() == "https://jev.internal.example"

    raw_leak = f"Authorization: Bearer {secret_key} | url={base_url} | JEV_API_KEY='{secret_key}'"
    scrubbed = redact_sensitive_text(raw_leak, extra_secrets=(secret_key,))
    assert secret_key not in scrubbed
    assert "[REDACTED]" in scrubbed

    # Transport that deliberately throws an error echoing full Authorization header & key
    def _leaky_failing_transport(
        url: str,
        body: bytes,
        headers: Mapping[str, str],
        timeout: float,
    ) -> tuple[int, bytes]:
        raise OSError(f"upstream rejected headers={dict(headers)} url={url}")

    sig, _, b5, idx, b4, b1d, pit, vol, sea, imp = _build_synthetic_world(future_bull=True)
    snap = build_ex_ante_snapshot_for_signal(
        sig,
        POOL_P1_S3,
        bars_5m_by_sym=b5,
        close5m_idx_by_sym=idx,
        bars_4h_by_sym=b4,
        bars_1d_by_sym=b1d,
        pit_meta=pit,
        vol_by_sym=vol,
        season_timeline=sea,
        btc_up_impulses=imp,
        btc_sym="BTC/USDT:USDT",
        eth_sym="ETH/USDT:USDT",
    )
    payload = build_jev_payload(snap)

    test_logger = logging.getLogger("test.jev.redaction")
    test_logger.handlers.clear()
    test_logger.setLevel(logging.DEBUG)
    test_logger.addFilter(RedactingLogFilter(secret_provider=lambda: (secret_key,)))

    client = JevClient(
        cfg,
        mock=False,
        transport=_leaky_failing_transport,
        sleep_fn=lambda _: None,
        logger=test_logger,
    )
    with caplog.at_level(logging.WARNING, logger="test.jev.redaction"):
        with pytest.raises(RuntimeError) as exc_info:
            client.judge_single_stateless(snap["candidate_id"], payload)

    assert secret_key not in str(exc_info.value)
    for rec in caplog.records:
        assert secret_key not in rec.getMessage()


def test_jev_client_random_order_idempotent_cache_retry_and_budget_truncation(
    tmp_path: Path,
):
    sig_base, _, b5, idx, b4, b1d, pit, vol, sea, imp = _build_synthetic_world(
        future_bull=True
    )
    snapshots: list[dict[str, Any]] = []
    priorities: dict[str, CandidatePriorityMeta] = {}
    for k in range(20):
        sig_k = CandidateSignal(
            signal_id=f"sig_{k:03d}",
            wave_cluster_id="wave_0002",
            symbol=sig_base.symbol,
            direction="UP",
            entry_ts=sig_base.entry_ts - (20 - k) * 300 if k < 19 else sig_base.entry_ts,
            entry_price=sig_base.entry_price + k * 0.01,
            stop_price=sig_base.stop_price,
            atr_1m_entry=sig_base.atr_1m_entry,
            adv_5m_entry_usd=sig_base.adv_5m_entry_usd,
        )
        pool = POOL_P1_S3 if k % 2 == 0 else POOL_P2_S1
        snap_k = build_ex_ante_snapshot_for_signal(
            sig_k,
            pool,
            bars_5m_by_sym=b5,
            close5m_idx_by_sym=idx,
            bars_4h_by_sym=b4,
            bars_1d_by_sym=b1d,
            pit_meta=pit,
            vol_by_sym=vol,
            season_timeline=sea,
            btc_up_impulses=imp,
            btc_sym="BTC/USDT:USDT",
            eth_sym="ETH/USDT:USDT",
        )
        snapshots.append(snap_k)
        # Assign tiers 1..4 across candidates
        tier = (k % 4) + 1
        priorities[snap_k["candidate_id"]] = CandidatePriorityMeta(
            candidate_id=snap_k["candidate_id"],
            priority_tier=tier,
            entry_ts=snap_k["entry_ts"],
            symbol=snap_k["symbol"],
        )

    # 1. Verify budget truncation strictly selects OOS-first tiers (Tier 1 -> Tier 2) chronologically
    selected_7, trunc_cnt = select_and_shuffle_pending(
        snapshots,
        set(),
        priority_by_id=priorities,
        max_calls=7,
        shuffle_seed=20261111,
    )
    assert len(selected_7) == 7
    assert trunc_cnt == 13
    expected_top7_ids = {
        s["candidate_id"]
        for s in sorted(
            snapshots,
            key=lambda x: (
                priorities[x["candidate_id"]].priority_tier,
                priorities[x["candidate_id"]].entry_ts,
                priorities[x["candidate_id"]].symbol,
                x["candidate_id"],
            ),
        )[:7]
    }
    assert {s["candidate_id"] for s in selected_7} == expected_top7_ids

    # 2. Verify live transport stateless submission, random order != input order, retry on 429, and idempotent cache
    call_payloads: list[dict[str, Any]] = []
    sleeps: list[float] = []
    mono_clock = [100.0]

    def _fake_mono() -> float:
        mono_clock[0] += 0.02
        return mono_clock[0]

    def _fake_sleep(dt: float) -> None:
        sleeps.append(dt)
        mono_clock[0] += dt

    attempt_counter = [0]

    def _fake_transport(
        url: str,
        body: bytes,
        headers: Mapping[str, str],
        timeout: float,
    ) -> tuple[int, bytes]:
        attempt_counter[0] += 1
        obj = json.loads(body.decode("utf-8"))
        assert_blind_payload_clean(obj)
        # Simulate transient 429 on the very first attempt
        if attempt_counter[0] == 1:
            return 429, b'{"error":"rate_limited"}'
        call_payloads.append(obj)
        ep = float(obj["context"]["trade_geometry"]["entry_price"])
        p_out = min(0.95, max(0.05, 0.50 + (ep % 0.1)))
        return 200, json.dumps({"p": round(p_out, 4)}).encode("utf-8")

    cfg = JevConfig(
        base_url="https://jev.example/v1/judge",
        api_key="jev_test_key_123456",
        timeout_sec=5.0,
        max_retries=2,
        min_interval_sec=0.12,
        max_calls=5000,
    )
    client = JevClient(
        cfg,
        mock=False,
        transport=_fake_transport,
        sleep_fn=_fake_sleep,
        monotonic_fn=_fake_mono,
    )

    cache_file = tmp_path / "live_cache.jsonl"
    manifest_file = tmp_path / "live_manifest.json"
    cache_map, manifest = client.run_batch(
        snapshots,
        cache_file,
        manifest_path=manifest_file,
        priority_by_id=priorities,
        shuffle_seed=20261111,
    )

    assert len(cache_map) == 20
    assert manifest.new_calls_completed == 20
    assert manifest.cached_hits == 0
    assert manifest.shuffle_seed == 20261111
    assert attempt_counter[0] == 21  # 20 successes + 1 retried 429
    assert len(sleeps) > 0

    # Submitted sequence of entry_price is shuffled (not sorted ascending like input `snapshots`)
    input_ep_order = [s["trade_geometry"]["entry_price"] for s in snapshots]
    submitted_ep_order = [p["context"]["trade_geometry"]["entry_price"] for p in call_payloads]
    assert submitted_ep_order != input_ep_order
    assert sorted(submitted_ep_order) == sorted(input_ep_order)

    # Rerun on existing cache -> 0 new HTTP calls!
    prev_attempts = attempt_counter[0]
    cache_map_2, manifest_2 = client.run_batch(
        snapshots,
        cache_file,
        manifest_path=manifest_file,
        priority_by_id=priorities,
        shuffle_seed=20261111,
    )
    assert attempt_counter[0] == prev_attempts
    assert manifest_2.cached_hits == 20
    assert manifest_2.new_calls_completed == 0
    assert set(cache_map_2.keys()) == set(cache_map.keys())

    # Mixing mock reader on live cache (or live reader on mock cache) raises ValueError
    with pytest.raises(ValueError, match="cache mode mismatch"):
        load_judgment_cache(cache_file, expected_mode="mock")


def test_evaluator_matches_phase3a_s3_cr1_math_and_calibration_metrics(tmp_path: Path):
    """Spot-check that `evaluate_population_judgments` matches Phase 3a S3 CR1 stats to 1e-12."""
    sym = "SOL/USDT:USDT"
    btc = "BTC/USDT:USDT"
    base_ts = 1_762_646_400

    # Build 18 separated wave clusters (> 24h apart so `G_oos = 17 >= 15`)
    bars_1m_sym: list[MinuteBar] = []
    bars_1m_btc: list[MinuteBar] = []
    bars_4h_sym: list[tuple[int, float, float, float, float, float]] = []
    bars_1d_sym: list[tuple[int, float, float, float, float, float]] = []

    for d in range(60):
        bars_1d_sym.append(
            (base_ts - (60 - d) * 86_400, 90.0 + d * 0.2, 92.0 + d * 0.2, 89.0 + d * 0.2, 91.5 + d * 0.2, 5e7)
        )
    for h in range(400):
        ts_4h = base_ts - 100 * 14_400 + h * 14_400
        bars_4h_sym.append((ts_4h, 100.0 + h * 0.05, 102.0 + h * 0.05, 99.5 + h * 0.05, 101.2 + h * 0.05, 1e7))

    tickets: list[AnnotatedTicketSignal] = []
    for g in range(18):
        cl_start = base_ts + g * 172_800  # 48h apart -> distinct `<24h` wave clusters
        specs = [
            (100.0, 101.0, 99.6, 100.4, 200_000.0)
            for _ in range(35)
        ]
        # Peak around bar 20, shallow pullback, undertaking without breakout
        specs[20] = (101.0, 103.0, 100.8, 102.5, 250_000.0)
        specs[25] = (102.0, 102.1, 101.2, 101.5, 180_000.0)
        specs[34] = (101.8, 102.4, 101.7, 102.2, 260_000.0)
        m_bars = _make_minute_bars_aligned_5m(cl_start, specs)
        bars_1m_sym.extend(m_bars)
        bars_1m_btc.extend(m_bars)

        for j in range(2):
            entry_ts = cl_start + (33 + j) * 300
            # Alternate positive vs negative net_r_wave
            net_r = (0.65 + 0.02 * g) if j == 0 else (-0.42 - 0.01 * g)
            sig = CandidateSignal(
                signal_id=f"s_{g:02d}_{j}",
                wave_cluster_id=f"wave_{g:04d}",
                symbol=sym,
                direction="UP",
                entry_ts=entry_ts,
                entry_price=102.0 if j == 0 else 103.5,  # j==1 breaks above peak -> fails C1
                stop_price=100.0,
                atr_1m_entry=0.2,
                adv_5m_entry_usd=900_000.0,
            )
            tickets.append(
                AnnotatedTicketSignal(
                    signal=sig,
                    feature_ts=entry_ts,
                    season_regime="SUMMER",
                    season_regime_oos="SUMMER",
                    breadth_fast=0.6,
                    breadth_slow=0.55,
                    volume_ratio=1.2,
                    btc_sync_mode="RESONANCE",
                    btc_impulse_lag_sec=0.0,
                    btc_near_high=True,
                    crowding_pct=0.5,
                    net_r_wave=net_r,
                    net_r_bracket=net_r * 0.8,
                    net_r_30m=net_r * 0.5,
                    stop_hit=net_r < 0,
                )
            )

    bars_by_sym = {sym: bars_1m_sym, btc: bars_1m_btc}
    bars_4h = {sym: bars_4h_sym, btc: bars_4h_sym}
    bars_1d = {sym: bars_1d_sym, btc: bars_1d_sym}

    s3_rep = evaluate_s3_point2_constraints(
        tickets,
        bars_by_sym,
        bars_4h,
        bars_1d,
    )
    all_cids = sorted({a.ticket.signal.wave_cluster_id for a in s3_rep.annotations})
    oos_cids = set(all_cids[1:])

    outcomes: list[GroundTruthTicketOutcome] = []
    fake_cache: dict[str, JudgmentCacheEntry] = {}
    for idx_a, ann in enumerate(s3_rep.annotations):
        cid = f"cand_{idx_a:016x}"
        outcomes.append(
            GroundTruthTicketOutcome(
                candidate_id=cid,
                source_pool=POOL_P1_S3,
                signal_id=ann.ticket.signal.signal_id,
                symbol=ann.ticket.signal.symbol,
                side="LONG",
                entry_ts=ann.ticket.signal.entry_ts,
                wave_cluster_id=ann.ticket.signal.wave_cluster_id,
                is_oos=ann.ticket.signal.wave_cluster_id in oos_cids,
                net_r_wave=ann.ticket.net_r_wave,
                net_r_bracket=ann.ticket.net_r_bracket,
                s3_rule_retained=ann.retained,
            )
        )
        # Set Jev p = 0.75 (> PRIMARY_TAU=0.60) iff S3 rule retained, else 0.30
        fake_cache[cid] = JudgmentCacheEntry(
            candidate_id=cid,
            p_netr_gt_0=0.75 if ann.retained else 0.30,
            mode="live",
            template_id=TEMPLATE_ID,
            payload_sha256="0" * 64,
            latency_ms=80.0,
            cached_at_utc="2026-11-11T00:00:00Z",
        )

    eval_rep = evaluate_population_judgments(
        "P1",
        POOL_P1_S3,
        "Synthetic P1 Parity Check",
        outcomes,
        fake_cache,
        mode="live",
    )

    # Exact parity with Phase 3a S3 CR1 statistics!
    assert eval_rep.pool_oos_stat.n_obs == s3_rep.all_oos_stat.n_obs
    assert eval_rep.pool_oos_stat.n_clusters == s3_rep.all_oos_stat.n_clusters
    assert math.isclose(eval_rep.pool_oos_stat.mean, s3_rep.all_oos_stat.mean, abs_tol=1e-12)
    assert math.isclose(
        eval_rep.accepted_oos_stat.mean, s3_rep.retained_oos_stat.mean, abs_tol=1e-12
    )
    assert math.isclose(
        eval_rep.diff_accepted_vs_pool_oos.diff_mean,
        s3_rep.diff_retained_vs_all_oos.diff_mean,
        abs_tol=1e-12,
    )
    assert math.isclose(
        eval_rep.diff_accepted_vs_pool_oos.one_sided_p,
        s3_rep.diff_retained_vs_all_oos.one_sided_p,
        abs_tol=1e-12,
    )
    assert eval_rep.verdict == "PASS"

    # Sensitivity check: even if tau=0.55 passes while tau=0.60 fails, primary verdict stays FAIL
    borderline_cache = {
        k: JudgmentCacheEntry(
            candidate_id=k,
            p_netr_gt_0=0.57 if v.p_netr_gt_0 >= PRIMARY_TAU else 0.30,
            mode="live",
            template_id=TEMPLATE_ID,
            payload_sha256="0" * 64,
            latency_ms=80.0,
            cached_at_utc="2026-11-11T00:00:00Z",
        )
        for k, v in fake_cache.items()
    }
    eval_fail = evaluate_population_judgments(
        "P1",
        POOL_P1_S3,
        "Borderline Tau Check",
        outcomes,
        borderline_cache,
        mode="live",
    )
    assert eval_fail.sensitivity_rows[0].tau == 0.55
    assert eval_fail.sensitivity_rows[0].diff_vs_pool_oos.diff_mean > 0.18
    assert eval_fail.verdict.startswith("FAIL")

    # Calibration math check
    cal = compute_calibration_summary([(0.8, 0.5), (0.8, -0.2), (0.2, -0.4), (0.2, 0.3)])
    expected_bs = ((0.8 - 1.0) ** 2 + (0.8 - 0.0) ** 2 + (0.2 - 0.0) ** 2 + (0.2 - 1.0) ** 2) / 4.0
    assert math.isclose(cal.brier_score, expected_bs, abs_tol=1e-12)
    assert math.isclose(cal.base_pos_rate, 0.5, abs_tol=1e-12)
    assert sum(b.n_obs for b in cal.bins) == 4

    # Mock pipeline smoke test on synthetic empty-signal DB (`enforce_anchor=False`)
    con = duckdb.connect(":memory:")
    ensure_market_bars_schema(con)
    bundle, suite_mock, snap_p, cache_p, man_p = run_phase3bj_pipeline(
        con,
        work_dir=tmp_path / "work",
        mock=True,
        enforce_anchor=False,
    )
    con.close()
    assert snap_p.is_file()
    assert cache_p.is_file()
    assert man_p.is_file()
    md_dry = format_phase3bj_evaluation_markdown(suite_mock)
    md_tpl = format_phase3bj_stage2_template_markdown(bundle)
    assert "STAGE 1 MOCK DRY-RUN" in md_dry
    assert "TEMPLATE" in md_tpl
    assert 0.25 <= mock_jev_probability("cand_0123456789abcdef") <= 0.85
