"""
Market Data — 跨链价格查询，零依赖，无需 API Key

数据源：
  - DefiLlama (coins.llama.fi): 价格/symbol/decimals，支持批量
  - DIA (api.diadata.org): 主流代币价格备用
"""

import json
import time
import urllib.request
from typing import Optional

CHAIN_MAP = {
    "solana": "solana",
    "eth": "ethereum",
    "base": "base",
    "bsc": "bsc",
}


class MarketDataClient:
    def __init__(self):
        self._cache: dict[str, dict] = {}

    def get_prices(self, chain: str, addresses: list[str]) -> dict[str, dict]:
        key = f"{chain}:{','.join(sorted(addresses))}"
        if key in self._cache:
            return self._cache[key]

        cf = CHAIN_MAP.get(chain)
        if not cf:
            return {}

        ids = ",".join(f"{cf}:{a}" for a in addresses)
        url = f"https://coins.llama.fi/prices/current/{ids}"
        try:
            req = urllib.request.Request(url, headers={"User-Agent": "chain-trace/0.2"})
            with urllib.request.urlopen(req, timeout=10) as resp:
                data = json.loads(resp.read().decode()).get("coins", {})
                result = {}
                for addr in addresses:
                    cid = f"{cf}:{addr}"
                    if cid in data:
                        result[addr] = data[cid]
                self._cache[key] = result
                return result
        except Exception:
            return {}

    def get_price(self, chain: str, address: str) -> Optional[dict]:
        prices = self.get_prices(chain, [address])
        return prices.get(address)

    def get_name(self, chain: str, address: str) -> Optional[str]:
        p = self.get_price(chain, address)
        if p and "symbol" in p:
            return p.get("symbol")
        return None

    def get_decimals(self, chain: str, address: str) -> Optional[int]:
        p = self.get_price(chain, address)
        if p and "decimals" in p:
            return p.get("decimals")
        return None


if __name__ == "__main__":
    import sys
    client = MarketDataClient()
    if len(sys.argv) > 2:
        chain = sys.argv[1]
        addrs = sys.argv[2:]
        print(json.dumps(client.get_prices(chain, addrs), indent=2))
    else:
        print("Usage: python market_data.py <chain> <addr1> [addr2 ...]")
        print("Example: python market_data.py solana So11111111111111111111111111111111111111112")
