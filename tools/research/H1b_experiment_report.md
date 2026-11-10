# Experiment H1b — BTC Resonance vs Lagging Catchup & Crowding Fakeout (2026-11-10)

- **Verdict**: **FAIL** (`FAIL: RESONANCE (-0.3905R, N=29) vs LAGGING_CATCHUP (-0.6381R, N=54) Delta NetR = +0.2477R (Cluster SE = 0.5028, one-sided p = 0.3281, pre-registered bar requires Delta > 0 and p < 0.05).`)
- **Dataset**: OKX USDT-SWAP PIT 1m Replay (18-Day Multi-Cluster Sample)
- **Candidate Signals (`N`)**: 162 (across 7 `<24h`-merged `wave_cluster_id` clusters)
- **Canonical Holding Policy**: `wave_k_be=0.8` (Phase 1 P0 plateau winner, full-friction NetR)
- **Ex-Ante Causal Guard**: `BTCSyncMode` and `CrowdingPct` use ONLY BTC impulse timestamps and 8h funding / breadth observations at `<= entry_ts` (`validate_ex_ante_grouping` enforced).

## 1. Pre-Registered Acceptance Bar

- **BTC Sync Resonance Bar**: After a BTC upward `market_impulse`, alts breaking out in sync within `[0, 15m]` (`RESONANCE`) must achieve a cluster-adjusted mean NetR significantly above laggards starting `[2h, 12h]` later (`LAGGING_CATCHUP`): `Delta NetR = Mean(RESONANCE) - Mean(LAGGING_CATCHUP) > 0` with one-sided `p < 0.05` (CR1 `wave_cluster_id` robust SE).
- **Crowding & Fakeout Diagnostic**: Measure alt long-breakout fakeout (stop-hit) rate when BTC is near rolling highs (`<= 0.6%` from 4h high) and market-wide funding/crowding is at an extreme percentile (`CrowdingPct >= 0.75`).

## 2. BTCSyncMode Group Performance (`wave_k_be=0.8`)

| BTCSyncMode | Window After BTC Upward Impulse | N | Clusters (G) | Mean NetR | Cluster SE | t-stat | 95% CI |
|---|---|---:|---:|---:|---:|---:|---:|
| `RESONANCE` | `[0, 15m]` synchronous breakout | 29 | 4 | **-0.3905R** | 0.5298 | -0.74 | [-2.0765, +1.2955] |
| `LAGGING_CATCHUP` | `[2h, 12h]` lagging catch-up | 54 | 4 | **-0.6381R** | 0.0305 | -20.89 | [-0.7353, -0.5409] |
| `NONE` | No prior BTC upward impulse in window | 79 | 7 | **-0.3708R** | 0.0967 | -3.84 | [-0.6074, -0.1343] |

## 3. H1b Hypothesis Test (`RESONANCE` vs `LAGGING_CATCHUP`)

| Comparison | `RESONANCE` Mean | `LAGGING_CATCHUP` Mean | `Delta NetR` | Cluster SE | t-stat | One-Sided p | Verdict |
|---|---:|---:|---:|---:|---:|---:|---|
| `RESONANCE - LAGGING_CATCHUP` | `-0.3905R` | `-0.6381R` | **`+0.2477R`** | `0.5028` | `+0.49` | **`0.3281`** | **FAIL** |

## 4. BTC Near Rolling Highs + Extreme Crowding Fakeout Rate

| Condition | Long Breakouts (N) | Preset Stop-Hit (Fakeout) Rate | Mean Full-Friction NetR |
|---|---:|---:|---:|
| `BTC Near High + Extreme Crowding (CrowdingPct >= 0.75)` | 20 | **`80.0%`** | `-0.7868R` |
| `Baseline Long Breakouts (Uncrowded / Non-High)` | 55 | **`47.3%`** | `-0.3107R` |
| **Difference (`Crowded - Baseline`)** | — | **`+32.7 pp`** | `-0.4761R` |

## 5. Shadow Wiring Decision (H1b)

- **H1b Overall Verdict**: **FAIL**
- **Shadow Fields `BTCSyncMode` & `CrowdingPct` on `MarketSnapshot` / `MarketPulseSnapshotRecord`**: **DO NOT WIRE (FALSIFIED / MISSED BAR)**
