# Signal Alert Contracts

## Scope

- Trigger: changes to scanner response envelopes, Telegram alert delivery, realtime anomaly dispatch, alert config, or human-control boundaries.
- Applies to `src/kairos/scanner.py`, `src/kairos/telegram.py`, `src/kairos/alert_runner.py`, `src/kairos/watch_runner.py`, `src/kairos/data/data_manager.py`, and config/defaults.
- Product source of truth: `docs/architecture.md`.

## Signatures

- `scan_market(config=None, exchange_getter=None, exchange=None, blacklist=None) -> dict[str, Any]`
- `analyze_symbol_setup(symbol, config=None, exchange_getter=None, exchange=None, blacklist=None) -> dict[str, Any]`
- `_make_signal_envelope(...) -> dict[str, Any]` (internal, scanner.py)
- `TelegramClient.send_event(event: AlertEvent) -> bool`
- `TelegramClient.send_text(text: str) -> bool`

## Contracts

- No production alert path may require an LLM, assistant skill, MCP server, or order execution module.
- Core signal envelope fields (produced by `_make_signal_envelope` in scanner.py): `success`, `schema_version`, `timestamp`, `symbol`, `data`, `score`, `reasons`, `warnings`, `errors`.
- Symbol input normalizes to `BASE/USDT:USDT`; invalid symbols return a failed envelope instead of raising across CLI boundaries.
- Scanner states are deterministic filters only: `no_trade`, `watch`, `prepare`, `trade_candidate`.
- Alert copy must clearly state human control in Chinese, currently `仅供人工判断，不自动交易。`
- Risk output is bounded context only: entry zone, structural stop, targets, RR, max position percentage, and max leverage. Do not include account-equity sizing or order placement.
- Telegram credentials come only from `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID`; optional scanner filters are `KAIROS_ALERT_MIN_STATE` and `KAIROS_ALERT_LIMIT`.
- `DataManager` applies `alertPolicy` before Telegram delivery and before mutating dedup/cooldown state.
- CoinGlass data may enrich hard-data context but must remain optional evidence, not a hard dependency.

## Validation

- Missing Telegram credentials: realtime alerts are dropped with a warning; one-shot `kairos-alert` exits with a clear error unless `--dry-run` is used.
- Scanner failure: `kairos-alert` prints envelope errors and returns non-zero.
- No setups above `--min-state`: `kairos-alert` exits successfully without sending.
- Required timeframe or BTC context missing: scanner may return candidates, but must withhold or downgrade setup states with warnings.
- Liquidity/RR/threshold failure: never emit `trade_candidate`.

## Tests Required

- Envelope tests assert all standard fields and symbol normalization.
- Telegram tests assert formatted messages include the human-control line and send through Telegram `sendMessage`.
- Scanner tests assert BTC-context/liquidity/threshold gates block `trade_candidate`.
- Alert runner tests assert `--dry-run`, min-state filtering, missing credentials, and send behavior.
- DataManager tests assert `alertPolicy` filters before Telegram dispatch.

## Wrong vs Correct

### Wrong

```python
return {"signal": "buy now", "size": "10000 USDT"}
```

### Correct

```python
return _make_signal_envelope(
    success=True,
    symbol="BTC/USDT:USDT",
    data={"setup": {"action_state": "prepare"}},
    score={"setup_score": 5.8},
    warnings=["15m trigger not active"],
)
```
