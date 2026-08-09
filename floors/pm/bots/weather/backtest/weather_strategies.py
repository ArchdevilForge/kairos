"""Polymarket Weather Trading Strategies.

Based on research of top traders:
- neobrother: laddering strategy, $20K+ profit, 2,373 predictions
- Hans323: tail betting, $1.127M profit
- gopfan2: simple price rules, $2M+ profit

Strategies:
1. LadderStrategy (neobrother): Buy adjacent 3-5 buckets at low prices
2. TailStrategy (Hans323): Buy underpriced tail buckets (<12¢)
3. Gopfan2Strategy: Buy YES <15¢ with model backing
"""

from __future__ import annotations

import json
from dataclasses import dataclass
from math import erf, sqrt
from pathlib import Path

import numpy as np

# ── EMOS Integration ──
EMOS_DIR = Path("data/emos_coeffs")


def load_emos_coeffs() -> dict[str, dict[str, float]]:
    """Load all trained EMOS coefficients from data/emos_coeffs/."""
    all_path = EMOS_DIR / "emos_all.json"
    if all_path.exists():
        return json.loads(all_path.read_text())
    return {}


def emos_calibrate(members: list[float], coeffs: dict[str, float]) -> tuple[float, float]:
    """Apply EMOS calibration to ensemble members.

    Returns (location, scale) for a Gaussian predictive distribution.
    """
    arr = np.array(members)
    raw_mean = float(np.mean(arr))
    raw_var = float(np.var(arr)) if len(arr) > 1 else 4.0

    a = coeffs.get("a", 0.0)
    b = coeffs.get("b", 1.0)
    c = coeffs.get("c", 4.0)
    d = coeffs.get("d", 0.5)

    location = a + b * raw_mean
    scale_sq = c + d * raw_var
    scale = max(0.5, np.sqrt(max(0.01, scale_sq)))

    return location, scale


def emos_bucket_prob(mu: float, sigma: float, bucket_low_c: float, bucket_high_c: float) -> float:
    """Compute bucket probability using EMOS-calibrated Gaussian CDF."""
    TAIL_BOUND = 999.0

    if bucket_high_c >= TAIL_BOUND:
        z = (bucket_low_c - mu) / sigma
        p = 0.5 * (1.0 - erf(z / sqrt(2)))
    elif bucket_low_c <= -TAIL_BOUND:
        z = (bucket_high_c - mu) / sigma
        p = 0.5 * (1.0 + erf(z / sqrt(2)))
    else:
        z_low = (bucket_low_c - mu) / sigma
        z_high = (bucket_high_c - mu) / sigma
        p = 0.5 * (erf(z_high / sqrt(2)) - erf(z_low / sqrt(2)))

    return max(0.0, min(1.0, p))


# ── Data Structures ──
@dataclass
class Bucket:
    """A single temperature bucket in a Polymarket market."""
    low_c: float
    high_c: float
    market_price: float
    true_prob: float = 0.0
    model_prob: float = 0.0
    volume: float = 0.0

    @property
    def center_c(self) -> float:
        if self.low_c <= -999:
            return self.high_c
        if self.high_c >= 999:
            return self.low_c
        return (self.low_c + self.high_c) / 2

    @property
    def label(self) -> str:
        if self.low_c <= -999:
            return f"≤{self.high_c:.0f}°C"
        if self.high_c >= 999:
            return f"≥{self.low_c:.0f}°C"
        return f"{self.low_c:.0f}-{self.high_c:.0f}°C"


@dataclass
class TradeSignal:
    """A trade signal from a strategy."""
    city: str
    date: str
    bucket: Bucket
    direction: str  # "YES" or "NO"
    edge: float
    model_prob: float
    strategy: str
    confidence: float = 1.0
    size_pct: float = 0.10  # fraction of bankroll


# ── Strategy Base ──
class WeatherStrategy:
    """Base class for weather trading strategies."""

    name: str = "base"
    description: str = ""

    def __init__(
        self,
        bankroll: float = 100.0,
        edge_threshold: float = 0.05,
        kelly_fraction: float = 0.25,
        max_single_pct: float = 0.15,
        max_total_pct: float = 0.60,
        min_notional: float = 1.0,
    ):
        self.bankroll = bankroll
        self.edge_threshold = edge_threshold
        self.kelly_fraction = kelly_fraction
        self.max_single_pct = max_single_pct
        self.max_total_pct = max_total_pct
        self.min_notional = min_notional

    def generate_signals(
        self,
        city: str,
        date: str,
        buckets: list[Bucket],
        model_mu: float,
        model_sigma: float,
        emos_coeffs: dict[str, float] | None = None,
        lead_time_hours: float = 48.0,
    ) -> list[TradeSignal]:
        """Generate trade signals for a city/date."""
        return []

    def compute_kelly_size(
        self,
        edge: float,
        market_price: float,
        model_prob: float,
        confidence: float = 1.0,
    ) -> float:
        """Compute Kelly position size as fraction of bankroll."""
        if edge <= 0 or market_price <= 0 or market_price >= 1:
            return 0.0

        payout = (1.0 - market_price) / market_price
        full_kelly = (model_prob * payout - (1.0 - model_prob)) / payout

        if full_kelly <= 0:
            return 0.0

        kelly_pct = full_kelly * self.kelly_fraction * confidence
        kelly_pct = min(kelly_pct, self.max_single_pct)
        return kelly_pct


# ── Strategy 1: Laddering (neobrother) ──
class LadderStrategy(WeatherStrategy):
    """neobrother's laddering strategy.

    Buy adjacent 3-5 buckets at low prices (2-15¢).
    If temperature lands in range, 1-2 buckets pay 6-50x, covering others' losses.

    Key: focus on the zone around the model's predicted temperature.
    """

    name = "ladder"
    description = "neobrother 梯度覆盖：同时买入相邻 3-5 个低价 bucket"

    def __init__(
        self,
        bankroll: float = 100.0,
        edge_threshold: float = 0.03,
        kelly_fraction: float = 0.15,
        max_single_pct: float = 0.08,
        max_total_pct: float = 0.40,
        max_ladder_buckets: int = 5,
        max_bucket_price: float = 0.20,
        **kwargs,
    ):
        super().__init__(bankroll, edge_threshold, kelly_fraction, max_single_pct, max_total_pct)
        self.max_ladder_buckets = max_ladder_buckets
        self.max_bucket_price = max_bucket_price

    def generate_signals(
        self,
        city: str,
        date: str,
        buckets: list[Bucket],
        model_mu: float,
        model_sigma: float,
        emos_coeffs: dict[str, float] | None = None,
        lead_time_hours: float = 48.0,
    ) -> list[TradeSignal]:
        signals = []

        bucket_probs = []
        for b in buckets:
            prob = emos_bucket_prob(model_mu, model_sigma, b.low_c, b.high_c)
            bucket_probs.append((b, prob))

        bucket_probs.sort(key=lambda x: x[1], reverse=True)

        if not bucket_probs:
            return signals

        best_bucket, best_prob = bucket_probs[0]
        best_center = best_bucket.center_c

        ladder_buckets = []
        for b, prob in bucket_probs:
            if abs(b.center_c - best_center) <= 6:
                edge = prob - b.market_price
                if edge > 0 and b.market_price <= self.max_bucket_price:
                    ladder_buckets.append((b, prob, edge))

        ladder_buckets = ladder_buckets[:self.max_ladder_buckets]

        for b, prob, edge in ladder_buckets:
            dist = abs(b.center_c - best_center)
            confidence = max(0.5, 1.0 - dist * 0.1)

            signals.append(TradeSignal(
                city=city,
                date=date,
                bucket=b,
                direction="YES",
                edge=edge,
                model_prob=prob,
                strategy=self.name,
                confidence=confidence,
                size_pct=self.max_single_pct / len(ladder_buckets),
            ))

        return signals


# ── Strategy 2: Tail Betting (Hans323) ──
class TailStrategy(WeatherStrategy):
    """Hans323's tail betting strategy.

    Buy underpriced tail buckets (<12¢) where model shows higher probability.
    High variance, high expected return. One hit can cover 10+ losses.

    Entry: market_price < 12¢, model_prob > 8%, edge > 5%
    """

    name = "tail"
    description = "Hans323 尾部押注：买入被严重低估的尾部 bucket"

    def __init__(
        self,
        bankroll: float = 100.0,
        edge_threshold: float = 0.05,
        kelly_fraction: float = 0.10,
        max_single_pct: float = 0.10,
        max_total_pct: float = 0.30,
        max_tail_price: float = 0.12,
        min_model_prob: float = 0.08,
        **kwargs,
    ):
        super().__init__(bankroll, edge_threshold, kelly_fraction, max_single_pct, max_total_pct)
        self.max_tail_price = max_tail_price
        self.min_model_prob = min_model_prob

    def generate_signals(
        self,
        city: str,
        date: str,
        buckets: list[Bucket],
        model_mu: float,
        model_sigma: float,
        emos_coeffs: dict[str, float] | None = None,
        lead_time_hours: float = 48.0,
    ) -> list[TradeSignal]:
        signals = []

        for b in buckets:
            if b.market_price > self.max_tail_price:
                continue

            prob = emos_bucket_prob(model_mu, model_sigma, b.low_c, b.high_c)

            if prob < self.min_model_prob:
                continue

            edge = prob - b.market_price
            if edge < self.edge_threshold:
                continue

            confidence = min(2.0, edge / self.edge_threshold)

            signals.append(TradeSignal(
                city=city,
                date=date,
                bucket=b,
                direction="YES",
                edge=edge,
                model_prob=prob,
                strategy=self.name,
                confidence=confidence,
                size_pct=self.max_single_pct,
            ))

        return signals


# ── Strategy 3: Gopfan2 Rules ──
class Gopfan2Strategy(WeatherStrategy):
    """gopfan2's simple price rules.

    Buy YES when price < 15¢ and model supports it.
    Small bets ($1-3), high frequency.

    Win rate: 50-80%, returns compound over thousands of trades.
    """

    name = "gopfan2"
    description = "gopfan2 规则：YES<15¢ 买入，模型支持"

    def __init__(
        self,
        bankroll: float = 100.0,
        edge_threshold: float = 0.05,
        kelly_fraction: float = 0.20,
        max_single_pct: float = 0.05,
        max_total_pct: float = 0.40,
        max_yes_price: float = 0.15,
        **kwargs,
    ):
        super().__init__(bankroll, edge_threshold, kelly_fraction, max_single_pct, max_total_pct)
        self.max_yes_price = max_yes_price

    def generate_signals(
        self,
        city: str,
        date: str,
        buckets: list[Bucket],
        model_mu: float,
        model_sigma: float,
        emos_coeffs: dict[str, float] | None = None,
        lead_time_hours: float = 48.0,
    ) -> list[TradeSignal]:
        signals = []

        for b in buckets:
            prob = emos_bucket_prob(model_mu, model_sigma, b.low_c, b.high_c)

            # Buy YES when price < 15¢ and model says higher
            if b.market_price <= self.max_yes_price:
                edge = prob - b.market_price
                if edge >= self.edge_threshold and prob > b.market_price * 1.5:
                    confidence = min(2.0, prob / max(b.market_price, 0.01))
                    signals.append(TradeSignal(
                        city=city, date=date, bucket=b,
                        direction="YES", edge=edge, model_prob=prob,
                        strategy=self.name, confidence=confidence,
                        size_pct=self.max_single_pct,
                    ))

        return signals


# ── Combined Strategy ──
class CombinedStrategy(WeatherStrategy):
    """Combines Ladder + Tail + Gopfan2 with configurable weights.

    All sub-strategies buy <20¢, ensuring liquidity safety.
    """

    name = "combined"
    description = "组合策略：梯度(40%) + 尾部(30%) + gopfan2(30%)"

    def __init__(
        self,
        bankroll: float = 100.0,
        edge_threshold: float = 0.04,
        kelly_fraction: float = 0.20,
        max_single_pct: float = 0.12,
        max_total_pct: float = 0.60,
        strategy_weights: dict[str, float] | None = None,
        **kwargs,
    ):
        super().__init__(bankroll, edge_threshold, kelly_fraction, max_single_pct, max_total_pct)

        self.sub_strategies = {
            "ladder": LadderStrategy(bankroll, 0.03, 0.15, 0.08, 0.40),
            "tail": TailStrategy(bankroll, 0.05, 0.10, 0.10, 0.30),
            "gopfan2": Gopfan2Strategy(bankroll, 0.05, 0.20, 0.05, 0.40),
        }

        self.strategy_weights = strategy_weights or {
            "ladder": 0.40,
            "tail": 0.30,
            "gopfan2": 0.30,
        }

    def generate_signals(
        self,
        city: str,
        date: str,
        buckets: list[Bucket],
        model_mu: float,
        model_sigma: float,
        emos_coeffs: dict[str, float] | None = None,
        lead_time_hours: float = 48.0,
    ) -> list[TradeSignal]:
        all_signals = []

        for strat_name, strat in self.sub_strategies.items():
            weight = self.strategy_weights.get(strat_name, 0.0)
            if weight <= 0:
                continue

            signals = strat.generate_signals(
                city, date, buckets, model_mu, model_sigma,
                emos_coeffs, lead_time_hours,
            )

            for s in signals:
                s.size_pct *= weight
                s.strategy = f"{self.name}_{strat_name}"
                all_signals.append(s)

        # Deduplicate: if multiple strategies signal same bucket, keep highest edge
        bucket_signals: dict[tuple[float, float], TradeSignal] = {}
        for s in all_signals:
            key = (s.bucket.low_c, s.bucket.high_c, s.direction)
            if key in bucket_signals:
                if s.edge > bucket_signals[key].edge:
                    bucket_signals[key] = s
            else:
                bucket_signals[key] = s

        return list(bucket_signals.values())


# ── Factory ──
def get_all_strategies() -> dict[str, WeatherStrategy]:
    """Return all available strategies."""
    return {
        "ladder": LadderStrategy(),
        "tail": TailStrategy(),
        "gopfan2": Gopfan2Strategy(),
        "combined": CombinedStrategy(),
    }
