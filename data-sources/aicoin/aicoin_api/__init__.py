"""AiCoin API toolkit.

- public: www.aicoin.com chart endpoints (no auth)
- pc_client: desktop apipc encrypted protocol (+ login)
- open_client: open.aicoin.com HMAC open-data API
"""

from .public import *  # noqa: F401,F403
from . import pc_client, open_client

__all__ = [n for n in globals() if not n.startswith("_")]
