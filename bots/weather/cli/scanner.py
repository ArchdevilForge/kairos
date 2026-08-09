"""CLI commands for market scanning and EMOS training."""

from __future__ import annotations

import asyncio

import typer
from rich.console import Console
from rich.table import Table

app = typer.Typer(help="Weather market scanner and EMOS training")
console = Console()

@app.command()
def scan(
    mode: str = typer.Option("all", help="Scanning mode: arb, all"),
    min_edge: float = typer.Option(0.08, help="Minimum edge threshold"),
    cities: str = typer.Option(None, help="Comma-separated cities"),
    max_cities: int = typer.Option(15, help="Max cities to scan"),
):
    """Scan weather markets for trading opportunities."""
    from pm_bot.scripts.scan_markets import scan_and_report

    city_list = cities.split(",") if cities else None
    asyncio.run(scan_and_report(mode=mode, min_edge=min_edge, cities=city_list))

@app.command()
def train_emos(
    city: str = typer.Option(None, help="City to train"),
    all_cities: bool = typer.Option(False, help="Train all cities"),
    days: int = typer.Option(90, help="Days of history"),
):
    """Train EMOS calibration coefficients."""
    from pm_bot.scripts.train_emos import train_city, train_all_cities

    if all_cities:
        calibrators = asyncio.run(train_all_cities(days=days))
        console.print(f"Trained calibrators for {len(calibrators)} cities")

        # Show summary table
        table = Table(title="EMOS Training Results")
        table.add_column("City")
        table.add_column("a")
        table.add_column("b")
        table.add_column("c")
        table.add_column("d")
        table.add_column("Samples")

        for city_name, cal in calibrators.items():
            table.add_row(
                city_name,
                f"{cal.coeffs['a']:.3f}",
                f"{cal.coeffs['b']:.3f}",
                f"{cal.coeffs['c']:.3f}",
                f"{cal.coeffs['d']:.3f}",
                str(len(cal.coeffs)),
            )

        console.print(table)
    elif city:
        calibrator = asyncio.run(train_city(city, days=days))
        console.print(f"Trained calibrator for {city}:")
        console.print(f"  Coefficients: {calibrator.coeffs}")
    else:
        console.print("Please specify --city or --all-cities")

if __name__ == "__main__":
    app()
