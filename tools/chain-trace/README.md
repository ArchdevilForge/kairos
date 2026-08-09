# chain-trace

Zero-key crypto forensics for ETH / Base / BSC / Solana.

## Features

- Multi-chain support (ETH, Base, BSC, Solana)
- Zero API keys required
- Automated suspicious holder detection
- Holder clustering & origin tracking
- ASCII visualizations

## Quick Start

```bash
# Install
npx skills add https://github.com/Xeron2000/chain-trace

# Basic analysis
/chain-trace <token_or_wallet>

# Deep analysis
/chain-trace <target> --chain bsc --mode deep
```

## Modes

- `quick` (5min): Basic info + suspicious detection
- `standard` (15min): + DBSCAN clustering
- `deep` (30-60min): + Origin tracking + coordinated distribution

## Data Sources

- **Market (all chains)**: DefiLlama (no key), DexScreener, GeckoTerminal
- **Base/ETH**: Blockscout API + Public RPC
- **BSC**: Public RPC (BSCScan searchHandler 403'd)
- **Solana**: Public RPC (Solscan API deprecated)
- **Security**: GoPlus, Honeypot.is
- **No API keys required** — all sources are public/free

## Standalone Scripts

```bash
cd ~/.claude/skills/chain-trace

# Config
python scripts/config.py --init

# Analysis
python scripts/chain_trace.py 0x... --chain bsc --mode deep

# Threshold calibration
python scripts/calibrate_thresholds.py --input templates/calibration_dataset.example.json

# Tests
python tests/test_all.py
```

## License

MIT
