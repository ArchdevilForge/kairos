"""Tests for OkxExchange."""

import pytest
from unittest.mock import MagicMock, patch
from kairos.exchanges.okx import OkxExchange, _safe_float


class TestSafeFloat:
    """Test _safe_float function."""
    
    def test_safe_float_none(self):
        """Test _safe_float with None."""
        assert _safe_float(None) is None
    
    def test_safe_float_empty(self):
        """Test _safe_float with empty string."""
        assert _safe_float("") is None
    
    def test_safe_float_valid(self):
        """Test _safe_float with valid number."""
        assert _safe_float("123.45") == 123.45
    
    def test_safe_float_zero(self):
        """Test _safe_float with zero."""
        assert _safe_float("0") == 0.0


class TestOkxExchange:
    """Test OkxExchange class."""
    
    @patch('kairos.exchanges.base.ccxt')
    def test_init(self, mock_ccxt):
        """Test OkxExchange initialization."""
        mock_exchange = MagicMock()
        mock_ccxt.okx.return_value = mock_exchange
        mock_ccxt.exchanges = ['okx']
        
        exchange = OkxExchange()
        assert exchange.exchange_name == "okx"
        mock_ccxt.okx.assert_called_once()
    
    @patch('kairos.exchanges.base.ccxt')
    def test_canonical_symbol(self, mock_ccxt):
        """Test _canonical_symbol static method."""
        mock_exchange = MagicMock()
        mock_ccxt.okx.return_value = mock_exchange
        mock_ccxt.exchanges = ['okx']
        
        result = OkxExchange._canonical_symbol("BTC-USDT-SWAP")
        assert result == "BTC/USDT:USDT"
    
    @patch('kairos.exchanges.base.ccxt')
    def test_canonical_symbol_invalid(self, mock_ccxt):
        """Test _canonical_symbol with invalid format."""
        mock_exchange = MagicMock()
        mock_ccxt.okx.return_value = mock_exchange
        mock_ccxt.exchanges = ['okx']
        
        result = OkxExchange._canonical_symbol("BTC")
        assert result == "BTC"