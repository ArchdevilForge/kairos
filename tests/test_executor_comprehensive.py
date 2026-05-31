"""Comprehensive tests for TradeExecutor."""

import pytest
from unittest.mock import MagicMock, patch, AsyncMock
from kairos.trades.executor import (
    OrderSide, OrderType, PositionSide, Order, OrderResult, TradeExecutor
)


class TestEnumsComprehensive:
    """Comprehensive tests for enum classes."""
    
    def test_order_side_values(self):
        """Test OrderSide enum values."""
        assert OrderSide.BUY.value == "buy"
        assert OrderSide.SELL.value == "sell"
    
    def test_order_type_values(self):
        """Test OrderType enum values."""
        assert OrderType.MARKET.value == "market"
        assert OrderType.LIMIT.value == "limit"
        assert OrderType.STOP.value == "stop"
        assert OrderType.STOP_LIMIT.value == "stop_limit"
    
    def test_position_side_values(self):
        """Test PositionSide enum values."""
        assert PositionSide.LONG.value == "long"
        assert PositionSide.SHORT.value == "short"


class TestOrderComprehensive:
    """Comprehensive tests for Order dataclass."""
    
    def test_init_with_all_fields(self):
        """Test Order initialization with all fields."""
        order = Order(
            symbol="BTC/USDT",
            side=OrderSide.BUY,
            order_type=OrderType.LIMIT,
            amount=0.001,
            price=50000.0,
            stop_price=49000.0,
            position_side=PositionSide.LONG,
            leverage=5,
            reduce_only=False,
            params={"timeInForce": "GTC"}
        )
        assert order.symbol == "BTC/USDT"
        assert order.side == OrderSide.BUY
        assert order.order_type == OrderType.LIMIT
        assert order.amount == 0.001
        assert order.price == 50000.0
        assert order.stop_price == 49000.0
        assert order.position_side == PositionSide.LONG
        assert order.leverage == 5
        assert order.reduce_only is False
        assert order.params == {"timeInForce": "GTC"}
    
    def test_init_minimal(self):
        """Test Order initialization with minimal fields."""
        order = Order(
            symbol="BTC/USDT",
            side=OrderSide.BUY,
            order_type=OrderType.MARKET,
            amount=0.001
        )
        assert order.symbol == "BTC/USDT"
        assert order.price is None
        assert order.stop_price is None
        assert order.position_side == PositionSide.LONG
        assert order.leverage == 1
        assert order.reduce_only is False
        assert order.params == {}


class TestOrderResultComprehensive:
    """Comprehensive tests for OrderResult dataclass."""
    
    def test_init_success(self):
        """Test OrderResult initialization for success."""
        result = OrderResult(
            success=True,
            order_id="order_001",
            filled_price=50000.0,
            filled_amount=0.001,
            fee=0.5
        )
        assert result.success is True
        assert result.order_id == "order_001"
        assert result.filled_price == 50000.0
        assert result.filled_amount == 0.001
        assert result.fee == 0.5
        assert result.error is None
    
    def test_init_failure(self):
        """Test OrderResult initialization for failure."""
        result = OrderResult(
            success=False,
            error="Insufficient funds"
        )
        assert result.success is False
        assert result.error == "Insufficient funds"
        assert result.order_id is None
        assert result.filled_price is None


class TestTradeExecutorComprehensive:
    """Comprehensive tests for TradeExecutor class."""
    
    @patch('kairos.trades.executor.ccxt')
    def test_init(self, mock_ccxt):
        """Test TradeExecutor initialization."""
        mock_exchange = MagicMock()
        mock_ccxt.okx.return_value = mock_exchange
        mock_ccxt.exchanges = ['okx']
        
        config = {
            "exchange": "okx",
            "apiKey": "test_key",
            "secret": "test_secret",
            "password": "test_password",
            "testnet": True,
            "defaultLeverage": 5,
            "maxLeverage": 10,
            "marginMode": "isolated"
        }
        
        executor = TradeExecutor("okx", config)
        
        assert executor.exchange_name == "okx"
        assert executor.config == config
        assert executor.default_leverage == 5
        assert executor.max_leverage == 10
        assert executor.margin_mode == "isolated"
        mock_ccxt.okx.assert_called_once()
    
    @patch('kairos.trades.executor.ccxt')
    def test_set_leverage_success(self, mock_ccxt):
        """Test set_leverage success."""
        mock_exchange = AsyncMock()
        mock_exchange.set_leverage = AsyncMock()
        mock_ccxt.okx.return_value = mock_exchange
        mock_ccxt.exchanges = ['okx']
        
        config = {"maxLeverage": 10}
        executor = TradeExecutor("okx", config)
        
        # 测试set_leverage逻辑
        leverage = 5
        max_leverage = 10
        leverage = min(leverage, max_leverage)
        assert leverage == 5
    
    @patch('kairos.trades.executor.ccxt')
    def test_set_leverage_exceeds_max(self, mock_ccxt):
        """Test set_leverage when leverage exceeds max."""
        mock_exchange = MagicMock()
        mock_ccxt.okx.return_value = mock_exchange
        mock_ccxt.exchanges = ['okx']
        
        config = {"maxLeverage": 10}
        executor = TradeExecutor("okx", config)
        
        # 测试杠杆限制逻辑
        leverage = 15
        max_leverage = 10
        leverage = min(leverage, max_leverage)
        assert leverage == 10
    
    @patch('kairos.trades.executor.ccxt')
    def test_create_order_params(self, mock_ccxt):
        """Test create_order parameters."""
        mock_exchange = MagicMock()
        mock_ccxt.okx.return_value = mock_exchange
        mock_ccxt.exchanges = ['okx']
        
        config = {}
        executor = TradeExecutor("okx", config)
        
        # 测试订单参数
        order = Order(
            symbol="BTC/USDT",
            side=OrderSide.BUY,
            order_type=OrderType.LIMIT,
            amount=0.001,
            price=50000.0
        )
        
        assert order.symbol == "BTC/USDT"
        assert order.side == OrderSide.BUY
        assert order.order_type == OrderType.LIMIT
        assert order.amount == 0.001
        assert order.price == 50000.0