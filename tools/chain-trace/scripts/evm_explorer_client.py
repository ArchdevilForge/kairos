#!/usr/bin/env python3
"""
EVM Explorer Client - 免费访问 BSC/Base 链上数据

数据源优先级：
1. Blockscout API (Base) - 完全免费，无需 key
2. Etherscan searchHandler (BSC/Base) - 免费，无需 key
3. 公共 RPC 降级

逆向工程发现：
- BSCScan/BaseScan 的 searchHandler 端点返回 JSON，无需认证
- Base 有独立的 Blockscout 实例，提供完整的 REST API
- BSC 没有公开的 Blockscout，主要依赖 searchHandler
"""

import json
import time
import urllib.request
import urllib.parse
from typing import Optional, Dict, Any, List
from dataclasses import dataclass


@dataclass
class ChainConfig:
    """链配置"""
    name: str
    chain_id: int
    explorer_url: str
    blockscout_url: Optional[str]
    rpcs: List[str]


# 链配置
CHAINS = {
    "bsc": ChainConfig(
        name="BNB Smart Chain",
        chain_id=56,
        explorer_url="https://bscscan.com",
        blockscout_url=None,  # BSC 没有公开 Blockscout
        rpcs=[
            "https://bsc-dataseed.binance.org",
            "https://bsc-dataseed1.binance.org",
            "https://bsc-dataseed.bnbchain.org",
            "https://bsc-rpc.publicnode.com",
        ]
    ),
    "base": ChainConfig(
        name="Base",
        chain_id=8453,
        explorer_url="https://basescan.org",
        blockscout_url="https://base.blockscout.com",
        rpcs=[
            "https://mainnet.base.org",
            "https://base-rpc.publicnode.com",
            "https://base.drpc.org",
        ]
    ),
    "eth": ChainConfig(
        name="Ethereum",
        chain_id=1,
        explorer_url="https://etherscan.io",
        blockscout_url="https://eth.blockscout.com",
        rpcs=[
            "https://cloudflare-eth.com",
            "https://ethereum-rpc.publicnode.com",
            "https://eth.drpc.org",
        ]
    ),
}


class EVMExplorerClient:
    """EVM 链浏览器客户端，优先使用免费 API"""

    def __init__(self, chain: str = "base"):
        if chain not in CHAINS:
            raise ValueError(f"Unsupported chain: {chain}. Available: {list(CHAINS.keys())}")

        self.chain = chain
        self.config = CHAINS[chain]
        self._rpc_index = 0
        self._rpc_fails: Dict[str, int] = {}

    @property
    def source(self) -> str:
        if self.config.blockscout_url:
            return f"blockscout_{self.chain}"
        return f"etherscan_{self.chain}"

    def _request(self, url: str, headers: Optional[Dict] = None) -> Optional[Dict]:
        """发送 HTTP 请求"""
        default_headers = {
            "User-Agent": "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36",
            "Accept": "application/json",
        }
        if headers:
            default_headers.update(headers)

        try:
            req = urllib.request.Request(url, headers=default_headers)
            with urllib.request.urlopen(req, timeout=15) as resp:
                return json.loads(resp.read().decode())
        except Exception as e:
            print(f"[Request Error] {url}: {e}")
            return None

    # ========== Blockscout API (Base/ETH) ==========

    def _blockscout_get(self, endpoint: str) -> Optional[Dict]:
        """Blockscout API 请求"""
        if not self.config.blockscout_url:
            return None
        url = f"{self.config.blockscout_url}/api/v2{endpoint}"
        return self._request(url)

    def token_info(self, address: str) -> Optional[Dict]:
        """获取代币信息"""
        # 优先 Blockscout
        if self.config.blockscout_url:
            result = self._blockscout_get(f"/tokens/{address}")
            if result:
                return result

        # 降级到 searchHandler
        return self._search_handler(address)

    def token_holders(self, address: str, page: int = 1) -> Optional[Dict]:
        """获取代币持有者"""
        if self.config.blockscout_url:
            # Blockscout 分页用 items_count 和 block_number
            result = self._blockscout_get(f"/tokens/{address}/holders")
            if result:
                return result
        return None  # searchHandler 不支持 holders

    def token_transfers(self, address: str, page: int = 1) -> Optional[Dict]:
        """获取代币转账"""
        if self.config.blockscout_url:
            result = self._blockscout_get(f"/tokens/{address}/transfers")
            if result:
                return result
        return None

    def address_info(self, address: str) -> Optional[Dict]:
        """获取地址信息"""
        if self.config.blockscout_url:
            result = self._blockscout_get(f"/addresses/{address}")
            if result:
                return result
        return self._search_handler(address)

    def address_transactions(self, address: str) -> Optional[Dict]:
        """获取地址交易"""
        if self.config.blockscout_url:
            result = self._blockscout_get(f"/addresses/{address}/transactions")
            if result:
                return result
        return None

    def address_token_transfers(self, address: str) -> Optional[Dict]:
        """获取地址的代币转账"""
        if self.config.blockscout_url:
            result = self._blockscout_get(f"/addresses/{address}/token-transfers")
            if result:
                return result
        return None

    def transaction(self, tx_hash: str) -> Optional[Dict]:
        """获取交易详情"""
        if self.config.blockscout_url:
            result = self._blockscout_get(f"/transactions/{tx_hash}")
            if result:
                return result
        return self._rpc_get_transaction(tx_hash)

    def block(self, block_number: int) -> Optional[Dict]:
        """获取区块信息"""
        if self.config.blockscout_url:
            result = self._blockscout_get(f"/blocks/{block_number}")
            if result:
                return result
        return None

    def stats(self) -> Optional[Dict]:
        """获取链统计"""
        if self.config.blockscout_url:
            result = self._blockscout_get("/stats")
            if result:
                return result
        return None

    # ========== Etherscan searchHandler (BSC/Base) ==========
    # NOTE: 2025年起 BSCScan/BaseScan 已对 searchHandler 返回 403（Cloudflare WAF）。
    # 改用 RPC eth_call 代替。

    def _search_handler(self, query: str) -> Optional[Dict]:
        """searchHandler 已失效（403），降级到 RPC eth_call"""
        return self._rpc_get_token_basic(query)

    def search(self, query: str) -> Optional[List[Dict]]:
        return None

    def _rpc_get_token_basic(self, address: str) -> Optional[Dict]:
        """通过 RPC eth_call 获取代币基本信息"""
        name = self._rpc_erc20_call(address, "0x06fdde03")   # name()
        symbol = self._rpc_erc20_call(address, "0x95d89b41")  # symbol()
        decimals = self._rpc_erc20_call(address, "0x313ce567") # decimals()
        total_supply = self._rpc_erc20_call(address, "0x18160ddd") # totalSupply()

        if not name and not symbol:
            return None

        result = {"address": address}
        if name:
            result["name"] = name
        if symbol:
            result["symbol"] = symbol
        if decimals is not None:
            result["decimals"] = str(decimals)
        if total_supply is not None:
            result["total_supply"] = str(total_supply)
        result["source"] = f"eth_call_{self.chain}"
        return result

    def _rpc_erc20_call(self, contract: str, data: str) -> Optional[str]:
        """执行 ERC20 eth_call，解码返回的 bytes"""
        result = self._rpc_call("eth_call", [{"to": contract, "data": data}, "latest"])
        if not result:
            return None
        raw = result[2:] if result.startswith("0x") else result
        if not raw:
            return None
        # ERC20 string return: 0x20 + offset(32) + len(32) + data
        if data == "0x313ce567":  # decimals: uint8
            try:
                return str(int(raw[-2:], 16)) if raw[-2:] else "0"
            except (ValueError, IndexError):
                return None
        if data == "0x18160ddd":  # totalSupply: uint256
            try:
                return str(int(raw[-64:], 16)) if len(raw) >= 64 else None
            except (ValueError, IndexError):
                return None
        # name/symbol: string
        try:
            length = int(raw[64:128], 16) if len(raw) >= 128 else 0
            if length > 0 and len(raw) >= 128 + length * 2:
                return bytes.fromhex(raw[128:128 + length * 2]).decode("utf-8", errors="replace")
        except (ValueError, IndexError):
            pass
        return None

    # ========== 公共 RPC Fallback ==========

    def _get_rpc(self) -> str:
        """轮换获取可用 RPC"""
        rpcs = self.config.rpcs
        for _ in range(len(rpcs)):
            rpc = rpcs[self._rpc_index]
            if self._rpc_fails.get(rpc, 0) < 3:
                return rpc
            self._rpc_index = (self._rpc_index + 1) % len(rpcs)
        self._rpc_fails.clear()
        return rpcs[0]

    def _rpc_call(self, method: str, params: list) -> Optional[Any]:
        """执行 RPC 调用"""
        rpc = self._get_rpc()
        payload = json.dumps({
            "jsonrpc": "2.0",
            "id": 1,
            "method": method,
            "params": params
        }).encode()

        for attempt in range(3):
            try:
                req = urllib.request.Request(
                    rpc,
                    data=payload,
                    headers={"Content-Type": "application/json"}
                )
                with urllib.request.urlopen(req, timeout=12) as resp:
                    result = json.loads(resp.read().decode())
                    if "result" in result:
                        self._rpc_fails[rpc] = 0
                        return result["result"]
            except Exception as e:
                self._rpc_fails[rpc] = self._rpc_fails.get(rpc, 0) + 1
                if attempt < 2:
                    time.sleep(1 << attempt)
                    self._rpc_index = (self._rpc_index + 1) % len(self.config.rpcs)
        return None

    def _rpc_get_transaction(self, tx_hash: str) -> Optional[Dict]:
        return self._rpc_call("eth_getTransactionByHash", [tx_hash])

    def get_balance(self, address: str) -> Optional[int]:
        """获取原生币余额"""
        result = self._rpc_call("eth_getBalance", [address, "latest"])
        if result:
            return int(result, 16)
        return None

    def get_token_balance(self, token: str, address: str) -> Optional[int]:
        """获取 ERC20 代币余额"""
        # balanceOf(address) = 0x70a08231
        data = f"0x70a08231000000000000000000000000{address[2:]}"
        result = self._rpc_call("eth_call", [{"to": token, "data": data}, "latest"])
        if result:
            return int(result, 16)
        return None


# ========== CLI ==========

if __name__ == "__main__":
    import argparse

    parser = argparse.ArgumentParser(description="EVM Explorer Client - 免费 API")
    parser.add_argument("--chain", "-c", default="base", choices=["bsc", "base", "eth"])
    parser.add_argument("--address", "-a", help="地址")
    parser.add_argument("--token", "-t", help="代币地址")
    parser.add_argument("--tx", help="交易哈希")
    parser.add_argument("--method", default="info",
                        choices=["info", "holders", "transfers", "transactions", "search", "stats", "balance"])
    args = parser.parse_args()

    client = EVMExplorerClient(chain=args.chain)
    print(f"[Chain] {client.config.name}")
    print(f"[Source] {client.source}")

    result = None

    if args.method == "info":
        if args.token:
            result = client.token_info(args.token)
        elif args.address:
            result = client.address_info(args.address)
    elif args.method == "holders" and args.token:
        result = client.token_holders(args.token)
    elif args.method == "transfers" and args.token:
        result = client.token_transfers(args.token)
    elif args.method == "transactions" and args.address:
        result = client.address_transactions(args.address)
    elif args.method == "search" and args.address:
        result = client.search(args.address)
    elif args.method == "stats":
        result = client.stats()
    elif args.method == "balance" and args.address:
        balance = client.get_balance(args.address)
        result = {"balance_wei": balance, "balance_eth": balance / 1e18 if balance else None}
    elif args.tx:
        result = client.transaction(args.tx)

    if result:
        print(json.dumps(result, indent=2, ensure_ascii=False, default=str))
    else:
        print("No result or unsupported operation")
        print("\nExamples:")
        print(f"  --chain base --token 0x4200000000000000000000000000000000000006 --method info")
        print(f"  --chain base --token 0x4200000000000000000000000000000000000006 --method holders")
        print(f"  --chain bsc --address 0xbb4CdB9CBd36B01bD1cBaEBF2De08d9173bc095c --method search")
