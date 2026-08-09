from __future__ import annotations

import re

from pm_bot.models.market import TemperatureBucket

# Pattern: (compiled regex, unit, is_tail_low, is_tail_high)
# is_tail_low=True means "X or below" → low=-inf (use -999)
# is_tail_high=True means "X or higher" → high=+inf (use 999)
_PATTERNS: list[tuple[re.Pattern[str], str, bool, bool]] = [
    # "between 23-24°F" (Polymarket primary format)
    (re.compile(r"between\s+(\d+)\s*-\s*(\d+)°F", re.I), "F", False, False),
    # "between 23-24°C"
    (re.compile(r"between\s+(\d+)\s*-\s*(\d+)°C", re.I), "C", False, False),
    # "16°F or below"
    (re.compile(r"(\d+)°F\s+or\s+below", re.I), "F", True, False),
    # "16°C or below"
    (re.compile(r"(\d+)°C\s+or\s+below", re.I), "C", True, False),
    # "27°F or higher"
    (re.compile(r"(\d+)°F\s+or\s+higher", re.I), "F", False, True),
    # "27°C or higher"
    (re.compile(r"(\d+)°C\s+or\s+higher", re.I), "C", False, True),
    # "≥ 25°C and < 26°C"
    (re.compile(r"≥\s*(\d+)°C\s+and\s*<\s*(\d+)°C", re.I), "C", False, False),
    # "25°C to 26°C"
    (re.compile(r"(\d+)°C\s+to\s*(\d+)°C", re.I), "C", False, False),
    # "≥ 90°F and < 95°F"
    (re.compile(r"≥\s*(\d+)°F\s+and\s*<\s*(\d+)°F", re.I), "F", False, False),
    # "90°F to 95°F"
    (re.compile(r"(\d+)°F\s+to\s*(\d+)°F", re.I), "F", False, False),
    # "25 - 26°C" range with dash (no "between")
    (re.compile(r"(\d+)\s*-\s*(\d+)°C", re.I), "C", False, False),
    # "90 - 95°F" range with dash
    (re.compile(r"(\d+)\s*-\s*(\d+)°F", re.I), "F", False, False),
    # "31°C" (single value)
    (re.compile(r"(\d+)°C", re.I), "C", False, False),
    # "90°F" (single value)
    (re.compile(r"(\d+)°F", re.I), "F", False, False),
]


def parse_bucket(
    question: str,
    market_id: str = "",
    yes_price: float = 0.0,
    no_price: float = 0.0,
    volume: float = 0.0,
) -> TemperatureBucket | None:
    for pattern, unit, is_tail_low, is_tail_high in _PATTERNS:
        m = pattern.search(question)
        if m:
            if is_tail_low:
                low = -999.0
                high = float(m.group(1))
            elif is_tail_high:
                low = float(m.group(1))
                high = 999.0
            else:
                low = float(m.group(1))
                high = float(m.group(2)) if m.lastindex and m.lastindex >= 2 else low
            return TemperatureBucket(
                market_id=market_id,
                question=question,
                temp_low=low,
                temp_high=high,
                temp_unit=unit,
                yes_price=yes_price,
                no_price=no_price,
                volume=volume,
            )
    return None
