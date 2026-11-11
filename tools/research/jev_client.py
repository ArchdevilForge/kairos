"""Phase 3b-J Blind Judgment Client Scaffold (`jev_client.py`).

Implements `docs/40-research/experiments/PHASE3BJ_PREREG.md` §3.2, §6 & Appendix A:
- Reads `JEV_BASE_URL` and `JEV_API_KEY` strictly from environment variables.
- Redacts API keys and `Authorization` headers across all `__repr__`, logs, and errors.
- Constructs blind judgment payloads (`kairos.phase3bj.blind_judgment_payload.v1`)
  stripping `source_pool`, `candidate_id`, ordering metadata, and blocking any
  ex-post outcome or rule-verdict fields.
- Submits single-ticket stateless requests in deterministic PRNG-shuffled order
  (`shuffle_seed` recorded in run manifest), honoring the `<= 5,000` call cap and
  pre-registered OOS-first chronological truncation rule.
- Persists idempotent per-`candidate_id` JSONL cache lines on disk so reruns never
  re-invoke already-judged candidates.
- Supports `--mock` deterministic pseudo-judgment (`HMAC-SHA256` of `candidate_id`)
  for Stage 1 end-to-end dry-run verification without calling any substitute LLM/rule.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timezone
import hashlib
import hmac
import json
import logging
import math
import os
from pathlib import Path
import random
import re
import time
from typing import Any, Callable, Mapping, Sequence
import urllib.error
import urllib.parse
import urllib.request

from clustering import ExAnteViolationError, FORBIDDEN_EX_POST_COLUMNS

ENV_JEV_BASE_URL: str = "JEV_BASE_URL"
ENV_JEV_API_KEY: str = "JEV_API_KEY"

TEMPLATE_ID: str = "kairos.phase3bj.blind_judgment_payload.v1"
MAX_TOTAL_JEV_CALLS: int = 5_000
DEFAULT_SHUFFLE_SEED: int = 20261111
MOCK_HMAC_KEY: bytes = b"kairos-phase3bj-mock-v1"

FROZEN_TARGET_DEFINITION: dict[str, Any] = {
    "event": "PolicyNetR > 0",
    "output_field": "p",
    "output_range": [0.0, 1.0],
    "settlement_model": (
        "kairos wave_k_be=0.8 full-friction 1m stop-first holding policy: "
        "max_hold=1440m; +0.8R fee-inclusive breakeven at next 1m open; "
        "causal 15m/1h/4h higher-low structure trail; bilateral 5bps taker fee "
        "+ 1.5bps(major)/5.0bps(alt) base slippage "
        "+ 0.5*(ATR_1m/P)*sqrt($10000/ADV_5m) impact slippage + realized 8h funding"
    ),
}

FROZEN_PROMPT_TEMPLATE: str = (
    "TASK: Estimate p = P(PolicyNetR > 0 | ex-ante context at entry_ts) in [0.0, 1.0].\n"
    "SETTLEMENT_MODEL: kairos wave_k_be=0.8 full-friction 1m stop-first holding policy "
    "(max_hold=1440m; +0.8R fee-inclusive breakeven at next 1m open; causal 15m/1h/4h HL "
    "structure trail; bilateral 5bps taker + 1.5/5.0bps base slippage + sqrt market impact "
    "on $10,000 notional + 8h funding).\n"
    "EX_ANTE_TICKET_CONTEXT_JSON:\n"
    "{canonical_context_json}"
)

EXPECTED_CONTEXT_TOP_KEYS: tuple[str, ...] = (
    "symbol",
    "side",
    "entry_ts",
    "entry_iso_utc",
    "trade_geometry",
    "symbol_profile",
    "cycle_map",
    "box_state",
    "divergence_state",
    "btc_state",
    "season_state",
    "resonance_state",
)

# Strictly forbidden in BOTH ex-ante snapshots and blind payloads (outcomes, future bars,
# component verdicts, rule pass/fail booleans, cluster/OOS assignments).
FORBIDDEN_SNAPSHOT_KEYS: frozenset[str] = FORBIDDEN_EX_POST_COLUMNS | frozenset(
    {
        "net_r_wave",
        "net_r_bracket",
        "net_r_30m",
        "gross_r",
        "gross_r_wave",
        "pnl",
        "return_r",
        "stop_hit",
        "future_bars",
        "future_bar",
        "win",
        "is_win",
        "outcome",
        "verdict",
        "verdict_detail",
        "retained",
        "rejected",
        "is_filtered",
        "pass",
        "passed",
        "fail",
        "failed",
        "rule_pass",
        "rule_retained",
        "s3_rule_retained",
        "c1_ok",
        "c2_ok",
        "c3_ok",
        "c4_ok",
        "c1_undertake_no_breakout",
        "c2_div_count_le_2",
        "c3_htf_main_uptrend",
        "c4_overhead_space",
        "is_box_mid_chop",
        "is_box_internal_rebound",
        "is_box_counter_short",
        "is_top2_langlang",
        "is_top2_current",
        "setup_code",
        "sub_template",
        "wave_cluster_id",
        "is_oos",
        "split_group",
    }
)

# Permitted on disk in the snapshot JSONL as local grouping/join metadata,
# MUST be stripped before constructing the blind payload sent to Jev.
STRIPPED_FROM_JEV_PAYLOAD_KEYS: frozenset[str] = frozenset(
    {
        "source_pool",
        "population",
        "pool",
        "candidate_id",
        "signal_id",
        "wake_id",
        "max_feature_ts",
        "seq",
        "sequence_index",
        "seq_id",
        "row_num",
        "row_index",
        "order",
        "order_idx",
    }
)

FORBIDDEN_PROMPT_SUBSTRINGS: tuple[str, ...] = (
    "S3_POOL",
    "S1_SET",
    "P1_S3",
    "P2_S1",
    "cand_",
    "s1_p1_",
    "s2_p4_",
    "wave_0",
    "net_r_wave",
    "RETAINED",
    "KILLED",
)

_AUTH_HEADER_RE = re.compile(
    r"(?i)(authorization\s*[:=]\s*)([^\r\n,;]+)"
)
_BEARER_RE = re.compile(
    r"(?i)\b(bearer\s+)[A-Za-z0-9._\-~+/=]+"
)
_KEY_KV_RE = re.compile(
    r"(?i)\b(x-api-key|jev_api_key|api[_-]?key|access[_-]?token|secret)\s*([:=])\s*(['\"]?)[A-Za-z0-9._\-~+/=]{4,}\3"
)
_URL_QUERY_SECRET_RE = re.compile(
    r"(?i)([?&](?:api_key|apikey|key|token|access_token|secret)=)[^&\s#]+"
)
_URL_USERINFO_RE = re.compile(r"(https?://)([^/@\s]+)@")


def redact_sensitive_text(
    text: str,
    *,
    extra_secrets: Sequence[str] = (),
) -> str:
    """Scrub API keys, Authorization/Bearer tokens, and URL credentials from `text`."""
    if not text:
        return text
    out = str(text)
    for sec in extra_secrets:
        if sec and len(sec) >= 4:
            out = out.replace(sec, "[REDACTED]")
            url_enc = urllib.parse.quote(sec, safe="")
            if url_enc and url_enc != sec:
                out = out.replace(url_enc, "[REDACTED]")
    out = _URL_USERINFO_RE.sub(r"\1[REDACTED]@", out)
    out = _URL_QUERY_SECRET_RE.sub(r"\1[REDACTED]", out)
    out = _AUTH_HEADER_RE.sub(r"\1[REDACTED]", out)
    out = _BEARER_RE.sub(r"\1[REDACTED]", out)
    out = _KEY_KV_RE.sub(r"\1\2[REDACTED]", out)
    return out


class RedactingLogFilter(logging.Filter):
    """Logging filter that scrubs secrets and request headers from every log record."""

    def __init__(self, secret_provider: Callable[[], Sequence[str]] | None = None) -> None:
        super().__init__()
        self._secret_provider = secret_provider

    def filter(self, record: logging.LogRecord) -> bool:
        secrets = tuple(self._secret_provider()) if self._secret_provider is not None else ()
        try:
            rendered = record.getMessage()
        except Exception:
            rendered = str(record.msg)
        record.msg = redact_sensitive_text(rendered, extra_secrets=secrets)
        record.args = ()
        if record.exc_info and record.exc_info[1] is not None:
            # Ensure exception text is also sanitized if formatted downstream
            exc = record.exc_info[1]
            safe_exc_msg = redact_sensitive_text(str(exc), extra_secrets=secrets)
            if safe_exc_msg != str(exc):
                record.exc_info = (
                    record.exc_info[0],
                    RuntimeError(safe_exc_msg),
                    None,
                )
        return True


def _build_default_logger() -> logging.Logger:
    lg = logging.getLogger("kairos.research.jev_client")
    if not any(isinstance(f, RedactingLogFilter) for f in lg.filters):
        lg.addFilter(
            RedactingLogFilter(
                secret_provider=lambda: (
                    tuple(
                        v
                        for v in (os.environ.get(ENV_JEV_API_KEY, ""),)
                        if v
                    )
                )
            )
        )
    return lg


LOGGER = _build_default_logger()


@dataclass(frozen=True)
class JevConfig:
    """Runtime configuration read exclusively from environment variables."""

    base_url: str
    api_key: str
    timeout_sec: float = 10.0
    max_retries: int = 3
    min_interval_sec: float = 0.12
    max_calls: int = MAX_TOTAL_JEV_CALLS

    @classmethod
    def from_env(
        cls,
        *,
        environ: Mapping[str, str] | None = None,
        timeout_sec: float = 10.0,
        max_retries: int = 3,
        min_interval_sec: float = 0.12,
        max_calls: int = MAX_TOTAL_JEV_CALLS,
    ) -> JevConfig:
        env = environ if environ is not None else os.environ
        base_url = str(env.get(ENV_JEV_BASE_URL, "")).strip()
        api_key = str(env.get(ENV_JEV_API_KEY, "")).strip()
        return cls(
            base_url=base_url,
            api_key=api_key,
            timeout_sec=float(timeout_sec),
            max_retries=int(max_retries),
            min_interval_sec=float(min_interval_sec),
            max_calls=int(max_calls),
        )

    def endpoint_host_label(self) -> str:
        """Return sanitized `scheme://host[:port]` without path/query/userinfo/secrets."""
        if not self.base_url:
            return "unconfigured"
        parsed = urllib.parse.urlsplit(self.base_url)
        host = parsed.hostname or "invalid-host"
        port_part = f":{parsed.port}" if parsed.port else ""
        scheme = parsed.scheme or "https"
        return f"{scheme}://{host}{port_part}"

    def __repr__(self) -> str:
        masked_key = "[REDACTED]" if self.api_key else "<empty>"
        return (
            f"JevConfig(endpoint={self.endpoint_host_label()!r}, "
            f"api_key={masked_key!r}, timeout_sec={self.timeout_sec}, "
            f"max_retries={self.max_retries}, min_interval_sec={self.min_interval_sec}, "
            f"max_calls={self.max_calls})"
        )

    __str__ = __repr__


@dataclass(frozen=True)
class CandidatePriorityMeta:
    """Ex-ante scheduling priority metadata used ONLY for `<= 5,000` call-cap truncation.

    Never included in the blind payload sent to Jev.
    Priority tiers (frozen in `PHASE3BJ_PREREG.md` §3.2):
      1 = P1 (`S3_POOL`) Walk-Forward OOS
      2 = P2 (`S1_SET`) Walk-Forward OOS
      3 = P1 (`S3_POOL`) IS-only warmup
      4 = P2 (`S1_SET`) IS-only warmup
    """

    candidate_id: str
    priority_tier: int
    entry_ts: int
    symbol: str


@dataclass(frozen=True)
class JudgmentCacheEntry:
    """Single cached Jev judgment line keyed by `candidate_id`."""

    candidate_id: str
    p_netr_gt_0: float
    mode: str  # "mock" | "live"
    template_id: str
    payload_sha256: str
    latency_ms: float
    cached_at_utc: str

    def to_dict(self) -> dict[str, Any]:
        return {
            "candidate_id": self.candidate_id,
            "p_netr_gt_0": round(float(self.p_netr_gt_0), 6),
            "mode": self.mode,
            "template_id": self.template_id,
            "payload_sha256": self.payload_sha256,
            "latency_ms": round(float(self.latency_ms), 3),
            "cached_at_utc": self.cached_at_utc,
        }

    @classmethod
    def from_dict(cls, raw: Mapping[str, Any]) -> JudgmentCacheEntry:
        p_val = float(raw["p_netr_gt_0"])
        if math.isnan(p_val) or not (0.0 <= p_val <= 1.0):
            raise ValueError(f"invalid cached probability p_netr_gt_0={p_val!r}")
        mode = str(raw["mode"])
        if mode not in ("mock", "live"):
            raise ValueError(f"invalid judgment cache mode={mode!r}")
        return cls(
            candidate_id=str(raw["candidate_id"]),
            p_netr_gt_0=p_val,
            mode=mode,
            template_id=str(raw.get("template_id", TEMPLATE_ID)),
            payload_sha256=str(raw["payload_sha256"]),
            latency_ms=float(raw.get("latency_ms", 0.0)),
            cached_at_utc=str(raw.get("cached_at_utc", "")),
        )


@dataclass(frozen=True)
class JevRunManifest:
    """Audit manifest written alongside a judgment cache run (zero secrets)."""

    mode: str
    template_id: str
    template_spec_sha256: str
    shuffle_seed: int
    max_calls: int
    total_candidates_input: int
    cached_hits: int
    selected_for_call: int
    new_calls_completed: int
    truncated_by_budget: int
    submitted_order_sha256: str
    endpoint_host: str
    created_at_utc: str

    def to_dict(self) -> dict[str, Any]:
        return {
            "mode": self.mode,
            "template_id": self.template_id,
            "template_spec_sha256": self.template_spec_sha256,
            "shuffle_seed": self.shuffle_seed,
            "max_calls": self.max_calls,
            "total_candidates_input": self.total_candidates_input,
            "cached_hits": self.cached_hits,
            "selected_for_call": self.selected_for_call,
            "new_calls_completed": self.new_calls_completed,
            "truncated_by_budget": self.truncated_by_budget,
            "submitted_order_sha256": self.submitted_order_sha256,
            "endpoint_host": self.endpoint_host,
            "created_at_utc": self.created_at_utc,
        }


def compute_template_spec_sha256() -> str:
    """Compute SHA-256 fingerprint of the frozen Appendix A payload specification."""
    spec_doc = json.dumps(
        {
            "template_id": TEMPLATE_ID,
            "target_definition": FROZEN_TARGET_DEFINITION,
            "prompt_template": FROZEN_PROMPT_TEMPLATE,
            "expected_context_top_keys": list(EXPECTED_CONTEXT_TOP_KEYS),
        },
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
    )
    return hashlib.sha256(spec_doc.encode("utf-8")).hexdigest()


def _is_forbidden_key_pattern(norm_key: str) -> bool:
    if norm_key in FORBIDDEN_SNAPSHOT_KEYS:
        return True
    bad_prefixes = (
        "net_r",
        "gross_r",
        "realized_",
        "future_",
        "exit_",
        "c1_",
        "c2_",
        "c3_",
        "c4_",
        "is_top2",
    )
    bad_suffixes = (
        "_verdict",
        "_pass",
        "_passed",
        "_fail",
        "_failed",
        "_retained",
        "_rejected",
        "_filtered",
    )
    if norm_key.startswith(bad_prefixes) or norm_key.endswith(bad_suffixes):
        return True
    if ("hold" in norm_key and "dur" in norm_key) or ("hold" in norm_key and "sec" in norm_key):
        return True
    return False


def _walk_keys(obj: Any, path: str = "$") -> list[tuple[str, str]]:
    found: list[tuple[str, str]] = []
    if isinstance(obj, Mapping):
        for k, v in obj.items():
            ks = str(k)
            cur = f"{path}.{ks}"
            found.append((ks, cur))
            found.extend(_walk_keys(v, cur))
    elif isinstance(obj, (list, tuple)):
        for idx, item in enumerate(obj):
            found.extend(_walk_keys(item, f"{path}[{idx}]"))
    return found


def assert_snapshot_ex_ante_clean(snapshot: Mapping[str, Any]) -> None:
    """Verify an ex-ante snapshot contains zero post-entry outcomes or rule verdict keys."""
    for raw_key, key_path in _walk_keys(snapshot):
        norm = raw_key.strip().lower()
        if _is_forbidden_key_pattern(norm):
            raise ExAnteViolationError(
                f"forbidden post-entry / rule-verdict field {raw_key!r} at {key_path}"
            )
    if "entry_ts" in snapshot and "max_feature_ts" in snapshot:
        if float(snapshot["max_feature_ts"]) > float(snapshot["entry_ts"]):
            raise ExAnteViolationError(
                f"snapshot max_feature_ts ({snapshot['max_feature_ts']}) > entry_ts ({snapshot['entry_ts']})"
            )


def assert_blind_payload_clean(payload: Mapping[str, Any]) -> None:
    """Verify a Jev request payload is 100% stripped of grouping, order, and outcome leakage."""
    assert_snapshot_ex_ante_clean(payload)
    for raw_key, key_path in _walk_keys(payload):
        norm = raw_key.strip().lower()
        if norm in STRIPPED_FROM_JEV_PAYLOAD_KEYS:
            raise ExAnteViolationError(
                f"blind-protocol violation: grouping/order metadata {raw_key!r} leaked at {key_path}"
            )
    serialized = json.dumps(payload, ensure_ascii=False, separators=(",", ":"))
    for tok in FORBIDDEN_PROMPT_SUBSTRINGS:
        if tok in serialized:
            raise ExAnteViolationError(
                f"blind-protocol violation: forbidden token {tok!r} present in serialized Jev payload"
            )


def build_jev_payload(snapshot: Mapping[str, Any]) -> dict[str, Any]:
    """Transform an §B ex-ante snapshot into the frozen Appendix A blind judgment payload.

    Strips `source_pool`, `candidate_id`, `max_feature_ts`, and any ordering fields.
    """
    assert_snapshot_ex_ante_clean(snapshot)

    missing = [k for k in EXPECTED_CONTEXT_TOP_KEYS if k not in snapshot]
    if missing:
        raise ValueError(f"snapshot missing required Appendix A context keys: {missing}")

    # Preserve exact Appendix A key ordering while deep-copying nested JSON values
    context: dict[str, Any] = {}
    for key in EXPECTED_CONTEXT_TOP_KEYS:
        context[key] = json.loads(json.dumps(snapshot[key]))

    canonical_context_json = json.dumps(
        context,
        ensure_ascii=False,
        separators=(",", ":"),
    )
    prompt_text = FROZEN_PROMPT_TEMPLATE.format(
        canonical_context_json=canonical_context_json
    )

    payload: dict[str, Any] = {
        "schema_version": TEMPLATE_ID,
        "task": "estimate_probability_net_r_gt_zero",
        "target_definition": json.loads(json.dumps(FROZEN_TARGET_DEFINITION)),
        "context": context,
        "prompt": prompt_text,
    }
    assert_blind_payload_clean(payload)
    return payload


def compute_payload_sha256(payload: Mapping[str, Any]) -> str:
    canonical = json.dumps(payload, ensure_ascii=False, separators=(",", ":"))
    return hashlib.sha256(canonical.encode("utf-8")).hexdigest()


def mock_jev_probability(candidate_id: str) -> float:
    """Deterministic `--mock` pseudo-probability derived solely from `HMAC-SHA256(candidate_id)`.

    Maps uniformly into `[0.25, 0.85]` so both `p >= 0.60` and `p < 0.60` branches
    and all 5 calibration bins are exercised in Stage 1 dry-runs without invoking
    any substitute model or rule heuristic.
    """
    digest = hmac.new(
        MOCK_HMAC_KEY,
        str(candidate_id).encode("utf-8"),
        hashlib.sha256,
    ).digest()
    u64 = int.from_bytes(digest[:8], byteorder="big", signed=False)
    frac = u64 / float(1 << 64)
    return round(0.25 + 0.60 * frac, 6)


def load_judgment_cache(
    cache_path: str | Path,
    *,
    expected_mode: str | None = None,
) -> dict[str, JudgmentCacheEntry]:
    """Load cached JSONL judgments keyed by `candidate_id`.

    Raises `ValueError` if `expected_mode` is specified and any cached line was
    generated under a different mode (preventing Stage 1 `mock` cache from ever
    contaminating Stage 2 `live` evaluation).
    """
    p = Path(cache_path)
    if not p.is_file():
        return {}
    out: dict[str, JudgmentCacheEntry] = {}
    for line_no, raw_line in enumerate(p.read_text(encoding="utf-8").splitlines(), start=1):
        line = raw_line.strip()
        if not line:
            continue
        obj = json.loads(line)
        entry = JudgmentCacheEntry.from_dict(obj)
        if expected_mode is not None and entry.mode != expected_mode:
            raise ValueError(
                f"cache mode mismatch at {p}:{line_no}: found mode={entry.mode!r}, "
                f"expected mode={expected_mode!r} (refusing to mix mock and live caches)"
            )
        out[entry.candidate_id] = entry
    return out


def select_and_shuffle_pending(
    snapshots: Sequence[Mapping[str, Any]],
    cached_ids: frozenset[str] | set[str],
    *,
    priority_by_id: Mapping[str, CandidatePriorityMeta] | None = None,
    max_calls: int = MAX_TOTAL_JEV_CALLS,
    shuffle_seed: int = DEFAULT_SHUFFLE_SEED,
) -> tuple[list[Mapping[str, Any]], int]:
    """Apply pre-registered `<= 5,000` call truncation (if needed) then PRNG-shuffle order.

    Returns `(shuffled_pending_snapshots, truncated_count)`.
    """
    if max_calls < 0:
        raise ValueError(f"max_calls must be >= 0, got {max_calls}")

    seen_in_batch: set[str] = set()
    uncached: list[Mapping[str, Any]] = []
    for snap in snapshots:
        cid = str(snap["candidate_id"])
        if cid in cached_ids or cid in seen_in_batch:
            continue
        seen_in_batch.add(cid)
        uncached.append(snap)

    def _truncation_key(snap: Mapping[str, Any]) -> tuple[int, int, str, str]:
        cid = str(snap["candidate_id"])
        if priority_by_id is not None and cid in priority_by_id:
            meta = priority_by_id[cid]
            return (meta.priority_tier, meta.entry_ts, meta.symbol, cid)
        pool = str(snap.get("source_pool", ""))
        default_tier = 1 if pool == "S3_POOL" else 2
        return (
            default_tier,
            int(snap["entry_ts"]),
            str(snap["symbol"]),
            cid,
        )

    truncated_count = 0
    if len(uncached) > max_calls:
        ordered_by_priority = sorted(uncached, key=_truncation_key)
        selected = ordered_by_priority[:max_calls]
        truncated_count = len(uncached) - len(selected)
    else:
        selected = list(uncached)

    # Canonicalize pre-shuffle order by opaque `candidate_id` so shuffle depends ONLY
    # on the candidate set and `shuffle_seed` (not input file order).
    selected.sort(key=lambda s: str(s["candidate_id"]))
    rng = random.Random(int(shuffle_seed))
    rng.shuffle(selected)
    return selected, truncated_count


def _extract_probability_from_response(resp_obj: Any) -> float:
    """Extract `p in [0, 1]` from Jev JSON response."""
    if isinstance(resp_obj, Mapping):
        for key in ("p", "p_netr_gt_0", "probability"):
            if key in resp_obj and resp_obj[key] is not None:
                val = float(resp_obj[key])
                if math.isnan(val) or not (0.0 <= val <= 1.0):
                    raise ValueError(f"Jev returned out-of-range probability {key}={val!r}")
                return val
        if "data" in resp_obj and isinstance(resp_obj["data"], Mapping):
            return _extract_probability_from_response(resp_obj["data"])
    raise ValueError(
        "Jev response JSON missing valid probability field ('p' / 'p_netr_gt_0' / 'probability')"
    )


HttpTransportFn = Callable[[str, bytes, Mapping[str, str], float], tuple[int, bytes]]


def _default_urllib_transport(
    url: str,
    body_bytes: bytes,
    headers: Mapping[str, str],
    timeout_sec: float,
) -> tuple[int, bytes]:
    req = urllib.request.Request(
        url=url,
        data=body_bytes,
        headers=dict(headers),
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=timeout_sec) as resp:
        status = int(getattr(resp, "status", 200))
        raw = resp.read()
        return status, raw


class JevClient:
    """Stateless, rate-limited, idempotent-caching client for Jev blind ticket judgment."""

    def __init__(
        self,
        config: JevConfig | None = None,
        *,
        mock: bool = False,
        transport: HttpTransportFn | None = None,
        sleep_fn: Callable[[float], None] = time.sleep,
        monotonic_fn: Callable[[], float] = time.monotonic,
        logger: logging.Logger | None = None,
    ) -> None:
        self._config = config if config is not None else JevConfig.from_env()
        self._mock = bool(mock)
        self._transport = transport if transport is not None else _default_urllib_transport
        self._sleep = sleep_fn
        self._monotonic = monotonic_fn
        self._last_call_mono: float | None = None
        self._logger = logger if logger is not None else LOGGER
        if not any(isinstance(f, RedactingLogFilter) for f in self._logger.filters):
            self._logger.addFilter(
                RedactingLogFilter(secret_provider=self._secrets_tuple)
            )

        if not self._mock:
            if not self._config.base_url:
                raise ValueError(
                    f"live Jev mode requires {ENV_JEV_BASE_URL} in environment "
                    "(Stage 2 gate closed; use --mock for Stage 1 dry-run)"
                )
            if not self._config.api_key:
                raise ValueError(
                    f"live Jev mode requires {ENV_JEV_API_KEY} in environment "
                    "(Stage 2 gate closed; use --mock for Stage 1 dry-run)"
                )
            if self._config.max_calls > MAX_TOTAL_JEV_CALLS:
                raise ValueError(
                    f"max_calls ({self._config.max_calls}) exceeds pre-registered cap "
                    f"({MAX_TOTAL_JEV_CALLS})"
                )

    def _secrets_tuple(self) -> tuple[str, ...]:
        if self._config.api_key:
            return (self._config.api_key,)
        return ()

    @property
    def mode(self) -> str:
        return "mock" if self._mock else "live"

    def _respect_polite_interval(self) -> None:
        min_gap = max(0.0, float(self._config.min_interval_sec))
        if min_gap <= 0.0:
            self._last_call_mono = self._monotonic()
            return
        now = self._monotonic()
        if self._last_call_mono is not None:
            wait = (self._last_call_mono + min_gap) - now
            if wait > 0:
                self._sleep(wait)
                now = self._monotonic()
        self._last_call_mono = now

    def judge_single_stateless(
        self,
        candidate_id: str,
        payload: Mapping[str, Any],
    ) -> JudgmentCacheEntry:
        """Submit one blind payload statelessly (or compute deterministic mock hash)."""
        assert_blind_payload_clean(payload)
        payload_sha = compute_payload_sha256(payload)
        now_iso = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

        if self._mock:
            t0 = self._monotonic()
            p_val = mock_jev_probability(candidate_id)
            dt_ms = max(0.0, (self._monotonic() - t0) * 1000.0)
            return JudgmentCacheEntry(
                candidate_id=str(candidate_id),
                p_netr_gt_0=p_val,
                mode="mock",
                template_id=TEMPLATE_ID,
                payload_sha256=payload_sha,
                latency_ms=dt_ms,
                cached_at_utc=now_iso,
            )

        body_bytes = json.dumps(
            payload,
            ensure_ascii=False,
            separators=(",", ":"),
        ).encode("utf-8")
        headers = {
            "Content-Type": "application/json; charset=utf-8",
            "Accept": "application/json",
            "Authorization": f"Bearer {self._config.api_key}",
            "User-Agent": "kairos-research-phase3bj/1.0",
        }

        max_attempts = max(1, int(self._config.max_retries) + 1)
        last_err_msg = "unknown error"

        for attempt in range(max_attempts):
            self._respect_polite_interval()
            t0 = self._monotonic()
            try:
                status, resp_bytes = self._transport(
                    self._config.base_url,
                    body_bytes,
                    headers,
                    float(self._config.timeout_sec),
                )
                dt_ms = max(0.0, (self._monotonic() - t0) * 1000.0)
                if status in (429, 500, 502, 503, 504) and attempt + 1 < max_attempts:
                    backoff = 0.4 * (2**attempt)
                    self._logger.warning(
                        "Jev transient HTTP %d on attempt %d/%d (endpoint=%s); retrying in %.2fs",
                        status,
                        attempt + 1,
                        max_attempts,
                        self._config.endpoint_host_label(),
                        backoff,
                    )
                    self._sleep(backoff)
                    continue
                if status < 200 or status >= 300:
                    raise RuntimeError(
                        f"Jev HTTP status {status} from {self._config.endpoint_host_label()}"
                    )
                decoded = json.loads(resp_bytes.decode("utf-8"))
                p_val = _extract_probability_from_response(decoded)
                return JudgmentCacheEntry(
                    candidate_id=str(candidate_id),
                    p_netr_gt_0=p_val,
                    mode="live",
                    template_id=TEMPLATE_ID,
                    payload_sha256=payload_sha,
                    latency_ms=dt_ms,
                    cached_at_utc=datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
                )
            except urllib.error.HTTPError as exc:
                safe_msg = redact_sensitive_text(
                    f"HTTP {exc.code}: {exc.reason}",
                    extra_secrets=self._secrets_tuple(),
                )
                last_err_msg = safe_msg
                if exc.code in (429, 500, 502, 503, 504) and attempt + 1 < max_attempts:
                    backoff = 0.4 * (2**attempt)
                    self._logger.warning(
                        "Jev HTTPError (%s) attempt %d/%d; retrying in %.2fs",
                        safe_msg,
                        attempt + 1,
                        max_attempts,
                        backoff,
                    )
                    self._sleep(backoff)
                    continue
                break
            except (urllib.error.URLError, TimeoutError, OSError) as exc:
                safe_msg = redact_sensitive_text(
                    str(exc),
                    extra_secrets=self._secrets_tuple(),
                )
                last_err_msg = safe_msg
                if attempt + 1 < max_attempts:
                    backoff = 0.4 * (2**attempt)
                    self._logger.warning(
                        "Jev network/timeout error (%s) attempt %d/%d; retrying in %.2fs",
                        safe_msg,
                        attempt + 1,
                        max_attempts,
                        backoff,
                    )
                    self._sleep(backoff)
                    continue
                break
            except Exception as exc:
                safe_msg = redact_sensitive_text(
                    str(exc),
                    extra_secrets=self._secrets_tuple(),
                )
                raise RuntimeError(f"Jev invocation failed: {safe_msg}") from None

        raise RuntimeError(
            redact_sensitive_text(
                f"Jev invocation exhausted {max_attempts} attempts "
                f"(endpoint={self._config.endpoint_host_label()}): {last_err_msg}",
                extra_secrets=self._secrets_tuple(),
            )
        )

    def run_batch(
        self,
        snapshots: Sequence[Mapping[str, Any]],
        cache_path: str | Path,
        *,
        manifest_path: str | Path | None = None,
        priority_by_id: Mapping[str, CandidatePriorityMeta] | None = None,
        shuffle_seed: int = DEFAULT_SHUFFLE_SEED,
    ) -> tuple[dict[str, JudgmentCacheEntry], JevRunManifest]:
        """Judge uncached candidates in shuffled order, caching each result immediately."""
        cpath = Path(cache_path)
        cpath.parent.mkdir(parents=True, exist_ok=True)
        cpath.touch(exist_ok=True)

        cache_map = load_judgment_cache(cpath, expected_mode=self.mode)
        cached_hits = sum(
            1 for s in snapshots if str(s["candidate_id"]) in cache_map
        )

        effective_cap = min(int(self._config.max_calls), MAX_TOTAL_JEV_CALLS)
        pending, truncated_count = select_and_shuffle_pending(
            snapshots,
            set(cache_map.keys()),
            priority_by_id=priority_by_id,
            max_calls=effective_cap,
            shuffle_seed=shuffle_seed,
        )

        submitted_ids: list[str] = []
        new_completed = 0

        if pending:
            with cpath.open("a", encoding="utf-8") as fh:
                for snap in pending:
                    cid = str(snap["candidate_id"])
                    payload = build_jev_payload(snap)
                    entry = self.judge_single_stateless(cid, payload)
                    fh.write(
                        json.dumps(
                            entry.to_dict(),
                            ensure_ascii=False,
                            separators=(",", ":"),
                        )
                        + "\n"
                    )
                    fh.flush()
                    cache_map[cid] = entry
                    submitted_ids.append(cid)
                    new_completed += 1

        order_digest = hashlib.sha256(
            "\n".join(submitted_ids).encode("utf-8")
        ).hexdigest()
        manifest = JevRunManifest(
            mode=self.mode,
            template_id=TEMPLATE_ID,
            template_spec_sha256=compute_template_spec_sha256(),
            shuffle_seed=int(shuffle_seed),
            max_calls=effective_cap,
            total_candidates_input=len(snapshots),
            cached_hits=cached_hits,
            selected_for_call=len(pending),
            new_calls_completed=new_completed,
            truncated_by_budget=truncated_count,
            submitted_order_sha256=order_digest,
            endpoint_host="mock://local-hmac-sha256"
            if self._mock
            else self._config.endpoint_host_label(),
            created_at_utc=datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        )
        if manifest_path is not None:
            mpath = Path(manifest_path)
            mpath.parent.mkdir(parents=True, exist_ok=True)
            mpath.write_text(
                json.dumps(manifest.to_dict(), indent=2, ensure_ascii=False) + "\n",
                encoding="utf-8",
            )
        self._logger.info(
            "Jev batch complete: mode=%s total=%d cached=%d new=%d truncated=%d",
            self.mode,
            len(snapshots),
            cached_hits,
            new_completed,
            truncated_count,
        )
        return cache_map, manifest
