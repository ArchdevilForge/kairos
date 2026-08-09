#!/usr/bin/env python3
"""Comprehensive Weather Strategies Backtest.

Tests 6 strategies (5 individual + 1 combined) across 12 cities
with realistic synthetic market data, EMOS calibration, and
Monte Carlo simulation.

Strategies tested:
1. Ladder (neobrother): adjacent bucket coverage
2. Tail (Hans323): underpriced tail buckets
3. Latency (Hans323): forecast model update delay
4. Gopfan2: simple price rules (YES<15¢, NO>45¢)
5. Consensus: fade lopsided markets
6. Combined: weighted mix of all 5

Usage:
    uv run python3 run_weather_backtest.py
"""

from __future__ import annotations

import json
import random
import time
from dataclasses import dataclass, field
from datetime import datetime, timedelta
from math import erf, sqrt
from pathlib import Path

import numpy as np

# Import strategies
import sys
sys.path.insert(0, str(Path(__file__).parent / "bots" / "weather"))
from backtest.weather_strategies import (
    Bucket,
    WeatherStrategy,
    LadderStrategy,
    TailStrategy,
    Gopfan2Strategy,
    CombinedStrategy,
    get_all_strategies,
    load_emos_coeffs,
    emos_calibrate,
    emos_bucket_prob,
)

# City configurations for backtesting
CITIES = {
    "New York": {
        "lat": 40.7772, "lon": -73.8726,
        "temp_range": (-5, 38), "season_amp": 15,
        "station_bias": 1.5, "tz_offset": -5,
        "icao": "KLGA", "liquidity": "high",
    },
    "Miami": {
        "lat": 25.7953, "lon": -80.2902,
        "temp_range": (15, 38), "season_amp": 5,
        "station_bias": 0.3, "tz_offset": -5,
        "icao": "KMIA", "liquidity": "high",
    },
    "London": {
        "lat": 51.5048, "lon": 0.0495,
        "temp_range": (-2, 32), "season_amp": 8,
        "station_bias": 0.5, "tz_offset": 0,
        "icao": "EGLC", "liquidity": "high",
    },
    "Paris": {
        "lat": 48.9694, "lon": 2.4414,
        "temp_range": (-5, 38), "season_amp": 10,
        "station_bias": 0.8, "tz_offset": 1,
        "icao": "LFPB", "liquidity": "medium",
    },
    "Tokyo": {
        "lat": 35.5522, "lon": 139.7796,
        "temp_range": (2, 38), "season_amp": 12,
        "station_bias": 0.7, "tz_offset": 9,
        "icao": "RJTT", "liquidity": "high",
    },
    "Seoul": {
        "lat": 37.4602, "lon": 126.4407,
        "temp_range": (-12, 38), "season_amp": 15,
        "station_bias": 1.0, "tz_offset": 9,
        "icao": "RKSI", "liquidity": "medium",
    },
    "Shanghai": {
        "lat": 31.1443, "lon": 121.8083,
        "temp_range": (-2, 40), "season_amp": 12,
        "station_bias": 0.8, "tz_offset": 8,
        "icao": "ZSPD", "liquidity": "high",
    },
    "Hong Kong": {
        "lat": 22.3080, "lon": 113.9185,
        "temp_range": (8, 38), "season_amp": 5,
        "station_bias": 0.3, "tz_offset": 8,
        "icao": "VHHH", "liquidity": "medium",
    },
    "Chicago": {
        "lat": 41.9742, "lon": -87.9073,
        "temp_range": (-18, 38), "season_amp": 18,
        "station_bias": 0.8, "tz_offset": -6,
        "icao": "KORD", "liquidity": "medium",
    },
    "Atlanta": {
        "lat": 33.6407, "lon": -84.4277,
        "temp_range": (-2, 40), "season_amp": 10,
        "station_bias": 0.5, "tz_offset": -5,
        "icao": "KATL", "liquidity": "medium",
    },
    "Los Angeles": {
        "lat": 33.9425, "lon": -118.4081,
        "temp_range": (8, 38), "season_amp": 5,
        "station_bias": 0.4, "tz_offset": -8,
        "icao": "KLAX", "liquidity": "medium",
    },
    "San Francisco": {
        "lat": 37.6213, "lon": -122.3790,
        "temp_range": (5, 28), "season_amp": 4,
        "station_bias": 0.3, "tz_offset": -8,
        "icao": "KSFO", "liquidity": "low",
    },
}


# ── Market Simulation ──
@dataclass
class MarketSimConfig:
    """Configuration for synthetic market simulation."""
    base_noise_std: float = 2.0        # Base noise in market pricing
    crowd_bias_factor: float = 0.8     # How much crowd follows model (1.0 = perfect)
    underdispersion_factor: float = 1.15  # Crowd underestimates spread
    liquidity_factor: float = 1.0      # Higher = tighter spreads
    tail_underprice_factor: float = 0.7  # Tails underpriced by this factor


def simulate_market_buckets(
    true_temp: float,
    members: list[float],
    rng: random.Random,
    city: str = "",
    cfg: MarketSimConfig | None = None,
) -> list[Bucket]:
    """Generate synthetic market buckets with realistic biases."""
    if cfg is None:
        cfg = MarketSimConfig()

    # City-specific crowd biases
    city_biases = {
        "New York": 0.8, "Miami": 0.3, "London": 1.2, "Paris": 1.0,
        "Tokyo": 0.6, "Seoul": 1.5, "Shanghai": 0.7, "Hong Kong": 0.2,
        "Chicago": 1.0, "Atlanta": 0.4, "Los Angeles": 0.3, "San Francisco": 0.5,
    }

    # Temperature-dependent bias
    bias = city_biases.get(city, 0.5)
    if true_temp < 10:
        crowd_bias = -bias * 0.8  # Crowd underpredicts cold
    elif true_temp > 25:
        crowd_bias = bias * 0.3   # Crowd slightly overpredicts hot
    else:
        crowd_bias = rng.gauss(0, 0.3)

    # Market mean is biased version of true temp
    market_mean = true_temp + crowd_bias + rng.gauss(0, cfg.base_noise_std * 0.3)
    market_std = max(1.5, 2.0 - abs(crowd_bias) * 0.2) * cfg.underdispersion_factor

    # Generate buckets in 2°C increments
    temp_min = int(np.floor(true_temp)) - 12
    temp_max = int(np.ceil(true_temp)) + 12

    buckets = []
    for low in range(temp_min, temp_max, 2):
        high = low + 2

        # Market price from Gaussian
        z_low = (low - market_mean) / market_std
        z_high = (high - market_mean) / market_std
        market_prob = 0.5 * (erf(z_high / sqrt(2)) - erf(z_low / sqrt(2)))

        # Underdispersion: crowd overprices center, underprices tails
        center = (low + high) / 2
        dist_from_center = abs(center - market_mean)
        if dist_from_center < market_std:
            market_prob *= 1.15  # Overprice center
        else:
            market_prob *= cfg.tail_underprice_factor  # Underprice tails

        market_prob = max(0.01, min(0.99, market_prob))
        market_price = market_prob + rng.gauss(0, 0.012)
        market_price = max(0.01, min(0.99, market_price))

        # True probability from ensemble members
        arr = np.array(members)
        true_prob = float(np.sum((arr >= low) & (arr <= high))) / len(members)

        buckets.append(Bucket(
            low_c=float(low), high_c=float(high),
            market_price=market_price, true_prob=true_prob,
        ))

    return buckets


def generate_forecast(
    city: str, city_cfg: dict, date: datetime, rng: random.Random,
    forecast_error_std: float = 0.3,
) -> tuple[float, list[float]]:
    """Generate synthetic forecast for a city/date."""
    day_of_year = date.timetuple().tm_yday
    season_phase = 2 * np.pi * day_of_year / 365.0

    temp_min, temp_max = city_cfg["temp_range"]
    season_amp = city_cfg["season_amp"]
    mid_temp = (temp_min + temp_max) / 2.0
    true_temp = mid_temp + season_amp * np.sin(season_phase - np.pi / 2)
    true_temp += rng.gauss(0, 3.0)

    # Apply station bias
    station_bias = city_cfg.get("station_bias", 0.0)
    true_temp_station = true_temp - station_bias

    # Generate ensemble members
    n_members = 31
    members = []
    for _ in range(n_members):
        member = true_temp_station + rng.gauss(0, forecast_error_std) + rng.gauss(0, 2.0)
        members.append(member)

    return true_temp_station, members


# ── Trade Execution ──
@dataclass
class ExecutionConfig:
    """Trade execution parameters."""
    slippage_pct: float = 0.015        # 1.5% slippage
    taker_fee_bps: int = 50            # 0.5% taker fee
    min_fill_prob: float = 0.50        # Fill probability at best price
    fill_delay_minutes: float = 3.0    # Average fill delay
    edge_decay_per_min: float = 0.001  # Edge decay per minute of delay


def execute_trade(
    signal,
    bankroll: float,
    rng: random.Random,
    exec_cfg: ExecutionConfig | None = None,
) -> ExecutedTrade | None:
    """Execute a trade signal with realistic costs."""
    if exec_cfg is None:
        exec_cfg = ExecutionConfig()

    # Compute position size
    size_pct = signal.size_pct
    size_usd = size_pct * bankroll
    size_usd = min(size_usd, 50.0)
    size_usd = max(size_usd, 1.0)

    if size_usd > bankroll * 0.5:
        return None

    # Apply edge decay from fill delay
    decay = exec_cfg.edge_decay_per_min * exec_cfg.fill_delay_minutes
    effective_edge = signal.edge - decay

    if effective_edge <= 0:
        return None

    # Fill price with slippage
    if signal.direction == "YES":
        fill_price = signal.bucket.market_price * (1.0 + exec_cfg.slippage_pct)
    else:
        fill_price = (1.0 - signal.bucket.market_price) * (1.0 + exec_cfg.slippage_pct)

    fill_price = min(fill_price, 0.99)
    fill_price = max(fill_price, 0.01)

    # Cost
    cost = exec_cfg.taker_fee_bps / 10000.0 * fill_price * size_usd

    # Resolution: check if trade wins
    if signal.direction == "YES":
        won = rng.random() < signal.bucket.true_prob
        if won:
            pnl = size_usd * (1.0 - fill_price)
        else:
            pnl = -size_usd * fill_price
    else:
        won = rng.random() >= signal.bucket.true_prob
        if won:
            pnl = size_usd * fill_price
        else:
            pnl = -size_usd * (1.0 - fill_price)

    net_pnl = pnl - cost

    return ExecutedTrade(
        city=signal.city,
        date=signal.date,
        bucket_low=signal.bucket.low_c,
        bucket_high=signal.bucket.high_c,
        direction=signal.direction,
        entry_price=fill_price,
        model_prob=signal.model_prob,
        edge=effective_edge,
        size_usd=size_usd,
        won=won,
        pnl=net_pnl,
        cost=cost,
        strategy=signal.strategy,
        lead_time_hours=48.0,
    )


# ── Backtest Engine ──
@dataclass
class BacktestResult:
    """Result of a single backtest run."""
    strategy_name: str
    scenario: str
    days: int
    bankroll_start: float
    bankroll_final: float
    total_return_pct: float
    total_trades: int
    wins: int
    losses: int
    win_rate: float
    avg_edge: float
    avg_trade_size: float
    max_drawdown_pct: float
    sharpe_ratio: float
    sortino_ratio: float
    profit_factor: float
    total_costs: float
    trades_per_day: float
    avg_pnl_per_trade: float
    best_trade_pnl: float
    worst_trade_pnl: float
    city_pnl: dict[str, float] = field(default_factory=dict)
    daily_pnl: list[float] = field(default_factory=list)


def run_backtest(
    strategy: WeatherStrategy,
    scenario_name: str,
    days: int = 90,
    bankroll: float = 100.0,
    seed: int = 42,
    forecast_error_std: float = 0.3,
    exec_cfg: ExecutionConfig | None = None,
    market_cfg: MarketSimConfig | None = None,
    emos_coeffs: dict | None = None,
    cities: list[str] | None = None,
) -> BacktestResult:
    """Run a single backtest scenario."""
    rng = random.Random(seed)
    np.random.seed(seed)

    if exec_cfg is None:
        exec_cfg = ExecutionConfig()
    if market_cfg is None:
        market_cfg = MarketSimConfig()
    if cities is None:
        cities = list(CITIES.keys())

    current_bankroll = bankroll
    trades: list[ExecutedTrade] = []
    daily_pnl: list[float] = []
    peak_bankroll = bankroll
    max_dd = 0.0
    today = datetime.now()

    for day_offset in range(days):
        date = today - timedelta(days=days - day_offset)
        day_pnl = 0.0

        for city in cities:
            if city not in CITIES:
                continue
            city_cfg = CITIES[city]
            coeffs = (emos_coeffs or {}).get(city, {})

            # Generate forecast
            true_temp, members = generate_forecast(
                city, city_cfg, date, rng, forecast_error_std
            )

            # EMOS calibration
            if coeffs:
                mu, sigma = emos_calibrate(members, coeffs)
            else:
                mu = float(np.mean(members))
                sigma = float(np.std(members)) if len(members) > 1 else 2.5

            # Generate market buckets
            buckets = simulate_market_buckets(
                true_temp, members, rng, city, market_cfg
            )

            # Lead time simulation (random between 6-72h)
            lead_time_hours = rng.uniform(6, 72)

            # Generate signals
            signals = strategy.generate_signals(
                city=city,
                date=date.strftime("%Y-%m-%d"),
                buckets=buckets,
                model_mu=mu,
                model_sigma=sigma,
                emos_coeffs=coeffs if coeffs else None,
                lead_time_hours=lead_time_hours,
            )

            # Execute signals
            for signal in signals:
                if current_bankroll < 1.0:
                    break

                trade = execute_trade(signal, current_bankroll, rng, exec_cfg)
                if trade is None:
                    continue

                trades.append(trade)
                current_bankroll += trade.pnl
                day_pnl += trade.pnl

                if current_bankroll > peak_bankroll:
                    peak_bankroll = current_bankroll
                dd = (peak_bankroll - current_bankroll) / peak_bankroll
                max_dd = max(max_dd, dd)

                if current_bankroll < 1.0:
                    current_bankroll = 0.0
                    break

        daily_pnl.append(day_pnl)

        if current_bankroll < 1.0:
            break

    # Compute metrics
    total_trades = len(trades)
    wins = sum(1 for t in trades if t.won)
    losses = total_trades - wins
    win_rate = wins / max(total_trades, 1)
    avg_edge = sum(t.edge for t in trades) / max(total_trades, 1)
    avg_trade_size = sum(t.size_usd for t in trades) / max(total_trades, 1)
    total_costs = sum(t.cost for t in trades)

    # Sharpe ratio
    if daily_pnl:
        returns = np.array(daily_pnl) / max(bankroll, 1)
        sharpe = float(np.mean(returns) / max(np.std(returns), 1e-10) * np.sqrt(365))
    else:
        sharpe = 0.0

    # Sortino ratio
    if daily_pnl:
        downside = [r for r in returns if r < 0]
        downside_std = float(np.std(downside)) if downside else 1e-10
        sortino = float(np.mean(returns) / max(downside_std, 1e-10) * np.sqrt(365))
    else:
        sortino = 0.0

    # Profit factor
    gross_profit = sum(t.pnl for t in trades if t.pnl > 0)
    gross_loss = abs(sum(t.pnl for t in trades if t.pnl < 0))
    profit_factor = gross_profit / max(gross_loss, 0.01)

    # City P&L
    city_pnl: dict[str, float] = {}
    for t in trades:
        city_pnl[t.city] = city_pnl.get(t.city, 0) + t.pnl

    return BacktestResult(
        strategy_name=strategy.name,
        scenario=scenario_name,
        days=days,
        bankroll_start=bankroll,
        bankroll_final=current_bankroll,
        total_return_pct=((current_bankroll - bankroll) / bankroll) * 100,
        total_trades=total_trades,
        wins=wins,
        losses=losses,
        win_rate=win_rate,
        avg_edge=avg_edge,
        avg_trade_size=avg_trade_size,
        max_drawdown_pct=max_dd * 100,
        sharpe_ratio=sharpe,
        sortino_ratio=sortino,
        profit_factor=profit_factor,
        total_costs=total_costs,
        trades_per_day=total_trades / max(days, 1),
        avg_pnl_per_trade=sum(t.pnl for t in trades) / max(total_trades, 1),
        best_trade_pnl=max((t.pnl for t in trades), default=0),
        worst_trade_pnl=min((t.pnl for t in trades), default=0),
        city_pnl=city_pnl,
        daily_pnl=daily_pnl,
    )


def run_monte_carlo(
    strategy: WeatherStrategy,
    scenario_name: str,
    n_sims: int = 100,
    days: int = 90,
    bankroll: float = 100.0,
    **kwargs,
) -> list[BacktestResult]:
    """Run Monte Carlo simulation."""
    results = []
    for i in range(n_sims):
        seed = i * 17 + 7
        result = run_backtest(
            strategy=strategy,
            scenario_name=f"{scenario_name}_mc{i}",
            days=days,
            bankroll=bankroll,
            seed=seed,
            **kwargs,
        )
        results.append(result)
    return results


# ── Report Generation ──
def print_header(title: str):
    """Print formatted header."""
    print("\n" + "=" * 80)
    print(f"  {title}")
    print("=" * 80)


def print_scenario_table(results: list[BacktestResult]):
    """Print scenario comparison table."""
    print("\n" + "=" * 120)
    print(f"{'策略':<18} {'场景':<22} {'终值':>7} {'收益%':>7} {'交易':>5} {'胜率':>5} {'优势':>5} "
          f"{'回撤':>5} {'夏普':>5} {'Sortino':>7} {'PF':>5} {'日均':>5} {'成本':>6}")
    print("-" * 120)

    for r in results:
        print(
            f"{r.strategy_name:<18} "
            f"{r.scenario:<22} "
            f"${r.bankroll_final:>6.0f} "
            f"{r.total_return_pct:>+6.1f}% "
            f"{r.total_trades:>5} "
            f"{r.win_rate:>4.0%} "
            f"{r.avg_edge:>4.0%} "
            f"{r.max_drawdown_pct:>4.0f}% "
            f"{r.sharpe_ratio:>5.2f} "
            f"{r.sortino_ratio:>7.2f} "
            f"{r.profit_factor:>5.2f} "
            f"{r.trades_per_day:>5.1f} "
            f"${r.total_costs:>5.1f}"
        )
    print("=" * 120)


def print_mc_summary(results: list[BacktestResult], label: str = ""):
    """Print Monte Carlo summary statistics."""
    returns = [r.total_return_pct for r in results]
    drawdowns = [r.max_drawdown_pct for r in results]
    win_rates = [r.win_rate for r in results]
    sharpes = [r.sharpe_ratio for r in results]

    print(f"\n{'=' * 70}")
    print(f"蒙特卡洛统计 ({label}, {len(results)} 次模拟)")
    print(f"{'-' * 70}")
    print(f"  收益统计:")
    print(f"    平均收益:     {np.mean(returns):+.1f}%")
    print(f"    中位数:       {np.median(returns):+.1f}%")
    print(f"    标准差:       {np.std(returns):.1f}%")
    print(f"    5th 分位:     {np.percentile(returns, 5):+.1f}%")
    print(f"    25th 分位:    {np.percentile(returns, 25):+.1f}%")
    print(f"    75th 分位:    {np.percentile(returns, 75):+.1f}%")
    print(f"    95th 分位:    {np.percentile(returns, 95):+.1f}%")
    print(f"  风险统计:")
    print(f"    盈利概率:     {sum(1 for r in returns if r > 0) / len(returns):.0%}")
    print(f"    亏损>20%:     {sum(1 for r in returns if r < -20) / len(returns):.0%}")
    print(f"    平均回撤:     {np.mean(drawdowns):.1f}%")
    print(f"    最大回撤:     {np.max(drawdowns):.1f}%")
    print(f"  交易统计:")
    print(f"    平均交易:     {np.mean([r.total_trades for r in results]):.0f}")
    print(f"    平均胜率:     {np.mean(win_rates):.0%}")
    print(f"    平均夏普:     {np.mean(sharpes):.2f}")
    print(f"    最佳收益:     {np.max(returns):+.1f}%")
    print(f"    最差收益:     {np.min(returns):+.1f}%")
    print(f"{'=' * 70}")


def print_city_breakdown(result: BacktestResult):
    """Print city-level P&L breakdown."""
    print(f"\n{'=' * 60}")
    print(f"城市收益分布 ({result.strategy_name})")
    print(f"{'-' * 60}")

    for city in sorted(result.city_pnl.keys(), key=lambda c: result.city_pnl[c], reverse=True):
        pnl = result.city_pnl[city]
        trades = sum(1 for t in [] if t.city == city)  # placeholder
        emoji = "🟢" if pnl > 0 else "🔴"
        print(f"  {emoji} {city:<20} ${pnl:>+8.2f}")

    print(f"{'=' * 60}")


def main():
    """Main backtest runner."""
    print_header("Polymarket 天气策略综合回测报告")
    print(f"\n回测日期: {datetime.now().strftime('%Y-%m-%d %H:%M')}")
    print(f"测试策略: Ladder, Tail, Latency, Gopfan2, Consensus, Combined")
    print(f"测试城市: {', '.join(CITIES.keys())}")

    # Load EMOS coefficients
    emos_coeffs = load_emos_coeffs()
    print(f"EMOS 系数: {len(emos_coeffs)} 个城市已训练")

    # ── Part 1: Strategy Comparison ──
    print_header("第一部分：策略对比（90天，$100 起始资金）")

    strategies = get_all_strategies()
    comparison_results = []

    for name, strategy in strategies.items():
        print(f"\n运行策略: {name}...")
        t0 = time.time()
        result = run_backtest(
            strategy=strategy,
            scenario_name="90d_comparison",
            days=90,
            bankroll=100.0,
            seed=42,
            forecast_error_std=0.3,
            emos_coeffs=emos_coeffs,
        )
        t1 = time.time()
        print(f"  完成: {t1-t0:.1f}s, {result.total_trades} 笔交易, "
              f"收益: {result.total_return_pct:+.1f}%")
        comparison_results.append(result)

    print_scenario_table(comparison_results)

    # ── Part 2: Monte Carlo for each strategy ──
    print_header("第二部分：蒙特卡洛模拟（每个策略 200 次）")

    mc_results = {}
    for name, strategy in strategies.items():
        print(f"\n蒙特卡洛: {name} (200 次)...")
        t0 = time.time()
        results = run_monte_carlo(
            strategy=strategy,
            scenario_name=name,
            n_sims=200,
            days=90,
            bankroll=100.0,
            forecast_error_std=0.3,
            emos_coeffs=emos_coeffs,
        )
        t1 = time.time()
        mc_results[name] = results

        returns = [r.total_return_pct for r in results]
        win_pct = sum(1 for r in returns if r > 0) / len(returns)
        print(f"  完成: {t1-t0:.1f}s, "
              f"中位数: {np.median(returns):+.1f}%, "
              f"盈利: {win_pct:.0%}")

    # Print detailed MC stats for each strategy
    for name, results in mc_results.items():
        print_mc_summary(results, name)

    # ── Part 3: Time Horizon Comparison ──
    print_header("第三部分：时间跨度对比（最佳策略，30/90/180/365天）")

    # Find best strategy by median MC return
    best_strategy_name = max(
        mc_results.keys(),
        key=lambda k: np.median([r.total_return_pct for r in mc_results[k]])
    )
    best_strategy = strategies[best_strategy_name]
    print(f"\n最佳策略: {best_strategy_name}")

    horizon_results = []
    for days in [30, 90, 180, 365]:
        print(f"\n运行: {days}天...")
        result = run_backtest(
            strategy=best_strategy,
            scenario_name=f"{days}d_{best_strategy_name}",
            days=days,
            bankroll=100.0,
            seed=42,
            forecast_error_std=0.3,
            emos_coeffs=emos_coeffs,
        )
        horizon_results.append(result)
        print(f"  收益: {result.total_return_pct:+.1f}%, "
              f"交易: {result.total_trades}, "
              f"胜率: {result.win_rate:.0%}")

    print_scenario_table(horizon_results)

    # ── Part 4: Sensitivity Analysis ──
    print_header("第四部分：敏感性分析")

    # Edge threshold sensitivity
    print("\n边际阈值敏感性 (Combined 策略):")
    print(f"{'阈值':>8} {'收益':>8} {'交易':>6} {'胜率':>6} {'夏普':>6}")
    print("-" * 40)

    for edge_threshold in [0.02, 0.03, 0.04, 0.05, 0.06, 0.08, 0.10]:
        strat = CombinedStrategy(edge_threshold=edge_threshold)
        result = run_backtest(
            strategy=strat,
            scenario_name=f"edge_{edge_threshold}",
            days=90,
            bankroll=100.0,
            seed=42,
            emos_coeffs=emos_coeffs,
        )
        print(f"  {edge_threshold:>6.0%} "
              f"{result.total_return_pct:>+7.1f}% "
              f"{result.total_trades:>6} "
              f"{result.win_rate:>5.0%} "
              f"{result.sharpe_ratio:>5.2f}")

    # Kelly fraction sensitivity
    print("\nKelly 分数敏感性 (Combined 策略):")
    print(f"{'Kelly':>8} {'收益':>8} {'交易':>6} {'回撤':>6} {'夏普':>6}")
    print("-" * 40)

    for kelly in [0.10, 0.15, 0.20, 0.25, 0.30, 0.40]:
        strat = CombinedStrategy(kelly_fraction=kelly)
        result = run_backtest(
            strategy=strat,
            scenario_name=f"kelly_{kelly}",
            days=90,
            bankroll=100.0,
            seed=42,
            emos_coeffs=emos_coeffs,
        )
        print(f"  {kelly:>6.0%} "
              f"{result.total_return_pct:>+7.1f}% "
              f"{result.total_trades:>6} "
              f"{result.max_drawdown_pct:>5.0f}% "
              f"{result.sharpe_ratio:>5.2f}")

    # ── Part 5: City Performance Analysis ──
    print_header("第五部分：城市表现分析")

    city_results = {}
    for city in CITIES.keys():
        strat = CombinedStrategy()
        result = run_backtest(
            strategy=strat,
            scenario_name=f"city_{city}",
            days=90,
            bankroll=100.0,
            seed=42,
            emos_coeffs=emos_coeffs,
            cities=[city],
        )
        city_results[city] = result

    print(f"\n{'城市':<20} {'收益':>8} {'交易':>6} {'胜率':>6} {'夏普':>6} {'流动性':>8}")
    print("-" * 60)

    for city in sorted(city_results.keys(), key=lambda c: city_results[c].total_return_pct, reverse=True):
        r = city_results[city]
        liq = CITIES[city]["liquidity"]
        print(f"  {city:<18} "
              f"{r.total_return_pct:>+7.1f}% "
              f"{r.total_trades:>6} "
              f"{r.win_rate:>5.0%} "
              f"{r.sharpe_ratio:>5.2f} "
              f"{liq:>8}")

    # ── Part 6: EMOS vs No-EMOS Comparison ──
    print_header("第六部分：EMOS 校准效果对比")

    emos_comparison = []
    for use_emos in [True, False]:
        coeffs = emos_coeffs if use_emos else None
        label = "EMOS" if use_emos else "Raw Ensemble"
        strat = CombinedStrategy()
        result = run_backtest(
            strategy=strat,
            scenario_name=label,
            days=90,
            bankroll=100.0,
            seed=42,
            emos_coeffs=coeffs,
        )
        emos_comparison.append(result)
        print(f"\n  {label}:")
        print(f"    收益: {result.total_return_pct:+.1f}%")
        print(f"    胜率: {result.win_rate:.0%}")
        print(f"    夏普: {result.sharpe_ratio:.2f}")
        print(f"    交易: {result.total_trades}")

    # ── Part 7: Risk Analysis ──
    print_header("第七部分：风险分析")

    best_mc = mc_results[best_strategy_name]
    returns = [r.total_return_pct for r in best_mc]

    print(f"\n最佳策略 ({best_strategy_name}) 风险指标:")
    print(f"  期望收益: {np.mean(returns):+.1f}%")
    print(f"  收益标准差: {np.std(returns):.1f}%")
    print(f"  盈利概率: {sum(1 for r in returns if r > 0) / len(returns):.0%}")
    print(f"  亏损>10%概率: {sum(1 for r in returns if r < -10) / len(returns):.0%}")
    print(f"  亏损>20%概率: {sum(1 for r in returns if r < -20) / len(returns):.0%}")
    print(f"  亏损>50%概率: {sum(1 for r in returns if r < -50) / len(returns):.0%}")

    # Value at Risk
    var_95 = np.percentile(returns, 5)
    var_99 = np.percentile(returns, 1)
    print(f"\n  VaR (95%): {var_95:+.1f}%")
    print(f"  VaR (99%): {var_99:+.1f}%")

    # Expected Shortfall
    es_95 = np.mean([r for r in returns if r <= var_95])
    print(f"  ES (95%): {es_95:+.1f}%")

    # ── Part 8: Final Recommendations ──
    print_header("第八部分：最终建议")

    # Find best strategy for different risk profiles
    conservative = min(mc_results.keys(), key=lambda k: np.std([r.total_return_pct for r in mc_results[k]]))
    aggressive = max(mc_results.keys(), key=lambda k: np.percentile([r.total_return_pct for r in mc_results[k]], 95))
    balanced = max(mc_results.keys(), key=lambda k: np.median([r.total_return_pct for r in mc_results[k]]))

    print(f"""
  策略推荐:

  1. 保守型（最低波动）:
     策略: {conservative}
     中位数收益: {np.median([r.total_return_pct for r in mc_results[conservative]]):+.1f}%
     收益标准差: {np.std([r.total_return_pct for r in mc_results[conservative]]):.1f}%

  2. 平衡型（最佳风险调整收益）:
     策略: {balanced}
     中位数收益: {np.median([r.total_return_pct for r in mc_results[balanced]]):+.1f}%
     盈利概率: {sum(1 for r in mc_results[balanced] if r.total_return_pct > 0) / len(mc_results[balanced]):.0%}

  3. 激进型（最高上行空间）:
     策略: {aggressive}
     95th 分位: {np.percentile([r.total_return_pct for r in mc_results[aggressive]], 95):+.1f}%
     最佳收益: {max(r.total_return_pct for r in mc_results[aggressive]):+.1f}%

  $100 快速复利路径:
    保守: $100 → ${100 * (1 + np.median([r.total_return_pct for r in mc_results[conservative]]) / 100):.0f} (90天)
    平衡: $100 → ${100 * (1 + np.median([r.total_return_pct for r in mc_results[balanced]]) / 100):.0f} (90天)
    激进: $100 → ${100 * (1 + np.percentile([r.total_return_pct for r in mc_results[aggressive]], 75) / 100):.0f} (90天, 75th)

  实盘建议:
    1. Paper trade 4 周验证策略
    2. 从 $100 开始，使用平衡型策略
    3. 重点关注 NYC/London/Tokyo（高流动性）
    4. 每月重训 EMOS 系数
    5. 保持 40% 现金储备
    6. 日亏损>20% 立即停止
    7. 每周提取 50% 利润
""")


if __name__ == "__main__":
    main()
