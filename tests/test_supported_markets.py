"""Tests for supported_markets utility."""

import pytest
from unittest.mock import MagicMock, patch
from kairos.utils.supported_markets import (
    SUPPORTED_MARKETS_PATH,
    DEFAULT_MARKETS,
    _read_supported_markets,
    list_cached_exchanges,
    refresh_supported_markets
)


class TestSupportedMarkets:
    """Test supported_markets functions."""
    
    def test_default_markets_structure(self):
        """Test DEFAULT_MARKETS has correct structure."""
        assert "okx" in DEFAULT_MARKETS
        assert "bybit" in DEFAULT_MARKETS
        assert "binance" in DEFAULT_MARKETS
        
        for exchange, symbols in DEFAULT_MARKETS.items():
            assert isinstance(symbols, list)
            assert len(symbols) > 0
            for symbol in symbols:
                assert isinstance(symbol, str)
    
    def test_read_supported_markets_no_file(self):
        """Test _read_supported_markets when no cache file exists."""
        with patch('kairos.utils.supported_markets.SUPPORTED_MARKETS_PATH') as mock_path:
            mock_path.exists.return_value = False
            result = _read_supported_markets()
            assert result == {}
    
    def test_read_supported_markets_with_file(self, tmp_path):
        """Test _read_supported_markets with cache file."""
        import json
        
        # 创建临时文件
        cache_file = tmp_path / "markets.json"
        test_data = {"okx": ["BTC/USDT:USDT", "ETH/USDT:USDT"]}
        cache_file.write_text(json.dumps(test_data))
        
        with patch('kairos.utils.supported_markets.SUPPORTED_MARKETS_PATH', cache_file):
            result = _read_supported_markets()
            assert result == test_data
    
    def test_list_cached_exchanges_no_cache(self):
        """Test list_cached_exchanges when no cache."""
        with patch('kairos.utils.supported_markets._read_supported_markets', return_value={}):
            result = list_cached_exchanges()
            assert result == []
    
    def test_list_cached_exchanges_with_cache(self):
        """Test list_cached_exchanges with cache."""
        test_data = {"okx": ["BTC/USDT:USDT"], "bybit": ["ETH/USDT"]}
        with patch('kairos.utils.supported_markets._read_supported_markets', return_value=test_data):
            result = list_cached_exchanges()
            assert set(result) == {"okx", "bybit"}


class TestRefreshSupportedMarkets:
    """Test refresh_supported_markets function."""
    
    @patch('kairos.utils.supported_markets._fetch_exchange_symbols')
    def test_refresh_supported_markets_success(self, mock_fetch, tmp_path):
        """Test refresh_supported_markets success."""
        mock_fetch.return_value = ["BTC/USDT:USDT", "ETH/USDT:USDT"]
        
        # 模拟路径
        cache_file = tmp_path / "markets.json"
        
        with patch('kairos.utils.supported_markets.SUPPORTED_MARKETS_PATH', cache_file):
            result = refresh_supported_markets(["okx"])
            
            assert "okx" in result
            assert len(result["okx"]) > 0
    
    @patch('kairos.utils.supported_markets._fetch_exchange_symbols')
    def test_refresh_supported_markets_exchange_error(self, mock_fetch):
        """Test refresh_supported_markets with exchange error."""
        mock_fetch.side_effect = Exception("Connection error")
        
        # 应该抛出异常
        with pytest.raises(Exception, match="Connection error"):
            refresh_supported_markets(["okx"])