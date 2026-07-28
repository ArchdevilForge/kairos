package cycle

import (
	"fmt"
	"sort"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// preferred context timeframe order when deriving primary direction.
var contextPreference = []string{"1d", "4h", "1D", "4H"}

// BuildMap assembles a CycleMap from per-TF nodes and computes alignment.
func BuildMap(asOfUnix int64, legacy types.MarketPhase, nodes map[string]types.CycleNode) types.CycleMap {
	if nodes == nil {
		nodes = map[string]types.CycleNode{}
	}
	primary, alignment, tradeClass, summary := classifyHierarchy(nodes)
	return types.CycleMap{
		SchemaVersion:    types.CycleMapSchemaVersion,
		AsOfUnix:         asOfUnix,
		LegacyClimate:    legacy,
		Nodes:            nodes,
		PrimaryDirection: primary,
		Alignment:        alignment,
		TradeClass:       tradeClass,
		Summary:          summary,
	}
}

func classifyHierarchy(nodes map[string]types.CycleNode) (types.CycleDirection, types.CycleAlignment, types.TradeClass, []string) {
	if len(nodes) == 0 {
		return types.CycleDirectionNeutral, types.AlignmentNoTrade, types.TradeClassNoTrade,
			[]string{"no cycle nodes"}
	}

	var context, setup, trigger []types.CycleNode
	for _, n := range nodes {
		switch n.Role {
		case types.TimeframeRoleContext:
			context = append(context, n)
		case types.TimeframeRoleSetup:
			setup = append(setup, n)
		case types.TimeframeRoleTrigger:
			trigger = append(trigger, n)
		}
	}

	primary := primaryFromContext(context, nodes)
	lowerDir := dominantDirection(append(append([]types.CycleNode{}, setup...), trigger...))

	summary := make([]string, 0, len(nodes)+2)
	// stable summary order
	keys := make([]string, 0, len(nodes))
	for k := range nodes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		n := nodes[k]
		summary = append(summary, fmt.Sprintf("%s: %s/%s", k, n.Direction, n.Phase))
	}

	// Context winter/neutral → no trade
	if contextIsNoTrade(context, primary) {
		summary = append(summary, "context no-trade environment")
		return primary, types.AlignmentNoTrade, types.TradeClassNoTrade, summary
	}

	// mixed: setup and trigger disagree hard
	if lowerMixed(setup, trigger) {
		summary = append(summary, "setup/trigger mixed")
		return primary, types.AlignmentMixed, types.TradeClassNoTrade, summary
	}

	if lowerDir == types.CycleDirectionNeutral {
		summary = append(summary, "lower timeframes neutral")
		return primary, types.AlignmentMixed, types.TradeClassNoTrade, summary
	}

	if lowerDir == primary {
		// pullback if trigger was recently opposite? we only have snapshot:
		// if any setup/trigger is autumn while context summer → still full/pullback
		if hasPhase(append(setup, trigger...), types.WavePhaseSpring) &&
			(hasPhase(context, types.WavePhaseSummer) || hasPhase(context, types.WavePhaseSpring)) {
			// spring on lower with aligned context = pullback restart or early align
			if hasPhase(setup, types.WavePhaseSummer) || hasPhase(setup, types.WavePhaseSpring) {
				summary = append(summary, "aligned trend stack")
				return primary, types.AlignmentFull, tradeClassFor(primary, false), summary
			}
		}
		summary = append(summary, "aligned with context")
		return primary, types.AlignmentFull, tradeClassFor(primary, false), summary
	}

	// lower opposite to context → counter trend
	summary = append(summary, "lower TF opposite context → counter_trend")
	return primary, types.AlignmentCounterTrend, tradeClassFor(lowerDir, true), summary
}

func tradeClassFor(dir types.CycleDirection, counter bool) types.TradeClass {
	switch {
	case dir == types.CycleDirectionUp && !counter:
		return types.TradeClassAlignedLong
	case dir == types.CycleDirectionDown && !counter:
		return types.TradeClassAlignedShort
	case dir == types.CycleDirectionUp && counter:
		return types.TradeClassCounterTrendLong
	case dir == types.CycleDirectionDown && counter:
		return types.TradeClassCounterTrendShort
	default:
		return types.TradeClassNoTrade
	}
}

func primaryFromContext(context []types.CycleNode, all map[string]types.CycleNode) types.CycleDirection {
	// prefer named TFs
	for _, tf := range contextPreference {
		if n, ok := all[tf]; ok && n.Role == types.TimeframeRoleContext {
			if n.Direction != types.CycleDirectionNeutral {
				return n.Direction
			}
		}
	}
	if d := dominantDirection(context); d != types.CycleDirectionNeutral {
		return d
	}
	// fall back any node
	return dominantDirection(values(all))
}

func contextIsNoTrade(context []types.CycleNode, primary types.CycleDirection) bool {
	if len(context) == 0 {
		return primary == types.CycleDirectionNeutral
	}
	winterish := 0
	for _, n := range context {
		if n.Direction == types.CycleDirectionNeutral || n.Phase == types.WavePhaseWinter {
			winterish++
		}
	}
	if winterish == len(context) {
		return true
	}
	return primary == types.CycleDirectionNeutral
}

func dominantDirection(nodes []types.CycleNode) types.CycleDirection {
	if len(nodes) == 0 {
		return types.CycleDirectionNeutral
	}
	up, down, neu := 0, 0, 0
	for _, n := range nodes {
		switch n.Direction {
		case types.CycleDirectionUp:
			up++
		case types.CycleDirectionDown:
			down++
		default:
			neu++
		}
	}
	switch {
	case up > down && up >= neu:
		return types.CycleDirectionUp
	case down > up && down >= neu:
		return types.CycleDirectionDown
	default:
		return types.CycleDirectionNeutral
	}
}

func lowerMixed(setup, trigger []types.CycleNode) bool {
	sd := dominantDirection(setup)
	td := dominantDirection(trigger)
	if sd == types.CycleDirectionNeutral || td == types.CycleDirectionNeutral {
		return false
	}
	return sd != td
}

func hasPhase(nodes []types.CycleNode, phase types.WavePhase) bool {
	for _, n := range nodes {
		if n.Phase == phase {
			return true
		}
	}
	return false
}

func values(m map[string]types.CycleNode) []types.CycleNode {
	out := make([]types.CycleNode, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}
