# Cycle Model V2

> Status: **canonical freeze (PR1)**  
> Parent: `docs/TRADING_SYSTEM_CANONICAL.md`  
> Replaces machine authority of single `MarketPhase` from BTC 1d `DetectPhase`

Legacy detector (`internal/indicators/cycle.go` → `MarketCycle`) remains as
**LegacyClimate / shadow** only. It does not grant trade permission.

---

## 1. Core idea

Machine state is multi-timeframe and bidirectional:

```text
Direction × WavePhase × Timeframe (with Role)
```

Human climate (春夏秋冬) is a separate display field: `LegacyClimate`.

Example that current single-phase model cannot express:

```text
1d:  DOWN / SUMMER
4h:  UP   / SPRING
15m: UP   / SUMMER
5m:  UP   / SPRING

→ Alignment: counter_trend
→ TradeClass: counter_trend_long (if playbook matches)
```

---

## 2. Types (contract freeze)

Schema version field required on persisted cycle snapshots: `schema_version`.

### 2.1 Direction

```go
type CycleDirection string

const (
    CycleDirectionUp      CycleDirection = "up"
    CycleDirectionDown    CycleDirection = "down"
    CycleDirectionNeutral CycleDirection = "neutral"
)
```

### 2.2 WavePhase

```go
type WavePhase string

const (
    WavePhaseSpring WavePhase = "spring" // new direction starting
    WavePhaseSummer WavePhase = "summer" // trend expansion
    WavePhaseAutumn WavePhase = "autumn" // trend decay
    WavePhaseWinter WavePhase = "winter" // no direction / reset / garbage chop
)
```

Semantics:

| Direction | Phase | Meaning |
| --- | --- | --- |
| UP | SPRING | Upside just started |
| UP | SUMMER | Main uptrend |
| UP | AUTUMN | Upside exhaustion |
| DOWN | SPRING | Downside just started |
| DOWN | SUMMER | Main downtrend |
| DOWN | AUTUMN | Downside exhaustion / catch-down |
| NEUTRAL | WINTER | Chop / no trade environment |
| * | WINTER | Prefer no directional trend trade on that TF |

**Winter is not a short signal.**

### 2.3 Timeframe role

```go
type TimeframeRole string

const (
    TimeframeRoleContext TimeframeRole = "context"
    TimeframeRoleSetup   TimeframeRole = "setup"
    TimeframeRoleTrigger TimeframeRole = "trigger"
)
```

Default mapping (config may list TFs; roles stay three):

```yaml
contextTimeframes: ["1d", "4h"]
setupTimeframes:   ["1h", "15m"]
triggerTimeframes: ["5m"]
```

### 2.4 Evidence

```go
type Evidence struct {
    Code        string  `json:"code"`
    Description string  `json:"description"`
    Value       float64 `json:"value,omitempty"`
}
```

Every `CycleNode` must carry evidence. Bare `confidence` without evidence is incomplete.

### 2.5 CycleNode

```go
type CycleNode struct {
    Timeframe string        `json:"timeframe"`
    Role      TimeframeRole `json:"role"`

    Direction CycleDirection `json:"direction"`
    Phase     WavePhase      `json:"phase"`

    TrendStrength    float64 `json:"trend_strength"`
    StructureQuality float64 `json:"structure_quality"`
    MomentumChange   float64 `json:"momentum_change"`
    Volatility       float64 `json:"volatility"`
    VolumeQuality    float64 `json:"volume_quality"`

    RoomUpPct   float64 `json:"room_up_pct"`
    RoomDownPct float64 `json:"room_down_pct"`

    Confidence float64    `json:"confidence"`
    Evidence   []Evidence `json:"evidence"`
}
```

### 2.6 Alignment / trade class

```go
type CycleAlignment string

const (
    AlignmentFull         CycleAlignment = "full_alignment"
    AlignmentPullback     CycleAlignment = "trend_pullback"
    AlignmentCounterTrend CycleAlignment = "counter_trend"
    AlignmentMixed        CycleAlignment = "mixed"
    AlignmentNoTrade      CycleAlignment = "no_trade"
)
```

```go
type TradeClass string
// examples: aligned_long | aligned_short | counter_trend_long | counter_trend_short | no_trade
```

### 2.7 CycleMap

```go
type CycleMap struct {
    AsOfUnix int64 `json:"as_of_unix"`

    LegacyClimate MarketPhase `json:"legacy_climate"` // display only

    Nodes map[string]CycleNode `json:"nodes"` // key = timeframe

    PrimaryDirection CycleDirection `json:"primary_direction"`
    Alignment        CycleAlignment `json:"alignment"`
    TradeClass       TradeClass     `json:"trade_class"`

    Summary []string `json:"summary"`
}
```

---

## 3. Detection order

Always:

```text
1. Direction per TF
2. WavePhase per TF (given direction)
3. Build CycleMap hierarchy
4. Alignment + TradeClass
```

Never score spring/summer/autumn/winter first from BTC 7d/30d change alone and then invent direction.

### 3.1 Direction V1 (few interpretable inputs)

- Price vs mid-term MA
- Fast/slow MA slope
- Swing high/low structure
- Recent range break direction
- Higher-low / lower-high after pullback

Output: `up | down | neutral`

### 3.2 Phase V1

**Spring**

- Switch from neutral or opposite direction
- Important structure break
- Trend strength rising
- Breadth starting to expand (market map)

**Summer**

- Direction persists
- Stable HH/HL or LH/LL
- High trend strength
- Pullbacks continue in direction

**Autumn**

- Direction still present
- Trend strength decaying
- Breadth shrinking
- More false breaks
- Leaders stall (market map)

**Winter**

- No stable direction
- Repeated MA crosses
- Serial false breaks
- Structure does not offer usable R

### 3.3 Transition hysteresis

```go
type TransitionPolicy struct {
    ConfirmBars       int
    MinConfidenceGain float64
    MinStateBars      int
}
```

Default intent:

```yaml
transition:
  confirmBars: 3
  minStateBars: 3
  minConfidenceGain: 0.15
```

Preferred phase flow:

```text
WINTER → SPRING → SUMMER → AUTUMN → WINTER
```

Direction reverse:

```text
UP/AUTUMN → NEUTRAL/WINTER → DOWN/SPRING
```

Discouraged:

```text
UP/SUMMER → DOWN/SUMMER   (no reset)
```

---

## 4. Hierarchy rules

### Context (1d + 4h)

Defines:

- Primary direction
- Aligned vs counter-trend
- Risk template family
- Whether trend holding is allowed

### Setup (1h + 15m)

Defines:

- Playbook eligibility
- Leader / core-laggard quality
- Break / pullback / second-wave class

### Trigger (5m)

Defines:

- Entry timing
- Structural stop / invalidation

Trigger never upgrades risk beyond Context template.

### Alignment examples

| Context | Setup/Trigger | Alignment | TradeClass |
| --- | --- | --- | --- |
| UP/SUMMER | UP pullback then UP/SPRING | trend_pullback | aligned_long |
| DOWN/SUMMER | DOWN bounce then DOWN/SPRING | trend_pullback | aligned_short |
| DOWN/SUMMER | UP/SPRING stack | counter_trend | counter_trend_long |
| NEUTRAL/WINTER | any noisy impulse | no_trade | no_trade |
| Mixed TF chaos | — | mixed | no_trade or C-only observe |

---

## 5. Closed bar rule

Default: only **closed** candles enter Direction/Phase.

Trigger may declare mode:

```text
closed_bar (default)
intrabar   (explicit only)
```

No lookahead: state at time T uses bars fully closed at T.

---

## 6. Legacy boundary

| Item | Role after V2 |
| --- | --- |
| `MarketPhase` spring/summer/autumn/winter | `LegacyClimate` display + shadow compare |
| `DetectPhase` BTC 1d | legacy only |
| `cycleSupports(winter, short)` style gates | **retired as authority** |
| CycleMap nodes | new authority for playbooks / risk |

Shadow mode: emit both legacy and V2; decisions still use old path until pipeline PR enables V2 gates.

---

## 7. Package layout (future PRs)

```text
internal/cycle/
  classifier.go
  direction.go
  phase.go
  hierarchy.go
  transition.go
  market_context.go
  evidence.go
```

Legacy file stays available as `legacy_cycle` (rename or copy) for comparison.

---

## 8. Acceptance fixtures (required later)

1. **Mirror symmetry**: up sequence → UP/SPRING→SUMMER; price-mirrored → DOWN/SPRING→SUMMER
2. **Nested counter-trend**: 1d DOWN/SUMMER + lower TF UP stack → counter_trend_long, grade ≤ C, add-on false
3. **Chop winter**: MA thrash + false breaks → neutral/winter, alignment no_trade
4. **Stability**: last-bar noise must not flip SPRING↔AUTUMN↔SUMMER without hysteresis
5. **Replay equality**: same inputs + config → identical CycleMap

---

## 9. What CycleModel does not do

- Does not place orders
- Does not replace MarketPulse attention
- Does not alone emit Decision Tickets
- Does not use LLM
- Does not treat winter as short permission
