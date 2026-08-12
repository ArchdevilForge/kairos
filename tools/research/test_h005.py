"""h005 判定报告自检: 合成 40 个已收盘 auction,验证分位统计与输出。"""
import json
from pathlib import Path

import research


def make_closed(auction: str, bidders: int, graduated: bool) -> dict:
    return {
        "schema_version": "kairos.event.v1",
        "event_id": f"launch-ac-1-{auction}",
        "ts": "2026-08-12T00:00:00Z",
        "floor": "launch",
        "event_type": "launch_auction_closed",
        "key": auction,
        "symbol": "0xtok",
        "data": {
            "unique_bidders": bidders,
            "bid_count": bidders * 2,
            "amount_total": str(bidders * 1000),
            "top1_share": 0.5,
            "top5_share": 0.9,
            "auction_state": {"isGraduated": 1 if graduated else 0},
        },
    }


def test_h005_report(tmp_path: Path, capsys):
    # 40 个样本: bidders 越多 graduation 越高(合成正相关,分位表应单调)
    events = [make_closed(f"0xa{i:02d}", bidders=i, graduated=(i > 20)) for i in range(1, 41)]
    f = tmp_path / "2026-08-12.jsonl"
    f.write_text("\n".join(json.dumps(e) for e in events) + "\n")

    out_md = tmp_path / "report.md"
    rc = research.h005(str(tmp_path), str(out_md))
    assert rc == 0
    text = out_md.read_text()
    assert "已收盘 CCA auction 样本: 40" in text
    assert "graduation 率" in text
    # Q1(bidders 1-10)全未毕业,Q4(31-40)全毕业
    lines = [l for l in text.splitlines() if l.startswith("| 1 ") or l.startswith("| 4 ")]
    assert "0.0" in lines[0] and "1.0" in lines[1]


def test_h005_insufficient_sample(tmp_path: Path):
    f = tmp_path / "d.jsonl"
    f.write_text(json.dumps(make_closed("0xa", 5, False)) + "\n")
    rc = research.h005(str(tmp_path), None)
    assert rc == 0  # 样本不足仍正常退出,只是不下结论


def test_h005_no_data(tmp_path: Path):
    assert research.h005(str(tmp_path / "missing"), None) == 1
