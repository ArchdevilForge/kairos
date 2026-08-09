"""摄入测试:增量读取、malformed 跳过、floor 补全。"""

import json

from kairos_bus.config import BusConfig, FloorConfig
from kairos_bus.ingest import Ingestor, JsonlTailer


def write_lines(path, lines):
    path.parent.mkdir(parents=True, exist_ok=True)
    with open(path, "a", encoding="utf-8") as f:
        f.writelines(line + "\n" for line in lines)


def make_cfg(tmp_path) -> BusConfig:
    cfg = BusConfig(inbound_dir=tmp_path / "inbound", out_dir=tmp_path / "out")
    cfg.floors = {
        "pm": FloorConfig(name="pm", source="pm", allowed_event_types=["spread_alert"]),
    }
    (cfg.inbound_dir / "pm").mkdir(parents=True, exist_ok=True)
    return cfg


def test_tailer_reads_only_new_lines(tmp_path):
    p = tmp_path / "a.jsonl"
    write_lines(p, [json.dumps({"event_type": "x", "key": "1"})])
    t = JsonlTailer(p)
    events, bad = t.poll()
    assert len(events) == 1 and bad == 0
    write_lines(p, [json.dumps({"event_type": "y", "key": "2"})])
    events, bad = t.poll()
    assert len(events) == 1 and events[0].key == "2"


def test_tailer_skips_malformed_and_empty(tmp_path):
    p = tmp_path / "b.jsonl"
    write_lines(p, ["not json", "", json.dumps({"event_type": "x"}), json.dumps({"key": "no-type"})])
    t = JsonlTailer(p)
    events, bad = t.poll()
    assert [e.event_type for e in events] == ["x"]
    assert bad == 2


def test_tailer_restarts_after_truncation(tmp_path):
    p = tmp_path / "c.jsonl"
    write_lines(p, [json.dumps({"event_type": "x", "key": "long-payload"})])
    t = JsonlTailer(p)
    assert len(t.poll()[0]) == 1
    # 轮转:文件被截断为更短的新内容(size < offset → 从头读)
    p.write_text(json.dumps({"event_type": "y"}) + "\n")
    events, _ = t.poll()
    assert [e.event_type for e in events] == ["y"]


def test_ingestor_fills_floor_from_dirname(tmp_path):
    cfg = make_cfg(tmp_path)
    write_lines(cfg.inbound_dir / "pm" / "1.jsonl", [json.dumps({"event_type": "spread_alert", "key": "k"})])
    ing = Ingestor(cfg)
    events, bad = ing.poll_once()
    assert bad == 0
    assert len(events) == 1
    assert events[0].floor == "pm"


def test_ingestor_event_floor_overrides_dirname(tmp_path):
    cfg = make_cfg(tmp_path)
    write_lines(cfg.inbound_dir / "pm" / "1.jsonl",
                [json.dumps({"event_type": "spread_alert", "key": "k", "floor": "other"})])
    ing = Ingestor(cfg)
    events, _ = ing.poll_once()
    assert events[0].floor == "other"


def test_ingestor_idempotent_second_poll(tmp_path):
    cfg = make_cfg(tmp_path)
    write_lines(cfg.inbound_dir / "pm" / "1.jsonl", [json.dumps({"event_type": "spread_alert", "key": "k"})])
    ing = Ingestor(cfg)
    assert len(ing.poll_once()[0]) == 1
    assert len(ing.poll_once()[0]) == 0
