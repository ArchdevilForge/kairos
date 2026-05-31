"""Tests for PositionManager."""

import pytest
from unittest.mock import MagicMock, patch
from kairos.trades.position import PositionStatus, Position, PositionManager


class TestPositionStatus:
    """Test PositionStatus enum."""
    
    def test_position_status_values(self):
        """Test PositionStatus enum values."""
        assert PositionStatus.OPEN == "open"
        assert PositionStatus.CLOSED == "closed"
        assert PositionStatus.PARTIAL == "partial"


class TestPosition:
    """Test Position dataclass."""
    
    def test_init(self):
        """Test Position initialization."""
        position = Position(
            id="pos_001",
            symbol="BTC/USDT",
            side="long",
            entry_price=50000.0,
            amount=0.001,
            leverage=5
        )
        assert position.id == "pos_001"
        assert position.symbol == "BTC/USDT"
        assert position.side == "long"
        assert position.entry_price == 50000.0
        assert position.amount == 0.001
        assert position.leverage == 5
        assert position.status == PositionStatus.OPEN
    
    def test_to_dict(self):
        """Test Position to_dict method."""
        position = Position(
            id="pos_001",
            symbol="BTC/USDT",
            side="long",
            entry_price=50000.0,
            amount=0.001,
            leverage=5
        )
        result = position.to_dict()
        assert result["id"] == "pos_001"
        assert result["symbol"] == "BTC/USDT"
        assert result["side"] == "long"
    
    def test_from_dict(self):
        """Test Position from_dict method."""
        data = {
            "id": "pos_001",
            "symbol": "BTC/USDT",
            "side": "long",
            "entry_price": 50000.0,
            "amount": 0.001,
            "leverage": 5,
            "status": "open"
        }
        position = Position.from_dict(data)
        assert position.id == "pos_001"
        assert position.symbol == "BTC/USDT"
        assert position.status == PositionStatus.OPEN


class TestPositionManager:
    """Test PositionManager class."""
    
    def test_init(self):
        """Test PositionManager initialization."""
        manager = PositionManager()
        assert manager is not None