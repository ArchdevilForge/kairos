#!/bin/bash
# PM-Bot 启动脚本
# 用法: ./start.sh [--live]

set -e

cd "$(dirname "$0")"

export PATH="$HOME/.local/bin:$PATH"

# Reduce glibc heap fragmentation on long-running daemon
export MALLOC_TRIM_THRESHOLD_=0

# 默认纸面交易
MODE="dry-run"
if [ "$1" = "--live" ]; then
    MODE="live"
    echo "⚠️  警告：实盘交易模式！"
    echo "按 Ctrl+C 取消，5秒后开始..."
    sleep 5
fi

echo "=========================================="
echo "PM-Bot 启动"
echo "=========================================="
echo "模式: $MODE"
echo "本金: \$100"
echo "城市: Chicago, Miami, Buenos Aires"
echo ""

if [ "$MODE" = "dry-run" ]; then
    echo "启动纸面交易..."
    uv run pm-bot daemon start \
        --dry-run \
        --cities 'Chicago,Miami,Buenos Aires' \
        2>&1 | tee -a ~/.pm-bot/daemon.log
else
    echo "启动实盘交易..."
    uv run pm-bot daemon start \
        --cities 'Chicago,Miami,Buenos Aires' \
        2>&1 | tee -a ~/.pm-bot/daemon.log
fi
