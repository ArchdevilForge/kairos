from __future__ import annotations

import csv
from pathlib import Path

from rich.table import Table

from pm_bot.backtest.engine import BacktestResult


def render_table(results: list[BacktestResult]) -> Table:
    table = Table(title="Backtest Results", show_lines=True)
    table.add_column("Strategy", style="bold cyan")
    table.add_column("P&L", justify="right")
    table.add_column("Return%", justify="right")
    table.add_column("Sharpe", justify="right")
    table.add_column("Sortino", justify="right")
    table.add_column("MaxDD", justify="right", style="red")
    table.add_column("Win%", justify="right")
    table.add_column("Trades", justify="right")

    sorted_results = sorted(results, key=lambda r: r.total_pnl, reverse=True)

    for r in sorted_results:
        ret_pct = (r.final_value / r.bankroll - 1.0) * 100 if r.bankroll > 0 else 0.0
        pnl_str = f"${r.total_pnl:+.2f}"
        pnl_style = "green" if r.total_pnl >= 0 else "red"
        table.add_row(
            r.strategy_name,
            f"[{pnl_style}]{pnl_str}[/{pnl_style}]",
            f"{ret_pct:+.1f}%",
            f"{r.sharpe_ratio:.2f}",
            f"{r.sortino_ratio:.2f}",
            f"{r.max_drawdown:.1%}",
            f"{r.win_rate:.1%}",
            str(len(r.trades)),
        )

    return table


def render_comparison_table(results: list[BacktestResult]) -> Table:
    table = Table(title="Strategy Comparison", show_lines=True)
    table.add_column("Metric", style="bold")
    for r in results:
        table.add_column(r.strategy_name, justify="right")

    metrics_rows = [
        ("P&L", lambda r: f"${r.total_pnl:+.2f}"),
        ("Return%", lambda r: f"{(r.final_value / r.bankroll - 1.0) * 100:+.1f}%" if r.bankroll > 0 else "N/A"),
        ("Sharpe", lambda r: f"{r.sharpe_ratio:.2f}"),
        ("Sortino", lambda r: f"{r.sortino_ratio:.2f}"),
        ("MaxDD", lambda r: f"{r.max_drawdown:.1%}"),
        ("Win%", lambda r: f"{r.win_rate:.1%}"),
        ("Avg Win", lambda r: f"${r.avg_win:.2f}"),
        ("Avg Loss", lambda r: f"${r.avg_loss:.2f}"),
        ("Brier", lambda r: f"{r.brier_score:.4f}"),
        ("Trades", lambda r: str(len(r.trades))),
    ]

    for label, fmt in metrics_rows:
        row = [label] + [fmt(r) for r in results]
        table.add_row(*row)

    return table


def export_csv(results: list[BacktestResult], path: str | Path) -> None:
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)

    with open(path, "w", newline="") as f:
        writer = csv.writer(f)
        writer.writerow(["strategy", "date", "action", "bucket", "price", "size", "cost", "pnl", "bankroll"])
        for r in results:
            running = r.bankroll
            for t in r.trades:
                if t.resolved:
                    running += t.pnl
                writer.writerow(
                    [
                        t.strategy,
                        t.date,
                        t.direction,
                        t.bucket_key,
                        f"{t.price:.4f}",
                        f"{t.size_usd:.2f}",
                        f"{t.cost:.4f}",
                        f"{t.pnl:.2f}",
                        f"{running:.2f}",
                    ]
                )
