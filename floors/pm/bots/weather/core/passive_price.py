"""Passive (maker) limit pricing for weather markets.

Ported from ``weatherbot/paper_execution.py`` (Xeron2000/weatherbot, archived
2026-08-09) during the weatherbot -> pm-bot absorb. These helpers compute the
price at which a passive YES/NO order should rest in the book instead of
paying the spread, so edge checks and paper fills reflect maker reality.

Usage:
    from pm_bot.core.passive_price import compute_passive_limit_price

    price, err = compute_passive_limit_price(quote["yes"], DEFAULT_ORDER_POLICY)
"""

from __future__ import annotations

from typing import Any

# Order policy defaults, inherited from weatherbot config.json.
# GTD orders expire after max_order_hours_open; GTC rest until cancelled.
DEFAULT_ORDER_POLICY: dict[str, Any] = {
    "yes_time_in_force": "GTC",
    "gtd_buffer_hours": 6.0,
    "price_improve_ticks": 1,
    "replace_edge_buffer": 0.02,
    "max_order_hours_open": 72.0,
}

# NO orders anchor at fair_no minus this offset, then step to the tick grid.
_NO_ANCHOR_OFFSET = 0.10


def compute_passive_limit_price(
    side_quote: dict[str, Any],
    policy: dict[str, Any] = DEFAULT_ORDER_POLICY,
) -> tuple[float | None, str | None]:
    """Best rest price for a passive buy on ``side_quote``.

    ``side_quote`` must carry ``bid``, ``ask`` and ``tick_size``. The result
    improves the bid by ``policy["price_improve_ticks"]`` ticks but never
    crosses the ask. Returns ``(price, None)`` or ``(None, reason)`` where
    ``reason`` is a stable machine key.
    """

    bid = side_quote.get("bid")
    ask = side_quote.get("ask")
    tick_size = side_quote.get("tick_size")
    if tick_size is None:
        return None, "tick_size_missing"
    try:
        tick_size = float(tick_size)
    except Exception:
        return None, "tick_size_missing"
    if tick_size <= 0:
        return None, "tick_size_missing"
    if bid is None or ask is None:
        return None, "quote_price_missing"
    try:
        bid = float(bid)
        ask = float(ask)
    except Exception:
        return None, "quote_price_missing"
    improve_ticks = int(policy.get("price_improve_ticks", 0) or 0)
    candidate = bid + (tick_size * improve_ticks)
    candidate = max(candidate, bid)
    if ask > bid:
        candidate = min(candidate, ask - tick_size)
    candidate = round(candidate, 6)
    if candidate <= 0:
        return None, "quote_price_missing"
    return round(candidate, 4), None


def compute_no_anchored_limit_price(
    side_quote: dict[str, Any],
    fair_no: float | None,
    policy: dict[str, Any] = DEFAULT_ORDER_POLICY,
) -> tuple[float | None, str | None]:
    """Anchored passive NO price: ``fair_no - 0.10``, stepped to the tick grid.

    The anchor offset is a weatherbot legacy constant: NO is rarely traded,
    so the limit rests visibly below fair value instead of chasing the ask.
    """

    bid = side_quote.get("bid")
    ask = side_quote.get("ask")
    tick_size = side_quote.get("tick_size")
    if tick_size is None:
        return None, "tick_size_missing"
    try:
        tick_size = float(tick_size)
    except Exception:
        return None, "tick_size_missing"
    if tick_size <= 0:
        return None, "tick_size_missing"
    if bid is None or ask is None:
        return None, "quote_price_missing"
    try:
        bid = float(bid)
        ask = float(ask)
    except Exception:
        return None, "quote_price_missing"
    if fair_no is None:
        return None, "fair_value_missing"
    try:
        fair_no = float(fair_no)
    except Exception:
        return None, "fair_value_missing"

    anchored_target = fair_no - _NO_ANCHOR_OFFSET
    candidate = anchored_target
    if ask > bid:
        candidate = min(candidate, ask - tick_size)
    candidate = int(candidate / tick_size) * tick_size
    candidate = round(candidate, 6)
    if candidate <= 0:
        return None, "quote_price_missing"
    return round(candidate, 4), None
