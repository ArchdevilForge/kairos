"""Tests for DataManager and DataService."""

import pytest
import asyncio
from unittest.mock import MagicMock, patch
from kairos.data.data_manager import DataManager, DataService, MarketData, ExchangeType


class TestDataManager:
    """Test DataManager class."""
    
    def test_init(self):
        """Test DataManager initialization."""
        manager = DataManager()
        assert manager.exchanges == {}
        assert manager.market_data == {}
        assert manager.running is False
        assert manager.callbacks == []
    
    def test_add_callback(self):
        """Test adding callback."""
        manager = DataManager()
        callback = MagicMock()
        manager.add_callback(callback)
        assert callback in manager.callbacks
    
    def test_get_market_data_empty(self):
        """Test getting market data when empty."""
        manager = DataManager()
        result = manager.get_market_data("BTC/USDT")
        assert result is None
    
    def test_get_market_data_with_data(self):
        """Test getting market data with data."""
        manager = DataManager()
        data = MarketData(
            symbol="BTC/USDT",
            exchange="okx",
            price=50000.0,
            volume_24h=1000000000,
            timestamp=1234567890
        )
        manager.market_data["BTC/USDT"] = {"okx": data}
        
        result = manager.get_market_data("BTC/USDT")
        assert result == data
    
    def test_get_market_data_with_exchange(self):
        """Test getting market data for specific exchange."""
        manager = DataManager()
        data_okx = MarketData(
            symbol="BTC/USDT",
            exchange="okx",
            price=50000.0,
            volume_24h=1000000000,
            timestamp=1234567890
        )
        data_bybit = MarketData(
            symbol="BTC/USDT",
            exchange="bybit",
            price=50100.0,
            volume_24h=900000000,
            timestamp=1234567891
        )
        manager.market_data["BTC/USDT"] = {"okx": data_okx, "bybit": data_bybit}
        
        result = manager.get_market_data("BTC/USDT", ExchangeType.OKX)
        assert result == data_okx
        
        result = manager.get_market_data("BTC/USDT", ExchangeType.BYBIT)
        assert result == data_bybit
    
    def test_get_all_market_data(self):
        """Test getting all market data for a symbol."""
        manager = DataManager()
        data_okx = MarketData(
            symbol="BTC/USDT",
            exchange="okx",
            price=50000.0,
            volume_24h=1000000000,
            timestamp=1234567890
        )
        data_bybit = MarketData(
            symbol="BTC/USDT",
            exchange="bybit",
            price=50100.0,
            volume_24h=900000000,
            timestamp=1234567891
        )
        manager.market_data["BTC/USDT"] = {"okx": data_okx, "bybit": data_bybit}
        
        result = manager.get_all_market_data("BTC/USDT")
        assert len(result) == 2
        assert "okx" in result
        assert "bybit" in result
    
    def test_update_market_data(self):
        """Test updating market data."""
        manager = DataManager()
        callback = MagicMock()
        manager.add_callback(callback)
        
        manager._update_market_data("BTC/USDT:USDT", "okx", 50000.0)
        
        assert "BTC/USDT" in manager.market_data
        assert "okx" in manager.market_data["BTC/USDT"]
        assert manager.market_data["BTC/USDT"]["okx"].price == 50000.0
        callback.assert_called_once()
    
    def test_get_price(self):
        """Test getting price."""
        manager = DataManager()
        data = MarketData(
            symbol="BTC/USDT",
            exchange="okx",
            price=50000.0,
            volume_24h=1000000000,
            timestamp=1234567890
        )
        manager.market_data["BTC/USDT"] = {"okx": data}
        
        price = manager.get_price("BTC/USDT")
        assert price == 50000.0
    
    def test_get_volume(self):
        """Test getting volume."""
        manager = DataManager()
        data = MarketData(
            symbol="BTC/USDT",
            exchange="okx",
            price=50000.0,
            volume_24h=1000000000,
            timestamp=1234567890
        )
        manager.market_data["BTC/USDT"] = {"okx": data}
        
        volume = manager.get_volume("BTC/USDT")
        assert volume == 1000000000


class TestDataService:
    """Test DataService class."""
    
    def test_init(self):
        """Test DataService initialization."""
        service = DataService()
        assert service.data_manager is not None
        assert service.is_initialized is False
        assert service._cache == {}
        assert service._cache_ttl == 5
    
    def test_get_price_with_cache(self):
        """Test getting price with cache."""
        service = DataService()
        # 模拟data_manager.get_price
        service.data_manager.get_price = MagicMock(return_value=50000.0)
        
        # 第一次调用
        price1 = service.get_price("BTC/USDT")
        assert price1 == 50000.0
        
        # 第二次调用应该使用缓存
        price2 = service.get_price("BTC/USDT")
        assert price2 == 50000.0
        
        # data_manager.get_price应该只被调用一次
        service.data_manager.get_price.assert_called_once()
    
    def test_get_price_cache_expired(self):
        """Test getting price with expired cache."""
        service = DataService()
        service._cache_ttl = 0  # 立即过期
        service.data_manager.get_price = MagicMock(return_value=50000.0)
        
        # 第一次调用
        price1 = service.get_price("BTC/USDT")
        assert price1 == 50000.0
        
        # 第二次调用应该重新获取
        price2 = service.get_price("BTC/USDT")
        assert price2 == 50000.0
        
        # data_manager.get_price应该被调用两次
        assert service.data_manager.get_price.call_count == 2
    
    def test_get_price_no_data(self):
        """Test getting price when no data."""
        service = DataService()
        service.data_manager.get_price = MagicMock(return_value=None)
        
        price = service.get_price("BTC/USDT")
        assert price is None
    
    def test_add_callback(self):
        """Test adding callback."""
        service = DataService()
        callback = MagicMock()
        service.add_callback(callback)
        assert callback in service.data_manager.callbacks