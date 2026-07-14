# Kairos Alert Domain

Human-controlled futures alert system: hard-data anomalies and structure scans surface candidates; humans alone decide trades.

## Language

**Realtime Anomaly**:
A hard-data market event (currently price velocity) that passes detector and alert policy gates and may be delivered to Telegram.
_Avoid_: signal (alone), trade signal, order, entry

**Scanner Candidate**:
A deterministic structure-scan result with action state `watch`, `prepare`, or `trade_candidate`. Not an order instruction.
_Avoid_: signal, alert (when meaning realtime), setup to open

**Action State**:
Scanner filter ladder: `no_trade` < `watch` < `prepare` < `trade_candidate`. Never means “open now.”
_Avoid_: buy/sell recommendation, conviction

**Alert Gate**:
A delivery filter (event type, severity, liquidity weight, type-specific thresholds, dedup, cooldown) applied before Telegram send. Rejects must be observable.
_Avoid_: silent drop, blacklist (blacklist is separate)

**Liquidity Weight**:
Market-cap-derived 0..1 scalar that tightens thresholds for smaller names (`strict = 1/weight`). Majors are weight 1.
_Avoid_: liquidity score as trade quality, position size

**Human Control Boundary**:
All entries, exits, sizing, and chart judgment stay with the human. Risk fields are bounds, not instructions.
_Avoid_: auto trade, bot order, LLM production path
