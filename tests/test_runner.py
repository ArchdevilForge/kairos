"""Tests for runner module."""

import pytest
from unittest.mock import MagicMock, patch, AsyncMock
from kairos.app.runner import main


class TestRunner:
    """Test runner module."""
    
    @pytest.mark.asyncio
    async def test_main_success(self):
        """Test main function success."""
        with patch('kairos.app.runner.PriceSentry') as mock_sentry, \
             patch('kairos.app.runner.TelegramBotService') as mock_bot_service, \
             patch('kairos.app.runner.setup_logging') as mock_logging:
            
            # 设置mock
            mock_sentry_instance = MagicMock()
            mock_sentry_instance.config = {"logLevel": "INFO", "telegram": {"token": "test"}}
            mock_sentry.return_value = mock_sentry_instance
            
            mock_bot_instance = AsyncMock()
            mock_bot_service.return_value = mock_bot_instance
            
            # 运行main
            await main()
            
            # 验证调用
            mock_sentry.assert_called_once()
            mock_logging.assert_called_once()
            mock_bot_instance.start.assert_called_once()
            mock_sentry_instance.run.assert_called_once()
            mock_bot_instance.stop.assert_called_once()
    
    @pytest.mark.asyncio
    async def test_main_with_exception(self):
        """Test main function with exception."""
        with patch('kairos.app.runner.PriceSentry') as mock_sentry, \
             patch('kairos.app.runner.TelegramBotService') as mock_bot_service, \
             patch('kairos.app.runner.setup_logging') as mock_logging:
            
            # 设置mock抛出异常
            mock_sentry.side_effect = Exception("Test error")
            
            # 运行main，不应该抛出异常
            await main()
            
            # 验证调用
            mock_sentry.assert_called_once()
    
    @pytest.mark.asyncio
    async def test_main_with_bot_stop_exception(self):
        """Test main function with bot stop exception."""
        with patch('kairos.app.runner.PriceSentry') as mock_sentry, \
             patch('kairos.app.runner.TelegramBotService') as mock_bot_service, \
             patch('kairos.app.runner.setup_logging') as mock_logging:
            
            # 设置mock
            mock_sentry_instance = MagicMock()
            mock_sentry_instance.config = {"logLevel": "INFO", "telegram": {"token": "test"}}
            mock_sentry.return_value = mock_sentry_instance
            
            mock_bot_instance = AsyncMock()
            mock_bot_instance.stop.side_effect = Exception("Stop error")
            mock_bot_service.return_value = mock_bot_instance
            
            # 运行main，不应该抛出异常
            await main()
            
            # 验证调用
            mock_bot_instance.stop.assert_called_once()