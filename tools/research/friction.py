"""Parameterized perpetual full-friction cost & canonical NetR model (Work Items 2 & 12).

Cost equation:
    Cost = V * (f_open + f_close + s_open + s_close) + sum(funding over 8h ticks)

Defaults:
    - Taker fee f = 5 bps (0.0005)
    - Maker fee f = 2 bps (0.0002), ONLY when limit price is traded through by >= 1 tick
    - Dynamic slippage s = s0 + lambda * (ATR_1m / P) * sqrt(V / ADV_5m)
      with s0 = 1.5 bps (BTC/ETH majors) and s0 = 5.0 bps (altcoins)
    - Protective / breakeven / trailing stop exits ALWAYS pay taker fee + dynamic slippage
    - Canonical NetR denominator = pre-entry initial 1R including ex-ante slippage buffer
"""
from __future__ import annotations

import math
from dataclasses import dataclass
from typing import Sequence

from pit_universe import is_major_symbol

FUNDING_INTERVAL_SEC = 8 * 3600  # 28,800s (00:00, 08:00, 16:00 UTC)


@dataclass(frozen=True)
class FrictionParams:
    """Parameterized perpetual execution cost knobs."""

    taker_fee_bps: float = 5.0
    maker_fee_bps: float = 2.0
    s0_major_bps: float = 1.5
    s0_alt_bps: float = 5.0
    lambda_impact: float = 0.5
    default_funding_rate: float = 0.0

    @property
    def taker_fee_frac(self) -> float:
        return self.taker_fee_bps * 1e-4

    @property
    def maker_fee_frac(self) -> float:
        return self.maker_fee_bps * 1e-4

    def base_slippage_frac(self, symbol: str) -> float:
        bps = self.s0_major_bps if is_major_symbol(symbol) else self.s0_alt_bps
        return bps * 1e-4


@dataclass(frozen=True)
class CostBreakdown:
    """USD friction components and canonical initial 1R denominator."""

    notional_usd: float
    fee_open_usd: float
    fee_close_usd: float
    slip_open_usd: float
    slip_close_usd: float
    funding_usd: float
    total_cost_usd: float
    initial_1r_pct: float
    initial_1r_usd: float
    gross_pnl_usd: float
    net_pnl_usd: float
    net_r: float


def direction_sign(direction: str) -> float:
    """Return +1.0 for long/up, -1.0 for short/down."""
    d = direction.strip().upper()
    if d in ("UP", "LONG", "+1", "BUY"):
        return 1.0
    if d in ("DOWN", "SHORT", "-1", "SELL"):
        return -1.0
    raise ValueError(f"unsupported trade direction: {direction!r}")


def dynamic_slippage_frac(
    symbol: str,
    price: float,
    atr_1m: float = 0.0,
    notional_usd: float = 0.0,
    adv_5m_usd: float = 0.0,
    params: FrictionParams = FrictionParams(),
) -> float:
    """Compute one-way dynamic slippage fraction s = s0 + lambda*(ATR_1m/P)*sqrt(V/ADV_5m)."""
    s0 = params.base_slippage_frac(symbol)
    if price <= 0 or atr_1m <= 0 or notional_usd <= 0 or adv_5m_usd <= 0:
        return s0
    rel_atr = atr_1m / price
    participation = math.sqrt(notional_usd / adv_5m_usd)
    return s0 + params.lambda_impact * rel_atr * participation


def limit_order_fills(
    is_buy: bool,
    limit_price: float,
    bar_low: float,
    bar_high: float,
    tick_size: float,
) -> bool:
    """Limit order fills as maker ONLY if price trades through by >= 1 tick."""
    if tick_size <= 0:
        raise ValueError("tick_size must be positive for limit fill check")
    if is_buy:
        return bar_low <= limit_price - tick_size + 1e-12
    return bar_high >= limit_price + tick_size - 1e-12


def funding_ticks_between(entry_ts: int, exit_ts: int) -> list[int]:
    """Return 8h UTC funding settlement timestamps in (entry_ts, exit_ts]."""
    if exit_ts <= entry_ts:
        return []
    first = ((int(entry_ts) // FUNDING_INTERVAL_SEC) + 1) * FUNDING_INTERVAL_SEC
    out: list[int] = []
    t = first
    while t <= int(exit_ts):
        out.append(t)
        t += FUNDING_INTERVAL_SEC
    return out


def compute_funding_usd(
    direction: str,
    notional_usd: float,
    entry_ts: int,
    exit_ts: int,
    funding_schedule: dict[int, float] | Sequence[tuple[int, float]] | None = None,
    default_rate: float = 0.0,
) -> float:
    """Accumulate funding cost in USD across 8h settlement ticks in `(entry_ts, exit_ts]`.

    Positive return = net funding PAID (reduces NetPnL);
    negative return = net funding RECEIVED (rebate, increases NetPnL).
    """
    if exit_ts <= entry_ts or notional_usd <= 0:
        return 0.0
    sgn = direction_sign(direction)
    rate_by_tick: dict[int, float] = {}
    if isinstance(funding_schedule, dict):
        rate_by_tick = {int(k): float(v) for k, v in funding_schedule.items()}
    elif funding_schedule is not None:
        rate_by_tick = {int(ts): float(r) for ts, r in funding_schedule}

    total = 0.0
    for tick_ts in funding_ticks_between(entry_ts, exit_ts):
        rate = rate_by_tick.get(tick_ts, default_rate)
        total += sgn * notional_usd * rate
    return total


def initial_1r_pct(
    entry_price: float,
    stop_price: float,
    slippage_buffer_frac: float,
) -> float:
    """Pre-entry initial 1R fraction of notional, including ex-ante slippage buffer."""
    if entry_price <= 0 or stop_price <= 0:
        raise ValueError("entry_price and stop_price must be positive")
    raw_stop_pct = abs(entry_price - stop_price) / entry_price
    if raw_stop_pct <= 0:
        raise ValueError("stop_price cannot equal entry_price")
    return raw_stop_pct + max(0.0, slippage_buffer_frac)


def evaluate_trade_net_r(
    *,
    symbol: str,
    direction: str,
    entry_price: float,
    exit_price: float,
    stop_price: float,
    entry_ts: int,
    exit_ts: int,
    notional_usd: float = 10_000.0,
    open_is_maker: bool = False,
    close_is_maker: bool = False,
    is_stop_exit: bool = False,
    atr_1m_open: float = 0.0,
    adv_5m_open_usd: float = 0.0,
    atr_1m_close: float = 0.0,
    adv_5m_close_usd: float = 0.0,
    funding_schedule: dict[int, float] | Sequence[tuple[int, float]] | None = None,
    params: FrictionParams = FrictionParams(),
) -> CostBreakdown:
    """Compute full-friction Cost and canonical NetR for a simulated/counterfactual trade.

    Protective, breakeven, and trailing stops (`is_stop_exit=True`) ALWAYS pay
    taker fee and dynamic slippage on exit, regardless of `close_is_maker`.
    """
    sgn = direction_sign(direction)
    f_open = params.maker_fee_frac if open_is_maker else params.taker_fee_frac
    use_maker_close = close_is_maker and not is_stop_exit
    f_close = params.maker_fee_frac if use_maker_close else params.taker_fee_frac

    s_open_ex_ante = dynamic_slippage_frac(
        symbol,
        entry_price,
        atr_1m=atr_1m_open,
        notional_usd=notional_usd,
        adv_5m_usd=adv_5m_open_usd,
        params=params,
    )
    s_open = 0.0 if open_is_maker else s_open_ex_ante
    s_close = (
        0.0
        if use_maker_close
        else dynamic_slippage_frac(
            symbol,
            exit_price,
            atr_1m=atr_1m_close if atr_1m_close > 0 else atr_1m_open,
            notional_usd=notional_usd,
            adv_5m_usd=adv_5m_close_usd if adv_5m_close_usd > 0 else adv_5m_open_usd,
            params=params,
        )
    )

    # Pre-entry initial 1R denominator uses ONLY ex-ante known quantities (t <= entry_ts):
    # structural stop distance + ex-ante round-trip slippage buffer (2 * s_open_ex_ante).
    slip_buffer_ex_ante = 2.0 * s_open_ex_ante
    r1_pct = initial_1r_pct(entry_price, stop_price, slip_buffer_ex_ante)
    r1_usd = notional_usd * r1_pct

    fee_open_usd = notional_usd * f_open
    fee_close_usd = notional_usd * f_close
    slip_open_usd = notional_usd * s_open
    slip_close_usd = notional_usd * s_close
    funding_usd = compute_funding_usd(
        direction=direction,
        notional_usd=notional_usd,
        entry_ts=entry_ts,
        exit_ts=exit_ts,
        funding_schedule=funding_schedule,
        default_rate=params.default_funding_rate,
    )

    total_cost_usd = (
        fee_open_usd + fee_close_usd + slip_open_usd + slip_close_usd + funding_usd
    )
    gross_pnl_usd = sgn * notional_usd * ((exit_price - entry_price) / entry_price)
    net_pnl_usd = gross_pnl_usd - total_cost_usd
    net_r = net_pnl_usd / r1_usd

    return CostBreakdown(
        notional_usd=notional_usd,
        fee_open_usd=fee_open_usd,
        fee_close_usd=fee_close_usd,
        slip_open_usd=slip_open_usd,
        slip_close_usd=slip_close_usd,
        funding_usd=funding_usd,
        total_cost_usd=total_cost_usd,
        initial_1r_pct=r1_pct,
        initial_1r_usd=r1_usd,
        gross_pnl_usd=gross_pnl_usd,
        net_pnl_usd=net_pnl_usd,
        net_r=net_r,
    )
