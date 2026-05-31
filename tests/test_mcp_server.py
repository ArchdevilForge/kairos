"""Tests for MCP server."""

import pytest
import asyncio
from unittest.mock import MagicMock, patch
from kairos.mcp_server import mcp, get_market_cycle, detect_box_pattern, scan_symbols, detect_signal


class TestMCPServer:
    """Test MCP server tools."""
    
    def test_get_market_cycle(self):
        """Test get_market_cycle tool."""
        result = get_market_cycle()
        assert result["success"] is True
        assert "cycle" in result
        assert "phase" in result["cycle"]
        assert "confidence" in result["cycle"]
        assert "description" in result["cycle"]
    
    def test_detect_box_pattern(self):
        """Test detect_box_pattern tool."""
        with patch('kairos.mcp_server.data_service') as mock_service:
            mock_service.get_price.return_value = 68500.0
            result = detect_box_pattern("BTC/USDT")
            assert result["success"] is True
            assert "box_pattern" in result
            assert "detected" in result["box_pattern"]
            assert "high" in result["box_pattern"]
            assert "low" in result["box_pattern"]
    
    def test_scan_symbols(self):
        """Test scan_symbols tool."""
        result = scan_symbols()
        assert result["success"] is True
        assert "candidates" in result
        assert "summary" in result
    
    def test_detect_signal(self):
        """Test detect_signal tool."""
        with patch('kairos.mcp_server.data_service') as mock_service:
            mock_service.get_price.return_value = 68500.0
            mock_service.get_funding_rate.return_value = 0.012
            result = detect_signal("BTC/USDT")
            assert result["success"] is True
            assert "signal" in result
            assert "detected" in result["signal"]
            assert "direction" in result["signal"]
            assert "entry_price" in result["signal"]
    
    def test_get_market_cycle_with_mock_data(self):
        """Test get_market_cycle with mock data."""
        with patch('kairos.mcp_server.data_service') as mock_service:
            mock_service.get_price.return_value = 68500.0
            mock_service.get_volume.return_value = 2500000000
            mock_service.get_funding_rate.return_value = 0.012
            
            result = get_market_cycle()
            assert result["success"] is True
            assert result["cycle"]["indicators"]["btc_price"] == 68500.0
    
    def test_detect_box_pattern_with_mock_data(self):
        """Test detect_box_pattern with mock data."""
        with patch('kairos.mcp_server.data_service') as mock_service:
            mock_service.get_price.return_value = 68500.0
            
            result = detect_box_pattern("BTC/USDT")
            assert result["success"] is True
            assert result["box_pattern"]["high"] > result["box_pattern"]["low"]
    
    def test_scan_symbols_with_mock_data(self):
        """Test scan_symbols with mock data."""
        with patch('kairos.mcp_server.data_service') as mock_service:
            mock_service.get_all_symbols.return_value = ["BTC/USDT", "ETH/USDT"]
            mock_service.get_market_data.return_value = MagicMock(
                volume_24h=2500000000,
                open_interest=850000000,
                price=68500.0
            )
            
            result = scan_symbols()
            assert result["success"] is True
            assert len(result["candidates"]) > 0
    
    def test_detect_signal_with_mock_data(self):
        """Test detect_signal with mock data."""
        with patch('kairos.mcp_server.data_service') as mock_service:
            mock_service.get_price.return_value = 68500.0
            mock_service.get_funding_rate.return_value = 0.012
            
            result = detect_signal("BTC/USDT", "box_breakout")
            assert result["success"] is True
            assert result["signal"]["detected"] is True
            assert result["signal"]["direction"] == "long"