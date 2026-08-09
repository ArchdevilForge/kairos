"""Abstract base class for data sources."""
from abc import ABC, abstractmethod
from typing import AsyncIterator

from src.models import Token


class DataSource(ABC):
    """Base class for token data sources."""

    name: str = "base"

    @abstractmethod
    async def fetch_new_tokens(self, chain: str) -> AsyncIterator[Token]:
        """Fetch newly created tokens."""
        yield  # type: ignore

    @abstractmethod
    async def get_token_details(self, chain: str, address: str) -> Token | None:
        """Get detailed info for a specific token."""
        pass

    async def close(self):
        """Cleanup resources."""
        pass
