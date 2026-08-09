import importlib.util
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]

spec = importlib.util.spec_from_file_location(
    "rpc_probe_cloudscraper", ROOT / "scripts" / "rpc_probe_cloudscraper.py"
)
if spec is None or spec.loader is None:
    raise RuntimeError("unable to load scripts/rpc_probe_cloudscraper.py")

probe_module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(probe_module)

build_payload = probe_module.build_payload
classify_response = probe_module.classify_response
summarize_attempts = probe_module.summarize_attempts


class RpcProbeCloudscraperTests(unittest.TestCase):
    def test_build_payload_for_supported_chains(self):
        solana_payload = build_payload("solana")
        bsc_payload = build_payload("bsc")
        eth_payload = build_payload("eth")
        base_payload = build_payload("base")

        self.assertEqual(solana_payload["method"], "getSlot")
        self.assertEqual(bsc_payload["method"], "eth_blockNumber")
        self.assertEqual(eth_payload["method"], "eth_blockNumber")
        self.assertEqual(base_payload["method"], "eth_blockNumber")

    def test_classify_response_marks_blocked_patterns(self):
        blocked = classify_response(403, "Error code: 1010", {"error": {"code": 403}})
        self.assertEqual(blocked, "blocked")

    def test_classify_response_marks_success(self):
        ok = classify_response(200, "", {"result": 123})
        self.assertEqual(ok, "ok")

    def test_summarize_attempts_active_and_blocked(self):
        active_summary = summarize_attempts(
            [
                {"status": "ok", "latency_ms": 120},
                {"status": "rpc_error", "latency_ms": 130},
            ]
        )
        blocked_summary = summarize_attempts(
            [
                {"status": "blocked", "latency_ms": 100},
                {"status": "blocked", "latency_ms": 140},
            ]
        )

        self.assertEqual(active_summary["final_status"], "active")
        self.assertEqual(blocked_summary["final_status"], "blocked")


if __name__ == "__main__":
    unittest.main()
