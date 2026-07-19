# Market Pulse Tuning Log

> Phase 4 working log. Record every parameter version here.

## Rules

1. Change at most 1–2 parameters per version.
2. Observe ≥3 days or ≥20 events before next change.
3. Never tune on a single session.

## Versions

### v0 — initial defaults (Phase 1 shadow)

```yaml
impulse:
  minBreadth: 0.65
  minMedianReturnPct: 0.18
  minMedianZ: 1.5
  confirmationSamples: 3
  confirmationWindowSamples: 4
trend:
  minBreadth: 0.60
  minMedianReturnPct: 0.45
  minPersistSeconds: 180
stress:
  minBreadth: 0.80
  minMedianReturnPct: 0.35
  minMedianZ: 2.5
```

| Metric | Value | Notes |
| --- | --- | --- |
| Events observed | 0 (first ~1h) | ccs shadow since 2026-07-19 09:34 EDT |
| Live heartbeat | `valid=30 fresh=1 state=QUIET` | med60 typically ±0.06%, breadth <0.4 |
| Impulse precision (5m +0.20%) | — | need events |
| Subjective worth-open rate | — | |
| Alerts / day | 0 market events | single-coin path unchanged |

### Live shadow sample (ccs)

```text
09:35 med60=-0.022 upB=0.133 downB=0.200 QUIET
09:37 med60=-0.045 upB=0.033 downB=0.367 QUIET
09:42 med60=+0.034 upB=0.367 downB=0.100 QUIET
```

No false impulse during quiet tape. Continue to 48h.

## Event outcome template

```text
ts=
event=
direction=
median_return_60s=
breadth=
+1m=
+3m=
+5m=
+15m=
MFE=
MAE=
worth_open= yes|no|unsure
notes=
```
