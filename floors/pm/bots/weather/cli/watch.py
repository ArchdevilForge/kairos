from __future__ import annotations

from typing import Any

import asyncio

import httpx
import structlog
from rich.console import Console

from pm_bot.core.polymarket import fetch_weather_events
from pm_bot.core.weather import fetch_forecast
from pm_bot.core.ws import MarketWsClient
from pm_bot.core.observation import fetch_observation, filter_recommendations
from pm_bot.strategies.base import ALL_STRATEGIES, Strategy
from pm_bot.models.config import DEFAULT_CITIES, STRATEGY_DEFAULTS, resolve_city_alias
from pm_bot.core.config_loader import get_station_for_city, load_config
from pm_bot.cli.display import render_recommendations
from pm_bot.models.market import Recommendation, ForecastResult

console = Console()


async def run_watch(
    interval: int = 60,
    strategy: str = "all",
    cities_str: str | None = None,
    all_cities: bool = False,
    edge_override: float | None = None,
    include_closed: bool = False,
    use_ws: bool = True,
    observed: bool = False,
    debug: bool = False,
) -> None:
    _setup_logging(debug)

    config = load_config()
    cities = _resolve_cities(cities_str)
    strategies = _resolve_strategies(strategy)

    console.print("[bold]PM-Bot Watch[/] — Ctrl+C to stop.")

    try:
        while True:
            async with httpx.AsyncClient(timeout=30.0) as client:
                events = await fetch_weather_events(client, include_closed=include_closed)
                if not all_cities:
                    events = [e for e in events if e.city in cities]

                forecasts: dict[str, ForecastResult] = {}
                airport_forecasts: dict[str, ForecastResult] = {}
                city_forecasts: dict[str, ForecastResult] = {}
                for ev in events:
                    fc = await fetch_forecast(client, ev.city, ev.date)
                    if fc:
                        forecasts[ev.city] = fc

                    station_info = get_station_for_city(config, ev.city)
                    if station_info:
                        lat = station_info.get("lat")
                        lon = station_info.get("lon")
                        if lat is not None and lon is not None:
                            from pm_bot.cli.trade import fetch_forecast_at

                            afc = await fetch_forecast_at(client, float(lat), float(lon), ev.city, ev.date)
                            if afc:
                                airport_forecasts[ev.city] = afc

                        from pm_bot.models.config import CITY_COORDS

                        city_coords = CITY_COORDS.get(ev.city)
                        if city_coords:
                            from pm_bot.cli.trade import fetch_forecast_at

                            cfc = await fetch_forecast_at(client, city_coords[0], city_coords[1], ev.city, ev.date)
                            if cfc:
                                city_forecasts[ev.city] = cfc

                all_recs: list[Recommendation] = []
                for ev in events:
                    for strat_name, strat in strategies:
                        kwargs: dict = {}
                        for k, v in STRATEGY_DEFAULTS.get(strat_name, {}).items():
                            kwargs[k] = edge_override if k in ("edge_min",) and edge_override else v
                        if ev.city in forecasts:
                            kwargs["forecast"] = forecasts[ev.city]
                        if strat_name == "ensemble_spread":
                            pass  # strategy removed
                        recs = strat.run(ev, **kwargs)
                        if edge_override is not None:
                            recs = [r for r in recs if r.edge >= edge_override]
                        all_recs.extend(recs)

                # Filter by city variance
                pass  # variance filter removed

                if observed:
                    obs_map: dict[str, Any] = {}
                    for city in {ev.city for ev in events}:
                        obs = await fetch_observation(client, city)
                        if obs:
                            obs_map[city] = obs
                    filtered = []
                    for r in all_recs:
                        if r.city in obs_map:
                            remaining = filter_recommendations([r], obs_map[r.city])
                            filtered.extend(remaining)
                        else:
                            filtered.append(r)
                    all_recs = filtered

            console.clear()
            render_recommendations(all_recs)

            token_ids = [b.market_id for r in all_recs for b in [r.bucket] if b.market_id]

            if use_ws and token_ids:
                console.print(f"\n[dim]Streaming via WebSocket ({len(token_ids)} markets)...[/dim]")
                await _ws_watch(token_ids, strategies, edge_override, config)
                break
            else:
                console.print(f"\n[dim]Polling mode — next refresh in {interval}s...[/dim]")
                await asyncio.sleep(interval)

    except KeyboardInterrupt:
        console.print("\n[yellow]Watch stopped.[/yellow]")


async def _ws_watch(
    token_ids: list[str],
    strategies: list[tuple[str, Strategy]],
    edge_override: float | None,
    config: dict,
) -> None:
    ws_client = MarketWsClient()
    ws_task = asyncio.create_task(ws_client.connect())
    await asyncio.sleep(1)

    await ws_client.subscribe(token_ids)

    price_map: dict[str, dict[str, float | None]] = {}
    try:
        while True:
            try:
                update = await asyncio.wait_for(ws_client.updates(), timeout=15)
            except asyncio.TimeoutError:
                continue

            if update.token_id:
                price_map[update.token_id] = {
                    "best_bid": update.best_bid,
                    "best_ask": update.best_ask,
                }

            if update.event_type in ("best_bid_ask", "last_trade_price"):
                console.print(
                    f"  [dim]{update.event_type}[/dim] "
                    f"{update.token_id[:12]} "
                    f"bid={update.best_bid or '-'} ask={update.best_ask or '-'}",
                    end="\r",
                )
    except KeyboardInterrupt:
        ws_client.stop()
        ws_task.cancel()


def _resolve_cities(cities_str: str | None) -> set[str]:
    if cities_str:
        return {resolve_city_alias(c.strip()) for c in cities_str.split(",")}
    return {resolve_city_alias(c) for c in DEFAULT_CITIES}


def _resolve_strategies(name: str) -> list[tuple[str, Strategy]]:
    if name == "all":
        return list(ALL_STRATEGIES.items())
    if name in ALL_STRATEGIES:
        return [(name, ALL_STRATEGIES[name])]
    return list(ALL_STRATEGIES.items())


def _setup_logging(debug: bool) -> None:
    import logging

    if debug:
        structlog.configure(wrapper_class=structlog.make_filtering_bound_logger(logging.DEBUG))
    else:
        structlog.configure(wrapper_class=structlog.make_filtering_bound_logger(logging.WARNING))
