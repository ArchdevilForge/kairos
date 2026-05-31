"""Tests for FundingArbitrage."""

import pytest
from unittest.mock import MagicMock
from kairos.arbitrage.funding_arb import ArbitragePosition, FundingArbitrage


class TestArbitragePosition:
    """Test ArbitragePosition dataclass."""
    
    def test_init(self):
        """Test ArbitragePosition initialization."""
        position = ArbitragePosition(
            id="arb_001",
            symbol="BTC/USDT",
            long_exchange="okx",
            short_exchange="bybit",
            long_position_id="pos_001",
            short_position_id="pos_002",
            entry_spread=0.05,
            entry_time=1234567890.0
        )
        assert position.id == "arb_001"
        assert position.symbol == "BTC/USDT"
        assert position.long_exchange == "okx"
        assert position.short_exchange == "bybit"
        assert position.long_position_id == "pos_001"
        assert position.short_position_id == "pos_002"
        assert position.entry_spread == 0.05
        assert position.entry_time == 1234567890.0
        assert position.status == "open"
        assert position.pnl == 0
        assert position.funding_collected == 0
    
    def test_default_values(self):
        """Test ArbitragePosition default values."""
        position = ArbitragePosition(
            id="arb_001",
            symbol="BTC/USDT",
            long_exchange="okx",
            short_exchange="bybit",
            long_position_id="pos_001",
            short_position_id="pos_002",
            entry_spread=0.05,
            entry_time=1234567890.0
        )
        assert position.status == "open"
        assert position.pnl == 0
        assert position.funding_collected == 0


class TestFundingArbitrage:
    """Test FundingArbitrage class."""
    
    def test_init(self):
        """Test FundingArbitrage initialization."""
        config = {
            "minSpreadPct": 0.05,
            "positionSizePct": 0.1,
            "maxPositions": 3,
            "closeSpreadPct": 0.01
        }
        executors = {"okx": MagicMock(), "bybit": MagicMock()}
        position_manager = MagicMock()
        funding_monitor = MagicMock()
        
        arb = FundingArbitrage(config, executors, position_manager, funding_monitor)
        
        assert arb.config == config
        assert arb.executors == executors
        assert arb.position_manager == position_manager
        assert arb.funding_monitor == funding_monitor
        assert arb.min_spread_pct == 0.05
        assert arb.position_size_pct == 0.1
        assert arb.max_positions == 3
        assert arb.close_spread_pct == 0.01