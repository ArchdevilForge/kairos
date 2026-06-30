"""Tests for Telegram hard-data alerts."""

from unittest.mock import AsyncMock, MagicMock, patch

import pytest

from kairos.telegram import AlertEvent, TelegramClient, format_event


def test_alert_event_payload_shape():
    event = AlertEvent(
        event="price_velocity",
        symbol="BTC/USDT:USDT",
        price=65000.0,
        condition="30s_0.5pct",
        severity="MEDIUM",
        change_pct=1.5,
        event_id="evt-1",
        timestamp="2026-06-27T00:00:00+00:00",
    )

    assert event.to_payload() == {
        "event": "price_velocity",
        "event_type": "price_velocity",
        "event_id": "evt-1",
        "timestamp": "2026-06-27T00:00:00+00:00",
        "symbol": "BTC/USDT:USDT",
        "price": 65000.0,
        "condition": "30s_0.5pct",
        "exchange": "",
        "change_pct": 1.5,
        "severity": "MEDIUM",
    }


def test_format_event_marks_human_decision_only():
    text = format_event(
        AlertEvent(
            event="open_interest_change",
            symbol="ETH/USDT:USDT",
            price=3000.0,
            condition="oi_change=6%",
            severity="HIGH",
            change_pct=6.0,
            timestamp="2026-06-27T01:02:03+00:00",
        )
    )

    assert "<b>非指令</b> 仅供人工判断" in text
    assert "<b>[高] ETH 持仓量异动</b>" in text
    assert "<b>价/变</b>: 3000.0 / +6.00%" in text


@pytest.mark.asyncio
async def test_telegram_client_without_credentials_drops_message(monkeypatch):
    monkeypatch.delenv("TELEGRAM_BOT_TOKEN", raising=False)
    monkeypatch.delenv("TELEGRAM_CHAT_ID", raising=False)

    assert await TelegramClient().send_text("hello") is False


@pytest.mark.asyncio
async def test_telegram_client_sends_message():
    response = MagicMock()
    response.raise_for_status.return_value = None
    client = MagicMock()
    client.post = AsyncMock(return_value=response)

    with patch("kairos.telegram.httpx.AsyncClient", return_value=client):
        telegram = TelegramClient(bot_token="token", chat_id="chat")
        assert await telegram.send_text("hello") is True

    client.post.assert_awaited_once()
    assert client.post.call_args.args[0] == "https://api.telegram.org/bottoken/sendMessage"
