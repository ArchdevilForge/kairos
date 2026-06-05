# Kairos Architecture Baseline

> Status: authoritative baseline as of 2026-06-06.
>
> This document is the source of truth for future Kairos design and implementation work.
> If README, Hermes skills, AGENTS instructions, config examples, or code comments conflict
> with this document, treat this document as the intended architecture and update the older
> material in follow-up work.

## Product Boundary

Kairos is a cryptocurrency futures signal discovery and analysis system for Hermes Agent.

Kairos does:

- Discover liquid futures candidates.
- Analyze multi-timeframe market structure.
- Score candidate quality and trade setup quality with deterministic code.
- Return structured signals, risk guidance, chart specs, and review data through MCP tools.
- Emit lightweight WebSocket anomaly hints to Hermes.
- Persist facts, scores, fingerprints, and later outcome metrics for review.

Kairos does not:

- Place orders.
- Manage live positions.
- Track real account equity as the source of truth.
- Auto-size trades by account balance.
- Own Telegram delivery decisions.
- Let an LLM override deterministic score thresholds.

If live execution is reintroduced later, it must be a separate execution/risk service with
position limits, kill switches, order reconciliation, independent monitoring, audit logs, and
explicit human controls. It should not be bolted onto the signal MCP server.

## Authority Model

- `docs/architecture.md` is the design source of truth.
- Kairos code owns deterministic market data handling, scoring, risk constraints, persistence,
  fingerprints, and MCP schemas.
- Hermes owns scheduling, LLM review, final veto, Telegram delivery, and LLM decision records.
- Hermes skills are operation manuals. They should explain how to call Kairos MCP tools and how
  Hermes should review or veto results; they should not define the core architecture or scoring formula.
- Legacy CLI command references are deprecated. MCP tools are the current interface.

## Primary Workflow

The scanner is the primary workflow. WebSocket anomaly events are secondary hints.

```text
Hermes cron / manual request
    |
    v
Kairos MCP scan_market / analyze_symbol_setup
    |
    v
Deterministic candidate scoring + setup scoring
    |
    v
Qualified setup list with chart_spec, scores, risk bounds, reasons
    |
    v
Hermes LLM veto
    |
    v
Chart generation for approved setups
    |
    v
Telegram notification for human judgment
```

The WebSocket path is lower priority:

```text
Exchange WebSocket
    |
    v
Price/volume anomaly detector
    |
    v
Webhook SignalEvent
    |
    v
Hermes treats it as a candidate hint, not a confirmed trading signal
```

WebSocket anomalies can interrupt the normal workflow, but they must still pass the same
multi-timeframe setup analysis before being treated as a trade candidate.

## MCP Surface

The external MCP surface should converge toward one main Kairos MCP server.
Specialized analysis/chart/Coinglass modules may remain internal implementation modules, but
Hermes should interact with a stable primary tool surface.

### `scan_market`

Runs the scanner workflow.

Default behavior:

- Runs every 5 minutes when scheduled by Hermes.
- Uses OKX futures volume Top 30 as the primary universe.
- Binance and Bybit only supplement candidates or serve as availability backup.
- Performs lightweight scoring across Top 30.
- Keeps up to Top 20 candidates.
- Runs full `1d + 4h + 15m` analysis for up to Top 10.
- Returns candidates and qualified setups.
- Does not directly push Telegram.
- Does not generate charts by default.
- Returns `chart_spec` for Hermes to use after LLM veto.

Timeout policy:

- Total call budget: 75 seconds.
- Single exchange/API request budget: 8 seconds.
- Single symbol deep analysis budget: 12 seconds.
- Partial symbol failures should not fail the whole scan.
- If BTC critical context fails, return candidates but do not return trade setups.

### `analyze_symbol_setup(symbol)`

Runs the same scoring logic for one manually requested symbol.

Rules:

- Not limited to Top 30.
- Must still satisfy minimum liquidity.
- Default minimum liquidity: OKX 24h futures `quoteVolume >= 30,000,000 USDT`.
- If liquidity is below threshold, return `watch` or `no_trade`, not `trade_candidate`.

### Other Tools

Existing tools such as blacklist management, box detection, cycle analysis, support/resistance,
sentiment, pyramiding, exit checks, and chart generation should be aligned behind standardized
schemas. They can remain as lower-level tools, but scanner tools are the preferred high-level
entry points for Hermes.

## Symbol Format

Internal canonical format is CCXT USDT perpetual format:

```text
BASE/USDT:USDT
```

External inputs may accept:

- `BASE/USDT`
- `BASEUSDT`
- `BASE/USDT:USDT`

All entry points should normalize to canonical format before scoring, fingerprinting,
blacklist checks, or persistence.

## Candidate Discovery

The baseline candidate universe is OKX public futures data.

Candidate sources:

- OKX futures volume Top 30: primary source.
- Binance/Bybit futures markets: supplemental source and backup.
- Coinglass RSI, OI, funding, and market heat data: scoring inputs only, not required for basic operation.
- New/secondary listings: candidate boost only.

Coinglass or other third-party data must not be the only path into the scanner. Missing external
hotness/OI/funding data should degrade scoring and add warnings; it should not fail the scan.

## Timeframes

Core scoring uses exactly three timeframes:

- `1d`: trend direction and overhead/downside space.
- `4h`: main structure and box maturity.
- `15m`: entry trigger and execution context.

Additional timeframes such as `1h` or `5m` may be used for debugging or explanation, but they
are not part of first-version core scoring.

## Scores

Kairos must separate candidate hotness from trade setup quality.

### `candidate_score`

Used to decide which symbols deserve deep analysis.

Inputs may include:

- Futures quote volume.
- Price velocity.
- Volume spike.
- RSI/Coinglass hotness.
- Funding/OI availability and direction.
- New/secondary listing status.
- Recent relative strength or weakness.

Candidate score does not make a trade signal valid. It only affects analysis priority.

### `setup_score`

Used to decide whether an analyzed structure is a trade candidate.

Inputs include:

- Daily trend and market structure.
- Upside room for longs or downside room for shorts.
- 4H box or major structure validity.
- 15m entry trigger.
- BTC resonance.
- Market cycle weight.
- Volume confirmation.
- Funding/OI risk adjustments.
- Risk/reward.

Kairos code owns the scoring logic. Hermes may veto a setup, but it must not promote a setup
below threshold unless the user manually instructs it.

## Market Cycle

Market cycle is a scoring input, not a hard gate.

Cycle weight should be low, roughly 10%-15% of setup quality, but weak cycles should tighten
thresholds and risk parameters.

Thresholds:

| Cycle | Minimum `setup_score` for `trade_candidate` |
| --- | ---: |
| Spring | 5.5 |
| Summer | 5.5 |
| Autumn | 6.5 |
| Winter | 7.5 |

Winter does not forbid trading. It demands better evidence, lower risk, and clearer structure.

## Direction Support

Kairos supports both long and short setups.

Long and short logic must be implemented independently. Short scoring is not a simple inversion
of long scoring.

Long setup examples:

- Box breakout with volume confirmation.
- Pullback acceptance above support.
- Box-bottom support bounce.
- Strong second-wave continuation.

Short setup examples:

- Box breakdown with volume confirmation.
- Failed rebound under resistance.
- Support break and retest failure.
- Weak-market continuation with BTC downside resonance.

Shorts are stricter by default because of squeeze risk and crypto's long-biased behavior. In weak
cycles or clearly bearish BTC conditions, short thresholds may relax toward normal thresholds.

## Resonance

BTC resonance rules:

- BTC itself has no BTC resonance requirement.
- ETH uses BTC resonance with lower weight.
- Altcoins use normal BTC resonance weight.

Altcoin setups can still pass without perfect BTC alignment, but missing or conflicting BTC
resonance should reduce score and surface a warning.

## Volume, Funding, and OI

Volume:

- Breakout/breakdown setups require volume confirmation.
- Pullback, bounce, and acceptance setups treat volume as a scoring input, not a hard gate.

Funding and OI:

- First version uses funding and OI for scoring and risk notes.
- Missing funding/OI data should add warnings and degrade confidence, not fail the scan.
- Extreme funding should warn about crowded positioning and squeeze risk.
- OI expansion may add confidence when it agrees with price/structure.

## Risk Output

Kairos outputs risk bounds, not execution commands.

Allowed:

- Suggested maximum position percentage.
- Suggested maximum leverage.
- Entry zone.
- Structural stop.
- Structural targets.
- Risk/reward.
- Invalidation condition.

Not allowed:

- "Open exactly 10,000 USDT."
- "Place this order now."
- Any account-equity-specific sizing unless future execution/risk services provide verified account state.

Baseline risk constraints:

- BTC/ETH maximum suggested position: 33%.
- Altcoin maximum suggested position: 33%.
- BTC/ETH maximum suggested leverage: 10x.
- Altcoin maximum suggested leverage: 5x.
- Weak cycle, default short, or inverse-cycle setups should automatically reduce suggested risk.

## Entry, Stop, Target, RR

Entry must be a zone, not a single point.

Examples:

- Long breakout: `[breakout_level, breakout_level * 1.003]`.
- Long support bounce: support plus a tolerance range.
- Short breakdown: `[breakdown_level * 0.997, breakdown_level]`.
- Short rejection: resistance rejection zone.

Stop loss:

- Must come from structure.
- Long stops use box low, pullback low, or support below a buffer.
- Short stops use box high, rebound high, or resistance above a buffer.
- ATR or fixed percentage may only be a fallback constraint.
- No structural stop means no `trade_candidate`.

Targets:

- Prefer support/resistance, box-height projection, and round numbers.
- At least TP1 is required.
- Strong setups may provide TP1 and TP2.

Risk/reward:

- Default minimum RR: `>= 1.8`.
- Winter, inverse-cycle, or default stricter short setups: `>= 2.2`.
- In weak cycles, trend-aligned shorts may use the normal `>= 1.8` threshold.
- If nearby target space makes RR too poor, do not return `trade_candidate`.

## Action States

Scanner results should use explicit action states:

- `no_trade`: invalid or insufficient setup.
- `watch`: interesting candidate but not actionable.
- `prepare`: near setup, waiting for trigger or confirmation.
- `trade_candidate`: meets deterministic threshold and risk requirements.

Only `trade_candidate` should be eligible for Telegram push, and even then Hermes can veto.

## Duplicate Control

Duplicate control uses structure fingerprints first and time cooldown second.

Fingerprint should include, at minimum:

- Symbol.
- Direction.
- Setup type.
- Timeframe structure.
- Box or key level boundaries.
- Entry zone.
- Stop.
- Target set.

If repeated events have the same fingerprint, Hermes should remain silent unless there is a
material structure change. Material changes include breakout state change, confirmed retest,
new stop/target, or direction change.

## Blacklist

Kairos owns blacklist storage and enforcement. Hermes may manage it through MCP tools.

Rules:

- Scanner skips blacklisted symbols.
- Entries should support reason, source, created time, and optional expiry.
- Blacklist blocks new signals only.
- It does not manage existing manual positions.

## WebSocket Anomaly Events

WebSocket anomalies are auxiliary hints and may be dropped after bounded retry failure.

Delivery policy:

- At-least-once semantics.
- Stable `event_id`.
- HMAC signature.
- Bounded retries.
- Exponential backoff with jitter.
- Retry only transient failures: network/timeouts, 429, 5xx.
- Do not retry ordinary permanent 4xx failures.
- No first-version Kafka/SQS/Redis Streams/DLQ.
- Log exhausted deliveries.

Payload should be backward compatible and include:

```json
{
  "schema_version": "1.1",
  "event_id": "...",
  "event": "price_velocity",
  "timestamp": "...",
  "symbol": "BTC/USDT:USDT",
  "exchange": "okx",
  "price": 68500.0,
  "condition": "30s_0.5pct",
  "severity": "MEDIUM",
  "change_pct": 0.82,
  "source": "websocket",
  "candidate_score": 0.0,
  "fingerprint": "..."
}
```

## MCP Response Envelope

Core MCP results should standardize around an envelope:

```json
{
  "success": true,
  "schema_version": "1.0",
  "timestamp": "...",
  "symbol": "BTC/USDT:USDT",
  "data": {},
  "score": {},
  "reasons": [],
  "warnings": [],
  "errors": []
}
```

For market-wide tools, `symbol` may be omitted or set to `null`.

Do not fabricate neutral/default conclusions when critical data is missing. Return explicit
warnings/errors, reduce confidence, and withhold `trade_candidate` if required context is absent.

## Storage

First-version persistence uses SQLite.

Defaults:

- Database path: `~/.local/share/kairos/kairos.db`.
- Config may override the path.
- Tests use temporary paths.
- Detailed records are retained for 90 days.
- Long-term retention uses aggregate statistics.
- JSONL is optional export/debug/audit output, not the primary query store.

Kairos records facts and algorithm outputs:

- Scan run metadata.
- Candidate summaries.
- Candidate score and reasons.
- Deep-analysis summaries.
- Setup score and reasons.
- Fingerprint.
- Action state.
- Entry zone, stop, targets, RR.
- Chart spec.
- Future MFE/MAE/R-multiple outcome metrics.

Hermes records judgment and decision outputs:

- Prompt context.
- LLM reasoning or summary.
- Veto reason.
- Push decision.
- Telegram delivery result.

Kairos should not store full OHLCV payloads long-term by default. It should store enough
summary and price checkpoints to support review and threshold tuning.

Second-stage review tools may include:

- `review_signals`.
- `get_signal_stats`.
- `analyze_thresholds`.

These are not required for the first scanner implementation.

## Review Metrics

Signal review must be based on trade structure, not simple price up/down.

Track:

- Whether entry zone was touched.
- Whether stop was touched.
- TP1/TP2 touches.
- MFE.
- MAE.
- 1h/4h/24h R-multiple.
- Performance by score bucket, cycle, direction, setup type, and symbol class.

## Charts

Charts are generated after Hermes veto, not by default inside `scan_market`.

Policy:

- `scan_market` returns `chart_spec`.
- Hermes calls chart generation only for setups it intends to push.
- Default Telegram signal includes one 15m entry chart.
- If `setup_score >= 8` or the signal comes from a 4H major structure breakout/breakdown,
  include a multi-timeframe chart.
- BTC comparison chart is generated only when BTC resonance is disputed or useful.
- If chart generation fails, Hermes may send text-only output with a degraded-chart warning.

## Configuration

Configuration should be reorganized into these sections:

- `scanner`: interval, universe size, candidate/deep-analysis limits, timeout budgets.
- `exchanges`: primary exchange, backup exchanges, rate limits, symbol normalization.
- `scoring`: weights, thresholds, cycle adjustments, long/short settings, RR requirements.
- `risk`: max position/leverage guidance, phase/direction risk reductions.
- `webhook`: URL, secret, retry settings, schema version.
- `storage`: SQLite path, retention, JSONL export.
- `charts`: default chart count, output path, cleanup policy.

First version does not need runtime hot reload. Config changes take effect after restart.

## Documentation Alignment

Follow-up work should align older materials:

- README: describe MCP-first, scanner-first architecture.
- AGENTS: mark old CLI command examples deprecated or replace them with MCP tool names.
- `skills/kairos-harness`: describe Hermes review/veto behavior and tool-calling flow.
- `skills/kairos-scanner-orchestrator`: stop defining scoring/pipeline internals; call `scan_market`.
- `config/config.yaml.example`: adopt the new config section model.
- `progress.md`: either update or mark as historical.

## Implementation Sequence

Recommended follow-up sequence:

1. Standardize schemas and config types.
2. Add storage path helpers and SQLite persistence.
3. Implement candidate scoring and setup scoring as pure/testable modules.
4. Implement `scan_market`.
5. Implement `analyze_symbol_setup`.
6. Add chart specs and delayed chart generation flow.
7. Expand Webhook payload schema.
8. Update Hermes skills and README.
9. Add review/statistics MCP tools.

## Design Rationale

The architecture deliberately keeps LLM judgment outside deterministic scoring. External research
and production reliability patterns point to the same conclusion: event systems need stable schemas,
idempotency, bounded retries, and observability; trading systems that execute need much stronger
risk controls. Since Kairos is a signal system, the pragmatic first version should keep execution out,
make scanner decisions reproducible, and use Hermes as a reviewer and delivery layer.
