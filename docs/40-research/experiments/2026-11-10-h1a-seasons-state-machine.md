# Experiment H1a — Causal Seasons State Machine & Winter Defense (2026-11-10)

- **Verdict**: **FAIL** (`FAIL: OOS Delta NetR = +0.0885R < +0.25R bar; OOS one-sided p = 0.2747 >= 0.05 bar`)
- **Dataset**: OKX USDT-SWAP PIT 1m Replay (18-Day Multi-Cluster Sample)
- **Candidate Signals (`N`)**: 162
- **Merged Wave Clusters (`G`, <24h rule)**: 7
- **Canonical Holding Policy**: `wave_k_be=0.8` (Phase 1 P0 plateau winner, full-friction NetR)
- **Ex-Ante Causal Guard**: `SeasonRegime` at `t` uses ONLY rolling 30m/2h cross-sectional breadth and quote-volume ratios closed at `<= t` (`validate_ex_ante_grouping` enforced; zero manual labels).

## 1. Pre-Registered Acceptance Bars

1. **Regime Edge Bar**: Walk-forward out-of-sample cluster-adjusted mean NetR difference `Delta NetR = Mean(SUMMER/AUTUMN) - Mean(WINTER/SPRING) >= +0.25R` with one-sided `p < 0.05` (CR1 `wave_cluster_id` robust SE).
2. **Winter Defense Bar**: Applying a `0.25x` risk multiplier (or `0.0x` silence) in `WINTER` retains `>= 80%` of cumulative net profit while cutting maximum drawdown by `>= 30%`.

## 2. Per-Season Breakdown (`wave_k_be=0.8` Full-Friction NetR)

| SeasonRegime | N | Wave Clusters (G) | Mean NetR | Cluster SE | t-stat | 95% CI |
|---|---:|---:|---:|---:|---:|---:|
| `SPRING` | 1 | 1 | **-0.1230R** | n/a | n/a | n/a |
| `SUMMER` | 22 | 7 | **-0.6467R** | 0.1046 | -6.18 | [-0.9025, -0.3908] |
| `AUTUMN` | 30 | 6 | **-0.2384R** | 0.1748 | -1.36 | [-0.6877, +0.2109] |
| `WINTER` | 109 | 7 | **-0.4915R** | 0.1858 | -2.65 | [-0.9462, -0.0369] |

## 3. H1a Regime Comparison (`SUMMER/AUTUMN` vs `WINTER/SPRING`)

| Evaluation Split | `SUMMER/AUTUMN` Mean (N, G) | `WINTER/SPRING` Mean (N, G) | `Delta NetR` | Cluster SE | t-stat | One-Sided p | Bar (`>= +0.25R, p < 0.05`) |
|---|---:|---:|---:|---:|---:|---:|---|
| **In-Sample Causal** | `-0.4111R` (52, 7) | `-0.4882R` (110, 7) | **`+0.0771R`** | `0.1905` | `+0.40` | `0.3499` | FAIL |
| **Walk-Forward OOS** | `-0.4039R` (53, 7) | `-0.4924R` (109, 7) | **`+0.0885R`** | `0.1395` | `+0.63` | `0.2747` | **FAIL** |

## 4. Winter Defense Counterfactual (`WINTER` Risk Multiplier)

| Policy | Cumulative NetR | Max Drawdown (R) | Profit Retention vs 1.0x | Max DD Reduction vs 1.0x | Bar (`>=80% Profit, >=30% DD Cut`) |
|---|---:|---:|---:|---:|---|
| `Baseline (1.0x all seasons)` | `-75.0792R` | `75.2049R` | `100.0%` | `0.0%` | — |
| `Winter 0.25x Risk Multiplier` | `-34.8962R` | `35.0219R` | **`153.5%`** | **`53.4%`** | PASS |
| `Winter 0.0x (Silence)` | `-21.5018R` | `21.6276R` | **`171.4%`** | **`71.2%`** | PASS |

## 5. Shadow Wiring Decision (H1a)

- **H1a Overall Verdict**: **FAIL**
- **Shadow Field `SeasonRegime` on `MarketSnapshot` / `MarketPulseSnapshotRecord`**: **DO NOT WIRE (FALSIFIED / MISSED BAR)**
