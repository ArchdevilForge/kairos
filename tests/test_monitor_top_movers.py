"""Tests for monitor_top_movers utility."""

import pytest
from unittest.mock import MagicMock, AsyncMock
from kairos.utils.monitor_top_movers import monitor_top_movers


class TestMonitorTopMovers:
    """Test monitor_top_movers function."""
    
    @pytest.mark.asyncio
    async def test_monitor_top_movers_success(self):
        """Test monitor_top_movers success."""
        # 模拟exchange
        exchange = MagicMock()
        exchange.get_price_minutes_ago = AsyncMock(return_value={
            "BTC/USDT": 50000.0,
            "ETH/USDT": 3000.0
        })
        exchange.get_current_prices = AsyncMock(return_value={
            "BTC/USDT": 52500.0,  # +5%
            "ETH/USDT": 3030.0   # +1%
        })
        
        config = {
            "priorityThresholds": {"high": 5.0, "medium": 2.0},
            "highPriorityBypassCooldown": True
        }
        
        result = await monitor_top_movers(
            minutes=5,
            symbols=["BTC/USDT", "ETH/USDT"],
            threshold=2.0,
            exchange=exchange,
            config=config
        )
        
        assert result is not None
        assert len(result) > 0
        assert result[0]["symbol"] == "BTC/USDT"
    
    @pytest.mark.asyncio
    async def test_monitor_top_movers_no_movers(self):
        """Test monitor_top_movers with no movers."""
        exchange = MagicMock()
        exchange.get_price_minutes_ago = AsyncMock(return_value={
            "BTC/USDT": 50000.0,
            "ETH/USDT": 3000.0
        })
        exchange.get_current_prices = AsyncMock(return_value={
            "BTC/USDT": 50100.0,  # +0.2%
            "ETH/USDT": 3003.0   # +0.1%
        })
        
        config = {
            "priorityThresholds": {"high": 5.0, "medium": 2.0},
            "highPriorityBypassCooldown": True
        }
        
        result = await monitor_top_movers(
            minutes=5,
            symbols=["BTC/USDT", "ETH/USDT"],
            threshold=2.0,
            exchange=exchange,
            config=config
        )
        
        assert result is None
    
    @pytest.mark.asyncio
    async def test_monitor_top_movers_invalid_exchange(self):
        """Test monitor_top_movers with invalid exchange."""
        exchange = MagicMock()
        del exchange.get_price_minutes_ago
        
        config = {}
        
        with pytest.raises(ValueError):
            await monitor_top_movers(
                minutes=5,
                symbols=["BTC/USDT"],
                threshold=2.0,
                exchange=exchange,
                config=config
            )