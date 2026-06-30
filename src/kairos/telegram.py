"""Telegram delivery for human-controlled Kairos alerts."""

from __future__ import annotations

import html
import logging
import os
import uuid
from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Any

import httpx

logger = logging.getLogger(__name__)


@dataclass
class AlertEvent:
    """Hard-data market alert. This is not a trading instruction."""

    event: str
    symbol: str
    price: float
    condition: str
    event_id: str = field(default_factory=lambda: str(uuid.uuid4()))
    timestamp: str = field(default_factory=lambda: datetime.now(timezone.utc).isoformat())
    exchange: str = ""
    change_pct: float = 0.0
    severity: str = "LOW"
    data: dict[str, Any] = field(default_factory=dict)

    def to_payload(self) -> dict[str, Any]:
        """Return a JSON-safe event payload for logs/tests."""
        return {
            "event": self.event,
            "event_type": self.event,
            "event_id": self.event_id,
            "timestamp": self.timestamp,
            "symbol": self.symbol,
            "price": self.price,
            "condition": self.condition,
            "exchange": self.exchange,
            "change_pct": self.change_pct,
            "severity": self.severity,
        }


class TelegramClient:
    """Small Telegram sender shared by scanner and realtime alerts."""

    def __init__(
        self,
        bot_token: str = "",
        chat_id: str = "",
        parse_mode: str = "HTML",
        timeout: float = 10.0,
    ) -> None:
        self.bot_token = bot_token or os.getenv("TELEGRAM_BOT_TOKEN", "")
        self.chat_id = chat_id or os.getenv("TELEGRAM_CHAT_ID", "")
        self.parse_mode = parse_mode
        self.timeout = httpx.Timeout(timeout, connect=5.0)
        self._client: httpx.AsyncClient | None = None

    def is_configured(self) -> bool:
        """Return whether Telegram credentials are available."""
        return bool(self.bot_token and self.chat_id)

    async def send_event(self, event: AlertEvent) -> bool:
        """Send one formatted alert event."""
        return await self.send_text(format_event(event))

    async def send_text(self, text: str) -> bool:
        """Send one Telegram message."""
        if not self.is_configured():
            logger.warning("TELEGRAM_BOT_TOKEN or TELEGRAM_CHAT_ID not set; dropping alert")
            return False

        if self._client is None:
            self._client = httpx.AsyncClient(timeout=self.timeout)

        url = f"https://api.telegram.org/bot{self.bot_token}/sendMessage"
        try:
            response = await self._client.post(
                url,
                json={"chat_id": self.chat_id, "text": text, "parse_mode": self.parse_mode},
            )
            response.raise_for_status()
            return True
        except Exception:
            logger.exception("Telegram send failed")
            return False

    async def close(self) -> None:
        """Close the underlying HTTP client."""
        if self._client is not None:
            await self._client.aclose()
            self._client = None


def format_event(event: AlertEvent) -> str:
    """Format a hard-data event for human review."""
    symbol = html.escape(event.symbol.replace("/USDT:USDT", "").replace("/USDT", ""))
    event_name = html.escape(_event_name_zh(event.event))
    severity = html.escape(_severity_zh(event.severity))
    timestamp = event.timestamp.split("T", 1)[-1][:5] if "T" in event.timestamp else event.timestamp[:5]

    if event.event == "resonance":
        return _format_resonance(event, symbol, severity, timestamp)
    if event.event == "liquidation":
        return _format_liquidation(event, symbol, severity, timestamp)
    if event.event == "long_short_ratio":
        return _format_long_short(event, symbol, severity, timestamp)

    condition = html.escape(event.condition)
    lines = [
        f"<b>[{severity}] {symbol} {event_name}</b>",
        "<b>非指令</b> 仅供人工判断",
        f"<b>价/变</b>: {event.price} / {event.change_pct:+.2f}% | {timestamp} UTC",
        f"<b>触发</b>: {condition}",
    ]
    if event.exchange:
        lines.append(f"<b>交易所</b>: {html.escape(event.exchange)}")
    return "\n".join(lines)


def _format_resonance(event: AlertEvent, symbol: str, severity: str, timestamp: str) -> str:
    """Format a multi-dimension resonance event with all contributing dimensions."""
    data = event.data or {}
    dims = data.get("dimensions", [])
    dim_count = data.get("dimension_count", len(dims))
    score = data.get("signal_score", 0)

    lines = [
        f"<b>[{severity}] {symbol} 信号质量={score}</b>",
        "<b>非指令</b> 仅供人工判断",
        f"<b>维度</b>: {dim_count}个 | {timestamp} UTC",
    ]
    for dim in dims:
        dim_data = data.get(f"{dim}_data", {})
        dim_zh = _event_name_zh(dim)
        if dim == "price_velocity":
            pct = dim_data.get("change_pct", "?")
            ws = dim_data.get("window_seconds", "?")
            lines.append(f"  ▸ {dim_zh}: {pct}% / {ws}s")
        elif dim == "volume_spike":
            ratio = dim_data.get("ratio", "?")
            lines.append(f"  ▸ {dim_zh}: {ratio}x")
        elif dim == "open_interest_change":
            pct = dim_data.get("change_pct", "?")
            lines.append(f"  ▸ {dim_zh}: {pct}%")
        elif dim == "funding_rate_anomaly":
            rate = dim_data.get("funding_rate", "?")
            lines.append(f"  ▸ {dim_zh}: {rate}")
        elif dim == "long_short_ratio":
            long_r = dim_data.get("long_rate", "?")
            short_r = dim_data.get("short_rate", "?")
            lines.append(f"  ▸ {dim_zh}: 多{long_r}% / 空{short_r}%")
        elif dim == "liquidation":
            total = dim_data.get("total_liquidation_millions", "?")
            lines.append(f"  ▸ {dim_zh}: ${total}M")
        else:
            lines.append(f"  ▸ {dim_zh}")
    return "\n".join(lines)


def _format_liquidation(event: AlertEvent, symbol: str, severity: str, timestamp: str) -> str:
    """Format a liquidation event."""
    data = event.data or {}
    total = data.get("total_liquidation_millions", 0)
    long_pct = data.get("long_liquidation_pct", 50)
    short_pct = data.get("short_liquidation_pct", 50)
    reason = data.get("reason", "?")
    zs = data.get("zscore")
    zs_text = f" | Z={zs}" if zs else ""
    lines = [
        f"<b>[{severity}] {symbol} 爆仓异动</b>",
        "<b>非指令</b> 仅供人工判断",
        f"<b>金额</b>: ${total}M{zs_text} | {timestamp} UTC",
        f"<b>多/空</b>: {long_pct}% / {short_pct}%",
        f"<b>原因</b>: {html.escape(str(reason))}",
    ]
    return "\n".join(lines)


def _format_long_short(event: AlertEvent, symbol: str, severity: str, timestamp: str) -> str:
    """Format a long/short ratio event."""
    data = event.data or {}
    long_r = data.get("long_rate", "?")
    short_r = data.get("short_rate", "?")
    ratio = data.get("ls_ratio", "?")
    reason = data.get("reason", "?")
    zs = data.get("zscore")
    zs_text = f" | Z={zs}" if zs else ""
    lines = [
        f"<b>[{severity}] {symbol} 多空比异动</b>",
        "<b>非指令</b> 仅供人工判断",
        f"<b>多/空</b>: {long_r}% / {short_r}% (比={ratio}){zs_text} | {timestamp} UTC",
        f"<b>原因</b>: {html.escape(str(reason))}",
    ]
    return "\n".join(lines)


def _event_name_zh(event: str) -> str:
    return {
        "price_velocity": "价格异动",
        "volume_spike": "成交量异动",
        "open_interest_change": "持仓量异动",
        "funding_rate_anomaly": "资金费率异动",
        "long_short_ratio": "多空比异动",
        "liquidation": "爆仓异动",
        "resonance": "多维度共振",
    }.get(event, event)


def _severity_zh(severity: str) -> str:
    return {"LOW": "低", "MEDIUM": "中", "HIGH": "高"}.get(severity, severity)
