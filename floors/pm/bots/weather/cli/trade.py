from __future__ import annotations

from typing import Any

import httpx
import structlog
from rich.console import Console
from rich.prompt import Confirm

from pm_bot.core.polymarket import fetch_weather_events
from pm_bot.core.weather import fetch_forecast
from pm_bot.core.clob import ClobTrader
from pm_bot.core.observation import fetch_observation, filter_recommendations
from pm_bot.core.config_loader import load_config, get_station_for_city
from pm_bot.strategies.base import ALL_STRATEGIES, Strategy
from pm_bot.models.config import DEFAULT_CITIES, STRATEGY_DEFAULTS, resolve_city_alias, CITY_COORDS
from pm_bot.cli.display import render_recommendations
from pm_bot.cli.notifications import notify
from pm_bot.models.market import Recommendation, ForecastResult

console = Console()
log = structlog.get_logger()


async def run_trade(
    strategy: str = "all",
    cities_str: str | None = None,
    all_cities: bool = False,
    edge_override: float | None = None,
    include_closed: bool = False,
    confirm: bool = False,
    observed: bool = False,
    debug: bool = False,
) -> None:
    _setup_logging(debug)

    config = load_config()
    trader = ClobTrader(config)

    if confirm and not trader.is_configured():
        console.print("[red]Trading requires POLY_PK and CLOB credentials. Set config.toml [clob] and env vars.[/red]")
        return

    cities = _resolve_cities(cities_str, all_cities)
    strategies = _resolve_strategies(strategy)

    async with httpx.AsyncClient(timeout=30.0) as client:
        events = await fetch_weather_events(client, include_closed=include_closed)
        events = [e for e in events if all_cities or e.city in cities]

        if not events:
            console.print("[yellow]No weather markets found.[/yellow]")
            return

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
                    afc = await fetch_forecast_at(client, float(lat), float(lon), ev.city, ev.date)
                    if afc:
                        airport_forecasts[ev.city] = afc

                city_coords = CITY_COORDS.get(ev.city)
                if city_coords:
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

    if not all_recs:
        console.print("[dim]No edges found above threshold.[/dim]")
        return

    render_recommendations(all_recs)

    if not confirm:
        console.print("\n[dim]Run with --confirm to execute trades[/dim]")
        return

    from pm_bot.core.config_loader import get_sizing

    sizing = get_sizing(config)
    max_single = sizing["max_single"]
    max_daily = sizing["max_daily"]
    console.print(f"\n[bold]Safety limits:[/] max_single=${max_single:.2f}, max_daily=${max_daily:.2f}")
    console.print(f"[dim]Daily spent so far: ${trader.daily_spent:.2f}[/dim]")

    trader.start_heartbeat()
    try:
        for rec in sorted(all_recs, key=lambda r: r.edge, reverse=True):
            bucket = rec.bucket
            price = rec.price
            amount_usd = max_single

            console.print(
                f"\n[bold]{rec.strategy}[/] | {rec.city} | {rec.temp_label} | "
                f"[bold {'green' if rec.direction == 'YES' else 'red'}]{rec.direction}[/] "
                f"@ {price:.2f} | Edge: {rec.edge:.1%}"
            )
            console.print(f"  Amount: ${amount_usd:.2f} | Market: {bucket.market_id[:20]}...")
            console.print(f"  Reason: {rec.reasoning}")

            if not Confirm.ask("Execute this trade?", default=False):
                console.print("[dim]Skipped.[/dim]")
                continue

            if rec.direction == "YES":
                result = trader.place_limit_buy(
                    token_id=bucket.market_id,
                    price=price,
                    size=amount_usd / price if price > 0 else 1,
                    neg_risk=True,
                )
            else:
                result = trader.place_limit_sell(
                    token_id=bucket.market_id,
                    price=price,
                    size=amount_usd / price if price > 0 else 1,
                    neg_risk=True,
                )

            if result:
                order_id = str(result.get("orderID", result.get("order_id", "")))
                console.print(f"[green]Order placed: {order_id[:16]}[/green]")
                await notify(
                    config,
                    "created",
                    rec.strategy,
                    rec.direction,
                    rec.city,
                    rec.temp_label,
                    price,
                    rec.edge,
                    order_id,
                )
            else:
                console.print("[red]Order failed.[/red]")
    finally:
        trader.stop_heartbeat()


async def fetch_forecast_at(
    client: httpx.AsyncClient,
    lat: float,
    lon: float,
    city: str,
    date: str = "",
    model: str = "gfs_seamless",
) -> ForecastResult | None:
    from pm_bot.models.market import ForecastResult
    from pm_bot.core.weather import OPEN_METEO_BASE, _MEMBER_KEYS

    params: dict[str, str | int | float] = {
        "latitude": lat,
        "longitude": lon,
        "daily": "temperature_2m_max",
        "forecast_days": 3,
        "timezone": "auto",
    }
    try:
        params_model = {**params, "models": model}
        resp = await client.get(f"{OPEN_METEO_BASE}/forecast", params=params_model)
        resp.raise_for_status()
        data = resp.json()
    except httpx.HTTPError as e:
        log.error("forecast_at_error", lat=lat, lon=lon, error=str(e))
        return None

    daily = data.get("daily", {})
    temps = daily.get("temperature_2m_max", [])
    main_temp = float(temps[0]) if temps and isinstance(temps[0], (int, float)) else 0.0

    members: list[float] = []
    try:
        from pm_bot.core.weather import ENSEMBLE_BASE

        params_ens = {**params, "models": model}
        resp = await client.get(ENSEMBLE_BASE, params=params_ens)
        resp.raise_for_status()
        ens_data = resp.json()
        ens_daily = ens_data.get("daily", {})
        for mk in _MEMBER_KEYS:
            member_data = ens_daily.get(mk, [])
            if member_data:
                v = member_data[0]
                if isinstance(v, (int, float)):
                    members.append(float(v))
    except httpx.HTTPError:
        pass

    return ForecastResult(
        city=city,
        date=date,
        model=model,
        temp_high_c=main_temp,
        members=members,
    )


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
