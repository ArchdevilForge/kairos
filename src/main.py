"""Main entry point and scheduler."""
import asyncio
import signal
import sys
from pathlib import Path

import structlog
from dotenv import load_dotenv

from src.cli import get_config_path, get_env_path, CONFIG_FILE

# Load .env from appropriate location
env_path = get_env_path()
if env_path.exists():
    load_dotenv(env_path)

from src.config import load_config, Config
from src.sources import DexscreenerSource, DataSource, SmartMoneyChecker
from src.filters import TokenFilter
from src.notifiers import TelegramNotifier
from src.models import Token

structlog.configure(
    processors=[
        structlog.processors.TimeStamper(fmt="iso"),
        structlog.processors.add_log_level,
        structlog.dev.ConsoleRenderer(),
    ]
)

logger = structlog.get_logger()


class MemeMonitor:
    """Main monitoring orchestrator."""

    def __init__(self, config: Config):
        self.config = config
        self.filter = TokenFilter(config.filters)
        self.notifier = TelegramNotifier(config.telegram)
        self.sources: list[DataSource] = []
        self.smart_money_checker = SmartMoneyChecker()
        self._running = False

        if config.sources.dexscreener_enabled:
            self.sources.append(DexscreenerSource())

    async def start(self):
        """Start the monitoring loop."""
        self._running = True
        logger.info("monitor_starting", chains=self.config.chains)

        await self.notifier.send_startup_message(self.config.chains)

        while self._running:
            try:
                await self._poll_cycle()
            except Exception as e:
                logger.error("poll_cycle_error", error=str(e))

            await asyncio.sleep(self.config.polling.interval_seconds)

    async def stop(self):
        """Stop the monitoring loop."""
        self._running = False
        logger.info("monitor_stopping")

        for source in self.sources:
            await source.close()
        await self.smart_money_checker.close()
        await self.notifier.close()

    async def _poll_cycle(self):
        """Single polling cycle across all sources and chains."""
        all_tokens: dict[str, Token] = {}  # address -> token for dedup

        # Fetch from all sources in parallel
        tasks = []
        for source in self.sources:
            for chain in self.config.chains:
                tasks.append(self._fetch_from_source(source, chain))

        results = await asyncio.gather(*tasks, return_exceptions=True)

        for result in results:
            if isinstance(result, Exception):
                logger.warning("fetch_error", error=str(result))
                continue
            for token in result:
                key = f"{token.chain}:{token.address.lower()}"
                if key not in all_tokens:
                    all_tokens[key] = token

        # Enrich with smart money data
        sm_tasks = [
            self.smart_money_checker.check_token(t)
            for t in all_tokens.values()
        ]
        await asyncio.gather(*sm_tasks, return_exceptions=True)

        # Filter and notify
        valid_tokens = []
        for token in all_tokens.values():
            if self.filter.is_valid(token):
                valid_tokens.append(token)

        logger.info(
            "poll_complete",
            total=len(all_tokens),
            valid=len(valid_tokens)
        )

        # Send notifications
        for token in valid_tokens:
            await self.notifier.send_token_alert(token)
            await asyncio.sleep(0.5)  # Rate limit

    async def _fetch_from_source(
        self, source: DataSource, chain: str
    ) -> list[Token]:
        """Fetch tokens from a single source."""
        tokens = []
        try:
            async for token in source.fetch_new_tokens(chain):
                tokens.append(token)
        except Exception as e:
            logger.warning(
                "source_fetch_error",
                source=source.name,
                chain=chain,
                error=str(e)
            )
        return tokens


async def main():
    """Entry point."""
    config_path = get_config_path()

    if not config_path.exists():
        print("\n❌ 未找到配置文件")
        print(f"   请先运行 'meme-monitor-setup' 进行配置")
        print(f"   或将 config.yaml 放在当前目录\n")
        sys.exit(1)

    config = load_config(config_path)
    monitor = MemeMonitor(config)

    # Handle graceful shutdown
    loop = asyncio.get_event_loop()

    def shutdown_handler():
        logger.info("shutdown_signal_received")
        asyncio.create_task(monitor.stop())

    for sig in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(sig, shutdown_handler)

    try:
        await monitor.start()
    except asyncio.CancelledError:
        pass
    finally:
        await monitor.stop()


def cli():
    """Sync entry point for console script."""
    asyncio.run(main())


if __name__ == "__main__":
    cli()
