"""Token data models."""
from datetime import datetime
from typing import Optional
from pydantic import BaseModel, Field


class SmartWalletSignal(BaseModel):
    address: str
    label: Optional[str] = None
    action: str = "buy"  # buy/sell
    amount_usd: Optional[float] = None
    price: Optional[float] = None
    timestamp: Optional[datetime] = None


class Token(BaseModel):
    chain: str
    address: str
    name: str
    symbol: str

    market_cap: Optional[float] = None
    liquidity: Optional[float] = None
    price: Optional[float] = None

    created_at: Optional[datetime] = None
    age_minutes: Optional[int] = None

    lp_burned: bool = False
    is_honeypot: bool = False

    # Holder info
    holder_count: Optional[int] = None
    top_holder_rate: Optional[float] = None  # top 10 holder percentage

    # Smart money
    smart_money_signals: list[SmartWalletSignal] = Field(default_factory=list)
    smart_money_count: int = 0
    smart_degen_count: int = 0  # from GMGN
    avg_buy_price: Optional[float] = None

    # Security flags
    renounced_mint: bool = False
    renounced_freeze: bool = False

    source: str = ""  # gmgn/dexscreener

    @property
    def gmgn_url(self) -> str:
        return f"https://gmgn.ai/{self.chain}/token/{self.address}"

    @property
    def dexscreener_url(self) -> str:
        chain_map = {"sol": "solana", "base": "base", "bsc": "bsc"}
        return f"https://dexscreener.com/{chain_map.get(self.chain, self.chain)}/{self.address}"

    @property
    def short_address(self) -> str:
        if len(self.address) > 12:
            return f"{self.address[:6]}...{self.address[-4:]}"
        return self.address

    def __hash__(self):
        return hash((self.chain, self.address.lower()))

    def __eq__(self, other):
        if not isinstance(other, Token):
            return False
        return self.chain == other.chain and self.address.lower() == other.address.lower()
