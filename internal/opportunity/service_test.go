package opportunity

import (
	"path/filepath"
	"testing"

	"github.com/ArchdevilForge/kairos/internal/ranker"
	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
)

func measuredInput(sym string, change, market float64) ranker.Input {
	return ranker.Input{
		Symbol: sym, ChangePct: change, MarketMedianChange: market,
		BTCChangeSet: true, BTCChange: 0.5, QuoteVolume: 5e6,
		PullbackDepthPct: 1, PullbackMeasured: true,
		ReboundPct: 2, ReboundMeasured: true,
		RoomUpPct: 10, RoomDownPct: 3, RoomMeasured: true,
		LiquidityOK: true, LiquidityMeasured: true,
		SpreadOK: true, SpreadMeasured: true, DataOK: true,
	}
}

func testJournal(t *testing.T) *storage.Journal {
	t.Helper()
	j, err := storage.NewJournal(types.StorageConfig{
		DatabasePath: filepath.Join(t.TempDir(), "kairos.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func cycleUp() types.CycleMap {
	n := func(tf string, role types.TimeframeRole, dir types.CycleDirection, phase types.WavePhase) types.CycleNode {
		return types.CycleNode{
			SchemaVersion: types.CycleNodeSchemaVersion,
			Timeframe:     tf, Role: role, Direction: dir, Phase: phase,
			RoomUpPct: 10, RoomDownPct: 3, Confidence: 0.7,
			Evidence: []types.Evidence{{Code: "t", Description: "t"}},
		}
	}
	return types.CycleMap{
		SchemaVersion:    types.CycleMapSchemaVersion,
		PrimaryDirection: types.CycleDirectionUp,
		Alignment:        types.AlignmentFull,
		TradeClass:       types.TradeClassAlignedLong,
		Nodes: map[string]types.CycleNode{
			"1d":  n("1d", types.TimeframeRoleContext, types.CycleDirectionUp, types.WavePhaseSummer),
			"4h":  n("4h", types.TimeframeRoleContext, types.CycleDirectionUp, types.WavePhaseSummer),
			"15m": n("15m", types.TimeframeRoleSetup, types.CycleDirectionUp, types.WavePhaseSummer),
			"5m":  n("5m", types.TimeframeRoleTrigger, types.CycleDirectionUp, types.WavePhaseSpring),
		},
	}
}

func TestHandlePulseEvent_OneSessionPerEvent(t *testing.T) {
	s := NewService(testJournal(t), DefaultConfig())
	evt := types.AnomalyEvent{
		EventType: "market_impulse",
		EventID:   "e1",
		Timestamp: 1000,
		Data:      map[string]any{"direction": "up", "state_to": "IMPULSE_UP", "leaders": []string{"SOL/USDT:USDT"}},
	}
	sess, err := s.HandlePulseEvent(evt)
	if err != nil || sess == nil || sess.ID == "" {
		t.Fatalf("sess=%v err=%v", sess, err)
	}
	again, err := s.HandlePulseEvent(evt)
	if err != nil || again != nil {
		t.Fatalf("duplicate should no-op, got %v err=%v", again, err)
	}
	list, err := s.journal.ListSessions()
	if err != nil || len(list) != 1 {
		t.Fatalf("sessions=%d err=%v", len(list), err)
	}
}

func TestEvaluate_MaxThreeTickets(t *testing.T) {
	j := testJournal(t)
	s := NewService(j, DefaultConfig())
	mkt := cycleUp()
	var inputs []ranker.Input
	syms := []string{"A/USDT:USDT", "B/USDT:USDT", "C/USDT:USDT", "D/USDT:USDT"}
	for i, sym := range syms {
		inputs = append(inputs, measuredInput(sym, 8-float64(i), 1))
	}
	inv := map[string][]string{}
	structOK := map[string]bool{}
	entry := map[string]float64{}
	stop := map[string]float64{}
	for _, sym := range syms {
		inv[sym] = []string{"5m low breaks"}
		structOK[sym] = true
		entry[sym] = 100
		stop[sym] = 97
	}
	res, err := s.Evaluate(EvaluateRequest{
		EventID:        "ev-max",
		PulseState:     types.MarketStateImpulseUp,
		PulseDirection: types.CycleDirectionUp,
		MarketCycle:    mkt,
		SymbolCycles:   map[string]types.CycleMap{"A/USDT:USDT": mkt, "B/USDT:USDT": mkt, "C/USDT:USDT": mkt, "D/USDT:USDT": mkt},
		RankInputs:     inputs,
		Invalidations:  inv,
		StructureValid: structOK,
		EntryPrice:     entry,
		StopPrice:      stop,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tickets) != 3 {
		t.Fatalf("want 3 tickets, got %d matches=%d", len(res.Tickets), len(res.Matches))
	}
	tickets, err := j.ListTickets(res.Session.ID)
	if err != nil || len(tickets) != 3 {
		t.Fatalf("persisted tickets=%d err=%v", len(tickets), err)
	}
}

func TestApplyHumanDecision_Reject(t *testing.T) {
	j := testJournal(t)
	s := NewService(j, DefaultConfig())
	mkt := cycleUp()
	res, err := s.Evaluate(EvaluateRequest{
		EventID:        "ev-dec",
		PulseState:     types.MarketStateImpulseUp,
		PulseDirection: types.CycleDirectionUp,
		MarketCycle:    mkt,
		SymbolCycles:   map[string]types.CycleMap{"SOL/USDT:USDT": mkt},
		RankInputs:     []ranker.Input{measuredInput("SOL/USDT:USDT", 6, 1)},
		Invalidations:  map[string][]string{"SOL/USDT:USDT": {"x"}},
		StructureValid: map[string]bool{"SOL/USDT:USDT": true},
		EntryPrice:     map[string]float64{"SOL/USDT:USDT": 100},
		StopPrice:      map[string]float64{"SOL/USDT:USDT": 98},
	})
	if err != nil || len(res.Tickets) != 1 {
		t.Fatalf("tickets=%d err=%v failures try playbook", len(res.Tickets), err)
	}
	tid := res.Tickets[0].ID
	if err := s.ApplyHumanDecision(tid, types.DecisionRejected, []string{types.ReasonTooExtended}, "late"); err != nil {
		t.Fatal(err)
	}
	tkt, ok, err := j.GetTicket(tid)
	if err != nil || !ok || tkt.Status != types.TicketStatusRejected {
		t.Fatalf("ticket=%+v ok=%v err=%v", tkt, ok, err)
	}
	dec, ok, err := j.GetDecision(tid)
	if err != nil || !ok || dec.Decision != types.DecisionRejected {
		t.Fatalf("dec=%+v", dec)
	}
}

func TestEvaluate_WinterNoTickets(t *testing.T) {
	s := NewService(testJournal(t), DefaultConfig())
	mkt := cycleUp()
	mkt.PrimaryDirection = types.CycleDirectionNeutral
	mkt.Alignment = types.AlignmentNoTrade
	mkt.TradeClass = types.TradeClassNoTrade
	mkt.Nodes["1d"] = types.CycleNode{
		Timeframe: "1d", Role: types.TimeframeRoleContext,
		Direction: types.CycleDirectionNeutral, Phase: types.WavePhaseWinter,
	}
	mkt.Nodes["4h"] = types.CycleNode{
		Timeframe: "4h", Role: types.TimeframeRoleContext,
		Direction: types.CycleDirectionNeutral, Phase: types.WavePhaseWinter,
	}
	res, err := s.Evaluate(EvaluateRequest{
		EventID:        "ev-win",
		PulseDirection: types.CycleDirectionUp,
		PulseState:     types.MarketStateImpulseUp,
		MarketCycle:    mkt,
		RankInputs:     []ranker.Input{measuredInput("X/USDT:USDT", 5, 1)},
		Invalidations:  map[string][]string{"X/USDT:USDT": {"x"}},
		StructureValid: map[string]bool{"X/USDT:USDT": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tickets) != 0 {
		t.Fatalf("winter must yield 0 tickets, got %d", len(res.Tickets))
	}
}

func TestApplyHumanDecision_RequiresReasonCode(t *testing.T) {
	j := testJournal(t)
	s := NewService(j, DefaultConfig())
	mkt := cycleUp()
	res, err := s.Evaluate(EvaluateRequest{
		EventID:        "ev-reason",
		PulseState:     types.MarketStateImpulseUp,
		PulseDirection: types.CycleDirectionUp,
		MarketCycle:    mkt,
		SymbolCycles:   map[string]types.CycleMap{"SOL/USDT:USDT": mkt},
		RankInputs:     []ranker.Input{measuredInput("SOL/USDT:USDT", 6, 1)},
		Invalidations:  map[string][]string{"SOL/USDT:USDT": {"x"}},
		StructureValid: map[string]bool{"SOL/USDT:USDT": true},
		EntryPrice:     map[string]float64{"SOL/USDT:USDT": 100},
		StopPrice:      map[string]float64{"SOL/USDT:USDT": 98},
	})
	if err != nil || len(res.Tickets) != 1 {
		t.Fatalf("tickets=%d err=%v", len(res.Tickets), err)
	}
	tid := res.Tickets[0].ID

	// No reason code → rejected at the service layer (canonical §8).
	if err := s.ApplyHumanDecision(tid, types.DecisionAccepted, nil, "no code"); err == nil {
		t.Fatal("decision without reason code must fail")
	}
	// Unknown code → rejected.
	if err := s.ApplyHumanDecision(tid, types.DecisionAccepted, []string{"made_up_code"}, ""); err == nil {
		t.Fatal("unknown reason code must fail")
	}
	// Valid code → persisted.
	if err := s.ApplyHumanDecision(tid, types.DecisionAccepted, []string{types.ReasonStructureGood}, ""); err != nil {
		t.Fatal(err)
	}
	dec, ok, err := j.GetDecision(tid)
	if err != nil || !ok || dec.Decision != types.DecisionAccepted {
		t.Fatalf("dec=%+v ok=%v err=%v", dec, ok, err)
	}
}

func TestConfigFromTypes_RiskBudgets(t *testing.T) {
	cfg := ConfigFromTypes(types.OpportunityConfig{
		MaxTicketsPerSession: 2,
		RiskBudgets: map[string]float64{
			"alignedSpring": 1.0,
			"counterTrend":  0.1,
			"no_trade":      0.0,
		},
		MaxLeverage: 3,
	})
	if cfg.MaxTicketsPerSession != 2 {
		t.Fatalf("max tickets: got %d", cfg.MaxTicketsPerSession)
	}
	if cfg.MaxLeverage != 3 {
		t.Fatalf("max leverage: got %v", cfg.MaxLeverage)
	}
	if got := cfg.RiskBudgets[types.RiskTemplateAlignedSpring]; got != 1.0 {
		t.Fatalf("aligned spring budget: got %v", got)
	}
	if got := cfg.RiskBudgets[types.RiskTemplateCounterTrend]; got != 0.1 {
		t.Fatalf("counter trend budget: got %v", got)
	}
	// Templates not in config keep the conservative fallback.
	if got := cfg.RiskBudgets[types.RiskTemplateAlignedSummer]; got != 0.75 {
		t.Fatalf("aligned summer budget: got %v", got)
	}
}
