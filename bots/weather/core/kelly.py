from __future__ import annotations

import structlog

from pm_bot.models.market import Recommendation

log = structlog.get_logger()


def kelly_fraction(
    p_true: float,
    yes_price: float,
    direction: str = "YES",
    kelly_multiplier: float = 0.10,
) -> float:
    if direction == "YES":
        edge = p_true - yes_price
        payout_if_correct = 1.0 - yes_price
        if edge <= 0 or payout_if_correct <= 0:
            return 0.0
        full_kelly = edge / payout_if_correct
    else:
        no_price = 1.0 - yes_price
        edge = (1.0 - p_true) - no_price
        payout_if_correct = no_price
        if edge <= 0 or payout_if_correct <= 0:
            return 0.0
        full_kelly = edge / payout_if_correct

    return full_kelly * kelly_multiplier


def kelly_size(
    edge: float,
    yes_price: float,
    bankroll: float,
    kelly_fraction_val: float = 0.10,
    max_single: float = 50.0,
    strategy_kelly: float | None = None,
) -> float:
    """Calculate Kelly position size.

    Args:
        edge: Estimated edge (p_true - price)
        yes_price: Current YES price
        bankroll: Current bankroll
        kelly_fraction_val: Default Kelly fraction multiplier
        max_single: Maximum single position size in $
        strategy_kelly: Override Kelly fraction from strategy (for aggressive $100 mode)
    """
    # Use strategy's Kelly if provided (for aggressive $100 mode)
    kelly_mult = strategy_kelly if strategy_kelly is not None else kelly_fraction_val

    payout_if_correct = 1.0 - yes_price
    if edge <= 0 or payout_if_correct <= 0:
        return 0.0
    full_kelly = edge / payout_if_correct
    fraction_kelly = full_kelly * kelly_mult
    wager = bankroll * fraction_kelly
    wager = min(wager, max_single)
    notional = wager / yes_price if yes_price > 0 else 0.0
    return notional


def compute_kelly_for_recommendation(
    rec: Recommendation,
    bankroll: float,
    kelly_multiplier: float = 0.10,
    max_single: float = 50.0,
    max_daily: float = 200.0,
    daily_spent: float = 0.0,
    max_per_city: float = 100.0,
    city_spent: float = 0.0,
    max_total_pct: float = 0.30,
    total_exposure: float = 0.0,
    use_strategy_kelly: bool = False,
) -> Recommendation | None:
    """Compute Kelly sizing for a recommendation.

    Args:
        use_strategy_kelly: If True and strategy provided kelly_fraction, use it directly.
                          For aggressive $100 mode where strategies set their own sizing.
    """
    yes_price = rec.bucket.yes_price
    if yes_price <= 0:
        return None

    # If strategy already set size_usd and we're in strategy-kelly mode, use it
    if use_strategy_kelly and rec.size_usd > 0 and rec.kelly_fraction > 0:
        # Just apply risk limits
        size_usd = rec.size_usd
        size_usd = min(size_usd, max_single)
        size_usd = min(size_usd, max_daily - daily_spent)
        size_usd = min(size_usd, max_per_city - city_spent)
        max_total_exposure = bankroll * max_total_pct
        remaining_exposure = max_total_exposure - total_exposure
        size_usd = min(size_usd, remaining_exposure)

        if size_usd < 1.0:
            return None

        rec.size_usd = size_usd
        return rec

    # Original logic
    if rec.direction == "YES":
        p_true = yes_price + rec.edge
    else:
        p_true = 1.0 - (rec.bucket.no_price + rec.edge)

    p_true = max(0.0, min(1.0, p_true))
    frac = kelly_fraction(p_true, yes_price, rec.direction, kelly_multiplier)
    if frac <= 0:
        return None

    size_usd = bankroll * frac
    size_usd = min(size_usd, max_single)
    size_usd = min(size_usd, max_daily - daily_spent)
    size_usd = min(size_usd, max_per_city - city_spent)
    max_total_exposure = bankroll * max_total_pct
    remaining_exposure = max_total_exposure - total_exposure
    size_usd = min(size_usd, remaining_exposure)

    if size_usd < 1.0:
        return None

    rec.size_usd = size_usd
    rec.kelly_fraction = frac
    return rec
