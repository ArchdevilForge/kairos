"""Smart money detection via holder checking."""
import json
from pathlib import Path
from typing import Optional

import aiohttp
import structlog

from src.models import Token, SmartWalletSignal

logger = structlog.get_logger()


class SmartMoneyChecker:
    """Check if smart wallets hold a token."""

    def __init__(self, wallets_path: Optional[Path] = None):
        if wallets_path is None:
            wallets_path = Path(__file__).parent.parent.parent / "data" / "smart_wallets.json"

        self._wallets: dict[str, list[dict]] = {}
        self._wallet_sets: dict[str, set[str]] = {}  # chain -> set of addresses (lowercase)
        self._session: Optional[aiohttp.ClientSession] = None

        self._load_wallets(wallets_path)

    def _load_wallets(self, path: Path):
        """Load smart wallet addresses from JSON."""
        if not path.exists():
            logger.warning("smart_wallets_not_found", path=str(path))
            return

        try:
            with open(path) as f:
                data = json.load(f)

            for chain, wallets in data.items():
                if chain.startswith("_"):  # skip metadata
                    continue
                self._wallets[chain] = wallets
                # Build lookup set (lowercase for case-insensitive matching)
                self._wallet_sets[chain] = {
                    w["address"].lower() for w in wallets
                }

            total = sum(len(w) for w in self._wallets.values())
            logger.info("smart_wallets_loaded", total=total, chains=list(self._wallets.keys()))

        except Exception as e:
            logger.error("load_wallets_error", error=str(e))

    @property
    async def session(self) -> aiohttp.ClientSession:
        if self._session is None or self._session.closed:
            self._session = aiohttp.ClientSession()
        return self._session

    def get_wallet_label(self, chain: str, address: str) -> Optional[str]:
        """Get label for a wallet address."""
        wallets = self._wallets.get(chain, [])
        addr_lower = address.lower()
        for w in wallets:
            if w["address"].lower() == addr_lower:
                return w.get("label")
        return None

    async def check_token(self, token: Token) -> Token:
        """Check if any smart wallets hold the token, update token in-place."""
        chain = token.chain

        if chain not in self._wallet_sets:
            return token

        try:
            holders = await self._fetch_holders(chain, token.address)

            if not holders:
                return token

            smart_wallets = self._wallet_sets[chain]
            matches = []

            for holder in holders:
                holder_addr = holder.get("address", "").lower()
                if holder_addr in smart_wallets:
                    label = self.get_wallet_label(chain, holder_addr)
                    signal = SmartWalletSignal(
                        address=holder_addr,
                        label=label,
                        action="hold",
                        amount_usd=holder.get("amount_usd"),
                    )
                    matches.append(signal)

            if matches:
                token.smart_money_signals = matches
                token.smart_money_count = len(matches)
                logger.info(
                    "smart_money_detected",
                    token=token.symbol,
                    count=len(matches),
                    wallets=[s.label or s.address[:8] for s in matches]
                )

        except Exception as e:
            logger.warning("check_token_error", token=token.symbol, error=str(e))

        return token

    async def _fetch_holders(self, chain: str, token_address: str) -> list[dict]:
        """Fetch token holders from chain-specific API."""
        if chain == "sol":
            return await self._fetch_solana_holders(token_address)
        elif chain in ("base", "bsc", "eth"):
            return await self._fetch_evm_holders(chain, token_address)
        return []

    async def _fetch_solana_holders(self, token_address: str) -> list[dict]:
        """Fetch Solana token holders via public API."""
        # Using Solscan public API
        url = f"https://api.solscan.io/token/holders?token={token_address}&limit=100"

        try:
            sess = await self.session
            async with sess.get(url, timeout=10, headers={
                "User-Agent": "Mozilla/5.0",
                "Accept": "application/json",
            }) as resp:
                if resp.status != 200:
                    logger.debug("solscan_api_error", status=resp.status)
                    return []

                data = await resp.json()
                holders = data.get("data", [])

                return [
                    {
                        "address": h.get("owner", ""),
                        "amount": h.get("amount", 0),
                        "amount_usd": h.get("amountUsd"),
                    }
                    for h in holders
                ]
        except Exception as e:
            logger.debug("fetch_solana_holders_error", error=str(e))
            return []

    async def _fetch_evm_holders(self, chain: str, token_address: str) -> list[dict]:
        """Fetch EVM chain token holders."""
        # For Base/BSC, use respective explorer APIs
        # These typically require API keys for production use

        api_map = {
            "base": "https://api.basescan.org/api",
            "bsc": "https://api.bscscan.com/api",
            "eth": "https://api.etherscan.io/api",
        }

        base_url = api_map.get(chain)
        if not base_url:
            return []

        # Note: Token holder list requires paid API on most explorers
        # This is a placeholder - in production, use proper API key
        # Or use alternative services like Moralis, Alchemy, etc.

        logger.debug("evm_holders_not_implemented", chain=chain)
        return []

    async def close(self):
        """Cleanup session."""
        if self._session and not self._session.closed:
            await self._session.close()
