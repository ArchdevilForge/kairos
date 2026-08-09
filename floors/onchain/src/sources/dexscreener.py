"""Dexscreener API adapter."""
import asyncio
from datetime import datetime, timezone
from typing import AsyncIterator, Optional

import aiohttp
import structlog

from src.models import Token
from .base import DataSource

logger = structlog.get_logger()


class DexscreenerSource(DataSource):
    """Dexscreener data source."""

    name = "dexscreener"
    BASE_URL = "https://api.dexscreener.com"

    CHAIN_MAP = {
        "sol": "solana",
        "base": "base",
        "bsc": "bsc",
        "eth": "ethereum",
    }

    def __init__(self):
        self._session: Optional[aiohttp.ClientSession] = None

    @property
    async def session(self) -> aiohttp.ClientSession:
        if self._session is None or self._session.closed:
            self._session = aiohttp.ClientSession(
                headers={
                    "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
                    "Accept": "application/json",
                }
            )
        return self._session

    async def fetch_new_tokens(self, chain: str) -> AsyncIterator[Token]:
        """Fetch latest token profiles from Dexscreener."""
        url = f"{self.BASE_URL}/token-profiles/latest/v1"

        try:
            sess = await self.session
            async with sess.get(url, timeout=10) as resp:
                if resp.status != 200:
                    logger.warning("dexscreener_fetch_failed", status=resp.status)
                    return

                data = await resp.json()

                chain_id = self.CHAIN_MAP.get(chain, chain)
                for item in data:
                    if item.get("chainId") != chain_id:
                        continue

                    token = await self._enrich_token(item, chain)
                    if token:
                        yield token

        except asyncio.TimeoutError:
            logger.warning("dexscreener_timeout", chain=chain)
        except Exception as e:
            logger.error("dexscreener_error", chain=chain, error=str(e))

    async def _enrich_token(self, profile: dict, chain: str) -> Optional[Token]:
        """Get full token details from profile."""
        address = profile.get("tokenAddress", "")
        if not address:
            return None

        # Fetch detailed token info
        details = await self.get_token_details(chain, address)
        return details

    async def get_token_details(self, chain: str, address: str) -> Optional[Token]:
        """Get detailed token info from Dexscreener."""
        chain_id = self.CHAIN_MAP.get(chain, chain)
        url = f"{self.BASE_URL}/tokens/v1/{chain_id}/{address}"

        try:
            sess = await self.session
            async with sess.get(url, timeout=10) as resp:
                if resp.status != 200:
                    return None

                data = await resp.json()
                if not data:
                    return None

                # Dexscreener returns array of pairs, take the main one
                pair = data[0] if isinstance(data, list) and data else data

                return self._parse_pair(pair, chain)

        except Exception as e:
            logger.error("get_token_details_error", error=str(e))
            return None

    def _parse_pair(self, pair: dict, chain: str) -> Optional[Token]:
        try:
            base_token = pair.get("baseToken", {})

            # Parse creation time
            created_at = None
            age_minutes = None
            if pair.get("pairCreatedAt"):
                created_ts = pair["pairCreatedAt"] / 1000  # ms to seconds
                created_at = datetime.fromtimestamp(created_ts, tz=timezone.utc)
                age_minutes = int((datetime.now(timezone.utc) - created_at).total_seconds() / 60)

            return Token(
                chain=chain,
                address=base_token.get("address", ""),
                name=base_token.get("name", "Unknown"),
                symbol=base_token.get("symbol", "???"),
                market_cap=pair.get("marketCap"),
                liquidity=pair.get("liquidity", {}).get("usd"),
                price=float(pair.get("priceUsd", 0)) if pair.get("priceUsd") else None,
                created_at=created_at,
                age_minutes=age_minutes,
                source=self.name,
            )
        except Exception as e:
            logger.warning("parse_pair_error", error=str(e))
            return None

    async def get_top_boosted(self) -> list[Token]:
        """Get top boosted tokens across all chains."""
        url = f"{self.BASE_URL}/token-boosts/top/v1"
        tokens = []

        try:
            sess = await self.session
            async with sess.get(url, timeout=10) as resp:
                if resp.status != 200:
                    return tokens

                data = await resp.json()
                for item in data[:20]:  # Top 20
                    chain_id = item.get("chainId", "")
                    # Reverse map chain
                    chain = next(
                        (k for k, v in self.CHAIN_MAP.items() if v == chain_id),
                        chain_id
                    )
                    token = await self.get_token_details(chain, item.get("tokenAddress", ""))
                    if token:
                        tokens.append(token)

        except Exception as e:
            logger.error("get_top_boosted_error", error=str(e))

        return tokens

    async def close(self):
        if self._session and not self._session.closed:
            await self._session.close()
