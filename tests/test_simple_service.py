"""Tests for SimpleDataService."""

import pytest
from unittest.mock import MagicMock
from kairos.data.simple_service import SimpleDataService, MarketData


class TestSimpleDataService:
    """Test SimpleDataService class."""
    
    def test_init(self):
        """Test SimpleDataService initialization."""
        service = SimpleDataService()
        assert len(service.mock_data) == 4
        assert "BTC/USDT" in service.mock_data
        assert "ETH/USDT" in service.mock_data
        assert "SOL/USDT" in service.mock_data
        assert "AVAX/USDT" in service.mock_data
    
    def test_get_price_btc(self):
        """Test getting BTC price."""
        service = SimpleDataService()
        price = service.get_price("BTC/USDT")
        assert price == 68500.0
    
    def test_get_price_eth(self):
        """Test getting ETH price."""
        service = SimpleDataService()
        price = service.get_price("ETH/USDT")
        assert price == 3850.0
    
    def test_get_price_sol(self):
        """Test getting SOL price."""
        service = SimpleDataService()
        price = service.get_price("SOL/USDT")
        assert price == 185.5
    
    def test_get_price_avax(self):
        """Test getting AVAX price."""
        service = SimpleDataService()
        price = service.get_price("AVAX/USDT")
        assert price == 42.8
    
    def test_get_price_nonexistent(self):
        """Test getting price for non-existent symbol."""
        service = SimpleDataService()
        price = service.get_price("DOGE/USDT")
        assert price is None
    
    def test_get_volume_btc(self):
        """Test getting BTC volume."""
        service = SimpleDataService()
        volume = service.get_volume("BTC/USDT")
        assert volume == 2500000000
    
    def test_get_volume_nonexistent(self):
        """Test getting volume for non-existent symbol."""
        service = SimpleDataService()
        volume = service.get_volume("DOGE/USDT")
        assert volume is None
    
    def test_get_funding_rate_btc(self):
        """Test getting BTC funding rate."""
        service = SimpleDataService()
        rate = service.get_funding_rate("BTC/USDT")
        assert rate == 0.012
    
    def test_get_funding_rate_nonexistent(self):
        """Test getting funding rate for non-existent symbol."""
        service = SimpleDataService()
        rate = service.get_funding_rate("DOGE/USDT")
        assert rate is None
    
    def test_get_open_interest_btc(self):
        """Test getting BTC open interest."""
        service = SimpleDataService()
        oi = service.get_open_interest("BTC/USDT")
        assert oi == 850000000
    
    def test_get_open_interest_nonexistent(self):
        """Test getting open interest for non-existent symbol."""
        service = SimpleDataService()
        oi = service.get_open_interest("DOGE/USDT")
        assert oi is None
    
    def test_get_market_data_btc(self):
        """Test getting BTC market data."""
        service = SimpleDataService()
        data = service.get_market_data("BTC/USDT")
        assert data is not None
        assert data.symbol == "BTC/USDT"
        assert data.price == 68500.0
        assert data.volume_24h == 2500000000
    
    def test_get_market_data_nonexistent(self):
        """Test getting market data for non-existent symbol."""
        service = SimpleDataService()
        data = service.get_market_data("DOGE/USDT")
        assert data is None
    
    def test_get_all_symbols(self):
        """Test getting all symbols."""
        service = SimpleDataService()
        symbols = service.get_all_symbols()
        assert len(symbols) == 4
        assert "BTC/USDT" in symbols
        assert "ETH/USDT" in symbols
        assert "SOL/USDT" in symbols
        assert "AVAX/USDT" in symbols
    
    def test_add_callback(self):
        """Test adding callback."""
        service = SimpleDataService()
        callback = MagicMock()
        service.add_callback(callback)
        assert callback in service.callbacks
    
    def test_update_price(self):
        """Test updating price."""
        service = SimpleDataService()
        callback = MagicMock()
        service.add_callback(callback)
        
        service.update_price("BTC/USDT", 70000.0)
        
        assert service.get_price("BTC/USDT") == 70000.0
        callback.assert_called_once()
    
    def test_update_price_nonexistent(self):
        """Test updating price for non-existent symbol."""
        service = SimpleDataService()
        callback = MagicMock()
        service.add_callback(callback)
        
        service.update_price("DOGE/USDT", 0.5)
        
        callback.assert_not_called()
    
    def test_update_price_multiple_callbacks(self):
        """Test updating price with multiple callbacks."""
        service = SimpleDataService()
        callback1 = MagicMock()
        callback2 = MagicMock()
        service.add_callback(callback1)
        service.add_callback(callback2)
        
        service.update_price("BTC/USDT", 70000.0)
        
        callback1.assert_called_once()
        callback2.assert_called_once()