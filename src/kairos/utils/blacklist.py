"""Blacklist — set of symbols to skip, loaded from a text file."""

from __future__ import annotations

from pathlib import Path


class Blacklist:
    """Read-only blacklist. One symbol per line in ~/.config/kairos/blacklist.txt."""

    def __init__(self) -> None:
        path = Path.home() / ".config" / "kairos" / "blacklist.txt"
        self._blocked: set[str] = set()
        if path.exists():
            self._blocked = {line.strip().upper() for line in path.read_text().splitlines() if line.strip()}

    def is_blocked(self, symbol: str) -> bool:
        return symbol.upper() in self._blocked
