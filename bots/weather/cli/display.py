from __future__ import annotations

from rich.console import Console
from rich.table import Table
from rich.panel import Panel
from rich.text import Text

from pm_bot.models.market import Recommendation, WeatherEvent


def render_recommendations(recs: list[Recommendation]) -> None:
    console = Console()

    if not recs:
        console.print("[dim]No edges found above threshold.[/dim]")
        return

    table = Table(title="PM-Bot Recommendations", show_lines=False, padding=(0, 1))
    table.add_column("Strategy", style="cyan", width=8)
    table.add_column("City", style="white", width=12)
    table.add_column("Temp", style="white", width=10)
    table.add_column("Dir", style="bold", width=3)
    table.add_column("Price", style="white", width=6, justify="right")
    table.add_column("Edge", style="green", width=6, justify="right")
    table.add_column("Reason", style="dim", min_width=20)

    for r in sorted(recs, key=lambda x: x.edge, reverse=True):
        edge_style = "green" if r.edge > 0.1 else "yellow" if r.edge > 0.05 else "dim"
        dir_style = "bold green" if r.direction == "YES" else "bold red"
        table.add_row(
            r.strategy,
            r.city,
            r.temp_label,
            Text(r.direction, style=dir_style),
            f"{r.price:.2f}",
            Text(f"{r.edge:.1%}", style=edge_style),
            r.reasoning[:60],
        )

    console.print(table)
    console.print(f"[dim]{len(recs)} recommendations found[/dim]")


def render_verbose(recs: list[Recommendation]) -> None:
    console = Console()

    if not recs:
        console.print("[dim]No edges found above threshold.[/dim]")
        return

    for r in sorted(recs, key=lambda x: x.edge, reverse=True):
        title = f"[cyan]{r.strategy}[/] | {r.city} | {r.temp_label} | {r.direction}"
        body = (
            f"  Price:     {r.price:.2f}\n"
            f"  Edge:      {r.edge:.1%}\n"
            f"  Market ID: {r.bucket.market_id}\n"
            f"  Temp:      {r.bucket.temp_low_c:.0f}-{r.bucket.temp_high_c:.0f}°C\n"
            f"  Volume:    {r.bucket.volume:,.0f}\n"
            f"  Reason:    {r.reasoning}"
        )
        console.print(Panel(body, title=title, border_style="blue"))


def render_events(events: list[WeatherEvent]) -> None:
    console = Console()

    if not events:
        console.print("[dim]No weather markets currently available.[/dim]")
        return

    table = Table(title="Weather Markets", show_lines=False, padding=(0, 1))
    table.add_column("City", style="white", width=12)
    table.add_column("Date", style="dim", width=16)
    table.add_column("Buckets", style="cyan", width=7, justify="right")
    table.add_column("Sum(YES)", style="yellow", width=8, justify="right")
    table.add_column("Airport", style="dim", width=7)
    table.add_column("Event ID", style="dim", min_width=12)

    for ev in events:
        sum_yes = f"{ev.sum_yes:.3f}"
        gap_style = "red" if abs(ev.sum_gap) > 0.02 else "green"
        table.add_row(
            ev.city,
            ev.date,
            str(len(ev.buckets)),
            Text(sum_yes, style=gap_style),
            ev.airport_code or "—",
            ev.event_id[:12],
        )

    console.print(table)
