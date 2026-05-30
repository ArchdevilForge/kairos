# pwatch Trading System

## Overview

pwatch now includes a complete trading system based on Bit浪浪's trading philosophy, designed for integration with Hermes Agent.

## Features

### 1. Market Cycle Detection (春夏秋冬理论)
- Quantitative indicators for market phase detection
- BTC trend, volatility, volume, funding rates analysis
- Phase-specific trading advice

### 2. Box Pattern Detection
- Automatic box pattern recognition
- Convergence detection
- Breakout signal generation

### 3. Support & Resistance
- Pivot point detection
- Level clustering
- Round number identification

### 4. Position Management
- Fixed position sizing
- Risk management
- Trade history tracking

### 5. Funding Rate Arbitrage
- Cross-exchange rate monitoring
- Arbitrage opportunity detection
- Automated execution

### 6. Risk Management
- Position size limits
- Daily loss limits
- Consecutive loss tracking

## Quick Start

### Installation
```bash
# Clone repository
git clone https://github.com/Xeron2000/pwatch.git
cd pwatch

# Create virtual environment
uv venv
source .venv/bin/activate

# Install with dependencies
uv pip install -e .
```

### Configuration
Create `~/.config/pwatch/trading.yaml`:
```yaml
defaultExchange: "okx"
executionMode: "semi-auto"
capital: 10000
riskPerTradePct: 33
defaultLeverage: 5
```

### Basic Commands

#### Market Analysis
```bash
# Show market cycle
pwatch cycle

# Scan for trading symbols
pwatch scan --exchange okx --min-volume 80000000

# Detect box patterns
pwatch box-detect --symbol BTC/USDT --timeframe 15m

# Get trading signals
pwatch signal --symbol BTC/USDT --strategy box_breakout

# View support/resistance
pwatch sr --symbol BTC/USDT
```

#### Position Management
```bash
# View positions
pwatch position status

# Calculate position size
pwatch position size --capital 10000 --risk-pct 33 --leverage 5

# View history
pwatch position history --limit 20
```

#### Funding Rate Arbitrage
```bash
# View funding rates
pwatch funding status

# View extreme rates
pwatch funding extreme --threshold 0.05

# View opportunities
pwatch funding opportunities

# Execute arbitrage
pwatch arb execute --symbol SOL/USDT --size 1000
```

#### Risk Management
```bash
# View risk status
pwatch risk status

# Check trade risk
pwatch risk check --symbol BTC/USDT --size 5000
```

## Hermes Agent Integration

### Install Skill
```bash
# Copy skill to Hermes Agent
mkdir -p ~/.hermes/skills/finance/bitlanglang-trading
cp -r skills/bitlanglang-trading/* ~/.hermes/skills/finance/bitlanglang-trading/

# Or install via Hermes Agent
hermes skills install local/path/to/skills/bitlanglang-trading
```

### Using with Hermes Agent
Once the skill is installed, you can interact with the trading system through Hermes Agent:

```
# Market analysis
"Analyze the current market cycle"
"Scan for potential trading symbols on OKX"
"Detect box patterns for BTC/USDT"

# Trading signals
"What are the current trading signals for SOL/USDT?"
"Show me support and resistance levels for ETH/USDT"

# Position management
"What's my current position status?"
"Calculate position size for a $10,000 trade"

# Funding rate arbitrage
"Show me funding rate opportunities"
"Execute funding arbitrage for SOL/USDT"
```

## Trading Philosophy

### Core Principles
1. **顺势而为** (Follow the trend) - Never trade against the trend
2. **敬畏市场** (Respect the market) - The market is always right
3. **严格止损** (Strict stop-loss) - Stop-loss is your lifeline
4. **分仓管理** (Position splitting) - Divide capital into fixed portions
5. **低倍杠杆** (Low leverage) - BTC ≤ 10x, Altcoins ≤ 5x

### Market Phases
- **春天 (Spring)**: Market starts, begin building positions
- **夏天 (Summer)**: Main uptrend, aggressive trading on leaders
- **秋天 (Autumn)**: High-level consolidation, defensive posture
- **冬天 (Winter)**: Downtrend, stay out and wait

### Box Trading Strategy
1. **Box Breakout**: Enter on volume-confirmed breakout
2. **Box Bottom**: Enter at box bottom with stop below
3. **Avoid Middle**: Never trade in the middle of a box

## Architecture

### Modules
```
pwatch/
├── src/pwatch/
│   ├── trades/           # Trade execution
│   │   ├── executor.py   # Order execution
│   │   ├── position.py   # Position management
│   │   └── risk.py       # Risk management
│   ├── analysis/         # Technical analysis
│   │   ├── box_pattern.py # Box detection
│   │   ├── cycle.py      # Market cycle
│   │   └── support_resistance.py
│   └── arbitrage/        # Funding arbitrage
│       ├── funding_monitor.py
│       └── funding_arb.py
└── skills/
    └── bitlanglang-trading/
        ├── SKILL.md
        ├── references/
        └── templates/
```

### CLI Commands
- `pwatch cycle` - Market cycle analysis
- `pwatch scan` - Symbol scanning
- `pwatch box-detect` - Box pattern detection
- `pwatch signal` - Trading signals
- `pwatch sr` - Support/resistance levels
- `pwatch position` - Position management
- `pwatch funding` - Funding rates
- `pwatch arb` - Arbitrage operations
- `pwatch risk` - Risk management

## Risk Warnings

⚠️ **Important Disclaimers**

1. **Paper Trading First**: Test with paper trading before using real money
2. **Capital Risk**: Autonomous trading can lead to total loss
3. **Not Financial Advice**: This is a tool, not financial advice
4. **Regulatory**: Check local regulations for automated trading
5. **Security**: Never share API keys, use testnet first

## Development

### Running Tests
```bash
# Install dev dependencies
uv pip install -e ".[dev]"

# Run tests
python -m pytest tests/

# Run specific test
python -m pytest tests/test_trading_cli.py -v
```

### Adding New Features
1. Add module in `src/pwatch/`
2. Add CLI command in `src/pwatch/app/trade_cli.py`
3. Register command in `register_trading_commands()`
4. Update skill documentation

## License

MIT
