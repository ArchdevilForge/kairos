"""Tests for PriceDetector."""

import pytest
from unittest.mock import MagicMock
from kairos.detectors.price_detector import PriceDetector


class TestPriceDetector:
    """Test PriceDetector class."""
    
    def test_init(self):
        """Test PriceDetector initialization."""
        detector = PriceDetector()
        assert detector.callbacks == []
    
    def test_add_callback(self):
        """Test adding callback."""
        detector = PriceDetector()
        callback = MagicMock()
        
        detector.add_callback(callback)
        assert callback in detector.callbacks
    
    def test_on_price_update_calls_callbacks(self):
        """Test that price update calls all callbacks."""
        detector = PriceDetector()
        callback1 = MagicMock()
        callback2 = MagicMock()
        
        detector.add_callback(callback1)
        detector.add_callback(callback2)
        
        detector.on_price_update("BTC/USDT", 50000.0, 1234567890.0)
        
        callback1.assert_called_once_with("BTC/USDT", 50000.0)
        callback2.assert_called_once_with("BTC/USDT", 50000.0)
    
    def test_on_price_update_with_exception(self):
        """Test price update with callback exception."""
        detector = PriceDetector()
        callback1 = MagicMock(side_effect=Exception("Test error"))
        callback2 = MagicMock()
        
        detector.add_callback(callback1)
        detector.add_callback(callback2)
        
        # 不应该抛出异常
        detector.on_price_update("BTC/USDT", 50000.0, 1234567890.0)
        
        callback1.assert_called_once()
        callback2.assert_called_once()
    
    def test_on_volume_update_does_nothing(self):
        """Test that volume update does nothing."""
        detector = PriceDetector()
        callback = MagicMock()
        detector.add_callback(callback)
        
        detector.on_volume_update("BTC/USDT", 1000000.0, 1234567890.0)
        
        callback.assert_not_called()
    
    def test_multiple_price_updates(self):
        """Test multiple price updates."""
        detector = PriceDetector()
        callback = MagicMock()
        detector.add_callback(callback)
        
        detector.on_price_update("BTC/USDT", 50000.0, 1234567890.0)
        detector.on_price_update("ETH/USDT", 3000.0, 1234567891.0)
        
        assert callback.call_count == 2
    
    def test_callback_receives_correct_arguments(self):
        """Test callback receives correct arguments."""
        detector = PriceDetector()
        callback = MagicMock()
        detector.add_callback(callback)
        
        detector.on_price_update("SOL/USDT", 100.0, 1234567890.0)
        
        callback.assert_called_once_with("SOL/USDT", 100.0)