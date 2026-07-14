# Realtime is the primary alert channel; scanner timer stays off

Operator confirmed scanner `prepare` noise is useless for them; only realtime price-velocity anomalies are actionable attention cues. We stop `kairos-alert.timer` by default on the production host, lower price-velocity thresholds for “daily few events” volume, keep 45m symbol cooldown, and log gate rejects / successful sends at INFO so a silent pipeline is diagnosable.

**Status**: accepted

## Considered Options
- Keep 4h scanner, only raise min state to `trade_candidate` — still blurs “signal” and was rejected.
- Disable liquidity weight entirely — more volume, less control; deferred.
- Expand symbol universe — deferred until 0.9% thresholds prove too quiet.

## Consequences
- Production attention path is `kairosd` only.
- Manual `kairos-alert` remains a research tool, not a signal source.
- Hermes is not part of the alert path and is stopped on the host.
