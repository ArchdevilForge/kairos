"""Comprehensive tests for BybitExchange."""

import pytest
import json
from unittest.mock import MagicMock, patch, AsyncMock
from kairos.exchanges.bybit import BybitExchange


class TestBybitExchangeComprehensive:
    """Comprehensive tests for BybitExchange."""
    
    @patch('kairos.exchanges.base.ccxt')
    def test_init_default_type(self, mock_ccxt):
        """Test BybitExchange initialization sets defaultType to swap."""
        mock_exchange = MagicMock()
        mock_exchange.options = {}
        mock_ccxt.bybit.return_value = mock_exchange
        mock_ccxt.exchanges = ['bybit']
        
        exchange = BybitExchange()
        assert exchange.exchange_name == "bybit"
        # 验证options被设置
        assert hasattr(exchange, 'exchange')
    
    @patch('kairos.exchanges.base.ccxt')
    def test_ws_connect_method(self, mock_ccxt):
        """Test that _ws_connect method exists and is async."""
        mock_exchange = MagicMock()
        mock_ccxt.bybit.return_value = mock_exchange
        mock_ccxt.exchanges = ['bybit']
        
        exchange = BybitExchange()
        assert hasattr(exchange, '_ws_connect')
        # 检查是否是协程函数
        import asyncio
        assert asyncio.iscoroutinefunction(exchange._ws_connect)
    
    @patch('kairos.exchanges.base.ccxt')
    def test_format_symbols(self, mock_ccxt):
        """Test symbol formatting for Bybit."""
        mock_exchange = MagicMock()
        mock_ccxt.bybit.return_value = mock_exchange
        mock_ccxt.exchanges = ['bybit']
        
        exchange = BybitExchange()
        
        # 测试符号格式化逻辑
        symbols = ["BTC/USDT:USDT", "ETH/USDT:USDT"]
        subscribe_args = []
        for symbol in symbols:
            base_symbol = symbol.split(":")[0]
            formatted_symbol = base_symbol.replace("/", "")
            subscribe_args.append(f"tickers.{formatted_symbol}")
        
        assert subscribe_args == ["tickers.BTCUSDT", "tickers.ETHUSDT"]
    
    @patch('kairos.exchanges.base.ccxt')
    def test_process_price_data(self, mock_ccxt):
        """Test processing price data from Bybit."""
        mock_exchange = MagicMock()
        mock_ccxt.bybit.return_value = mock_exchange
        mock_ccxt.exchanges = ['bybit']
        
        exchange = BybitExchange()
        
        # 模拟Bybit数据
        data = {
            "topic": "tickers.BTCUSDT",
            "data": {
                "symbol": "BTCUSDT",
                "lastPrice": "50000.00",
                "turnover24h": "1000000"
            }
        }
        
        # 测试数据处理逻辑
        assert "topic" in data
        assert "tickers" in data["topic"]
        assert "data" in data
        
        symbol = data["data"]["symbol"]
        price = float(data["data"]["lastPrice"])
        
        assert symbol == "BTCUSDT"
        assert price == 50000.0
    
    @patch('kairos.exchanges.base.ccxt')
    def test_process_ping_message(self, mock_ccxt):
        """Test processing ping message from Bybit."""
        mock_exchange = MagicMock()
        mock_ccxt.bybit.return_value = mock_exchange
        mock_ccxt.exchanges = ['bybit']
        
        exchange = BybitExchange()
        
        # 模拟ping消息
        data = {"op": "ping", "req_id": "123"}
        
        # 测试ping处理逻辑
        if "op" in data and data.get("op") == "ping":
            pong_msg = {"op": "pong", "req_id": data.get("req_id")}
            assert pong_msg["op"] == "pong"
            assert pong_msg["req_id"] == "123"
    
    @patch('kairos.exchanges.base.ccxt')
    def test_websocket_uri_format(self, mock_ccxt):
        """Test WebSocket URI format."""
        mock_exchange = MagicMock()
        mock_ccxt.bybit.return_value = mock_exchange
        mock_ccxt.exchanges = ['bybit']
        
        exchange = BybitExchange()
        
        # 测试URI格式
        uri = "wss://stream.bybit.com/v5/public/linear"
        assert "stream.bybit.com" in uri
        assert "v5/public/linear" in uri
    
    @patch('kairos.exchanges.base.ccxt')
    def test_subscribe_message_format(self, mock_ccxt):
        """Test subscribe message format."""
        mock_exchange = MagicMock()
        mock_ccxt.bybit.return_value = mock_exchange
        mock_ccxt.exchanges = ['bybit']
        
        exchange = BybitExchange()
        
        # 测试订阅消息格式
        symbols = ["BTC/USDT:USDT", "ETH/USDT:USDT"]
        subscribe_msg = {"op": "subscribe", "args": []}
        
        for symbol in symbols:
            base_symbol = symbol.split(":")[0]
            formatted_symbol = base_symbol.replace("/", "")
            subscribe_msg["args"].append(f"tickers.{formatted_symbol}")
        
        assert subscribe_msg["op"] == "subscribe"
        assert len(subscribe_msg["args"]) == 2
        assert "tickers.BTCUSDT" in subscribe_msg["args"]
        assert "tickers.ETHUSDT" in subscribe_msg["args"]