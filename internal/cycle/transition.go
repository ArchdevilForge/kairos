package cycle

import (
	"github.com/ArchdevilForge/kairos/internal/types"
)

// DefaultTransitionPolicy matches the freeze defaults.
func DefaultTransitionPolicy() types.TransitionPolicy {
	return types.TransitionPolicy{
		ConfirmBars:       3,
		MinStateBars:      3,
		MinConfidenceGain: 0.15,
	}
}

// stateKey tracks hysteresis per symbol|timeframe.
type stateKey struct {
	symbol    string
	timeframe string
}

type stickyState struct {
	node        types.CycleNode
	barsInState int
	lastBarUnix int64 // identity of last closed bar that advanced state
	// pending candidate while confirming a switch
	pending      types.CycleNode
	pendingCount int
	pendingBar   int64
}

// applyTransition enforces confirm bars / min state bars / confidence gain.
// barUnix identifies the last closed bar. Same barUnix as prev → refresh metrics
// only; do NOT increment barsInState/pendingCount (poll ≠ new bar).
func applyTransition(policy types.TransitionPolicy, prev *stickyState, raw types.CycleNode, barUnix int64) (types.CycleNode, *stickyState) {
	if policy.ConfirmBars <= 0 {
		policy.ConfirmBars = 1
	}
	if policy.MinStateBars <= 0 {
		policy.MinStateBars = 1
	}

	if prev == nil {
		s := &stickyState{node: raw, barsInState: 1, lastBarUnix: barUnix}
		return raw, s
	}

	// Same closed bar re-polled: refresh display metrics, freeze transition counters.
	if barUnix > 0 && barUnix == prev.lastBarUnix {
		out := prev.node
		out.TrendStrength = raw.TrendStrength
		out.MomentumChange = raw.MomentumChange
		out.Volatility = raw.Volatility
		out.VolumeQuality = raw.VolumeQuality
		out.RoomUpPct = raw.RoomUpPct
		out.RoomDownPct = raw.RoomDownPct
		out.StructureQuality = raw.StructureQuality
		out.Confidence = raw.Confidence
		out.Evidence = raw.Evidence
		// keep direction/phase from sticky identity
		prev.node = out
		return out, prev
	}

	same := prev.node.Direction == raw.Direction && prev.node.Phase == raw.Phase
	if same {
		prev.node = raw
		prev.barsInState++
		prev.lastBarUnix = barUnix
		prev.pendingCount = 0
		prev.pending = types.CycleNode{}
		prev.pendingBar = 0
		return prev.node, prev
	}

	confGain := raw.Confidence - prev.node.Confidence
	canLeave := prev.barsInState >= policy.MinStateBars
	strongJump := confGain >= policy.MinConfidenceGain && raw.Confidence >= prev.node.Confidence

	if !canLeave && !strongJump {
		prev.barsInState++ // still a new bar observation while holding
		prev.lastBarUnix = barUnix
		prev.pendingCount = 0
		out := prev.node
		out.TrendStrength = raw.TrendStrength
		out.MomentumChange = raw.MomentumChange
		out.Volatility = raw.Volatility
		out.VolumeQuality = raw.VolumeQuality
		out.RoomUpPct = raw.RoomUpPct
		out.RoomDownPct = raw.RoomDownPct
		return out, prev
	}

	// pending confirmation — only count distinct bars
	if prev.pending.Direction == raw.Direction && prev.pending.Phase == raw.Phase {
		if barUnix == 0 || barUnix != prev.pendingBar {
			prev.pendingCount++
			prev.pendingBar = barUnix
		}
		prev.pending = raw
	} else {
		prev.pending = raw
		prev.pendingCount = 1
		prev.pendingBar = barUnix
	}

	if prev.pendingCount >= policy.ConfirmBars || strongJump {
		out := prev.pending
		return out, &stickyState{node: out, barsInState: 1, lastBarUnix: barUnix}
	}

	prev.barsInState++
	prev.lastBarUnix = barUnix
	out := prev.node
	out.TrendStrength = raw.TrendStrength
	out.MomentumChange = raw.MomentumChange
	out.Volatility = raw.Volatility
	out.VolumeQuality = raw.VolumeQuality
	return out, prev
}
