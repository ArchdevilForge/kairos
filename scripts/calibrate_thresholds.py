import argparse
import json
from itertools import product
from pathlib import Path
from typing import Mapping, TypedDict


LP_BUCKETS = ("lp_lt_20k", "lp_20k_100k", "lp_gt_100k")


class ScoreRecord(TypedDict):
    chain: str
    lp_usd: float
    label: int
    relation_score: float
    insider_score: float
    link_confidence: float


class Metrics(TypedDict):
    tp: int
    fp: int
    tn: int
    fn: int
    fpr: float
    fnr: float
    precision: float
    recall: float
    loss: float


class Thresholds(TypedDict):
    relation_t: float
    insider_t: float
    link_conf_t: float


class BucketCalibration(TypedDict):
    thresholds: Thresholds
    metrics: Metrics
    sample_size: int


def bucket_key(chain: str, lp_usd: float) -> str:
    chain_name = str(chain)
    lp = float(lp_usd)
    if lp < 20000:
        return f"{chain_name}:lp_lt_20k"
    if lp <= 100000:
        return f"{chain_name}:lp_20k_100k"
    return f"{chain_name}:lp_gt_100k"


def _evaluate(
    records: list[ScoreRecord], relation_t: float, insider_t: float, link_conf_t: float
) -> Metrics:
    tp = fp = tn = fn = 0
    for row in records:
        label = int(row["label"])
        pred = (
            float(row["relation_score"]) >= relation_t
            and float(row["insider_score"]) >= insider_t
            and float(row["link_confidence"]) >= link_conf_t
        )
        if pred and label == 1:
            tp += 1
        elif pred and label == 0:
            fp += 1
        elif (not pred) and label == 0:
            tn += 1
        else:
            fn += 1

    pos = tp + fn
    neg = tn + fp
    fpr = fp / neg if neg else 0.0
    fnr = fn / pos if pos else 0.0
    precision = tp / (tp + fp) if (tp + fp) else 0.0
    recall = tp / pos if pos else 0.0
    score = (2.5 * fpr) + fnr
    return {
        "tp": tp,
        "fp": fp,
        "tn": tn,
        "fn": fn,
        "fpr": round(fpr, 4),
        "fnr": round(fnr, 4),
        "precision": round(precision, 4),
        "recall": round(recall, 4),
        "loss": round(score, 4),
    }


def _candidate_list(
    values: list[float], floor: float, cap: float, step: float
) -> list[float]:
    cands = {round(v, 4) for v in values if floor <= v <= cap}
    cur = floor
    while cur <= cap + 1e-12:
        cands.add(round(cur, 4))
        cur += step
    return sorted(cands)


def _default_thresholds() -> Thresholds:
    return {"relation_t": 0.75, "insider_t": 0.70, "link_conf_t": 75.0}


def _default_metrics() -> Metrics:
    return {
        "tp": 0,
        "fp": 0,
        "tn": 0,
        "fn": 0,
        "fpr": 0.0,
        "fnr": 0.0,
        "precision": 0.0,
        "recall": 0.0,
        "loss": 0.0,
    }


def _calibrate_bucket(records: list[ScoreRecord]) -> tuple[Thresholds, Metrics]:
    relation_values = [float(r["relation_score"]) for r in records]
    insider_values = [float(r["insider_score"]) for r in records]
    link_values = [float(r["link_confidence"]) for r in records]

    relation_cands = _candidate_list(relation_values, 0.55, 0.95, 0.05)
    insider_cands = _candidate_list(insider_values, 0.50, 0.95, 0.05)
    link_cands = _candidate_list(link_values, 60.0, 95.0, 5.0)

    best: tuple[Thresholds, Metrics] | None = None
    for relation_t, insider_t, link_conf_t in product(
        relation_cands, insider_cands, link_cands
    ):
        metrics = _evaluate(records, relation_t, insider_t, link_conf_t)
        thresholds: Thresholds = {
            "relation_t": round(relation_t, 4),
            "insider_t": round(insider_t, 4),
            "link_conf_t": round(link_conf_t, 2),
        }
        candidate = (thresholds, metrics)
        if best is None:
            best = candidate
            continue

        left = (candidate[1]["loss"], -candidate[1]["recall"])
        right = (best[1]["loss"], -best[1]["recall"])
        if left < right:
            best = candidate

    if best is None:
        return (_default_thresholds(), _default_metrics())
    return best


def calibrate_thresholds(records: list[ScoreRecord]) -> dict[str, BucketCalibration]:
    buckets: dict[str, list[ScoreRecord]] = {}
    for row in records:
        label = int(row["label"])
        if label not in (0, 1):
            raise ValueError(f"record label must be 0 or 1, got: {label}")
        key = bucket_key(row["chain"], row["lp_usd"])
        buckets.setdefault(key, []).append(row)

    calibrated: dict[str, BucketCalibration] = {}
    for key, rows in buckets.items():
        thresholds, metrics = _calibrate_bucket(rows)
        calibrated[key] = {
            "thresholds": thresholds,
            "metrics": metrics,
            "sample_size": len(rows),
        }
    return calibrated


def _require_number(row: Mapping[str, object], key: str) -> float:
    value = row.get(key)
    if not isinstance(value, (int, float)):
        raise ValueError(f"record field must be numeric: {key}")
    return float(value)


def _normalize_record(row: Mapping[str, object]) -> ScoreRecord:
    chain = row.get("chain")
    if not isinstance(chain, str) or chain.strip() == "":
        raise ValueError("record field must be non-empty string: chain")

    label = _require_number(row, "label")
    if label not in (0.0, 1.0):
        raise ValueError(f"record field label must be 0 or 1: {label}")

    return {
        "chain": chain,
        "lp_usd": _require_number(row, "lp_usd"),
        "label": int(label),
        "relation_score": _require_number(row, "relation_score"),
        "insider_score": _require_number(row, "insider_score"),
        "link_confidence": _require_number(row, "link_confidence"),
    }


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    _ = parser.add_argument("--input", required=True, type=Path)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    with args.input.open("r", encoding="utf-8") as f:
        payload = json.load(f)

    raw_records: list[object]
    if isinstance(payload, list):
        raw_records = payload
    elif isinstance(payload, dict):
        data = payload.get("records", [])
        raw_records = data if isinstance(data, list) else []
    else:
        raise ValueError("input JSON must be a list or an object with records[]")

    records: list[ScoreRecord] = []
    for item in raw_records:
        if isinstance(item, dict):
            records.append(_normalize_record(item))

    result: dict[str, dict[str, BucketCalibration]] = {
        "buckets": calibrate_thresholds(records)
    }
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
