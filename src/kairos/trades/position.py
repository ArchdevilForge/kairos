"""Position management and tracking."""

import json
import logging
import time
from dataclasses import asdict, dataclass, field
from enum import Enum
from typing import Optional

from kairos.paths import get_config_dir


class PositionStatus(str, Enum):
    OPEN = "open"
    CLOSED = "closed"
    PARTIAL = "partial"


@dataclass
class Position:
    """Represents a trading position."""
    id: str
    symbol: str
    side: str  # "long" or "short"
    entry_price: float
    amount: float
    leverage: int
    stop_loss: Optional[float] = None
    take_profit: Optional[float] = None
    entry_time: float = field(default_factory=time.time)
    exit_price: Optional[float] = None
    exit_time: Optional[float] = None
    pnl: Optional[float] = None
    pnl_percent: Optional[float] = None
    status: PositionStatus = PositionStatus.OPEN
    strategy: str = ""  # e.g., "box_breakout", "funding_arb", "trend_follow"
    notes: str = ""

    def to_dict(self) -> dict:
        """Convert to dictionary."""
        return asdict(self)

    @classmethod
    def from_dict(cls, data: dict) -> "Position":
        """Create from dictionary."""
        if "status" in data and isinstance(data["status"], str):
            data["status"] = PositionStatus(data["status"])
        return cls(**data)


class PositionManager:
    """Manages position tracking and persistence."""

    def __init__(self):
        self.logger = logging.getLogger("kairos.trades.position")
        self.positions_file = get_config_dir() / "positions.json"
        self.positions: dict[str, Position] = {}
        self._load_positions()

    def _load_positions(self):
        """Load positions from file."""
        if self.positions_file.exists():
            try:
                with open(self.positions_file, "r") as f:
                    data = json.load(f)
                for pos_id, pos_data in data.items():
                    self.positions[pos_id] = Position.from_dict(pos_data)
                self.logger.info(f"Loaded {len(self.positions)} positions")
            except Exception as e:
                self.logger.error(f"Failed to load positions: {e}")

    def _save_positions(self):
        """Save positions to file."""
        try:
            data = {pid: pos.to_dict() for pid, pos in self.positions.items()}
            with open(self.positions_file, "w") as f:
                json.dump(data, f, indent=2)
        except Exception as e:
            self.logger.error(f"Failed to save positions: {e}")

    def open_position(
        self,
        symbol: str,
        side: str,
        entry_price: float,
        amount: float,
        leverage: int,
        stop_loss: float = None,
        take_profit: float = None,
        strategy: str = "",
        notes: str = ""
    ) -> Position:
        """Open a new position."""
        pos_id = f"{symbol}_{side}_{int(time.time())}"
        position = Position(
            id=pos_id,
            symbol=symbol,
            side=side,
            entry_price=entry_price,
            amount=amount,
            leverage=leverage,
            stop_loss=stop_loss,
            take_profit=take_profit,
            strategy=strategy,
            notes=notes
        )
        self.positions[pos_id] = position
        self._save_positions()
        self.logger.info(f"Opened position {pos_id}: {side} {amount} {symbol} @ {entry_price}")
        return position

    def close_position(
        self,
        position_id: str,
        exit_price: float,
        pnl: float = None
    ) -> Optional[Position]:
        """Close a position."""
        position = self.positions.get(position_id)
        if not position:
            self.logger.error(f"Position {position_id} not found")
            return None

        position.exit_price = exit_price
        position.exit_time = time.time()
        position.status = PositionStatus.CLOSED

        if pnl is not None:
            position.pnl = pnl
        else:
            # Calculate PnL
            if position.side == "long":
                position.pnl = (exit_price - position.entry_price) * position.amount
            else:
                position.pnl = (position.entry_price - exit_price) * position.amount

        # Calculate PnL percentage
        position.pnl_percent = (position.pnl / (position.entry_price * position.amount)) * 100

        self._save_positions()
        self.logger.info(f"Closed position {position_id}: PnL={position.pnl:.2f} ({position.pnl_percent:.2f}%)")
        return position

    def get_open_positions(self, symbol: str = None) -> list[Position]:
        """Get all open positions, optionally filtered by symbol."""
        open_positions = [
            p for p in self.positions.values()
            if p.status == PositionStatus.OPEN
        ]
        if symbol:
            open_positions = [p for p in open_positions if p.symbol == symbol]
        return open_positions

    def get_position_history(self, symbol: str = None, limit: int = 50) -> list[Position]:
        """Get closed positions history."""
        closed = [
            p for p in self.positions.values()
            if p.status == PositionStatus.CLOSED
        ]
        if symbol:
            closed = [p for p in closed if p.symbol == symbol]

        # Sort by exit time, most recent first
        closed.sort(key=lambda p: p.exit_time or 0, reverse=True)
        return closed[:limit]

    def get_strategy_stats(self, strategy: str = None) -> dict:
        """Get statistics for a strategy."""
        positions = self.get_position_history()
        if strategy:
            positions = [p for p in positions if p.strategy == strategy]

        if not positions:
            return {"total": 0, "wins": 0, "losses": 0, "win_rate": 0, "total_pnl": 0}

        wins = [p for p in positions if p.pnl and p.pnl > 0]
        losses = [p for p in positions if p.pnl and p.pnl <= 0]
        total_pnl = sum(p.pnl for p in positions if p.pnl)

        return {
            "total": len(positions),
            "wins": len(wins),
            "losses": len(losses),
            "win_rate": len(wins) / len(positions) * 100 if positions else 0,
            "total_pnl": total_pnl,
            "avg_win": sum(p.pnl for p in wins) / len(wins) if wins else 0,
            "avg_loss": sum(p.pnl for p in losses) / len(losses) if losses else 0,
            "best_trade": max((p.pnl for p in positions if p.pnl), default=0),
            "worst_trade": min((p.pnl for p in positions if p.pnl), default=0)
        }
