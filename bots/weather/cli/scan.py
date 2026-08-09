from __future__ import annotations

import httpx
import structlog
from rich.console import Console
from rich.progress import Progress, SpinnerColumn, TextColumn

from typing import Any

from pm_bot.core.polymarket import fetch_weather_events
from pm_bot.core.weather import fetch_forecast
from pm_bot.core.observation import fetch_observation, filter_recommendations
from pm_bot.strategies.base import ALL_STRATEGIES, Strategy
from pm_bot.models.config import DEFAULT_CITIES, STRATEGY_DEFAULTS, resolve_city_alias
from pm_bot.cli.display import render_recommendations, render_verbose

console = Console()


async def run_scan(
    strategy: str = "all",
    cities_str: str | None = None,
    all_cities: bool = False,
    edge_override: float | None = None,
    verbose: bool = False,
    include_closed: bool = False,
    observed: bool = False,
    debug: bool = False,
) -> None:
    _setup_logging(debug)

    cities = _resolve_cities(cities_str, all_cities)
    strategies = _resolve_strategies(strategy)

    with Progress(SpinnerColumn(), TextColumn("[bold blue]{task.description}"), console=console) as progress:
        task = progress.add_task("Fetching markets...", total=None)

        async with httpx.AsyncClient(timeout=30.0) as client:
            events = await fetch_weather_events(client, include_closed=include_closed)
            events = [e for e in events if all_cities or e.city in cities]

            if not events:
                console.print(
                    "[yellow]No weather markets found. Try --closed to include settled markets, or wait for new markets to open.[/yellow]"
                )
                return

            progress.update(task, description="Fetching forecasts...")
            forecasts: dict[str, Any] = {}
            for ev in events:
                fc = await fetch_forecast(client, ev.city, ev.date)
                if fc:
                    forecasts[ev.city] = fc

            obs_map: dict[str, Any] = {}
            if observed:
                progress.update(task, description="Fetching observations...")
                for city in {ev.city for ev in events}:
                    obs = await fetch_observation(client, city)
                    if obs:
                        obs_map[city] = obs

            progress.update(task, description="Computing edges...")
            all_recs = []
            for ev in events:
                for strat_name, strat in strategies:
                    kwargs: dict = {}
                    for k, v in STRATEGY_DEFAULTS.get(strat_name, {}).items():
                        kwargs[k] = edge_override if k in ("edge_min",) and edge_override else v
                    if ev.city in forecasts:
                        kwargs["forecast"] = forecasts[ev.city]
                    recs = strat.run(ev, **kwargs)
                    if edge_override is not None:
                        recs = [r for r in recs if r.edge >= edge_override]
                    if ev.city in obs_map:
                        recs = filter_recommendations(recs, obs_map[ev.city])
                    # Filter by city variance (skip high-volatility cities)
                    pass  # variance filter removed
                    all_recs.extend(recs)

    if verbose:
        render_verbose(all_recs)
    else:
        render_recommendations(all_recs)

    if include_closed:
        console.print("[dim]Note: --closed included settled markets (prices may be 0)[/dim]")


def _resolve_cities(cities_str: str | None, all_cities: bool) -> set[str]:
    if all_cities:
        return set()
    if cities_str:
        return {resolve_city_alias(c.strip()) for c in cities_str.split(",")}
    return {resolve_city_alias(c) for c in DEFAULT_CITIES}


def _resolve_strategies(name: str) -> list[tuple[str, Strategy]]:
    if name == "all":
        return list(ALL_STRATEGIES.items())
    if name in ALL_STRATEGIES:
        return [(name, ALL_STRATEGIES[name])]
    console.print(f"[red]Unknown strategy: {name}. Available: {', '.join(ALL_STRATEGIES)}[/red]")
    return []


def _setup_logging(debug: bool) -> None:
    import logging

    if debug:
        structlog.configure(wrapper_class=structlog.make_filtering_bound_logger(logging.DEBUG))
    else:
        structlog.configure(wrapper_class=structlog.make_filtering_bound_logger(logging.WARNING))
