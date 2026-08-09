"""
Solana RPC Client — 完全基于公共 RPC，无外部依赖

优化：
  - getMultipleAccounts 批量查 account info（替代单次 getAccountInfo）
  - 指数退避 + jitter 应对 429
  - getTokenSupply 作为 holder 查询的降级
"""

import json
import random
import time
from typing import Optional, Dict, Any

SOLANA_RPCS = [
    "https://api.mainnet-beta.solana.com",
    "https://api.mainnet.solana.com",
    "https://solana-rpc.publicnode.com",
]


class SolscanClient:
    def __init__(self, prefer_solscan: bool = True):
        self._rpc_index = 0
        self._rpc_fails: Dict[str, int] = {}

    @property
    def source(self) -> str:
        return "public_rpc"

    def _get_rpc(self) -> str:
        for _ in range(len(SOLANA_RPCS)):
            rpc = SOLANA_RPCS[self._rpc_index]
            if self._rpc_fails.get(rpc, 0) < 3:
                return rpc
            self._rpc_index = (self._rpc_index + 1) % len(SOLANA_RPCS)
        self._rpc_fails.clear()
        return SOLANA_RPCS[0]

    def _rpc_call(self, method: str, params: list) -> Optional[Any]:
        import urllib.request
        rpc = self._get_rpc()
        payload = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params}).encode()
        for attempt in range(4):
            try:
                req = urllib.request.Request(rpc, data=payload, headers={"Content-Type": "application/json"})
                with urllib.request.urlopen(req, timeout=12) as resp:
                    result = json.loads(resp.read().decode())
                    if "result" in result:
                        self._rpc_fails[rpc] = 0
                        return result["result"]
                    if "error" in result:
                        code = result["error"].get("code", 0)
                        if code == 429:
                            self._rpc_fails[rpc] = self._rpc_fails.get(rpc, 0) + 1
                            if attempt < 3:
                                sleep = (2 ** attempt) + random.random()
                                time.sleep(sleep)
                                self._rpc_index = (self._rpc_index + 1) % len(SOLANA_RPCS)
                                rpc = self._get_rpc()
                                continue
                        return None
            except Exception as e:
                self._rpc_fails[rpc] = self._rpc_fails.get(rpc, 0) + 1
                if attempt < 3:
                    time.sleep((1 << attempt) + random.random())
                    self._rpc_index = (self._rpc_index + 1) % len(SOLANA_RPCS)
                    rpc = self._get_rpc()
        return None

    def transaction(self, tx_hash: str) -> Optional[Dict[str, Any]]:
        return self._rpc_call("getTransaction", [tx_hash, {"encoding": "jsonParsed", "maxSupportedTransactionVersion": 0}])

    def transactions(self, address: str, page: int = 1, page_size: int = 40) -> Optional[Dict[str, Any]]:
        sigs = self._rpc_call("getSignaturesForAddress", [address, {"limit": min(page_size, 100)}])
        return {"signatures": sigs} if sigs else None

    def accounts_info(self, addresses: list[str]) -> dict[str, Optional[Dict[str, Any]]]:
        if not addresses:
            return {}
        result = self._rpc_call("getMultipleAccounts", [addresses, {"encoding": "jsonParsed"}])
        if not result or "value" not in result:
            return {}
        vals = result["value"] or []
        out: dict[str, Optional[Dict[str, Any]]] = {}
        for i, addr in enumerate(addresses):
            v = vals[i] if i < len(vals) else None
            out[addr] = v.get("data") if v and isinstance(v, dict) else None
        return out

    def account_info(self, address: str) -> Optional[Dict[str, Any]]:
        result = self._rpc_call("getAccountInfo", [address, {"encoding": "jsonParsed"}])
        if result and "value" in result:
            return result["value"]
        return None

    def token_data(self, mint: str = "So11111111111111111111111111111111111111112") -> Optional[Dict[str, Any]]:
        supply = self._rpc_call("getTokenSupply", [mint])
        if supply:
            return {
                "supply": supply["value"]["uiAmountString"] if supply.get("value") else None,
                "decimals": supply["value"]["decimals"] if supply.get("value") else None,
            }
        return None

    def token_holders(self, mint: str, page: int = 1, page_size: int = 100) -> Optional[Dict[str, Any]]:
        result = self._rpc_call("getTokenLargestAccounts", [mint])
        if result and "value" in result:
            accounts = []
            for v in result["value"]:
                if isinstance(v, dict):
                    accounts.append({
                        "address": v.get("address", ""),
                        "amount": str(v.get("amount", 0)),
                        "decimals": v.get("decimals", 0),
                        "uiAmountString": str(v.get("uiAmount", v.get("uiAmountString", "0"))),
                    })
            return {"accounts": accounts, "note": "Top 20 holders only"}
        return None

    def token_holders_total(self, mint: str) -> Optional[int]:
        return None

    def transfers(self, address: str, page: int = 1, page_size: int = 100,
                  remove_spam: bool = True, exclude_amount_zero: bool = True) -> Optional[Dict[str, Any]]:
        return None

    def defi_activities(self, address: str, page: int = 1, page_size: int = 100) -> Optional[Dict[str, Any]]:
        return None

    def portfolio(self, address: str, token_type: str = "token",
                  page: int = 1, page_size: int = 100, hide_zero: bool = True) -> Optional[Dict[str, Any]]:
        return None

    def balance_history(self, address: str) -> Optional[Dict[str, Any]]:
        return None

    def top_address_transfers(self, address: str, range_days: int = 7) -> Optional[Dict[str, Any]]:
        return None


if __name__ == "__main__":
    import argparse
    parser = argparse.ArgumentParser(description="Solana RPC Client")
    parser.add_argument("--address", "-a")
    parser.add_argument("--addresses", nargs="+")
    parser.add_argument("--tx", "-t")
    parser.add_argument("--mint", "-m")
    parser.add_argument("--method", default="account_info",
                        choices=["account_info", "accounts_info", "transactions", "transaction", "token_holders", "token_data"])
    args = parser.parse_args()
    client = SolscanClient()
    print(f"[Source] {client.source}")
    result = None
    if args.method == "account_info" and args.address:
        result = client.account_info(args.address)
    elif args.method == "accounts_info" and args.addresses:
        result = client.accounts_info(args.addresses)
    elif args.method == "transactions" and args.address:
        result = client.transactions(args.address)
    elif args.method == "transaction" and args.tx:
        result = client.transaction(args.tx)
    elif args.method == "token_holders" and args.mint:
        result = client.token_holders(args.mint)
    elif args.method == "token_data" and args.mint:
        result = client.token_data(args.mint)
    if result:
        print(json.dumps(result, indent=2, ensure_ascii=False))
    else:
        print("No result")
