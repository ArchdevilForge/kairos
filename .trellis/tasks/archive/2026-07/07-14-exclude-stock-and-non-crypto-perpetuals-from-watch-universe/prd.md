# PRD: Crypto-only OKX universe

## Goal
Exclude stock/ETF and commodity USDT perpetuals from kairos watch/scan universe. Crypto only.

## Mechanism
OKX `GET /api/v5/public/instruments?instType=SWAP`:
- `instCategory=1` → crypto (keep)
- `instCategory=3` → stock/ETF (drop)
- `instCategory=4` → commodity (drop)

Filter inside OKX `FetchTickers` so pipeline + scanner both inherit.

## Acceptance
- Top-volume set no longer includes AAPL/SNDK/SOXL/XAU/CL etc.
- Unit test with mock instruments
- Deploy kairosd to ccs and restart
