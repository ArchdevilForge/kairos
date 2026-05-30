#!/bin/bash

# Test script for pwatch trading system

set -e

echo "🧪 Testing pwatch Trading System"
echo "================================="
echo

# Activate virtual environment
source .venv/bin/activate

echo "✅ Virtual environment activated"
echo

# Test imports
echo "📦 Testing imports..."
python -c "
from pwatch.analysis.box_pattern import BoxDetector
from pwatch.analysis.cycle import CycleDetector
from pwatch.analysis.support_resistance import SupportResistance
from pwatch.trades.position import PositionManager
from pwatch.trades.risk import RiskManager
from pwatch.arbitrage.funding_monitor import FundingRateMonitor
print('✅ All imports successful')
"
echo

# Test CLI commands
echo "🔧 Testing CLI commands..."
echo

echo "1. Market Cycle Analysis"
pwatch cycle
echo

echo "2. Symbol Scanning"
pwatch scan
echo

echo "3. Box Pattern Detection"
pwatch box-detect --symbol BTC/USDT
echo

echo "4. Trading Signal Detection"
pwatch signal --symbol BTC/USDT
echo

echo "5. Support/Resistance Levels"
pwatch sr --symbol BTC/USDT
echo

echo "6. Funding Rates"
pwatch funding status
echo

echo "7. Position Status"
pwatch position status
echo

echo "8. Risk Status"
pwatch risk status
echo

echo "================================="
echo "✅ All tests passed!"
echo
echo "📚 Documentation:"
echo "  - Main README: README.md"
echo "  - Trading Guide: TRADING_README.md"
echo "  - Skill: skills/bitlanglang-trading/SKILL.md"
echo
echo "🚀 Next steps:"
echo "  1. Configure exchange API keys in ~/.config/pwatch/trading.yaml"
echo "  2. Install skill in Hermes Agent"
echo "  3. Start trading with paper trading first"
echo
