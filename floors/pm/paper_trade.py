#!/usr/bin/env python3
"""Paper Trading Bot — $100 Compounding Test.

Fetches real Polymarket weather markets + Open-Meteo forecasts,
runs Combined strategy (Ladder 40% + Tail 30% + Gopfan2 30%),
and logs trades to SQLite for analysis.

Memory-optimized for VPS deployment.

Usage:
    uv run python3 paper_trade.py              # Run once (cron)
    uv run python3 paper_trade.py --loop       # Run continuously
    uv run python3 paper_trade.py --status     # Show current state
"""

from __future__ import annotations

import argparse
import asyncio
import json
import os
import sys
import time
from datetime import datetime, timezone
from pathlib import Path
from math import erf, sqrt

import httpx
import numpy as np
import structlog

# Project root
ROOT = Path(__file__).parent
sys.path.insert(0, str(ROOT / "bots" / "weather"))

from backtest.weather_strategies import (
    Bucket,
    CombinedStrategy,
    LadderStrategy,
    TailStrategy,
    Gopfan2Strategy,
    TradeSignal,
    load_emos_coeffs,
    emos_calibrate,
    emos_bucket_prob,
)
from core.paper_trade import PaperTradeDB

log = structlog.get_logger()

# ── Config ──
INITIAL_BANKROLL = float(os.environ.get("PAPER_BANKROLL", "100.0"))
GAMMA_API = "https://gamma-api.polymarket.com"
OPEN_METEO_FORECAST = "https://api.open-meteo.com/v1/forecast"
OPEN_METEO_ENSEMBLE = "https://ensemble-api.open-meteo.com/v1/ensemble"

# Cities to trade (matching Polymarket active markets)
TRADE_CITIES = [
    "New York", "London", "Paris", "Tokyo", "Shanghai",
    "Seoul", "Hong Kong", "Miami", "Chicago", "Los Angeles",
]

# Weather series slugs for Gamma API
WEATHER_SERIES = {
    "New York": "nyc-daily-weather",
    "London": "london-daily-weather",
    "Tokyo": "tokyo-daily-weather",
    "Shanghai": "shanghai-daily-weather",
    "Miami": "miami-daily-weather",
    "Chicago": "chicago-daily-weather",
    "Los Angeles": "los-angeles-daily-weather",
    "Hong Kong": "hong-kong-daily-weather",
    "Seoul": "seoul-daily-weather",
    "Paris": "paris-daily-weather",
}

# Airport coordinates for forecast fetching
AIRPORT_COORDS = {
    "New York": (40.7772, -73.8726),
    "London": (51.5048, 0.0495),
    "Paris": (48.9694, 2.4414),
    "Tokyo": (35.5522, 139.7796),
    "Shanghai": (31.1443, 121.8083),
    "Seoul": (37.4602, 126.4407),
    "Hong Kong": (22.3080, 113.9185),
    "Miami": (25.7953, -80.2902),
    "Chicago": (41.9742, -87.9073),
    "Los Angeles": (33.9425, -118.4081),
}

# Station bias priors (airport vs city center)
STATION_BIAS = {
    "New York": 1.5, "London": 0.5, "Paris": 0.8, "Tokyo": 0.7,
    "Shanghai": 0.8, "Seoul": 1.0, "Hong Kong": 0.3, "Miami": 0.3,
    "Chicago": 0.8, "Los Angeles": 0.4,
}

# EMOS coefficients
EMOS_DIR = ROOT / "data" / "emos_coeffs"


def load_emos() -> dict[str, dict[str, float]]:
    """Load EMOS coefficients."""
    all_path = EMOS_DIR / "emos_all.json"
    if all_path.exists():
        return json.loads(all_path.read_text())
    return {}


async def fetch_forecast(city: str, client: httpx.AsyncClient) -> tuple[float, list[float]]:
    """Fetch ensemble forecast from Open-Meteo."""
    coords = AIRPORT_COORDS.get(city)
    if not coords:
        return 0.0, []

    lat, lon = coords
    params = {
        "latitude": lat,
        "longitude": lon,
        "daily": "temperature_2m_max",
        "models": "gfs_seamless,ecmwf_ifs025,icon_global,gem_global",
        "timezone": "auto",
        "forecast_days": 3,
    }

    try:
        resp = await client.get(OPEN_METEO_FORECAST, params=params, timeout=15.0)
        resp.raise_for_status()
        data = resp.json()

        daily = data.get("daily", {})
        temps = daily.get("temperature_2m_max", [])

        if not temps:
            return 0.0, []

        # Use tomorrow's forecast
        tomorrow_temp = temps[1] if len(temps) > 1 else temps[0]

        # Generate synthetic ensemble members (in production, use real ensemble data)
        members = []
        for _ in range(31):
            members.append(tomorrow_temp + np.random.normal(0, 2.0))

        return tomorrow_temp, members

    except Exception as e:
        log.error("forecast_fetch_failed", city=city, error=str(e))
        return 0.0, []


async def fetch_markets(client: httpx.AsyncClient) -> list[dict]:
    """Fetch active weather markets from Gamma API."""
    all_markets = []
    
    for city, series_slug in WEATHER_SERIES.items():
        try:
            resp = await client.get(
                f"{GAMMA_API}/events",
                params={
                    "series_slug": series_slug,
                    "limit": 5,
                    "order": "end_date",
                    "ascending": False,
                },
                timeout=15.0,
            )
            resp.raise_for_status()
            events = resp.json()
            
            # Extract markets from events
            for event in events:
                if event.get("closed", False):
                    continue
                for market in event.get("markets", []):
                    market["_city"] = city
                    all_markets.append(market)
                    
        except Exception as e:
            log.error("market_fetch_failed", city=city, error=str(e))
            continue
    
    log.info("markets_fetched", count=len(all_markets))
    return all_markets


def parse_bucket_from_question(question: str) -> tuple[float, float] | None:
    """Extract temperature bucket from market question."""
    import re

    # Match patterns like "10-12°C" or "≥30°C" or "≤5°C"
    range_match = re.search(r'(\d+)\s*-\s*(\d+)\s*°[CF]', question)
    if range_match:
        return float(range_match.group(1)), float(range_match.group(2))

    high_match = re.search(r'[≥>]=?\s*(\d+)\s*°[CF]', question)
    if high_match:
        return float(high_match.group(1)), 999.0

    low_match = re.search(r'[≤<]=?\s*(\d+)\s*°[CF]', question)
    if low_match:
        return -999.0, float(low_match.group(1))

    return None


def match_city_from_market(market: dict) -> str | None:
    """Extract city from market title/question."""
    # Check if city was already added during fetch
    if "_city" in market:
        return market["_city"]
    
    title = market.get("question", "").lower()

    for city in TRADE_CITIES:
        if city.lower() in title:
            return city

    # Check aliases
    aliases = {"nyc": "New York", "la": "Los Angeles", "hk": "Hong Kong"}
    for alias, city in aliases.items():
        if alias in title:
            return city

    return None


async def run_paper_trade():
    """Main paper trading loop."""
    log.info("paper_trade_start", bankroll=INITIAL_BANKROLL)

    db = PaperTradeDB(initial_bankroll=INITIAL_BANKROLL)
    emos_coeffs = load_emos()
    strategy = CombinedStrategy(bankroll=INITIAL_BANKROLL)

    # Get current bankroll from DB
    current_bankroll = float(db.get_state("bankroll", str(INITIAL_BANKROLL)))
    strategy.bankroll = current_bankroll

    log.info("bankroll_loaded", bankroll=current_bankroll)

    async with httpx.AsyncClient() as client:
        # Fetch markets
        markets = await fetch_markets(client)
        log.info("markets_fetched", count=len(markets))

        if not markets:
            log.warning("no_markets_found")
            return

        # Process each city
        trades_today = 0

        for city in TRADE_CITIES:
            # Fetch forecast
            forecast_temp, members = await fetch_forecast(city, client)

            if not members:
                continue

            # Apply EMOS calibration
            coeffs = emos_coeffs.get(city, {})
            if coeffs:
                mu, sigma = emos_calibrate(members, coeffs)
            else:
                mu = float(np.mean(members))
                sigma = max(float(np.std(members)), 2.5)

            # Find matching markets
            city_markets = [m for m in markets if match_city_from_market(m) == city]

            if not city_markets:
                continue

            # Parse buckets from markets
            buckets = []
            for market in city_markets:
                bucket_range = parse_bucket_from_question(market.get("question", ""))
                if not bucket_range:
                    continue

                low_c, high_c = bucket_range

                # Get price from market (default 0.10 if not available)
                yes_price = float(market.get("outcomePrices", "[0.1]")[0]) if market.get("outcomePrices") else 0.10
                yes_price = max(0.01, min(0.99, yes_price))

                buckets.append(Bucket(
                    low_c=low_c,
                    high_c=high_c,
                    market_price=yes_price,
                ))

            if not buckets:
                continue

            # Generate signals
            signals = strategy.generate_signals(
                city=city,
                date=datetime.now().strftime("%Y-%m-%d"),
                buckets=buckets,
                model_mu=mu,
                model_sigma=sigma,
                emos_coeffs=coeffs if coeffs else None,
                lead_time_hours=24.0,  # Assume 24h lead time
            )

            # Execute signals
            for signal in signals:
                if current_bankroll < 1.0:
                    break

                # Compute position size
                size_usd = signal.size_pct * current_bankroll
                size_usd = min(size_usd, 50.0)
                size_usd = max(size_usd, 1.0)

                if size_usd > current_bankroll * 0.5:
                    continue

                # Record trade in DB
                order_id = f"paper-{int(time.time())}-{trades_today}"
                db.record_trade(
                    order_id=order_id,
                    market_id=f"{city}-{signal.bucket.label}",
                    strategy=signal.strategy,
                    side="YES",
                    price=signal.bucket.market_price,
                    size_usd=size_usd,
                    shares=size_usd / signal.bucket.market_price,
                    kelly_fraction=signal.size_pct,
                    edge=signal.edge,
                    city=city,
                    temp_label=signal.bucket.label,
                    reasoning=f"Model prob: {signal.model_prob:.1%}, Market: {signal.bucket.market_price:.1%}, Edge: {signal.edge:.1%}",
                )

                trades_today += 1
                log.info(
                    "trade_executed",
                    city=city,
                    bucket=signal.bucket.label,
                    price=f"{signal.bucket.market_price:.3f}",
                    size=f"${size_usd:.2f}",
                    edge=f"{signal.edge:.1%}",
                    strategy=signal.strategy,
                )

        # Update bankroll in state
        db.set_state("bankroll", str(current_bankroll))
        db.set_state("last_run", datetime.now(timezone.utc).isoformat())

        log.info("paper_trade_complete", trades=trades_today, bankroll=current_bankroll)


def show_status():
    """Show current paper trading status."""
    db = PaperTradeDB(initial_bankroll=INITIAL_BANKROLL)

    print("\n" + "=" * 60)
    print("  Paper Trading Status")
    print("=" * 60)
    print(f"  Bankroll: ${float(db.get_state('bankroll', str(INITIAL_BANKROLL))):.2f}")
    print(f"  Last Run: {db.get_state('last_run', 'Never')}")
    print(f"  Initial:  ${INITIAL_BANKROLL:.2f}")

    # Get recent trades
    trades = db.get_recent_trades(limit=10)
    if trades:
        print(f"\n  Recent Trades ({len(trades)}):")
        for t in trades[:5]:
            print(f"    {t.get('city', '?'):>15} {t.get('temp_label', '?'):>12} "
                  f"${t.get('size_usd', 0):>6.2f} @{t.get('price', 0):.3f} "
                  f"edge={t.get('edge', 0):.1%}")

    # Get daily stats
    daily = db.get_state("daily_pnl", "0")
    print(f"\n  Today P&L: ${float(daily):.2f}")
    print("=" * 60)


async def main():
    parser = argparse.ArgumentParser(description="Paper Trading Bot")
    parser.add_argument("--loop", action="store_true", help="Run continuously")
    parser.add_argument("--status", action="store_true", help="Show status")
    parser.add_argument("--interval", type=int, default=3600, help="Loop interval (seconds)")

    args = parser.parse_args()

    if args.status:
        show_status()
        return

    if args.loop:
        log.info("starting_loop", interval=args.interval)
        while True:
            try:
                await run_paper_trade()
            except Exception as e:
                log.error("loop_error", error=str(e))
            await asyncio.sleep(args.interval)
    else:
        await run_paper_trade()


if __name__ == "__main__":
    asyncio.run(main())
