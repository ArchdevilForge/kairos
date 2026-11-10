# Phase 2b Pre-Registration — Expanded-Sample Revalidation of H1–H3 (2026-11-10)

> **Status**: Pre-Registered Audit Anchor (Committed BEFORE Phase 2b Data Pull & Evaluation)
> **Branch**: `feat/kairos-phase2b-expanded-sample`
> **Parent**: Phase 1 (PR #6, `161e7d1`) & Phase 2 (PR #7, `df4a84e`)

---

## 1. Purpose of This Re-Run

Phase 2 (PR #7) evaluated hypotheses **H1a**, **H1b**, **H2**, and **H3** on an **18-day** OKX USDT-SWAP 1m replay sample (`N = 162` tickets across `G = 7` `<24h`-merged wave clusters; H1b had `G = 4` impulse clusters; H3 had `G_oos = 6` vs the `>= 15` pre-registered bar).

This Phase 2b re-run is a **sample-size / statistical-power expansion only**, not a re-specification:
- **Every hypothesis definition, factor formula, holding policy (`wave_k_be=0.8` full-friction NetR), cluster-robust SE estimator (`wave_cluster_id` `<24h` merge rule), acceptance bar, and kill rule from Phase 2 stays 100% identical.**
- Zero thresholds, weights, or eligibility floors are loosened or tuned after seeing Phase 2 results.

---

## 2. Pre-Declared Data Window & Sampling Protocol

1. **Target Contiguous Window (`12 Months` Ending `2026-11-09`)**:
   - **Window Start**: `2025-11-09T00:00:00Z` (`1762646400` Unix sec)
   - **Window End**: `2026-11-09T23:59:00Z` (`1794268740` Unix sec, `366` calendar days / `12` months)
   - **Hard Floor**: `>= 6 months` (`>= 180` calendar days) contiguous coverage ending `2026-11-09T23:59:00Z`.
   - **No Sub-Window Cherry-Picking**: One contiguous pre-declared window `[start_ts, end_ts]` is evaluated across all four hypotheses.

2. **Multi-Cluster 1m + HTF + Funding + OI Replay Coverage Across the 12-Month Window**:
   - Across the full 12-month (`366`-day) span `[2025-11-09, 2026-11-09]`, we pull **40 evenly-spaced contiguous 1-minute candle slices** (each `15 hours = 900` genuine 1m bars, spaced every `~9.15 days > 24h` across the entire 12-month window, plus the final `24h` window ending `2026-11-09T23:59:00Z`), along with continuous `4h`/`1d` HTF structure bars, `8h` realized funding rates, and `5m`/`1h` open-interest history from OKX public REST endpoints (`/api/v5/market/history-candles`, `/api/v5/market/candles`, `/api/v5/public/funding-rate-history`, `/api/v5/rubik/stat/contracts/open-interest-history`).
   - Because adjacent slices are separated by `~9.15 days` (`>> 24h`), the `<24h` `wave_cluster_id` merge rule yields up to **40 independent macro wave clusters** spanning all four quarters of 2025–2026 while preserving 100% genuine 1-minute intra-bar path resolution (`stop-first`, next-minute-open fee-inclusive breakeven, causal 5m/15m/1h/4h structure) within OKX public API rate limits.

3. **Point-in-Time (PIT) Universe (`dim_perp_universe_pit`)**:
   - All OKX linear `USDT-SWAP` perpetual contracts (`fetch_okx_usdt_swaps()`) are synced into `dim_perp_universe_pit(symbol, list_ts, delist_ts, tick_size)`.
   - Historical 1m/HTF/funding/OI series are pulled for a **27-symbol PIT cross-section** spanning:
     - **15 Liquid Majors & Established Core Swaps**: `BTC`, `ETH`, `SOL`, `XRP`, `DOGE`, `SUI`, `LINK`, `AVAX`, `ADA`, `DOT`, `LTC`, `BCH`, `NEAR`, `UNI`, `AAVE` (`-USDT-SWAP`)
     - **12 Swaps Listed Across the 2025–2026 Window (`次新` at Historical Wakes & Mid/Lower-Liquidity Tiers)**: `PIEVERSE`, `LIT`, `ZAMA`, `EDGE`, `UP`, `GRAM`, `CAP`, `VVV`, `AKE`, `PONS`, `DGAI`, `CT` (`-USDT-SWAP`)
   - At every historical evaluation timestamp `t`, a contract is eligible **if and only if** `list_ts <= t`, `(delist_ts IS NULL OR delist_ts > t)`, and causal trailing 24h quote volume at `<= t` meets the unchanged pre-registered floor (`$30M` for H1a/H1b, `$10M` for H2 incl. new/illiquid listings).

---

## 3. Candidate-Generation Method (Unchanged from Phase 2)

- **H1a & H1b (`tools/research/h1_experiment.py:run_h1_experiment`)**:
  - Replays causal 5m cross-sectional MarketPulse wakes and BTC upward `market_impulse` events across the PIT-eligible universe, paired with causal `_detect_5m_pullback_trigger` (`leader_pullback_v1`) entries and `wave_k_be=0.8` full-friction simulation.
  - Enforces `validate_ex_ante_grouping` (`feature_ts <= entry_ts`) on `season_regime`, `btc_sync_mode`, and `crowding_pct`.
- **H2 (`tools/research/h2_experiment.py:run_h2_experiment`)**:
  - Replays multi-candidate directional MarketPulse wakes (`>= 4` valid pullback-triggered candidates per wake) across the PIT universe, scoring each candidate strictly at `wake_ts <= entry_ts` by both the current ranker (`score_current_ranker`) and the 5-factor / 4-factor pruned Dynamic Leader Score under full friction + square-root market impact slippage.
- **H3 (`tools/research/h3_experiment.py:run_h3_experiment`)**:
  - Replays causal 5m swing pivots with mandatory confirmation lag `k = 3` bars (`confirm_idx = pivot_idx + 3 <= t_idx`), detecting `BOX_BREAKOUT` (`>= 2` confirmed upper + `>= 2` confirmed lower touches, `ATRCompressionRatio <= 0.88`, `RoomToResistanceR >= 1.0R`) and `SECOND_WAVE_STAGE4` (`Stage-1 >= 1.2%` -> `Stage-2` retracement with `TriggerKnifeLock=True` -> `Stage-3` contraction -> `Stage-4` right-side re-break).

---

## 4. Power Targets & Unchanged Pre-Registered Acceptance Bars

### Power Targets (Reported First in Every Report)
- **H3**: Out-of-sample independent wave clusters `G_oos >= 15`
- **H1b**: BTC upward impulse wave clusters `G >= 15`
- **H1a**: Merged regime wave clusters `G` materially above Phase 2 (`G = 7`), targeting `G >= 25` across the 12-month window
- **Underpowered Reporting Rule**: If any power target is unreachable even at the achieved window, the report will explicitly state the achieved `G` and publish `"FAIL (underpowered, G=…)"` rather than hiding or altering the test.

### Unchanged Hypothesis Bars & Kill Rules
| Hypothesis | Pre-Registered Bar (Unchanged from Phase 2) | Kill Rule / Defense Bar |
|---|---|---|
| **H1a** | Walk-forward OOS `SUMMER/AUTUMN` vs `WINTER/SPRING` cluster-adjusted `Delta NetR >= +0.25R` AND one-sided `p < 0.05` | `WINTER 0.25x` risk multiplier (or `0.0x` silence) retains `>= 80%` of cumulative net profit and cuts max drawdown `>= 30%` |
| **H1b** | `RESONANCE [0, 15m]` mean NetR significantly `>` `LAGGING_CATCHUP [2h, 12h]`, `Delta NetR > 0` AND one-sided `p < 0.05` | Report `BTC near 4h high + CrowdingPct >= 0.75` alt long-breakout fakeout (stop-hit) rate |
| **H2** | Dynamic Leader Score Top-3 vs Current Ranker Top-3 `Delta NetR >= +0.18R` after full friction + impact slippage | **`次新` Kill Rule**: If `listing_age_cixin` (`<= 90d`) post-slippage standalone excess `<= 0R` (or degrades Top-3 NetR), remove `listing_age_cixin` and report the kill |
| **H3** | Causal `k = 3` lag `BOX_BREAKOUT` + `SECOND_WAVE_STAGE4` OOS `G_oos >= 15` AND OOS NetR 95% CI lower bound `>= 0.0000R` | **H3 Kill Rule**: If `G_oos < 15` OR OOS NetR 95% CI lower bound `< 0R`, do NOT build any automatic geometry recognizer (reference PR #7 manual watch-range spec) |
