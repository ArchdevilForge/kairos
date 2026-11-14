# Experiment P0 — Path-Dependent NetR Calibration (2026-11-10)

- **Dataset**: OKX USDT-SWAP PIT 1m Replay
- **Candidate Signals (`N`)**: 35
- **Merged Wave Clusters (`G`, <24h rule)**: 3
- **Friction Model**: Taker 5 bps / Maker 2 bps (>=1 tick trade-through), dynamic slippage `s0=1.5 bps (majors) / 5.0 bps (alts) + 0.5*(ATR_1m/P)*sqrt(V/ADV_5m)`, 8h UTC funding ticks, stop exits always charged taker + dynamic slippage
- **Intra-bar Resolution**: Worst-case `stop-first`; breakeven arms at confirming minute close and activates at next minute open

## 1. Holding Policy Comparison (Cluster-Robust SE by `wave_cluster_id`)

| Policy | N | Clusters (G) | Mean NetR | Cluster SE | t-stat | 95% CI |
|---|---:|---:|---:|---:|---:|---:|
| `fixed_5m` | 35 | 3 | **-0.1663R** | 0.0495 | -3.36 | [-0.3794, +0.0468] |
| `fixed_30m` | 35 | 3 | **-0.1746R** | 0.0642 | -2.72 | [-0.4507, +0.1014] |
| `fixed_+1.5R/-1.0R` | 35 | 3 | **+0.1349R** | 0.3232 | +0.42 | [-1.2557, +1.5255] |
| `wave_k_be=0.8` | 35 | 3 | **+0.0827R** | 0.2734 | +0.30 | [-1.0935, +1.2588] |
| `wave_k_be=1.2` | 35 | 3 | **+0.0175R** | 0.3158 | +0.06 | [-1.3414, +1.3763] |
| `wave_k_be=1.5` | 35 | 3 | **+0.0134R** | 0.3177 | +0.04 | [-1.3536, +1.3805] |
| `wave_k_be=2.0` | 35 | 3 | **+0.0246R** | 0.3124 | +0.08 | [-1.3197, +1.3690] |
| `wave_k_be=inf` | 35 | 3 | **+0.0246R** | 0.3124 | +0.08 | [-1.3197, +1.3690] |

## 2. Parameter Plateau & Stop-Condition Verdict

- **Wave Plateau Winner**: `k_be = 0.8` (mean `+0.0827R`, `is_plateau=True` — selected flat plateau optimum k_be=0.8 (plateau grid=[0.8, 1.2, 1.5, 2.0, inf]))
- **Best Overall Policy**: `fixed_+1.5R/-1.0R` (`+0.1349R`)
- **Stop Condition (`all policies < -0.15R`)**: **PASSED — PROCEED TO ITEMS 7–14**

## 3. Protective-Stop Counterfactual (`<5m` Exits, Work Item 6)

- **Qualifying `<5m` Protective/Fakeout Exits**: 3 (across 2 wave clusters)
- **Mean `NetR_actual`**: `-0.4852R`
- **Mean `NetR_hold_to_5m`**: `-0.4978R`
- **Mean `SavedR` (`NetR_actual - NetR_hold_to_5m`)**: **`+0.0126R`** (Cluster SE `0.0602`)
