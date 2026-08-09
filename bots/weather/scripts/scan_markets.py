"""Market scanner for weather trading opportunities.

Continuously monitors all Polymarket weather markets for:
1. Model-mispriced buckets - forecast arb opportunities
2. Best cities to trade based on competition/liquidity

Usage:
    python -m pm_bot.scripts.scan_markets
    python -m pm_bot.scripts.scan_markets --mode arb --min-edge 0.15
"""

from __future__ import annotations

import asyncio
import json
from dataclasses import dataclass, field
from datetime import datetime

import httpx
import structlog

from pm_bot.core.city_selector import CitySelector, WEATHER_SERIES
from pm_bot.core.emos import EMOSCalibrator, bucket_probability_emos
from pm_bot.core.weather import bucket_probability_numpy
from pm_bot.models.config import CITY_COORDS
from pm_bot.models.market import ForecastResult
from pm_bot.core.parser import parse_bucket

log = structlog.get_logger()

GAMMA_API = "https://gamma-api.polymarket.com"


@dataclass
class TradingOpportunity:
    """A single trading opportunity."""

    city: str
    event_id: str
    event_title: str
    market_question: str
    yes_price: float
    model_prob: float
    edge: float
    direction: str  # "YES" or "NO"
    strategy: str  # "tail" or "arb"
    bucket_low: float
    bucket_high: float
    bucket_unit: str
    timestamp: datetime = field(default_factory=datetime.now)

    @property
    def token_id(self) -> str:
        """Extract token ID from market question (simplified)."""
        return self.event_id + ":" + self.market_question[:20]


@dataclass
class ScanResult:
    """Results of a market scan."""

    timestamp: datetime
    opportunities: list[TradingOpportunity]
    cities_scanned: int
    events_scanned: int
    markets_scanned: int

    def summary(self) -> str:
        """Print scan summary."""
        lines = [
            f"Scan completed at {self.timestamp.strftime('%Y-%m-%d %H:%M:%S')}",
            f"Cities: {self.cities_scanned}, Events: {self.events_scanned}, Markets: {self.markets_scanned}",
            f"Opportunities found: {len(self.opportunities)}",
            "",
        ]

        if self.opportunities:
            # Group by strategy
            tail_opps = [o for o in self.opportunities if o.strategy == "tail"]
            arb_opps = [o for o in self.opportunities if o.strategy == "arb"]

            if tail_opps:
                lines.append(f"TAIL opportunities ({len(tail_opps)}):")
                for opp in sorted(tail_opps, key=lambda x: x.edge, reverse=True)[:10]:
                    lines.append(
                        f"  {opp.city}: {opp.market_question[:50]}... "
                        f"YES@{opp.yes_price:.2f} model={opp.model_prob:.1%} edge={opp.edge:.1%}"
                    )

            if arb_opps:
                lines.append(f"\nARB opportunities ({len(arb_opps)}):")
                for opp in sorted(arb_opps, key=lambda x: x.edge, reverse=True)[:10]:
                    lines.append(
                        f"  {opp.city}: {opp.market_question[:50]}... "
                        f"YES@{opp.yes_price:.2f} model={opp.model_prob:.1%} edge={opp.edge:.1%}"
                    )

        return "\n".join(lines)


class MarketScanner:
    """Scans Polymarket weather markets for trading opportunities.

    Usage:
        scanner = MarketScanner()
        result = await scanner.scan(mode="tail", min_edge=0.08)
    """

    def __init__(
        self,
        emos_calibrators: dict[str, EMOSCalibrator] | None = None,
        use_ensemble: bool = True,
    ):
        self.emos_calibrators = emos_calibrators or {}
        self.use_ensemble = use_ensemble

    async def scan(
        self,
        mode: str = "all",
        min_edge: float = 0.08,
        cities: list[str] | None = None,
        max_cities: int = 15,
    ) -> ScanResult:
        """Scan markets for opportunities.

        Args:
            mode: "arb" (forecast arb) or "all"
            min_edge: Minimum edge threshold
            cities: Specific cities to scan
            max_cities: Maximum cities to scan

        Returns:
            ScanResult with all opportunities
        """
        async with httpx.AsyncClient(timeout=30.0) as client:
            # Select cities
            if cities:
                target_cities = cities
            else:
                selector = CitySelector()
                city_metrics = await selector.select_cities(client, n=max_cities)
                target_cities = [m.city for m in city_metrics]

            log.info("scanning_cities", cities=target_cities, mode=mode)

            opportunities: list[TradingOpportunity] = []
            events_scanned = 0
            markets_scanned = 0

            for city in target_cities:
                try:
                    city_opps, events, markets = await self._scan_city(
                        client, city, mode, min_edge
                    )
                    opportunities.extend(city_opps)
                    events_scanned += events
                    markets_scanned += markets
                except Exception as e:
                    log.warning("city_scan_failed", city=city, error=str(e))

        return ScanResult(
            timestamp=datetime.now(),
            opportunities=opportunities,
            cities_scanned=len(target_cities),
            events_scanned=events_scanned,
            markets_scanned=markets_scanned,
        )

    async def _scan_city(
        self,
        client: httpx.AsyncClient,
        city: str,
        mode: str,
        min_edge: float,
    ) -> tuple[list[TradingOpportunity], int, int]:
        """Scan a single city's weather markets."""
        series_slug = WEATHER_SERIES.get(city)
        if not series_slug:
            return [], 0, 0

        # Fetch active events
        resp = await client.get(
            f"{GAMMA_API}/events",
            params={
                "series_slug": series_slug,
                "limit": 5,
                "order": "end_date",
                "ascending": False,
            },
        )
        resp.raise_for_status()
        events_data = resp.json()

        # Filter active events
        active_events = [e for e in events_data if not e.get("closed", False)]
        if not active_events:
            return [], 0, 0

        # Fetch forecast
        from pm_bot.core.weather import fetch_forecast

        forecast = await fetch_forecast(client, city)
        if not forecast:
            return [], 0, 0

        # Get EMOS calibrator for this city
        calibrator = self.emos_calibrators.get(city)

        opportunities: list[TradingOpportunity] = []
        markets_count = 0

        for event in active_events:
            event_id = event.get("id", "")
            event_title = event.get("title", "")
            markets = event.get("markets", [])

            for market in markets:
                markets_count += 1
                opp = self._analyze_market(
                    city=city,
                    event_id=event_id,
                    event_title=event_title,
                    market=market,
                    forecast=forecast,
                    calibrator=calibrator,
                    mode=mode,
                    min_edge=min_edge,
                )
                if opp:
                    opportunities.append(opp)

        return opportunities, len(active_events), markets_count

    def _analyze_market(
        self,
        city: str,
        event_id: str,
        event_title: str,
        market: dict,
        forecast: ForecastResult,
        calibrator: EMOSCalibrator | None,
        mode: str,
        min_edge: float,
    ) -> TradingOpportunity | None:
        """Analyze a single market for trading opportunity."""
        question = market.get("question", "")

        # Parse prices
        prices_raw = market.get("outcomePrices", "")
        if isinstance(prices_raw, str):
            try:
                prices = json.loads(prices_raw)
                yes_price = float(prices[0]) if prices else 0
            except (json.JSONDecodeError, IndexError):
                return None
        else:
            return None

        if yes_price <= 0 or yes_price >= 1:
            return None

        # Parse bucket bounds from question
        parsed = parse_bucket(question)
        if not parsed:
            return None

        # Calculate model probability
        if calibrator and calibrator._trained:
            model_prob = bucket_probability_emos(
                calibrator,
                forecast.members,
                parsed.temp_low_c,
                parsed.temp_high_c,
                parsed.temp_unit,
            )
        else:
            model_prob = bucket_probability_numpy(
                forecast,
                parsed.temp_low_c,
                parsed.temp_high_c,
                parsed.temp_unit,
            )

        edge = model_prob - yes_price

        # Apply strategy filters
        if mode == "arb" or mode == "all":
            # Forecast arb: buy when model >> market
            if edge >= min_edge and yes_price <= 0.30:
                return TradingOpportunity(
                    city=city,
                    event_id=event_id,
                    event_title=event_title,
                    market_question=question,
                    yes_price=yes_price,
                    model_prob=model_prob,
                    edge=edge,
                    direction="YES",
                    strategy="arb",
                    bucket_low=parsed.temp_low_c,
                    bucket_high=parsed.temp_high_c,
                    bucket_unit=parsed.temp_unit,
                )

        return None


async def scan_and_report(
    mode: str = "all",
    min_edge: float = 0.08,
    cities: list[str] | None = None,
) -> ScanResult:
    """Scan markets and print report.

    Convenience function for CLI usage.
    """
    scanner = MarketScanner()
    result = await scanner.scan(mode=mode, min_edge=min_edge, cities=cities)
    print(result.summary())
    return result


def main():
    """CLI entry point for market scanning."""
    import argparse

    parser = argparse.ArgumentParser(description="Scan Polymarket weather markets")
    parser.add_argument(
        "--mode",
        choices=["tail", "arb", "all"],
        default="all",
        help="Scanning mode",
    )
    parser.add_argument(
        "--min-edge",
        type=float,
        default=0.08,
        help="Minimum edge threshold",
    )
    parser.add_argument(
        "--cities",
        type=str,
        help="Comma-separated cities to scan",
    )
    args = parser.parse_args()

    cities = args.cities.split(",") if args.cities else None
    asyncio.run(scan_and_report(mode=args.mode, min_edge=args.min_edge, cities=cities))


if __name__ == "__main__":
    main()
