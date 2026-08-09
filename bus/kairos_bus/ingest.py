"""JSONL 摄入 — 每楼层一个目录,按文件 offset 增量读取。

楼层落盘约定:inbound/<floor>/*.jsonl,每行一个事件 JSON。
行内 "floor" 字段可覆盖目录名;目录名默认就是 floor 名。
"""

from __future__ import annotations

import json
from pathlib import Path

from .config import BusConfig
from .model import Event


class JsonlTailer:
    """单个文件:记住已读 offset,新追加行解析为 Event。"""

    def __init__(self, path: Path) -> None:
        self.path = path
        self.offset = 0

    def poll(self) -> tuple[list[Event], int]:
        """返回 (事件列表, malformed 行数)。文件被轮转/截断则从头读。"""
        size = self.path.stat().st_size
        if size < self.offset:
            self.offset = 0
        events: list[Event] = []
        malformed = 0
        if size == self.offset:
            return events, malformed
        with open(self.path, encoding="utf-8") as f:
            f.seek(self.offset)
            lines = f.readlines()
            self.offset = f.tell()
        for line in lines:
            line = line.strip()
            if not line:
                continue
            try:
                raw = json.loads(line)
                evt = Event.from_dict(raw)
                if not evt.event_type:
                    malformed += 1
                else:
                    events.append(evt)
            except (json.JSONDecodeError, TypeError, ValueError):
                malformed += 1
        return events, malformed


class Ingestor:
    """遍历所有楼层目录,聚合增量事件。"""

    def __init__(self, cfg: BusConfig) -> None:
        self.cfg = cfg
        self.tailers: dict[Path, JsonlTailer] = {}

    def _tailer(self, path: Path) -> JsonlTailer:
        if path not in self.tailers:
            self.tailers[path] = JsonlTailer(path)
        return self.tailers[path]

    def poll_once(self) -> tuple[list[Event], int]:
        """返回 (本批事件, 本批 malformed 行数)。事件 floor 由目录名补全。"""
        events: list[Event] = []
        malformed = 0
        for floor_cfg in self.cfg.floors.values():
            src = self.cfg.inbound_dir / floor_cfg.source
            if not src.exists():
                continue
            for path in sorted(src.glob("*.jsonl")):
                evts, bad = self._tailer(path).poll()
                malformed += bad
                for evt in evts:
                    if not evt.floor:
                        evt.floor = floor_cfg.name
                    events.append(evt)
        return events, malformed
