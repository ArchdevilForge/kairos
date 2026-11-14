# Experiment H2 — Dynamic Leader Scoring for Directional Ranker (2026-11-10)

- **Verdict**: **FAIL** (`FAIL: Top-3 New Score (-0.1615R) vs Current Ranker Top-3 (-0.1833R) net improvement = +0.0218R < +0.18R pre-registered bar.`)
- **Dataset**: OKX USDT-SWAP PIT 1m Replay (12-Month Expanded Sample: 2025-11-09 -> 2026-11-09, 40 Slices, 27 PIT Symbols incl. 12 2025-2026 New/Illiquid Swaps)
- **Canonical Holding Policy**: `wave_k_be=0.8` (full friction + square-root market impact slippage for illiquid/new listings)
- **Ex-Ante Causal Guard**: All 5 ranker factors computed strictly at `wake_ts <= entry_ts` (`validate_ex_ante_grouping` enforced; PIT universe only).

## 1. Achieved Sample Window & Statistical Power Target (Reported First)

| Power / Coverage Metric | Phase 2 (18-Day Sample, PR #7) | Phase 2b Pre-Registered Target | Phase 2b Achieved (12-Month Sample) | Power Status |
|---|---:|---:|---:|---|
| **Contiguous Calendar Span** | `18 days` (`2026-10-22 -> 2026-11-09`) | `12 months` (`>= 6m` floor) | **`366 days / 12.0 months` (`2025-11-09 -> 2026-11-09`)** | **MET (`12.0m`)** |
| **1m Replay Slices (`>24h` Spacing)** | `4 slices` | `40 slices` (`~9.15d` step) | **`40 slices` (`758,013` 1m bars)** | **MET** |
| **Evaluated Multi-Candidate Wakes** | `74` | Scale with window | **`522`** (`7.05x` Phase 2) | **MET** |
| **Scored Directional Candidates (`N`)** | `376` (`Top-3 N = 222`) | Scale with window | **`2818`** (`Top-3 N = 1566`, `7.49x` Phase 2) | **MET** |
| **Merged Wave Clusters (`G`, `<24h` rule)** | `7` | `G >= 25` | **`39`** (`5.57x` Phase 2) | **MET (`G = 39`)** |
| **`次新` (`<= 90d` Listing Age) Candidates** | `75` | Scale across 2025–2026 listings | **`272`** (`Established N = 2546`) | **MET** |

## 2. Pre-Registered Acceptance Bar & Kill Rule (Unchanged from Phase 2)

1. **Top-3 Net Improvement Bar**: Top-3 candidates selected by the new Dynamic Leader Score vs Top-3 by the CURRENT ranker (`internal/ranker/ranker.go`), evaluated on the same wake tickets under full friction + impact slippage, must achieve `Delta NetR >= +0.18R`.
2. **`次新` (New-Listing) KILL Rule**: If the `次新` (`listing_age_cixin`, `<= 90d` since `list_ts`) factor's excess return vanishes after slippage (`Net Excess R <= 0` or degrades Top-3 NetR), remove `listing_age_cixin` from the factor set and report the kill.

## 3. Side-by-Side Comparison: Phase 2 (18-Day) vs Phase 2b (12-Month Expanded Sample)

| Metric | Phase 2 (18-Day, PR #7) | Phase 2b (12-Month Expanded) | Pre-Registered Bar | Phase 2b Status |
|---|---:|---:|---:|---|
| `Multi-Candidate Wakes / Total Candidates (N)` | `74 / 376` | **`522 / 2818`** | — | `7.49x` expansion |
| `Merged Wave Clusters (G)` | `7` | **`39`** | `G >= 25` | **PASS (`G = 39`)** |
| `New Listings (<= 90d) Mean Gross / Net R (N)` | `-0.3006R / -0.4415R (75)` | **`+0.1539R / -0.0385R (272)`** | — | `Slip = 0.1924R` |
| `Established (> 90d) Mean Gross / Net R (N)` | `-0.2477R / -0.4365R (301)` | **`+0.0655R / -0.1628R (2546)`** | — | `Slip = 0.2283R` |
| `次新 Standalone Net Excess (New - Established)` | `-0.0050R (KILLED)` | **`+0.1243R (RETAINED)`** | `Kill if <= 0.0000R` | **NOT KILLED** |
| `Current Ranker Top-3 Mean NetR (N, G)` | `-0.3873R (222, 7)` | **`-0.1833R (1566, 39)`** | Baseline | `SE = 0.0542` |
| `New 5-Factor (incl. 次新) Top-3 Mean NetR` | `-0.4348R (diff -0.0475R)` | **`-0.1615R (diff +0.0218R)`** | `Delta >= +0.18R` | FAIL |
| `New 4-Factor (Pruned, 次新 Removed) Top-3 Mean NetR` | `-0.4210R (diff -0.0337R)` | **`-0.1705R (diff +0.0128R)`** | `Delta >= +0.18R` | FAIL |
| **`Active New Score vs Current Ranker Delta NetR`** | **`-0.0337R (SE=0.0427, p=0.7700)`** | **`+0.0218R (SE=0.0277, p=0.2183)`** | **`>= +0.1800R`** | **FAIL** |
| **Overall H2 Verdict** | **FAIL** | **FAIL** | `Delta NetR >= +0.18R` | **FAIL** |

## 4. `次新` (New-Listing) Factor Slippage Kill-Rule Test (Phase 2b 12-Month Sample)

| Cohort | Candidate Tickets (N) | Mean Gross R (Zero Friction) | Mean Friction + Impact Slippage Cost | Mean Full-Friction NetR |
|---|---:|---:|---:|---:|
| `New Listings (<= 90d age)` | 272 | `+0.1539R` | **`0.1924R`** | **`-0.0385R`** |
| `Established Swaps (> 90d age)` | 2546 | `+0.0655R` | **`0.2283R`** | **`-0.1628R`** |
| **Excess (`New - Established`)** | — | `+0.0883R` | `-0.0359R` | **`+0.1243R`** |

- **Kill Rule Decision**: **NOT TRIGGERED (Standalone Excess +0.1243R > 0R, though Absolute NetR is Still Negative -0.0385R)**
- **Detail**: RETAINED: 次新 standalone net excess return after slippage is +0.1243R > 0.

## 5. Top-3 Ranker Comparison (Same Wakes, Full Friction + Impact Slippage)

| Ranker Variant | Top-3 Tickets (N) | Clusters (G) | Mean NetR | Cluster SE | t-stat | 95% CI | Improvement vs Current |
|---|---:|---:|---:|---:|---:|---:|---:|
| `Current Ranker (internal/ranker baseline)` | 1566 | 39 | **`-0.1833R`** | `0.0542` | `-3.38` | `[-0.2932, -0.0735]` | `0.0000R` |
| `New Score (5-Factor incl. 次新)` | 1566 | 39 | **`-0.1615R`** | `0.0736` | `-2.19` | `[-0.3106, -0.0124]` | **`+0.0218R`** |
| `New Score (4-Factor Pruned, 次新 Removed)` | 1566 | 39 | **`-0.1705R`** | `0.0718` | `-2.37` | `[-0.3160, -0.0250]` | `+0.0128R` |

## 6. H2 Hypothesis Verdict (`Active New Top-3` vs `Current Ranker Top-3`)

| Metric | Value | Pre-Registered Bar | Status |
|---|---:|---:|---|
| `Gross Top-3 Improvement (Before Slippage)` | `+0.0247R` | — | Diagnostic |
| **`Net Top-3 Improvement (After Full Friction + Slippage)`** | **`+0.0218R`** | **`>= +0.1800R`** | **FAIL** |
| `Cluster-Robust SE (wave_cluster_id)` | `0.0277R` | — | `G = 39` |
| `t-statistic / One-Sided p-value` | `t = +0.79, p = 0.2183` | — | — |

## 7. Shadow Wiring Decision (H2)

- **H2 Overall Verdict**: **FAIL**
- **Passing Factors Wired as Shadow Fields on `DirectionalCandidate`**: `NONE (H2 MISSED +0.18R BAR)`
- **Killed Factors Excluded from Wiring**: `NONE`
