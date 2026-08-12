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
