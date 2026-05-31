"""Tests for kairos.data module."""

import pytest
from kairos.data import DataManager, DataService, MarketData, ExchangeType


class TestDataModule:
    """Test data module imports."""
    
    def test_data_manager_import(self):
        """Test DataManager can be imported."""
        assert DataManager is not None
    
    def test_data_service_import(self):
        """Test DataService can be imported."""
        assert DataService is not None
    
    def test_market_data_import(self):
        """Test MarketData can be imported."""
        assert MarketData is not None
    
    def test_exchange_type_import(self):
        """Test ExchangeType can be imported."""
        assert ExchangeType is not None
    
    def test_exchange_type_values(self):
        """Test ExchangeType enum values."""
        assert ExchangeType.BINANCE == "binance"
        assert ExchangeType.OKX == "okx"
        assert ExchangeType.BYBIT == "bybit"
    
    def test_market_data_creation(self):
        """Test MarketData dataclass creation."""
        data = MarketData(
            symbol="BTC/USDT",
            exchange="okx",
            price=50000.0,
            volume_24h=1000000000,
            timestamp=1234567890
        )
        assert data.symbol == "BTC/USDT"
        assert data.exchange == "okx"
        assert data.price == 50000.0
        assert data.volume_24h == 1000000000
        assert data.timestamp == 1234567890
    
    def test_market_data_defaults(self):
        """Test MarketData default values."""
        data = MarketData(
            symbol="BTC/USDT",
            exchange="okx",
            price=50000.0,
            volume_24h=1000000000,
            timestamp=1234567890
        )
        assert data.bid == 0.0
        assert data.ask == 0.0
        assert data.high_24h == 0.0
        assert data.low_24h == 0.0
        assert data.change_24h == 0.0
        assert data.funding_rate == 0.0
        assert data.open_interest == 0.0