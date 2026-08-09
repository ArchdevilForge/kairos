from __future__ import annotations

from dataclasses import dataclass
from typing import Protocol, runtime_checkable

import structlog

log = structlog.get_logger()


@runtime_checkable
class RiskDB(Protocol):
    def get_daily_pnl(self) -> float: ...
    def get_consecutive_losses(self) -> int: ...
    def get_state(self, key: str) -> str | None: ...
    def set_state(self, key: str, value: str) -> None: ...
    def get_daily_spent(self) -> float: ...
    def get_city_spent(self, city: str) -> float: ...
    def get_total_exposure(self) -> float: ...


@dataclass
class RiskCheckResult:
    allowed: bool
    reason: str = ""
    kelly_adjustment: float = 1.0
    circuit_breaker_level: int = 0


class RiskManager:
    def __init__(
        self,
        db: RiskDB,
        bankroll: float = 500.0,
        circuit_breaker_l1: float = 0.05,
        circuit_breaker_l2: float = 0.10,
        circuit_breaker_l3: float = 0.15,
        no_new_before_resolution_h: int = 6,
        consecutive_loss_pause_count: int = 5,
        consecutive_loss_pause_minutes: int = 60,
        max_spread: float = 0.10,
        max_per_city: float = 100.0,
        max_total_pct: float = 0.30,
        max_daily: float = 200.0,
    ) -> None:
        self.db = db
        self.bankroll = bankroll
        self.circuit_breaker_l1 = circuit_breaker_l1
        self.circuit_breaker_l2 = circuit_breaker_l2
        self.circuit_breaker_l3 = circuit_breaker_l3
        self.no_new_before_resolution_h = no_new_before_resolution_h
        self.consecutive_loss_pause_count = consecutive_loss_pause_count
        self.consecutive_loss_pause_minutes = consecutive_loss_pause_minutes
        self.max_spread = max_spread
        self.max_per_city = max_per_city
        self.max_total_pct = max_total_pct
        self.max_daily = max_daily

    def check_circuit_breaker(self) -> RiskCheckResult:
        daily_pnl = self.db.get_daily_pnl()
        loss_pct = abs(min(daily_pnl, 0.0)) / max(self.bankroll, 1.0)

        if loss_pct >= self.circuit_breaker_l3:
            return RiskCheckResult(
                allowed=False,
                reason=f"L3 CIRCUIT BREAKER: Daily loss {loss_pct:.1%} >= {self.circuit_breaker_l3:.0%}",
                kelly_adjustment=0.0,
                circuit_breaker_level=3,
            )
        if loss_pct >= self.circuit_breaker_l2:
            return RiskCheckResult(
                allowed=True,
                reason=f"L2: Daily loss {loss_pct:.1%} >= {self.circuit_breaker_l2:.0%}, quarter Kelly",
                kelly_adjustment=0.25,
                circuit_breaker_level=2,
            )
        if loss_pct >= self.circuit_breaker_l1:
            return RiskCheckResult(
                allowed=True,
                reason=f"L1: Daily loss {loss_pct:.1%} >= {self.circuit_breaker_l1:.0%}, half Kelly",
                kelly_adjustment=0.5,
                circuit_breaker_level=1,
            )
        return RiskCheckResult(allowed=True, kelly_adjustment=1.0)

    def check_consecutive_losses(self) -> RiskCheckResult:
        count = self.db.get_consecutive_losses()
        if count >= self.consecutive_loss_pause_count:
            pause_key = "consecutive_loss_paused_until"
            paused = self.db.get_state(pause_key)
            if paused:
                from datetime import datetime, timezone

                try:
                    until = datetime.fromisoformat(paused)
                    if datetime.now(timezone.utc) < until:
                        return RiskCheckResult(
                            allowed=False,
                            reason=f"Consecutive {count} losses, paused until {paused}",
                            kelly_adjustment=0.0,
                        )
                except ValueError:
                    pass

            from datetime import datetime, timezone, timedelta

            until_str = (
                datetime.now(timezone.utc) + timedelta(minutes=self.consecutive_loss_pause_minutes)
            ).isoformat()
            self.db.set_state(pause_key, until_str)
            log.warning("consecutive_loss_pause", count=count, until=until_str)
            return RiskCheckResult(
                allowed=False,
                reason=f"Consecutive {count} losses, pausing {self.consecutive_loss_pause_minutes}min",
                kelly_adjustment=0.0,
            )
        return RiskCheckResult(allowed=True)

    def check_spread(self, yes_price: float, no_price: float) -> RiskCheckResult:
        spread = abs(yes_price + no_price - 1.0)
        if spread > self.max_spread:
            return RiskCheckResult(
                allowed=False,
                reason=f"Spread {spread:.2f} > max {self.max_spread:.2f}",
            )
        return RiskCheckResult(allowed=True)

    def check_time_risk(self, hours_to_resolution: float | None = None) -> RiskCheckResult:
        if hours_to_resolution is not None and hours_to_resolution < self.no_new_before_resolution_h:
            return RiskCheckResult(
                allowed=False,
                reason=f"Too close to resolution ({hours_to_resolution:.1f}h < {self.no_new_before_resolution_h}h)",
            )
        return RiskCheckResult(allowed=True)

    def check_daily_limit(self, amount_usd: float) -> RiskCheckResult:
        daily_spent = self.db.get_daily_spent()
        if daily_spent + amount_usd > self.max_daily:
            remaining = self.max_daily - daily_spent
            return RiskCheckResult(
                allowed=False,
                reason=f"Daily limit ${self.max_daily:.0f} reached (remaining: ${remaining:.2f})",
            )
        return RiskCheckResult(allowed=True)

    def check_city_limit(self, city: str, amount_usd: float) -> RiskCheckResult:
        city_spent = self.db.get_city_spent(city)
        if city_spent + amount_usd > self.max_per_city:
            remaining = self.max_per_city - city_spent
            return RiskCheckResult(
                allowed=False,
                reason=f"City {city} limit ${self.max_per_city:.0f} (remaining: ${remaining:.2f})",
            )
        return RiskCheckResult(allowed=True)

    def check_total_exposure(self, amount_usd: float) -> RiskCheckResult:
        total = self.db.get_total_exposure()
        max_total = self.bankroll * self.max_total_pct
        if total + amount_usd > max_total:
            remaining = max_total - total
            return RiskCheckResult(
                allowed=False,
                reason=f"Total exposure limit {self.max_total_pct:.0%} reached (remaining: ${remaining:.2f})",
            )
        return RiskCheckResult(allowed=True)

    def full_check(
        self,
        city: str,
        amount_usd: float,
        yes_price: float,
        no_price: float,
        hours_to_resolution: float | None = None,
    ) -> RiskCheckResult:
        results = [
            self.check_circuit_breaker(),
            self.check_consecutive_losses(),
            self.check_spread(yes_price, no_price),
            self.check_time_risk(hours_to_resolution),
            self.check_daily_limit(amount_usd),
            self.check_city_limit(city, amount_usd),
            self.check_total_exposure(amount_usd),
        ]

        kelly_adj = 1.0
        for r in results:
            if not r.allowed:
                return r
            kelly_adj = min(kelly_adj, r.kelly_adjustment)

        return RiskCheckResult(allowed=True, kelly_adjustment=kelly_adj)

    def daily_loss_pct(self) -> float:
        daily_pnl = self.db.get_daily_pnl()
        return abs(min(daily_pnl, 0.0)) / max(self.bankroll, 1.0)
