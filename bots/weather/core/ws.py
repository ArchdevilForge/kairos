from __future__ import annotations

import asyncio
import json
from typing import Any

import structlog

log = structlog.get_logger()

WS_URL = "wss://ws-subscriptions-clob.polymarket.com/ws/market"
PING_INTERVAL = 10
RECONNECT_DELAY = 5


class PriceUpdate:
    __slots__ = ("token_id", "best_bid", "best_ask", "event_type", "data")

    def __init__(
        self,
        token_id: str,
        best_bid: float | None = None,
        best_ask: float | None = None,
        event_type: str = "",
        data: dict | None = None,
    ) -> None:
        self.token_id = token_id
        self.best_bid = best_bid
        self.best_ask = best_ask
        self.event_type = event_type
        self.data = data or {}


class MarketWsClient:
    def __init__(self) -> None:
        self._ws: Any = None
        self._subscribed_ids: set[str] = set()
        self._running: bool = False
        self._queue: asyncio.Queue[PriceUpdate] = asyncio.Queue()

    async def connect(self) -> None:
        import websockets  # type: ignore[import-untyped]

        self._running = True
        while self._running:
            try:
                async with websockets.connect(WS_URL) as ws:  # type: ignore[union-attr]
                    self._ws = ws
                    log.info("ws_connected", url=WS_URL)

                    if self._subscribed_ids:
                        await self._send_subscribe(list(self._subscribed_ids))

                    await self._recv_loop(ws)
            except Exception as e:
                log.warning("ws_disconnected", error=str(e))
                self._ws = None
                if self._running:
                    await asyncio.sleep(RECONNECT_DELAY)

    async def _recv_loop(self, ws: Any) -> None:
        assert ws is not None
        while self._running:
            try:
                raw = await asyncio.wait_for(ws.recv(), timeout=PING_INTERVAL - 1)  # type: ignore[union-attr]
                if isinstance(raw, str) and raw == "PONG":
                    continue
                data = json.loads(raw) if isinstance(raw, str) else {}
                event_type = data.get("event_type", "")
                if event_type == "book":
                    await self._handle_book(data)
                elif event_type == "price_change":
                    await self._handle_price_change(data)
                elif event_type == "last_trade_price":
                    await self._handle_last_trade(data)
                elif event_type == "best_bid_ask":
                    await self._handle_best_bid_ask(data)
                elif event_type == "tick_size_change":
                    pass
                else:
                    log.debug("ws_unknown_event", event_type=event_type)
            except asyncio.TimeoutError:
                try:
                    await ws.send("PING")  # type: ignore[union-attr]
                except Exception:
                    break
            except Exception as e:
                log.warning("ws_recv_error", error=str(e))
                break

    async def _handle_best_bid_ask(self, data: dict) -> None:
        asset_id = data.get("asset_id", "")
        best_bid = data.get("best_bid")
        best_ask = data.get("best_ask")
        bid = float(best_bid) if best_bid is not None else None
        ask = float(best_ask) if best_ask is not None else None
        await self._queue.put(
            PriceUpdate(
                token_id=asset_id,
                best_bid=bid,
                best_ask=ask,
                event_type="best_bid_ask",
                data=data,
            )
        )

    async def _handle_price_change(self, data: dict) -> None:
        asset_id = data.get("asset_id", "")
        best_bid = data.get("best_bid")
        best_ask = data.get("best_ask")
        bid = float(best_bid) if best_bid is not None else None
        ask = float(best_ask) if best_ask is not None else None
        await self._queue.put(
            PriceUpdate(
                token_id=asset_id,
                best_bid=bid,
                best_ask=ask,
                event_type="price_change",
                data=data,
            )
        )

    async def _handle_last_trade(self, data: dict) -> None:
        asset_id = data.get("asset_id", "")
        price = data.get("price")
        await self._queue.put(
            PriceUpdate(
                token_id=asset_id,
                event_type="last_trade_price",
                data={"price": float(price) if price else None, **data},
            )
        )

    async def _handle_book(self, data: dict) -> None:
        asset_id = data.get("asset_id", data.get("market", ""))
        await self._queue.put(
            PriceUpdate(
                token_id=asset_id,
                event_type="book",
                data=data,
            )
        )

    async def _send_subscribe(self, token_ids: list[str]) -> None:
        if not self._ws:
            return
        msg = json.dumps(
            {
                "assets_ids": token_ids,
                "type": "market",
                "custom_feature_enabled": True,
            }
        )
        try:
            await self._ws.send(msg)  # type: ignore[union-attr]
            self._subscribed_ids.update(token_ids)
            log.info("ws_subscribed", count=len(token_ids))
        except Exception as e:
            log.warning("ws_subscribe_failed", error=str(e))

    async def subscribe(self, token_ids: list[str]) -> None:
        if self._ws:
            msg = json.dumps(
                {
                    "assets_ids": token_ids,
                    "operation": "subscribe",
                    "custom_feature_enabled": True,
                }
            )
            try:
                await self._ws.send(msg)  # type: ignore[union-attr]
                self._subscribed_ids.update(token_ids)
                log.info("ws_dynamic_subscribe", count=len(token_ids))
            except Exception as e:
                log.warning("ws_dynamic_subscribe_failed", error=str(e))
        else:
            self._subscribed_ids.update(token_ids)

    async def unsubscribe(self, token_ids: list[str]) -> None:
        if not self._ws:
            self._subscribed_ids -= set(token_ids)
            return
        msg = json.dumps(
            {
                "assets_ids": token_ids,
                "operation": "unsubscribe",
            }
        )
        try:
            await self._ws.send(msg)  # type: ignore[union-attr]
            self._subscribed_ids -= set(token_ids)
            log.info("ws_unsubscribed", count=len(token_ids))
        except Exception as e:
            log.warning("ws_unsubscribe_failed", error=str(e))

    async def updates(self) -> PriceUpdate:
        return await self._queue.get()

    def stop(self) -> None:
        self._running = False
