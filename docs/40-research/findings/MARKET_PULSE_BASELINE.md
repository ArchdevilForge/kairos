# Market Pulse Baseline

> Phase 0 of `docs/GOAL_MARKET_PULSE.md`  
> Recorded: 2026-07-19  
> Purpose: freeze single-symbol alert behaviour before MarketPulse changes delivery.

## 1. Data availability

| Source | Status |
| --- | --- |
| `~/.local/share/kairos/kairos.db` | empty / no runtime history on this host |
| Telegram send logs | not retained locally |
| HintStore watch hints | no durable 7-day alert count |

**Conclusion**: quantitative 7-day alert counts are **unavailable** on this workstation. Baseline below is derived from **live config defaults** and **code-path behaviour**. Fill the numeric table after the next 7-day production window.

### Numeric placeholders (fill after observation)

| Metric | Value | Notes |
| --- | --- | --- |
| Total single-symbol alerts (7d) | N/A | no DB |
| Daily average alerts | N/A | no DB |
| BTC/ETH share of alerts | N/A | no DB |
| Small-cap isolated share | N/A | no DB |
| Subjective “worth opening chart” | N/A | requires human labels |

## 2. Current production alert profile (config snapshot)

Source of truth: `config/config.yaml.example` + `internal/config/config.go` defaults.

```yaml
alertPolicy:
  enabled: true
  allowedEventTypes: ["price_velocity"]
  minSeverity: "LOW"
  minPriceChangePct: 0.9
  liquidityWeight:
    enabled: true
    minWeight: 0.5
    majorSymbols: ["BTC/USDT:USDT", "ETH/USDT:USDT"]

priceVelocity:
  enabled: true
  windows:
    - seconds: 60
      threshold: 0.9
    - seconds: 120
      threshold: 1.4
  cooldownSeconds: 120

dataManager:
  topSymbols: 30
  dedupWindowSeconds: 5
  symbolCooldownMinutes: 45
```

Other detectors (`volumeSpike`, `futuresMetrics`, `longShortRatio`, `liquidation`, `resonanceScorer`) default **disabled**.

## 3. Telegram cooldown / gate behaviour (code)

Path: `internal/engine/pipeline.go` → `deliverEvent` / `passesAlertPolicy`.

1. **Blacklist** — drop blocked symbols.
2. **Event type allow-list** — only `price_velocity` by default.
3. **Severity + liquidityWeight** — low-cap symbols get severity penalty and stricter `minPriceChangePct`.
4. **Dedup window** — same `symbol__event_type` within `dedupWindowSeconds` (default 5s).
5. **Symbol cooldown** — same key within `symbolCooldownMinutes` (default 45 min).
6. **Detector-local cooldown** — `priceVelocity.cooldownSeconds` (default 120s) per `symbol_window`.

Net effect: one symbol can still produce at most ~1 Telegram alert / 45 min after policy, but **many different symbols** can each fire independently → noisy “small-cap pump” nights.

## 4. Typical false-positive classes (qualitative)

From product problem statement and detector design (not labeled logs):

1. **Isolated alt pumps** — one meme/L2 coin clears 0.9% / 60s while breadth is near zero.
2. **Needle / WebSocket glitch** — single-window spike without continuation.
3. **News-coin spikes** — high velocity, no BTC/ETH confirmation.
4. **No market context** — alert answers “what moved”, not “is the market open for trading attention”.

## 5. Subjective utility (pre-MarketPulse)

Without 7-day labels, the working hypothesis for Phase 3 success criteria:

- Most Telegram wakes are **not** “open the full board” moments.
- Target after gate: **≥50% reduction** in total Telegram volume; market alerts **1–8 / day**.

## 6. Baseline freeze checklist

- [x] Document default config snapshot
- [x] Document cooldown / dedup chain
- [x] Document false-positive classes
- [ ] Fill 7-day numeric table after production observation
- [ ] Label ≥20 alerts as worth / not-worth opening chart

## 7. Shadow config recommendation (Phase 1)

```yaml
marketPulse:
  enabled: true
  shadowMode: true   # compute + log only; do not change Telegram behaviour
```

Leave `alertPolicy.allowedEventTypes` as `["price_velocity"]` until Phase 2.
