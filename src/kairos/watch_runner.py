"""Realtime hard-data alert runner."""

from __future__ import annotations

import argparse
import asyncio
import logging
import sys

from kairos.config import load_config
from kairos.data.data_manager import DataManager

logger = logging.getLogger(__name__)


async def run_forever() -> None:
    """Start realtime market watchers until interrupted."""
    try:
        config = load_config()
    except Exception:
        logger.warning("Config load failed; using defaults", exc_info=True)
        config = {}

    manager = DataManager(config)
    await manager.start()
    try:
        await asyncio.Event().wait()
    finally:
        await manager.stop()


def build_parser() -> argparse.ArgumentParser:
    return argparse.ArgumentParser(description="Run realtime no-LLM Kairos Telegram alerts.")


def main(argv: list[str] | None = None) -> int:
    build_parser().parse_args(argv)
    print("Starting Kairos realtime Telegram alerts...", file=sys.stderr)
    try:
        asyncio.run(run_forever())
    except KeyboardInterrupt:
        print("\nShutting down...", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
