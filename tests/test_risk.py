"""Tests for RiskManager."""

import pytest
from unittest.mock import MagicMock
from kairos.trades.risk import RiskConfig, RiskManager


class TestRiskConfig:
    """Test RiskConfig dataclass."""
    
    def test_default_values(self):
        """Test RiskConfig default values."""
        config = RiskConfig()
        assert config.max_position_size_pct == 0.33
        assert config.max_total_exposure_pct == 0.66
        assert config.max_leverage_btc == 10
        assert config.max_leverage_alt == 5
        assert config.max_drawdown_pct == 0.20
        assert config.max_daily_loss_pct == 0.10
        assert config.max_consecutive_losses == 3
        assert config.max_open_positions == 2
        assert config.min_risk_reward_ratio == 2.0
    
    def test_custom_values(self):
        """Test RiskConfig custom values."""
        config = RiskConfig(
            max_position_size_pct=0.5,
            max_leverage_btc=20,
            max_leverage_alt=10
        )
        assert config.max_position_size_pct == 0.5
        assert config.max_leverage_btc == 20
        assert config.max_leverage_alt == 10


class TestRiskManager:
    """Test RiskManager class."""
    
    def test_init(self):
        """Test RiskManager initialization."""
        config = {"risk": {"max_position_size_pct": 0.5}}
        position_manager = MagicMock()
        
        manager = RiskManager(config, position_manager)
        
        assert manager.config.max_position_size_pct == 0.5
        assert manager.position_manager == position_manager
        assert manager.daily_pnl == 0.0
        assert manager.consecutive_losses == 0
    
    def test_init_default_config(self):
        """Test RiskManager initialization with default config."""
        config = {}
        position_manager = MagicMock()
        
        manager = RiskManager(config, position_manager)
        
        assert manager.config.max_position_size_pct == 0.33
        assert manager.config.max_leverage_btc == 10
        assert manager.config.max_leverage_alt == 5
    
    def test_calculate_position_size_btc(self):
        """Test calculate_position_size for BTC."""
        config = {}
        position_manager = MagicMock()
        manager = RiskManager(config, position_manager)
        
        result = manager.calculate_position_size(
            capital=10000,
            entry_price=50000,
            stop_loss=49000,
            leverage=5,
            is_btc=True
        )
        
        assert "position_size" in result
        assert "risk_amount" in result
        assert "leverage" in result
    
    def test_calculate_position_size_alt(self):
        """Test calculate_position_size for altcoin."""
        config = {}
        position_manager = MagicMock()
        manager = RiskManager(config, position_manager)
        
        result = manager.calculate_position_size(
            capital=10000,
            entry_price=100,
            stop_loss=95,
            leverage=10,  # 应该被限制到5
            is_btc=False
        )
        
        assert result["leverage"] == 5