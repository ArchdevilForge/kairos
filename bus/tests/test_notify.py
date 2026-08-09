"""格式化与推送测试。"""

import json

import httpx

from kairos_bus.model import Event
from kairos_bus.notify import TelegramNotifier, format_alert


def test_format_alert_html_escapes_user_text():
    evt = Event(floor="pm", event_type="spread_alert", severity="HIGH",
                title="<b>价差</b>", message="a & b < c")
    text = format_alert(evt)
    assert "<b>" in text                      # 我们自己加的标签
    assert "&lt;b&gt;价差&lt;/b&gt;" in text  # 用户文本被转义
    assert "a &amp; b &lt; c" in text


def test_format_alert_contains_key_fields():
    evt = Event(floor="pm", event_type="spread_alert", severity="MEDIUM",
                title="t", data={"spread": 0.12, "threshold": 0.08})
    text = format_alert(evt)
    assert "[pm]" in text and "MEDIUM" in text and "spread_alert" in text
    assert "spread=0.12" in text


def test_notifier_disabled_without_creds():
    n = TelegramNotifier(token=None, chat_id=None)
    assert not n.enabled
    assert n.send(Event()) is False


def test_notifier_send_success(monkeypatch):
    calls = {}

    def handler(request: httpx.Request) -> httpx.Response:
        calls["url"] = str(request.url)
        calls["json"] = json.loads(request.content) if request.content else None
        return httpx.Response(200, json={"ok": True})

    client = httpx.Client(transport=httpx.MockTransport(handler))
    n = TelegramNotifier(token="t", chat_id="c", client=client)
    evt = Event(floor="pm", event_type="spread_alert", title="hi")
    assert n.send(evt) is True
    assert calls["json"]["chat_id"] == "c"
    assert calls["json"]["parse_mode"] == "HTML"


def test_notifier_send_failure(monkeypatch):
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(500, text="boom")

    client = httpx.Client(transport=httpx.MockTransport(handler))
    n = TelegramNotifier(token="t", chat_id="c", client=client)
    assert n.send(Event()) is False
