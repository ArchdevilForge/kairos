# Experiment H3 — Box Maturity & Second-Wave Lifecycle (2026-11-10)

- **Verdict**: **FAIL** (`FAIL (KILLED BY PRE-REGISTERED H3 RULE): OOS NetR 95% CI lower bound = -0.4077R < 0.0000R minimum bar. Automatic box/second-wave geometry recognizer is NOT built; delivering `kairos-desk` manual watch-range entry specification instead.`)
- **Dataset**: OKX USDT-SWAP PIT 1m + HTF (4h/1d) Replay (12-Month Expanded Sample: 2025-11-09 -> 2026-11-09, 40 Slices, 27 PIT Symbols)
- **Swing Pivot Confirmation Lag**: `k = 3` bars (`15m` mandatory right-shoulder confirmation; zero lookahead)

## 1. Achieved Sample Window & Statistical Power Target (Reported First)

| Power / Coverage Metric | Phase 2 (18-Day Sample, PR #7) | Phase 2b Pre-Registered Target | Phase 2b Achieved (12-Month Sample) | Power Status |
|---|---:|---:|---:|---|
| **Contiguous Calendar Span** | `18 days` (`2026-10-22 -> 2026-11-09`) | `12 months` (`>= 6m` floor) | **`366 days / 12.0 months` (`2025-11-09 -> 2026-11-09`)** | **MET (`12.0m`)** |
| **1m Replay Slices + Continuous HTF** | `4 slices` + `4h/1d` | `40 slices` + continuous `4h/1d` | **`40 slices` (`758,013` 1m + `52,946` HTF bars)** | **MET** |
| **Detected Structural Setups (`N`)** | `114` (`Box=23`, `SW4=91`) | Scale with window | **`990`** (`Box=123`, `SW4=867`, `8.68x` Phase 2) | **MET** |
| **Total Merged Wave Clusters (`G_total`)** | `7` | `>= 25` | **`40`** (`5.71x` Phase 2) | **MET** |
| **Out-of-Sample Independent Wave Clusters (`G_oos`)** | `6` (underpowered `< 15`) | **`G_oos >= 15`** | **`39`** (`OOS N = 949`, `6.50x` Phase 2) | **MET (`39 >= 15`)** |

## 2. Pre-Registered Formalization & Kill Rule (Unchanged from Phase 2)

1. **Box Maturity (`BOX_BREAKOUT`)**: Requires `>= 2` confirmed swing high probes + `>= 2` confirmed swing low probes (`BoxTouchCount >= 4`), `ATRCompressionRatio <= 0.88`, and causal `RoomToResistanceR >= 1.0R` to the next 4h/1d resistance.
2. **Divergence / Second-Wave Lifecycle (`SECOND_WAVE_STAGE4`)**: Stage-1 impulse (`>= 1.2%`) -> Stage-2 high-volume divergence (`TriggerKnifeLock = True`, no left-side catching) -> Stage-3 volatility contraction after confirmed higher low -> Stage-4 right-side re-break.
3. **Pre-Registered KILL Rule (Item 7)**: If out-of-sample independent wave clusters `G_oos < 15` **OR** OOS NetR 95% CI lower bound `< 0.0000R`, **DO NOT** build any automatic geometry recognizer or wire `CycleMap` geometry fields; reference instead the `kairos-desk` manual watch-range entry specification from PR #7.

## 3. Side-by-Side Comparison: Phase 2 (18-Day) vs Phase 2b (12-Month Expanded Sample)

| Metric | Phase 2 (18-Day, PR #7) | Phase 2b (12-Month Expanded) | Pre-Registered Bar / Kill Rule | Phase 2b Status |
|---|---:|---:|---:|---|
| `Total Structural Setups (N)` | `114` (`Box=23`, `SW4=91`) | **`990` (`Box=123`, `SW4=867`)** | — | `8.68x` expansion |
| **`OOS Independent Wave Clusters (G_oos)`** | `6` (`KILLED: < 15`) | **`39` (`G_total = 40`)** | **`G_oos >= 15`** | **PASS (`39 >= 15`)** |
| `BOX_BREAKOUT Mean NetR (N, G, 95% CI)` | `-0.3928R (23, 6, [-0.6681, -0.1176])` | **`-0.3790R (123, 35, [-0.5549, -0.2031])`** | Diagnostic | `SE = 0.0865` |
| `SECOND_WAVE_STAGE4 Mean NetR (N, G, 95% CI)` | `-0.4302R (91, 6, [-0.8640, +0.0036])` | **`-0.2258R (867, 40, [-0.3841, -0.0676])`** | Diagnostic | `SE = 0.0782` |
| `Combined In-Sample Mean NetR (N, G, 95% CI)` | `-0.4227R (114, 7, [-0.7735, -0.0718])` | **`-0.2449R (990, 40, [-0.3932, -0.0965])`** | Diagnostic | `SE = 0.0733` |
| **`Walk-Forward OOS Mean NetR (N, G_oos)`** | **`-0.3792R (98, 6)`** | **`-0.2528R (949, 39)`** | — | `SE = 0.0765, t = -3.30` |
| **`Walk-Forward OOS 95% CI [Lower, Upper]`** | **`[-0.7935R, +0.0352R]`** | **`[-0.4077R, -0.0978R]`** | **`Lower Bound >= 0.0000R`** | **FAIL (KILLED: `-0.4077R < 0`)** |
| `Left-Side Knife vs Stage-4 Right-Side Edge` | `-0.0195R vs -0.4302R (-0.4107R)` | **`-0.4803R vs -0.2258R (+0.2545R)`** | Diagnostic (`TriggerKnifeLock`) | `+0.2545R` knife avoidance |
| **Overall H3 Verdict** | **FAIL (KILLED)** | **FAIL (KILLED)** | `G_oos >= 15` AND `CI_low >= 0` | **FAIL (KILLED)** |

## 4. Structural Setup Performance (`wave_k_be=0.8` Full-Friction NetR — Phase 2b 12-Month Sample)

| Cohort | Setups (N) | Wave Clusters (G) | Mean NetR | Cluster SE | t-stat | 95% CI |
|---|---:|---:|---:|---:|---:|---:|
| `Box Maturity Breakouts (BOX_BREAKOUT)` | 123 | 35 | **`-0.3790R`** | `0.0865` | `-4.38` | `[-0.5549, -0.2031]` |
| `Second-Wave Stage-4 Re-Break (SECOND_WAVE_STAGE4)` | 867 | 40 | **`-0.2258R`** | `0.0782` | `-2.89` | `[-0.3841, -0.0676]` |
| `Combined In-Sample Setups` | 990 | 40 | **`-0.2449R`** | `0.0733` | `-3.34` | `[-0.3932, -0.0965]` |
| **`Walk-Forward Out-of-Sample (OOS)`** | 949 | **39** | **`-0.2528R`** | `0.0765` | `-3.30` | **`[-0.4077, -0.0978]`** |

## 5. Left-Side Knife Catching vs Stage-4 Right-Side Re-Break Counterfactual

| Entry Timing | Mean Full-Friction NetR | Difference (`Stage-4 - Left-Side Knife`) |
|---|---:|---:|
| `Stage-2 Left-Side Falling Knife (Unconfirmed Pullback)` | `-0.4803R` | — |
| `Stage-4 Right-Side Confirmed Re-Break (k=3 Lag)` | `-0.2258R` | **`+0.2545R`** |

## 6. H3 Kill-Rule Evaluation & Shadow Wiring Decision

| Kill Criterion | Observed Value | Required Bar | Triggered? |
|---|---:|---:|---|
| `OOS Independent Wave Clusters (G_oos)` | **`39`** | `>= 15` | **NO (POWER TARGET MET: 39 >= 15)** |
| `OOS NetR 95% CI Lower Bound` | **`-0.4077R`** | `>= 0.0000R` | **YES (KILLED: -0.4077R < 0.0000R)** |

- **H3 Overall Verdict**: **FAIL** (`KILLED`)
- **Shadow Fields on `CycleMap` (`RoomToResistanceR`, `BoxTouchCount`, `ATRCompressionRatio`, `TriggerKnifeLock`)**: **DO NOT WIRE (KILLED BY ITEM 7 RULE)**
- **Fallback Deliverable (Item 7, Unchanged from PR #7)**: Reference existing `kairos-desk` manual watch-range entry specification (`tools/research/H3_watch_range_spec.md` & `docs/90-engineering/specs/kairos-desk-watch-range-spec.md`).
