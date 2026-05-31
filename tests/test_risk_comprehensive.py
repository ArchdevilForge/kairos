"""Comprehensive tests for RiskManager."""

import pytest
from unittest.mock import MagicMock, patch
from kairos.trades.risk import RiskConfig, RiskManager


class TestRiskConfigComprehensive:
    """Comprehensive tests for RiskConfig dataclass."""
    
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
            max_total_exposure_pct=0.8,
            max_leverage_btc=20,
            max_leverage_alt=10,
            max_drawdown_pct=0.30,
            max_daily_loss_pct=0.15,
            max_consecutive_losses=5,
            max_open_positions=3,
            min_risk_reward_ratio=3.0
        )
        assert config.max_position_size_pct == 0.5
        assert config.max_total_exposure_pct == 0.8
        assert config.max_leverage_btc == 20
        assert config.max_leverage_alt == 10
        assert config.max_drawdown_pct == 0.30
        assert config.max_daily_loss_pct == 0.15
        assert config.max_consecutive_losses == 5
        assert config.max_open_positions == 3
        assert config.min_risk_reward_ratio == 3.0


class TestRiskManagerComprehensive:
    """Comprehensive tests for RiskManager class."""
    
    def test_init_with_config(self):
        """Test RiskManager initialization with config."""
        config = {
            "risk": {
                "max_position_size_pct": 0.5,
                "max_leverage_btc": 20,
                "max_leverage_alt": 10
            }
        }
        position_manager = MagicMock()
        
        manager = RiskManager(config, position_manager)
        
        assert manager.config.max_position_size_pct == 0.5
        assert manager.config.max_leverage_btc == 20
        assert manager.config.max_leverage_alt == 10
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
        assert "position_value" in result
        assert "margin_required" in result
        assert "leverage" in result
        assert "risk_amount" in result
        assert "risk_pct" in result
        
        # 验证计算
        risk_per_unit = abs(50000 - 49000)
        risk_amount = 10000 * 0.33
        position_size = risk_amount / risk_per_unit
        
        assert result["leverage"] == 5
        assert result["risk_amount"] == 3300.0
    
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
    
    def test_calculate_position_size_zero_risk(self):
        """Test calculate_position_size with zero risk per unit."""
        config = {}
        position_manager = MagicMock()
        manager = RiskManager(config, position_manager)
        
        result = manager.calculate_position_size(
            capital=10000,
            entry_price=50000,
            stop_loss=50000,  # 零风险
            leverage=5,
            is_btc=True
        )
        
        assert result["position_size"] == 0
        assert result["risk_amount"] == 3300.0
    
    def test_check_position_allowed_success(self):
        """Test check_position_allowed success."""
        config = {}
        position_manager = MagicMock()
        position_manager.get_open_positions.return_value = []
        
        manager = RiskManager(config, position_manager)
        manager.daily_pnl = 0.0
        manager.consecutive_losses = 0
        
        allowed, message = manager.check_position_allowed(
            capital=10000,
            symbol="BTC/USDT",
            position_value=5000
        )
        
        assert allowed is True
        assert message == "OK"
    
    def test_check_position_allowed_daily_loss(self):
        """Test check_position_allowed with daily loss limit."""
        config = {}
        position_manager = MagicMock()
        position_manager.get_open_positions.return_value = []
        
        manager = RiskManager(config, position_manager)
        manager.daily_pnl = -1500  # 超过10%的10000
        
        allowed, message = manager.check_position_allowed(
            capital=10000,
            symbol="BTC/USDT",
            position_value=5000
        )
        
        assert allowed is False
        assert "Daily loss limit" in message
    
    def test_check_position_allowed_consecutive_losses(self):
        """Test check_position_allowed with consecutive losses."""
        config = {}
        position_manager = MagicMock()
        position_manager.get_open_positions.return_value = []
        
        manager = RiskManager(config, position_manager)
        manager.consecutive_losses = 3
        
        allowed, message = manager.check_position_allowed(
            capital=10000,
            symbol="BTC/USDT",
            position_value=5000
        )
        
        assert allowed is False
        assert "Max consecutive losses" in message
    
    def test_check_position_allowed_total_exposure(self):
        """Test check_position_allowed with total exposure limit."""
        config = {}
        position_manager = MagicMock()
        
        # 模拟现有持仓
        mock_position = MagicMock()
        mock_position.entry_price = 50000
        mock_position.amount = 0.1
        position_manager.get_open_positions.return_value = [mock_position]
        
        manager = RiskManager(config, position_manager)
        
        # 现有持仓价值5000，新持仓5000，总计10000 > 10000 * 0.66 = 6600
        allowed, message = manager.check_position_allowed(
            capital=10000,
            symbol="BTC/USDT",
            position_value=5000
        )
        
        assert allowed is False
        assert "Total exposure limit" in message