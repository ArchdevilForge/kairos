# Experiment H2 — Dynamic Leader Scoring for Directional Ranker (2026-11-10)

- **Verdict**: **FAIL** (`FAIL: Top-3 New Score (-0.4210R) vs Current Ranker Top-3 (-0.3873R) net improvement = -0.0337R < +0.18R pre-registered bar.`)
- **Dataset**: OKX USDT-SWAP PIT 1m Replay (21-Symbol PIT Pool incl. New/Illiquid Swaps)
- **Evaluated Multi-Candidate Wakes**: 74 (across 7 `<24h`-merged `wave_cluster_id` clusters, total directional candidates `N = 376`)
- **Canonical Holding Policy**: `wave_k_be=0.8` (full friction + square-root market impact slippage for illiquid/new listings)
- **Ex-Ante Causal Guard**: All 5 ranker factors computed strictly at `wake_ts <= entry_ts` (`validate_ex_ante_grouping` enforced; PIT universe only).

## 1. Pre-Registered Acceptance Bar & Kill Rule

1. **Top-3 Net Improvement Bar**: Top-3 candidates selected by the new Dynamic Leader Score vs Top-3 by the CURRENT ranker (`internal/ranker/ranker.go`), evaluated on the same wake tickets under full friction + impact slippage, must achieve `Delta NetR >= +0.18R`.
2. **`次新` (New-Listing) KILL Rule**: If the `次新` (`listing_age_cixin`, `<= 90d` since `list_ts`) factor's excess return vanishes after slippage (`Net Excess R <= 0` or degrades Top-3 NetR), remove `listing_age_cixin` from the factor set and report the kill.

## 2. `次新` (New-Listing) Factor Slippage Kill-Rule Test

| Cohort | Candidate Tickets (N) | Mean Gross R (Zero Friction) | Mean Friction + Impact Slippage Cost | Mean Full-Friction NetR |
|---|---:|---:|---:|---:|
| `New Listings (<= 90d age)` | 75 | `-0.3006R` | **`0.1409R`** | **`-0.4415R`** |
| `Established Swaps (> 90d age)` | 301 | `-0.2477R` | **`0.1889R`** | **`-0.4365R`** |
| **Excess (`New - Established`)** | — | `-0.0530R` | `-0.0480R` | **`-0.0050R`** |

- **Kill Rule Decision**: **TRIGGERED — REMOVED `listing_age_cixin`**
- **Detail**: KILLED: 次新 (new-listing <=90d) standalone net excess return after full friction + impact slippage is -0.0050R (mean NetR -0.4415R vs established -0.4365R; friction+slippage drag 0.1409R vs 0.1889R; 5-factor Top-3 mean -0.4348R vs 4-factor pruned -0.4210R). Removed `listing_age_cixin` per pre-registered H2 KILL rule.

## 3. Top-3 Ranker Comparison (Same Wakes, Full Friction + Impact Slippage)

| Ranker Variant | Top-3 Tickets (N) | Clusters (G) | Mean NetR | Cluster SE | t-stat | 95% CI | Improvement vs Current |
|---|---:|---:|---:|---:|---:|---:|---:|
| `Current Ranker (internal/ranker baseline)` | 222 | 7 | **`-0.3873R`** | `0.1848` | `-2.09` | `[-0.8396, +0.0651]` | `0.0000R` |
| `New Score (5-Factor incl. 次新)` | 222 | 7 | **`-0.4348R`** | `0.1747` | `-2.49` | `[-0.8623, -0.0072]` | `-0.0475R` |
| **`New Score (4-Factor Pruned, 次新 Removed)`** | 222 | 7 | **`-0.4210R`** | `0.1857` | `-2.27` | `[-0.8753, +0.0333]` | **`-0.0337R`** |

## 4. H2 Hypothesis Verdict (`New Pruned Top-3` vs `Current Ranker Top-3`)

| Metric | Value | Pre-Registered Bar | Status |
|---|---:|---:|---|
| `Gross Top-3 Improvement (Before Slippage)` | `-0.0406R` | — | Diagnostic |
| **`Net Top-3 Improvement (After Full Friction + Slippage)`** | **`-0.0337R`** | **`>= +0.1800R`** | **FAIL** |
| `Cluster-Robust SE (wave_cluster_id)` | `0.0427R` | — | `G = 7` |
| `t-statistic / One-Sided p-value` | `t = -0.79, p = 0.7700` | — | — |

## 5. Shadow Wiring Decision (H2)

- **H2 Overall Verdict**: **FAIL**
- **Passing Factors Wired as Shadow Fields on `DirectionalCandidate`**: `NONE`
- **Killed Factors Excluded from Wiring**: `listing_age_cixin`
