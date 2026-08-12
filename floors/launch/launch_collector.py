#!/usr/bin/env python3
"""Robinhood Chain / Uniswap CCA Launchpad 事件采集器。

数据层: 每个新 launch 的 auction 事件 + 状态快照落 JSONL(bus/inbound/launch)。
H-005/H-006/H-007 的样本都从这份原始数据出。只采集,不交易。

用法:
  uv run python launch_collector.py probe                       # 验证 RPC + 合约
  uv run python launch_collector.py scan --from-block 31800000  # 扫一段历史
  uv run python launch_collector.py watch --from-block 31800000 # 持续监听(默认每 30 块轮询)
"""
from __future__ import annotations

import argparse
import json
import sys
import time
from datetime import UTC, datetime
from pathlib import Path

import requests
from Crypto.Hash import keccak

RPC = "https://rpc.mainnet.chain.robinhood.com"
CHAIN_ID = 4663
# Robinhood Chain 部署(developers.uniswap.org/deployments.json, chainId 4663)
CCA_FACTORY = "0x000000001F26a0044BaA66024e7b6599c61963F8"          # ContinuousClearingAuctionFactory v1
CCA_FACTORY_V2 = "0x00cCa200BF124dBfA848937c553864f4B4CE0632"       # v2.0.0
LIQUIDITY_LAUNCHER = "0x0000FffFBE8efE702c8703aE3477FF5dE3d319C0"   # LiquidityLauncher
LIQUIDITY_LAUNCHER_V3 = "0x00004c4ccc709Ef590F7C81102C0689F0263D4e9"  # v3.0.0
LBP_STRATEGY = "0x05d552391067389EE44fec3924157ed33F976000"         # LBPStrategy
LBP_STRATEGY_V3 = "0x095e38a2135aeBcfFa98A5B6911591937f912000"      # v3.0.0
FACTORIES = [CCA_FACTORY, CCA_FACTORY_V2]  # 扫描两个版本

EVENT_AUCTION_CREATED = "AuctionCreated(address,address,uint256,bytes)"


def keccak256(s: str) -> str:
    k = keccak.new(digest_bits=256)
    k.update(s.encode())
    return "0x" + k.hexdigest()


# Solidity 事件签名 = 事件名 + 参数类型(无 "event " 前缀)
TOPIC_AUCTION_CREATED = keccak256(EVENT_AUCTION_CREATED)

# Auction 合约视图函数(IContinuousClearingAuction) — 全部无参 view
AUCTION_VIEWS = {
    "token": "token()",
    "currency": "currency()",
    "totalSupply": "totalSupply()",
    "startBlock": "startBlock()",
    "endBlock": "endBlock()",
    "claimBlock": "claimBlock()",
    "fundsRecipient": "fundsRecipient()",
    "tokensRecipient": "tokensRecipient()",
    "clearingPrice": "clearingPrice()",
    "isGraduated": "isGraduated()",
}


class RPCError(Exception):
    pass


class Chain:
    def __init__(self, rpc: str = RPC, timeout: int = 20):
        self.rpc = rpc
        self.timeout = timeout
        self._id = 0

    def call(self, method: str, params: list) -> any:
        self._id += 1
        r = requests.post(self.rpc, json={
            "jsonrpc": "2.0", "id": self._id, "method": method, "params": params,
        }, timeout=self.timeout)
        r.raise_for_status()
        data = r.json()
        if "error" in data:
            raise RPCError(f"{method}: {data['error']}")
        return data.get("result")

    def block_number(self) -> int:
        return int(self.call("eth_blockNumber", []), 16)

    def get_code(self, addr: str) -> str:
        return self.call("eth_getCode", [addr, "latest"]) or "0x"

    def get_logs(self, addr: str, topic0: str, from_block: int, to_block: int) -> list:
        res = self.call("eth_getLogs", [{
            "address": addr, "topics": [topic0],
            "fromBlock": hex(from_block), "toBlock": hex(to_block),
        }])
        return res or []

    def call_contract(self, addr: str, sig: str) -> str:
        """eth_call 无参 view 函数。"""
        # 4-byte selector
        k = keccak.new(digest_bits=256)
        k.update(sig.encode())
        sel = k.hexdigest()[:8]
        data = "0x" + sel
        res = self.call("eth_call", [{"to": addr, "data": data}, "latest"])
        return res or "0x"


def decode_uint256(hexstr: str) -> int:
    return int(hexstr, 16)


def decode_addr(hexstr: str) -> str:
    return "0x" + hexstr[-40:].lower()


def now_iso() -> str:
    return datetime.now(UTC).strftime("%Y-%m-%dT%H:%M:%SZ")


def probe(chain: Chain) -> None:
    cid = int(chain.call("eth_chainId", []), 16)
    bn = chain.block_number()
    print(f"chainId: {cid} (期望 {CHAIN_ID})  block: {bn}")
    for name, addr in [("CCA Factory", CCA_FACTORY), ("LiquidityLauncher", LIQUIDITY_LAUNCHER),
                       ("LBPStrategy", LBP_STRATEGY)]:
        code = chain.get_code(addr)
        print(f"  {name}: {'✅' if code and code != '0x' else '❌'} {addr}")


def fetch_auction(chain: Chain, auction_addr: str) -> dict:
    """拉 auction 合约当前状态(视图快照)。"""
    out = {"auction": auction_addr}
    for name, sig in AUCTION_VIEWS.items():
        try:
            raw = chain.call_contract(auction_addr, sig)
            if name in ("token", "currency", "fundsRecipient", "tokensRecipient"):
                out[name] = decode_addr(raw)
            else:
                out[name] = decode_uint256(raw)
        except RPCError as e:
            out[name] = None
    return out


def scan_range(chain: Chain, from_block: int, to_block: int, out_dir: Path, limit: int = 50000) -> int:
    """扫 [from_block, to_block] 的 AuctionCreated 事件,逐段避免 RPC 范围限制。"""
    written = 0
    f, t = from_block, min(to_block, from_block + limit)
    while f <= to_block:
        logs = []
        for factory in FACTORIES:
            try:
                logs.extend(chain.get_logs(factory, TOPIC_AUCTION_CREATED, f, t))
            except RPCError as e:
                print(f"  getLogs [{f},{t}] {factory} ERR: {e}")
        if not logs and f == from_block:
            pass  # 空段正常,继续
        logs.sort(key=lambda l: int(l["blockNumber"], 16))
        for log in logs:
            auction = decode_addr(log["topics"][1])
            token = decode_addr(log["topics"][2])
            amount = decode_uint256(log["data"]) if len(log["data"]) >= 66 else 0
            block = int(log["blockNumber"], 16)
            snap = fetch_auction(chain, auction)
            ev = {
                "schema_version": "kairos.event.v1",
                "event_id": f"launch-{block}-{auction[:10]}",
                "ts": now_iso(),
                "floor": "launch",
                "event_type": "launch_auction_created",
                "severity": "LOW",
                "key": auction,
                "symbol": token,
                "title": f"CCA Auction: {token[:8]}… amount={amount}",
                "message": f"auction={auction} block={block}",
                "strategy_id": "launch_intel_v1",
                "experiment_id": "exp-launch-collector-001",
                "mode": "shadow",
                "venue": "robinhood-chain",
                "direction": "neutral",
                "data": {"block": block, "amount": amount, "factory": CCA_FACTORY, "auction_state": snap},
            }
            out = out_dir / f"{datetime.now(UTC).strftime('%Y-%m-%d')}.jsonl"
            with open(out, "a") as fh:
                fh.write(json.dumps(ev, ensure_ascii=False) + "\n")
            written += 1
            print(f"  [{block}] auction={auction} token={token} amount={amount}")
        f, t = t + 1, min(to_block, t + 1 + limit)
        if written and t - f < limit:
            pass
    return written


def main() -> None:
    ap = argparse.ArgumentParser(description="Robinhood Chain Uniswap CCA launch collector")
    sub = ap.add_subparsers(dest="cmd", required=True)

    p_probe = sub.add_parser("probe")

    p_scan = sub.add_parser("scan")
    p_scan.add_argument("--from-block", type=int, required=True)
    p_scan.add_argument("--to-block", type=int, default=0)
    p_scan.add_argument("--out", default="out")

    p_watch = sub.add_parser("watch")
    p_watch.add_argument("--from-block", type=int, default=0)
    p_watch.add_argument("--out", default="out")
    p_watch.add_argument("--interval-blocks", type=int, default=30)
    p_watch.add_argument("--once", action="store_true")

    args = ap.parse_args()
    chain = Chain()

    if args.cmd == "probe":
        probe(chain)
        return

    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)

    if args.cmd == "scan":
        to = args.to_block or chain.block_number()
        n = scan_range(chain, args.from_block, to, out)
        print(f"scan done: {n} launches written -> {out}")
        return

    if args.cmd == "watch":
        cur = args.from_block or chain.block_number()
        while True:
            head = chain.block_number()
            if head >= cur:
                n = scan_range(chain, cur, head, out)
                print(f"[{now_iso()}] blocks {cur}..{head}: {n} new launches")
                cur = head + 1
            if args.once:
                break
            time.sleep(3 * args.interval_blocks)  # 粗粒度:块时间 ~1s,30 块 ≈ 30s


if __name__ == "__main__":
    sys.exit(main())
