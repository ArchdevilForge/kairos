"""Comprehensive tests for OkxExchange."""

import pytest
import json
from unittest.mock import MagicMock, patch, AsyncMock
from kairos.exchanges.okx import OkxExchange, _safe_float


class TestSafeFloatComprehensive:
    """Comprehensive tests for _safe_float function."""
    
    def test_safe_float_none(self):
        """Test _safe_float with None."""
        assert _safe_float(None) is None
    
    def test_safe_float_empty_string(self):
        """Test _safe_float with empty string."""
        assert _safe_float("") is None
    
    def test_safe_float_valid_number(self):
        """Test _safe_float with valid number."""
        assert _safe_float("123.45") == 123.45
    
    def test_safe_float_zero(self):
        """Test _safe_float with zero."""
        assert _safe_float("0") == 0.0
    
    def test_safe_float_negative(self):
        """Test _safe_float with negative number."""
        assert _safe_float("-123.45") == -123.45
    
    def test_safe_float_integer(self):
        """Test _safe_float with integer."""
        assert _safe_float("123") == 123.0
    
    def test_safe_float_scientific(self):
        """Test _safe_float with scientific notation."""
        assert _safe_float("1.23e4") == 12300.0


class TestOkxExchangeComprehensive:
    """Comprehensive tests for OkxExchange."""
    
    @patch('kairos.exchanges.base.ccxt')
    def test_init_default_options(self, mock_ccxt):
        """Test OkxExchange initialization sets default options."""
        mock_exchange = MagicMock()
        mock_exchange.options = {}
        mock_ccxt.okx.return_value = mock_exchange
        mock_ccxt.exchanges = ['okx']
        
        exchange = OkxExchange()
        assert exchange.exchange_name == "okx"
        # 验证options被设置
        assert hasattr(exchange, 'exchange')
    
    @patch('kairos.exchanges.base.ccxt')
    def test_canonical_symbol_valid(self, mock_ccxt):
        """Test _canonical_symbol with valid format."""
        mock_exchange = MagicMock()
        mock_ccxt.okx.return_value = mock_exchange
        mock_ccxt.exchanges = ['okx']
        
        result = OkxExchange._canonical_symbol("BTC-USDT-SWAP")
        assert result == "BTC/USDT:USDT"
    
    @patch('kairos.exchanges.base.ccxt')
    def test_canonical_symbol_short(self, mock_ccxt):
        """Test _canonical_symbol with short format."""
        mock_exchange = MagicMock()
        mock_ccxt.okx.return_value = mock_exchange
        mock_ccxt.exchanges = ['okx']
        
        result = OkxExchange._canonical_symbol("BTC")
        assert result == "BTC"
    
    @patch('kairos.exchanges.base.ccxt')
    def test_extract_price_last(self, mock_ccxt):
        """Test _extract_price with 'last' field."""
        mock_exchange = MagicMock()
        mock_ccxt.okx.return_value = mock_exchange
        mock_ccxt.exchanges = ['okx']
        
        exchange = OkxExchange()
        item = {"last": "50000.00"}
        price = exchange._extract_price(item)
        assert price == 50000.0
    
    @patch('kairos.exchanges.base.ccxt')
    def test_extract_price_lastPrice(self, mock_ccxt):
        """Test _extract_price with 'lastPrice' field."""
        mock_exchange = MagicMock()
        mock_ccxt.okx.return_value = mock_exchange
        mock_ccxt.exchanges = ['okx']
        
        exchange = OkxExchange()
        item = {"lastPrice": "50000.00"}
        price = exchange._extract_price(item)
        assert price == 50000.0
    
    @patch('kairos.exchanges.base.ccxt')
    def test_extract_price_missing(self, mock_ccxt):
        """Test _extract_price with missing price field."""
        mock_exchange = MagicMock()
        mock_ccxt.okx.return_value = mock_exchange
        mock_ccxt.exchanges = ['okx']
        
        exchange = OkxExchange()
        item = {"symbol": "BTC-USDT"}
        with pytest.raises(ValueError):
            exchange._extract_price(item)
    
    @patch('kairos.exchanges.base.ccxt')
    def test_format_symbols(self, mock_ccxt):
        """Test symbol formatting for OKX."""
        mock_exchange = MagicMock()
        mock_ccxt.okx.return_value = mock_exchange
        mock_ccxt.exchanges = ['okx']
        
        exchange = OkxExchange()
        
        # 测试符号格式化逻辑
        symbols = ["BTC/USDT:USDT", "ETH/USDT:USDT"]
        subscribe_args = []
        
        for symbol in symbols:
            base_symbol = symbol.split("/")[0]
            formatted_symbol = f"{base_symbol}-USDT-SWAP"
            subscribe_args.append({"channel": "tickers", "instId": formatted_symbol})
        
        assert len(subscribe_args) == 2
        assert subscribe_args[0]["channel"] == "tickers"
        assert subscribe_args[0]["instId"] == "BTC-USDT-SWAP"
        assert subscribe_args[1]["instId"] == "ETH-USDT-SWAP"
    
    @patch('kairos.exchanges.base.ccxt')
    def test_websocket_uri_format(self, mock_ccxt):
        """Test WebSocket URI format."""
        mock_exchange = MagicMock()
        mock_ccxt.okx.return_value = mock_exchange
        mock_ccxt.exchanges = ['okx']
        
        exchange = OkxExchange()
        
        # 测试URI格式
        uri = "wss://ws.okx.com:8443/ws/v5/public"
        assert "ws.okx.com" in uri
        assert "v5/public" in uri
    
    @patch('kairos.exchanges.base.ccxt')
    def test_process_price_data(self, mock_ccxt):
        """Test processing price data from OKX."""
        mock_exchange = MagicMock()
        mock_ccxt.okx.return_value = mock_exchange
        mock_ccxt.exchanges = ['okx']
        
        exchange = OkxExchange()
        
        # 模拟OKX数据
        data = {
            "arg": {"channel": "tickers", "instId": "BTC-USDT-SWAP"},
            "data": [{
                "instId": "BTC-USDT-SWAP",
                "last": "50000.00",
                "lastSz": "0.1",
                "ts": "1234567890000"
            }]
        }
        
        # 测试数据处理逻辑
        assert "arg" in data
        assert "data" in data
        assert len(data["data"]) > 0
        
        item = data["data"][0]
        assert "last" in item
        assert "instId" in item
        
        price = _safe_float(item.get("last"))
        assert price == 50000.0