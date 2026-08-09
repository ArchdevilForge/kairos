# Polymarket CLOB API & py-clob-client-v2 Research

## 1. py-clob-client-v2 Overview

**GitHub Repo**: https://github.com/Polymarket/py-clob-client-v2
**PyPI**: https://pypi.org/project/py-clob-client-v2/
**Latest Version**: 1.0.0 (Apr 17, 2026) / 1.0.1rc1 (May 1, 2026 pre-release)
**Python Requirement**: >=3.9.10
**License**: MIT

### Installation

```bash
pip install py-clob-client-v2
```

### Core Import Structure

```python
from py_clob_client_v2 import (
    ApiCreds,
    ClobClient,
    OrderArgs,          # → actually OrderArgsV2 internally
    MarketOrderArgs,    # → MarketOrderArgsV2 internally
    OrderType,
    PartialCreateOrderOptions,
    Side,
    OpenOrderParams,
    TradeParams,
    PostOrdersV2Args,
    OrderScoringParams,
    OrdersScoringParams,
    OrderPayload,
    OrderMarketCancelParams,
)
from py_clob_client_v2.order_builder.constants import BUY, SELL
```

---

## 2. Authentication Flow

The CLOB uses **two-level authentication**:

### L1 — Wallet Signature (EIP-712)

- Requires: **Private Key** (PK) of your Polygon wallet
- Used for: Creating/deriving API keys, signing order payloads locally
- Mechanism: Signs an EIP-712 typed data structure (`ClobAuthDomain`)

### L2 — HMAC-SHA256 with API Credentials

- Requires: **API Key**, **API Secret**, **API Passphrase**
- Used for: Posting/canceling orders, querying balances, heartbeat
- Mechanism: HMAC-SHA256 signature of request using API secret

### Full Auth Flow

```python
import os
from py_clob_client_v2 import ClobClient, ApiCreds

host = "https://clob.polymarket.com"
chain_id = 137  # Polygon mainnet; 80002 for Amoy testnet

# Step 1: L1 auth — obtain API credentials using wallet private key
client = ClobClient(host=host, chain_id=chain_id, key=os.environ["PK"])
creds = client.create_or_derive_api_key()
# Returns: ApiCreds(api_key=..., api_secret=..., api_passphrase=...)

# Step 2: L2 auth — initialize fully-authenticated client
client = ClobClient(host=host, chain_id=chain_id, key=os.environ["PK"], creds=creds)
```

### Required Environment Variables (.env.example)

```
PK=                          # Wallet private key (0x...)
CLOB_API_KEY=                # From create_or_derive_api_key()
CLOB_SECRET=                 # From create_or_derive_api_key()
CLOB_PASS_PHRASE=            # From create_or_derive_api_key()
CLOB_API_URL=https://clob.polymarket.com
```

### Signature Types

| Type | Value | Description |
|------|-------|-------------|
| EOA | 0 | Standard Ethereum wallet (MetaMask). Funder = EOA address. Needs POL for gas. |
| POLY_PROXY | 1 | Proxy wallet from Magic Link email/Google login. Must export PK from Polymarket.com. |
| GNOSIS_SAFE | 2 | Gnosis Safe multisig proxy (most common for new/returning users). |

For our use case (PK-controlled EOA): `signature_type=0` (default) or `signature_type=2` if using the Polymarket proxy wallet.

---

## 3. Order Creation on neg_risk Markets

Weather markets are **negRisk=true** multi-outcome events. This is critical for order construction.

### How neg_risk Affects Orders

- neg_risk markets use the **Neg Risk CTF Exchange** contract instead of the standard CTF Exchange
- The `neg_risk` flag must be passed in `PartialCreateOrderOptions`
- If omitted, the SDK auto-fetches it via `client.get_neg_risk(token_id)` — but this is an extra API call
- The SDK caches neg_risk per token_id after first lookup

### Placing a Limit Order on a neg_risk Market

```python
from py_clob_client_v2 import OrderArgs, OrderType, PartialCreateOrderOptions
from py_clob_client_v2.order_builder.constants import BUY

response = client.create_and_post_order(
    order_args=OrderArgs(
        token_id="TOKEN_ID_OF_OUTCOME",
        price=0.50,
        size=100,          # number of shares
        side=BUY,
    ),
    options=PartialCreateOrderOptions(
        tick_size="0.01",
        neg_risk=True,     # REQUIRED for weather/multi-outcome markets
    ),
    order_type=OrderType.GTC,
)
```

### Placing a Market Order on a neg_risk Market

```python
from py_clob_client_v2 import MarketOrderArgs, OrderType, PartialCreateOrderOptions
from py_clob_client_v2.order_builder.constants import BUY

response = client.create_and_post_market_order(
    order_args=MarketOrderArgs(
        token_id="TOKEN_ID_OF_OUTCOME",
        side=BUY,
        amount=100,        # USDC amount for BUY, shares for SELL
        price=0.55,        # worst-price limit (slippage protection)
    ),
    options=PartialCreateOrderOptions(
        tick_size="0.01",
        neg_risk=True,
    ),
    order_type=OrderType.FOK,
)
```

### Detecting neg_risk Programmatically

```python
# Method 1: SDK lookup
is_neg_risk = client.get_neg_risk(token_id)  # returns bool

# Method 2: From Gamma API market object
# market["neg_risk"] field on the market/event JSON
```

---

## 4. Order Types Supported

| Type | Behavior | Use Case |
|------|----------|----------|
| **GTC** | Good-Til-Cancelled — rests on book until filled or cancelled | Default for limit orders |
| **GTD** | Good-Til-Date — active until specified expiration time | Auto-expire before known events |
| **FOK** | Fill-Or-Kill — must fill immediately and entirely, or cancel | All-or-nothing market orders |
| **FAK** | Fill-And-Kill — fills what's available, cancels rest | Partial-fill market orders |

### For Our Weather Trading Bot

- **GTC**: Primary order type for placing resting limit orders at desired prices
- **GTD**: Could use for orders that auto-expire before weather event resolution
- **FOK**: For immediate fills when we want all-or-nothing execution
- **FAK**: Less useful — partial fills create open positions

### GTD Expiration Note

GTD has a **60-second security threshold**. To expire in N seconds, set: `expiration = now + 60 + N`

### Post-Only Orders

- Only works with GTC/GTD
- Guarantees maker-side (no immediate matching)
- Rejected if would cross the spread

### Batch Orders

Up to **15 orders** per `post_orders()` call. Useful for placing multiple outcomes simultaneously.

---

## 5. Order Status & Fill Tracking

### Querying Order Status

```python
# Single order by ID
order = client.get_order("0xb816482a...")

# All open orders (with optional filters)
from py_clob_client_v2 import OpenOrderParams
orders = client.get_open_orders()  # all
orders = client.get_open_orders(OpenOrderParams(market="0xbd31dc8a..."))  # by market
orders = client.get_open_orders(OpenOrderParams(asset_id="52114319..."))  # by token
```

### Order Response Fields

| Field | Description |
|-------|-------------|
| `id` | Order ID |
| `status` | `live`, `matched`, `delayed`, `unmatched` |
| `original_size` | Original order size |
| `size_matched` | Amount filled so far |
| `price` | Limit price |
| `side` | BUY / SELL |
| `order_type` | GTC, GTD, FOK, FAK |
| `expiration` | Unix timestamp (0 if none) |
| `associate_trades` | Trade IDs this order participated in |

### Trade Status Lifecycle

| Status | Terminal? | Description |
|--------|-----------|-------------|
| MATCHED | No | Sent to executor for onchain submission |
| MINED | No | Observed on chain, no finality yet |
| CONFIRMED | Yes | **Successful** — strong finality achieved |
| RETRYING | No | Transaction failed, being retried |
| FAILED | Yes | **Permanently failed** |

### Querying Trades

```python
from py_clob_client_v2 import TradeParams

# All trades
trades = client.get_trades()

# Filtered by market
trades = client.get_trades(TradeParams(market="0xbd31dc8a..."))

# Paginated
result = client.get_trades_paginated(TradeParams(market="0xbd31dc8a..."))
# Returns: { trades: [...], next_cursor: "...", limit: N, count: N }
```

### Real-Time Updates via WebSocket (User Channel)

Authenticated WebSocket for real-time order/trade updates. See: `wss://ws-subscriptions-clob.polymarket.com/ws/user`

---

## 6. Rate Limits

All limits enforced via Cloudflare throttling (delayed/queued, not rejected).

### CLOB API (https://clob.polymarket.com)

| Category | Endpoint | Limit |
|----------|----------|-------|
| General | Overall | 9,000 req / 10s |
| Market Data | /book | 1,500 req / 10s |
| Market Data | /price | 1,500 req / 10s |
| Market Data | /midpoint | 1,500 req / 10s |
| Market Data | /prices-history | 1,000 req / 10s |
| Market Data | /books, /prices, /midpoints | 500 req / 10s |
| Market Data | Tick size | 200 req / 10s |
| Ledger | /trades, /orders, /notifications | 900 req / 10s |
| Ledger | /data/orders, /data/trades | 500 req / 10s |
| Auth | API key endpoints | 100 req / 10s |
| **Trading** | **POST /order** | **3,500 burst / 10s, 36,000 / 10 min** |
| **Trading** | **DELETE /order** | **3,000 burst / 10s, 30,000 / 10 min** |
| **Trading** | **POST /orders** (batch) | **1,000 burst / 10s, 15,000 / 10 min** |
| **Trading** | **DELETE /orders** (batch) | **1,000 burst / 10s, 15,000 / 10 min** |
| **Trading** | **DELETE /cancel-all** | **250 / 10s, 6,000 / 10 min** |

### Gamma API (https://gamma-api.polymarket.com)

| Endpoint | Limit |
|----------|-------|
| General | 4,000 req / 10s |
| /events | 500 req / 10s |
| /markets | 300 req / 10s |

### Data API (https://data-api.polymarket.com)

| Endpoint | Limit |
|----------|-------|
| General | 1,000 req / 10s |

### Implications for Our Bot

- 3,500 POST /order per 10 seconds is very generous — won't be a bottleneck
- The 200 req/10s for tick_size lookup means we should cache tick sizes (the SDK already does this)
- Gamma API at 300 req/10s for markets listing — fine for periodic market discovery

---

## 7. Heartbeat Mechanism

**Critical for automated trading**: If no heartbeat within **10 seconds** (5s buffer), **all open orders are cancelled**.

```python
import time

heartbeat_id = ""
while True:
    resp = client.post_heartbeat(heartbeat_id)
    heartbeat_id = resp["heartbeat_id"]
    time.sleep(5)
```

- First heartbeat: use empty string `""`
- Subsequent: use the `heartbeat_id` from previous response
- If expired ID sent, server returns 400 with correct ID — update and retry

**For our bot**: Must implement heartbeat loop in a background thread/async task when any orders are live.

---

## 8. Market Data APIs (No Auth Required)

### Token ID Discovery

```python
# Get market info by condition_id
market_info = client.get_clob_market_info(condition_id)

# Get market by token_id (resolves to parent market)
market = client.get_market_by_token(token_id)

# List all markets (paginated)
markets = client.get_markets()  # keyset pagination
```

### Price & Orderbook Data

```python
# Order book
book = client.get_order_book(token_id)

# Best price
price = client.get_price(token_id, side="BUY")

# Midpoint
mid = client.get_midpoint(token_id)

# Spread
spread = client.get_spread(token_id)

# Last trade price
last_price = client.get_last_trade_price(token_id)

# Price history
from py_clob_client_v2 import PricesHistoryParams
history = client.get_prices_history(PricesHistoryParams(market=condition_id, interval="1d"))
```

---

## 9. Alternative Approaches

### Option A: py-clob-client-v2 SDK (Recommended)

- **Pros**: Handles EIP-712 signing, HMAC auth, order construction, auto-caching
- **Cons**: New SDK (v2 released Apr 2026), relatively few stars (67), may have bugs
- **Best for**: Rapid development, less boilerplate

### Option B: Raw REST API Calls

- **Pros**: No dependency on SDK, full control
- **Cons**: Must implement EIP-712 signing, HMAC-SHA256 L2 headers, order payload construction manually
- **Required headers for L2**: `POLY_ADDRESS`, `POLY_SIGNATURE`, `POLY_TIMESTAMP`, `POLY_API_KEY`, `POLY_PASSPHRASE`
- **Key endpoints**:
  - `POST https://clob.polymarket.com/order` — place order
  - `DELETE https://clob.polymarket.com/order` — cancel order
  - `GET https://clob.polymarket.com/book` — orderbook
  - `GET https://clob.polymarket.com/midpoint` — midpoint price
- **Signing reference**: EIP-712 for L1 (`ClobAuthDomain`), HMAC-SHA256 for L2

### Option C: TypeScript SDK (@polymarket/clob-client-v2)

- Same functionality as Python SDK but in TypeScript
- More mature / more widely used
- Not relevant for our Python-based bot

### Option D: Rust SDK (polymarket-client-sdk-v2)

- Most feature-complete, auto-fetches tick_size/neg_risk
- Overkill for our use case

---

## 10. Key Gotchas & Constraints

1. **neg_risk flag is MANDATORY** for weather markets — omitting it will use the wrong exchange contract and orders will fail
2. **Tick size validation** — price must conform to market's minimum tick size or order is rejected
3. **Balance checks are real-time** — can't place orders exceeding available balance minus reserved amounts from open orders
4. **Allowance prerequisite** — funder must approve Exchange contract to spend pUSD (for BUY) and conditional tokens (for SELL)
5. **Heartbeat is critical** — 10-second timeout cancels ALL open orders
6. **GTD 60-second threshold** — set expiration = now + 60 + desired_lifetime
7. **Sports markets** — limit orders auto-cancelled at game start; marketable orders have 1-second delay
8. **Batch limit** — max 15 orders per `post_orders()` call
9. **Cancel batch limit** — max 3,000 order IDs per `cancel_orders()` call
10. **Order versioning** — SDK handles version mismatch retry automatically

---

## 11. Minimum Viable Integration Code

```python
import os
import time
import threading
from py_clob_client_v2 import (
    ClobClient, ApiCreds, OrderArgs, OrderType,
    PartialCreateOrderOptions, MarketOrderArgs,
)
from py_clob_client_v2.order_builder.constants import BUY, SELL

class PolymarketTrader:
    def __init__(self):
        self.host = "https://clob.polymarket.com"
        self.chain_id = 137

        # Initialize L2 client
        self.client = ClobClient(
            host=self.host,
            chain_id=self.chain_id,
            key=os.environ["PK"],
            creds=ApiCreds(
                api_key=os.environ["CLOB_API_KEY"],
                api_secret=os.environ["CLOB_SECRET"],
                api_passphrase=os.environ["CLOB_PASS_PHRASE"],
            ),
        )

        self._heartbeat_thread = None
        self._heartbeat_id = ""
        self._running = False

    def start_heartbeat(self):
        """Start heartbeat loop in background thread."""
        self._running = True
        self._heartbeat_thread = threading.Thread(target=self._heartbeat_loop, daemon=True)
        self._heartbeat_thread.start()

    def stop_heartbeat(self):
        self._running = False

    def _heartbeat_loop(self):
        while self._running:
            try:
                resp = self.client.post_heartbeat(self._heartbeat_id)
                self._heartbeat_id = resp["heartbeat_id"]
            except Exception as e:
                print(f"Heartbeat error: {e}")
            time.sleep(5)

    def place_limit_buy(self, token_id: str, price: float, size: float,
                        tick_size: str = "0.01", neg_risk: bool = True):
        """Place a GTC limit buy order."""
        return self.client.create_and_post_order(
            order_args=OrderArgs(
                token_id=token_id,
                price=price,
                size=size,
                side=BUY,
            ),
            options=PartialCreateOrderOptions(
                tick_size=tick_size,
                neg_risk=neg_risk,
            ),
            order_type=OrderType.GTC,
        )

    def place_market_buy(self, token_id: str, amount: float,
                         tick_size: str = "0.01", neg_risk: bool = True):
        """Place a FOK market buy order (amount in USDC)."""
        return self.client.create_and_post_market_order(
            order_args=MarketOrderArgs(
                token_id=token_id,
                side=BUY,
                amount=amount,
            ),
            options=PartialCreateOrderOptions(
                tick_size=tick_size,
                neg_risk=neg_risk,
            ),
            order_type=OrderType.FOK,
        )

    def cancel_order(self, order_id: str):
        """Cancel a single order."""
        from py_clob_client_v2 import OrderPayload
        return self.client.cancel_order(OrderPayload(orderID=order_id))

    def cancel_all_orders(self):
        """Cancel all open orders."""
        return self.client.cancel_all()

    def get_order_status(self, order_id: str):
        """Check order status."""
        return self.client.get_order(order_id)

    def get_my_open_orders(self, market: str = None):
        """Get open orders, optionally filtered by market."""
        from py_clob_client_v2 import OpenOrderParams
        params = OpenOrderParams(market=market) if market else None
        return self.client.get_open_orders(params)

    def is_neg_risk_market(self, token_id: str) -> bool:
        """Check if a market is neg_risk."""
        return self.client.get_neg_risk(token_id)
```

---

## Sources

- GitHub: https://github.com/Polymarket/py-clob-client-v2
- PyPI: https://pypi.org/project/py-clob-client-v2/
- Docs: https://docs.polymarket.com
- Auth docs: https://docs.polymarket.com/api-reference/authentication
- Order docs: https://docs.polymarket.com/trading/orders/create
- Neg risk docs: https://docs.polymarket.com/advanced/neg-risk
- Rate limits: https://docs.polymarket.com/api-reference/rate-limits
- Client source: https://github.com/Polymarket/py-clob-client-v2/blob/main/py_clob_client_v2/client.py
