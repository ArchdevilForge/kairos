"""Tests for load_symbols_from_file utility."""

import pytest
import tempfile
import os
from kairos.utils.load_symbols_from_file import load_symbols_from_file


class TestLoadSymbolsFromFile:
    """Test load_symbols_from_file function."""
    
    def test_load_symbols_success(self, tmp_path):
        """Test loading symbols from a valid file."""
        # 创建临时文件
        symbols_file = tmp_path / "symbols.txt"
        symbols_file.write_text("BTC/USDT\nETH/USDT\nSOL/USDT\n")
        
        result = load_symbols_from_file(str(symbols_file))
        assert result == ["BTC/USDT", "ETH/USDT", "SOL/USDT"]
    
    def test_load_symbols_with_empty_lines(self, tmp_path):
        """Test loading symbols with empty lines."""
        symbols_file = tmp_path / "symbols.txt"
        symbols_file.write_text("BTC/USDT\n\nETH/USDT\n\nSOL/USDT\n")
        
        result = load_symbols_from_file(str(symbols_file))
        assert result == ["BTC/USDT", "ETH/USDT", "SOL/USDT"]
    
    def test_load_symbols_with_whitespace(self, tmp_path):
        """Test loading symbols with whitespace."""
        symbols_file = tmp_path / "symbols.txt"
        symbols_file.write_text("  BTC/USDT  \n  ETH/USDT  \n  SOL/USDT  \n")
        
        result = load_symbols_from_file(str(symbols_file))
        assert result == ["BTC/USDT", "ETH/USDT", "SOL/USDT"]
    
    def test_load_symbols_file_not_found(self):
        """Test loading symbols from non-existent file."""
        result = load_symbols_from_file("non_existent_file.txt")
        assert result == []
    
    def test_load_symbols_empty_file(self, tmp_path):
        """Test loading symbols from empty file."""
        symbols_file = tmp_path / "empty.txt"
        symbols_file.write_text("")
        
        result = load_symbols_from_file(str(symbols_file))
        assert result == []
    
    def test_load_symbols_single_symbol(self, tmp_path):
        """Test loading single symbol."""
        symbols_file = tmp_path / "single.txt"
        symbols_file.write_text("BTC/USDT\n")
        
        result = load_symbols_from_file(str(symbols_file))
        assert result == ["BTC/USDT"]
    
    def test_load_symbols_no_newline(self, tmp_path):
        """Test loading symbols without trailing newline."""
        symbols_file = tmp_path / "no_newline.txt"
        symbols_file.write_text("BTC/USDT\nETH/USDT")
        
        result = load_symbols_from_file(str(symbols_file))
        assert result == ["BTC/USDT", "ETH/USDT"]