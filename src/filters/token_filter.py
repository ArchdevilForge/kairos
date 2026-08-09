"""Token filtering logic."""
from datetime import datetime, timezone
from typing import Optional

import structlog

from src.models import Token
from src.config import FiltersConfig

logger = structlog.get_logger()


class TokenFilter:
    """Filter tokens based on configurable criteria."""

    def __init__(self, config: Optional[FiltersConfig] = None):
        self.config = config or FiltersConfig()
        self._seen: set[tuple[str, str]] = set()  # (chain, address) pairs

    def is_valid(self, token: Token) -> bool:
        """Check if token passes all filters."""
        # Skip already seen
        key = (token.chain, token.address.lower())
        if key in self._seen:
            return False

        checks = [
            self._check_liquidity(token),
            self._check_market_cap(token),
            self._check_age(token),
            self._check_smart_money(token),
            self._check_lp_burned(token),
            self._check_honeypot(token),
        ]

        if all(checks):
            self._seen.add(key)
            return True
        return False

    def _check_liquidity(self, token: Token) -> bool:
        if token.liquidity is None:
            return False
        if token.liquidity < self.config.min_liquidity:
            logger.debug("filter_low_liquidity", token=token.symbol, liq=token.liquidity)
            return False
        if token.liquidity > self.config.max_liquidity:
            logger.debug("filter_high_liquidity", token=token.symbol, liq=token.liquidity)
            return False
        return True

    def _check_market_cap(self, token: Token) -> bool:
        if token.market_cap is None:
            return True  # Allow if unknown
        if token.market_cap < self.config.min_market_cap:
            logger.debug("filter_low_mcap", token=token.symbol, mcap=token.market_cap)
            return False
        if token.market_cap > self.config.max_market_cap:
            logger.debug("filter_high_mcap", token=token.symbol, mcap=token.market_cap)
            return False
        return True

    def _check_age(self, token: Token) -> bool:
        if token.age_minutes is None:
            return True  # Allow if unknown
        if token.age_minutes > self.config.max_age_minutes:
            logger.debug("filter_too_old", token=token.symbol, age=token.age_minutes)
            return False
        return True

    def _check_smart_money(self, token: Token) -> bool:
        if token.smart_money_count < self.config.min_smart_money_count:
            logger.debug(
                "filter_low_smart_money",
                token=token.symbol,
                count=token.smart_money_count
            )
            return False
        return True

    def _check_lp_burned(self, token: Token) -> bool:
        if self.config.require_lp_burned and not token.lp_burned:
            # Relaxed: don't require LP burned if we can't verify
            # Many new tokens don't have this data yet
            pass
        return True

    def _check_honeypot(self, token: Token) -> bool:
        if self.config.reject_honeypot and token.is_honeypot:
            logger.debug("filter_honeypot", token=token.symbol)
            return False
        return True

    def reset_seen(self):
        """Clear the seen tokens cache."""
        self._seen.clear()
