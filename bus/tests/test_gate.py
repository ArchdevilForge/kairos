"""门控纪律测试 — 对应 kairos pipeline.go 的 delivery gating。"""

import time

from kairos_bus.config import BusConfig
from kairos_bus.gate import Gate
from kairos_bus.model import Event


def make_cfg(**over) -> BusConfig:
    cfg = BusConfig(
        inbound_dir="inbound",
        out_dir="out",
        dedup_window_seconds=5,
        symbol_cooldown_seconds=1800,
        max_alerts_per_day=100,
        telegram_enabled=False,
    )
    from kairos_bus.config import FloorConfig

    cfg.floors = {
        "pm": FloorConfig(
            name="pm",
            source="inbound/pm",
            allowed_event_types=["spread_alert", "edge_alert"],
            min_severity="MEDIUM",
            blacklist=["BAD-SYMBOL"],
        ),
        "futures": FloorConfig(
            name="futures",
            source="inbound/futures",
            allowed_event_types=["price_velocity", "market_impulse"],
            min_severity="LOW",
            skip_cooldown_event_types=["market_impulse"],
        ),
    }
    return cfg


def evt(**kw) -> Event:
    base = {
        "floor": "pm", "event_type": "spread_alert", "severity": "MEDIUM",
        "key": "k1", "symbol": "S1", "title": "t", "message": "m",
    }
    base.update(kw)
    return Event(**base)


def test_unknown_floor_rejected():
    g = Gate(make_cfg())
    allowed, reason = g.decide(evt(floor="nope"))
    assert (allowed, reason) == (False, "unknown_floor")


def test_event_type_allow_list():
    g = Gate(make_cfg())
    allowed, reason = g.decide(evt(event_type="sniper_signal"))
    assert (allowed, reason) == (False, "event_type")


def test_blacklist_drops_symbol():
    g = Gate(make_cfg())
    allowed, reason = g.decide(evt(symbol="BAD-SYMBOL"))
    assert (allowed, reason) == (False, "blacklist")


def test_severity_gate():
    g = Gate(make_cfg())
    allowed, reason = g.decide(evt(severity="LOW"))
    assert (allowed, reason) == (False, "severity")


def test_never_notify_always_rejected():
    g = Gate(make_cfg())
    allowed, reason = g.decide(evt(event_type="outcome"))
    assert (allowed, reason) == (False, "never_notify")
    allowed, reason = g.decide(evt(event_type="calibration", severity="HIGH"))
    assert (allowed, reason) == (False, "never_notify")


def test_pass_allows_and_commits_dedup():
    g = Gate(make_cfg())
    now = time.time()
    allowed, _ = g.decide(evt(), now=now)
    assert allowed is True
    # 同 key 立刻重复 → dedup
    allowed, reason = g.decide(evt(), now=now + 1)
    assert (allowed, reason) == (False, "dedup")
    # 窗口过后放行
    allowed, _ = g.decide(evt(), now=now + 10)
    assert allowed is True


def test_dedup_key_is_per_key_not_per_type():
    g = Gate(make_cfg())
    now = time.time()
    assert g.decide(evt(key="k1"), now=now)[0] is True
    assert g.decide(evt(key="k2"), now=now + 1)[0] is True


def test_symbol_cooldown_blocks_other_keys_same_symbol():
    g = Gate(make_cfg())
    now = time.time()
    assert g.decide(evt(key="k1", symbol="S1"), now=now)[0] is True
    g.commit_success(evt(key="k1", symbol="S1"), now=now)
    allowed, reason = g.decide(evt(key="k2", symbol="S1"), now=now + 60)
    assert (allowed, reason) == (False, "cooldown")


def test_cooldown_only_commits_on_success():
    g = Gate(make_cfg())
    now = time.time()
    assert g.decide(evt(symbol="S1"), now=now)[0] is True
    # 没 commit_success(推送失败)→ 冷却未生效,立即重试不烧冷却
    assert g.decide(evt(key="k2", symbol="S1"), now=now + 1)[0] is True


def test_skip_cooldown_event_types():
    g = Gate(make_cfg())
    now = time.time()
    e1 = evt(floor="futures", event_type="market_impulse", severity="LOW", symbol="S1")
    assert g.decide(e1, now=now)[0] is True
    g.commit_success(e1, now=now)
    # market_impulse 不占 cooldown → 同 symbol 价格事件立即放行
    assert g.decide(evt(floor="futures", event_type="price_velocity", symbol="S1"),
                    now=now + 1)[0] is True


def test_attention_budget():
    g = Gate(make_cfg())
    now = time.time()
    for i in range(100):
        assert g.decide(evt(key=f"k{i}", symbol=f"S{i}"), now=now)[0] is True
        g.commit_success(evt(key=f"k{i}", symbol=f"S{i}"), now=now)
    allowed, reason = g.decide(evt(key="k101", symbol="S101"), now=now)
    assert (allowed, reason) == (False, "attention_budget")
