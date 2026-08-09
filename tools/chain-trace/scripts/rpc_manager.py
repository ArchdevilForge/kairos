"""
RPC Manager with automatic fallback, retry, and rate limit handling.

Based on research from:
- eth_retry library patterns
- web3.py retry mechanisms
- Chainstack best practices
"""

import time
import random
from typing import Dict, List, Optional, Any, Callable
from dataclasses import dataclass, field
from enum import Enum
import requests
from datetime import datetime, timedelta, timezone


class RPCError(Exception):
    """Base exception for RPC errors"""
    pass


class RateLimitError(RPCError):
    """Rate limit exceeded"""
    pass


class AllRPCsFailedError(RPCError):
    """All RPC endpoints failed"""
    pass


@dataclass
class RPCEndpoint:
    """RPC endpoint with health tracking"""
    url: str
    tier: int = 1  # 1=highest priority, 3=lowest
    consecutive_failures: int = 0
    cooldown_until: Optional[datetime] = None
    total_requests: int = 0
    total_failures: int = 0
    avg_response_time: float = 0.0

    def is_available(self) -> bool:
        """Check if endpoint is available (not in cooldown)"""
        if self.cooldown_until is None:
            return True
        return datetime.now(timezone.utc) >= self.cooldown_until

    def mark_success(self, response_time: float):
        """Mark successful request"""
        self.consecutive_failures = 0
        self.cooldown_until = None
        self.total_requests += 1
        # Exponential moving average
        alpha = 0.3
        self.avg_response_time = (alpha * response_time +
                                   (1 - alpha) * self.avg_response_time)

    def mark_failure(self, cooldown_base: int = 30):
        """Mark failed request and set cooldown"""
        self.consecutive_failures += 1
        self.total_failures += 1
        self.total_requests += 1

        # Exponential backoff: 30s, 60s, 120s, 240s, ...
        cooldown_seconds = cooldown_base * (2 ** min(self.consecutive_failures - 1, 5))
        self.cooldown_until = datetime.now(timezone.utc) + timedelta(seconds=cooldown_seconds)


class Chain(Enum):
    """Supported chains"""
    ETH = "eth"
    BASE = "base"
    BSC = "bsc"
    SOLANA = "solana"


# RPC endpoint pools (from SKILL.md)
RPC_POOLS = {
    Chain.ETH: [
        ("https://cloudflare-eth.com", 1),
        ("https://ethereum-rpc.publicnode.com", 1),
        ("https://rpc.flashbots.net", 2),
        ("https://eth.drpc.org", 2),
        ("https://1rpc.io/eth", 2),
    ],
    Chain.BASE: [
        ("https://mainnet.base.org", 1),
        ("https://base-rpc.publicnode.com", 1),
        ("https://base.drpc.org", 2),
        ("https://1rpc.io/base", 2),
    ],
    Chain.BSC: [
        ("https://bsc-dataseed.binance.org", 1),
        ("https://bsc-dataseed1.binance.org", 1),
        ("https://bsc-dataseed2.binance.org", 1),
        ("https://bsc-dataseed3.binance.org", 1),
        ("https://bsc-dataseed.bnbchain.org", 1),
        ("https://bsc-dataseed1.bnbchain.org", 1),
        ("https://bsc-dataseed-public.bnbchain.org", 1),
        ("https://bsc-dataseed.defibit.io", 2),
        ("https://bsc-dataseed1.defibit.io", 2),
        ("https://bsc-dataseed.ninicoin.io", 2),
        ("https://bsc.nodereal.io", 2),
        ("https://1rpc.io/bnb", 2),
    ],
    Chain.SOLANA: [
        ("https://api.mainnet-beta.solana.com", 1),
        ("https://api.mainnet.solana.com", 1),
        ("https://solana-rpc.publicnode.com", 2),
    ],
}


class RPCManager:
    """
    Manages RPC calls with automatic fallback and retry.

    Features:
    - Automatic endpoint rotation on failure
    - Exponential backoff with jitter
    - Rate limit detection and handling
    - Health tracking per endpoint
    - Tier-based prioritization
    """

    def __init__(
        self,
        chain: Chain,
        max_retries: int = 3,
        timeout: int = 12,
        probe_on_init: bool = False
    ):
        """
        Initialize RPC Manager.

        Args:
            chain: Target blockchain
            max_retries: Max retries per endpoint
            timeout: Request timeout in seconds
            probe_on_init: Probe endpoints on initialization
        """
        self.chain = chain
        self.max_retries = max_retries
        self.timeout = timeout

        # Initialize endpoints
        self.endpoints: List[RPCEndpoint] = []
        for url, tier in RPC_POOLS.get(chain, []):
            self.endpoints.append(RPCEndpoint(url=url, tier=tier))

        if not self.endpoints:
            raise ValueError(f"No RPC endpoints configured for {chain}")

        # Sort by tier (lower tier = higher priority)
        self.endpoints.sort(key=lambda e: (e.tier, e.url))

        if probe_on_init:
            self._probe_endpoints()

    def _probe_endpoints(self):
        """Probe endpoints to check availability"""
        print(f"[RPCManager] Probing {len(self.endpoints)} endpoints for {self.chain.value}...")

        # Lightweight probe method
        if self.chain == Chain.SOLANA:
            probe_payload = {"jsonrpc": "2.0", "id": 1, "method": "getSlot", "params": []}
        else:
            probe_payload = {"jsonrpc": "2.0", "id": 1, "method": "eth_blockNumber", "params": []}

        active_count = 0
        for endpoint in self.endpoints:
            try:
                start = time.time()
                response = requests.post(
                    endpoint.url,
                    json=probe_payload,
                    timeout=8,
                    headers={"Content-Type": "application/json"}
                )
                elapsed = time.time() - start

                if response.status_code == 200 and "result" in response.json():
                    endpoint.mark_success(elapsed)
                    active_count += 1
                    print(f"  ✓ {endpoint.url[:50]} ({elapsed:.2f}s)")
                else:
                    endpoint.mark_failure()
                    print(f"  ✗ {endpoint.url[:50]} (status {response.status_code})")
            except Exception as e:
                endpoint.mark_failure()
                print(f"  ✗ {endpoint.url[:50]} ({type(e).__name__})")

        print(f"[RPCManager] {active_count}/{len(self.endpoints)} endpoints active\n")

    def _is_rate_limit_error(self, response: Optional[requests.Response], error: Optional[Exception]) -> bool:
        """Detect rate limit errors"""
        if response is not None:
            if response.status_code == 429:
                return True
            if response.status_code >= 500:
                return True

            # Check response body
            try:
                body = response.text.lower()
                if any(keyword in body for keyword in [
                    "rate limit", "too many requests", "error code: 1010",
                    "error code: 1020", "unauthorized", "limit exceeded"
                ]):
                    return True
            except:
                pass

        if error is not None:
            error_str = str(error).lower()
            if any(keyword in error_str for keyword in [
                "rate limit", "too many", "429", "limit exceeded"
            ]):
                return True

        return False

    def _retry_sleep(self, attempt: int):
        """Sleep with exponential backoff and jitter"""
        base = 2 ** attempt
        jitter = random.uniform(0, 1)
        total = min(base + jitter, 16)  # Cap at 16 seconds
        time.sleep(total)

    def call(
        self,
        method: str,
        params: List[Any],
        custom_timeout: Optional[int] = None
    ) -> Any:
        """
        Make RPC call with automatic fallback and retry.

        Args:
            method: RPC method name
            params: Method parameters
            custom_timeout: Override default timeout

        Returns:
            RPC result

        Raises:
            AllRPCsFailedError: If all endpoints fail
        """
        timeout = custom_timeout or self.timeout
        payload = {
            "jsonrpc": "2.0",
            "id": 1,
            "method": method,
            "params": params
        }

        # Try each available endpoint
        for endpoint in self.endpoints:
            if not endpoint.is_available():
                continue

            # Retry logic for this endpoint
            for attempt in range(self.max_retries):
                try:
                    start = time.time()
                    response = requests.post(
                        endpoint.url,
                        json=payload,
                        timeout=timeout,
                        headers={"Content-Type": "application/json"}
                    )
                    elapsed = time.time() - start

                    # Success
                    if response.status_code == 200:
                        data = response.json()
                        if "result" in data:
                            endpoint.mark_success(elapsed)
                            return data["result"]
                        elif "error" in data:
                            # RPC error (not transport error)
                            error_msg = data["error"].get("message", str(data["error"]))
                            raise RPCError(f"RPC error: {error_msg}")

                    # Rate limit or server error
                    if self._is_rate_limit_error(response, None):
                        endpoint.mark_failure()
                        print(f"[RPCManager] Rate limit on {endpoint.url[:40]}, switching endpoint")
                        break  # Try next endpoint

                    # Other HTTP error - retry
                    if attempt < self.max_retries - 1:
                        self._retry_sleep(attempt)

                except requests.exceptions.Timeout:
                    if attempt < self.max_retries - 1:
                        self._retry_sleep(attempt)
                    else:
                        endpoint.mark_failure()
                        break

                except requests.exceptions.RequestException as e:
                    if self._is_rate_limit_error(None, e):
                        endpoint.mark_failure()
                        break

                    if attempt < self.max_retries - 1:
                        self._retry_sleep(attempt)
                    else:
                        endpoint.mark_failure()
                        break

                except Exception as e:
                    print(f"[RPCManager] Unexpected error: {e}")
                    if attempt < self.max_retries - 1:
                        self._retry_sleep(attempt)
                    else:
                        endpoint.mark_failure()
                        break

        # All endpoints failed
        raise AllRPCsFailedError(
            f"All RPC endpoints failed for {self.chain.value} method {method}"
        )

    def batch_call(
        self,
        calls: List[tuple[str, List[Any]]]
    ) -> List[Any]:
        """
        Make batch RPC calls.

        Args:
            calls: List of (method, params) tuples

        Returns:
            List of results
        """
        # Simple sequential implementation
        # TODO: Implement true JSON-RPC batch requests
        results = []
        for method, params in calls:
            result = self.call(method, params)
            results.append(result)
        return results

    def get_stats(self) -> Dict[str, Any]:
        """Get endpoint statistics"""
        stats = {
            "chain": self.chain.value,
            "total_endpoints": len(self.endpoints),
            "available_endpoints": sum(1 for e in self.endpoints if e.is_available()),
            "endpoints": []
        }

        for endpoint in self.endpoints:
            stats["endpoints"].append({
                "url": endpoint.url,
                "tier": endpoint.tier,
                "available": endpoint.is_available(),
                "consecutive_failures": endpoint.consecutive_failures,
                "total_requests": endpoint.total_requests,
                "total_failures": endpoint.total_failures,
                "success_rate": (
                    (endpoint.total_requests - endpoint.total_failures) / endpoint.total_requests
                    if endpoint.total_requests > 0 else 0
                ),
                "avg_response_time": endpoint.avg_response_time,
                "cooldown_until": endpoint.cooldown_until.isoformat() if endpoint.cooldown_until else None
            })

        return stats


# Convenience functions
def create_rpc_manager(chain_name: str, **kwargs) -> RPCManager:
    """Create RPC manager from chain name string"""
    chain_map = {
        "eth": Chain.ETH,
        "ethereum": Chain.ETH,
        "base": Chain.BASE,
        "bsc": Chain.BSC,
        "bnb": Chain.BSC,
        "solana": Chain.SOLANA,
        "sol": Chain.SOLANA,
    }

    chain = chain_map.get(chain_name.lower())
    if not chain:
        raise ValueError(f"Unknown chain: {chain_name}")

    return RPCManager(chain, **kwargs)


if __name__ == "__main__":
    # Test
    import sys

    if len(sys.argv) < 2:
        print("Usage: python rpc_manager.py <chain>")
        print("Chains: eth, base, bsc, solana")
        sys.exit(1)

    chain_name = sys.argv[1]
    manager = create_rpc_manager(chain_name, probe_on_init=True)

    # Test call
    try:
        if chain_name.lower() in ["solana", "sol"]:
            result = manager.call("getSlot", [])
            print(f"\nCurrent slot: {result}")
        else:
            result = manager.call("eth_blockNumber", [])
            block_num = int(result, 16)
            print(f"\nCurrent block: {block_num}")

        # Show stats
        print("\n=== RPC Manager Stats ===")
        stats = manager.get_stats()
        print(f"Chain: {stats['chain']}")
        print(f"Available: {stats['available_endpoints']}/{stats['total_endpoints']}")

    except AllRPCsFailedError as e:
        print(f"\nError: {e}")
        sys.exit(1)
