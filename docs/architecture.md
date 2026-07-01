# Kairos Architecture Baseline

> Status: authoritative baseline as of 2026-06-27.

Kairos is a human-controlled cryptocurrency futures alert system.

## Product Boundary

Kairos does:

- Discover liquid USDT perpetual candidates.
- Watch realtime market data for price, volume, open-interest, and funding anomalies.
- Analyze multi-timeframe market structure with deterministic code.
- Return candidate state, scores, risk bounds, reasons, and warnings.
- Push hard-data alerts directly to Telegram.

Kairos does not:

- Use an LLM in the production alert path.
- Expose model-facing tool servers.
- Place orders.
- Manage live positions.
- Track real account equity as the source of truth.
- Auto-size trades by account balance.
- Tell the user to open a trade.

All trading decisions are manual. Scanner labels such as `watch`, `prepare`, and
`trade_candidate` are candidate filters, not instructions.

If live execution is reintroduced later, it must be a separate execution/risk service
with position limits, kill switches, order reconciliation, independent monitoring,
audit logs, and explicit human controls.

## Runtime Commands

```text
kairos-watch
    Exchange WebSocket feeds
    -> price/volume anomaly detectors
    -> periodic futures metrics polling
    -> Telegram hard-data alerts

kairos-alert
    scan_market
    -> deterministic setup scoring
    -> Telegram candidate summary
```

## Authority Model

- Kairos code owns market data handling, scoring, risk bounds, blacklist storage, and alert formatting.
- Telegram is only a delivery channel.
- Humans own chart review, trade selection, sizing decisions, entries, exits, and whether to ignore an alert.
- `docs/trading-system.md` is the strategy knowledge source.

## Alert Types

Realtime alerts:

- `price_velocity`
- `volume_spike`
- `open_interest_change`
- `funding_rate_anomaly`

Scanner candidate states:

- `no_trade`: invalid or insufficient setup.
- `watch`: interesting candidate, not actionable by itself.
- `prepare`: near setup, waiting for trigger or confirmation.
- `trade_candidate`: deterministic threshold and risk requirements are met.

Telegram copy must not use language equivalent to "open this trade now".

## Symbol Format

Internal canonical format is CCXT USDT perpetual format:

```text
BASE/USDT:USDT
```

External inputs may accept:

- `BASE/USDT`
- `BASEUSDT`
- `BASE/USDT:USDT`

Entry points normalize to canonical format before scoring, blacklist checks, or persistence.

## Candidate Discovery

The baseline candidate universe is public futures data:

- OKX futures volume Top 30 is the primary universe.
- Binance and Bybit supplement candidates or serve as availability backups.
- CoinGlass RSI, open-interest, funding, and heat data may be optional scoring inputs.

Missing optional third-party data should degrade confidence and add warnings; it must not fail basic scanning.

## Timeframes

Core scoring uses exactly three timeframes:

- `1d`: trend direction and overhead/downside space.
- `4h`: main structure and box/range maturity.
- `15m`: entry trigger and execution context.

## Scores

Kairos separates candidate hotness from trade setup quality.

`candidate_score` decides which symbols deserve deeper human attention. Inputs may include futures quote volume, price velocity, volume spike, RSI/hotness, funding/OI availability, and recent relative strength.

`setup_score` decides which analyzed structures are worth surfacing. Inputs include daily trend, 4H structure, 15m trigger, BTC resonance, market cycle, volume confirmation, funding/OI risk notes, and risk/reward.

## Risk Output

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
- Any account-equity-specific sizing.

Baseline risk constraints:

- BTC/ETH maximum suggested position: 33%.
- Altcoin maximum suggested position: 33%.
- BTC/ETH maximum suggested leverage: 10x.
- Altcoin maximum suggested leverage: 5x.
- Weak cycle, default short, or inverse-cycle setups reduce suggested risk.

## Telegram Delivery

Credentials come from environment variables:

```text
TELEGRAM_BOT_TOKEN
TELEGRAM_CHAT_ID
```

Scanner alert knobs:

```text
KAIROS_ALERT_MIN_STATE
KAIROS_ALERT_LIMIT
```

There is no remote AI server, inbound alert route, or HMAC route in the current architecture.

For a multi-lens architecture/strategy review and optimization roadmap, see
[`architecture-review.md`](architecture-review.md).
