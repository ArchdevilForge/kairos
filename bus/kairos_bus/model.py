"""Event model for the bus.

JSONL 每行一个事件。字段宽松解析:未知字段保留进 ``data``。
"""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import UTC, datetime

SEVERITY_RANK = {"LOW": 0, "MEDIUM": 1, "HIGH": 2}


def now_iso() -> str:
    return datetime.now(UTC).strftime("%Y-%m-%dT%H:%M:%SZ")


SCHEMA_VERSION = "kairos.event.v1"


def new_event_id(floor: str, symbol: str, event_type: str, ts: str) -> str:
    """短稳定事件 id:<floor>-<ts>-<event_type>-<symbol>,去重友好。"""
    return f"{floor}-{ts}-{event_type}-{symbol}"


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
    # 研究元数据(schemas/event.v1.json):缺省不阻断,但 attribution 依赖它们
    schema_version: str = SCHEMA_VERSION
    event_id: str = ""
    parent_event_id: str = ""
    strategy_id: str = ""
    experiment_id: str = ""
    mode: str = ""                     # shadow | paper | live
    venue: str = ""
    direction: str = ""                # up | down | neutral
    observed_at: str = ""

    @property
    def dedup_key(self) -> str:
        return f"{self.floor}__{self.event_type}__{self.key or self.symbol or 'default'}"

    @property
    def severity_rank(self) -> int:
        return SEVERITY_RANK.get(self.severity.upper(), 0)

    def to_json(self) -> dict:
        out = {
            "schema_version": self.schema_version or SCHEMA_VERSION,
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
        meta = {
            "event_id": self.event_id or new_event_id(self.floor, self.symbol, self.event_type, self.ts),
            "parent_event_id": self.parent_event_id,
            "strategy_id": self.strategy_id,
            "experiment_id": self.experiment_id,
            "mode": self.mode,
            "venue": self.venue,
            "direction": self.direction,
            "observed_at": self.observed_at,
        }
        out.update({k: v for k, v in meta.items() if v})
        return out

    @classmethod
    def from_dict(cls, raw: dict, floor: str = "") -> Event:
        known = {"ts", "event_type", "severity", "key", "symbol", "title", "message", "floor",
                 "schema_version", "event_id", "parent_event_id", "strategy_id",
                 "experiment_id", "mode", "venue", "direction", "observed_at"}
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
            schema_version=str(raw.get("schema_version") or SCHEMA_VERSION),
            event_id=str(raw.get("event_id", "")),
            parent_event_id=str(raw.get("parent_event_id", "")),
            strategy_id=str(raw.get("strategy_id", "")),
            experiment_id=str(raw.get("experiment_id", "")),
            mode=str(raw.get("mode", "")),
            venue=str(raw.get("venue", "")),
            direction=str(raw.get("direction", "")),
            observed_at=str(raw.get("observed_at", "")),
        )
        return evt
