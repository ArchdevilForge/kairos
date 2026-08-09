#!/bin/bash
# PM-Bot 快速测试脚本

set -e

cd "$(dirname "$0")"

export PATH="$HOME/.local/bin:$PATH"

echo "=========================================="
echo "PM-Bot 快速测试"
echo "=========================================="
echo ""

echo "1. 检查安装..."
uv run pm-bot-v2 --help > /dev/null 2>&1 && echo "✓ CLI 正常" || echo "✗ CLI 异常"

echo "2. 检查钱包..."
uv run pm-bot-v2 wallet-list

echo "3. 扫描最近交易..."
uv run pm-bot-v2 wallet-scan --max-age 120 --min-size 5

echo ""
echo "=========================================="
echo "测试完成！"
echo "=========================================="
echo ""
echo "启动纸面交易: ./start.sh"
echo "查看钱包: pm-bot-v2 wallet-list"
echo "扫描交易: pm-bot-v2 wallet-scan"
