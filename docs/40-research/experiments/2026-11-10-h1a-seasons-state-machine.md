# Experiment H1a — Causal Seasons State Machine & Winter Defense (2026-11-10)

- **Verdict**: **FAIL** (`FAIL: OOS Delta NetR = +0.0371R < +0.25R bar; OOS one-sided p = 0.3432 >= 0.05 bar`)
- **Dataset**: OKX USDT-SWAP PIT 1m Replay (12-Month Expanded Sample: 2025-11-09 -> 2026-11-09, 40 Slices, 27 PIT Symbols)
- **Canonical Holding Policy**: `wave_k_be=0.8` (Phase 1 P0 plateau winner, full-friction NetR)
- **Ex-Ante Causal Guard**: `SeasonRegime` at `t` uses ONLY rolling 30m/2h cross-sectional breadth and quote-volume ratios closed at `<= t` (`validate_ex_ante_grouping` enforced; zero manual labels).

## 1. Achieved Sample Window & Statistical Power Target (Reported First)

| Power / Coverage Metric | Phase 2 (18-Day Sample, PR #7) | Phase 2b Pre-Registered Target | Phase 2b Achieved (12-Month Sample) | Power Status |
|---|---:|---:|---:|---|
| **Contiguous Calendar Span** | `18 days` (`2026-10-22 -> 2026-11-09`) | `12 months` (`>= 6m` floor) | **`366 days / 12.0 months` (`2025-11-09 -> 2026-11-09`)** | **MET (`12.0m`)** |
| **1m Replay Slices (`>24h` Spacing)** | `4 slices` | `40 slices` (`~9.15d` step) | **`40 slices` (`758,013` 1m bars)** | **MET** |
| **Candidate Pullback Tickets (`N`)** | `162` | Scale with window | **`1550`** (`9.57x` Phase 2) | **MET** |
| **Merged Wave Clusters (`G`, `<24h` rule)** | `7` | `G >= 25` (materially `> 7`) | **`40`** (`5.71x` Phase 2) | **MET** |
| **OOS Regime Wave Clusters (`G_oos`)** | `7` | `G >= 25` | **`40`** (`SUMMER/AUTUMN G=39`, `WINTER/SPRING G=40`) | **MET** |

## 2. Pre-Registered Acceptance Bars (Unchanged from Phase 2)

1. **Regime Edge Bar**: Walk-forward out-of-sample cluster-adjusted mean NetR difference `Delta NetR = Mean(SUMMER/AUTUMN) - Mean(WINTER/SPRING) >= +0.25R` with one-sided `p < 0.05` (CR1 `wave_cluster_id` robust SE).
2. **Winter Defense Bar**: Applying a `0.25x` risk multiplier (or `0.0x` silence) in `WINTER` retains `>= 80%` of cumulative net profit while cutting maximum drawdown by `>= 30%`.

## 3. Side-by-Side Comparison: Phase 2 (18-Day) vs Phase 2b (12-Month Expanded Sample)

| Metric | Phase 2 (18-Day, PR #7) | Phase 2b (12-Month Expanded) | Pre-Registered Bar | Phase 2b Status |
|---|---:|---:|---:|---|
| `Total Candidate Signals (N)` | `162` | **`1550`** | — | `9.57x` expansion |
| `Merged Wave Clusters (G)` | `7` | **`40`** | `G >= 25` | **PASS (`G = 40`)** |
| `SPRING Mean NetR (N, G)` | `-0.1230R (1, 1)` | **`-0.5153R (43, 21)`** | Diagnostic | — |
| `SUMMER Mean NetR (N, G)` | `-0.6467R (22, 7)` | **`-0.1961R (264, 36)`** | Diagnostic | — |
| `AUTUMN Mean NetR (N, G)` | `-0.2384R (30, 6)` | **`-0.2631R (273, 37)`** | Diagnostic | — |
| `WINTER Mean NetR (N, G)` | `-0.4915R (109, 7)` | **`-0.2458R (970, 40)`** | Diagnostic | — |
| `In-Sample Delta NetR (SUMMER/AUTUMN - WINTER/SPRING)` | `+0.0771R (SE=0.1905, p=0.3499)` | **`+0.0271R (SE=0.0871, p=0.3787)`** | `>= +0.25R, p < 0.05` | FAIL |
| **`Walk-Forward OOS Delta NetR`** | **`+0.0885R (SE=0.1395, p=0.2747)`** | **`+0.0371R (SE=0.0913, p=0.3432)`** | **`>= +0.25R, p < 0.05`** | **FAIL** |
| `Winter 0.25x Profit Retention / Max DD Cut` | `153.5% / 53.4%` | **`146.5% / 47.1%`** | `>= 80% / >= 30%` | **PASS** |
| `Winter 0.0x Profit Retention / Max DD Cut` | `171.4% / 71.2%` | **`162.1% / 62.7%`** | `>= 80% / >= 30%` | **PASS** |
| **Overall H1a Verdict** | **FAIL** | **FAIL** | Both bars required | **FAIL** |

## 4. Per-Season Breakdown (`wave_k_be=0.8` Full-Friction NetR — Phase 2b 12-Month Sample)

| SeasonRegime | N | Wave Clusters (G) | Mean NetR | Cluster SE | t-stat | 95% CI |
|---|---:|---:|---:|---:|---:|---:|
| `SPRING` | 43 | 21 | **-0.5153R** | 0.1438 | -3.58 | [-0.8154, -0.2153] |
| `SUMMER` | 264 | 36 | **-0.1961R** | 0.1026 | -1.91 | [-0.4047, +0.0124] |
| `AUTUMN` | 273 | 37 | **-0.2631R** | 0.0792 | -3.32 | [-0.4239, -0.1023] |
| `WINTER` | 970 | 40 | **-0.2458R** | 0.0671 | -3.66 | [-0.3816, -0.1101] |

## 5. H1a Regime Comparison (`SUMMER/AUTUMN` vs `WINTER/SPRING`)

| Evaluation Split | `SUMMER/AUTUMN` Mean (N, G) | `WINTER/SPRING` Mean (N, G) | `Delta NetR` | Cluster SE | t-stat | One-Sided p | Bar (`>= +0.25R, p < 0.05`) |
|---|---:|---:|---:|---:|---:|---:|---|
| **In-Sample Causal** | `-0.2302R` (537, 39) | `-0.2573R` (1013, 40) | **`+0.0271R`** | `0.0871` | `+0.31` | `0.3787` | FAIL |
| **Walk-Forward OOS** | `-0.2249R` (592, 39) | `-0.2621R` (958, 40) | **`+0.0371R`** | `0.0913` | `+0.41` | `0.3432` | **FAIL** |

## 6. Winter Defense Counterfactual (`WINTER` Risk Multiplier)

| Policy | Cumulative NetR | Max Drawdown (R) | Profit Retention vs 1.0x | Max DD Reduction vs 1.0x | Bar (`>=80% Profit, >=30% DD Cut`) |
|---|---:|---:|---:|---:|---|
| `Baseline (1.0x all seasons)` | `-384.2349R` | `391.9690R` | `100.0%` | `0.0%` | — |
| `Winter 0.25x Risk Multiplier` | `-205.3843R` | `207.3178R` | **`146.5%`** | **`47.1%`** | PASS |
| `Winter 0.0x (Silence)` | `-145.7674R` | `146.2237R` | **`162.1%`** | **`62.7%`** | PASS |

## 7. Shadow Wiring Decision (H1a)

- **H1a Overall Verdict**: **FAIL**
- **Shadow Field `SeasonRegime` on `MarketSnapshot` / `MarketPulseSnapshotRecord`**: **DO NOT WIRE (FALSIFIED / MISSED BAR)**
