from __future__ import annotations

import httpx
import structlog
from rich.console import Console
from rich.progress import Progress, SpinnerColumn, TextColumn

from pm_bot.core.polymarket import fetch_weather_events
from pm_bot.models.config import resolve_city_alias
from pm_bot.cli.display import render_events

console = Console()


async def run_markets(
    cities_str: str | None = None, all_cities: bool = False, include_closed: bool = False, debug: bool = False
) -> None:
    _setup_logging(debug)

    with Progress(SpinnerColumn(), TextColumn("[bold blue]Fetching markets..."), console=console) as progress:
        progress.add_task("Fetching markets...", total=None)

        async with httpx.AsyncClient(timeout=30.0) as client:
            events = await fetch_weather_events(client, include_closed=include_closed)

    if cities_str and not all_cities:
        cities = {resolve_city_alias(c.strip()) for c in cities_str.split(",")}
        events = [e for e in events if e.city in cities]

    render_events(events)

    if not events:
        console.print("[yellow]No weather markets found. Try --closed to include settled markets.[/yellow]")


def _setup_logging(debug: bool) -> None:
    import logging

    if debug:
        structlog.configure(wrapper_class=structlog.make_filtering_bound_logger(logging.DEBUG))
    else:
        structlog.configure(wrapper_class=structlog.make_filtering_bound_logger(logging.WARNING))
