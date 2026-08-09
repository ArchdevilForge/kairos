"""Weather trading strategies.

Currently no active strategies. Framework preserved for future implementation.
See: .trellis/spec/backend/trading-config.md for strategy design history.
"""

from pm_bot.strategies.base import ALL_STRATEGIES, Strategy

__all__ = [
    "ALL_STRATEGIES",
    "Strategy",
]
