# Research: Polymarket Weather Markets API

- **Query**: How to programmatically access Polymarket's weather markets (API, CLOB, SDKs, market data)
- **Scope**: External
- **Date**: 2026-05-04

## Findings

### API Architecture Overview

Polymarket is served by **three separate APIs**, each handling a different domain:

| API | Base URL | Purpose | Auth Required |
|---|---|---|---|
| **Gamma API** | `https://gamma-api.polymarket.com` | Markets, events, tags, series, search, profiles | No (fully public) |
| **CLOB API** | `https://clob.polymarket.com` | Orderbook, pricing, trading, order management | L1/L2 for trading; public for reading |
| **Data API** | `https://data-api.polymarket.com` | Supplementary data endpoints | No |

### Gamma API Endpoints (Market Discovery)

The Gamma API is the primary API for discovering and browsing market data. No authentication required.

| Endpoint | Description |
|---|---|
| `GET /events` | List events with filtering and pagination |
| `GET /events/{id}` | Get a single event by ID |
| `GET /markets` | List markets with filtering and pagination |
| `GET /markets/{id}` | Get a single market by ID |
| `GET /public-search` | Search across events, markets, and profiles |
| `GET /tags` | Ranked tags/categories |
| `GET /series` | Series (grouped events) |
| `GET /sports` | Sports metadata |
| `GET /teams` | Teams |

**Key filter parameters for `/events` and `/markets`:**
- `tag_id` -- Filter by tag (weather tag ID discoverable via `/tags`)
- `active=true` -- Filter for live tradable events
- `closed=false` -- Exclude closed markets (default)
- `slug` -- Filter by specific slug
- `clob_token_ids` -- Filter by CLOB token IDs
- `condition_ids` -- Filter by condition IDs
- `liquidity_num_min/max` -- Liquidity range filter
- `limit` / `offset` -- Pagination
- `order` -- Sort field (`volume_24hr`, `volume`, `liquidity`, `start_date`, `end_date`)
- `ascending` -- Sort direction

**Discovering weather markets:**
```bash
# Get weather tag ID
curl "https://gamma-api.polymarket.com/tags" | jq '.[] | select(.label | test("weather"; "i"))'

# Fetch weather events
curl "https://gamma-api.polymarket.com/events?tag_id=<WEATHER_TAG_ID>&active=true&closed=false&limit=100"
```

### CLOB API Endpoints (Prices & Trading)

Base URL: `https://clob.polymarket.com`

**Public endpoints (no auth):**

| Endpoint | Method | Description |
|---|---|---|
| `/markets` | GET | List CLOB markets |
| `/markets/{condition_id}` | GET | Single market details |
| `/books` | GET/POST | Order book for a token ID |
| `/prices` | GET/POST | Best prices for token IDs |
| `/midpoints` | GET/POST | Midpoint prices |
| `/spreads` | GET/POST | Bid-ask spreads |
| `/prices-history` | GET | Historical price data |
| `/last-trade-price` | GET | Last trade price |
| `/market-trades-events` | GET | Market trade events |
| `/tick-size` | GET | Tick size for a token |
| `/neg-risk` | GET | Whether market is negative risk |
| `/fee-rate-bps` | GET | Fee rate in basis points |
| `/time` | GET | Server time |
| `/ok` | GET | Health check |

**Authenticated endpoints (L2 required):**

| Endpoint | Method | Description |
|---|---|---|
| `/order` | POST | Place a new order |
| `/orders` | POST | Batch post up to 15 orders |
| `/order/{order_id}` | DELETE | Cancel an order |
| `/cancel-all` | DELETE | Cancel all orders |
| `/orders` | GET | Get open orders |
| `/trades` | GET | Get trade history |
| `/notifications` | GET | Get notifications |

**WebSocket for real-time data:**
- Connect to CLOB WebSocket for live orderbook streaming
- Channels: `market`, `book`, `price_change`, `last_trade_price`, `tick_size_change`, `best_bid_ask`, `new_market`, `market_resolved`

### Market Data Structure

Every market has:
- **Condition ID**: Unique identifier in CTF contracts
- **Question ID**: Hash of the market question for resolution
- **Token IDs**: ERC1155 token IDs (one for Yes, one for No)
- **outcomes**: String like "Yes,No" (or temperature bucket labels for weather)
- **outcomePrices**: Current prices as comma-separated string
- **volume**: Trading volume
- **tickSize**: Minimum price increment (e.g., "0.01")
- **negRisk**: Boolean for multi-outcome markets (weather markets are typically negRisk=true)

### Official SDKs (V2 - Current)

| Language | Package | Repository | Install |
|---|---|---|---|
| TypeScript | `@polymarket/clob-client-v2` | github.com/Polymarket/clob-client-v2 | `npm install @polymarket/clob-client-v2 viem` |
| Python | `py-clob-client-v2` | github.com/Polymarket/py-clob-client-v2 | `pip install py-clob-client-v2` |
| Rust | `polymarket_client_sdk_v2` | github.com/Polymarket/rs-clob-client-v2 | Cargo |

**Legacy V1 SDKs (deprecated but still available):**
- TypeScript: `@polymarket/clob-client` (uses ethers)
- Python: `py-clob-client` (pip install py-clob-client)

### Python SDK Quickstart (V2)

```python
from py_clob_client_v2 import ClobClient, OrderArgs, OrderType, Side, ApiCreds

# Public (no auth) - read-only
client = ClobClient(host="https://clob.polymarket.com", chain_id=137)
markets = client.get_markets()

# Authenticated - for trading
# Step 1: Derive API credentials
temp_client = ClobClient(host="https://clob.polymarket.com", chain_id=137, key=PRIVATE_KEY)
creds = temp_client.create_or_derive_api_key()

# Step 2: Initialize fully-authenticated client
client = ClobClient(
    host="https://clob.polymarket.com",
    chain_id=137,
    key=PRIVATE_KEY,
    creds=creds,
)

# Place a limit buy order
resp = client.create_and_post_order(
    order_args=OrderArgs(
        token_id="0x...",  # token ID from market data
        price=0.4,
        side=Side.BUY,
        size=100,
    ),
    options=PartialCreateOrderOptions(tick_size="0.01"),
    order_type=OrderType.GTC,
)
```

### TypeScript SDK Quickstart (V2)

```typescript
import { ClobClient, Side, OrderType } from "@polymarket/clob-client-v2";
import { createWalletClient, http } from "viem";
import { privateKeyToAccount } from "viem/accounts";

const account = privateKeyToAccount(process.env.PRIVATE_KEY as `0x${string}`);
const signer = createWalletClient({ account, transport: http() });

// Derive API credentials
const tempClient = new ClobClient({ host: "https://clob.polymarket.com", chain: 137, signer });
const apiCreds = await tempClient.createOrDeriveApiKey();

// Initialize authenticated client
const client = new ClobClient({
  host: "https://clob.polymarket.com",
  chain: 137,
  signer,
  creds: apiCreds,
});

// Place an order
const response = await client.createAndPostOrder(
  { tokenID: "YOUR_TOKEN_ID", price: 0.5, size: 10, side: Side.BUY },
  { tickSize: "0.01", negRisk: false },
  OrderType.GTC,
);
```

### Weather Market Specifics

Weather markets on Polymarket are structured as:
- **Events**: e.g., "Highest temperature in New York City on 2026-05-05"
- **Markets (outcomes)**: Temperature buckets (e.g., "< 55F", "55-59F", "60-64F", "65-69F", etc.)
- These are **multi-outcome (negRisk=true)** markets -- mutually exclusive buckets
- Resolution source: Wunderground data for airport stations (ICAO codes)
- Token IDs map to individual temperature bucket outcomes

**Discovering weather markets via Gamma API:**
```bash
# Search for weather markets
curl "https://gamma-api.polymarket.com/public-search?q=temperature&events_tag=weather&limit=20"

# Or filter by tag_id after discovering the weather tag
curl "https://gamma-api.polymarket.com/events?tag_id=<WEATHER_TAG_ID>&active=true&closed=false"
```

### Related Specs

- None yet (new project)

## Caveats / Not Found

- The exact weather `tag_id` is not documented; it must be discovered dynamically via `GET /tags` on the Gamma API
- The V1 SDKs (py-clob-client, @polymarket/clob-client) are still on PyPI/npm but are deprecated in favor of V2
- The Gamma API schema shows `outcomes` and `outcomePrices` as nullable strings -- parsing is required
- There is no dedicated "weather market" filter parameter in the CLOB API; weather filtering is done via Gamma API tags
- Documentation index available at: https://docs.polymarket.com/llms.txt
