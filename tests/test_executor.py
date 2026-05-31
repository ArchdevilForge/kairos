"""Tests for TradeExecutor."""

import pytest
from unittest.mock import MagicMock, patch
from kairos.trades.executor import (
    OrderSide, OrderType, PositionSide, Order, OrderResult, TradeExecutor
)


class TestEnums:
    """Test enum classes."""
    
    def test_order_side(self):
        """Test OrderSide enum."""
        assert OrderSide.BUY == "buy"
        assert OrderSide.SELL == "sell"
    
    def test_order_type(self):
        """Test OrderType enum."""
        assert OrderType.MARKET == "market"
        assert OrderType.LIMIT == "limit"
        assert OrderType.STOP == "stop"
        assert OrderType.STOP_LIMIT == "stop_limit"
    
    def test_position_side(self):
        """Test PositionSide enum."""
        assert PositionSide.LONG == "long"
        assert PositionSide.SHORT == "short"


class TestOrder:
    """Test Order dataclass."""
    
    def test_init(self):
        """Test Order initialization."""
        order = Order(
            symbol="BTC/USDT",
            side=OrderSide.BUY,
            order_type=OrderType.MARKET,
            amount=0.001
        )
        assert order.symbol == "BTC/USDT"
        assert order.side == OrderSide.BUY
        assert order.order_type == OrderType.MARKET
        assert order.amount == 0.001
        assert order.price is None
        assert order.stop_price is None
        assert order.position_side == PositionSide.LONG
        assert order.leverage == 1
        assert order.reduce_only is False
        assert order.params == {}


class TestOrderResult:
    """Test OrderResult dataclass."""
    
    def test_init_success(self):
        """Test OrderResult initialization for success."""
        result = OrderResult(
            success=True,
            order_id="order_001",
            filled_price=50000.0
        )
        assert result.success is True
        assert result.order_id == "order_001"
        assert result.filled_price == 50000.0
    
    def test_init_failure(self):
        """Test OrderResult initialization for failure."""
        result = OrderResult(success=False)
        assert result.success is False
        assert result.order_id is None
        assert result.filled_price is None


class TestTradeExecutor:
    """Test TradeExecutor class."""
    
    @patch('kairos.trades.executor.ccxt')
    def test_init(self, mock_ccxt):
        """Test TradeExecutor initialization."""
        mock_exchange = MagicMock()
        mock_ccxt.okx.return_value = mock_exchange
        mock_ccxt.exchanges = ['okx']
        
        config = {"exchange": "okx", "apiKey": "test", "secret": "test"}
        executor = TradeExecutor("okx", config)
        assert executor.exchange_name == "okx"
        mock_ccxt.okx.assert_called_once()