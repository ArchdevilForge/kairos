# Signal Alert Contracts

## Scope

- Trigger: changes to scanner response envelopes, Telegram alert delivery, realtime anomaly dispatch, alert config, or human-control boundaries.
- Applies to `internal/scanner/`, `internal/notify/`, `internal/alert/`, `cmd/kairosd/`, `internal/engine/`, and config/defaults.
- Product source of truth: `docs/architecture.md`.

## Signatures

- `(*MarketScanner).ScanMarket(ctx, exchangeName) -> *types.SignalEnvelope` (`internal/scanner`)
- `(*MarketScanner).AnalyzeSymbolSetup(ctx, symbol, exchangeName) -> *types.SignalEnvelope` (`internal/scanner`)
- `makeSignalEnvelope(...) -> *types.SignalEnvelope` (internal, `internal/scanner/helpers.go`)
- `(*TelegramClient).SendEvent(event types.AlertEvent) error` (`internal/notify`)
- `(*TelegramClient).SendText(text string) error` (`internal/notify`)
- `alert.SelectSetups(scan *types.SignalEnvelope, minState string, limit int) -> []map[string]any` (`internal/alert`)
- `alert.FormatAlert(setups []map[string]any) -> string` (`internal/alert`)
- `(*Pipeline).Start(ctx) error` / `(*Pipeline).Stop()` (`internal/engine`)

## Contracts

- No production alert path may require an LLM, assistant skill, MCP server, or order execution module.
- Core signal envelope fields (produced by `makeSignalEnvelope` in Go): `success`, `schema_version`, `timestamp`, `symbol`, `data`, `score`, `reasons`, `warnings`, `errors`.
- Symbol input normalizes to `BASE/USDT:USDT`; invalid symbols return a failed envelope instead of raising across CLI boundaries.
- Scanner states are deterministic filters only: `no_trade`, `watch`, `prepare`, `trade_candidate`.
- Alert copy must clearly state human control in Chinese, currently `仅供人工判断，不自动交易。`
- Risk output is bounded context only: entry zone, structural stop, targets, RR, max position percentage, and max leverage. Do not include account-equity sizing or order placement.
- Telegram credentials come only from `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID`; optional scanner filters are `KAIROS_ALERT_MIN_STATE` and `KAIROS_ALERT_LIMIT`.
- `engine.Pipeline` applies `alertPolicy` before Telegram delivery and before mutating dedup/cooldown state.
- CoinGlass data may enrich hard-data context but must remain optional evidence, not a hard dependency.
- CoinGlass fetch prefers Python `scripts/coinglass_fetch.py` + sibling `coinglass-decrypt` repo when available; falls back to native Go decrypt in `internal/data/coinglass.go`.
- CoinGlass env keys (all optional): `KAIROS_COINGLASS_DECRYPT`, `KAIROS_COINGLASS_PYTHON`, `KAIROS_COINGLASS_USE_PYTHON` (`0`/`false` forces Go-only).
- Scanner RSI hotness uses `data.FetchSpotRSIMap` → `scoreCandidate(..., rsiMap)`; weight key `scoring.candidateWeights.rsiHotness` (default `1.0`). Missing RSI adds a warning, never blocks scan.
- OKX funding rates are lazy-loaded via `exchange.FundingEnricher.EnrichFunding` for universe symbols only (not full ticker map).
- Scanner deep analysis runs candidates in parallel (semaphore max 6) and fetches multi-timeframe OHLCV concurrently per symbol; optional per-symbol timeout from `scanner.symbolAnalysisTimeoutSeconds`.

## Validation

- Missing Telegram credentials: realtime alerts are dropped with a warning; one-shot `kairos-alert` exits with a clear error unless `--dry-run` is used.
- Scanner failure: `kairos-alert` prints envelope errors and returns non-zero.
- No setups above `--min-state`: `kairos-alert` exits successfully without sending.
- Required timeframe or BTC context missing: scanner may return candidates, but must withhold or downgrade setup states with warnings.
- Liquidity/RR/threshold failure: never emit `trade_candidate`.

## Tests Required

- `internal/types`: JSON round-trip for `SignalEnvelope`, `AlertEvent`, `Setup`.
- `internal/notify`: formatted messages include the human-control line.
- `internal/scanner`: BTC-context/liquidity/threshold gates block `trade_candidate`; RSI unavailable degrades with warning only.
- `internal/data`: `ParseSpotRSIMap`, `RSIHotnessScore`, CoinGlass Python/Go fetch paths.
- `internal/alert`: `--dry-run` shape, min-state filtering (see `cmd/kairos-alert` tests).
- `internal/engine`: alert policy and pipeline wiring tests (`exchangeNew` injectable in tests).
- `internal/backtest`: OKX OHLCV backward pagination from `end` cursor (not forward `since`).

## Wrong vs Correct

### Wrong

```go
return map[string]any{"signal": "buy now", "size": "10000 USDT"}
```

### Correct

```go
return makeSignalEnvelope(true, map[string]any{
    "setup": map[string]any{"action_state": "prepare"},
}, "BTC/USDT:USDT", map[string]any{"setup_score": 5.8}, nil,
    []string{"15m trigger not active"}, nil, nil)
```
