#!/usr/bin/env python3
"""Robinhood Chain / Uniswap launchpad 事件采集器。

数据层: 每个新 launch(curve/instant + CCA crowd)的事件 + 状态快照落 JSONL
(bus/inbound/launch)。H-005/H-006/H-007 的样本都从这份原始数据出。只采集,不交易。

覆盖(见 docs/GOAL_LAUNCH_DATA_AND_RISK_GATE.md §1.2):
  - TokenCreated(address)        两个入口合约(现行+原始, 只监听一个会漏 ~40%)
  - AuctionCreated(...)          两个 CCA factory(crowd launch 拍卖创建)

用法:
  uv run python launch_collector.py probe                       # 验证 RPC + 合约
  uv run python launch_collector.py scan --from-block 31800000  # 扫一段历史
  uv run python launch_collector.py watch                       # 常驻监听(断点续扫)

RPC endpoint 用 env ROBINHOOD_RPC_URL 覆盖(公共端点限流,生产建议 Alchemy/blockmachine)。
"""
from __future__ import annotations

import argparse
import json
import os
import sys
import time
from datetime import UTC, datetime
from pathlib import Path

import requests
from Crypto.Hash import keccak

DEFAULT_RPC = "https://rpc.mainnet.chain.robinhood.com"
CHAIN_ID = 4663  # 块时间 ~100ms(Arbitrum Orbit L2),轮询按秒规划而非按块

# ── 合约地址(权威源: Bitquery pools.trade 文档 + Uniswap liquidity-launcher-sdk) ──
# 入口合约(LiquidityLauncher): TokenCreated 是所有 curve/instant launch 的唯一信号。
# 两个都活跃(2026-08-05 实测 6,907 + 4,530/天),事件签名逐字节一致。
LAUNCH_ENTRY_CURRENT = "0x0000ffffbe8efe702c8703ae3477ff5de3d319c0"   # 2026-08-05 起
LAUNCH_ENTRY_ORIGINAL = "0x00004c4ccc709ef590f7c81102c0689f0263d4e9"  # 2026-07-08 起,仍活跃
ENTRY_CONTRACTS = [LAUNCH_ENTRY_CURRENT, LAUNCH_ENTRY_ORIGINAL]

# CCA(ContinuousClearingAuction)factory: crowd launch 拍卖创建。
CCA_FACTORY = "0x000000001f26a0044baa66024e7b6599c61963f8"    # v1
CCA_FACTORY_V2 = "0x00cca200bf124dbfa848937c553864f4b4ce0632"  # v2.0.0
FACTORIES = [CCA_FACTORY, CCA_FACTORY_V2]

# token metadata factory(TokenCreated 重载版本,含 name/symbol)
TOKEN_FACTORY = "0x000000e200088d55c39a11f609e5f667729ad49b"


def keccak256(s: str) -> str:
    k = keccak.new(digest_bits=256)
    k.update(s.encode())
    return "0x" + k.hexdigest()


# Solidity 事件签名(名字+参数类型)。topic0 已与 Bitquery 公开口径逐一核对。
SIG_TOKEN_CREATED = "TokenCreated(address)"
SIG_AUCTION_CREATED = "AuctionCreated(address,address,uint256,bytes)"
SIG_TOKEN_DISTRIBUTED = "TokenDistributed(address,address,uint256)"
SIG_BID_SUBMITTED = "BidSubmitted(uint256,address,uint256,uint128)"
SIG_CLEARING_PRICE_UPDATED = "ClearingPriceUpdated(uint256,uint256)"

TOPIC_TOKEN_CREATED = keccak256(SIG_TOKEN_CREATED)
TOPIC_AUCTION_CREATED = keccak256(SIG_AUCTION_CREATED)
TOPIC_TOKEN_DISTRIBUTED = keccak256(SIG_TOKEN_DISTRIBUTED)
TOPIC_BID_SUBMITTED = keccak256(SIG_BID_SUBMITTED)
TOPIC_CLEARING_PRICE_UPDATED = keccak256(SIG_CLEARING_PRICE_UPDATED)

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

# getLogs 自适应窗口: 公共 RPC 会拒绝大范围,失败折半、成功回升。
STEP_MAX = 20_000
STEP_MIN = 500


class RPCError(Exception):
    pass


class Chain:
    """JSON-RPC 客户端,带指数退避重试(公共端点限流是常态)。"""

    def __init__(self, rpc: str | None = None, timeout: int = 20, retries: int = 3):
        self.rpc = rpc or os.environ.get("ROBINHOOD_RPC_URL", DEFAULT_RPC)
        self.timeout = timeout
        self.retries = retries
        self._id = 0

    def call(self, method: str, params: list) -> any:
        self._id += 1
        last_err: Exception | None = None
        for attempt in range(self.retries):
            try:
                r = requests.post(self.rpc, json={
                    "jsonrpc": "2.0", "id": self._id, "method": method, "params": params,
                }, timeout=self.timeout)
                r.raise_for_status()
                data = r.json()
                if "error" in data:
                    raise RPCError(f"{method}: {data['error']}")
                return data.get("result")
            except (requests.RequestException, RPCError) as e:
                last_err = e
                if attempt < self.retries - 1:
                    time.sleep(2 ** attempt)  # 1s, 2s
        raise RPCError(f"{method} failed after {self.retries} tries: {last_err}")

    def block_number(self) -> int:
        return int(self.call("eth_blockNumber", []), 16)

    def get_code(self, addr: str) -> str:
        return self.call("eth_getCode", [addr, "latest"]) or "0x"

    def get_logs(self, addresses: list[str], topic0: str, from_block: int, to_block: int) -> list:
        res = self.call("eth_getLogs", [{
            "address": addresses, "topics": [topic0],
            "fromBlock": hex(from_block), "toBlock": hex(to_block),
        }])
        return res or []

    def get_tx_sender(self, tx_hash: str) -> str | None:
        """launch creator(H-006 特征),失败返回 None 不阻塞采集。"""
        try:
            tx = self.call("eth_getTransactionByHash", [tx_hash])
            return (tx or {}).get("from")
        except RPCError:
            return None

    def call_contract(self, addr: str, sig: str) -> str:
        """eth_call 无参 view 函数。"""
        sel = keccak256(sig)[2:10]
        res = self.call("eth_call", [{"to": addr, "data": "0x" + sel}, "latest"])
        return res or "0x"


def decode_uint256(hexstr: str) -> int:
    return int(hexstr, 16)


def decode_addr(hexstr: str) -> str:
    return "0x" + hexstr[-40:].lower()


def now_iso() -> str:
    return datetime.now(UTC).strftime("%Y-%m-%dT%H:%M:%SZ")


# ── 断点续扫状态 ──────────────────────────────────────────────────────────────

def load_state(path: Path) -> dict:
    if path.exists():
        try:
            return json.loads(path.read_text())
        except (json.JSONDecodeError, OSError):
            pass
    return {}


def save_state(path: Path, state: dict) -> None:
    tmp = path.with_suffix(".tmp")
    tmp.write_text(json.dumps(state))
    tmp.replace(path)


# ── 事件构建与落盘 ────────────────────────────────────────────────────────────

def write_event(out_dir: Path, ev: dict) -> None:
    out = out_dir / f"{datetime.now(UTC).strftime('%Y-%m-%d')}.jsonl"
    with open(out, "a") as fh:
        fh.write(json.dumps(ev, ensure_ascii=False) + "\n")


def base_event(event_type: str, event_id: str, key: str, symbol: str,
               title: str, message: str, data: dict) -> dict:
    return {
        "schema_version": "kairos.event.v1",
        "event_id": event_id,
        "ts": now_iso(),
        "floor": "launch",
        "event_type": event_type,
        "severity": "LOW",
        "key": key,
        "symbol": symbol,
        "title": title,
        "message": message,
        "strategy_id": "launch_intel_v1",
        "experiment_id": "exp-launch-collector-001",
        "mode": "shadow",
        "venue": "robinhood-chain",
        "direction": "neutral",
        "data": data,
    }


def token_created_event(log: dict, creator: str | None) -> dict:
    """入口合约 TokenCreated(address): token 在 topics[1](indexed)或 data。"""
    block = int(log["blockNumber"], 16)
    entry = log["address"].lower()
    if len(log.get("topics", [])) > 1:
        token = decode_addr(log["topics"][1])
    else:
        token = decode_addr(log["data"])
    return base_event(
        "launch_token_created",
        f"launch-tc-{block}-{token[:10]}",
        token, token,
        f"Launch: {token[:10]}…",
        f"entry={entry} block={block}",
        {
            "block": block,
            "entry_contract": entry,
            "tx_hash": log.get("transactionHash"),
            "creator": creator,
        },
    )


def auction_created_event(log: dict, snap: dict) -> dict:
    block = int(log["blockNumber"], 16)
    auction = decode_addr(log["topics"][1])
    token = decode_addr(log["topics"][2])
    amount = decode_uint256(log["data"]) if len(log.get("data", "")) >= 66 else 0
    return base_event(
        "launch_auction_created",
        f"launch-{block}-{auction[:10]}",
        auction, token,
        f"CCA Auction: {token[:8]}… amount={amount}",
        f"auction={auction} block={block}",
        {
            "block": block,
            "amount": amount,
            "factory": log["address"].lower(),
            "tx_hash": log.get("transactionHash"),
            "auction_state": snap,
        },
    )


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
        except (RPCError, ValueError):
            out[name] = None
    return out


# ── 扫描 ─────────────────────────────────────────────────────────────────────

def scan_segment(chain: Chain, from_block: int, to_block: int, out_dir: Path,
                 with_creator: bool = True) -> int:
    """扫单个区块段(调用方负责窗口大小),返回写入事件数。"""
    written = 0

    logs = chain.get_logs(ENTRY_CONTRACTS, TOPIC_TOKEN_CREATED, from_block, to_block)
    logs.sort(key=lambda l: int(l["blockNumber"], 16))
    for log in logs:
        creator = chain.get_tx_sender(log["transactionHash"]) if with_creator else None
        ev = token_created_event(log, creator)
        write_event(out_dir, ev)
        written += 1
        print(f"  [{ev['data']['block']}] token={ev['key']} creator={creator}")

    logs = chain.get_logs(FACTORIES, TOPIC_AUCTION_CREATED, from_block, to_block)
    logs.sort(key=lambda l: int(l["blockNumber"], 16))
    for log in logs:
        auction = decode_addr(log["topics"][1])
        snap = fetch_auction(chain, auction)
        ev = auction_created_event(log, snap)
        write_event(out_dir, ev)
        written += 1
        print(f"  [{ev['data']['block']}] auction={auction} token={ev['symbol']}")

    return written


def scan_range(chain: Chain, from_block: int, to_block: int, out_dir: Path,
               with_creator: bool = True, state_path: Path | None = None) -> int:
    """扫 [from_block, to_block],自适应窗口: 失败折半,成功回升。"""
    written = 0
    step = STEP_MAX
    cur = from_block
    while cur <= to_block:
        end = min(to_block, cur + step - 1)
        try:
            written += scan_segment(chain, cur, end, out_dir, with_creator)
        except RPCError as e:
            if step > STEP_MIN:
                step = max(STEP_MIN, step // 2)
                print(f"  getLogs [{cur},{end}] ERR: {e} -> 窗口降到 {step}")
                continue  # 同段折半重试
            print(f"  getLogs [{cur},{end}] ERR at min window, skip: {e}")
            # ponytail: 最小窗口仍失败则跳过并记录 gap,不伪造数据
            write_event(out_dir, base_event(
                "launch_scan_gap", f"launch-gap-{cur}-{end}", str(cur), "",
                f"scan gap {cur}-{end}", str(e), {"from_block": cur, "to_block": end},
            ))
        cur = end + 1
        step = min(STEP_MAX, step * 2)
        if state_path is not None:
            save_state(state_path, {"last_scanned_block": end})
        time.sleep(0.2)  # 公共 RPC 限流,段间小憩
    return written


# ── 命令 ─────────────────────────────────────────────────────────────────────

def probe(chain: Chain) -> int:
    cid = int(chain.call("eth_chainId", []), 16)
    bn = chain.block_number()
    ok = cid == CHAIN_ID
    print(f"rpc: {chain.rpc}")
    print(f"chainId: {cid} ({'✅' if ok else f'❌ 期望 {CHAIN_ID}'})  block: {bn}")
    checks = [
        ("Entry current", LAUNCH_ENTRY_CURRENT),
        ("Entry original", LAUNCH_ENTRY_ORIGINAL),
        ("CCA Factory v1", CCA_FACTORY),
        ("CCA Factory v2", CCA_FACTORY_V2),
        ("Token Factory", TOKEN_FACTORY),
    ]
    for name, addr in checks:
        code = chain.get_code(addr)
        has = bool(code and code != "0x")
        ok = ok and has
        print(f"  {name}: {'✅' if has else '❌'} {addr}")
    return 0 if ok else 1


def main() -> int:
    ap = argparse.ArgumentParser(description="Robinhood Chain Uniswap launchpad collector")
    sub = ap.add_subparsers(dest="cmd", required=True)

    sub.add_parser("probe")

    p_scan = sub.add_parser("scan")
    p_scan.add_argument("--from-block", type=int, required=True)
    p_scan.add_argument("--to-block", type=int, default=0)
    p_scan.add_argument("--out", default="out")
    p_scan.add_argument("--no-creator", action="store_true", help="跳过 creator 查询(省 RPC 配额)")

    p_watch = sub.add_parser("watch")
    p_watch.add_argument("--from-block", type=int, default=0, help="0=续用 state.json,无 state 则从当前块开始")
    p_watch.add_argument("--out", default="out")
    p_watch.add_argument("--interval-seconds", type=int, default=30, help="轮询间隔(块时间~100ms,按秒规划)")
    p_watch.add_argument("--no-creator", action="store_true")
    p_watch.add_argument("--once", action="store_true")

    args = ap.parse_args()
    chain = Chain()

    if args.cmd == "probe":
        return probe(chain)

    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    with_creator = not args.no_creator

    if args.cmd == "scan":
        to = args.to_block or chain.block_number()
        n = scan_range(chain, args.from_block, to, out, with_creator)
        print(f"scan done: {n} events written -> {out}")
        return 0

    if args.cmd == "watch":
        state_path = out / "state.json"
        state = load_state(state_path)
        cur = args.from_block or (state.get("last_scanned_block", 0) + 1) or chain.block_number()
        if cur <= 1:
            cur = chain.block_number()
        print(f"[{now_iso()}] watch from block {cur} (state: {state_path})")
        while True:
            try:
                head = chain.block_number()
                if head >= cur:
                    n = scan_range(chain, cur, head, out, with_creator, state_path)
                    print(f"[{now_iso()}] blocks {cur}..{head}: {n} events")
                    cur = head + 1
            except RPCError as e:
                print(f"[{now_iso()}] rpc error, retrying next cycle: {e}")
            if args.once:
                break
            time.sleep(args.interval_seconds)
        return 0

    return 1


if __name__ == "__main__":
    sys.exit(main())
