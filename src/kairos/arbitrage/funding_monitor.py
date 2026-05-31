"""Funding rate monitoring across exchanges."""

import asyncio
import logging
import time
from dataclasses import dataclass, field
from typing import Optional

import ccxt.async_support as ccxt


@dataclass
class FundingRate:
    """Funding rate data for a symbol."""
    symbol: str
    exchange: str
    rate: float  # Current funding rate (e.g., 0.01 = 0.01%)
    annualized: float  # Annualized rate
    next_time: Optional[str] = None
    timestamp: float = field(default_factory=time.time)

    @property
    def is_extreme(self) -> bool:
        """Check if rate is extreme (>50% annualized)."""
        return abs(self.annualized) > 50

    @property
    def direction(self) -> str:
        """Direction of funding (positive = longs pay shorts)."""
        return "positive" if self.rate > 0 else "negative"


@dataclass
class FundingOpportunity:
    """Represents a funding rate arbitrage opportunity."""
    symbol: str
    exchange_long: str  # Exchange to go long (lower/negative funding)
    exchange_short: str  # Exchange to go short (higher/positive funding)
    rate_long: float
    rate_short: float
    spread: float  # Rate difference
    annualized_spread: float
    estimated_daily_profit_pct: float
    confidence: float  # 0-1
    reason: str = ""


class FundingRateMonitor:
    """Monitors funding rates across multiple exchanges."""

    def __init__(self, config: dict = None):
        self.logger = logging.getLogger("kairos.arbitrage.funding")
        config = config or {}

        self.exchanges = {}
        self.funding_rates: dict[str, dict[str, FundingRate]] = {}  # exchange -> symbol -> rate

        # Configuration
        self.min_spread_pct = config.get("minSpreadPct", 0.05)  # 0.05% minimum spread
        self.extreme_rate_threshold = config.get("extremeRateThreshold", 0.05)  # 0.05% per 8h
        self.update_interval = config.get("updateInterval", 300)  # 5 minutes

        # Initialize exchanges
        for exchange_name in config.get("exchanges", ["binance", "okx", "bybit"]):
            self._init_exchange(exchange_name, config.get(exchange_name, {}))

    def _init_exchange(self, name: str, config: dict):
        """Initialize an exchange connection."""
        try:
            exchange_class = getattr(ccxt, name)
            self.exchanges[name] = exchange_class({
                "enableRateLimit": True,
                "apiKey": config.get("apiKey"),
                "secret": config.get("secret"),
                "password": config.get("password"),
            })
            self.funding_rates[name] = {}
            self.logger.info(f"Initialized exchange {name} for funding monitor")
        except Exception as e:
            self.logger.error(f"Failed to initialize exchange {name}: {e}")

    async def update_funding_rates(self, symbols: list[str] = None):
        """Update funding rates for all exchanges."""
        tasks = []
        for exchange_name, exchange in self.exchanges.items():
            tasks.append(self._fetch_funding_rates(exchange_name, exchange, symbols))

        await asyncio.gather(*tasks, return_exceptions=True)

    async def _fetch_funding_rates(
        self,
        exchange_name: str,
        exchange: object,
        symbols: list[str] = None
    ):
        """Fetch funding rates for a single exchange."""
        try:
            # Fetch all funding rates
            funding_rates = await exchange.fetch_funding_rates(symbols)

            for symbol, data in funding_rates.items():
                rate = data.get("fundingRate", 0)
                if rate is None:
                    continue

                # Calculate annualized rate (funding every 8 hours = 3x daily)
                annualized = rate * 3 * 365 * 100  # As percentage

                self.funding_rates[exchange_name][symbol] = FundingRate(
                    symbol=symbol,
                    exchange=exchange_name,
                    rate=rate * 100,  # Convert to percentage
                    annualized=annualized,
                    next_time=data.get("fundingDatetime"),
                    timestamp=data.get("timestamp", time.time() * 1000) / 1000
                )

            self.logger.debug(f"Updated {len(funding_rates)} funding rates for {exchange_name}")

        except Exception as e:
            self.logger.error(f"Failed to fetch funding rates for {exchange_name}: {e}")

    def get_rates(self, symbol: str) -> dict[str, FundingRate]:
        """Get funding rates for a symbol across all exchanges."""
        result = {}
        for exchange_name, rates in self.funding_rates.items():
            if symbol in rates:
                result[exchange_name] = rates[symbol]
        return result

    def get_extreme_rates(
        self,
        threshold: float = None
    ) -> list[FundingRate]:
        """Get all symbols with extreme funding rates."""
        threshold = threshold or self.extreme_rate_threshold
        extreme = []

        for exchange_rates in self.funding_rates.values():
            for rate in exchange_rates.values():
                if abs(rate.rate) > threshold * 100:  # Convert to percentage
                    extreme.append(rate)

        return sorted(extreme, key=lambda r: abs(r.rate), reverse=True)

    def find_opportunities(self) -> list[FundingOpportunity]:
        """Find cross-exchange funding arbitrage opportunities."""
        opportunities = []

        # Get all symbols across exchanges
        all_symbols = set()
        for rates in self.funding_rates.values():
            all_symbols.update(rates.keys())

        for symbol in all_symbols:
            rates = self.get_rates(symbol)
            if len(rates) < 2:
                continue

            # Find best long and short rates
            exchanges = list(rates.keys())
            for i in range(len(exchanges)):
                for j in range(i + 1, len(exchanges)):
                    ex1, ex2 = exchanges[i], exchanges[j]
                    rate1, rate2 = rates[ex1], rates[ex2]

                    # Calculate spread
                    spread = abs(rate1.rate - rate2.rate)

                    if spread < self.min_spread_pct:
                        continue

                    # Determine which exchange to go long/short
                    if rate1.rate < rate2.rate:
                        long_ex, short_ex = ex1, ex2
                        long_rate, short_rate = rate1.rate, rate2.rate
                    else:
                        long_ex, short_ex = ex2, ex1
                        long_rate, short_rate = rate2.rate, rate1.rate

                    # Calculate expected profit
                    # Long position receives funding when rate is negative
                    # Short position receives funding when rate is positive
                    daily_profit = (short_rate - long_rate) / 100 * 3  # 3 funding periods per day
                    annualized_spread = spread * 3 * 365

                    opportunity = FundingOpportunity(
                        symbol=symbol,
                        exchange_long=long_ex,
                        exchange_short=short_ex,
                        rate_long=long_rate,
                        rate_short=short_rate,
                        spread=spread,
                        annualized_spread=annualized_spread,
                        estimated_daily_profit_pct=daily_profit * 100,
                        confidence=min(spread / 0.1, 1.0),  # Higher spread = higher confidence
                        reason=f"Funding spread {spread:.3f}% between {long_ex} and {short_ex}"
                    )

                    opportunities.append(opportunity)

        return sorted(opportunities, key=lambda o: o.annualized_spread, reverse=True)

    def get_statistics(self) -> dict:
        """Get overall funding rate statistics."""
        all_rates = []
        for rates in self.funding_rates.values():
            all_rates.extend(rates.values())

        if not all_rates:
            return {"total_symbols": 0}

        rates_values = [r.rate for r in all_rates]

        return {
            "total_symbols": len(set(r.symbol for r in all_rates)),
            "total_exchanges": len(self.funding_rates),
            "avg_rate": sum(rates_values) / len(rates_values),
            "max_rate": max(rates_values),
            "min_rate": min(rates_values),
            "extreme_positive": len([r for r in rates_values if r > 0.05]),
            "extreme_negative": len([r for r in rates_values if r < -0.05])
        }
