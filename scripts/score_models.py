import argparse
import json
import math
from pathlib import Path
from typing import Mapping


REQUIRED_FIELDS = {
    "co_funder",
    "co_time",
    "co_amount",
    "co_exit",
    "shared_sink",
    "pre_pump_accumulation",
    "early_cluster_share",
    "synchronized_exit",
    "shared_funder",
    "shared_sink_insider",
    "deterministic_strength",
    "cross_source_agreement",
    "temporal_stability",
}


def _read_float(payload: Mapping[str, float | int], key: str) -> float:
    if key not in payload:
        raise ValueError(f"missing required field: {key}")
    value = float(payload[key])
    if not math.isfinite(value):
        raise ValueError(f"field must be finite number: {key}")
    if value < 0.0 or value > 1.0:
        raise ValueError(f"field out of range [0,1]: {key}")
    return value


def relation_score(payload: Mapping[str, float | int]) -> float:
    return (
        0.30 * _read_float(payload, "co_funder")
        + 0.20 * _read_float(payload, "co_time")
        + 0.15 * _read_float(payload, "co_amount")
        + 0.20 * _read_float(payload, "co_exit")
        + 0.15 * _read_float(payload, "shared_sink")
    )


def insider_score(payload: Mapping[str, float | int]) -> float:
    return (
        0.25 * _read_float(payload, "pre_pump_accumulation")
        + 0.20 * _read_float(payload, "early_cluster_share")
        + 0.20 * _read_float(payload, "synchronized_exit")
        + 0.20 * _read_float(payload, "shared_funder")
        + 0.15 * _read_float(payload, "shared_sink_insider")
    )


def link_confidence(payload: Mapping[str, float | int]) -> float:
    return 100.0 * (
        0.5 * _read_float(payload, "deterministic_strength")
        + 0.3 * _read_float(payload, "cross_source_agreement")
        + 0.2 * _read_float(payload, "temporal_stability")
    )


def classify_relation(score: float) -> str:
    if score >= 0.75:
        return "high_confidence_linked_cluster"
    if score >= 0.55:
        return "suspected_linked_cluster"
    return "weak_link"


def classify_insider(score: float) -> str:
    if score >= 0.70:
        return "high_probability_insider"
    if score >= 0.50:
        return "suspected_insider"
    return "insufficient_evidence"


def classify_link_confidence(score: float) -> str:
    if score >= 75.0:
        return "high"
    if score >= 50.0:
        return "medium"
    return "low"


def build_scores(payload: Mapping[str, float | int]) -> dict[str, float | str]:
    missing = REQUIRED_FIELDS.difference(payload.keys())
    if missing:
        joined = ", ".join(sorted(missing))
        raise ValueError(f"missing required fields: {joined}")

    relation = round(relation_score(payload), 4)
    insider = round(insider_score(payload), 4)
    link = round(link_confidence(payload), 2)

    return {
        "relation_score": relation,
        "insider_score": insider,
        "link_confidence": link,
        "relation_label": classify_relation(relation),
        "insider_label": classify_insider(insider),
        "link_confidence_label": classify_link_confidence(link),
    }


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    _ = parser.add_argument("--input", required=True, type=Path)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    with args.input.open("r", encoding="utf-8") as f:
        payload_raw = json.load(f)
    if not isinstance(payload_raw, dict):
        raise ValueError("input JSON must be an object")

    payload: dict[str, float | int] = {}
    for key, value in payload_raw.items():
        if not isinstance(key, str):
            raise ValueError("input keys must be strings")
        if not isinstance(value, (int, float)):
            raise ValueError(f"field must be numeric: {key}")
        payload[key] = value

    result = build_scores(payload)
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
