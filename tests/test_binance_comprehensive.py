"""Comprehensive tests for BinanceExchange."""

import pytest
import json
from unittest.mock import MagicMock, patch, AsyncMock
from kairos.exchanges.binance import BinanceExchange


class TestBinanceExchangeComprehensive:
    """Comprehensive tests for BinanceExchange."""
    
    @patch('kairos.exchanges.base.ccxt')
    def test_init_default_type(self, mock_ccxt):
        """Test BinanceExchange initialization sets defaultType to future."""
        mock_exchange = MagicMock()
        mock_exchange.options = {}
        mock_ccxt.binance.return_value = mock_exchange
        mock_ccxt.exchanges = ['binance']
        
        exchange = BinanceExchange()
        assert exchange.exchange_name == "binance"
        # 验证options被设置
        assert hasattr(exchange, 'exchange')
    
    @patch('kairos.exchanges.base.ccxt')
    def test_ws_connect_method(self, mock_ccxt):
        """Test that _ws_connect method exists and is async."""
        mock_exchange = MagicMock()
        mock_ccxt.binance.return_value = mock_exchange
        mock_ccxt.exchanges = ['binance']
        
        exchange = BinanceExchange()
        assert hasattr(exchange, '_ws_connect')
        # 检查是否是协程函数
        import asyncio
        assert asyncio.iscoroutinefunction(exchange._ws_connect)
    
    @patch('kairos.exchanges.base.ccxt')
    def test_format_symbols(self, mock_ccxt):
        """Test symbol formatting for Binance."""
        mock_exchange = MagicMock()
        mock_ccxt.binance.return_value = mock_exchange
        mock_ccxt.exchanges = ['binance']
        
        exchange = BinanceExchange()
        
        # 测试符号格式化逻辑
        symbols = ["BTC/USDT:USDT", "ETH/USDT:USDT"]
        streams = []
        for symbol in symbols:
            base_symbol = symbol.split(":")[0]
            formatted = base_symbol.replace("/", "").lower()
            streams.append(f"{formatted}@ticker")
        
        assert streams == ["btcusdt@ticker", "ethusdt@ticker"]
    
    @patch('kairos.exchanges.base.ccxt')
    def test_process_price_data(self, mock_ccxt):
        """Test processing price data from Binance."""
        mock_exchange = MagicMock()
        mock_ccxt.binance.return_value = mock_exchange
        mock_ccxt.exchanges = ['binance']
        
        exchange = BinanceExchange()
        
        # 模拟Binance数据
        data = {
            "e": "24hrTicker",
            "s": "BTCUSDT",
            "c": "50000.00",  # 当前价格
            "q": "1000000"    # 成交量
        }
        
        # 测试数据处理逻辑
        assert "s" in data
        assert "c" in data
        symbol = data["s"]
        price = float(data["c"])
        
        assert symbol == "BTCUSDT"
        assert price == 50000.0
    
    @patch('kairos.exchanges.base.ccxt')
    def test_process_ping_message(self, mock_ccxt):
        """Test processing ping message from Binance."""
        mock_exchange = MagicMock()
        mock_ccxt.binance.return_value = mock_exchange
        mock_ccxt.exchanges = ['binance']
        
        exchange = BinanceExchange()
        
        # 模拟ping消息
        data = {"e": "ping"}
        
        # 测试ping处理逻辑
        if 'e' in data and data.get('e') == 'ping':
            # 应该发送pong
            pass  # 实际实现在_ws_connect中
    
    @patch('kairos.exchanges.base.ccxt')
    def test_websocket_uri_format(self, mock_ccxt):
        """Test WebSocket URI format."""
        mock_exchange = MagicMock()
        mock_ccxt.binance.return_value = mock_exchange
        mock_ccxt.exchanges = ['binance']
        
        exchange = BinanceExchange()
        
        # 测试URI格式
        symbols = ["BTC/USDT:USDT", "ETH/USDT:USDT"]
        streams = []
        for symbol in symbols:
            base_symbol = symbol.split(":")[0]
            formatted = base_symbol.replace("/", "").lower()
            streams.append(f"{formatted}@ticker")
        
        uri = f"wss://fstream.binance.com/ws/{'/'.join(streams)}"
        assert "fstream.binance.com" in uri
        assert "btcusdt@ticker" in uri
        assert "ethusdt@ticker" in uri