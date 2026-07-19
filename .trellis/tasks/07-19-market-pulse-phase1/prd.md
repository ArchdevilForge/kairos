# PRD: Market Pulse Phase 1–3

## Goal

Implement cross-sectional MarketPulse detector so Kairos answers “is the market worth watching?” instead of only “did one coin move?”.

## Scope (this task)

- Phase 0 baseline doc
- Phase 1 shadow detector + pipeline hookup + JSONL store + tests
- Phase 2 Telegram formatting + market event policy (no liquidity weight)
- Phase 3 quiet gate for single-symbol price_velocity (config off by default)

## Out of scope

- Derivatives enrichment (Phase 5)
- Sector impulse (Phase 6)
- 48h / 7d production observation (runtime)

## Acceptance

- `make check` green
- Default: `marketPulse.enabled=false`, `shadowMode=true`, gate off
- Shadow emits structured logs + JSONL, does not change Telegram volume
- Unit tests cover median/breadth/impulse/trend/decay/stress/gate/format
