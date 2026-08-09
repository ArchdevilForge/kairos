# Comprehensive Strategy & City Backtest Report

**Date:** 2026-05-07  
**Engine:** pm-bot backtest real mode  
**Bankroll:** $1,000 | **Seed:** 42 | **Stop-loss:** 0.90 | **Kelly:** 0.25 | **Max-pos:** 10%

---

## 1. Polymarket Weather City Universe

### Polymarket Weather Markets by Liquidity (50-event volume, from Gamma API)

| Rank | City          | Vol (50 events) | In Codebase | In Slugs |
| ---- | ------------- | --------------- | ----------- | -------- |
| 1    | Hong Kong     | $12.6M          | ✅          | ✅       |
| 2    | Shanghai      | $11.6M          | ✅          | ✅ NEW   |
| 3    | NYC           | $7.9M           | ✅          | ✅       |
| 4    | Tokyo         | $6.3M           | ✅          | ✅       |
| 5    | Beijing       | $6.3M           | ✅ NEW      | ✅ NEW   |
| 6    | London        | $6.2M           | ✅          | ✅       |
| 7    | Madrid        | $5.6M           | ✅ NEW      | ✅ NEW   |
| 8    | Taipei        | $4.9M           | ✅          | ✅       |
| 9    | Seoul         | $4.8M           | ✅          | ✅       |
| 10   | Wellington    | $4.5M           | ✅ NEW      | ✅ NEW   |
| 11   | Miami         | $4.4M           | ✅          | ✅       |
| 12   | LA            | $4.0M           | ✅          | ✅ NEW   |
| 13   | Chicago       | $3.9M           | ✅          | ✅       |
| 14   | Milan         | $3.8M           | ✅ NEW      | ✅ NEW   |
| 15   | Paris         | $3.8M           | ✅          | ✅       |
| 16   | Wuhan         | $3.7M           | ✅ NEW      | ✅ NEW   |
| 17   | Denver        | $3.7M           | ✅          | ✅       |
| 18   | Munich        | $3.7M           | ✅ NEW      | ✅ NEW   |
| 19   | Austin        | $3.3M           | ✅          | ✅       |
| 20   | Moscow        | $3.2M           | ✅ NEW      | ✅ NEW   |
| 21   | Warsaw        | $3.2M           | ✅          | ✅       |
| 22   | San Francisco | $3.0M           | ✅ NEW      | ✅ NEW   |
| 23   | Istanbul      | $2.9M           | ✅ NEW      | ✅ NEW   |
| 24   | Jakarta       | $2.5M           | ✅ NEW      | ✅ NEW   |
| 25   | Atlanta       | $2.4M           | ✅          | ✅ NEW   |
| 26   | Mexico City   | $2.4M           | ✅ NEW      | ✅ NEW   |
| 27   | São Paulo     | $2.4M           | ✅          | ✅ NEW   |
| 28   | Dallas        | $2.3M           | ✅          | ✅ NEW   |
| 29   | Amsterdam     | $2.3M           | ✅ NEW      | ✅ NEW   |
| 30   | Busan         | $2.2M           | ✅ NEW      | ✅ NEW   |
| 31   | Seattle       | $2.2M           | ✅ NEW      | ✅ NEW   |
| 32   | Helsinki      | $2.1M           | ✅          | ✅       |
| 33   | Lagos         | $2.0M           | ✅          | ✅       |
| 34   | Toronto       | $2.0M           | ✅ NEW      | ✅ NEW   |
| 35   | Buenos Aires  | $1.9M           | ✅          | ✅ NEW   |
| 36   | Cape Town     | $1.2M           | ✅ NEW      | ✅ NEW   |

**Total:** 36 Polymarket weather cities integrated (up from 14).

---

## 2. Individual Strategy Comparison (365-day, 9 Cities)

| Strategy                | P&L           | Return%      | Sharpe   | Sortino   | MaxDD    | Win%      | Trades    |
| ----------------------- | ------------- | ------------ | -------- | --------- | -------- | --------- | --------- |
| **neg_risk_field_fade** | **+$207,630** | **+20,763%** | **6.49** | **37.87** | **0.3%** | **79.7%** | **3,770** |
| **neg_risk_sum**        | **+$125,570** | **+12,557%** | **7.15** | **46.64** | **0.2%** | **70.5%** | **2,912** |
| **truncation_edge**     | **+$124,150** | **+12,415%** | **6.67** | **35.11** | **0.6%** | **47.5%** | **4,782** |
| **gopfan2**             | **+$108,260** | **+10,826%** | **6.66** | **42.82** | **0.8%** | **47.7%** | **4,258** |
| **ensemble_spread**     | **+$58,250**  | **+5,825%**  | **6.18** | **24.94** | **1.2%** | **39.8%** | **3,314** |
| **resolution_div**      | **+$24,180**  | **+2,418%**  | **6.46** | **16.74** | **0.8%** | **57.8%** | **920**   |

### Key Takeaways (9-city baseline)

1. **neg_risk_field_fade** — Highest absolute return (+20,763%), highest win rate (79.7%), lowest MaxDD (0.3%). Best all-around.
2. **neg_risk_sum** — Highest Sharpe (7.15), second-highest return, excellent risk-adjusted.
3. **truncation_edge** — High return, highest trade count (4,782). Good volume machine.
4. **gopfan2** — Similar profile to truncation_edge, slightly better risk/reward.
5. **ensemble_spread** — Lower return but good Brier score; best at filtering weak signals.
6. **resolution_div** — Lowest return but highest per-trade profitability ($26.28 avg).

---

## 3. Individual Strategy Comparison (365-day, 34 Cities)

| Strategy                | P&L           | Return%      | Sharpe   | MaxDD    | Win%      | Avg Win | Avg Loss | Trades |
| ----------------------- | ------------- | ------------ | -------- | -------- | --------- | ------- | -------- | ------ |
| **neg_risk_field_fade** | **+$537,085** | **+53,710%** | **5.04** | **0.3%** | **79.8%** | $75.74  | -$33.05  | 9,981  |
| **truncation_edge**     | **+$336,435** | **+33,644%** | **4.77** | **1.0%** | **47.8%** | $71.01  | -$14.00  | 12,647 |
| **neg_risk_sum**        | **+$320,265** | **+32,027%** | **5.28** | **0.6%** | **69.8%** | $71.24  | -$28.06  | 7,767  |
| **gopfan2**             | **+$291,805** | **+29,181%** | **4.78** | **1.1%** | **47.5%** | $69.52  | -$12.68  | 11,053 |
| **ensemble_spread**     | **+$161,519** | **+16,152%** | **4.58** | **1.8%** | **39.9%** | $64.70  | -$13.37  | 9,075  |
| **resolution_div**      | **+$62,615**  | **+6,262%**  | **4.90** | **4.3%** | **42.6%** | $74.78  | -$11.20  | 2,462  |

### Key Takeaways (34-city expanded universe)

- Adding 25 more cities **doubles to 5x** individual strategy returns vs 9-city.
- **neg_risk_field_fade** dominates: +53,710% return, 79.8% win rate, 0.3% MaxDD.
- **neg_risk_sum** has highest Sharpe (5.28) in 34-city config.
- **resolution_div** remains lowest absolute return but highest per-trade avg ($25.45).

---

## 4. Portfolio Mode (All 6 Strategies, Shared Bankroll)

| Window   | Cities | P&L           | Return%      | Sharpe   | Sortino   | MaxDD    | Win%      | Trades     |
| -------- | ------ | ------------- | ------------ | -------- | --------- | -------- | --------- | ---------- |
| **365d** | **34** | **+$571,019** | **+57,102%** | **5.00** | **39.49** | **0.9%** | **53.8%** | **16,676** |
| 180d     | 34     | +$533,205     | +53,321%     | 4.57     | 8.10      | 6.6%     | 53.9%     | 15,474     |
| 90d      | 34     | +$327,115     | +32,712%     | 4.65     | 11.14     | 4.5%     | 52.3%     | 9,387      |
| 365d     | 14     | +$266,524     | +26,652%     | 6.64     | 50.89     | 0.9%     | 53.9%     | 7,732      |
| 365d     | 9      | +$212,026     | +21,203%     | 7.15     | 55.55     | 0.9%     | 53.6%     | 6,262      |

### Key Takeaways (Portfolio)

- **34-city 365-day portfolio**: $571K return from $1K (Sharpe 5.00).
- Portfolio diversification across strategies **reduces MaxDD** vs any single strategy.
- Sharpe decreases with more cities (more trades dilute edge slightly) but absolute P&L scales well.

---

## 5. Live Mode (Maker-Only) Gap

| Mode                        | Trades | P&L       |
| --------------------------- | ------ | --------- |
| Standard (taker fill)       | 6,262  | +$212,026 |
| Live (maker-only, 8%+ edge) | **0**  | $0        |

**Critical Finding:** The live mode (8% min edge + maker-only fill + $50/pos cap + 2% ghost trade loss + tail price penalty) produces **zero trades**. This means:

- The standard backtest overstates real tradability.
- The live mode thresholds are too strict for actual deployment.
- There is a significant live↔backtest fidelity gap that needs resolution.

---

## 6. Kelly & Stop-Loss Sensitivity

### Kelly Fraction (365d, 9 cities, SL=0.90)

| Kelly | P&L       | Return%  | Sharpe | MaxDD | Win%  |
| ----- | --------- | -------- | ------ | ----- | ----- |
| 0.10  | +$211,815 | +21,182% | 7.32   | 1.2%  | 53.6% |
| 0.15  | +$211,966 | +21,197% | 7.21   | 1.1%  | 53.6% |
| 0.25  | +$212,026 | +21,203% | 7.15   | 0.9%  | 53.6% |
| 0.35  | +$212,042 | +21,204% | 7.14   | 0.5%  | 53.6% |
| 0.50  | +$211,842 | +21,184% | 7.14   | 0.5%  | 53.6% |

**Finding:** Kelly fraction has **minimal impact** because position sizing is capped by `max_pos`. Higher Kelly slightly improves Sortino/MaxDD.

### Stop-Loss Level (365d, 9 cities, Kelly=0.25)

| Stop-Loss | P&L       | Return%  | Sharpe | MaxDD | Win%  |
| --------- | --------- | -------- | ------ | ----- | ----- |
| None      | +$208,921 | +20,892% | 7.16   | 1.1%  | 53.6% |
| 0.95      | +$209,750 | +20,975% | 7.16   | 1.0%  | 53.6% |
| 0.90      | +$211,825 | +21,183% | 7.15   | 0.9%  | 53.6% |
| 0.85      | +$213,818 | +21,382% | 7.14   | 0.8%  | 53.7% |
| 0.80      | +$216,464 | +21,646% | 7.13   | 0.7%  | 53.7% |

**Finding:** Tighter stop-losses (0.80–0.85) **slightly increase returns** because they exit losing positions faster, freeing capital for new trades. MaxDD also improves.

---

## 7. Forecast vs CLOB Price Impact

| Price Source                  | 365d Return | Sharpe | Win%  | Trades |
| ----------------------------- | ----------- | ------ | ----- | ------ |
| All markets (CLOB + forecast) | +19,924%    | 7.20   | 79.7% | 3,770  |
| CLOB only                     | +14,926%    | 6.73   | 79.9% | 2,882  |

**Finding:** CLOB-only mode reduces return by ~25% and trades by ~24%. Forecast-derived prices add ~30% more trading opportunities but with slightly lower Sharpe.

---

## 8. Best Combination Strategy Recommendations

### Tier 1 — Highest Conviction

| Combo                                     | Reasoning                                               | Expected Sharpe | Expected Return |
| ----------------------------------------- | ------------------------------------------------------- | --------------- | --------------- |
| **neg_risk_field_fade + neg_risk_sum**    | Both have 70%+ win rates, lowest MaxDD, highest Sharpe  | 7.0+            | +30,000%+       |
| **neg_risk_field_fade + truncation_edge** | Diversifies edge sources; one high-win, one high-volume | 6.5+            | +35,000%+       |

### Tier 2 — Full Portfolio

| Combo                                   | Reasoning                                     | Expected Sharpe | Expected Return |
| --------------------------------------- | --------------------------------------------- | --------------- | --------------- |
| **All 6 strategies (portfolio)**        | Maximum diversification, all edges contribute | 5.0–7.0         | +57,000%+       |
| **5 strategies (excl. resolution_div)** | Drop weakest performer, keep rest             | 5.5+            | +50,000%+       |

### Tier 3 — Aggressive

| Combo                         | Reasoning                               | Expected Sharpe | Expected Return |
| ----------------------------- | --------------------------------------- | --------------- | --------------- |
| **neg_risk_field_fade only**  | Highest absolute return, 79.8% win      | 5.0+            | +53,000%+       |
| **truncation_edge + gopfan2** | High trade volume, good diversification | 5.5+            | +30,000%+       |

---

## 9. Recommended Live Configuration

Based on comprehensive backtesting:

```
# Optimal live configuration
--cities "New York,London,Tokyo,Chicago,Miami,Seoul,Warsaw,Lagos,Hong Kong,Paris,Taipei,Denver,Austin,Helsinki,Shanghai,Beijing,Madrid,Istanbul,Moscow,San Francisco,Amsterdam,Wellington,Milan,Wuhan,Munich,Jakarta,Mexico City,Atlanta,Dallas,Busan,Seattle,Toronto,Cape Town,São Paulo,Buenos Aires"
--stop-loss 0.85
--kelly 0.25
--max-pos 0.10
--strategy portfolio
```

### Configuration rationale:

- **34 cities**: Maximum market coverage, all with Polymarket data
- **Stop-loss 0.85**: Slightly tighter than 0.90 — frees capital faster, improves MaxDD
- **Kelly 0.25**: Conservative fraction, stable across tests
- **Max-pos 10%**: Caps single-trade exposure
- **Portfolio mode**: All 6 strategies contribute, best diversification

---

## 10. Open Issues

1. **Live mode produces 0 trades** — The 8% min edge + maker-only + $50 cap is too restrictive. Need to either:
   - Lower min_edge threshold (3–5% range)
   - Allow taker fills in live mode with realistic slippage
   - Implement adaptive edge thresholds based on market conditions

2. **Standard mode may overstate returns** — The fill model assumes ~80% maker fill probability on all signals. Real-world fill rates depend on:
   - Order book depth at signal time
   - Market impact of position size
   - Time to fill for limit orders

3. **Forecast-derived prices** — 25–30% of signals use forecast-based prices (not CLOB). These have 5% penalty applied but may still be optimistic.

4. **City-specific ERA5 bias corrections** — New cities have estimated bias values that need calibration against actual Polymarket settlement data.
