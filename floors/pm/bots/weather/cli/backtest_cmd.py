from __future__ import annotations

from typing import Optional

import structlog
import typer
from rich.console import Console
from rich.table import Table

from pm_bot.backtest.engine import BacktestEngine, BacktestResult
from pm_bot.backtest.report import export_csv, render_comparison_table, render_table
from pm_bot.backtest.costs import CostModel
from pm_bot.strategies.base import get_all_strategies

console = Console()
log = structlog.get_logger()

backtest_app = typer.Typer(help="Backtesting framework", no_args_is_help=True)


@backtest_app.command("run")
def backtest_run(
    strategy: Optional[str] = typer.Option(
        None,
        "--strategy",
        "-s",
        help="Strategy name or comma-separated names (omit for --all)",
    ),
    all_strats: bool = typer.Option(False, "--all", help="Run all strategies"),
    compare: bool = typer.Option(False, "--compare", help="Show side-by-side comparison"),
    bankroll: float = typer.Option(1000.0, "--bankroll", "-b", help="Starting bankroll in USD"),
    days: int = typer.Option(90, "--days", "-d", help="Number of days to backtest"),
    cities: Optional[str] = typer.Option("NYC", "--cities", "-c", help="Comma-separated cities"),
    csv_path: Optional[str] = typer.Option(None, "--csv", help="Export results to CSV file"),
    real: bool = typer.Option(False, "--real", help="Use real Polymarket historical prices and resolved outcomes"),
    stop_loss: float = typer.Option(
        0.0, "--stop-loss", help="Stop-loss as fraction of position (e.g. 0.5 = 50% stop-loss)"
    ),
    kelly: float = typer.Option(0.25, "--kelly", help="Kelly fraction (0.25=quarter, 0.5=half, 1.0=full)"),
    max_pos: float = typer.Option(0.02, "--max-pos", help="Max single position as fraction of bankroll"),
    no_compound: bool = typer.Option(False, "--no-compound", help="Disable compounding (fixed bankroll)"),
    live: bool = typer.Option(
        False, "--live", help="Live-trading mode: maker-only, $10/pos cap, 8%+ edge, ghost-trade friction"
    ),
    compare_forecast: bool = typer.Option(
        False, "--compare-forecast", help="Dual-run: all markets vs CLOB-only, showing forecast bias delta"
    ),
    forecast_penalty: float = typer.Option(
        0.05,
        "--forecast-penalty",
        help="Conservative penalty (cents/share) for forecast-derived prices (default: 0.05)",
    ),
    portfolio: bool = typer.Option(
        False, "--portfolio", help="Portfolio mode: all strategies share one bankroll, merged signals"
    ),
    seed: Optional[int] = typer.Option(None, "--seed", help="Random seed for deterministic FillModel sampling"),
    spread: float = typer.Option(
        0.0,
        "--spread",
        help="Realistic spread: absolute price added to mid for buy orders (e.g. 0.49 for Polymarket weather)",
    ),
    debug: bool = typer.Option(False, "--debug", help="Enable debug logging"),
):
    """Run backtest against historical data."""
    import asyncio

    asyncio.run(
        _run_backtest(
            strategy=strategy,
            all_strats=all_strats,
            compare=compare,
            bankroll=bankroll,
            days=days,
            cities_str=cities,
            csv_path=csv_path,
            real=real,
            stop_loss=stop_loss,
            kelly=kelly,
            max_pos=max_pos,
            no_compound=no_compound,
            live=live,
            compare_forecast=compare_forecast,
            forecast_penalty=forecast_penalty,
            portfolio=portfolio,
            seed=seed,
            spread=spread,
            debug=debug,
        )
    )


async def _run_backtest(
    strategy: str | None,
    all_strats: bool,
    compare: bool,
    bankroll: float,
    days: int,
    cities_str: str | None,
    csv_path: str | None,
    real: bool,
    stop_loss: float,
    kelly: float,
    max_pos: float,
    no_compound: bool,
    live: bool,
    compare_forecast: bool,
    forecast_penalty: float,
    portfolio: bool,
    seed: int | None,
    spread: float,
    debug: bool,
) -> None:
    _setup_logging(debug)
    if live and not real:
        real = True
        console.print("[yellow]--live implies --real; using real market data.[/yellow]")

    all_strategies = get_all_strategies()

    if strategy:
        names = [s.strip() for s in strategy.split(",") if s.strip()]
        unknown = [name for name in names if name not in all_strategies]
        if unknown:
            console.print(f"[red]Unknown strategy: {', '.join(unknown)}. Available: {', '.join(all_strategies)}[/red]")
            return
        strats = [all_strategies[name] for name in names]
    elif all_strats or compare or strategy is None:
        strats = list(all_strategies.values())
    else:
        console.print(f"[red]Unknown strategy: {strategy}. Available: {', '.join(all_strategies)}[/red]")
        return

    city_list = [c.strip() for c in cities_str.split(",")] if cities_str else ["NYC"]

    mode_label = "real market data" if real else "synthetic prices"
    if portfolio:
        mode_label += " (PORTFOLIO)"

    console.print(
        f"[bold]Running backtest ({mode_label}): {len(strats)} strategies, {days} days, ${bankroll:.0f} bankroll[/bold]"
    )

    costs = CostModel()
    costs.forecast_penalty_pct = forecast_penalty

    engine = BacktestEngine(
        strategies=strats,
        bankroll=bankroll,
        days=days,
        costs=costs,
        cities=city_list,
        stop_loss_pct=stop_loss,
        kelly_fraction_val=kelly,
        max_single_pct=max_pos,
        compound=not no_compound,
        live_mode=live,
        seed=seed,
        spread_pct=spread,
    )

    if portfolio and real:
        result = await engine.run_portfolio()
        results = [result]
    elif real:
        results = await engine.run_real()
    else:
        results = await engine.run()

    if not results:
        console.print("[yellow]No results produced.[/yellow]")
        return

    if compare:
        table = render_comparison_table(results)
    else:
        table = render_table(results)
    console.print(table)

    # Dual-run comparison mode: all markets vs CLOB-only
    if compare_forecast and real and results:
        console.print("\n[bold]--- Forecast Bias Comparison (All vs CLOB-only) ---[/bold]")
        clob_only_results = _filter_clob_only(results)
        if clob_only_results:
            bias_table = _render_forecast_bias_table(results, clob_only_results)
            console.print(bias_table)
        else:
            console.print("[dim]No CLOB-only trades found for comparison.[/dim]")

    if csv_path:
        export_csv(results, csv_path)
        console.print(f"[green]CSV exported to {csv_path}[/green]")


def _setup_logging(debug: bool) -> None:
    import logging

    if debug:
        structlog.configure(wrapper_class=structlog.make_filtering_bound_logger(logging.DEBUG))
    else:
        structlog.configure(wrapper_class=structlog.make_filtering_bound_logger(logging.WARNING))


def _filter_clob_only(results: list[BacktestResult]) -> list[BacktestResult]:
    """Create a filtered view excluding forecast-derived price trades."""
    from pm_bot.backtest.engine import BacktestResult as BR

    filtered: list[BR] = []
    for r in results:
        clob_trades = [t for t in r.trades if t.price_source != "forecast" and t.filled]
        if not clob_trades:
            filtered.append(
                BR(
                    strategy_name=r.strategy_name,
                    bankroll=r.bankroll,
                    final_value=r.bankroll,
                    total_pnl=0.0,
                    trades=[],
                )
            )
            continue

        # Recompute P&L from filtered trades with compounding
        current_bankroll = r.bankroll
        cumulative_pnl = 0.0
        for t in clob_trades:
            if t.resolved:
                current_bankroll += t.pnl
                current_bankroll = max(current_bankroll, 0.01)
                cumulative_pnl += t.pnl

        from pm_bot.backtest.metrics import calculate_metrics

        bankroll_series = [r.bankroll] + [r.bankroll + cumulative_pnl]
        metrics = calculate_metrics(clob_trades, bankroll_series)

        filtered.append(
            BR(
                strategy_name=r.strategy_name,
                bankroll=r.bankroll,
                final_value=r.bankroll + cumulative_pnl,
                total_pnl=cumulative_pnl,
                trades=clob_trades,
                sharpe_ratio=metrics.get("sharpe", 0.0),
                sortino_ratio=metrics.get("sortino", 0.0),
                max_drawdown=metrics.get("max_drawdown", 0.0),
                win_rate=metrics.get("win_rate", 0.0),
                avg_win=metrics.get("avg_win", 0.0),
                avg_loss=metrics.get("avg_loss", 0.0),
                brier_score=metrics.get("brier_score", 0.0),
            )
        )
    return filtered


def _render_forecast_bias_table(
    all_results: list[BacktestResult],
    clob_results: list[BacktestResult],
) -> Table:
    """Render side-by-side comparison showing forecast bias delta."""
    table = Table(title="Forecast Bias Analysis", show_lines=True)
    table.add_column("Strategy", style="bold cyan")
    table.add_column("Return (All)", justify="right")
    table.add_column("Return (CLOB)", justify="right")
    table.add_column("Bias Δ", justify="right")
    table.add_column("Trades (All)", justify="right")
    table.add_column("Trades (CLOB)", justify="right")
    table.add_column("Forecast Bias %", justify="right", style="yellow")

    for a, c in zip(all_results, clob_results):
        ret_all = (a.final_value / a.bankroll - 1.0) * 100 if a.bankroll > 0 else 0.0
        ret_clob = (c.final_value / c.bankroll - 1.0) * 100 if c.bankroll > 0 else 0.0
        delta = ret_all - ret_clob

        if ret_clob != 0.0:
            bias_pct = delta / abs(ret_clob) * 100
        else:
            bias_pct = 0.0

        n_all = len([t for t in a.trades if t.filled])
        n_clob = len([t for t in c.trades if t.filled])

        style = "red" if delta > 0 else "green"
        table.add_row(
            a.strategy_name,
            f"{ret_all:+.1f}%",
            f"{ret_clob:+.1f}%",
            f"[{style}]{delta:+.1f}%[/{style}]",
            str(n_all),
            str(n_clob),
            f"{bias_pct:+.1f}%",
        )

    return table
