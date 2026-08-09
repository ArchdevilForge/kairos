from __future__ import annotations

import argparse
import json
import time
from typing import Any

import cloudscraper
from requests import Response
from requests.exceptions import RequestException


DEFAULT_SOLANA_ENDPOINTS = [
    "https://api.mainnet-beta.solana.com",
    "https://api.mainnet.solana.com",
    "https://solana-rpc.publicnode.com",
]

DEFAULT_BSC_ENDPOINTS = [
    "https://bsc-dataseed.binance.org",
    "https://bsc-dataseed1.binance.org",
    "https://bsc-dataseed2.binance.org",
    "https://bsc-dataseed3.binance.org",
    "https://bsc-dataseed.bnbchain.org",
    "https://bsc-dataseed1.bnbchain.org",
    "https://bsc-dataseed-public.bnbchain.org",
    "https://bsc-dataseed.defibit.io",
    "https://bsc-dataseed1.defibit.io",
    "https://bsc.nodereal.io",
    "https://1rpc.io/bnb",
]

DEFAULT_ETH_ENDPOINTS = [
    "https://ethereum-rpc.publicnode.com",
    "https://cloudflare-eth.com",
    "https://eth.drpc.org",
    "https://1rpc.io/eth",
]

DEFAULT_BASE_ENDPOINTS = [
    "https://mainnet.base.org",
    "https://base-rpc.publicnode.com",
    "https://base.drpc.org",
    "https://1rpc.io/base",
]

CF_BLOCK_PATTERNS = (
    "error code: 1010",
    "error code 1010",
    "error 1020",
    "access denied",
    "attention required",
    "cloudflare",
    "cf-ray",
    "just a moment",
)


def build_payload(chain: str) -> dict[str, Any]:
    if chain == "solana":
        return {"jsonrpc": "2.0", "id": 1, "method": "getSlot", "params": []}
    return {"jsonrpc": "2.0", "id": 1, "method": "eth_blockNumber", "params": []}


def classify_response(status_code: int, text: str, body: Any) -> str:
    lowered = text.lower()
    if status_code in (403, 429):
        return "blocked"
    if any(pattern in lowered for pattern in CF_BLOCK_PATTERNS):
        return "blocked"
    if isinstance(body, dict) and "result" in body:
        return "ok"
    if isinstance(body, dict) and "error" in body:
        return "rpc_error"
    return "unknown"


def summarize_attempts(attempts: list[dict[str, Any]]) -> dict[str, Any]:
    success_count = sum(1 for item in attempts if item["status"] == "ok")
    blocked_count = sum(1 for item in attempts if item["status"] == "blocked")
    avg_latency_ms = round(
        sum(int(item["latency_ms"]) for item in attempts) / len(attempts)
    )

    final_status = "inactive"
    if success_count > 0:
        final_status = "active"
    elif blocked_count == len(attempts):
        final_status = "blocked"

    return {
        "final_status": final_status,
        "success_count": success_count,
        "blocked_count": blocked_count,
        "avg_latency_ms": avg_latency_ms,
    }


def parse_response_json(response: Response) -> Any:
    try:
        return response.json()
    except ValueError:
        return None


def probe_once(
    scraper: cloudscraper.CloudScraper,
    endpoint: str,
    payload: dict[str, Any],
    timeout_seconds: int,
) -> dict[str, Any]:
    started = time.time()
    try:
        response = scraper.post(endpoint, json=payload, timeout=timeout_seconds)
    except RequestException as exc:
        return {
            "status": "network_error",
            "status_code": None,
            "latency_ms": round((time.time() - started) * 1000),
            "error": str(exc),
        }

    body = parse_response_json(response)
    status = classify_response(response.status_code, response.text, body)
    rpc_error = None
    if isinstance(body, dict) and "error" in body:
        rpc_error = body["error"]

    return {
        "status": status,
        "status_code": response.status_code,
        "latency_ms": round((time.time() - started) * 1000),
        "rpc_error": rpc_error,
    }


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    _ = parser.add_argument(
        "--chain", choices=("solana", "bsc", "eth", "base"), required=True
    )
    _ = parser.add_argument("--tries", type=int, default=2)
    _ = parser.add_argument("--timeout", type=int, default=10)
    _ = parser.add_argument("--sleep", type=float, default=0.2)
    _ = parser.add_argument(
        "--endpoints",
        default="",
        help="Comma separated endpoints; if empty, use built-in pool",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    chain: str = args.chain

    endpoints: list[str]
    if args.endpoints.strip() != "":
        endpoints = [
            item.strip() for item in args.endpoints.split(",") if item.strip() != ""
        ]
    else:
        endpoint_map: dict[str, list[str]] = {
            "solana": DEFAULT_SOLANA_ENDPOINTS,
            "bsc": DEFAULT_BSC_ENDPOINTS,
            "eth": DEFAULT_ETH_ENDPOINTS,
            "base": DEFAULT_BASE_ENDPOINTS,
        }
        endpoints = endpoint_map[chain]

    payload = build_payload(chain)
    scraper = cloudscraper.create_scraper(
        browser={"browser": "chrome", "platform": "windows", "mobile": False}
    )

    results: list[dict[str, Any]] = []
    for endpoint in endpoints:
        attempts: list[dict[str, Any]] = []
        for _ in range(args.tries):
            attempts.append(probe_once(scraper, endpoint, payload, args.timeout))
            time.sleep(args.sleep)

        summary = summarize_attempts(attempts)
        results.append(
            {
                "endpoint": endpoint,
                "attempts": attempts,
                **summary,
            }
        )

    active = [item["endpoint"] for item in results if item["final_status"] == "active"]
    blocked = [
        item["endpoint"] for item in results if item["final_status"] == "blocked"
    ]

    output = {
        "chain": chain,
        "tries": args.tries,
        "timeout": args.timeout,
        "active_endpoints": active,
        "blocked_endpoints": blocked,
        "results": results,
        "notes": [
            "cloudscraper is best-effort and cannot guarantee bypass of all Cloudflare/WAF rules.",
            "Use only for lawful diagnostics and respect target terms/rate limits.",
        ],
    }
    print(json.dumps(output, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
