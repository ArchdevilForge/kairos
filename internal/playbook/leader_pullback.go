package playbook

import (
	"github.com/ArchdevilForge/kairos/internal/types"
)

// LeaderPullback is playbook leader_pullback_v1 (long/short mirrored).
type LeaderPullback struct {
	// MinRelativeEdge is min RS (long) or RW (short) to count as leader/laggard.
	MinRelativeEdge float64
	// MinRoomPct required on trade side from context nodes when room is present.
	MinRoomPct float64
}

// LeaderPullbackContext extends playbook context with trigger details needed for gates.
// Kept separate so types.PlaybookContext stays stable; desk fills this wrapper.
type LeaderPullbackInput struct {
	types.PlaybookContext

	// Invalidations must be non-empty to pass hard gate.
	Invalidations []string
	// StructureValid false → hard fail.
	StructureValid bool
	// MidBox true → hard fail (no edge).
	MidBox bool
	// LeaderRank 1 = best; 0 unknown. For long, rank by strength; short by weakness.
	LeaderRank int
	// MaxLeaderRank accepted as "leader" (default 3).
	MaxLeaderRank int
}

// ID implements Playbook.
func (p *LeaderPullback) ID() string { return types.PlaybookLeaderPullbackV1 }

// Match implements Playbook against bare context (no invalidation → fail closed).
func (p *LeaderPullback) Match(ctx types.PlaybookContext) types.PlaybookMatch {
	return p.MatchInput(LeaderPullbackInput{
		PlaybookContext: ctx,
		StructureValid:  false, // fail closed without explicit structure
		Invalidations:   nil,
	})
}

// MatchInput is the full gate path used by the desk.
func (p *LeaderPullback) MatchInput(in LeaderPullbackInput) types.PlaybookMatch {
	id := p.ID()
	minEdge := p.MinRelativeEdge
	if minEdge <= 0 {
		minEdge = 0.5
	}
	minRoom := p.MinRoomPct
	if minRoom <= 0 {
		minRoom = 1.0
	}
	maxRank := in.MaxLeaderRank
	if maxRank <= 0 {
		maxRank = 3
	}

	var failures []string

	if !in.Candidate.LiquidityOK {
		failures = append(failures, "liquidity_not_ok")
	}
	if !in.Candidate.SpreadOK {
		failures = append(failures, "spread_not_ok")
	}
	if !in.StructureValid {
		failures = append(failures, "structure_invalid")
	}
	if len(in.Invalidations) == 0 {
		failures = append(failures, "missing_invalidation")
	}
	if in.MidBox {
		failures = append(failures, "mid_box_no_edge")
	}

	mkt := in.MarketCycle
	sym := in.SymbolCycle
	ctxNodes := contextNodes(mkt)
	if len(contextNodes(sym)) > 0 {
		// prefer symbol context room, still use market for alignment
		_ = sym
	}
	if allContextWinterOrNeutral(ctxNodes) || mkt.Alignment == types.AlignmentNoTrade {
		failures = append(failures, "context_no_trade")
	}
	if mkt.Alignment == types.AlignmentMixed {
		failures = append(failures, "context_mixed")
	}

	if len(failures) > 0 {
		return fail(id, in.SessionID, in.Symbol, failures)
	}

	// Decide side from pulse + alignment preference
	pulse := in.PulseDirection
	primary := mkt.PrimaryDirection

	wantLong := false
	wantShort := false
	counter := false

	switch {
	case pulse == types.CycleDirectionUp && primary == types.CycleDirectionUp:
		wantLong = true
	case pulse == types.CycleDirectionDown && primary == types.CycleDirectionDown:
		wantShort = true
	case pulse == types.CycleDirectionUp && primary == types.CycleDirectionDown:
		// counter-trend long only if lower map says up
		if mkt.TradeClass == types.TradeClassCounterTrendLong ||
			anyDir(setupNodes(mkt), types.CycleDirectionUp) ||
			anyDir(triggerNodes(sym), types.CycleDirectionUp) ||
			sym.PrimaryDirection == types.CycleDirectionUp {
			wantLong = true
			counter = true
		} else {
			failures = append(failures, "pulse_up_but_no_counter_stack")
		}
	case pulse == types.CycleDirectionDown && primary == types.CycleDirectionUp:
		if mkt.TradeClass == types.TradeClassCounterTrendShort ||
			anyDir(setupNodes(mkt), types.CycleDirectionDown) ||
			sym.PrimaryDirection == types.CycleDirectionDown {
			wantShort = true
			counter = true
		} else {
			failures = append(failures, "pulse_down_but_no_counter_stack")
		}
	case pulse == types.CycleDirectionNeutral:
		failures = append(failures, "pulse_no_direction")
	default:
		// pulse aligned with lower but not primary — still allow counter path via trade class
		switch mkt.TradeClass {
		case types.TradeClassAlignedLong, types.TradeClassCounterTrendLong:
			wantLong = true
			counter = mkt.TradeClass == types.TradeClassCounterTrendLong
		case types.TradeClassAlignedShort, types.TradeClassCounterTrendShort:
			wantShort = true
			counter = mkt.TradeClass == types.TradeClassCounterTrendShort
		default:
			failures = append(failures, "cannot_derive_side")
		}
	}

	if len(failures) > 0 {
		return fail(id, in.SessionID, in.Symbol, failures)
	}

	var reasons, warnings []string
	var dir types.CycleDirection
	var tradeClass types.TradeClass
	var template string
	grade := types.TicketGradeA

	symSetup := setupNodes(sym)
	symTrig := triggerNodes(sym)
	if len(symSetup) == 0 {
		symSetup = setupNodes(mkt)
	}
	if len(symTrig) == 0 {
		symTrig = triggerNodes(mkt)
	}

	if wantLong {
		dir = types.CycleDirectionUp
		if in.Candidate.RelativeStrength < minEdge {
			failures = append(failures, "not_relative_leader")
		}
		if in.LeaderRank > 0 && in.LeaderRank > maxRank {
			failures = append(failures, "leader_rank_too_low")
		}
		room := maxRoom(append(contextNodes(sym), contextNodes(mkt)...), true)
		if room > 0 && room < minRoom {
			failures = append(failures, "insufficient_room_up")
		}
		if !anyDir(symSetup, types.CycleDirectionUp) && !anyDir(symTrig, types.CycleDirectionUp) {
			failures = append(failures, "setup_trigger_not_up")
		}
		// trigger spring restart preferred
		if anyPhase(symTrig, types.WavePhaseSpring) {
			reasons = append(reasons, "trigger UP/SPRING restart")
		} else if anyDir(symTrig, types.CycleDirectionUp) {
			warnings = append(warnings, "trigger up but not spring")
			grade = types.TicketGradeB
		}
		if anyPhase(ctxNodes, types.WavePhaseAutumn) {
			warnings = append(warnings, "context autumn — late trend")
			grade = minGrade(grade, types.TicketGradeB)
			template = types.RiskTemplateAlignedAutumn
		}
		if counter {
			tradeClass = types.TradeClassCounterTrendLong
			template = types.RiskTemplateCounterTrend
			grade = minGrade(grade, types.TicketGradeB)
			reasons = append(reasons, "counter_trend_long vs context")
		} else {
			tradeClass = types.TradeClassAlignedLong
			if template == "" {
				if anyPhase(ctxNodes, types.WavePhaseSpring) {
					template = types.RiskTemplateAlignedSpring
				} else {
					template = types.RiskTemplateAlignedSummer
				}
			}
			reasons = append(reasons, "aligned long leader pullback")
		}
		if in.Candidate.PullbackStrength >= 1 {
			reasons = append(reasons, "shallow pullback quality")
		} else {
			warnings = append(warnings, "pullback quality mediocre")
			grade = minGrade(grade, types.TicketGradeB)
		}
		reasons = append(reasons, "relative strength positive")
	}

	if wantShort {
		dir = types.CycleDirectionDown
		if in.Candidate.RelativeWeakness < minEdge {
			failures = append(failures, "not_relative_laggard")
		}
		if in.LeaderRank > 0 && in.LeaderRank > maxRank {
			failures = append(failures, "laggard_rank_too_low")
		}
		room := maxRoom(append(contextNodes(sym), contextNodes(mkt)...), false)
		if room > 0 && room < minRoom {
			failures = append(failures, "insufficient_room_down")
		}
		if !anyDir(symSetup, types.CycleDirectionDown) && !anyDir(symTrig, types.CycleDirectionDown) {
			failures = append(failures, "setup_trigger_not_down")
		}
		if anyPhase(symTrig, types.WavePhaseSpring) {
			reasons = append(reasons, "trigger DOWN/SPRING restart")
		} else if anyDir(symTrig, types.CycleDirectionDown) {
			warnings = append(warnings, "trigger down but not spring")
			grade = minGrade(grade, types.TicketGradeB)
		}
		if anyPhase(ctxNodes, types.WavePhaseAutumn) {
			warnings = append(warnings, "context autumn — late trend")
			grade = minGrade(grade, types.TicketGradeB)
			template = types.RiskTemplateAlignedAutumn
		}
		if counter {
			tradeClass = types.TradeClassCounterTrendShort
			template = types.RiskTemplateCounterTrend
			grade = minGrade(grade, types.TicketGradeB)
			reasons = append(reasons, "counter_trend_short vs context")
		} else {
			tradeClass = types.TradeClassAlignedShort
			if template == "" {
				if anyPhase(ctxNodes, types.WavePhaseSpring) {
					template = types.RiskTemplateAlignedSpring
				} else {
					template = types.RiskTemplateAlignedSummer
				}
			}
			reasons = append(reasons, "aligned short core laggard bounce-fail")
		}
		if in.Candidate.ReboundWeakness >= 1 {
			reasons = append(reasons, "weak rebound quality")
		} else {
			warnings = append(warnings, "rebound quality mediocre")
			grade = minGrade(grade, types.TicketGradeB)
		}
		reasons = append(reasons, "relative weakness positive")
	}

	// Context phase must be spring/summer for aligned; counter already capped
	if !counter && !anyPhase(ctxNodes, types.WavePhaseSpring, types.WavePhaseSummer) {
		failures = append(failures, "context_phase_not_spring_summer")
	}

	if len(failures) > 0 {
		return fail(id, in.SessionID, in.Symbol, failures)
	}

	if counter {
		grade = minGrade(grade, types.TicketGradeB)
		// extra warnings → C
		if len(warnings) >= 2 {
			grade = types.TicketGradeC
		}
	}

	return types.PlaybookMatch{
		SchemaVersion: types.PlaybookMatchSchemaVersion,
		PlaybookID:    id,
		SessionID:     in.SessionID,
		Symbol:        in.Symbol,
		Matched:       true,
		Grade:         grade,
		Direction:     dir,
		TradeClass:    tradeClass,
		Reasons:       reasons,
		Warnings:      warnings,
		RiskTemplate:  template,
	}
}

func minGrade(a, b types.TicketGrade) types.TicketGrade {
	order := map[types.TicketGrade]int{
		types.TicketGradeA: 1,
		types.TicketGradeB: 2,
		types.TicketGradeC: 3,
		types.TicketGradeD: 4,
	}
	if order[a] >= order[b] {
		return a
	}
	return b
}
