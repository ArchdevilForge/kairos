package cycle

import (
	"fmt"
	"sync"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// Classifier builds CycleNodes / CycleMaps. Shadow-safe: no scanner side effects.
type Classifier struct {
	Policy types.TransitionPolicy

	mu    sync.Mutex
	state map[stateKey]*stickyState
}

// NewClassifier returns a hysteresis-aware classifier.
func NewClassifier(policy types.TransitionPolicy) *Classifier {
	if policy.ConfirmBars == 0 && policy.MinStateBars == 0 && policy.MinConfidenceGain == 0 {
		policy = DefaultTransitionPolicy()
	}
	return &Classifier{
		Policy: policy,
		state:  make(map[stateKey]*stickyState),
	}
}

// Series is one TF of closed OHLCV (unix seconds timestamps optional).
type Series struct {
	Timeframe string
	Role      types.TimeframeRole
	Closes    []float64
	Highs     []float64
	Lows      []float64
	Volumes   []float64
}

// ClassifyNode detects direction+phase for one TF. symbol is hysteresis key only.
// Uses closed bars only (caller must not pass forming candle).
func (c *Classifier) ClassifyNode(symbol string, s Series) types.CycleNode {
	raw := classifyRaw(s)
	if c == nil {
		return raw
	}
	key := stateKey{symbol: symbol, timeframe: s.Timeframe}
	c.mu.Lock()
	defer c.mu.Unlock()
	prev := c.state[key]
	out, next := applyTransition(c.Policy, prev, raw)
	c.state[key] = next
	return out
}

// ClassifyMap classifies each series and builds hierarchy.
func (c *Classifier) ClassifyMap(symbol string, asOfUnix int64, legacy types.MarketPhase, series []Series) types.CycleMap {
	nodes := make(map[string]types.CycleNode, len(series))
	for _, s := range series {
		nodes[s.Timeframe] = c.ClassifyNode(symbol, s)
	}
	return BuildMap(asOfUnix, legacy, nodes)
}

// Reset clears hysteresis (tests / symbol rotation).
func (c *Classifier) Reset() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.state = make(map[stateKey]*stickyState)
	c.mu.Unlock()
}

func classifyRaw(s Series) types.CycleNode {
	highs, lows := s.Highs, s.Lows
	if len(highs) == 0 {
		highs = s.Closes
	}
	if len(lows) == 0 {
		lows = s.Closes
	}
	dir, dirConf, dirEv := detectDirection(s.Closes, highs, lows)
	phase, phaseConf, strength, mom, phaseEv := detectPhase(dir, s.Closes, s.Volumes)

	ev := append([]types.Evidence{}, dirEv...)
	ev = append(ev, phaseEv...)

	vol := 0.0
	if len(s.Closes) >= 15 {
		rets := make([]float64, 0, 14)
		for i := len(s.Closes) - 14; i < len(s.Closes); i++ {
			if s.Closes[i-1] == 0 {
				continue
			}
			rets = append(rets, (s.Closes[i]-s.Closes[i-1])/s.Closes[i-1]*100)
		}
		vol = stdPop(rets)
	}

	volQuality := 0.5
	if len(s.Volumes) >= 20 {
		r := mean(s.Volumes[len(s.Volumes)-10:])
		p := mean(s.Volumes[len(s.Volumes)-20 : len(s.Volumes)-10])
		if p > 0 {
			volQuality = clamp01(r / p / 2)
		}
	}

	// crude room vs prior swing extremes (exclude last 3 bars so the current
	// candle's own high/low does not zero out continuation room).
	roomUp, roomDown := 0.0, 0.0
	if len(highs) >= 40 && s.Closes[len(s.Closes)-1] > 0 {
		px := s.Closes[len(s.Closes)-1]
		end := len(highs) - 3
		if end < 10 {
			end = len(highs)
		}
		start := end - 40
		if start < 0 {
			start = 0
		}
		cap := maxOf(highs[start:end])
		floor := minOf(lows[start:end])
		if cap > px*1.002 {
			roomUp = (cap - px) / px * 100
		} else {
			// ponytail: breakout / at highs → open-air floor
			roomUp = 5
		}
		if floor < px*0.998 && floor > 0 {
			roomDown = (px - floor) / px * 100
		} else {
			roomDown = 5
		}
	}

	conf := clamp01(0.5*dirConf + 0.5*phaseConf)
	role := s.Role
	if role == "" {
		role = roleForTF(s.Timeframe)
	}

	return types.CycleNode{
		SchemaVersion:    types.CycleNodeSchemaVersion,
		Timeframe:        s.Timeframe,
		Role:             role,
		Direction:        dir,
		Phase:            phase,
		TrendStrength:    strength,
		StructureQuality: dirConf,
		MomentumChange:   mom,
		Volatility:       vol,
		VolumeQuality:    volQuality,
		RoomUpPct:        roomUp,
		RoomDownPct:      roomDown,
		Confidence:       conf,
		Evidence:         ev,
	}
}

func roleForTF(tf string) types.TimeframeRole {
	switch tf {
	case "1d", "1D", "4h", "4H":
		return types.TimeframeRoleContext
	case "1h", "1H", "15m", "15M":
		return types.TimeframeRoleSetup
	case "5m", "5M":
		return types.TimeframeRoleTrigger
	default:
		return types.TimeframeRoleSetup
	}
}

// String is a compact debug line.
func NodeString(n types.CycleNode) string {
	return fmt.Sprintf("%s %s/%s conf=%.2f", n.Timeframe, n.Direction, n.Phase, n.Confidence)
}
