from __future__ import annotations

import os
import shutil
from pathlib import Path

from rich.console import Console
from rich.table import Table

from pm_bot.models.config import DEFAULT_CITIES, STRATEGY_DEFAULTS, CACHE_TTL
from pm_bot.core.config_loader import load_config, find_config_path

console = Console()


def run_config(init: bool = False) -> None:
    if init:
        _run_config_init()
        return

    console.print("\n[bold]PM-Bot Configuration[/]\n")

    config_path = find_config_path()
    file_config = load_config(config_path)

    table = Table(title="Strategy Defaults", show_lines=False)
    table.add_column("Strategy", style="cyan")
    table.add_column("Parameter", style="white")
    table.add_column("Default", style="dim")
    table.add_column("Config.toml", style="green")

    for strat, params in STRATEGY_DEFAULTS.items():
        config_params = file_config.get("strategies", {}).get(strat, {})
        for k, v in params.items():
            config_val = config_params.get(k, "—")
            override = str(config_val) if config_val != "—" else ""
            style = "green" if override else "dim"
            table.add_row(strat, k, f"{v}", f"[{style}]{override or '—'}[/{style}]")

    console.print(table)

    console.print(f"\n[bold]Default Cities:[/] {', '.join(DEFAULT_CITIES)}")
    console.print("[dim]Override with --cities NYC,HK or --all[/dim]")

    cache_table = Table(title="Cache TTL", show_lines=False)
    cache_table.add_column("Data", style="cyan")
    cache_table.add_column("TTL (seconds)", style="green")

    for name, ttl in CACHE_TTL.items():
        cache_table.add_row(name, str(ttl))

    console.print(cache_table)

    sizing = file_config.get("sizing", {})
    if sizing:
        console.print(
            f"\n[bold]Sizing:[/] max_single=${sizing.get('max_single', 5.0):.2f}, max_daily=${sizing.get('max_daily', 50.0):.2f}"
        )

    clob = file_config.get("clob", {})
    has_creds = bool(clob.get("api_key"))
    console.print(f"\n[bold]CLOB:[/] {'✓ configured' if has_creds else '✗ not configured'}")

    env_pk = "✓ set" if os.environ.get("POLY_PK") else "✗ not set"
    console.print(f"[bold]POLY_PK:[/] {env_pk}")

    notifications = file_config.get("notifications", {})
    has_discord = bool(notifications.get("discord", {}).get("webhook_url"))
    has_telegram = bool(notifications.get("telegram", {}).get("bot_token"))
    console.print(
        f"[bold]Notifications:[/] Discord={'✓' if has_discord else '✗'}, Telegram={'✓' if has_telegram else '✗'}"
    )

    console.print(f"\n[dim]Config file: {config_path}[/dim]")
    console.print("[dim]Set --edge <value> to override all strategy edge thresholds[/dim]")
    console.print("[dim]Set --debug for structured request logging[/dim]")


def _run_config_init() -> None:
    config_path = find_config_path()

    if config_path.exists():
        console.print(f"[yellow]config.toml already exists at {config_path}[/yellow]")
        console.print("[dim]Delete it first if you want to regenerate, or edit it manually.[/dim]")
        return

    example_path = Path(__file__).resolve().parent.parent.parent / "config.toml.example"
    if not example_path.exists():
        console.print("[red]config.toml.example not found in project root[/red]")
        return

    shutil.copy2(example_path, config_path)
    console.print(f"[green]Created config.toml at {config_path}[/green]")
    console.print("[dim]Edit it to add your CLOB credentials and notification settings.[/dim]")
    console.print("[dim]Set POLY_PK env var for your wallet private key.[/dim]")
