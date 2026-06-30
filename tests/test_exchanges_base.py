"""
Tests for exchanges/base.py - Base exchange functionality.
"""

from unittest.mock import Mock, patch

import pytest
import time

from kairos.exchanges.base import BaseExchange


class _TestExchangeImpl(BaseExchange):
    """Test implementation of BaseExchange for testing purposes."""

    def __init__(self, exchange_name):
        # Mock the abstract method implementation
        self._ws_connect_called = False
        super().__init__(exchange_name)

    async def _ws_connect(self, symbols):
        """Mock implementation for testing."""
        self._ws_connect_called = True
        self.ws_connected = True
        # Simulate some WebSocket data
        for symbol in symbols:
            self.last_prices[symbol] = 50000.0  # Mock price


class TestBaseExchange:
    """Test cases for TestExchangeImpl class."""

    def test_init_valid_exchange(self):
        """Test initialization with a valid exchange name."""
        with patch("kairos.exchanges.base.ccxt.exchanges", ["binance"]), patch(
            "kairos.exchanges.base.ccxt.binance"
        ) as mock_binance:
            mock_exchange = Mock()
            mock_binance.return_value = mock_exchange

            exchange = _TestExchangeImpl("binance")

            assert exchange.exchange_name == "binance"
            assert exchange.exchange == mock_exchange
            assert not exchange.ws_connected
            assert exchange.ws_connected is False
            assert exchange.running is False
            assert exchange.last_prices == {}

            # Verify exchange was initialized with rate limiting and bounded REST timeout
            mock_binance.assert_called_once_with({"enableRateLimit": True, "timeout": 8000})

    def test_init_invalid_exchange(self):
        """Test initialization with an invalid exchange name."""
        with patch("kairos.exchanges.base.ccxt.exchanges", ["binance"]):
            with pytest.raises(
                ValueError, match="Exchange invalid not supported by ccxt"
            ):
                _TestExchangeImpl("invalid")


    def test_close(self):
        """Test closing the exchange connection."""
        with patch("kairos.exchanges.base.ccxt.exchanges", ["binance"]), patch(
            "kairos.exchanges.base.ccxt.binance"
        ) as mock_binance:
            mock_exchange = Mock()
            mock_binance.return_value = mock_exchange

            exchange = _TestExchangeImpl("binance")
            exchange.running = True

            # Create a mock thread
            mock_thread = Mock()
            exchange.ws_thread = mock_thread

            exchange.close()

            assert not exchange.running
            # Verify thread was joined
            mock_thread.join.assert_called_once_with(timeout=5)
            # Verify ws_thread is set to None after closing
            assert exchange.ws_thread is None
            mock_exchange.close.assert_called_once()




    def test_start_websocket_timeout(self):
        """Test WebSocket startup timeout."""
        with patch("kairos.exchanges.base.ccxt.exchanges", ["binance"]), patch(
            "kairos.exchanges.base.ccxt.binance"
        ), patch("kairos.exchanges.base.threading.Thread") as mock_thread, patch(
            "kairos.exchanges.base.time.sleep"
        ), patch("kairos.exchanges.base.logging"), patch(
            "kairos.exchanges.base.time.time", side_effect=[0, 5, 10, 11, 12, 13, 15]
        ):  # Multiple calls for timeout (+buffer for circuit breaker time checks)
            exchange = _TestExchangeImpl("binance")

            # Mock thread creation
            mock_thread_instance = Mock()
            mock_thread.return_value = mock_thread_instance

            with pytest.raises(
                ConnectionError,
                match="WebSocket connection timeout",
            ):
                exchange.start_websocket(["BTC/USDT"])

    def test_stop_websocket(self):
        """Test stopping WebSocket connection."""
        with patch("kairos.exchanges.base.ccxt.exchanges", ["binance"]), patch(
            "kairos.exchanges.base.ccxt.binance"
        ), patch("kairos.exchanges.base.logging"):
            exchange = _TestExchangeImpl("binance")
            exchange.running = True

            # Create a mock thread and verify join is called
            mock_thread = Mock()
            exchange.ws_thread = mock_thread

            exchange.stop_websocket()

            assert not exchange.running
            # Verify join was called on the original thread
            mock_thread.join.assert_called_once_with(timeout=5)
            # Verify ws_thread is set to None after stopping
            assert exchange.ws_thread is None

