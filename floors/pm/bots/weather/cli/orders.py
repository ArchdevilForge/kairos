from __future__ import annotations

import structlog
from rich.console import Console
from rich.table import Table

from pm_bot.core.clob import ClobTrader
from pm_bot.core.config_loader import load_config

console = Console()
log = structlog.get_logger()


async def run_orders(debug: bool = False) -> None:
    _setup_logging(debug)

    config = load_config()
    trader = ClobTrader(config)

    if not trader.is_configured():
        console.print("[red]CLOB credentials not configured. Set config.toml [clob] and POLY_PK env var.[/red]")
        return

    try:
        open_orders = trader.get_open_orders()
    except Exception as e:
        log.error("orders_fetch_failed", error=str(e))
        console.print(f"[red]Failed to fetch orders: {e}[/red]")
        return

    if not open_orders:
        console.print("[dim]No open orders.[/dim]")
        return

    table = Table(title="Open Orders", show_lines=False, padding=(0, 1))
    table.add_column("Order ID", style="dim", width=16)
    table.add_column("Market", style="white", width=20)
    table.add_column("Side", style="bold", width=4)
    table.add_column("Price", style="white", width=6, justify="right")
    table.add_column("Size", style="white", width=8, justify="right")
    table.add_column("Filled", style="green", width=8, justify="right")
    table.add_column("Status", style="yellow", width=10)

    for order in open_orders:
        oid = str(order.get("id", ""))[:16]
        asset = str(order.get("asset_id", order.get("market", "")))[:20]
        side = str(order.get("side", ""))
        price = str(order.get("price", ""))
        size = str(order.get("original_size", ""))
        filled = str(order.get("size_matched", "0"))
        status = str(order.get("status", ""))

        side_style = "bold green" if side == "BUY" else "bold red"
        table.add_row(
            oid,
            asset,
            f"[{side_style}]{side}[/]",
            price,
            size,
            filled,
            status,
        )

    console.print(table)

    try:
        trades = trader.get_trades()
        if trades:
            trade_table = Table(title="Recent Trades", show_lines=False, padding=(0, 1))
            trade_table.add_column("Trade ID", style="dim", width=16)
            trade_table.add_column("Market", style="white", width=20)
            trade_table.add_column("Side", style="bold", width=4)
            trade_table.add_column("Price", style="white", width=6, justify="right")
            trade_table.add_column("Size", style="white", width=8, justify="right")
            trade_table.add_column("Status", style="yellow", width=10)

            for t in trades[:20]:
                tid = str(t.get("id", ""))[:16]
                market = str(t.get("market", ""))[:20]
                side = str(t.get("side", ""))
                price = str(t.get("price", ""))
                size = str(t.get("size", ""))
                status = str(t.get("status", ""))

                side_style = "bold green" if side == "BUY" else "bold red"
                trade_table.add_row(
                    tid,
                    market,
                    f"[{side_style}]{side}[/]",
                    price,
                    size,
                    status,
                )

            console.print(trade_table)
    except Exception as e:
        log.warning("trades_fetch_failed", error=str(e))

    console.print(f"\n[dim]Daily spent: ${trader.daily_spent:.2f}[/dim]")


def _setup_logging(debug: bool) -> None:
    import logging

    if debug:
        structlog.configure(wrapper_class=structlog.make_filtering_bound_logger(logging.DEBUG))
    else:
        structlog.configure(wrapper_class=structlog.make_filtering_bound_logger(logging.WARNING))
