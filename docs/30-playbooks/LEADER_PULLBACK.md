# Playbook: Leader Pullback V1

> Status: **canonical freeze (PR1)**  
> ID: `leader_pullback_v1`  
> Parent: `docs/TRADING_SYSTEM_CANONICAL.md`  
> Cycle: `docs/CYCLE_MODEL.md`  
> Stage-1: **only enabled playbook**

Philosophy source: 龙头 + 小分歧回踩承接 (`docs/trading-system.md`).

No auto execution. Output is a Decision Ticket for human accept/wait/reject/miss.

---

## 1. Intent

Trade the **first clean pullback (or bounce) of a true directional leader** after MarketPulse wakes the desk, under a multi-TF CycleMap.

Long form: market impulse up → leader → pullback holds structure → restart.  
Short form: exact mirror on impulse down → core laggard → bounce fails → restart down.

---

## 2. Interface contract

```go
type Playbook interface {
    ID() string
    Match(PlaybookContext) MatchResult
}

type MatchResult struct {
    Matched      bool
    Grade        string // A|B|C|D
    Direction    CycleDirection
    TradeClass   TradeClass
    HardFailures []string
    Reasons      []string
    Warnings     []string
    RiskTemplate string
}
```

---

## 3. Decision pipeline

```text
Gate → Rank → Grade
```

Any hard gate fail → `Matched=false`, grade D / NO_TRADE.  
Soft weaknesses are warnings or grade caps, never silent score compensation.

---

## 4. Hard gates (non-compensable)

Shared:

- Data complete and healthy
- Liquidity OK / spread OK
- Explicit structural invalidation exists
- Enough room in trade direction
- Not stuck mid large range with no edge
- MarketPulse direction not fully opposite to ticket direction
  (unless explicit counter-trend path below is taken and labeled)

### 4.1 Aligned long

**Context**

- Primary context direction: UP
- Context phase: SPRING or SUMMER
- Room up still available
- MarketPulse: UP (impulse/trend)

**Setup**

- Candidate is directional leader (relative strength rank high)
- 1h/15m already showed clear impulse up
- Relative strength leads market
- On market dip, candidate holds better than peers (抗跌)

**Trigger**

- 5m pullback does not break structure
- Forms higher low
- Sell pressure fades
- Trigger cycle returns UP / SPRING (restart)

### 4.2 Aligned short (mirror)

**Context**

- Primary context direction: DOWN
- Context phase: SPRING or SUMMER
- Room down still available
- MarketPulse: DOWN

**Setup**

- Candidate is core laggard (relative weakness rank high)
- 1h/15m clear impulse down
- Relative weakness leads market lower
- On market bounce, candidate lags (滞涨)

**Trigger**

- 5m bounce does not break structure resistance
- Forms lower high
- Bounce exhausts
- Trigger cycle returns DOWN / SPRING

### 4.3 Counter-trend (allowed, capped)

Example:

```text
1d DOWN/SUMMER
4h UP/SPRING
15m UP/SUMMER
5m UP/SPRING
```

Allowed only if setup+trigger stack is clean **and** labeled:

```text
TradeClass: counter_trend_long  (or counter_trend_short mirror)
Grade: ≤ C (counter-trend is never above C)
RiskTemplate: counter_trend
```

Counter-trend risk rules:

- Lower risk budget
- Near targets only (next setup resistance/support)
- No main-trend holding narrative
- No add-on
- Exit more aggressively when setup/trigger enters AUTUMN

### 4.4 Forbidden (always)

- All TF directions mixed / chaotic
- Context NEUTRAL / WINTER as primary environment
- Mid large box, no edge
- Market impulse without relative strength/weakness on candidate
- Leader already clear AUTUMN / only catch-up left
- No definable invalidation
- Using legacy `winter` climate alone as short permission

---

## 5. Rank factors (after gates)

Long:

| Factor | Prefer |
| --- | --- |
| Relative strength | Leads market on impulse |
| Pullback quality | Shallow, holds prior HL |
| Resonance | Starts with MarketPulse up |
| Structure quality | Clear invalidation, clean restart |
| Volume quality | Impulse volume, pullback digestion |
| Room | Meaningful room up |

Short: mirror with relative weakness / rebound weakness / room down.

Rank is ordering only among gate passers. It does not revive gate failures.

---

## 6. Grade rules

| Grade | When |
| --- | --- |
| A | Full alignment Context→Setup→Trigger; leader/laggard #1-tier; clean invalidation |
| B | Direction OK; minor TF imperfection or slightly late |
| C | Counter-trend or incomplete stack; observation / tiny risk only |
| D | Gate fail / no trade |

Cap:

- Counter-trend → max C
- Context AUTUMN aligned continuation → max B, no add-on
- Missing invalidation → D

---

## 7. Risk templates

| Template | Add-on | Intent |
| --- | --- | --- |
| `aligned_spring` | yes | Early trend, allow planned add after restart |
| `aligned_summer` | yes | Expansion, still structure-bound |
| `aligned_autumn` | no | Late trend, take nearer targets |
| `counter_trend` | no | Scalp / near target only |
| `mixed_context` | no | Usually no ticket |
| `no_trade` | — | Gate fail |

Sizing intent (numbers from config):

```text
loss_budget = equity × risk_budget_pct(template)
notional    = loss_budget / stop_distance_pct
```

Stop = structural invalidation from Trigger (and Setup confirmation).  
Context sets ceiling; Trigger does not expand risk.

---

## 8. Entry / invalidation / exit

### Entry (human executes)

Prefer:

- Pullback hold + restart (long) / bounce fail + restart (short)
- Not chase extended break candles on Trigger

### Invalidation examples

Long:

- 5m pullback low breaks
- 15m flips DOWN with structure break
- Market breadth collapses back under impulse baseline

Short: mirror.

Ticket must list invalidations explicitly. `requireInvalidation: true`.

### Exit logic (human; mechanical baseline for counterfactual)

- Target: next Context/Setup room level
- Time stop / phase decay: setup or trigger enters AUTUMN → tighten / exit
- MarketPulse decay against position → re-evaluate immediately
- Counter-trend: exit at first clean target or phase decay — no “格局”

Mechanical baseline (for EV, not auto trade):

- Entry at trigger signal close
- Stop at ticket invalidation
- Targets at planned levels / horizon marks (5m, 15m, 1h, 4h MFE/MAE)

---

## 9. Candidate dual scores

Do not use one score for both sides.

```text
LongScore, ShortScore
RelativeStrength, RelativeWeakness
PullbackStrength, ReboundWeakness
```

MarketPulse UP → prefer long analysis for this playbook.  
MarketPulse DOWN → prefer short.  
No direction → no trend playbook match.

---

## 10. Ticket content (minimum)

```text
【市场周期】 multi-TF Direction/Phase
【交易属性】 direction, trade_class, playbook id, grade, risk template
【候选理由】 ranked reasons
【失效】     invalidations
【禁止行为】 e.g. chase, size-up, hold as main bull run (if counter_trend)
```

Human decision + reason codes required for research.

---

## 11. Counterfactual baseline

For every ticket (accepted or not), track:

- Horizon returns: 5m / 15m / 1h / 4h
- MFE / MAE
- Hit stop first vs target first
- Max realizable R under mechanical plan

Research targets:

- Accepted EV > all qualified EV (selection alpha)
- Rejected EV ≤ 0 or worse
- Timing/exit alpha vs mechanical baseline

---

## 12. Acceptance tests (later code PRs)

1. Long fixture leader ranks #1; mirrored short laggard ranks #1
2. Nested counter-trend long → trade_class counter_trend_long, grade ≤ C, add-on false
3. Chop winter → no match
4. Missing invalidation → no match
5. Replay equality on MatchResult
6. No order placement side effects

---

## 13. Non-goals for V1

- Box breakout / second-wave playbooks (docs may exist later; disabled)
- Auto sizing from live exchange balance as authority
- LLM narrative grading
- Compensating total score bypassing gates
