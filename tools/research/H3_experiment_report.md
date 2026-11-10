# Experiment H3 — Box Maturity & Second-Wave Lifecycle (2026-11-10)

- **Verdict**: **FAIL** (`FAIL (KILLED BY PRE-REGISTERED H3 RULE): OOS independent wave clusters G_oos = 6 < 15 minimum bar; OOS NetR 95% CI lower bound = -0.7935R < 0.0000R minimum bar. Automatic box/second-wave geometry recognizer is NOT built; delivering `kairos-desk` manual watch-range entry specification instead.`)
- **Dataset**: OKX USDT-SWAP PIT 1m + HTF (4h/1d) Replay
- **Swing Pivot Confirmation Lag**: `k = 3` bars (`15m` mandatory right-shoulder confirmation; zero lookahead)
- **Detected Structural Setups (`N`)**: 114 (`BOX_BREAKOUT = 23`, `SECOND_WAVE_STAGE4 = 91`)
- **Independent Wave Clusters (`G_total / G_oos`)**: `7` total / `6` out-of-sample (`<24h` merge rule)

## 1. Pre-Registered Formalization & Kill Rule

1. **Box Maturity (`BOX_BREAKOUT`)**: Requires `>= 2` confirmed swing high probes + `>= 2` confirmed swing low probes (`BoxTouchCount >= 4`), `ATRCompressionRatio <= 0.88`, and causal `RoomToResistanceR >= 1.0R` to the next 4h/1d resistance.
2. **Divergence / Second-Wave Lifecycle (`SECOND_WAVE_STAGE4`)**: Stage-1 impulse (`>= 1.2%`) -> Stage-2 high-volume divergence (`TriggerKnifeLock = True`, no left-side catching) -> Stage-3 volatility contraction after confirmed higher low -> Stage-4 right-side re-break.
3. **Pre-Registered KILL Rule (Item 7)**: If out-of-sample independent wave clusters `G_oos < 15` **OR** OOS NetR 95% CI lower bound `< 0.0000R`, **DO NOT** build any automatic geometry recognizer or wire `CycleMap` geometry fields; deliver instead the `kairos-desk` manual watch-range entry specification.

## 2. Structural Setup Performance (`wave_k_be=0.8` Full-Friction NetR)

| Cohort | Setups (N) | Wave Clusters (G) | Mean NetR | Cluster SE | t-stat | 95% CI |
|---|---:|---:|---:|---:|---:|---:|
| `Box Maturity Breakouts (BOX_BREAKOUT)` | 23 | 7 | **`-0.3928R`** | `0.1125` | `-3.49` | `[-0.6681, -0.1176]` |
| `Second-Wave Stage-4 Re-Break (SECOND_WAVE_STAGE4)` | 91 | 7 | **`-0.4302R`** | `0.1773` | `-2.43` | `[-0.8640, +0.0036]` |
| `Combined In-Sample Setups` | 114 | 7 | **`-0.4227R`** | `0.1434` | `-2.95` | `[-0.7735, -0.0718]` |
| **`Walk-Forward Out-of-Sample (OOS)`** | 98 | **6** | **`-0.3792R`** | `0.1612` | `-2.35` | **`[-0.7935, +0.0352]`** |

## 3. Left-Side Knife Catching vs Stage-4 Right-Side Re-Break Counterfactual

| Entry Timing | Mean Full-Friction NetR | Difference (`Stage-4 - Left-Side Knife`) |
|---|---:|---:|
| `Stage-2 Left-Side Falling Knife (Unconfirmed Pullback)` | `-0.0195R` | — |
| `Stage-4 Right-Side Confirmed Re-Break (k=3 Lag)` | `-0.4302R` | **`-0.4107R`** |

## 4. H3 Kill-Rule Evaluation & Shadow Wiring Decision

| Kill Criterion | Observed Value | Required Bar | Triggered? |
|---|---:|---:|---|
| `OOS Independent Wave Clusters (G_oos)` | **`6`** | `>= 15` | **YES (KILLED)** |
| `OOS NetR 95% CI Lower Bound` | **`-0.7935R`** | `>= 0.0000R` | **YES (KILLED)** |

- **H3 Overall Verdict**: **FAIL** (`KILLED`)
- **Shadow Fields on `CycleMap` (`RoomToResistanceR`, `BoxTouchCount`, `ATRCompressionRatio`, `TriggerKnifeLock`)**: **DO NOT WIRE (KILLED BY ITEM 7 RULE)**
- **Fallback Deliverable (Item 7)**: Delivered `kairos-desk` manual watch-range entry specification (`tools/research/H3_watch_range_spec.md` & `docs/90-engineering/specs/kairos-desk-watch-range-spec.md`).
