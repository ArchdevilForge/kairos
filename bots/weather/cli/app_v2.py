#!/usr/bin/env python3
"""PM-Bot: Polymarket Weather Trading Bot - Main Entry Point.

Usage:
    pm-bot scan              # Scan markets for opportunities
    pm-bot train             # Train EMOS calibrators
    pm-bot paper             # Paper trading mode
    pm-bot live              # Live trading mode
    pm-bot backtest          # Run backtest
    pm-bot daemon            # Run as daemon
    pm-bot status            # Show bot status
    pm-bot config            # Show configuration
"""

from __future__ import annotations

import asyncio
import sys
from pathlib import Path

import typer
from rich.console import Console
from rich.table import Table

app = typer.Typer(
    name="pm-bot",
    help="Polymarket Weather Trading Bot",
    add_completion=False,
)
console = Console()


@app.command()
def scan(
    cities: str = typer.Option(None, help="Comma-separated cities (default: auto-select)"),
    mode: str = typer.Option("all", help="Mode: tail, arb, all"),
    min_edge: float = typer.Option(0.08, help="Minimum edge threshold"),
    max_cities: int = typer.Option(15, help="Max cities to scan"),
    verbose: bool = typer.Option(False, "--verbose", "-v", help="Verbose output"),
):
    """Scan weather markets for trading opportunities."""
    from pm_bot.scripts.scan_markets import MarketScanner

    async def _scan():
        scanner = MarketScanner()
        city_list = cities.split(",") if cities else None

        with console.status("[bold green]Scanning markets..."):
            result = await scanner.scan(
                mode=mode,
                min_edge=min_edge,
                cities=city_list,
                max_cities=max_cities,
            )

        console.print(result.summary())

        if verbose and result.opportunities:
            console.print("\n[bold]Detailed Opportunities:[/bold]")
            for opp in sorted(result.opportunities, key=lambda x: x.edge, reverse=True):
                console.print(
                    f"  [{opp.strategy.upper()}] {opp.city}: "
                    f"{opp.market_question[:50]}..."
                )
                console.print(
                    f"    YES@{opp.yes_price:.2f}, model={opp.model_prob:.1%}, "
                    f"edge={opp.edge:.1%}"
                )

    asyncio.run(_scan())


@app.command()
def train(
    city: str = typer.Option(None, help="City to train"),
    all_cities: bool = typer.Option(False, "--all", help="Train all cities"),
    days: int = typer.Option(90, help="Days of historical data"),
    output: str = typer.Option(None, help="Output directory"),
):
    """Train EMOS calibration coefficients."""
    from pm_bot.scripts.train_emos import train_city, train_all_cities

    async def _train():
        if all_cities:
            console.print(f"[bold]Training EMOS for all cities ({days} days)...[/bold]")
            calibrators = await train_all_cities(days=days)

            table = Table(title="EMOS Training Results")
            table.add_column("City")
            table.add_column("a")
            table.add_column("b")
            table.add_column("c")
            table.add_column("d")
            table.add_column("Status")

            for city_name, cal in calibrators.items():
                status = "[green]✓[/green]" if cal._trained else "[red]✗[/red]"
                table.add_row(
                    city_name,
                    f"{cal.coeffs['a']:.3f}",
                    f"{cal.coeffs['b']:.3f}",
                    f"{cal.coeffs['c']:.3f}",
                    f"{cal.coeffs['d']:.3f}",
                    status,
                )

            console.print(table)
            console.print(f"\nTrained {len(calibrators)} calibrators")

        elif city:
            console.print(f"[bold]Training EMOS for {city} ({days} days)...[/bold]")
            with console.status("Collecting data and training..."):
                calibrator = await train_city(city, days=days)

            console.print(f"[green]✓[/green] Trained calibrator for {city}")
            console.print(f"  Coefficients: {calibrator.coeffs}")

        else:
            console.print("[red]Please specify --city or --all[/red]")
            raise typer.Exit(1)

    asyncio.run(_train())


@app.command()
def paper(
    cities: str = typer.Option(None, help="Comma-separated cities"),
    bankroll: float = typer.Option(100.0, help="Starting bankroll"),
    min_edge: float = typer.Option(0.08, help="Minimum edge"),
    interval: int = typer.Option(30, help="Scan interval (minutes)"),
):
    """Run in paper trading mode."""
    from pm_bot.scripts.trade_bot import WeatherTradingBot

    async def _paper():
        city_list = cities.split(",") if cities else None
        bot = WeatherTradingBot(
            mode="paper",
            bankroll=bankroll,
            min_edge=min_edge,
            cities=city_list,
        )

        console.print("[bold yellow]Starting Paper Trading Mode[/bold yellow]")
        console.print(f"  Bankroll: ${bankroll:.2f}")
        console.print(f"  Min edge: {min_edge:.1%}")
        console.print(f"  Interval: {interval} min")
        console.print(f"  Cities: {city_list or 'auto-select'}")
        console.print()

        await bot.run_paper_trading()

    asyncio.run(_paper())


@app.command()
def live(
    cities: str = typer.Option(None, help="Comma-separated cities"),
    bankroll: float = typer.Option(100.0, help="Starting bankroll"),
    min_edge: float = typer.Option(0.10, help="Minimum edge (higher for live)"),
    dry_run: bool = typer.Option(True, "--dry-run/--no-dry-run", help="Dry run mode"),
):
    """Run in live trading mode (use with caution!)."""
    if not dry_run:
        console.print("[bold red]⚠️  LIVE TRADING MODE[/bold red]")
        console.print("This will execute real trades with real money!")
        confirm = typer.confirm("Are you sure?")
        if not confirm:
            raise typer.Abort()

    from pm_bot.scripts.trade_bot import WeatherTradingBot

    city_list = cities.split(",") if cities else None
    bot = WeatherTradingBot(
        mode="live",
        bankroll=bankroll,
        min_edge=min_edge,
        cities=city_list,
    )

    console.print("[bold red]Live trading not fully implemented yet[/bold red]")
    console.print("Use 'pm-bot daemon' for live trading with proper risk management")


@app.command()
def daemon(
    action: str = typer.Argument(..., help="start, stop, status"),
    cities: str = typer.Option(None, help="Comma-separated cities"),
    dry_run: bool = typer.Option(True, "--dry-run/--no-dry-run", help="Dry run mode"),
):
    """Run as a daemon process."""
    import subprocess

    if action == "status":
        result = subprocess.run(["pm-bot", "daemon", "status"], capture_output=True, text=True)
        console.print(result.stdout)
        return

    if action == "stop":
        result = subprocess.run(["pm-bot", "daemon", "stop"], capture_output=True, text=True)
        console.print(result.stdout)
        return

    if action == "start":
        args = ["pm-bot", "daemon", "start"]
        if dry_run:
            args.append("--dry-run")
        if cities:
            args.extend(["--cities", cities])

        console.print(f"[bold]Starting daemon: {' '.join(args)}[/bold]")
        result = subprocess.run(args, capture_output=False)
        return

    console.print(f"[red]Unknown action: {action}[/red]")
    console.print("Usage: pm-bot daemon [start|stop|status]")


@app.command()
def backtest(
    strategy: str = typer.Option("all", help="Strategy to backtest"),
    days: int = typer.Option(30, help="Days to backtest"),
    bankroll: float = typer.Option(100.0, help="Starting bankroll"),
    cities: str = typer.Option(None, help="Comma-separated cities"),
    real: bool = typer.Option(False, "--real", help="Use real historical data"),
    emos: bool = typer.Option(False, "--emos", help="Use EMOS calibration"),
):
    """Run backtest on strategies."""
    from pm_bot.backtest.engine import BacktestEngine
    from pm_bot.strategies.base import ALL_STRATEGIES

    if not ALL_STRATEGIES:
        console.print("[red]No strategies available. All strategies deleted 2026-05-16.[/red]")
        console.print("See: .trellis/spec/backend/trading-config.md for strategy design history.")
        raise typer.Exit(1)

    async def _backtest():
        city_list = cities.split(",") if cities else ["Chicago"]

        # Select strategies
        if strategy == "all":
            strats = list(ALL_STRATEGIES.values())
        elif strategy in ALL_STRATEGIES:
            strats = [ALL_STRATEGIES[strategy]]
        else:
            console.print(f"[red]Unknown strategy: {strategy}[/red]")
            console.print(f"Available: {', '.join(ALL_STRATEGIES.keys())}")
            raise typer.Exit(1)

        # Train EMOS if requested
        emos_calibrators = {}
        if emos:
            console.print("[bold]Training EMOS calibrators...[/bold]")
            for city in city_list:
                try:
                    cal = await train_city(city, days=90)
                    emos_calibrators[city] = cal
                    console.print(f"  ✓ {city}")
                except Exception as e:
                    console.print(f"  ✗ {city}: {e}")

        console.print(f"\n[bold]Running backtest...[/bold]")
        console.print(f"  Strategy: {strategy}")
        console.print(f"  Days: {days}")
        console.print(f"  Bankroll: ${bankroll:.2f}")
        console.print(f"  Cities: {city_list}")
        console.print(f"  Real data: {real}")
        console.print(f"  EMOS: {emos}")

        engine = BacktestEngine(
            strategies=strats,
            bankroll=bankroll,
            days=days,
            cities=city_list,
            use_synthetic=not real,
        )

        if real:
            results = await engine.run_real()
        else:
            results = await engine.run()

        # Display results
        if results:
            table = Table(title="Backtest Results")
            table.add_column("Strategy")
            table.add_column("P&L")
            table.add_column("Return%")
            table.add_column("Win%")
            table.add_column("Trades")
            table.add_column("Sharpe")

            for r in results:
                table.add_row(
                    r.strategy,
                    f"${r.pnl:.2f}",
                    f"{r.return_pct:.1f}%",
                    f"{r.win_rate:.1f}%",
                    str(r.trade_count),
                    f"{r.sharpe:.2f}",
                )

            console.print(table)
        else:
            console.print("[yellow]No backtest results[/yellow]")

    asyncio.run(_backtest())


@app.command()
def wallet_scan(
    max_age: float = typer.Option(60.0, "--max-age", help="Max trade age in minutes"),
    min_size: float = typer.Option(1.0, "--min-size", help="Min trade size"),
    min_confidence: float = typer.Option(0.5, "--min-confidence", help="Min signal confidence"),
):
    """Scan tracked smart wallets for recent trades."""
    from pm_bot.cli.wallet_cmd import scan_wallets

    asyncio.run(scan_wallets(
        max_age_min=max_age,
        min_trade_size=min_size,
        min_confidence=min_confidence,
    ))


@app.command()
def wallet_list():
    """List tracked smart wallets."""
    from pm_bot.cli.wallet_cmd import show_wallets

    asyncio.run(show_wallets())


@app.command()
def wallet_monitor(
    interval: float = typer.Option(30.0, "--interval", help="Scan interval in seconds"),
    max_age: float = typer.Option(30.0, "--max-age", help="Max trade age in minutes"),
    min_size: float = typer.Option(5.0, "--min-size", help="Min trade size"),
):
    """Continuously monitor smart wallets for new trades."""
    from pm_bot.cli.wallet_cmd import monitor_wallets

    asyncio.run(monitor_wallets(
        interval_sec=interval,
        max_age_min=max_age,
        min_trade_size=min_size,
    ))





@app.command()
def status():
    """Show bot status and configuration."""
    from pm_bot.core.config_loader import load_config

    config = load_config()

    # Show basic info
    console.print("[bold]PM-Bot Status[/bold]")
    console.print()

    # Strategies
    from pm_bot.strategies.base import ALL_STRATEGIES
    console.print("[bold]Available Strategies:[/bold]")
    if ALL_STRATEGIES:
        for name, strat in ALL_STRATEGIES.items():
            console.print(f"  • {name}: {strat.name}")
    else:
        console.print("  None (all strategies deleted 2026-05-16)")

    # EMOS calibrators
    from pathlib import Path
    emos_dir = Path("data/emos")
    if emos_dir.exists():
        calibrators = list(emos_dir.glob("emos_*.json"))
        console.print(f"\n[bold]EMOS Calibrators:[/bold] {len(calibrators)} trained")
        for path in calibrators:
            city = path.stem.replace("emos_", "").replace("_", " ").title()
            console.print(f"  • {city}")
    else:
        console.print("\n[bold]EMOS Calibrators:[/bold] None (run 'pm-bot train --all')")

    # Config
    console.print(f"\n[bold]Configuration:[/bold]")
    console.print(f"  Mode: {config.get('mode', 'paper')}")
    console.print(f"  Bankroll: ${config.get('sizing', {}).get('bankroll', 100):.2f}")
    console.print(f"  Kelly: {config.get('sizing', {}).get('kelly_fraction', 0.25)}")


@app.command()
def config(
    show: bool = typer.Option(True, "--show/--no-show", help="Show config"),
    path: bool = typer.Option(False, "--path", help="Show config path"),
):
    """Show configuration."""
    from pm_bot.core.config_loader import get_config_path

    config_path = get_config_path()

    if path:
        console.print(str(config_path))
        return

    if show and config_path.exists():
        console.print(f"[bold]Config: {config_path}[/bold]")
        console.print()
        console.print(config_path.read_text())


def main():
    """Main entry point."""
    app()


if __name__ == "__main__":
    main()
