import importlib.util
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]

spec = importlib.util.spec_from_file_location(
    "calibrate_thresholds", ROOT / "scripts" / "calibrate_thresholds.py"
)
if spec is None or spec.loader is None:
    raise RuntimeError("unable to load scripts/calibrate_thresholds.py")

calibration_module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(calibration_module)

LP_BUCKETS = calibration_module.LP_BUCKETS
bucket_key = calibration_module.bucket_key
calibrate_thresholds = calibration_module.calibrate_thresholds


class CalibrationTests(unittest.TestCase):
    def test_bucket_key_assigns_expected_segment(self):
        self.assertEqual(bucket_key("BSC", 10000.0), "BSC:lp_lt_20k")
        self.assertEqual(bucket_key("BSC", 25000.0), "BSC:lp_20k_100k")
        self.assertEqual(bucket_key("Solana", 250000.0), "Solana:lp_gt_100k")
        self.assertEqual(len(LP_BUCKETS), 3)

    def test_calibrate_thresholds_returns_per_bucket_thresholds(self):
        records = [
            {
                "chain": "BSC",
                "lp_usd": 12000,
                "label": 1,
                "relation_score": 0.90,
                "insider_score": 0.88,
                "link_confidence": 86,
            },
            {
                "chain": "BSC",
                "lp_usd": 18000,
                "label": 1,
                "relation_score": 0.83,
                "insider_score": 0.80,
                "link_confidence": 82,
            },
            {
                "chain": "BSC",
                "lp_usd": 15000,
                "label": 0,
                "relation_score": 0.46,
                "insider_score": 0.42,
                "link_confidence": 58,
            },
            {
                "chain": "BSC",
                "lp_usd": 13000,
                "label": 0,
                "relation_score": 0.40,
                "insider_score": 0.35,
                "link_confidence": 50,
            },
        ]

        calibrated = calibrate_thresholds(records)
        bucket = calibrated["BSC:lp_lt_20k"]

        self.assertGreaterEqual(bucket["thresholds"]["relation_t"], 0.55)
        self.assertGreaterEqual(bucket["thresholds"]["insider_t"], 0.50)
        self.assertGreaterEqual(bucket["thresholds"]["link_conf_t"], 60.0)
        self.assertIn("fpr", bucket["metrics"])
        self.assertIn("fnr", bucket["metrics"])

    def test_cli_calibration_outputs_json(self):
        dataset = {
            "records": [
                {
                    "chain": "Solana",
                    "lp_usd": 110000,
                    "label": 1,
                    "relation_score": 0.87,
                    "insider_score": 0.79,
                    "link_confidence": 84,
                },
                {
                    "chain": "Solana",
                    "lp_usd": 115000,
                    "label": 0,
                    "relation_score": 0.35,
                    "insider_score": 0.31,
                    "link_confidence": 48,
                },
            ]
        }

        with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as f:
            json.dump(dataset, f)
            tmp_path = f.name

        result = subprocess.run(
            [
                sys.executable,
                str(ROOT / "scripts" / "calibrate_thresholds.py"),
                "--input",
                tmp_path,
            ],
            check=False,
            capture_output=True,
            text=True,
        )

        self.assertEqual(result.returncode, 0, msg=result.stderr)
        payload = json.loads(result.stdout)
        self.assertIn("Solana:lp_gt_100k", payload["buckets"])

    def test_invalid_label_rejected(self):
        records = [
            {
                "chain": "Solana",
                "lp_usd": 10000,
                "label": 2,
                "relation_score": 0.8,
                "insider_score": 0.8,
                "link_confidence": 80,
            }
        ]
        with self.assertRaises(ValueError):
            calibrate_thresholds(records)


if __name__ == "__main__":
    unittest.main()
