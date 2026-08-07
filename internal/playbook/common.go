package playbook

import "github.com/ArchdevilForge/kairos/internal/types"

func contextNodes(m types.CycleMap) []types.CycleNode {
	var out []types.CycleNode
	for _, n := range m.Nodes {
		if n.Role == types.TimeframeRoleContext {
			out = append(out, n)
		}
	}
	return out
}

func setupNodes(m types.CycleMap) []types.CycleNode {
	var out []types.CycleNode
	for _, n := range m.Nodes {
		if n.Role == types.TimeframeRoleSetup {
			out = append(out, n)
		}
	}
	return out
}

func triggerNodes(m types.CycleMap) []types.CycleNode {
	var out []types.CycleNode
	for _, n := range m.Nodes {
		if n.Role == types.TimeframeRoleTrigger {
			out = append(out, n)
		}
	}
	return out
}

func anyDir(nodes []types.CycleNode, dir types.CycleDirection) bool {
	for _, n := range nodes {
		if n.Direction == dir {
			return true
		}
	}
	return false
}

func anyPhase(nodes []types.CycleNode, phases ...types.WavePhase) bool {
	set := map[types.WavePhase]bool{}
	for _, p := range phases {
		set[p] = true
	}
	for _, n := range nodes {
		if set[n.Phase] {
			return true
		}
	}
	return false
}

func allContextWinterOrNeutral(nodes []types.CycleNode) bool {
	if len(nodes) == 0 {
		return true
	}
	for _, n := range nodes {
		if n.Direction != types.CycleDirectionNeutral && n.Phase != types.WavePhaseWinter {
			return false
		}
	}
	return true
}

func maxRoom(nodes []types.CycleNode, up bool) float64 {
	best := 0.0
	for _, n := range nodes {
		v := n.RoomDownPct
		if up {
			v = n.RoomUpPct
		}
		if v > best {
			best = v
		}
	}
	return best
}

func fail(id, session, symbol string, failures []string) types.PlaybookMatch {
	return types.PlaybookMatch{
		SchemaVersion: types.PlaybookMatchSchemaVersion,
		PlaybookID:    id,
		SessionID:     session,
		Symbol:        symbol,
		Matched:       false,
		Grade:         types.TicketGradeD,
		Direction:     types.CycleDirectionNeutral,
		TradeClass:    types.TradeClassNoTrade,
		HardFailures:  failures,
		RiskTemplate:  types.RiskTemplateNoTrade,
	}
}
