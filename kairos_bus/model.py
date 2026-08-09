"""Event model for the bus.

JSONL 每行一个事件。字段宽松解析:未知字段保留进 ``data``。
"""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import UTC, datetime

SEVERITY_RANK = {"LOW": 0, "MEDIUM": 1, "HIGH": 2}


def now_iso() -> str:
    return datetime.now(UTC).strftime("%Y-%m-%dT%H:%M:%SZ")


@dataclass
class Event:
    event_type: str = ""
    severity: str = "LOW"
    key: str = ""                      # 事件去重键(event_type 内)
    symbol: str = ""                   # cooldown 键
    title: str = ""
    message: str = ""
    data: dict = field(default_factory=dict)
    floor: str = ""                    # 目录名补全,行内可覆盖
    ts: str = field(default_factory=now_iso)

    @property
    def dedup_key(self) -> str:
        return f"{self.floor}__{self.event_type}__{self.key or self.symbol or 'default'}"

    @property
    def severity_rank(self) -> int:
        return SEVERITY_RANK.get(self.severity.upper(), 0)

    def to_json(self) -> dict:
        return {
            "ts": self.ts,
            "floor": self.floor,
            "event_type": self.event_type,
            "severity": self.severity.upper(),
            "key": self.key,
            "symbol": self.symbol,
            "title": self.title,
            "message": self.message,
            "data": self.data,
        }

    @classmethod
    def from_dict(cls, raw: dict, floor: str = "") -> Event:
        known = {"ts", "event_type", "severity", "key", "symbol", "title", "message", "floor"}
        data = {k: v for k, v in raw.items() if k not in known}
        evt = cls(
            event_type=str(raw.get("event_type", "")),
            severity=str(raw.get("severity", "LOW")).upper(),
            key=str(raw.get("key", "")),
            symbol=str(raw.get("symbol", "")),
            title=str(raw.get("title", "")),
            message=str(raw.get("message", "")),
            floor=str(raw.get("floor") or floor),
            ts=str(raw.get("ts") or now_iso()),
            data=data,
        )
        return evt
