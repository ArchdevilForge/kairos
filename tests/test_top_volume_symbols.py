"""Tests for top_volume_symbols utility."""

import pytest
from unittest.mock import MagicMock, patch
from kairos.utils.top_volume_symbols import (
    fetch_top_volume_symbols,
    _normalize_filters,
    _create_exchange,
    _fetch_symbols_by_volume
)


class TestNormalizeFilters:
    """Test _normalize_filters function."""
    
    def test_empty_filters(self):
        """Test with empty filters."""
        result = _normalize_filters({})
        assert "minQuoteVolume24h" in result
        assert "minOpenInterestUsd" in result
    
    def test_with_filters(self):
        """Test with filters."""
        filters = {"minQuoteVolume24h": 1000000, "minOpenInterestUsd": 500000}
        result = _normalize_filters(filters)
        assert result["minQuoteVolume24h"] == 1000000
        assert result["minOpenInterestUsd"] == 500000


class TestCreateExchange:
    """Test _create_exchange function."""
    
    @patch('kairos.utils.top_volume_symbols.ccxt')
    def test_create_exchange_success(self, mock_ccxt):
        """Test _create_exchange success."""
        mock_exchange = MagicMock()
        mock_ccxt.okx.return_value = mock_exchange
        mock_ccxt.exchanges = ['okx']
        
        result = _create_exchange("okx")
        assert result == mock_exchange
        mock_ccxt.okx.assert_called_once()
    
    @patch('kairos.utils.top_volume_symbols.ccxt')
    def test_create_exchange_invalid(self, mock_ccxt):
        """Test _create_exchange with invalid exchange."""
        mock_ccxt.exchanges = ['okx']
        
        with pytest.raises(ValueError):
            _create_exchange("invalid_exchange")


class TestFetchTopVolumeSymbols:
    """Test fetch_top_volume_symbols function."""
    
    @patch('kairos.utils.top_volume_symbols._create_exchange')
    @patch('kairos.utils.top_volume_symbols._fetch_symbols_by_volume')
    def test_fetch_top_volume_symbols_success(self, mock_fetch, mock_create):
        """Test fetch_top_volume_symbols success."""
        mock_exchange = MagicMock()
        mock_create.return_value = mock_exchange
        mock_fetch.return_value = ["BTC/USDT", "ETH/USDT"]
        
        result = fetch_top_volume_symbols("okx", limit=2)
        
        assert result == ["BTC/USDT", "ETH/USDT"]
        mock_create.assert_called_once_with("okx")
        mock_fetch.assert_called_once()
    
    @patch('kairos.utils.top_volume_symbols._create_exchange')
    def test_fetch_top_volume_symbols_error(self, mock_create):
        """Test fetch_top_volume_symbols with error."""
        mock_create.side_effect = Exception("Connection error")
        
        result = fetch_top_volume_symbols("okx")
        
        assert result == []