# Phase 2c — Winter Defense Human Risk Advisory Wiring (Shadow Advisory Only, 2026-11-11)

- **Decision Date**: `2026-11-11` (Repository Owner Explicit Authorization)
- **Parent Hypothesis Status**: **H1a Joint Bar = FAIL** (`Walk-Forward OOS Delta NetR = +0.0371R` vs `+0.25R` bar, one-sided `p = 0.3432 >= 0.05`)
- **Wiring Basis**: **Winter Defense Sub-Test Passed Pre-Registered Sub-Bars Across Two Independent Samples** (Phase 2 18-day sample in PR #7 and Phase 2b 12-month sample in PR #8)
- **System Positioning**: **Human-Controlled Advisory Only (`仓位由人决定、系统只提示`)** — zero changes to position sizing, filtering, alert suppression, `BehaviorGate`, or execution paths.

---

## 1. Honest Positioning: Why This Is Not "Hypothesis Passed -> Auto Rule"

1. **Parent Hypothesis H1a Failed Its Joint Acceptance Bar**:
   - In Phase 2b (`2025-11-09 -> 2026-11-09`, `N = 1,550` tickets across `G = 40` merged wave clusters), the causal 4-season classifier did **not** separate positive-expectancy expansion regimes (`SUMMER/AUTUMN`) from chop/reset regimes (`WINTER/SPRING`) by the pre-registered `+0.25R` margin:
     - In-sample `Delta NetR = +0.0271R` (`SE = 0.0871`, `p = 0.3787`) — **FAIL**
     - Walk-forward OOS `Delta NetR = +0.0371R` (`SE = 0.0913`, `p = 0.3432`) — **FAIL**
   - Because the joint bar required both the Regime Edge Bar and the Winter Defense Bar, **H1a as a whole remains FAIL**.
2. **Why Owner Authorized Extracting Winter Defense as a Human Hint (`2026-11-11`)**:
   - Across both independent samples, `WINTER` tickets exhibited statistically significant negative full-friction expectancy (`Mean NetR = -0.2458R`, `SE = 0.0671`, `t = -3.66`, `N = 970`, `G = 40` in Phase 2b), and scaling risk down in `WINTER` (`0.25x` or `0.0x`) mechanically passed the pre-registered Winter Defense sub-bars (`>= 80%` profit retention and `>= 30%` max drawdown reduction) in **both** datasets.
   - Because `kairos` is a human-controlled decision support system where position size is always chosen by the human operator, surfacing `season_regime` and a `winter_defense_hint` when `season_regime == "WINTER"` provides actionable defensive context without introducing an automated rule.

---

## 2. Two-Sample Replication Table: Winter Defense Sub-Test

| Sample / Metric | Phase 2 (18 Days, PR #7) | Phase 2b (12 Months, PR #8) | Pre-Registered Sub-Bar | Sub-Test Status |
|---|---:|---:|---:|---|
| **Sample Window** | `2026-10-22 -> 2026-11-09` (`18d`) | `2025-11-09 -> 2026-11-09` (`366d / 12m`) | `>= 6m` (Phase 2b) | **MET** |
| **Total Candidate Tickets (`N`) / Clusters (`G`)** | `N = 162` / `G = 7` | **`N = 1,550` / `G = 40`** | `G >= 25` (Phase 2b) | **MET** |
| **`WINTER` Ticket Count (`N_winter`, `G_winter`)** | `109` (`67.3%`), `G = 7` | **`970` (`62.6%`), `G = 40`** | Diagnostic | — |
| **`WINTER` Mean NetR (`wave_k_be=0.8`)** | `-0.4915R` (`SE = 0.1456`, `t = -3.38`) | **`-0.2458R` (`SE = 0.0671`, `t = -3.66`)** | Negative expectancy | **Replicated (`t = -3.66`)** |
| **`0.25x` Winter Risk Scaling — Profit Retention** | **`153.5%`** | **`146.5%`** (`-205.38R` vs `-384.23R`) | **`>= 80.0%`** | **PASS (2/2)** |
| **`0.25x` Winter Risk Scaling — Max DD Reduction** | **`53.4%`** | **`47.1%`** (`207.32R` vs `391.97R`) | **`>= 30.0%`** | **PASS (2/2)** |
| **`0.0x` Winter Silence — Profit Retention** | **`171.4%`** | **`162.1%`** (`-145.77R` vs `-384.23R`) | **`>= 80.0%`** | **PASS (2/2)** |
| **`0.0x` Winter Silence — Max DD Reduction** | **`71.2%`** | **`62.7%`** (`146.22R` vs `391.97R`) | **`>= 30.0%`** | **PASS (2/2)** |
| **Parent Hypothesis H1a Joint Bar (`OOS Delta NetR`)** | `+0.0885R` (`p = 0.2747`) | `+0.0371R` (`p = 0.3432`) | `>= +0.25R, p < 0.05` | **FAIL (Parent H1a)** |

---

## 3. Scope of Phase 2c Wiring & Red-Line Audit

### A. Causal `SeasonRegime` Classifier Port (`internal/cycle/season.go`)
- Ported verbatim from `tools/research/h1_experiment.py` (`classify_season_from_aggregates` + `build_causal_season_timeline` + `_build_5m_bars`):
  - **Thresholds**: `SummerBreadthMin = 0.58`, `SpringBreadthMin = 0.46`, `AutumnSlowBreadthMin = 0.52`, `AutumnFastBreadthMin = 0.40`, `BreadthMomentumFloor = -0.02`, `SummerVolMin = 0.98`, `WinterVolMax = 0.85`.
  - **Rolling Windows**: `FastBars = 6` (`30m`), `SlowBars = 24` (`2h`), `NoiseRetPct = 0.05%`, `MinValidSymbols = 4`, contiguity gap guard (`(len(f_slice)+2)*300` and `(len(s_slice)+4)*300`).
  - **Causal Hysteresis**: 2-bar confirmation (`prev_season = WINTER`, `streak_candidate = WINTER`, `streak_len = 0`) using only closed 5m bars `<= t`.

### B. Shadow-Only Advisory Fields (`internal/types/decision.go`, `internal/decision/ticket.go`, `internal/storage/journal.go`)
- Added to `types.DecisionTicket` and `storage.CounterfactualOutcome`:
  - `season_regime` (`omitempty`): populated (`SPRING` / `SUMMER` / `AUTUMN` / `WINTER`) **if and only if** genuinely computed via the causal classifier from faithful cross-sectional inputs; omitted (`""`) when cross-sectional inputs are not supplied.
  - `winter_defense_hint` (`omitempty`): populated **if and only if** genuine `season_regime == "WINTER"` (never emitted when `SPRING` / `SUMMER` / `AUTUMN` or when `season_regime` is absent), with value:
    `"advisory: consider 0.25x risk scaling (winter tickets historically negative expectancy, defense sub-test passed twice); human decides position size"`
- **Behavioral Red-Line Verification**:
  - `RiskPlan.RiskBudgetPct`, `SuggestedNotional`, `MaxNotional`, `MaxLeverage`, `AddOnAllowed`: **unchanged**.
  - Playbook matching, candidate ranking, alert emission, `BehaviorGate`: **unchanged**.

### C. Live-Chain Input Fidelity Audit & Owner Ruling (`2026-11-11`: Genuine Calculation Only)
Per Section A ("若某个输入在实时链路里无法按研究口径忠实获得：停下来在会话里明说，不要静默近似、不要换定义"), we audited the live runtime chain (`MarketPulseDetector` -> `opportunity.EnrichAndEvaluate`) against the research specification in `tools/research/h1_experiment.py`:
1. **`MarketPulseDetector` (`internal/detector/market_pulse.go`)**:
   - Retains only `900s` (`15m`) of tick price points (`mpPricePoint{ts, price}`) and latest 24h `QuoteVolume`. It does **not** maintain 2h (`24 x 5m`) closed 5m OHLCV bars or 5m quote-volume series across the universe.
2. **`opportunity.EnrichAndEvaluate` (`internal/opportunity/enrich.go`) & `exchange.FetchOHLCV` (`internal/exchange/okx.go`)**:
   - `EnrichAndEvaluate` only fetches 5m OHLCV for `orderedSyms` (up to `MaxSymbols = 3` candidate leaders) plus `MarketSymbol` (`BTC/USDT:USDT`), whereas `build_causal_season_timeline` requires cross-sectional 5m bars across the universe (`>= 4` valid symbols, 27 PIT swaps in Phase 2b).
   - `exchange.FetchOHLCV` parses `row[5]` (contract/base `vol`) into `types.Candle.Volume` and does not populate OKX `row[7]` (`volCcyQuote`, USD quote volume) on `types.Candle`.
3. **Owner Ruling (`2026-11-11` — "只认真实计算", Fallback Removed)**:
   - After reviewing the live-chain audit, the repository owner ruled on `2026-11-11` that `season_regime` accepts **only genuine cross-sectional classifier calculation** (`UniverseBars5m` -> `BuildCausalSeasonTimeline` / `SeasonTimeline` / `SeasonRegime`) and ordered removal of the `SeasonRegimeFromCycleMap` fallback.
   - Consequently, when a caller does not pass faithful cross-sectional inputs (`UniverseBars5m` / `SeasonTimeline` / `SeasonRegime`), **neither `CycleMap` mapping nor a default `WINTER` value is permitted**: `season_regime` remains absent (`""`, `omitempty`) and `winter_defense_hint` is never emitted until a full-universe 5m `volCcyQuote` feed is wired.

---

## 4. Go vs Python Consistency Proof (Section C)

Using an 8-slice `PHASE2B_PREREG` dataset regenerated from OKX (`2025-11-09 -> 2026-11-09`, `27` PIT symbols in `PHASE2B_27_SWAP_INST_IDS`, `164,254` 1m bars, `6,995` funding ticks), we ran both Python (`tools/research/h1_experiment.py`) and Go (`internal/cycle/season.go` via `TestPhase2bConsistencyWithPython`) and compared every 5m cross-sectional close timestamp and every candidate ticket decision point:

| Comparison Surface | Total Points Evaluated | Exact Matches (Go == Python) | Match Rate | Required Bar | Discrepancies |
|---|---:|---:|---:|---:|---:|
| **5m Cross-Sectional Timeline (`SeasonObservation`)** | `1,511` (`WINTER: 976, SPRING: 30, SUMMER: 258, AUTUMN: 247`) | **`1,511 / 1,511`** | **`100.0000%`** | `>= 99.9%` | **`0`** |
| **Candidate Ticket Decision Points (`SeasonRegime` at `entry_ts`)** | `423` (`WINTER: 269, SPRING: 6, SUMMER: 54, AUTUMN: 94`) | **`423 / 423`** | **`100.0000%`** | `>= 99.9%` | **`0`** |
| **Continuous Aggregates (`breadth_fast`, `breadth_slow`, `volume_ratio`, `crowding_pct`)** | `1,511` | **`1,511 / 1,511`** (`\|diff\| < 1e-12`) | **`100.0000%`** | `>= 99.9%` | **`0`** |

---

## 5. Phase 2d — Live Cross-Sectional 5m Season Feed (`2026-11-11`)

Phase 2c wired the causal classifier and shadow ticket fields while leaving live `UniverseBars5m` unpopulated until native 5m quote-volume (`volCcyQuote`) and a live cross-sectional window existed. Phase 2d supplies that faithful live input without relaxing any Phase 2c red line.

### A. Feed Design (`seasonFeed` & Quote-Volume Plumbing)

1. **Native Quote-Volume Plumbing (No `Close * Volume` Proxy)**:
   - `types.Candle` adds `QuoteVol float64` (`internal/types/types.go:74`).
   - Exchange OHLCV decoders populate `QuoteVol` from exchange-native quote-currency turnover while keeping `row[5] -> Volume` unchanged:
     - **OKX** (`/api/v5/market/candles` & `history-candles`, `internal/exchange/helpers.go:100-122`): `row[7]` (`volCcyQuote`) -> `QuoteVol`.
     - **Binance USDS-M** (`/fapi/v1/klines`, `internal/exchange/helpers.go:128-163`): `row[7]` (`quoteAssetVolume` in USDT) -> `QuoteVol` (`0` if absent on short synthetic rows).
     - **Bybit Linear** (`/v5/market/kline?category=linear`, `internal/exchange/bybit.go:216-237`): `row[6]` (`turnover` in quote coin) -> `QuoteVol`.
   - Single canonical bar converter `cycle.SeasonBar5mFromCandle` (`internal/cycle/season.go:98-111`) maps a closed 5m `types.Candle` (`Timestamp` = bar open) to `cycle.SeasonBar5m` with `CloseTS = openTS + 300`, matching `Build5mBarsFrom1m` (`internal/cycle/season.go:160-203`). `measureQuoteVolume`'s `Close * Volume` heuristic (`internal/opportunity/enrich.go:270-282`) is never used for season inputs.
2. **Universe Definition & Single Source of Truth**:
   - Universe = `tracked ∪ {"BTC/USDT:USDT"}` (`buildSeasonUniverse`, `internal/engine/season_feed.go:85-100`), where `tracked` comes directly from `Pipeline.symbolsByExchange` via `Pipeline.symbolsSnapshot` / `coinglassSymbols` (`internal/engine/season_feed.go:314-335, 368-377`) — the exact live watchlist discovered and refreshed by `Pipeline`.
   - Each ticket evaluation snapshots the universe active at evaluation time (`WatchAndEnrich` via `EnrichRequest.UniverseBars5mSnapshot`, `internal/engine/pipeline.go:582-588`, `internal/opportunity/watch.go:29-35`); departed symbols have their 24-bar windows reclaimed on `refreshOnce` (`internal/engine/pipeline.go:1730-1732`) and `poll` (`internal/engine/season_feed.go:137`). No historical backfill or post-hoc cross-section reconstruction is performed.
3. **Scheduling, Window, Freshness & Failure Semantics**:
   - **Lifecycle Scheduling**: `seasonFeedLoop` runs inside `Pipeline.Start`'s existing `errgroup` (`5i3` at `internal/engine/pipeline.go:472-478`, active whenever `opportunity.enabled` is true) on a `2m` cadence (`defaultSeasonPollInterval`, aligned with `RunOutcomeLoop`).
   - **Closed-Bar Window (`24` Bars = `2h`)**: Each poll requests `25` 5m candles via `FetchOHLCV(ctx, sym, "5m", 25, 0)`, sorts ascending, drops the unclosed forming tail bar (`ordered[:len(ordered)-1]`, `internal/engine/season_feed.go:180-182`, matching `internal/opportunity/enrich.go:174`), and retains at most the latest `24` closed `SeasonBar5m` per symbol.
   - **Freshness & Minimum Breadth Gates (`snapshotAt`, `internal/engine/season_feed.go:260-310`)**: At evaluation time `asOf`, any symbol with `< 24` closed bars (`CloseTS <= asOf.Unix()`) or whose latest closed bar satisfies `asOf.Unix() - latest.CloseTS > 600s` (`> 10m`) is excluded. If fewer than `4` valid symbols remain (`cycle.DefaultSeasonMinValidSymbols`), `snapshotAt` returns `nil` and both ticket fields remain omitted.
   - **Fail-Silent & Lock-Free I/O**: REST fetches run outside `seasonFeed.mu` with bounded concurrency (`4`) and per-symbol timeout (`10s`); any fetch or data failure degrades silently to absent shadow fields and never blocks or slows ticket generation.

### B. Live Floating Universe vs. Research Fixed 27-Symbol Panel

- **Research (`tools/research/h1_experiment.py`, Phase 2b)**: Used a fixed 27-symbol point-in-time perp universe (`PHASE2B_27_SWAP_INST_IDS`, always containing `BTC-USDT-SWAP`) held constant across the 12-month replay.
- **Live Engine (`kairosd`)**: Tracks a dynamic top-liquidity perp watchlist (`DataManager.TopSymbols`, periodically re-discovered in `Pipeline.refreshOnce`) unioned with `BTC/USDT:USDT`. As market liquidity rotates, symbols enter and leave the tracked set, so the live cross-section naturally floats with the engine's real-time watchlist rather than freezing a static list. Every emitted label is causally computed from the genuine closed-bar cross-section active at the moment of evaluation (`<= TriggeredAt`).

### C. Funding Checkpoint Audit (`EnrichRequest.UniverseFunding`)

We audited `BuildCausalSeasonTimeline(bars, req.UniverseFunding)` to verify whether omitting `UniverseFunding` alters `season_regime` or `winter_defense_hint`:
1. `fundingBySym` is aggregated solely into `fundingHistoryMeans` (`internal/cycle/season.go:264-303`).
2. `rawSeason` and `activeSeason` (`SeasonObservation.SeasonRegime`) are computed exclusively from `(bFast, bSlow, vRatio)` via `ClassifySeasonWithThresholds` (`internal/cycle/season.go:140-156`, invoked at `internal/cycle/season.go:392-412`) with 2-bar hysteresis — `fundingHistoryMeans` is never read in season classification.
3. `fundingHistoryMeans` is consumed only at `internal/cycle/season.go:443-462` to compute `SeasonObservation.CrowdingPct` (an H1b diagnostic metric, mirroring `tools/research/h1_experiment.py:308-326, 345-357`).
4. Ticket enrichment (`internal/opportunity/service.go:374-382`) and ticket construction (`internal/decision/ticket.go:83-85`) read only `obs.SeasonRegime` (and derive `WinterDefenseHint` via `types.WinterDefenseHintFor`), never reading `CrowdingPct`.
5. Verified mechanically by `TestBuildCausalSeasonTimeline_FundingDoesNotAffectSeasonRegime` (`internal/cycle/season_test.go:322-375`): `nil` funding and extreme positive/negative funding schedules produce identical `SeasonRegime` labels at every timestamp. Live funding history feed is therefore out of scope for Phase 2d with zero approximation.

### D. Status Summary

Once deployed, `season_regime` (and `winter_defense_hint` during `WINTER`) will genuinely appear on live tickets whenever a valid, fresh cross-section exists and remain absent otherwise — **this does not change Phase 2c's positioning**: parent hypothesis H1a remains **FAIL**, and these fields remain shadow-only human prompts rather than automated trading or sizing rules.
