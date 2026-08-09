"""Tests for pm_bot.core.passive_price — ported from weatherbot tests."""

from pm_bot.core.passive_price import (
    DEFAULT_ORDER_POLICY,
    compute_no_anchored_limit_price,
    compute_passive_limit_price,
)

QUOTE = {"bid": 0.09, "ask": 0.11, "tick_size": 0.01}


def test_passive_limit_price_improves_bid_without_crossing_ask():
    price, err = compute_passive_limit_price(QUOTE)
    assert err is None
    assert price == 0.10  # bid + 1 tick, capped below ask


def test_passive_limit_price_zero_improve_ticks_rests_at_bid():
    price, err = compute_passive_limit_price(QUOTE, {"price_improve_ticks": 0})
    assert err is None
    assert price == 0.09


def test_passive_limit_price_never_crosses_spread():
    quote = {"bid": 0.09, "ask": 0.095, "tick_size": 0.01}
    price, err = compute_passive_limit_price(quote, {"price_improve_ticks": 5})
    assert err is None
    assert price == quote["ask"] - quote["tick_size"]  # capped at ask - tick
    assert price == 0.085


def test_passive_limit_price_missing_tick_size():
    price, err = compute_passive_limit_price({"bid": 0.09, "ask": 0.11})
    assert price is None
    assert err == "tick_size_missing"


def test_passive_limit_price_missing_quote():
    price, err = compute_passive_limit_price({"bid": None, "ask": 0.11, "tick_size": 0.01})
    assert price is None
    assert err == "quote_price_missing"


def test_no_anchored_limit_price_anchors_below_fair_no():
    quote = {"bid": 0.82, "ask": 0.85, "tick_size": 0.01}
    price, err = compute_no_anchored_limit_price(quote, fair_no=0.80)
    assert err is None
    assert price == 0.70  # fair_no - 0.10, on the tick grid


def test_no_anchored_limit_price_steps_to_tick_grid():
    quote = {"bid": 0.82, "ask": 0.85, "tick_size": 0.05}
    price, err = compute_no_anchored_limit_price(quote, fair_no=0.80)
    assert err is None
    assert price == 0.70


def test_no_anchored_limit_price_missing_fair_value():
    price, err = compute_no_anchored_limit_price(QUOTE, fair_no=None)
    assert price is None
    assert err == "fair_value_missing"


def test_default_policy_has_price_improve_ticks():
    assert DEFAULT_ORDER_POLICY["price_improve_ticks"] == 1
