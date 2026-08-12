# Kairos Alert Domain

Human-controlled futures alert system: hard-data anomalies and structure scans surface candidates; humans alone decide trades.

## Language

**Realtime Anomaly**:
A hard-data market event (price velocity, market pulse, etc.) that passes detector and Alert Gate and may be delivered on configured Delivery Channels.
_Avoid_: signal (alone), trade signal, order, entry

**Scanner Candidate**:
A deterministic structure-scan result with action state `watch`, `prepare`, or `trade_candidate`. Not an order instruction.
_Avoid_: signal, alert (when meaning realtime), setup to open

**Action State**:
Scanner filter ladder: `no_trade` < `watch` < `prepare` < `trade_candidate`. Never means “open now.”
_Avoid_: buy/sell recommendation, conviction

**Alert Gate**:
A delivery filter (event type, severity, liquidity weight, type-specific thresholds, dedup, cooldown) applied once before channel fan-out. Rejects must be observable.
_Avoid_: silent drop, blacklist (blacklist is separate)

**Behavior Gate**:
Mechanical enforcement of doctrine §9b personal behavior red lines (isolated-only, cooldown after loss, live window, max loss, stop required) at decision time in kairos-desk. Rejects and overrides are journaled as annotations; it judges the trader's behavior, not the market.
_Avoid_: Alert Gate (that filters alerts), risk manager, auto stop-loss

**Delivery Channel**:
An outbound sink for gated alerts (Telegram, DingTalk custom-robot webhook). Channels are optional and independent; one failure does not block another.
_Avoid_: notifier bus, message queue, multi-tenant routing

**Mirror Delivery**:
The same post-gate AlertEvent is rendered per channel (HTML vs markdown) and sent to every configured Delivery Channel. Not event-type routing.
_Avoid_: primary/secondary failover, load-balanced notify

**Liquidity Weight**:
Market-cap-derived 0..1 scalar that tightens thresholds for smaller names (`strict = 1/weight`). Majors are weight 1. Not applied to market-level (`MARKET`) events.
_Avoid_: liquidity score as trade quality, position size

**Human Control Boundary**:
All entries, exits, sizing, and chart judgment stay with the human. Risk fields are bounds, not instructions.
_Avoid_: auto trade, bot order, LLM production path
