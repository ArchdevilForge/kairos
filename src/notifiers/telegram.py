"""Telegram notification service."""
import asyncio
from typing import Optional

import structlog
from telegram import Bot
from telegram.constants import ParseMode

from src.models import Token
from src.config import TelegramConfig

logger = structlog.get_logger()


class TelegramNotifier:
    """Send notifications via Telegram Bot."""

    def __init__(self, config: TelegramConfig):
        self.config = config
        self._bot: Optional[Bot] = None

    @property
    def bot(self) -> Bot:
        if self._bot is None:
            self._bot = Bot(token=self.config.bot_token)
        return self._bot

    async def send_token_alert(self, token: Token) -> bool:
        """Send a token discovery alert."""
        message = self._format_message(token)

        try:
            await self.bot.send_message(
                chat_id=self.config.chat_id,
                text=message,
                parse_mode=ParseMode.HTML,
                disable_web_page_preview=True,
            )
            logger.info("alert_sent", token=token.symbol, chain=token.chain)
            return True

        except Exception as e:
            logger.error("send_alert_failed", token=token.symbol, error=str(e))
            return False

    def _format_message(self, token: Token) -> str:
        chain_emoji = {"sol": "☀️", "base": "🔵", "bsc": "🟡"}.get(token.chain, "🔗")

        age_str = f"{token.age_minutes} 分钟前" if token.age_minutes else "未知"
        mcap_str = f"${token.market_cap:,.0f}" if token.market_cap else "未知"
        liq_str = f"${token.liquidity:,.0f}" if token.liquidity else "未知"

        # Smart money section
        sm_section = ""
        if token.smart_money_count > 0:
            wallet_labels = [
                s.label or s.address[:8]
                for s in token.smart_money_signals[:5]  # show top 5
            ]
            sm_section = f"""
🧠 <b>聪明钱信号:</b>
• {token.smart_money_count} 个聪明钱持有
• 钱包: {', '.join(wallet_labels)}"""
            if token.avg_buy_price:
                sm_section += f"\n• 平均买入价: ${token.avg_buy_price:.8f}"

        message = f"""🚀 <b>新发现代币</b> | {chain_emoji} {token.chain.upper()}

📛 名称: <b>${token.symbol}</b>
📍 CA: <code>{token.address}</code>
💰 市值: {mcap_str}
💧 流动性: {liq_str}
⏰ 创建: {age_str}
{sm_section}

🔗 <b>快速链接:</b>
• <a href="{token.gmgn_url}">GMGN</a>
• <a href="{token.dexscreener_url}">Dexscreener</a>

⚠️ DYOR - 高风险投资"""

        return message

    async def send_startup_message(self, chains: list[str]) -> bool:
        """Send a startup notification."""
        message = f"""✅ <b>Meme 监控已启动</b>

监控链: {', '.join(c.upper() for c in chains)}
轮询间隔: 30s
"""
        try:
            await self.bot.send_message(
                chat_id=self.config.chat_id,
                text=message,
                parse_mode=ParseMode.HTML,
            )
            return True
        except Exception as e:
            logger.error("send_startup_failed", error=str(e))
            return False

    async def close(self):
        """Cleanup."""
        if self._bot:
            await self._bot.shutdown()
