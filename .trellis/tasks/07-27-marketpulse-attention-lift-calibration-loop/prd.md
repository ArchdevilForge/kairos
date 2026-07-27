# PRD: MarketPulse attention-lift calibration loop

## Goal

Make the 90-day success metric measurable: attention lift of market alerts vs random same-hour baselines. Demote scanner entry/stop narrative to post-alert explanation.

## Scope (MVP)

1. Persist lightweight MarketPulse snapshots every 60s to JSONL (`market-pulse-snapshots.jsonl`).
2. Extend `kairos-calibrate` to report `lift_5m = cont_alert / cont_random_same_hour`.
3. Docs/config: scanner is optional post-alert explanation; not a default scheduled trade signal product.

## Out of scope

Multi-sector pulses, OI/orderbook gates, percentile baseline engine, trading frameworks, Prometheus/ClickHouse.

## Acceptance

- Snapshot rows written when MarketPulse enabled + store ready.
- `kairos-calibrate --snapshots ...` prints lift (or clear "insufficient data").
- `make check` passes.
- PR opened against ArchdevilForge/kairos.
