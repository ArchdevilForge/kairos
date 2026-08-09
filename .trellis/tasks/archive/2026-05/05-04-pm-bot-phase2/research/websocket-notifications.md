# WebSocket & Notifications Research

## 1. Polymarket WebSocket API

### 1.1 Endpoints

| Channel | Endpoint | Auth Required |
|---------|----------|-------------|
| **Market** | `wss://ws-subscriptions-clob.polymarket.com/ws/market` | No |
| **User** | `wss://ws-subscriptions-clob.polymarket.com/ws/user` | Yes (API key) |
| **Sports** | `wss://sports-api.polymarket.com/ws` | No |
| **RTDS** | `wss://ws-live-data.polymarket.com` | Optional |

### 1.2 Connection Protocol

1. Connect to WebSocket endpoint
2. Immediately send a subscription message (server may close idle connections)
3. Send `PING` every 10 seconds → server responds with `PONG`
4. For sports channel: server sends `ping` every 5s, respond with `pong` within 10s

### 1.3 Market Channel — Subscription

```json
{
  "assets_ids": ["<token_id_1>", "<token_id_2>"],
  "type": "market",
  "custom_feature_enabled": true
}
```

- `assets_ids`: array of token IDs (not condition IDs)
- `custom_feature_enabled: true` required for `best_bid_ask`, `new_market`, `market_resolved`

### 1.4 Market Channel — Event Types

| Event Type | Description | Custom Feature? |
|-----------|-------------|----------------|
| `book` | Full orderbook snapshot (on subscribe + after trades) | No |
| `price_change` | Price level updates (new order or cancel) | No |
| `last_trade_price` | Trade execution (maker+taker matched) | No |
| `tick_size_change` | Tick size changes (price >0.96 or <0.04) | No |
| `best_bid_ask` | Best bid/ask prices + spread | **Yes** |
| `new_market` | New market created | **Yes** |
| `market_resolved` | Market resolution event | **Yes** |

**Key data in `price_change`:** price, size, side, best_bid, best_ask per asset
**Key data in `last_trade_price`:** price, side, size, fee_rate_bps, timestamp
**Key data in `best_bid_ask`:** best_bid, best_ask, spread (lightweight, recommended for price monitoring)

### 1.5 Dynamic Subscription (Market Channel)

Add assets without reconnecting:
```json
{
  "assets_ids": ["new_asset_id"],
  "operation": "subscribe",
  "custom_feature_enabled": true
}
```

Remove assets:
```json
{
  "assets_ids": ["asset_id_to_remove"],
  "operation": "unsubscribe"
}
```

### 1.6 User Channel — Subscription (Authenticated)

```json
{
  "auth": {
    "apiKey": "your-api-key",
    "secret": "your-api-secret",
    "passphrase": "your-passphrase"
  },
  "markets": ["0x1234...condition_id"],
  "type": "user"
}
```

- Uses **condition IDs** (market-level), not asset IDs
- Auth credentials are the same as CLOB API L2 auth

### 1.7 User Channel — Event Types

| Event Type | Description |
|-----------|-------------|
| `trade` | Trade lifecycle: MATCHED → MINED → CONFIRMED (or RETRYING → FAILED) |
| `order` | Order PLACEMENT, UPDATE, CANCELLATION |

### 1.8 Authentication Requirements

- **Market Channel**: No auth needed. Public data.
- **User Channel**: Requires API key credentials (`apiKey`, `secret`, `passphrase`) in subscription message.
- API credentials obtained via `create_or_derive_api_creds()` on ClobClient.

### 1.9 For Our Use Case (Price Monitoring + Trade Alerts)

**Recommended approach:**
1. Subscribe to **Market Channel** with `custom_feature_enabled: true`
2. Use `best_bid_ask` events for lightweight price monitoring (no full book needed)
3. Use `last_trade_price` for trade fill detection
4. Use `price_change` for orderbook depth tracking if needed
5. User Channel only needed if we place orders and want real-time status

---

## 2. Python WebSocket Libraries

### 2.1 `websockets` (standalone library)

- **Install**: `pip install websockets`
- Pure asyncio, lightweight, no external dependencies
- Simple API: `async with websockets.connect(uri) as ws:`
- Best for: dedicated WebSocket connections, long-lived streams
- Manual PING/PONG handling required
- ~1.5K GitHub stars, well-maintained

```python
import asyncio
import websockets
import json

async def stream_prices(token_ids: list[str]):
    uri = "wss://ws-subscriptions-clob.polymarket.com/ws/market"
    async with websockets.connect(uri) as ws:
        # Subscribe
        await ws.send(json.dumps({
            "assets_ids": token_ids,
            "type": "market",
            "custom_feature_enabled": True
        }))
        # Heartbeat + receive loop
        while True:
            try:
                msg = await asyncio.wait_for(ws.recv(), timeout=9)
                data = json.loads(msg)
                # Process event...
            except asyncio.TimeoutError:
                await ws.send("PING")
```

### 2.2 `aiohttp` (full HTTP client with WS support)

- **Install**: `pip install aiohttp`
- Full HTTP client + WebSocket support in one package
- `async with session.ws_connect(uri) as ws:`
- Better if already using aiohttp for HTTP requests
- Slightly heavier dependency

### 2.3 Recommendation for MVP

**Use `websockets`** — it's purpose-built for WebSocket, lighter weight, and our project already uses `httpx` for HTTP (not aiohttp). No need to add aiohttp just for WebSocket.

---

## 3. Notification Approaches

### 3.1 Telegram Bot API

**Setup:**
1. Chat with [@BotFather](https://t.me/BotFather) on Telegram
2. Send `/newbot` → follow prompts → get bot token (format: `123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11`)
3. Get your chat ID: message the bot, then call `https://api.telegram.org/bot<token>/getUpdates`
4. Find `chat.id` in the response

**Send Message:**
```
POST https://api.telegram.org/bot{token}/sendMessage
Content-Type: application/json

{
  "chat_id": 123456789,
  "text": "🚨 Trade Alert: NYC temp bucket 90-95 now at 0.72",
  "parse_mode": "HTML"  // or "Markdown"
}
```

**Python implementation (httpx):**
```python
import httpx

async def send_telegram(token: str, chat_id: int, text: str):
    url = f"https://api.telegram.org/bot{token}/sendMessage"
    async with httpx.AsyncClient() as client:
        await client.post(url, json={
            "chat_id": chat_id,
            "text": text,
            "parse_mode": "HTML"
        })
```

**Features:**
- Free, no rate limits for reasonable use (~30 msg/sec)
- Supports HTML/Markdown formatting, inline keyboards
- Can send photos, files
- Bot can be added to groups/channels
- Python library: `python-telegram-bot` (optional, raw API is simple enough)

**Limitations:**
- One-way (bot → user) unless you set up a webhook/polling for commands
- Bot cannot message users unless they've started a conversation first
- Need to discover chat_id per user

### 3.2 Discord Webhook

**Setup:**
1. Open Discord channel settings → Integrations → Webhooks
2. Click "New Webhook" → name it → copy webhook URL
3. URL format: `https://discord.com/api/webhooks/{webhook_id}/{webhook_token}`

**Send Message:**
```
POST https://discord.com/api/webhooks/{id}/{token}
Content-Type: application/json

{
  "content": "🚨 Trade Alert: NYC temp bucket 90-95 now at 0.72",
  "username": "PM-Bot",
  "embeds": [...]  // optional rich embeds
}
```

**Python implementation (httpx):**
```python
import httpx

async def send_discord(webhook_url: str, content: str):
    async with httpx.AsyncClient() as client:
        await client.post(webhook_url, json={
            "content": content,
            "username": "PM-Bot"
        })
```

**Features:**
- No bot user needed, no gateway connection
- Supports rich embeds (title, description, fields, colors)
- Can override username and avatar per message
- Rate limit: ~5 messages per 2 seconds per channel
- Supports file attachments

**Limitations:**
- One-way only (webhook → channel), no interaction
- Need manage_webhooks permission in the Discord channel
- Webhook URL is a secret — if leaked, anyone can post

### 3.3 Comparison for MVP

| Factor | Telegram Bot | Discord Webhook |
|--------|-------------|-----------------|
| **Setup complexity** | Medium (BotFather + chat_id) | Low (channel settings → copy URL) |
| **Code complexity** | Low (1 HTTP POST) | Low (1 HTTP POST) |
| **No additional deps** | ✅ (raw httpx) | ✅ (raw httpx) |
| **Rich formatting** | HTML/Markdown | Rich embeds |
| **Mobile push** | ✅ (Telegram app) | ✅ (Discord app) |
| **Group support** | ✅ | ✅ |
| **Auth required** | Bot token | Webhook URL |
| **Bidirectional** | Possible (with polling/webhook) | No |
| **Rate limit** | ~30 msg/sec | ~5 msg/2sec |

### 3.4 MVP Recommendation

**Start with Discord Webhook** — simplest setup (just a URL, no chat_id discovery), then add Telegram Bot as a second option. Both use a single HTTP POST with httpx, so the notification layer is trivially extensible.

Architecture:
```python
# Abstract notifier
class Notifier(Protocol):
    async def send(self, message: str) -> None: ...

# Implementations
class DiscordWebhookNotifier:  # webhook_url in config
class TelegramBotNotifier:     # bot_token + chat_id in config
```

---

## 4. Existing Project Context

From codebase search, the current pm-bot uses:
- `httpx.AsyncClient` for all HTTP (Gamma API, CLOB API, Open-Meteo)
- `TTLCache` for caching market data
- `structlog` for structured logging
- Polling-based `watch` command (60s interval via `httpx`)
- No existing WebSocket or notification code

The research in `.trellis/tasks/00-bootstrap-guidelines/research/polymarket-weather-markets-api.md` already documented the WebSocket channels but did not provide connection/subscription details.

The existing `watch` command in `pm_bot/cli/watch.py` polls every 60 seconds — WebSocket would replace this with sub-second streaming.

---

## 5. Key Findings Summary

1. **WebSocket endpoint**: `wss://ws-subscriptions-clob.polymarket.com/ws/market` — no auth needed for price data
2. **Best event for price monitoring**: `best_bid_ask` (lightweight, includes spread) — requires `custom_feature_enabled: true`
3. **Trade detection**: `last_trade_price` event type
4. **Dynamic subscription**: Can add/remove token IDs without reconnecting
5. **Heartbeat**: Must send `PING` every 10 seconds
6. **Python library**: `websockets` (lightweight, pure asyncio, complements existing `httpx`)
7. **Notifications**: Discord Webhook is simplest for MVP (just a URL + HTTP POST); Telegram Bot also simple (token + chat_id + HTTP POST)
8. **No new heavy deps**: Both WebSocket and notifications work with `websockets` + existing `httpx`
