#!/usr/bin/env python3
"""Test MCP server tools."""

import asyncio
import sys
sys.path.insert(0, "src")

from kairos.mcp_server import mcp

async def test_tools():
    """Test MCP tools."""
    print("Testing MCP server...")
    
    # 列出所有工具
    tools = await mcp.list_tools()
    print(f"Available tools: {[tool.name for tool in tools]}")
    
    # 测试get_market_cycle工具
    print("\nTesting get_market_cycle...")
    result = await mcp.call_tool("get_market_cycle", {})
    print(f"Result: {result}")
    
    # 测试scan_symbols工具
    print("\nTesting scan_symbols...")
    result = await mcp.call_tool("scan_symbols", {"exchange": "okx"})
    print(f"Result: {result}")

if __name__ == "__main__":
    asyncio.run(test_tools())