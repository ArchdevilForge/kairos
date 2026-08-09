"""聚合 JSONL 输出 — calibration 与内容管线的唯一数据出口。

记录所有事件(含被 gate 拒绝的)及 gate/send 结果,每天一个文件:
out/YYYY-MM-DD.jsonl
"""

from __future__ import annotations

import json
from datetime import UTC, datetime
from pathlib import Path

from .model import Event


class JsonlSink:
    def __init__(self, out_dir: Path) -> None:
        self.out_dir = out_dir
        self.out_dir.mkdir(parents=True, exist_ok=True)

    def _day(self, evt: Event) -> str:
        try:
            return evt.ts[:10]
        except (TypeError, AttributeError):  # ts 异常时退回当天
            return datetime.now(UTC).strftime("%Y-%m-%d")

    def record(self, evt: Event, allowed: bool, reason: str | None, sent: bool) -> None:
        row = {
            "event": evt.to_json(),
            "gate": {"allowed": allowed, "reason": reason},
            "sent": sent,
        }
        path = self.out_dir / f"{self._day(evt)}.jsonl"
        with open(path, "a", encoding="utf-8") as f:
            f.write(json.dumps(row, ensure_ascii=False) + "\n")
