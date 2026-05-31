"""Tests for setup_logging utility."""

import pytest
import logging
from unittest.mock import MagicMock, patch
from kairos.utils.setup_logging import setup_logging


class TestSetupLogging:
    """Test setup_logging function."""
    
    def test_setup_logging_default(self):
        """Test setup_logging with default parameters."""
        # 清除现有的handlers
        logger = logging.getLogger()
        original_handlers = logger.handlers.copy()
        logger.handlers.clear()
        
        try:
            with patch('kairos.utils.setup_logging.get_log_path') as mock_path:
                mock_path.return_value = "/tmp/test.log"
                
                setup_logging()
                
                assert logger.level == logging.INFO
                assert len(logger.handlers) >= 1
        finally:
            # 恢复原始handlers
            logger.handlers = original_handlers
    
    def test_setup_logging_with_console(self):
        """Test setup_logging with console output."""
        logger = logging.getLogger()
        original_handlers = logger.handlers.copy()
        logger.handlers.clear()
        
        try:
            with patch('kairos.utils.setup_logging.get_log_path') as mock_path:
                mock_path.return_value = "/tmp/test.log"
                
                setup_logging(console=True)
                
                assert logger.level == logging.INFO
                assert len(logger.handlers) >= 2  # 文件handler + 控制台handler
        finally:
            logger.handlers = original_handlers
    
    def test_setup_logging_without_console(self):
        """Test setup_logging without console output."""
        logger = logging.getLogger()
        original_handlers = logger.handlers.copy()
        logger.handlers.clear()
        
        try:
            with patch('kairos.utils.setup_logging.get_log_path') as mock_path:
                mock_path.return_value = "/tmp/test.log"
                
                setup_logging(console=False)
                
                assert logger.level == logging.INFO
                assert len(logger.handlers) >= 1  # 只有文件handler
        finally:
            logger.handlers = original_handlers
    
    def test_setup_logging_custom_level(self):
        """Test setup_logging with custom log level."""
        logger = logging.getLogger()
        original_handlers = logger.handlers.copy()
        logger.handlers.clear()
        
        try:
            with patch('kairos.utils.setup_logging.get_log_path') as mock_path:
                mock_path.return_value = "/tmp/test.log"
                
                setup_logging(log_level="DEBUG")
                
                assert logger.level == logging.DEBUG
        finally:
            logger.handlers = original_handlers
    
    def test_setup_logging_with_existing_handlers(self):
        """Test setup_logging with existing handlers."""
        logger = logging.getLogger()
        original_handlers = logger.handlers.copy()
        
        # 添加一个handler
        test_handler = logging.StreamHandler()
        logger.addHandler(test_handler)
        
        try:
            with patch('kairos.utils.setup_logging.get_log_path') as mock_path:
                mock_path.return_value = "/tmp/test.log"
                
                # 应该直接返回，不添加新handlers
                setup_logging()
                
                # 验证没有添加新handlers
                assert len(logger.handlers) == len(original_handlers) + 1
        finally:
            logger.handlers = original_handlers