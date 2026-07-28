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
	// pending candidate while confirming a switch
	pending      types.CycleNode
	pendingCount int
}

// applyTransition enforces confirm bars / min state bars / confidence gain.
// raw is the freshly detected node; prev may be nil on first bar.
func applyTransition(policy types.TransitionPolicy, prev *stickyState, raw types.CycleNode) (types.CycleNode, *stickyState) {
	if policy.ConfirmBars <= 0 {
		policy.ConfirmBars = 1
	}
	if policy.MinStateBars <= 0 {
		policy.MinStateBars = 1
	}

	if prev == nil {
		s := &stickyState{node: raw, barsInState: 1}
		return raw, s
	}

	same := prev.node.Direction == raw.Direction && prev.node.Phase == raw.Phase
	if same {
		prev.node = raw // refresh metrics/evidence, keep identity
		prev.barsInState++
		prev.pendingCount = 0
		prev.pending = types.CycleNode{}
		return prev.node, prev
	}

	// Same direction, adjacent phase soft switch still needs confirm — but allow
	// faster upgrade when confidence jumps.
	confGain := raw.Confidence - prev.node.Confidence
	canLeave := prev.barsInState >= policy.MinStateBars
	strongJump := confGain >= policy.MinConfidenceGain && raw.Confidence >= prev.node.Confidence

	if !canLeave && !strongJump {
		// hold previous identity; keep counting
		prev.barsInState++
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

	// accumulate pending confirmation
	if prev.pending.Direction == raw.Direction && prev.pending.Phase == raw.Phase {
		prev.pendingCount++
		prev.pending = raw
	} else {
		prev.pending = raw
		prev.pendingCount = 1
	}

	if prev.pendingCount >= policy.ConfirmBars || strongJump {
		out := prev.pending
		return out, &stickyState{node: out, barsInState: 1}
	}

	// not confirmed yet
	prev.barsInState++
	out := prev.node
	out.TrendStrength = raw.TrendStrength
	out.MomentumChange = raw.MomentumChange
	out.Volatility = raw.Volatility
	out.VolumeQuality = raw.VolumeQuality
	return out, prev
}
