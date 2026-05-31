"""Tests for trading CLI commands."""

import pytest
import sys
from unittest.mock import patch, MagicMock
from io import StringIO

# Add src to path
sys.path.insert(0, "/home/xeron/Coding/kairos/src")

from kairos.app.trade_cli import (
    cmd_cycle,
    cmd_scan,
    cmd_box_detect,
    cmd_signal,
    cmd_sr,
    cmd_funding_status,
    cmd_position_status,
)


class TestTradingCLI:
    """Test trading CLI commands."""
    
    def test_cmd_cycle(self, capsys):
        """Test market cycle command."""
        args = MagicMock()
        cmd_cycle(args)
        
        captured = capsys.readouterr()
        assert "Market Cycle Analysis" in captured.out
        assert "SPRING" in captured.out
    
    def test_cmd_scan(self, capsys):
        """Test scan command."""
        args = MagicMock()
        args.exchange = "okx"
        args.min_volume = None
        args.min_oi = None
        args.min_age = None
        args.max_volatility = None
        
        cmd_scan(args)
        
        captured = capsys.readouterr()
        assert "Scanning okx" in captured.out
        assert "SOL/USDT" in captured.out
    
    def test_cmd_box_detect(self, capsys):
        """Test box detection command."""
        args = MagicMock()
        args.symbol = "BTC/USDT"
        args.timeframe = "15m"
        args.lookback = 100
        
        cmd_box_detect(args)
        
        captured = capsys.readouterr()
        assert "Box Pattern Detection" in captured.out
        assert "CONVERGING" in captured.out
    
    def test_cmd_signal(self, capsys):
        """Test signal detection command."""
        args = MagicMock()
        args.symbol = "BTC/USDT"
        args.strategy = "box_breakout"
        
        from kairos.app.trade_cli import cmd_signal
        cmd_signal(args)
        
        captured = capsys.readouterr()
        assert "Trading Signal Detection" in captured.out
        assert "LONG" in captured.out
    
    def test_cmd_sr(self, capsys):
        """Test support/resistance command."""
        args = MagicMock()
        args.symbol = "BTC/USDT"
        
        cmd_sr(args)
        
        captured = capsys.readouterr()
        assert "Support & Resistance" in captured.out
        assert "Resistance" in captured.out
        assert "Support" in captured.out
    
    def test_cmd_funding_status(self, capsys):
        """Test funding status command."""
        args = MagicMock()
        args.exchange = "all"
        
        cmd_funding_status(args)
        
        captured = capsys.readouterr()
        assert "Funding Rates" in captured.out
        assert "BTC/USDT" in captured.out
    
    def test_cmd_position_status(self, capsys):
        """Test position status command."""
        args = MagicMock()
        
        cmd_position_status(args)
        
        captured = capsys.readouterr()
        assert "Position Status" in captured.out
        assert "Capital" in captured.out


class TestTradingAnalysis:
    """Test trading analysis modules."""
    
    def test_box_detector(self):
        """Test box pattern detection."""
        from kairos.analysis.box_pattern import BoxDetector
        import numpy as np
        
        detector = BoxDetector()
        
        # Create sample data
        highs = np.array([100, 102, 101, 103, 102, 104, 103, 105, 104, 106, 105, 107, 106, 108, 107])
        lows = np.array([95, 97, 96, 98, 97, 99, 98, 100, 99, 101, 100, 102, 101, 103, 102])
        closes = np.array([98, 100, 99, 101, 100, 102, 101, 103, 102, 104, 103, 105, 104, 106, 105])
        volumes = np.array([1000, 1200, 1100, 1300, 1200, 1400, 1300, 1500, 1400, 1600, 1500, 1700, 1600, 1800, 1700])
        timestamps = np.arange(15) * 60  # 1-minute intervals
        
        boxes = detector.detect("BTC/USDT", "1m", highs, lows, closes, volumes, timestamps)
        
        # Should detect some pattern
        assert isinstance(boxes, list)
    
    def test_cycle_detector(self):
        """Test market cycle detection."""
        from kairos.analysis.cycle import CycleDetector
        import numpy as np
        
        detector = CycleDetector()
        
        # Create sample BTC data (upward trend)
        prices = np.linspace(50000, 60000, 30)
        volumes = np.random.uniform(1000, 2000, 30)
        
        cycle = detector.detect_phase(prices, volumes)
        
        assert cycle.phase is not None
        assert cycle.confidence >= 0 and cycle.confidence <= 1
    
    def test_support_resistance(self):
        """Test support/resistance detection."""
        from kairos.analysis.support_resistance import SupportResistance
        import numpy as np
        
        sr = SupportResistance()
        
        # Create sample data
        highs = np.array([100, 102, 101, 103, 102, 104, 103, 105, 104, 106])
        lows = np.array([95, 97, 96, 98, 97, 99, 98, 100, 99, 101])
        closes = np.array([98, 100, 99, 101, 100, 102, 101, 103, 102, 104])
        volumes = np.array([1000, 1200, 1100, 1300, 1200, 1400, 1300, 1500, 1400, 1600])
        timestamps = np.arange(10) * 60
        
        levels = sr.find_levels("BTC/USDT", highs, lows, closes, volumes, timestamps, 102)
        
        assert "resistance_levels" in levels
        assert "support_levels" in levels


class TestTradingModules:
    """Test trading execution modules."""
    
    def test_position_manager(self):
        """Test position manager."""
        from kairos.trades.position import PositionManager
        
        pm = PositionManager()
        
        # Open a position
        pos = pm.open_position(
            symbol="BTC/USDT",
            side="long",
            entry_price=68000,
            amount=0.1,
            leverage=5,
            strategy="test"
        )
        
        assert pos.symbol == "BTC/USDT"
        assert pos.side == "long"
        
        # Get open positions
        open_pos = pm.get_open_positions()
        assert len(open_pos) >= 1
    
    def test_risk_manager(self):
        """Test risk manager."""
        from kairos.trades.risk import RiskManager, RiskConfig
        from kairos.trades.position import PositionManager
        
        pm = PositionManager()
        config = {"risk": {}}
        rm = RiskManager(config, pm)
        
        # Calculate position size
        result = rm.calculate_position_size(
            capital=10000,
            entry_price=68000,
            stop_loss=67200,
            leverage=5,
            is_btc=True
        )
        
        assert "position_size" in result
        assert "margin_required" in result


if __name__ == "__main__":
    pytest.main([__file__, "-v"])
