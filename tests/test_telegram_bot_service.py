"""Tests for TelegramBotService."""

import pytest
from unittest.mock import MagicMock, patch, AsyncMock
from kairos.notifications.telegram_bot_service import TelegramBotService, WELCOME_MESSAGE, HELP_MESSAGE


class TestTelegramBotService:
    """Test TelegramBotService class."""
    
    def test_init_with_token(self):
        """Test TelegramBotService initialization with token."""
        service = TelegramBotService("test_token")
        assert service._token == "test_token"
        assert service._application is None
        assert service._running is False
    
    def test_init_without_token(self):
        """Test TelegramBotService initialization without token."""
        service = TelegramBotService(None)
        assert service._token == ""
        assert service._application is None
        assert service._running is False
    
    def test_welcome_message(self):
        """Test welcome message content."""
        assert "PriceSentry" in WELCOME_MESSAGE
        assert "通知机器人" in WELCOME_MESSAGE
    
    def test_help_message(self):
        """Test help message content."""
        assert "chatId" in HELP_MESSAGE
        assert "配置文件" in HELP_MESSAGE
    
    @pytest.mark.asyncio
    async def test_start_without_token(self):
        """Test start without token."""
        service = TelegramBotService(None)
        
        # 不应该抛出异常
        await service.start()
        
        assert service._running is False
    
    @pytest.mark.asyncio
    async def test_handle_start(self):
        """Test _handle_start handler."""
        service = TelegramBotService("test_token")
        
        # 模拟update和context
        update = MagicMock()
        update.message.reply_text = AsyncMock()
        context = MagicMock()
        context.bot.send_message = AsyncMock()
        
        await service._handle_start(update, context)
        
        context.bot.send_message.assert_called_once()
    
    @pytest.mark.asyncio
    async def test_handle_help(self):
        """Test _handle_help handler."""
        service = TelegramBotService("test_token")
        
        # 模拟update和context
        update = MagicMock()
        update.message.reply_text = AsyncMock()
        context = MagicMock()
        context.bot.send_message = AsyncMock()
        
        await service._handle_help(update, context)
        
        context.bot.send_message.assert_called_once()