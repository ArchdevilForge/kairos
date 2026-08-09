from __future__ import annotations

from dataclasses import dataclass, field


@dataclass
class TemperatureBucket:
    market_id: str
    question: str
    temp_low: float
    temp_high: float
    temp_unit: str  # "C" or "F"
    yes_price: float
    no_price: float
    volume: float

    TAIL_BOUND = 999.0

    @property
    def is_low_tail(self) -> bool:
        return self.temp_low <= -self.TAIL_BOUND

    @property
    def is_high_tail(self) -> bool:
        return self.temp_high >= self.TAIL_BOUND

    @property
    def temp_low_c(self) -> float:
        if self.is_low_tail:
            return float("-inf")
        val = self.temp_low if self.temp_unit == "C" else (self.temp_low - 32) / 1.8
        return val

    @property
    def temp_high_c(self) -> float:
        if self.is_high_tail:
            return float("inf")
        val = self.temp_high if self.temp_unit == "C" else (self.temp_high - 32) / 1.8
        return val

    @property
    def temp_center_c(self) -> float:
        low = self.temp_low_c
        high = self.temp_high_c
        if low == float("-inf") and high == float("inf"):
            return 0.0
        if low == float("-inf"):
            return high
        if high == float("inf"):
            return low
        return (low + high) / 2


@dataclass
class WeatherEvent:
    event_id: str
    title: str
    slug: str
    city: str
    date: str
    measure_type: str = "high"
    airport_code: str | None = None
    buckets: list[TemperatureBucket] = field(default_factory=list)

    @property
    def sum_yes(self) -> float:
        return sum(b.yes_price for b in self.buckets)

    @property
    def sum_gap(self) -> float:
        return 1.0 - self.sum_yes


@dataclass
class ForecastResult:
    city: str
    date: str
    model: str
    temp_high_c: float
    measure_type: str = "high"
    members: list[float] = field(default_factory=list)

    @property
    def mean(self) -> float:
        if not self.members:
            return self.temp_high_c
        return sum(self.members) / len(self.members)

    @property
    def std(self) -> float:
        if len(self.members) < 2:
            return 0.0
        import numpy as np

        return float(np.std(self.members))


@dataclass
class Recommendation:
    strategy: str
    event: WeatherEvent
    bucket: TemperatureBucket
    direction: str  # "YES" or "NO"
    edge: float
    reasoning: str
    size_usd: float = 0.0
    kelly_fraction: float = 0.0

    @property
    def city(self) -> str:
        return self.event.city

    @property
    def temp_label(self) -> str:
        b = self.bucket
        low_c = b.temp_low_c
        high_c = b.temp_high_c
        if b.is_low_tail:
            return f"≤{high_c:.0f}°C"
        if b.is_high_tail:
            return f"≥{low_c:.0f}°C"
        if low_c == high_c:
            return f"{low_c:.0f}°C"
        return f"{low_c:.0f}-{high_c:.0f}°C"

    @property
    def price(self) -> float:
        return self.bucket.yes_price if self.direction == "YES" else self.bucket.no_price
