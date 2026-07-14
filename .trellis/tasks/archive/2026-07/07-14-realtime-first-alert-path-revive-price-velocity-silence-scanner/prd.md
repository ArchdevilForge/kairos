# PRD: Realtime-first alert path

## Problem
Operator treats only realtime price-velocity as useful. Production was inverted: scanner pushed edge `prepare` every 4h; `kairosd` sent zero TG events for 5+ days. Hermes MCP path dead.

## Decisions (grilled, confirmed)
1. Primary channel: realtime price_velocity only.
2. Scanner timer: stop (`kairos-alert.timer` off). Manual scan only.
3. Hermes gateway: stop.
4. Thresholds: 60s 0.9%, 120s 1.4%, minPriceChangePct 0.9, minWeight 0.5.
5. Cooldown: keep 45min symbol cooldown.
6. Observability: gate rejects + successful sends at INFO.

## Out of scope
- volume/OI/funding/resonance re-enable
- universe expansion
- auto trading / Hermes rewire

## Acceptance
- Journal shows `telegram sent` and/or `alert gated` with reason/weight/change_pct.
- No 4h prepare spam.
- Hermes not running.
- Config + kairosd deployed on ccs.
