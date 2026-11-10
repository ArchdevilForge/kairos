# Experiment H1b — BTC Resonance vs Lagging Catchup & Crowding Fakeout (2026-11-10)

- **Verdict**: **FAIL** (`FAIL: RESONANCE (-0.1917R, N=216) vs LAGGING_CATCHUP (-0.2355R, N=850) Delta NetR = +0.0438R (Cluster SE = 0.1629, one-sided p = 0.3948, pre-registered bar requires Delta > 0 and p < 0.05).`)
- **Dataset**: OKX USDT-SWAP PIT 1m Replay (12-Month Expanded Sample: 2025-11-09 -> 2026-11-09, 40 Slices, 27 PIT Symbols)
- **Canonical Holding Policy**: `wave_k_be=0.8` (Phase 1 P0 plateau winner, full-friction NetR)
- **Ex-Ante Causal Guard**: `BTCSyncMode` and `CrowdingPct` use ONLY BTC impulse timestamps and 8h funding / breadth observations at `<= entry_ts` (`validate_ex_ante_grouping` enforced).

## 1. Achieved Sample Window & Statistical Power Target (Reported First)

| Power / Coverage Metric | Phase 2 (18-Day Sample, PR #7) | Phase 2b Pre-Registered Target | Phase 2b Achieved (12-Month Sample) | Power Status |
|---|---:|---:|---:|---|
| **Contiguous Calendar Span** | `18 days` (`2026-10-22 -> 2026-11-09`) | `12 months` (`>= 6m` floor) | **`366 days / 12.0 months` (`2025-11-09 -> 2026-11-09`)** | **MET (`12.0m`)** |
| **1m Replay Slices (`>24h` Spacing)** | `4 slices` | `40 slices` (`~9.15d` step) | **`40 slices` (`758,013` 1m bars)** | **MET** |
| **Candidate Pullback Tickets (`N`)** | `162` | Scale with window | **`1550`** (`9.57x` Phase 2) | **MET** |
| **BTC Upward Impulse Clusters (`G_impulse`)** | `4` | `G >= 15` | **`35`** (`RESONANCE G=33`, `LAGGING_CATCHUP G=35`) | **MET** |
| **Total Merged Wave Clusters (`G_total`)** | `7` | `>= 25` | **`40`** | **MET** |

## 2. Pre-Registered Acceptance Bar (Unchanged from Phase 2)

- **BTC Sync Resonance Bar**: After a BTC upward `market_impulse`, alts breaking out in sync within `[0, 15m]` (`RESONANCE`) must achieve a cluster-adjusted mean NetR significantly above laggards starting `[2h, 12h]` later (`LAGGING_CATCHUP`): `Delta NetR = Mean(RESONANCE) - Mean(LAGGING_CATCHUP) > 0` with one-sided `p < 0.05` (CR1 `wave_cluster_id` robust SE).
- **Crowding & Fakeout Diagnostic**: Measure alt long-breakout fakeout (stop-hit) rate when BTC is near rolling highs (`<= 0.6%` from 4h high) and market-wide funding/crowding is at an extreme percentile (`CrowdingPct >= 0.75`).

## 3. Side-by-Side Comparison: Phase 2 (18-Day) vs Phase 2b (12-Month Expanded Sample)

| Metric | Phase 2 (18-Day, PR #7) | Phase 2b (12-Month Expanded) | Pre-Registered Bar | Phase 2b Status |
|---|---:|---:|---:|---|
| `Total Candidate Signals (N)` | `162` | **`1550`** | — | `9.57x` expansion |
| `BTC Impulse Wave Clusters (G)` | `4` (underpowered) | **`35`** | `G >= 15` | **PASS (`G = 35`)** |
| `RESONANCE [0, 15m] Mean NetR (N, G)` | `-0.3905R (29, 4)` | **`-0.1917R (216, 33)`** | — | `SE = 0.1581` |
| `LAGGING_CATCHUP [2h, 12h] Mean NetR (N, G)` | `-0.6381R (54, 4)` | **`-0.2355R (850, 35)`** | — | `SE = 0.0764` |
| `NONE Mean NetR (N, G)` | `-0.3708R (79, 7)` | **`-0.2947R (484, 38)`** | — | `SE = 0.0670` |
| **`Delta NetR (RESONANCE - LAGGING_CATCHUP)`** | **`+0.2477R (SE=0.5028, t=+0.49, p=0.3281)`** | **`+0.0438R (SE=0.1629, t=+0.27, p=0.3948)`** | **`Delta > 0, p < 0.05`** | **FAIL** |
| `Crowded Near-High Fakeout Rate (N)` | `80.0% (20, -0.7868R)` | **`60.8% (79, -0.3900R)`** | Diagnostic | `+4.7 pp` vs baseline |
| `Baseline Long Fakeout Rate (N)` | `47.3% (55, -0.3107R)` | **`56.0% (662, -0.2261R)`** | Diagnostic | — |
| **Overall H1b Verdict** | **FAIL** | **FAIL** | `Delta > 0, p < 0.05` | **FAIL** |

## 4. BTCSyncMode Group Performance (`wave_k_be=0.8` — Phase 2b 12-Month Sample)

| BTCSyncMode | Window After BTC Upward Impulse | N | Clusters (G) | Mean NetR | Cluster SE | t-stat | 95% CI |
|---|---|---:|---:|---:|---:|---:|---:|
| `RESONANCE` | `[0, 15m]` synchronous breakout | 216 | 33 | **-0.1917R** | 0.1581 | -1.21 | [-0.5139, +0.1304] |
| `LAGGING_CATCHUP` | `[2h, 12h]` lagging catch-up | 850 | 35 | **-0.2355R** | 0.0764 | -3.08 | [-0.3908, -0.0802] |
| `NONE` | No prior BTC upward impulse in window | 484 | 38 | **-0.2947R** | 0.0670 | -4.40 | [-0.4307, -0.1588] |

## 5. H1b Hypothesis Test (`RESONANCE` vs `LAGGING_CATCHUP`)

| Comparison | `RESONANCE` Mean | `LAGGING_CATCHUP` Mean | `Delta NetR` | Cluster SE | t-stat | One-Sided p | Verdict |
|---|---:|---:|---:|---:|---:|---:|---|
| `RESONANCE - LAGGING_CATCHUP` | `-0.1917R` | `-0.2355R` | **`+0.0438R`** | `0.1629` | `+0.27` | **`0.3948`** | **FAIL** |

## 6. BTC Near Rolling Highs + Extreme Crowding Fakeout Rate

| Condition | Long Breakouts (N) | Preset Stop-Hit (Fakeout) Rate | Mean Full-Friction NetR |
|---|---:|---:|---:|
| `BTC Near High + Extreme Crowding (CrowdingPct >= 0.75)` | 79 | **`60.8%`** | `-0.3900R` |
| `Baseline Long Breakouts (Uncrowded / Non-High)` | 662 | **`56.0%`** | `-0.2261R` |
| **Difference (`Crowded - Baseline`)** | — | **`+4.7 pp`** | `-0.1639R` |

## 7. Shadow Wiring Decision (H1b)

- **H1b Overall Verdict**: **FAIL**
- **Shadow Fields `BTCSyncMode` & `CrowdingPct` on `MarketSnapshot` / `MarketPulseSnapshotRecord`**: **DO NOT WIRE (FALSIFIED / MISSED BAR)**
