"""Tests for BybitExchange."""

import pytest
from unittest.mock import MagicMock, patch
from kairos.exchanges.bybit import BybitExchange


class TestBybitExchange:
    """Test BybitExchange class."""
    
    @patch('kairos.exchanges.base.ccxt')
    def test_init(self, mock_ccxt):
        """Test BybitExchange initialization."""
        mock_exchange = MagicMock()
        mock_ccxt.bybit.return_value = mock_exchange
        mock_ccxt.exchanges = ['bybit']
        
        exchange = BybitExchange()
        assert exchange.exchange_name == "bybit"
        mock_ccxt.bybit.assert_called_once()
    
    @patch('kairos.exchanges.base.ccxt')
    def test_ws_connect_method_exists(self, mock_ccxt):
        """Test that _ws_connect method exists."""
        mock_exchange = MagicMock()
        mock_ccxt.bybit.return_value = mock_exchange
        mock_ccxt.exchanges = ['bybit']
        
        exchange = BybitExchange()
        assert hasattr(exchange, '_ws_connect')
        assert callable(exchange._ws_connect)