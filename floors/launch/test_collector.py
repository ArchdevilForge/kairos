"""launch_collector 最小自检: 签名哈希、解码、事件构建、断点状态、自适应窗口。"""
import json
from pathlib import Path

import launch_collector as lc


def test_topic_hashes_match_public_record():
    """topic0 与 Bitquery pools.trade 公开口径逐一核对——签名字符串写错则采不到数据。"""
    assert lc.TOPIC_TOKEN_CREATED == "0x2e2b3f61b70d2d131b2a807371103cc98d51adcaa5e9a8f9c32658ad8426e74e"
    assert lc.TOPIC_AUCTION_CREATED == "0x7ede475fad18ccf0039f2b956c4d43a8b4ed0853de4daaa8ae25299f331ae3b9"
    assert lc.TOPIC_TOKEN_DISTRIBUTED == "0x67226bacccef969dab310a9e55dc1cf821363658e433fd330344f5cc00c79ac8"
    assert lc.TOPIC_BID_SUBMITTED == "0x650baad5cd8ca09b8f580be220fa04ce2ba905a041f764b6a3fe2c848eb70540"
    assert lc.TOPIC_CLEARING_PRICE_UPDATED == "0x30adbe996d7a69a21fdebcc1f8a46270bf6c22d505a7d872c1ab4767aa707609"


def test_entry_contracts_both_present():
    """两个入口合约都必须监听(只监听一个漏 ~40% launch)。"""
    assert len(lc.ENTRY_CONTRACTS) == 2
    assert lc.LAUNCH_ENTRY_CURRENT in lc.ENTRY_CONTRACTS
    assert lc.LAUNCH_ENTRY_ORIGINAL in lc.ENTRY_CONTRACTS


def test_decode_helpers():
    assert lc.decode_uint256("0x0f") == 15
    assert lc.decode_addr("0x" + "00" * 12 + "ab" * 20) == "0x" + "ab" * 20


def test_token_created_event_indexed_topic():
    log = {
        "blockNumber": hex(31_900_000),
        "address": lc.LAUNCH_ENTRY_CURRENT,
        "topics": [lc.TOPIC_TOKEN_CREATED, "0x" + "00" * 12 + "aa" * 20],
        "data": "0x",
        "transactionHash": "0x" + "11" * 32,
    }
    ev = lc.token_created_event(log, creator="0x" + "bb" * 20)
    assert ev["event_type"] == "launch_token_created"
    assert ev["schema_version"] == "kairos.event.v1"
    assert ev["key"] == "0x" + "aa" * 20
    assert ev["data"]["entry_contract"] == lc.LAUNCH_ENTRY_CURRENT
    assert ev["data"]["creator"] == "0x" + "bb" * 20
    assert ev["mode"] == "shadow"


def test_token_created_event_from_data():
    """未 indexed 的变体: token 地址在 data 里。"""
    log = {
        "blockNumber": hex(1),
        "address": lc.LAUNCH_ENTRY_ORIGINAL,
        "topics": [lc.TOPIC_TOKEN_CREATED],
        "data": "0x" + "00" * 12 + "cc" * 20,
        "transactionHash": None,
    }
    ev = lc.token_created_event(log, creator=None)
    assert ev["key"] == "0x" + "cc" * 20


def test_state_roundtrip(tmp_path: Path):
    p = tmp_path / "state.json"
    assert lc.load_state(p) == {}
    lc.save_state(p, {"last_scanned_block": 42})
    assert lc.load_state(p) == {"last_scanned_block": 42}
    p.write_text("not json")
    assert lc.load_state(p) == {}  # 损坏状态不崩溃


class FlakyChain:
    """前 N 段 getLogs 抛错,验证自适应窗口折半后继续推进且状态持久化。"""

    def __init__(self, fail_first: int):
        self.fail_first = fail_first
        self.calls = 0

    def get_logs(self, addresses, topic0, from_block, to_block):
        self.calls += 1
        if self.calls <= self.fail_first:
            raise lc.RPCError("range too large")
        return []

    def get_tx_sender(self, tx_hash):
        return None


def test_scan_range_adaptive_window(tmp_path: Path, monkeypatch):
    monkeypatch.setattr(lc.time, "sleep", lambda *_: None)
    chain = FlakyChain(fail_first=2)
    state_path = tmp_path / "state.json"
    n = lc.scan_range(chain, 1, 40_000, tmp_path, with_creator=False, state_path=state_path)
    assert n == 0  # 空链无事件,但扫描必须完整推进
    assert lc.load_state(state_path)["last_scanned_block"] == 40_000
    assert chain.calls > 2  # 折半后重试过


# ── PR2: CCA demand ──────────────────────────────────────────────────────────

# 链上真实事件(2026-08-12 采样)作 fixture
REAL_BID_LOG = {
    "blockNumber": hex(34447871),
    "address": "0x18ba60ae97015e6ae813f8d20daaf84c72bc6b72",
    "topics": [
        "0x650baad5cd8ca09b8f580be220fa04ce2ba905a041f764b6a3fe2c848eb70540",
        "0x000000000000000000000000000000000000000000000000000000000000000a",
        "0x00000000000000000000000014d858c5c4c65f91a8118e8b1dc8497fd1432cea",
    ],
    "data": "0x00000000000000000000000000000000000000000001a36e2e2e0d5b228b1f76"
            "0000000000000000000000000000000000000000000000000001dbaa6d37b000",
}
REAL_CLEARING_LOG = {
    "blockNumber": hex(34463983),
    "address": "0x470710409251f82cdad948f112a1e6f534c336eb",
    "topics": ["0x30adbe996d7a69a21fdebcc1f8a46270bf6c22d505a7d872c1ab4767aa707609"],
    "data": "0x00000000000000000000000000000000000000000000000000000000020de0ef"
            "0000000000000000000000000000000000000000000000023fc214aeb61ce79c",
}


def test_parse_bid_real_log():
    b = lc.parse_bid(REAL_BID_LOG)
    assert b["tick"] == 10
    assert b["bidder"] == "0x14d858c5c4c65f91a8118e8b1dc8497fd1432cea"
    assert b["amount"] == 0x1A36E2E2E0D5B228B1F76
    assert b["limit"] == 0x1DBAA6D37B000


def test_parse_clearing_real_log():
    c = lc.parse_clearing(REAL_CLEARING_LOG)
    assert c["checkpoint_block"] == 34463983
    assert c["price"] == 0x23FC214AEB61CE79C


def test_demand_features_shares():
    agg: dict = {}
    lc.aggregate_bids(agg, [
        {"bidder": "0xa", "amount": 60},
        {"bidder": "0xb", "amount": 30},
        {"bidder": "0xa", "amount": 40},  # 同 bidder 累计 → 0xa=100
        {"bidder": "0xc", "amount": 10},
    ])
    f = lc.demand_features(agg)
    assert f["unique_bidders"] == 3
    assert f["bid_count"] == 4
    assert f["amount_total"] == "140"
    assert f["top1_share"] == round(100 / 140, 4)
    assert f["top5_share"] == 1.0


def test_demand_features_empty():
    f = lc.demand_features({})
    assert f == {"unique_bidders": 0, "bid_count": 0, "amount_total": "0",
                 "top1_share": 0.0, "top5_share": 0.0}


class AuctionChain:
    """假链: 一个活跃 auction,先出 bid,过 end_block+grace 后应收盘并从 state 移除。"""

    def __init__(self):
        self.bid_logs = [REAL_BID_LOG]

    def get_logs(self, addresses, topic0, from_block, to_block):
        if topic0 == lc.TOPIC_BID_SUBMITTED:
            out, self.bid_logs = self.bid_logs, []
            return out
        return []

    def call_contract(self, addr, sig):
        return "0x" + "00" * 32  # 收盘快照全 0


def test_poll_auctions_lifecycle(tmp_path: Path):
    chain = AuctionChain()
    auction = "0x18ba60ae97015e6ae813f8d20daaf84c72bc6b72"
    state: dict = {}
    lc.register_auction(state, auction, {"token": "0xTOK", "startBlock": 100, "endBlock": 200}, 100)

    # 活跃期: 收到 bid → 出 demand 快照
    n = lc.poll_auctions(chain, state, tmp_path, head=150)
    assert n == 1
    assert state["auctions"][auction]["bid_count"] == 1
    events = [json.loads(l) for f in tmp_path.glob("*.jsonl") for l in f.read_text().splitlines()]
    assert events[-1]["event_type"] == "launch_auction_update"
    assert events[-1]["data"]["unique_bidders"] == 1

    # 无新 bid → 不刷屏
    assert lc.poll_auctions(chain, state, tmp_path, head=160) == 0

    # 过 end_block + grace → 收盘事件 + 移出 state
    n = lc.poll_auctions(chain, state, tmp_path, head=200 + lc.AUCTION_GRACE_BLOCKS + 1)
    assert n == 1
    assert auction not in state["auctions"]
    events = [json.loads(l) for f in tmp_path.glob("*.jsonl") for l in f.read_text().splitlines()]
    assert events[-1]["event_type"] == "launch_auction_closed"


def test_auction_created_amount_first_word():
    """v1 factory 的 data 是 struct,amount 只取第一个字(整段解析是旧 bug)。"""
    log = {
        "blockNumber": hex(1),
        "address": lc.CCA_FACTORY,
        "topics": [lc.TOPIC_AUCTION_CREATED,
                   "0x" + "00" * 12 + "aa" * 20, "0x" + "00" * 12 + "bb" * 20],
        "data": "0x" + hex(500)[2:].rjust(64, "0") + "ff" * 64,  # word0=500 + 后续 struct 噪声
        "transactionHash": None,
    }
    ev = lc.auction_created_event(log, snap={})
    assert ev["data"]["amount"] == 500


def test_scan_range_min_window_gap(tmp_path: Path, monkeypatch):
    """最小窗口仍失败 → 写 gap 事件不伪造数据,且不死循环。"""
    monkeypatch.setattr(lc.time, "sleep", lambda *_: None)

    class DeadChain:
        def get_logs(self, *a):
            raise lc.RPCError("permanently down")

        def get_tx_sender(self, tx_hash):
            return None

    lc.scan_range(DeadChain(), 1, 1_000, tmp_path, with_creator=False)
    files = list(tmp_path.glob("*.jsonl"))
    assert files, "gap 事件必须落盘"
    lines = [json.loads(l) for l in files[0].read_text().splitlines()]
    assert all(e["event_type"] == "launch_scan_gap" for e in lines)
