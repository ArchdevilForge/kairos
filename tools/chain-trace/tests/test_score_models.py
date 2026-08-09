import importlib.util
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]

spec = importlib.util.spec_from_file_location(
    "score_models", ROOT / "scripts" / "score_models.py"
)
if spec is None or spec.loader is None:
    raise RuntimeError("unable to load scripts/score_models.py")

score_models = importlib.util.module_from_spec(spec)
spec.loader.exec_module(score_models)

build_scores = score_models.build_scores
classify_insider = score_models.classify_insider
classify_link_confidence = score_models.classify_link_confidence
classify_relation = score_models.classify_relation


class ScoreModelsTests(unittest.TestCase):
    def test_build_scores_matches_document_formula(self):
        sample = {
            "co_funder": 0.9,
            "co_time": 0.8,
            "co_amount": 0.6,
            "co_exit": 0.7,
            "shared_sink": 0.8,
            "pre_pump_accumulation": 0.9,
            "early_cluster_share": 0.7,
            "synchronized_exit": 0.6,
            "shared_funder": 0.9,
            "shared_sink_insider": 0.8,
            "deterministic_strength": 0.8,
            "cross_source_agreement": 0.7,
            "temporal_stability": 0.9,
        }

        scores = build_scores(sample)

        self.assertAlmostEqual(scores["relation_score"], 0.78, places=4)
        self.assertAlmostEqual(scores["insider_score"], 0.785, places=4)
        self.assertAlmostEqual(scores["link_confidence"], 79.0, places=2)

    def test_score_classification_thresholds(self):
        self.assertEqual(classify_relation(0.8), "high_confidence_linked_cluster")
        self.assertEqual(classify_relation(0.6), "suspected_linked_cluster")
        self.assertEqual(classify_relation(0.4), "weak_link")

        self.assertEqual(classify_insider(0.75), "high_probability_insider")
        self.assertEqual(classify_insider(0.6), "suspected_insider")
        self.assertEqual(classify_insider(0.2), "insufficient_evidence")

        self.assertEqual(classify_link_confidence(80.0), "high")
        self.assertEqual(classify_link_confidence(60.0), "medium")
        self.assertEqual(classify_link_confidence(30.0), "low")

    def test_cli_reads_json_and_prints_result(self):
        payload = {
            "co_funder": 0.9,
            "co_time": 0.8,
            "co_amount": 0.6,
            "co_exit": 0.7,
            "shared_sink": 0.8,
            "pre_pump_accumulation": 0.9,
            "early_cluster_share": 0.7,
            "synchronized_exit": 0.6,
            "shared_funder": 0.9,
            "shared_sink_insider": 0.8,
            "deterministic_strength": 0.8,
            "cross_source_agreement": 0.7,
            "temporal_stability": 0.9,
        }

        with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as f:
            json.dump(payload, f)
            tmp_path = f.name

        result = subprocess.run(
            [
                sys.executable,
                str(ROOT / "scripts" / "score_models.py"),
                "--input",
                tmp_path,
            ],
            check=False,
            capture_output=True,
            text=True,
        )

        self.assertEqual(result.returncode, 0, msg=result.stderr)
        body = json.loads(result.stdout)
        self.assertIn("relation_score", body)
        self.assertIn("insider_score", body)
        self.assertIn("link_confidence", body)

    def test_non_finite_values_are_rejected(self):
        payload = {
            "co_funder": float("nan"),
            "co_time": 0.8,
            "co_amount": 0.6,
            "co_exit": 0.7,
            "shared_sink": 0.8,
            "pre_pump_accumulation": 0.9,
            "early_cluster_share": 0.7,
            "synchronized_exit": 0.6,
            "shared_funder": 0.9,
            "shared_sink_insider": 0.8,
            "deterministic_strength": 0.8,
            "cross_source_agreement": 0.7,
            "temporal_stability": 0.9,
        }

        with self.assertRaises(ValueError):
            build_scores(payload)


if __name__ == "__main__":
    unittest.main()
