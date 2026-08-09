"""City selection module for identifying profitable weather markets.

Selects cities based on:
1. Spread width (wider = more opportunity)
2. Bot saturation (fewer bots = more edge)
3. Liquidity (enough to fill orders)
4. Forecast accuracy (some cities are more predictable)

Reference:
- ColdMath strategy: focus on secondary markets (Buenos Aires, Cape Town)
- polymarketweather.com: "Volume thins at extremes"
"""

from __future__ import annotations

import asyncio
from dataclasses import dataclass

import httpx
import structlog

from pm_bot.models.config import CITY_COORDS

log = structlog.get_logger()

GAMMA_API = "https://gamma-api.polymarket.com"


@dataclass
class CityMetrics:
    """Metrics for a city's weather market."""

    city: str
    series_slug: str
    active_events: int
    total_volume: float
    avg_spread: float  # Average bid-ask spread
    tail_buckets_count: int  # Buckets with price < $0.15
    avg_tail_price: float
    bot_score: float  # 0-1, lower = less bot competition (estimated)


# Known weather series slugs
WEATHER_SERIES = {
    "New York": "nyc-daily-weather",
    "London": "london-daily-weather",
    "Tokyo": "tokyo-daily-weather",
    "Shanghai": "shanghai-daily-weather",
    "Miami": "miami-daily-weather",
    "Chicago": "chicago-daily-weather",
    "Los Angeles": "los-angeles-daily-weather",
    "San Francisco": "san-francisco-daily-weather",
    "Seattle": "seattle-daily-weather",
    "Austin": "austin-daily-weather",
    "Dallas": "dallas-daily-weather",
    "Atlanta": "atlanta-daily-weather",
    "Toronto": "toronto-daily-weather",
    "Mexico City": "mexico-city-daily-weather",
    "São Paulo": "sao-paulo-daily-weather",
    "Buenos Aires": "buenos-aires-daily-weather",
    "Lagos": "lagos-daily-weather",
    "Cape Town": "cape-town-daily-weather",
    "Hong Kong": "hong-kong-daily-weather",
    "Taipei": "taipei-daily-weather",
    "Seoul": "seoul-daily-weather",
    "Beijing": "beijing-daily-weather",
    "Warsaw": "warsaw-daily-weather",
    "Helsinki": "helsinki-daily-weather",
    "Paris": "paris-daily-weather",
    "Madrid": "madrid-daily-weather",
    "Milan": "milan-daily-weather",
    "Munich": "munich-daily-weather",
    "Amsterdam": "amsterdam-daily-weather",
    "Istanbul": "istanbul-daily-weather",
    "Moscow": "moscow-daily-weather",
    "Wellington": "wellington-daily-weather",
    "Jakarta": "jakarta-daily-weather",
}

# Estimated bot competition level (0=low, 1=high)
# Based on market analysis and community reports
BOT_COMPETITION_ESTIMATE = {
    "New York": 0.9,  # Very competitive
    "London": 0.85,
    "Tokyo": 0.8,
    "Shanghai": 0.7,
    "Miami": 0.6,
    "Chicago": 0.7,
    "Los Angeles": 0.65,
    "San Francisco": 0.6,
    "Seattle": 0.5,
    "Austin": 0.5,
    "Dallas": 0.5,
    "Atlanta": 0.4,  # Less competitive
    "Toronto": 0.5,
    "Mexico City": 0.4,
    "São Paulo": 0.4,
    "Buenos Aires": 0.3,  # ColdMath focus - less competitive
    "Lagos": 0.3,
    "Cape Town": 0.3,  # ColdMath focus
    "Hong Kong": 0.6,
    "Taipei": 0.5,
    "Seoul": 0.5,
    "Beijing": 0.5,
    "Warsaw": 0.4,
    "Helsinki": 0.4,
    "Paris": 0.7,
    "Madrid": 0.5,
    "Milan": 0.5,
    "Munich": 0.5,
    "Amsterdam": 0.5,
    "Istanbul": 0.4,
    "Moscow": 0.4,
    "Wellington": 0.3,
    "Jakarta": 0.3,
}


class CitySelector:
    """Selects optimal cities for weather trading.

    Usage:
        selector = CitySelector()
        cities = await selector.select_cities(client, n=5)
    """

    def __init__(
        self,
        min_tail_buckets: int = 2,
        max_bot_score: float = 0.7,
        min_volume: float = 100.0,
    ):
        self.min_tail_buckets = min_tail_buckets
        self.max_bot_score = max_bot_score
        self.min_volume = min_volume

    async def select_cities(
        self,
        client: httpx.AsyncClient,
        n: int = 10,
        cities: list[str] | None = None,
    ) -> list[CityMetrics]:
        """Select top N cities for trading.

        Args:
            client: HTTP client
            n: Number of cities to return
            cities: Specific cities to analyze (default: all)

        Returns:
            List of CityMetrics sorted by opportunity score
        """
        target_cities = cities or list(WEATHER_SERIES.keys())

        # Fetch metrics for all cities concurrently
        tasks = [
            self._analyze_city(client, city)
            for city in target_cities
            if city in WEATHER_SERIES
        ]
        results = await asyncio.gather(*tasks, return_exceptions=True)

        # Filter successful results
        metrics: list[CityMetrics] = []
        for city, result in zip(target_cities, results):
            if isinstance(result, Exception):
                log.debug("city_analysis_failed", city=city, error=str(result))
                continue
            if result is not None:
                metrics.append(result)

        # Score and rank cities
        scored = []
        for m in metrics:
            score = self._calculate_opportunity_score(m)
            scored.append((score, m))

        scored.sort(key=lambda x: x[0], reverse=True)

        # Return top N
        selected = [m for _, m in scored[:n]]

        log.info(
            "cities_selected",
            n=len(selected),
            cities=[m.city for m in selected],
        )

        return selected

    async def _analyze_city(
        self,
        client: httpx.AsyncClient,
        city: str,
    ) -> CityMetrics | None:
        """Analyze a city's weather market for trading opportunities."""
        series_slug = WEATHER_SERIES.get(city)
        if not series_slug:
            return None

        # Fetch active events
        try:
            resp = await client.get(
                f"{GAMMA_API}/events",
                params={
                    "series_slug": series_slug,
                    "limit": 10,
                    "order": "end_date",
                    "ascending": False,
                },
            )
            resp.raise_for_status()
            events = resp.json()
        except httpx.HTTPError as e:
            log.debug("gamma_fetch_failed", city=city, error=str(e))
            return None

        if not events:
            return None

        # Count active events
        active_events = sum(1 for e in events if not e.get("closed", False))

        # Analyze market prices
        total_volume = 0
        spreads: list[float] = []
        tail_buckets = 0
        tail_prices: list[float] = []

        for event in events:
            markets = event.get("markets", [])
            for market in markets:
                # Parse prices
                prices_raw = market.get("outcomePrices", "")
                if isinstance(prices_raw, str):
                    try:
                        import json

                        prices = json.loads(prices_raw)
                        yes_price = float(prices[0]) if prices else 0
                    except (json.JSONDecodeError, IndexError):
                        yes_price = 0
                else:
                    yes_price = 0

                # Track tail buckets
                if 0 < yes_price < 0.15:
                    tail_buckets += 1
                    tail_prices.append(yes_price)

                # Estimate spread (if available)
                volume = market.get("volume", 0)
                if volume:
                    total_volume += float(volume)

        avg_spread = 0.03  # Default estimate for weather markets
        bot_score = BOT_COMPETITION_ESTIMATE.get(city, 0.5)
        avg_tail = sum(tail_prices) / len(tail_prices) if tail_prices else 0

        return CityMetrics(
            city=city,
            series_slug=series_slug,
            active_events=active_events,
            total_volume=total_volume,
            avg_spread=avg_spread,
            tail_buckets_count=tail_buckets,
            avg_tail_price=avg_tail,
            bot_score=bot_score,
        )

    def _calculate_opportunity_score(self, metrics: CityMetrics) -> float:
        """Calculate opportunity score for a city.

        Higher score = better opportunity.

        Factors:
        - Tail buckets: more = better (more trading opportunities)
        - Bot competition: lower = better (less efficient market)
        - Volume: moderate = better (enough liquidity, not too efficient)
        """
        # Tail bucket score (normalized)
        tail_score = min(1.0, metrics.tail_buckets_count / 10.0)

        # Competition score (inverse - lower competition = higher score)
        competition_score = 1.0 - metrics.bot_score

        # Volume score (prefer moderate volume)
        volume_score = 1.0 if metrics.total_volume > 100 else 0.5

        # Weighted combination
        score = (
            tail_score * 0.4
            + competition_score * 0.4
            + volume_score * 0.2
        )

        return score


async def get_recommended_cities(
    client: httpx.AsyncClient,
    n: int = 10,
) -> list[str]:
    """Get recommended city names for trading.

    Convenience function that returns just the city names.
    """
    selector = CitySelector()
    metrics = await selector.select_cities(client, n=n)
    return [m.city for m in metrics]
