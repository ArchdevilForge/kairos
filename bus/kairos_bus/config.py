"""Config loading — tomllib, 无第三方依赖。"""

from __future__ import annotations

import tomllib
from dataclasses import dataclass, field
from pathlib import Path

from .model import SEVERITY_RANK


@dataclass
class FloorConfig:
    name: str
    source: str
    allowed_event_types: list[str] = field(default_factory=list)
    min_severity: str = "LOW"
    blacklist: list[str] = field(default_factory=list)
    skip_cooldown_event_types: list[str] = field(default_factory=list)
    never_notify_event_types: list[str] = field(default_factory=lambda: ["outcome", "calibration"])

    @property
    def min_severity_rank(self) -> int:
        return SEVERITY_RANK.get(self.min_severity.upper(), 0)


@dataclass
class BusConfig:
    inbound_dir: Path
    out_dir: Path
    poll_seconds: int = 5
    dedup_window_seconds: int = 5
    symbol_cooldown_seconds: int = 1800
    max_alerts_per_day: int = 200
    telegram_enabled: bool = True
    telegram_token_env: str = "KAIBUS_TG_TOKEN"
    telegram_chat_env: str = "KAIBUS_TG_CHAT"
    floors: dict[str, FloorConfig] = field(default_factory=dict)

    def floor(self, name: str) -> FloorConfig | None:
        return self.floors.get(name)

    @classmethod
    def load(cls, path: str | Path) -> BusConfig:
        with open(path, "rb") as f:
            raw = tomllib.load(f)
        bus = raw.get("bus", {})
        tg = bus.get("telegram", {})
        floors: dict[str, FloorConfig] = {}
        for name, fc in (raw.get("floors") or {}).items():
            floors[name] = FloorConfig(
                name=name,
                source=str(fc.get("source", name)),
                allowed_event_types=list(fc.get("allowed_event_types", [])),
                min_severity=str(fc.get("min_severity", "LOW")).upper(),
                blacklist=list(fc.get("blacklist", [])),
                skip_cooldown_event_types=list(fc.get("skip_cooldown_event_types", [])),
                never_notify_event_types=list(fc.get("never_notify_event_types", ["outcome", "calibration"])),
            )
        return cls(
            inbound_dir=Path(bus.get("inbound_dir", "inbound")),
            out_dir=Path(bus.get("out_dir", "out")),
            poll_seconds=int(bus.get("poll_seconds", 5)),
            dedup_window_seconds=int(bus.get("dedup_window_seconds", 5)),
            symbol_cooldown_seconds=int(bus.get("symbol_cooldown_seconds", 1800)),
            max_alerts_per_day=int(bus.get("max_alerts_per_day", 200)),
            telegram_enabled=bool(tg.get("enabled", True)),
            telegram_token_env=str(tg.get("bot_token_env", "KAIBUS_TG_TOKEN")),
            telegram_chat_env=str(tg.get("chat_id_env", "KAIBUS_TG_CHAT")),
            floors=floors,
        )
