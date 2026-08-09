"""Telegram 推送 — kairos 风格紧凑 HTML 告警。"""

from __future__ import annotations

import html
import os

import httpx

from .model import Event

_SEV_ICON = {"LOW": "🟢", "MEDIUM": "🟡", "HIGH": "🔴"}


def format_alert(evt: Event) -> str:
    """一条事件 → 一行紧凑 HTML 文本(Telegram parse_mode=HTML)。"""
    icon = _SEV_ICON.get(evt.severity.upper(), "⚪")
    lines = [
        (
            f"<b>{icon} [{evt.floor}] {html.escape(evt.title or evt.event_type)}</b>"
            f" | {html.escape(evt.severity.upper())} | {html.escape(evt.event_type)}"
        ),
    ]
    if evt.message:
        lines.append(html.escape(evt.message))
    if evt.data:
        kv = " | ".join(f"{k}={v}" for k, v in list(evt.data.items())[:6])
        lines.append(f"<code>{html.escape(kv)}</code>")
    return "\n".join(lines)


class TelegramNotifier:
    def __init__(self, token: str | None = None, chat_id: str | None = None,
                 client: httpx.Client | None = None) -> None:
        self.token = token
        self.chat_id = chat_id
        self._client = client or httpx.Client(timeout=10)

    @property
    def enabled(self) -> bool:
        return bool(self.token and self.chat_id)

    @classmethod
    def from_env(cls, token_env: str, chat_env: str) -> TelegramNotifier:
        return cls(token=os.environ.get(token_env), chat_id=os.environ.get(chat_env))

    def send(self, evt: Event) -> bool:
        """发送一条告警。失败返回 False(由 caller 决定不提交 cooldown)。"""
        if not self.enabled:
            return False
        r = self._client.post(
            f"https://api.telegram.org/bot{self.token}/sendMessage",
            json={"chat_id": self.chat_id, "text": format_alert(evt), "parse_mode": "HTML"},
        )
        return r.is_success
