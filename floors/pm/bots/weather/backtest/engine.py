from __future__ import annotations

import random
from dataclasses import dataclass, field
from datetime import datetime, timedelta, timezone

import httpx
import structlog

from pm_bot.backtest.costs import CostModel, FillModel
from pm_bot.backtest.data import HistoricalDataFetcher
from pm_bot.backtest.metrics import calculate_metrics
from pm_bot.backtest.real_data import RealDataFetcher, ResolvedEvent
from pm_bot.core.kelly import kelly_size
from pm_bot.core.parser import parse_bucket
from pm_bot.core.staged_entry import apply_staged_entry_for_event
from pm_bot.models.market import ForecastResult, Recommendation, TemperatureBucket, WeatherEvent

log = structlog.get_logger()


@dataclass
class SimulatedTrade:
    date: str
    strategy: str
    bucket_key: str
    direction: str
    price: float
    size_usd: float
    cost: float
    pnl: float = 0.0
    resolved: bool = False
    entry_price: float = 0.0
    stop_loss_pct: float = 0.0
    price_source: str = "clob"
    filled: bool = True


@dataclass
class BacktestResult:
    strategy_name: str
    bankroll: float
    final_value: float
    total_pnl: float
    trades: list[SimulatedTrade] = field(default_factory=list)
    sharpe_ratio: float = 0.0
    sortino_ratio: float = 0.0
    max_drawdown: float = 0.0
    win_rate: float = 0.0
    avg_win: float = 0.0
    avg_loss: float = 0.0
    brier_score: float = 0.0


class BacktestEngine:
    """Weather backtest engine — Small Capital Optimized."""

    def __init__(
        self,
        strategies: list,
        bankroll: float = 1000.0,
        days: int = 90,
        costs: CostModel | None = None,
        cities: list[str] | None = None,
        stop_loss_pct: float = 0.0,
        kelly_fraction_val: float = 0.25,
        max_single_pct: float = 0.02,
        max_notional: float = 10.0,
        compound: bool = True,
        live_mode: bool = False,
        seed: int | None = None,
        fill_model: FillModel | None = None,
        preloaded_fetcher: RealDataFetcher | None = None,
        preloaded_events: list | None = None,
        spread_pct: float = 0.0,
        synthetic_only: bool = False,
        emos_calibrators: dict | None = None,
    ) -> None:
        self.strategies = strategies
        self.bankroll = bankroll
        self.days = days
        self.costs = costs or CostModel()
        self.cities = cities or ["NYC"]
        self.stop_loss_pct = stop_loss_pct
        self.kelly_fraction_val = kelly_fraction_val
        self.max_single_pct = max_single_pct
        self.max_notional = max_notional
        self.compound = compound
        self.live_mode = live_mode
        self.seed = seed
        self.spread_pct = spread_pct
        self.synthetic_only = synthetic_only
        self.emos_calibrators = emos_calibrators or {}
        self._preloaded_fetcher = preloaded_fetcher
        self._preloaded_events = preloaded_events
        if fill_model is not None:
            self.costs.fill_model = fill_model
        if live_mode:
            self.costs.live_mode = True
        self._rng = random.Random(seed)

    async def run(self) -> list[BacktestResult]:
        fetcher = HistoricalDataFetcher()
        results: list[BacktestResult] = []

        today = datetime.now(timezone.utc).date()

        async with httpx.AsyncClient(timeout=30.0) as client:
            for strat in self.strategies:
                trades: list[SimulatedTrade] = []
                bankroll_series: list[float] = [self.bankroll]
                current_bankroll = self.bankroll
                cumulative_pnl = 0.0

                for day_offset in range(self.days):
                    run_date = today - timedelta(days=self.days - day_offset)
                    date_str = run_date.isoformat()

                    for city in self.cities:
                        if self.synthetic_only:
                            import random as _rng

                            _seed = hash(f"{city}{date_str}") % 2**32
                            _r = _rng.Random(_seed)
                            temp_high = 20 + _r.uniform(-5, 15)
                            forecast = ForecastResult(
                                city=city,
                                date=date_str,
                                model="synthetic",
                                temp_high_c=temp_high,
                                members=[temp_high + _r.gauss(0, 3.0) for _ in range(200)],
                            )
                            obs_temp = temp_high + _r.gauss(0, 1)
                        else:
                            forecast = await fetcher.fetch_historical_forecasts(client, city, date_str)
                            obs_temp = await fetcher.fetch_historical_observations(client, city, date_str)

                        if not forecast:
                            continue

                        event = self._build_synthetic_event(city, date_str, forecast)
                        kwargs: dict = {"forecast": forecast, "bankroll": current_bankroll}

                        # Pass EMOS calibrator if available
                        if city in self.emos_calibrators:
                            kwargs["emos_calibrator"] = self.emos_calibrators[city]

                        if obs_temp is not None:
                            from pm_bot.core.observation import ObservedTemp

                            obs_obj = ObservedTemp(
                                city=city,
                                observed_c=obs_temp,
                                obs_time_utc=datetime.now(timezone.utc),
                                local_time=datetime.now(timezone.utc),
                                is_past_cutoff=True,
                            )
                            kwargs["observation"] = obs_obj

                        recs = strat.run(event, **kwargs)
                        recs = apply_staged_entry_for_event(recs, event.date)

                        for rec in recs:
                            effective_price = rec.price
                            side = self.costs.live_side if self.live_mode else "taker"

                            size = self._compute_position_size(rec, current_bankroll, effective_price)
                            if size is None:
                                continue
                            if size * rec.bucket.yes_price < 0.5:
                                continue

                            hit = self._bucket_hit(rec.bucket, obs_temp) if obs_temp is not None else None
                            resolved = obs_temp is not None

                            trade = self._resolve_trade(
                                rec=rec,
                                effective_price=effective_price,
                                side=side,
                                size=size,
                                source="clob",
                                hit=hit,
                                resolved=resolved,
                                date_str=date_str,
                                strategy_name=strat.name,
                            )
                            trades.append(trade)

                            if trade.resolved:
                                current_bankroll += trade.pnl
                                current_bankroll = max(current_bankroll, 0.01)
                                cumulative_pnl += trade.pnl
                                if not self.compound:
                                    current_bankroll = self.bankroll

                    self._append_bankroll_series(bankroll_series, current_bankroll, cumulative_pnl)

                metrics = calculate_metrics(trades, bankroll_series)
                results.append(self._build_result(strat.name, cumulative_pnl, current_bankroll, trades, metrics))

        return results

    async def _get_events_and_fetcher(self):
        """Fetch events or use preloaded data. Returns (fetcher, all_events, client_or_none)."""
        if self._preloaded_events is not None and self._preloaded_fetcher is not None:
            fetcher = self._preloaded_fetcher
            all_events = self._preloaded_events
            if self.cities:
                from pm_bot.models.config import resolve_city_alias

                allowed = {resolve_city_alias(c) for c in self.cities}
                all_events = [ev for ev in all_events if ev.city in allowed]
            return fetcher, all_events, None

        fetcher = RealDataFetcher()
        client = httpx.AsyncClient(timeout=30.0)
        return fetcher, None, client

    def _apply_spread(self, price: float, direction: str = "YES") -> float:
        if self.spread_pct <= 0:
            return price
        if price <= 0.15 or price >= 0.85:
            return 0.01
        return min(self.spread_pct, 0.99)

    def _compute_position_size(
        self,
        rec: Recommendation,
        current_bankroll: float,
        effective_price: float,
    ) -> float | None:
        if self.live_mode and rec.edge < self.costs.live_min_edge:
            return None

        size = kelly_size(
            edge=rec.edge,
            yes_price=effective_price,
            bankroll=current_bankroll,
            kelly_fraction_val=self.kelly_fraction_val,
            max_single=current_bankroll * self.max_single_pct,
        )
        size = min(size, self.max_notional)
        if self.live_mode:
            size = min(size, self.costs.live_max_position_usd / max(effective_price, 0.01))
        if self.live_mode and not self.costs.passes_live_filter(rec.edge, size):
            return None
        return size

    def _resolve_trade(
        self,
        rec: Recommendation,
        effective_price: float,
        side: str,
        size: float,
        source: str,
        hit: bool | None,
        resolved: bool,
        date_str: str,
        strategy_name: str,
    ) -> SimulatedTrade:
        filled = True
        if self.live_mode and side == "maker" and self.costs.fill_model is not None:
            fill_prob = self.costs.fill_model.fill_probability(effective_price)
            filled = self._rng.random() < fill_prob

        if not filled:
            return SimulatedTrade(
                date=date_str,
                strategy=strategy_name,
                bucket_key=f"{rec.bucket.temp_low_c:.0f}-{rec.bucket.temp_high_c:.0f}",
                direction=rec.direction,
                price=effective_price,
                size_usd=size,
                cost=0.0,
                pnl=0.0,
                resolved=resolved,
                entry_price=effective_price,
                stop_loss_pct=self.stop_loss_pct,
                price_source=source,
                filled=False,
            )

        cost = self.costs.calculate_cost(side, effective_price, size)
        if source == "forecast":
            cost += self.costs.forecast_penalty_cost(effective_price, size)

        if not resolved or hit is None:
            pnl = 0.0
        else:
            if rec.direction == "YES":
                pnl = size * (1.0 - effective_price) if hit else -size * effective_price
            else:
                pnl = size * (1.0 - effective_price) if not hit else -size * effective_price

        return SimulatedTrade(
            date=date_str,
            strategy=strategy_name,
            bucket_key=f"{rec.bucket.temp_low_c:.0f}-{rec.bucket.temp_high_c:.0f}",
            direction=rec.direction,
            price=effective_price,
            size_usd=size,
            cost=cost,
            pnl=pnl - cost,
            resolved=resolved,
            entry_price=effective_price,
            stop_loss_pct=self.stop_loss_pct,
            price_source=source,
            filled=True,
        )

    def _append_bankroll_series(self, bankroll_series: list[float], current_bankroll: float, cumulative_pnl: float) -> None:
        bankroll_series.append(current_bankroll)

    def _build_result(self, strategy_name: str, cumulative_pnl: float, current_bankroll: float, trades: list[SimulatedTrade], metrics: dict) -> BacktestResult:
        return BacktestResult(
            strategy_name=strategy_name,
            bankroll=self.bankroll,
            final_value=current_bankroll,
            total_pnl=cumulative_pnl,
            trades=trades,
            sharpe_ratio=metrics.get("sharpe", 0.0),
            sortino_ratio=metrics.get("sortino", 0.0),
            max_drawdown=metrics.get("max_drawdown", 0.0),
            win_rate=metrics.get("win_rate", 0.0),
            avg_win=metrics.get("avg_win", 0.0),
            avg_loss=metrics.get("avg_loss", 0.0),
            brier_score=metrics.get("brier_score", 0.0),
        )

    def _build_synthetic_event(self, city: str, date_str: str, forecast: ForecastResult) -> WeatherEvent:
        buckets: list[TemperatureBucket] = []
        center = round(forecast.temp_high_c)
        for offset in range(-4, 5):
            temp = center + offset
            price = max(0.01, min(0.99, 0.50 - offset * 0.04))
            buckets.append(
                TemperatureBucket(
                    market_id=f"{city}-{date_str}-{temp}",
                    question=f"{temp}°C",
                    temp_low=float(temp),
                    temp_high=float(temp),
                    temp_unit="C",
                    yes_price=price,
                    no_price=1.0 - price,
                    volume=1000.0,
                )
            )
        return WeatherEvent(
            event_id=f"{city}-{date_str}",
            title=f"Highest temp in {city} on {date_str}",
            slug=f"{city}-{date_str}",
            city=city,
            date=date_str,
            measure_type="high",
            buckets=buckets,
        )

    def _bucket_hit(self, bucket: TemperatureBucket, obs_c: float) -> bool:
        low = bucket.temp_low_c
        high = bucket.temp_high_c
        if low == float("-inf"):
            return obs_c <= high
        if high == float("inf"):
            return obs_c >= low
        return low <= obs_c <= high

    def _real_bucket_hit(self, ev: ResolvedEvent, bucket: TemperatureBucket) -> bool:
        # Get resolved temp from the actual_temps cache
        key = f"{ev.city}|{ev.target_date}"
        obs_c = self._actual_temps.get(key) if hasattr(self, '_actual_temps') else None
        if obs_c is None:
            # If no actual temp, check if any market is winning
            for m in ev.markets:
                if m.winning:
                    # The winning bucket hit
                    parsed = self._parse_bucket_from_question(m.question)
                    if parsed and bucket.temp_low_c == parsed[0] and bucket.temp_high_c == parsed[1]:
                        return True
            return False
        return self._bucket_hit(bucket, obs_c)

    def _parse_bucket_from_question(self, question: str) -> tuple[float, float] | None:
        """Extract temp range from bucket question."""
        import re
        # Match patterns like "between 52-53°F" or "51°F or below"
        range_match = re.search(r'between\s+(\d+)-(\d+)°[FC]', question)
        if range_match:
            return float(range_match.group(1)), float(range_match.group(2))
        below_match = re.search(r'(\d+)°[FC]\s+or\s+below', question)
        if below_match:
            return -999.0, float(below_match.group(1))
        above_match = re.search(r'(\d+)°[FC]\s+or\s+(?:above|higher)', question)
        if above_match:
            return float(above_match.group(1)), 999.0
        return None

    def _get_resolved_temp(self, ev: ResolvedEvent) -> float | None:
        return getattr(ev, "resolved_c", None)

    def _build_real_event_from_resolution(
        self, ev: ResolvedEvent, forecast: ForecastResult
    ) -> tuple[WeatherEvent | None, dict[str, str]]:
        """Convert a ResolvedEvent into a WeatherEvent with buckets from market data."""
        from pm_bot.core.parser import parse_bucket

        buckets: list[TemperatureBucket] = []
        price_sources: dict[str, str] = {}

        for mkt in ev.markets:
            # Parse the bucket from the question
            parsed = parse_bucket(mkt.question)
            if parsed is None:
                continue

            # Use the best available price
            yes_price = mkt.yes_price
            price_source = mkt.price_source or "clob"

            # If we have price history, use the last price before resolution
            if mkt.price_history:
                last_price = mkt.price_history[-1]
                yes_price = last_price.price
                price_source = "clob"

            if yes_price <= 0:
                continue

            # Update the parsed bucket with actual price
            bucket = TemperatureBucket(
                market_id=mkt.token_id,
                question=mkt.question,
                temp_low=parsed.temp_low_c,
                temp_high=parsed.temp_high_c,
                temp_unit="C",  # Already converted to Celsius by parse_bucket
                yes_price=yes_price,
                no_price=1.0 - yes_price,
                volume=1000.0,
            )
            buckets.append(bucket)
            price_sources[mkt.token_id] = price_source

        if not buckets:
            return None, {}

        event = WeatherEvent(
            event_id=ev.event_id,
            title=ev.title,
            slug=ev.slug,
            city=ev.city,
            date=ev.target_date,
            measure_type=ev.measure_type,
            buckets=buckets,
        )
        return event, price_sources

    async def run_real(self) -> list[BacktestResult]:
        fetcher, preloaded_events, client = await self._get_events_and_fetcher()
        results: list[BacktestResult] = []

        async with client or httpx.AsyncClient(timeout=30.0) as client_inner:
            if preloaded_events is not None:
                all_events = preloaded_events
            else:
                resolved_events = await fetcher.fetch_resolved_weather_events(client_inner, days=self.days)
                active_events = await fetcher.fetch_active_weather_events(client_inner, days=self.days)
                all_events = resolved_events + active_events

                if not all_events:
                    log.warning("no_events_found", days=self.days)
                    fetcher.close()
                    return []

                if self.cities:
                    from pm_bot.models.config import resolve_city_alias

                    allowed = {resolve_city_alias(c) for c in self.cities}
                    all_events = [ev for ev in all_events if ev.city in allowed]

                log.info(
                    "events_count",
                    resolved=len(resolved_events),
                    active=len(active_events),
                    total=len(all_events),
                )

                if all_events and all_events[0].markets:
                    sample_m = all_events[0].markets[0]
                    if len(sample_m.token_id) > 20:
                        await fetcher.enrich_events_with_clob_prices(client_inner, all_events)
                        from pm_bot.core.config_loader import load_config

                        config = load_config()
                        dune_key = config.get("dune", {}).get("api_key", "")
                        if dune_key:
                            await fetcher.enrich_events_with_dune_prices(client_inner, all_events, dune_key)

                self._actual_temps: dict[str, float] = {}
                actual_temps = await fetcher.fetch_actual_temps(client_inner, all_events)
                self._actual_temps = actual_temps
                log.info("actual_temps_loaded", count=len(actual_temps))

                active_prices = await fetcher.fetch_active_market_prices(client_inner)
                self._active_price_cache = active_prices

                unique_cities = list({ev.city for ev in all_events})
                await fetcher.prefetch_forecasts(client_inner, unique_cities, self.days)

            fill_count = 0
            skip_count = 0

            for strat in self.strategies:
                trades: list[SimulatedTrade] = []
                bankroll_series: list[float] = [self.bankroll]
                current_bankroll = self.bankroll
                cumulative_pnl = 0.0

                for ev in all_events:
                    forecast = fetcher.get_cached_forecast(ev.city, ev.target_date)
                    if forecast is None:
                        log.debug("no_forecast_for_event", city=ev.city, date=ev.target_date)
                        continue

                    event, price_sources = self._build_real_event_from_resolution(ev, forecast)
                    if event is None:
                        continue

                    kwargs: dict = {"forecast": forecast, "bankroll": current_bankroll}

                    # Pass EMOS calibrator if available
                    if ev.city in self.emos_calibrators:
                        kwargs["emos_calibrator"] = self.emos_calibrators[ev.city]

                    resolved_temp = self._get_resolved_temp(ev)
                    if resolved_temp is not None:
                        from pm_bot.core.observation import ObservedTemp

                        obs_obj = ObservedTemp(
                            city=ev.city,
                            observed_c=resolved_temp,
                            obs_time_utc=datetime.now(timezone.utc),
                            local_time=datetime.now(timezone.utc),
                            is_past_cutoff=True,
                        )
                        kwargs["observation"] = obs_obj

                    recs = strat.run(event, **kwargs)
                    recs = apply_staged_entry_for_event(recs, event.date)

                    for rec in recs:
                        effective_price = self._apply_spread(rec.price)
                        side = self.costs.live_side if self.live_mode else "taker"
                        source = price_sources.get(rec.bucket.market_id, "clob")

                        size = self._compute_position_size(rec, current_bankroll, effective_price)
                        if size is None:
                            continue
                        if size * effective_price < 0.5:
                            continue

                        hit = self._real_bucket_hit(ev, rec.bucket)
                        trade = self._resolve_trade(
                            rec=rec,
                            effective_price=effective_price,
                            side=side,
                            size=size,
                            source=source,
                            hit=hit,
                            resolved=True,
                            date_str=ev.target_date,
                            strategy_name=strat.name,
                        )
                        trades.append(trade)

                        if not trade.filled:
                            skip_count += 1
                            continue
                        fill_count += 1

                        current_bankroll += trade.pnl
                        current_bankroll = max(current_bankroll, 0.01)
                        cumulative_pnl += trade.pnl
                        if not self.compound:
                            current_bankroll = self.bankroll

                    self._append_bankroll_series(bankroll_series, current_bankroll, cumulative_pnl)

                metrics = calculate_metrics(trades, bankroll_series)
                results.append(self._build_result(strat.name, cumulative_pnl, current_bankroll, trades, metrics))

        if self.live_mode:
            log.info("fill_model_stats", filled=fill_count, skipped=skip_count)

        if self._preloaded_fetcher is None:
            fetcher.close()
        return results

    async def run_portfolio(self) -> BacktestResult:
        fetcher, preloaded_events, client = await self._get_events_and_fetcher()

        async with client or httpx.AsyncClient(timeout=30.0) as client_inner:
            if preloaded_events is not None:
                all_events = preloaded_events
            else:
                resolved_events = await fetcher.fetch_resolved_weather_events(client_inner, days=self.days)
                active_events = await fetcher.fetch_active_weather_events(client_inner, days=self.days)
                all_events = resolved_events + active_events

                if not all_events:
                    log.warning("no_events_found", days=self.days)
                    fetcher.close()
                    return BacktestResult(
                        strategy_name="portfolio", bankroll=self.bankroll, final_value=self.bankroll, total_pnl=0.0
                    )

                if self.cities:
                    from pm_bot.models.config import resolve_city_alias

                    allowed = {resolve_city_alias(c) for c in self.cities}
                    all_events = [ev for ev in all_events if ev.city in allowed]

                if all_events and all_events[0].markets:
                    sample_m = all_events[0].markets[0]
                    if len(sample_m.token_id) > 20:
                        await fetcher.enrich_events_with_clob_prices(client_inner, all_events)

            all_recs: list[Recommendation] = []
            current_bankroll = self.bankroll

            for ev in all_events:
                forecast = fetcher.get_cached_forecast(ev.city, ev.target_date)
                if forecast is None:
                    continue
                event, price_sources = self._build_real_event_from_resolution(ev, forecast)
                if event is None:
                    continue
                for strat in self.strategies:
                    recs = strat.run(event, forecast=forecast, bankroll=current_bankroll)
                    recs = apply_staged_entry_for_event(recs, event.date)
                    all_recs.extend(recs)

            for rec in all_recs:
                size = self._compute_position_size(rec, current_bankroll, rec.price)
                if size is None:
                    continue
                current_bankroll = max(current_bankroll - size * rec.price, 0.01)

            return BacktestResult(
                strategy_name="portfolio",
                bankroll=self.bankroll,
                final_value=current_bankroll,
                total_pnl=current_bankroll - self.bankroll,
                trades=[],
            )
