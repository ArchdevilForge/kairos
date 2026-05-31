"""Tests for BinanceExchange."""

import pytest
from unittest.mock import MagicMock, patch
from kairos.exchanges.binance import BinanceExchange


class TestBinanceExchange:
    """Test BinanceExchange class."""
    
    @patch('kairos.exchanges.base.ccxt')
    def test_init(self, mock_ccxt):
        """Test BinanceExchange initialization."""
        mock_exchange = MagicMock()
        mock_ccxt.binance.return_value = mock_exchange
        mock_ccxt.exchanges = ['binance']
        
        exchange = BinanceExchange()
        assert exchange.exchange_name == "binance"
        mock_ccxt.binance.assert_called_once()
    
    @patch('kairos.exchanges.base.ccxt')
    def test_ws_connect_method_exists(self, mock_ccxt):
        """Test that _ws_connect method exists."""
        mock_exchange = MagicMock()
        mock_ccxt.binance.return_value = mock_exchange
        mock_ccxt.exchanges = ['binance']
        
        exchange = BinanceExchange()
        assert hasattr(exchange, '_ws_connect')
        assert callable(exchange._ws_connect)