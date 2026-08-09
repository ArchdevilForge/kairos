"""Complete weather trading bot with EMOS calibration.

This is the main entry point for the trading bot that:
1. Scans all weather markets for opportunities
2. Uses EMOS-calibrated probabilities for edge calculation
3. Executes trades via Polymarket CLOB
4. Manages risk and position sizing

Usage:
    python -m pm_bot.scripts.trade_bot scan          # Scan only
    python -m pm_bot.scripts.trade_bot paper          # Paper trading
    python -m pm_bot.scripts.trade_bot live           # Live trading
"""

from __future__ import annotations

import asyncio
from datetime import datetime
from pathlib import Path

import httpx
import structlog
from rich.console import Console
from rich.table import Table

from pm_bot.core.city_selector import CitySelector
from pm_bot.core.emos import EMOSCalibrator, EMOSCalibrator
from pm_bot.scripts.scan_markets import MarketScanner, ScanResult

log = structlog.get_logger()
console = Console()

# EMOS model storage
EMOS_DIR = Path("data/emos")


class WeatherTradingBot:
    """Complete weather trading bot.

    Usage:
        bot = WeatherTradingBot(mode="paper")
        await bot.run()
    """

    def __init__(
        self,
        mode: str = "scan",  # scan, paper, live
        bankroll: float = 100.0,
        min_edge: float = 0.08,
        max_positions: int = 20,
        cities: list[str] | None = None,
    ):
        self.mode = mode
        self.bankroll = bankroll
        self.min_edge = min_edge
        self.max_positions = max_positions
        self.cities = cities

        # Load EMOS calibrators
        self.emos_calibrators: dict[str, EMOSCalibrator] = {}
        self._load_calibrators()

    def _load_calibrators(self):
        """Load trained EMOS calibrators from disk."""
        if not EMOS_DIR.exists():
            return

        for path in EMOS_DIR.glob("emos_*.json"):
            try:
                calibrator = EMOSCalibrator.load(path)
                if calibrator.city:
                    self.emos_calibrators[calibrator.city] = calibrator
                    log.info("loaded_emos", city=calibrator.city)
            except Exception as e:
                log.warning("emos_load_failed", path=str(path), error=str(e))

    async def scan(self) -> ScanResult:
        """Scan markets for opportunities."""
        scanner = MarketScanner(emos_calibrators=self.emos_calibrators)
        return await scanner.scan(
            mode="all",
            min_edge=self.min_edge,
            cities=self.cities,
        )

    async def run_scan_loop(self, interval_minutes: int = 30):
        """Run continuous scanning loop."""
        console.print(f"[bold green]Starting scan loop (every {interval_minutes} min)[/bold green]")

        while True:
            try:
                result = await self.scan()
                console.clear()
                console.print(result.summary())
                console.print(f"\n[dim]Next scan in {interval_minutes} minutes...[/dim]")
            except Exception as e:
                log.error("scan_failed", error=str(e))

            await asyncio.sleep(interval_minutes * 60)

    async def run_paper_trading(self):
        """Run paper trading mode."""
        from pm_bot.core.paper_trade import PaperTrader

        trader = PaperTrader()
        scanner = MarketScanner(emos_calibrators=self.emos_calibrators)

        console.print("[bold yellow]Starting paper trading mode[/bold yellow]")
        console.print(f"Bankroll: ${self.bankroll:.2f}")
        console.print(f"Min edge: {self.min_edge:.1%}")

        while True:
            try:
                # Scan for opportunities
                result = await scanner.scan(mode="all", min_edge=self.min_edge)

                if result.opportunities:
                    console.print(f"\n[green]Found {len(result.opportunities)} opportunities[/green]")

                    # Execute paper trades
                    for opp in result.opportunities[:self.max_positions]:
                        # Calculate position size (quarter Kelly)
                        win_payout = 1.0 - opp.yes_price
                        loss_amt = opp.yes_price
                        raw_kelly = (opp.model_prob * win_payout - (1 - opp.model_prob) * loss_amt) / win_payout

                        if raw_kelly <= 0:
                            continue

                        kelly_fraction = 0.25
                        position_usd = self.bankroll * raw_kelly * kelly_fraction
                        position_usd = min(position_usd, 2.0)  # Cap at $2
                        position_usd = max(position_usd, 1.0)  # Min $1

                        # Record paper trade
                        trader.open_trade(
                            city=opp.city,
                            event_id=opp.event_id,
                            bucket_low=opp.bucket_low,
                            bucket_high=opp.bucket_high,
                            direction=opp.direction,
                            entry_price=opp.yes_price,
                            size_usd=position_usd,
                            strategy=opp.strategy,
                            edge=opp.edge,
                        )

                        console.print(
                            f"  [{opp.strategy.upper()}] {opp.city}: "
                            f"{opp.market_question[:40]}... "
                            f"YES@{opp.yes_price:.2f} edge={opp.edge:.1%} "
                            f"${position_usd:.2f}"
                        )

                # Wait for next scan
                await asyncio.sleep(30 * 60)  # 30 minutes

            except KeyboardInterrupt:
                console.print("\n[yellow]Stopping paper trading...[/yellow]")
                break
            except Exception as e:
                log.error("paper_trade_error", error=str(e))
                await asyncio.sleep(60)

    def display_opportunities(self, result: ScanResult):
        """Display opportunities in a nice table."""
        if not result.opportunities:
            console.print("[dim]No opportunities found[/dim]")
            return

        table = Table(title="Trading Opportunities")
        table.add_column("City")
        table.add_column("Strategy")
        table.add_column("Market")
        table.add_column("YES Price")
        table.add_column("Model Prob")
        table.add_column("Edge")
        table.add_column("Suggested Size")

        for opp in sorted(result.opportunities, key=lambda x: x.edge, reverse=True)[:20]:
            # Calculate suggested size
            win_payout = 1.0 - opp.yes_price
            loss_amt = opp.yes_price
            raw_kelly = (opp.model_prob * win_payout - (1 - opp.model_prob) * loss_amt) / win_payout

            if raw_kelly <= 0:
                continue

            size = self.bankroll * raw_kelly * 0.25
            size = min(size, 2.0)
            size = max(size, 1.0)

            table.add_row(
                opp.city,
                opp.strategy,
                opp.market_question[:35] + "...",
                f"${opp.yes_price:.2f}",
                f"{opp.model_prob:.1%}",
                f"{opp.edge:.1%}",
                f"${size:.2f}",
            )

        console.print(table)


def main():
    """CLI entry point."""
    import argparse

    parser = argparse.ArgumentParser(description="Weather trading bot")
    parser.add_argument(
        "mode",
        choices=["scan", "paper", "live"],
        default="scan",
        help="Operating mode",
    )
    parser.add_argument("--bankroll", type=float, default=100.0, help="Starting bankroll")
    parser.add_argument("--min-edge", type=float, default=0.08, help="Minimum edge")
    parser.add_argument("--cities", type=str, help="Comma-separated cities")
    args = parser.parse_args()

    cities = args.cities.split(",") if args.cities else None

    bot = WeatherTradingBot(
        mode=args.mode,
        bankroll=args.bankroll,
        min_edge=args.min_edge,
        cities=cities,
    )

    if args.mode == "scan":
        result = asyncio.run(bot.scan())
        bot.display_opportunities(result)
    elif args.mode == "paper":
        asyncio.run(bot.run_paper_trading())
    else:
        console.print("[red]Live trading not implemented yet[/red]")


if __name__ == "__main__":
    main()
