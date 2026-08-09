from __future__ import annotations

import httpx
import structlog
from rich.console import Console
from rich.panel import Panel

from pm_bot.core.polymarket import fetch_weather_events, _parse_event
from pm_bot.core.weather import fetch_forecast
from pm_bot.strategies.base import ALL_STRATEGIES
from pm_bot.models.config import STRATEGY_DEFAULTS

console = Console()


async def run_explain(market_id: str, debug: bool = False) -> None:
    _setup_logging(debug)

    async with httpx.AsyncClient(timeout=30.0) as client:
        # Try fetching directly by ID first
        try:
            resp = await client.get(f"https://gamma-api.polymarket.com/events/{market_id}")
            if resp.status_code == 200:
                ev_data = resp.json()
                target = _parse_event(ev_data)
                if not target:
                    target = await _find_in_events(client, market_id)
            else:
                target = await _find_in_events(client, market_id)
        except httpx.HTTPError:
            target = await _find_in_events(client, market_id)

    if not target:
        console.print(f"[red]Market/event '{market_id}' not found. Try --closed to include settled markets.[/red]")
        return

    console.print(
        Panel(
            f"Event: {target.title}\n"
            f"City:  {target.city}\n"
            f"Date:  {target.date}\n"
            f"Buckets: {len(target.buckets)}\n"
            f"Sum(YES): {target.sum_yes:.3f}\n"
            f"Airport: {target.airport_code or 'unknown'}",
            title="Market Details",
            border_style="blue",
        )
    )

    async with httpx.AsyncClient(timeout=30.0) as client:
        forecast = await fetch_forecast(client, target.city, target.date)

    for strat_name, strat in ALL_STRATEGIES.items():
        kwargs: dict = dict(STRATEGY_DEFAULTS.get(strat_name, {}))
        if strat_name in ("truncation_edge", "ensemble_spread") and forecast:
            pass  # strategies removed
        recs = strat.run(target, **kwargs)

        if recs:
            console.print(f"\n[bold cyan]{strat_name}[/]: {len(recs)} recommendations")
            for r in recs:
                console.print(f"  {r.direction} {r.temp_label} @ {r.price:.2f} | edge={r.edge:.1%}")
                console.print(f"    [dim]{r.reasoning}[/dim]")
        else:
            console.print(f"\n[dim]{strat_name}: no edge found[/dim]")


async def _find_in_events(client: httpx.AsyncClient, market_id: str):
    events = await fetch_weather_events(client, include_closed=True)
    for ev in events:
        if ev.event_id == market_id or ev.event_id.startswith(market_id):
            return ev
        for b in ev.buckets:
            if b.market_id == market_id or b.market_id.startswith(market_id):
                return ev
    return None


def _setup_logging(debug: bool) -> None:
    import logging

    if debug:
        structlog.configure(wrapper_class=structlog.make_filtering_bound_logger(logging.DEBUG))
    else:
        structlog.configure(wrapper_class=structlog.make_filtering_bound_logger(logging.WARNING))
