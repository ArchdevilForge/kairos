# Kairos Trading System — Canonical Definitions

> Status: **canonical freeze (PR1)**  
> Source philosophy: `docs/trading-system.md`  
> Machine cycle model: `docs/CYCLE_MODEL.md`  
> First playbook: `docs/playbooks/LEADER_PULLBACK.md`

This document removes ambiguity between human trading philosophy and machine state.
If code and this file disagree, **this file wins** until a later PR updates both.

---

## 1. Product position

Kairos is **not** an auto-trading bot.

```text
Kairos = attention + market map + playbook filter + decision ticket + journal
Human  = accept / wait / reject / miss + entry + exit
```

Hard rules for stage 1:

- No auto order placement
- No LLM deciding direction
- No Rust rewrite of production path
- No single total-score that can compensate for a fatal gate failure

Full workflow:

```text
Realtime data
  → MarketPulse (is it worth looking?)
  → CycleMap (context / setup / trigger state)
  → Directional Ranker (leaders / laggards)
  → Playbook Matcher
  → Decision Ticket
  → Human decision
  → Risk plan
  → Manual execution
  → Journal + counterfactual
  → EV attribution
```

---

## 2. Authority stack

| Layer | Document / component | Answers |
| --- | --- | --- |
| Philosophy | `trading-system.md` | Human trading doctrine (春夏秋冬 climate, 龙头, 分歧, 箱体) |
| Canonical | **this file** | Disambiguated product rules |
| Machine cycle | `CYCLE_MODEL.md` | Direction × WavePhase × Timeframe |
| Opportunity | `playbooks/*.md` | Concrete long/short setups |
| Runtime | Go types + services | Implementation of the above |

`docs/trading-system.md` stays the philosophy source. It is **not** a direct machine state enum.

---

## 3. Two different “seasons”

### 3.1 Legacy climate (human reading)

From `trading-system.md`:

| Climate | Human meaning | Default stance |
| --- | --- | --- |
| Spring | Risk-on start, breadth expands | Search long leaders |
| Summer | Main trend expansion, true leaders separate | Concentrate on leaders |
| Autumn | Fish-tail / catch-up, harder edge | Shrink size, lower frequency |
| Winter | Bear or garbage chop | Prefer no trade |

This climate is **long-biased market weather language**. It remains on UI / human summaries as `LegacyClimate`.

### 3.2 Machine directional cycle (code)

Machine state is **not** “winter = short”.

Machine state is:

```text
Direction × WavePhase × TimeframeRole
```

Defined fully in `CYCLE_MODEL.md`.

Critical freeze:

```text
NEUTRAL + WINTER  = no directional trade (chop / reset)
DOWN + SPRING     = downside just starting
DOWN + SUMMER     = main downtrend expansion
DOWN + AUTUMN     = downside exhaustion / catch-down
UP + SPRING/SUMMER = aligned long environment
```

**Forbidden machine rule:**

```text
winter ⇒ short allowed
```

Winter (legacy climate) may contain tradeable nested cycles, but only via explicit
`Direction × WavePhase` nodes and playbook gates — never via climate string alone.

---

## 4. Fixed timeframe roles (exactly three)

Do not invent more roles.

| Role | Suggested TFs | Answers |
| --- | --- | --- |
| **Context** | 1d + 4h | Primary direction, trend vs counter-trend, room up/down |
| **Setup** | 1h + 15m | Which playbook, leader/laggard quality |
| **Trigger** | 5m | Entry timing, structural invalidation |

Decision order is fixed:

```text
Context → Setup → Trigger
```

Forbidden:

```text
See 5m breakout first, then hunt higher-TF justification
```

---

## 5. MarketPulse boundary

MarketPulse remains the **attention system**.

It answers only:

1. Is the market worth opening the chart for?
2. Is short-horizon market direction up, down, or mixed?
3. Who are the impulse leaders / laggards?

It does **not**:

- Own daily/4h cycle classification
- Decide playbook grade alone
- Authorize risk templates alone

Relation:

```text
MarketPulse  = wake-up trigger
CycleMap     = environment interpreter
Playbook     = opportunity definition
Risk         = max loss budget
Human        = final selector
Journal      = proof of EV
```

On each valid MarketPulse event → one `OpportunitySession` (not one session per coin).

---

## 6. Gate + Rank + Grade (not one total score)

### Gate (non-compensable)

Any core failure → `NO_TRADE`:

- Unhealthy / incomplete data
- Context forbids the direction
- No valid structure / invalidation
- Insufficient room
- Liquidity / spread fail
- Candidate mid-box with no edge

### Rank (among survivors)

- Relative strength / weakness
- Pullback / rebound quality
- Market resonance
- Structure quality
- Volume quality

### Grade

| Grade | Meaning |
| --- | --- |
| A | Full alignment, clean phase stack |
| B | Direction OK, some TF imperfect |
| C | Counter-trend only; low-risk observation |
| D | No trade |

Grades are **not** calibrated probabilities.

---

## 7. Long / short symmetry

Every directional rule must have a mirror.

| Long concept | Short mirror |
| --- | --- |
| Relative strength | Relative weakness |
| Shallow pullback | Shallow bounce |
| Higher low | Lower high |
| Room up | Room down |
| Leader on impulse up | Core laggard on impulse down |

Scanner must not only reward positive change% / beat-BTC.

---

## 8. Human decision model

Every Decision Ticket ends in one of:

```text
accepted | waiting | rejected | missed
```

Reason codes are required (free text alone is not enough). Standard codes:

```text
structure_good
structure_bad
too_extended
not_real_leader
insufficient_room
counter_trend
market_breadth_weak
funding_crowded
late_to_event
emotional_skip
fear_of_loss
manual_override
```

Rejected / missed tickets still get counterfactual outcome tracking.

---

## 9. Risk model freeze (intent)

Risk is **max loss per trade**, not “max position % of equity first”.

```text
loss_budget   = equity × risk_budget_pct
notional      = loss_budget / stop_distance_pct
```

Risk templates (names only; numeric pct from config, not code constants):

```text
aligned_spring
aligned_summer
aligned_autumn
counter_trend
mixed_context
no_trade
```

Context owns risk ceiling / add-on permission.  
Trigger owns entry + stop only.  
Trigger beauty cannot override Context risk limits.

---

## 9b. 行为 Risk Gate（2026-08-09 加入，来自真实交易画像）

> 依据：2026-02→08 共 70 笔已关闭仓位（WR 42.9%、PF 0.76、-67.32 USDT）+
> 2026-06 独立样本 25 笔（WR 44%、PF 0.90）。两份数据共同结论：负 EV 的主要来源
> 是**单币反复交易 + 逆势 + 超短持仓**，不是胜率本身。

硬规则（违反 = 该笔不算合格交易，不产生 ticket）：

```text
1. Isolated only            — 实验账户一律逐仓;Cross 共享保证金 = 一笔爆全爆。
2. no <5m discretionary     — 禁止 5 分钟内人工剥头皮(6 笔 0 胜 -29.76)。
3. loss → 同币 cooldown     — 亏损平仓后同币 30–60min 冷却,除非产生新 Setup ID。
4. direction from CycleMap  — 禁止"涨多了感觉该空";空头必须有 DOWN context + 弱势币 rank。
5. evening live window     — 18:00–01:00(UTC+8) 才允许 live_eligible;
                              其他时间只 Observe/Shadow/Counterfactual。
6. risk sizing             — 单笔预设最大损失 $2–4(挑战账户 $100 口径)。
7. liquidation ≠ stop      — 爆仓永远不是止损;止损在进入前设定。
```

双向引擎（$100 → $1000 挑战，2026-08 起）：

```text
Engine A — Binance(约 $40): 吃 2R–5R 的系统化机会,晚上窗口,Isolated。
Engine B — Meme Launchpad(约 $40): 只做 Spot,凸性下注,单笔 $5–10 probe,允许归零。
Reserve — $20: gas/测试/补充 bankroll,不交易。
```

Meme 退出逻辑不截断右尾：1.5x 观察 / 2x 回收本金 / 3–5x 分批 / 剩余 runner
（具体阶梯等 Launch 数据回测后定，不拍脑袋）。

---

## 10. Stage-1 success criteria

Engineering:

- One session per market event
- Multi-TF CycleMap per candidate
- Long/short mirror
- Every ticket has invalidation
- Human decisions persisted
- Rejected opportunities still tracked

Product:

- Few tickets per session (cap 3)
- Clear aligned vs counter-trend label
- No “chop = short trend”
- Discovers laggards as well as leaders

Research:

- Accepted group EV > all-qualified EV
- Rejected group EV worse or ≤ 0
- Attribute alpha: market selection / coin selection / entry / exit

---

## 11. Explicit non-goals (now)

- Live auto execution
- LLM autonomous direction
- Multi-playbook launch (only `leader_pullback_v1`)
- ML models
- Microservice bus / Redis / Kafka
- Compensating total-score scanner as decision authority

---

## 12. Doc map

```text
trading-system.md              philosophy (keep)
TRADING_SYSTEM_CANONICAL.md    this file — disambiguation
CYCLE_MODEL.md                 machine cycle state
playbooks/LEADER_PULLBACK.md   first opportunity definition
```

Next code PRs implement types and detectors against these freezes.
They must not reintroduce `winter ⇒ short` or long-only relative strength.
