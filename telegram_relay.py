#!/usr/bin/env python3
"""Split webhook: receive from Kairos → Telegram + forward to Hermes."""

import asyncio
import hashlib
import hmac
import json
import os
import sys
from datetime import datetime, timezone

import httpx
from aiohttp import web

BOT_TOKEN = os.getenv("TELEGRAM_BOT_TOKEN", "")
CHAT_ID = os.getenv("TELEGRAM_CHAT_ID", "6659574405")
SECRET = os.getenv("KAIROS_WEBHOOK_SECRET", "")
PORT = int(os.getenv("RELAY_PORT", "8655"))
HERMES_URL = os.getenv("HERMES_WEBHOOK_URL", "http://localhost:8644/webhooks/kairos-signals")

if not BOT_TOKEN:
    print("FATAL: TELEGRAM_BOT_TOKEN not set", file=sys.stderr)
    sys.exit(1)

_client: httpx.AsyncClient | None = None


def get_client() -> httpx.AsyncClient:
    global _client
    if _client is None:
        _client = httpx.AsyncClient(timeout=10)
    return _client


def verify(payload: bytes, sig: str) -> bool:
    if not SECRET:
        return True
    expected = hmac.new(SECRET.encode(), payload, hashlib.sha256).hexdigest()
    return hmac.compare_digest(expected, sig)


async def send_telegram(text: str) -> None:
    url = f"https://api.telegram.org/bot{BOT_TOKEN}/sendMessage"
    resp = await get_client().post(url, json={"chat_id": CHAT_ID, "text": text, "parse_mode": "HTML"})
    if resp.status_code != 200:
        print(f"TG error: {resp.status_code}", file=sys.stderr)


def format_message(data: dict) -> str:
    symbol = data.get("symbol", "").replace("/USDT:USDT", "").replace("/USDT", "")
    event = data.get("event", "?")
    price = data.get("price", 0)
    chg = data.get("change_pct", 0)
    sev = data.get("severity", "LOW")
    cond = data.get("condition", "")
    ts = datetime.now(timezone.utc).strftime("%H:%M")

    if event == "price_velocity":
        d = "📈" if chg > 0 else "📉"
        return f"<b>{d} {symbol} {chg:+.2f}%</b>\n{price} | {cond}\n{sev} | {ts}"
    elif event == "funding_rate_anomaly":
        return f"<b>💰 {symbol} 费率异动</b>\n{price} | {cond}\n{sev} | {ts}"
    elif event == "open_interest_change":
        return f"<b>📊 {symbol} OI {chg:+.2f}%</b>\n{price}\n{sev} | {ts}"
    return f"<b>📡 {symbol} {event}</b>\n{price} | {ts}"


async def forward_to_hermes(body: bytes, headers: dict) -> None:
    try:
        resp = await get_client().post(
            HERMES_URL,
            content=body,
            headers={
                "Content-Type": "application/json",
                "X-Webhook-Signature": headers.get("X-Webhook-Signature", ""),
                "X-Request-ID": headers.get("X-Request-ID", ""),
            },
        )
        if resp.status_code not in (200, 202):
            print(f"Hermes fwd: {resp.status_code}", file=sys.stderr)
    except Exception as e:
        print(f"Hermes fwd err: {e}", file=sys.stderr)


async def handle(request: web.Request) -> web.Response:
    body = await request.read()
    sig = request.headers.get("X-Webhook-Signature", "")
    if not verify(body, sig):
        return web.Response(status=403, text="bad sig")

    data = json.loads(body)
    event = data.get("event", "")

    # Always: send to Telegram for price_velocity
    if event == "price_velocity":
        text = format_message(data)
        await send_telegram(text)

    # Always: forward to Hermes for LLM analysis
    await forward_to_hermes(body, dict(request.headers))

    return web.Response(text="ok")


async def on_shutdown(app):
    global _client
    if _client:
        await _client.aclose()


def main():
    app = web.Application()
    app.router.add_post("/webhook", handle)
    app.on_shutdown.append(on_shutdown)
    print(f"Relay :{PORT} → TG + Hermes", file=sys.stderr)
    web.run_app(app, host="127.0.0.1", port=PORT)


if __name__ == "__main__":
    main()
