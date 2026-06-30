# Best-Practice Research Summary

## Sources

- AWS Well-Architected: retry calls should use exponential backoff, jitter, max retry limits, idempotency, and observability.
  - https://docs.aws.amazon.com/wellarchitected/latest/framework/rel_mitigate_interaction_failure_limit_retries.html
- AWS Architecture Blog: full jitter materially reduces synchronized retry spikes and client work.
  - https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/
- Binance WebSocket Streams: connections require ping/pong handling, have message and connection limits, and should reconnect before/after server shutdown events.
  - https://developers.binance.com/docs/binance-spot-api-docs/web-socket-streams
- OKX API docs: public/private WebSocket endpoints, subscription limits, idle/no-data close behavior, and channel errors must be treated explicitly.
  - https://www.okx.com/docs-v5/en
- Bybit V5 WebSocket docs: public streams, heartbeat every 20 seconds, reconnection on disconnect, and connection-count limits.
  - https://bybit-exchange.github.io/docs/v5/ws/connect
- FIA automated trading risk controls: if a system executes trades, it needs position limits, kill switches, cancel-on-disconnect, monitoring, and post-trade reconciliation.
  - https://www.fia.org/sites/default/files/2024-07/FIA_WP_AUTOMATED%20TRADING%20RISK%20CONTROLS_FINAL_0.pdf
- FINRA algorithmic strategy guidance: production trading systems need change management, independent testing, monitoring, quick disable mechanisms, and control ownership.
  - https://www.finra.org/sites/default/files/notice_doc_file_ref/Notice_Regulatory_15-09.pdf

## Takeaways For Kairos

- At-least-once event delivery is the practical default. Duplicates are expected; event IDs, fingerprints, and idempotent handling are mandatory.
- Webhook retries should be bounded and jittered. Retrying 429, 5xx, and network/timeouts is appropriate; most 4xx responses should not be retried.
- Because WebSocket anomalies are only secondary hints, a durable queue or DLQ is not necessary in the first version. Logs, retry attempts, and explicit failure records are sufficient.
- Exchange WebSocket clients need exchange-specific heartbeat, reconnect, and subscription-limit behavior. Reconnect loops should avoid connection storms.
- If Kairos ever regains execution capability, it should be a separate execution/risk service with hard controls, not an extension of the signal MCP server.
- LLMs should not own deterministic scoring or execution authorization. They can explain, challenge, and veto, while deterministic code owns scores, thresholds, and risk constraints.
- Financial/agentic systems should keep typed tool boundaries, audit records, deterministic risk checks, and clear ownership of facts vs judgment.
