# pwatch

Cryptocurrency futures price monitor & trading system with Hermes Agent integration.

## Install

```bash
uv tool install git+https://github.com/Xeron2000/pwatch
```

## Usage

```bash
pwatch                                         # Start monitoring in background
pwatch run                                     # Run in foreground for debugging
pwatch status                                  # Show background process status
pwatch stop                                    # Stop background process
pwatch logs                                    # Print background log file
pwatch update-markets                          # Update market data
pwatch update-markets --exchanges okx binance  # Update specific exchanges
pwatch config-path                             # Show config directory
```

First run guides you through setup — you'll need a [Telegram Bot Token](https://t.me/botfather). By default `pwatch` starts in background; use `pwatch run` for foreground debugging.

## Config

Located at `~/.config/pwatch/config.yaml`:

```yaml
exchange: "okx"
defaultTimeframe: "5m"
checkInterval: "1m"
defaultThreshold: 1
notificationSymbols: "auto"  # top symbols after quality filters, refreshes every 4h
autoModeProfile: "conservative"
autoModeLimit: 40
autoModeMinQuoteVolume24h: 80000000      # filter out low-turnover symbols
autoModeMinOpenInterestUsd: 25000000     # avoid low-OI, easier-to-manipulate contracts
autoModeMinListingAgeDays: 45            # exclude very new listings
autoModeMaxRecentVolatilityPct: 6.0      # exclude ultra-wild symbols before they enter the pool

telegram:
  token: "your-bot-token"
  chatId: "your-chat-id"
```

## Trading System

pwatch includes a complete trading system based on Bit浪浪's trading philosophy.

### Quick Start

```bash
# Market analysis
pwatch cycle                    # Show market cycle phase
pwatch scan                     # Scan for trading symbols
pwatch box-detect --symbol BTC/USDT  # Detect box patterns
pwatch signal --symbol BTC/USDT      # Detect trading signals

# Position management
pwatch position status          # Show current positions
pwatch position size --capital 10000 --risk-pct 33 --leverage 5

# Funding rate arbitrage
pwatch funding                  # Show funding rates
pwatch funding --extreme        # Show extreme rates
pwatch arb status               # Show arbitrage status

# Risk management
pwatch risk status              # Show risk status
pwatch history                  # Show trading history
pwatch stats                    # Show trading statistics
```

### Hermes Agent Integration

pwatch includes a skill for Hermes Agent integration. Install it:

```bash
# Copy skill to Hermes Agent
mkdir -p ~/.hermes/skills/finance/bitlanglang-trading
cp -r skills/bitlanglang-trading/* ~/.hermes/skills/finance/bitlanglang-trading/

# Or install via Hermes Agent
hermes skills install local/path/to/skills/bitlanglang-trading
```

See `skills/bitlanglang-trading/SKILL.md` for complete documentation.

## License

MIT
