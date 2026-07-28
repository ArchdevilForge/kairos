package playbook

import (
	"testing"

	"github.com/ArchdevilForge/kairos/internal/types"
)

func node(tf string, role types.TimeframeRole, dir types.CycleDirection, phase types.WavePhase, roomUp, roomDown float64) types.CycleNode {
	return types.CycleNode{
		SchemaVersion: types.CycleNodeSchemaVersion,
		Timeframe:     tf,
		Role:          role,
		Direction:     dir,
		Phase:         phase,
		RoomUpPct:     roomUp,
		RoomDownPct:   roomDown,
		Confidence:    0.7,
		Evidence:      []types.Evidence{{Code: "test", Description: "fixture"}},
	}
}

func alignedLongInput() LeaderPullbackInput {
	mkt := types.CycleMap{
		SchemaVersion:    types.CycleMapSchemaVersion,
		PrimaryDirection: types.CycleDirectionUp,
		Alignment:        types.AlignmentFull,
		TradeClass:       types.TradeClassAlignedLong,
		Nodes: map[string]types.CycleNode{
			"1d":  node("1d", types.TimeframeRoleContext, types.CycleDirectionUp, types.WavePhaseSummer, 10, 3),
			"4h":  node("4h", types.TimeframeRoleContext, types.CycleDirectionUp, types.WavePhaseSummer, 8, 3),
			"15m": node("15m", types.TimeframeRoleSetup, types.CycleDirectionUp, types.WavePhaseSummer, 5, 2),
			"5m":  node("5m", types.TimeframeRoleTrigger, types.CycleDirectionUp, types.WavePhaseSpring, 3, 1),
		},
	}
	return LeaderPullbackInput{
		PlaybookContext: types.PlaybookContext{
			SessionID:      "s1",
			Symbol:         "SOL/USDT:USDT",
			PulseState:     types.MarketStateImpulseUp,
			PulseDirection: types.CycleDirectionUp,
			MarketCycle:    mkt,
			SymbolCycle:    mkt,
			Candidate: types.DirectionalCandidate{
				Symbol:           "SOL/USDT:USDT",
				LongScore:        8,
				ShortScore:       1,
				RelativeStrength: 3,
				PullbackStrength: 1.5,
				LiquidityOK:      true,
				SpreadOK:         true,
			},
		},
		Invalidations:  []string{"5m swing low breaks"},
		StructureValid: true,
		LeaderRank:     1,
	}
}

func TestLeaderPullback_AlignedLong(t *testing.T) {
	p := &LeaderPullback{}
	m := p.MatchInput(alignedLongInput())
	if !m.Matched || m.Grade == types.TicketGradeD {
		t.Fatalf("want match, got %+v", m)
	}
	if m.Direction != types.CycleDirectionUp || m.TradeClass != types.TradeClassAlignedLong {
		t.Fatalf("dir/class=%s/%s", m.Direction, m.TradeClass)
	}
	if m.RiskTemplate == types.RiskTemplateNoTrade || m.RiskTemplate == types.RiskTemplateCounterTrend {
		t.Fatalf("template=%s", m.RiskTemplate)
	}
	if m.PlaybookID != types.PlaybookLeaderPullbackV1 {
		t.Fatalf("id=%s", m.PlaybookID)
	}
}

func TestLeaderPullback_AlignedShortMirror(t *testing.T) {
	in := alignedLongInput()
	// mirror maps
	for k, n := range in.MarketCycle.Nodes {
		n.Direction = types.CycleDirectionDown
		if n.Phase == types.WavePhaseSpring {
			n.Phase = types.WavePhaseSpring
		}
		n.RoomUpPct, n.RoomDownPct = n.RoomDownPct, n.RoomUpPct
		in.MarketCycle.Nodes[k] = n
	}
	in.SymbolCycle = in.MarketCycle
	in.MarketCycle.PrimaryDirection = types.CycleDirectionDown
	in.MarketCycle.TradeClass = types.TradeClassAlignedShort
	in.PulseDirection = types.CycleDirectionDown
	in.PulseState = types.MarketStateImpulseDown
	in.Candidate.RelativeStrength = 0.1
	in.Candidate.RelativeWeakness = 3
	in.Candidate.PullbackStrength = 0.2
	in.Candidate.ReboundWeakness = 1.5
	in.Candidate.LongScore, in.Candidate.ShortScore = 1, 8

	p := &LeaderPullback{}
	m := p.MatchInput(in)
	if !m.Matched {
		t.Fatalf("short mirror should match: %+v", m)
	}
	if m.Direction != types.CycleDirectionDown || m.TradeClass != types.TradeClassAlignedShort {
		t.Fatalf("got %s/%s failures=%v", m.Direction, m.TradeClass, m.HardFailures)
	}
}

func TestLeaderPullback_CounterTrendCap(t *testing.T) {
	in := alignedLongInput()
	// 1d/4h down summer, lower up
	in.MarketCycle.Nodes["1d"] = node("1d", types.TimeframeRoleContext, types.CycleDirectionDown, types.WavePhaseSummer, 3, 10)
	in.MarketCycle.Nodes["4h"] = node("4h", types.TimeframeRoleContext, types.CycleDirectionDown, types.WavePhaseSummer, 3, 8)
	in.MarketCycle.PrimaryDirection = types.CycleDirectionDown
	in.MarketCycle.Alignment = types.AlignmentCounterTrend
	in.MarketCycle.TradeClass = types.TradeClassCounterTrendLong
	in.SymbolCycle = in.MarketCycle
	in.PulseDirection = types.CycleDirectionUp

	p := &LeaderPullback{}
	m := p.MatchInput(in)
	if !m.Matched {
		t.Fatalf("counter trend long should match: %+v", m)
	}
	if m.TradeClass != types.TradeClassCounterTrendLong {
		t.Fatalf("class=%s", m.TradeClass)
	}
	if m.Grade == types.TicketGradeA {
		t.Fatalf("counter trend must cap grade <= B, got A")
	}
	if m.RiskTemplate != types.RiskTemplateCounterTrend {
		t.Fatalf("template=%s", m.RiskTemplate)
	}
}

func TestLeaderPullback_WinterNoTrade(t *testing.T) {
	in := alignedLongInput()
	in.MarketCycle.Nodes["1d"] = node("1d", types.TimeframeRoleContext, types.CycleDirectionNeutral, types.WavePhaseWinter, 0, 0)
	in.MarketCycle.Nodes["4h"] = node("4h", types.TimeframeRoleContext, types.CycleDirectionNeutral, types.WavePhaseWinter, 0, 0)
	in.MarketCycle.Alignment = types.AlignmentNoTrade
	in.MarketCycle.PrimaryDirection = types.CycleDirectionNeutral
	in.SymbolCycle = in.MarketCycle

	m := (&LeaderPullback{}).MatchInput(in)
	if m.Matched || m.Grade != types.TicketGradeD {
		t.Fatalf("winter must no-trade: %+v", m)
	}
}

func TestLeaderPullback_MissingInvalidation(t *testing.T) {
	in := alignedLongInput()
	in.Invalidations = nil
	m := (&LeaderPullback{}).MatchInput(in)
	if m.Matched {
		t.Fatal("missing invalidation must fail")
	}
	found := false
	for _, f := range m.HardFailures {
		if f == "missing_invalidation" {
			found = true
		}
	}
	if !found {
		t.Fatalf("failures=%v", m.HardFailures)
	}
}

func TestLeaderPullback_BareMatchFailClosed(t *testing.T) {
	m := (&LeaderPullback{}).Match(alignedLongInput().PlaybookContext)
	if m.Matched {
		t.Fatal("bare Match without structure/invalidation must fail closed")
	}
}

func TestRegistry_DefaultHasLeaderPullback(t *testing.T) {
	r := DefaultRegistry()
	p, ok := r.Get(types.PlaybookLeaderPullbackV1)
	if !ok || p.ID() != types.PlaybookLeaderPullbackV1 {
		t.Fatal("missing leader pullback")
	}
}
