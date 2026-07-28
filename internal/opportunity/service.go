// Package opportunity turns MarketPulse wake-ups into sessions and tickets.
package opportunity

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ArchdevilForge/kairos/internal/decision"
	"github.com/ArchdevilForge/kairos/internal/playbook"
	"github.com/ArchdevilForge/kairos/internal/ranker"
	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
)

// Config controls desk generation.
type Config struct {
	MaxTicketsPerSession int
	SessionTTLSeconds    int
	Equity               float64 // optional sizing reference
	Enabled              bool
}

// DefaultConfig returns stage-1 defaults.
func DefaultConfig() Config {
	return Config{
		MaxTicketsPerSession: 3,
		SessionTTLSeconds:    3600,
		Enabled:              true,
	}
}

// EvaluateRequest is a full offline/analysis path (tests + enricher).
type EvaluateRequest struct {
	EventID   string
	CreatedAt int64
	SignalAt  int64

	PulseState     types.MarketState
	PulseDirection types.CycleDirection

	MarketCycle  types.CycleMap
	SymbolCycles map[string]types.CycleMap

	RankInputs []ranker.Input

	// Optional per-symbol extras for playbook gates.
	Invalidations  map[string][]string
	StructureValid map[string]bool
	MidBox         map[string]bool

	EntryPrice  map[string]float64
	StopPrice   map[string]float64
	TriggeredAt map[string]int64
}

// EvaluateResult is what one session produced.
type EvaluateResult struct {
	Session types.OpportunitySession
	Ranked  []types.DirectionalCandidate
	Matches []types.PlaybookMatch
	Tickets []types.DecisionTicket
}

// Service orchestrates session → rank → playbook → ticket.
type Service struct {
	cfg      Config
	journal  *storage.Journal
	registry *playbook.Registry
	log      *slog.Logger

	mu sync.Mutex
	// openEventIDs prevents duplicate sessions for the same pulse event.
	openEventIDs map[string]string // eventID → sessionID
}

// NewService constructs a desk orchestrator. journal may be nil (no persist).
func NewService(journal *storage.Journal, cfg Config) *Service {
	if cfg.MaxTicketsPerSession <= 0 {
		cfg.MaxTicketsPerSession = 3
	}
	if cfg.SessionTTLSeconds <= 0 {
		cfg.SessionTTLSeconds = 3600
	}
	return &Service{
		cfg:          cfg,
		journal:      journal,
		registry:     playbook.DefaultRegistry(),
		log:          slog.Default().With("component", "opportunity"),
		openEventIDs: make(map[string]string),
	}
}

// HandlePulseEvent creates at most one session per event id from a raw pulse event.
// Without cycle/rank enrichment it stores an open session and zero tickets (fail closed on trades).
func (s *Service) HandlePulseEvent(evt types.AnomalyEvent) (*types.OpportunitySession, error) {
	if s == nil || !s.cfg.Enabled {
		return nil, nil
	}
	if !isPulseTradeEvent(evt.EventType) {
		return nil, nil
	}
	eventID := evt.EventID
	if eventID == "" {
		eventID = fmt.Sprintf("%s-%.0f", evt.EventType, evt.Timestamp)
	}

	s.mu.Lock()
	if sid, ok := s.openEventIDs[eventID]; ok {
		s.mu.Unlock()
		s.log.Info("session already exists for event", "event_id", eventID, "session_id", sid)
		return nil, nil
	}
	s.mu.Unlock()

	dir := pulseDirectionFromEvent(evt)
	state := pulseStateFromEvent(evt)
	now := time.Now().Unix()
	if evt.Timestamp > 0 {
		now = int64(evt.Timestamp)
	}
	sess := types.OpportunitySession{
		SchemaVersion:  types.OpportunitySessionSchemaVersion,
		ID:             fmt.Sprintf("sess-%s", eventID),
		EventID:        eventID,
		CreatedAt:      now,
		ExpiresAt:      now + int64(s.cfg.SessionTTLSeconds),
		PulseState:     state,
		PulseDirection: dir,
		Status:         types.OpportunitySessionOpen,
	}

	s.mu.Lock()
	s.openEventIDs[eventID] = sess.ID
	s.mu.Unlock()

	if err := s.persistSession(sess); err != nil {
		return &sess, err
	}

	// Soft rank board from pulse returns only (unmeasured liq/spread/pullback).
	if inputs := RankInputsFromPulse(evt); len(inputs) > 0 {
		var ranked []types.DirectionalCandidate
		switch dir {
		case types.CycleDirectionUp:
			ranked = ranker.RankLong(inputs, ranker.SoftConfig())
		case types.CycleDirectionDown:
			ranked = ranker.RankShort(inputs, ranker.SoftConfig())
		default:
			ranked = ranker.Rank(inputs, ranker.SoftConfig())
		}
		if s.journal != nil {
			_ = s.journal.SaveCandidates(storage.SessionCandidates{
				SessionID:  sess.ID,
				EventID:    eventID,
				Candidates: ranked,
			})
		}
		s.log.Info("opportunity session ranked",
			"session_id", sess.ID, "candidates", len(ranked))
	}

	s.log.Info("opportunity session opened",
		"session_id", sess.ID, "event", evt.EventType, "direction", dir,
		"leaders", evt.Data["leaders"], "laggards", evt.Data["laggards"])
	return &sess, nil
}

// Evaluate runs the full Gate path and persists session + tickets.
// Fails if a session for EventID already exists — use EvaluateOrAttach after pulse.
func (s *Service) Evaluate(req EvaluateRequest) (EvaluateResult, error) {
	return s.evaluate(req, false)
}

// EvaluateOrAttach is Evaluate, but if the pulse session already exists it attaches tickets to it.
func (s *Service) EvaluateOrAttach(req EvaluateRequest) (EvaluateResult, error) {
	return s.evaluate(req, true)
}

func (s *Service) evaluate(req EvaluateRequest, attachExisting bool) (EvaluateResult, error) {
	var res EvaluateResult
	if s == nil || !s.cfg.Enabled {
		return res, nil
	}
	now := req.CreatedAt
	if now == 0 {
		now = time.Now().Unix()
	}
	eventID := req.EventID
	if eventID == "" {
		eventID = fmt.Sprintf("manual-%d", now)
	}

	var sess types.OpportunitySession
	s.mu.Lock()
	if sid, ok := s.openEventIDs[eventID]; ok {
		if !attachExisting {
			s.mu.Unlock()
			return res, fmt.Errorf("session already exists for event %s (%s)", eventID, sid)
		}
		sess = types.OpportunitySession{
			SchemaVersion:  types.OpportunitySessionSchemaVersion,
			ID:             sid,
			EventID:        eventID,
			CreatedAt:      now,
			ExpiresAt:      now + int64(s.cfg.SessionTTLSeconds),
			PulseState:     req.PulseState,
			PulseDirection: req.PulseDirection,
			MarketCycle:    req.MarketCycle,
			Status:         types.OpportunitySessionWatching,
		}
	} else {
		sess = types.OpportunitySession{
			SchemaVersion:  types.OpportunitySessionSchemaVersion,
			ID:             fmt.Sprintf("sess-%s", eventID),
			EventID:        eventID,
			CreatedAt:      now,
			ExpiresAt:      now + int64(s.cfg.SessionTTLSeconds),
			PulseState:     req.PulseState,
			PulseDirection: req.PulseDirection,
			MarketCycle:    req.MarketCycle,
			Status:         types.OpportunitySessionOpen,
		}
		s.openEventIDs[eventID] = sess.ID
	}
	s.mu.Unlock()

	res.Session = sess
	if err := s.persistSession(sess); err != nil {
		return res, err
	}

	ranked := ranker.Rank(req.RankInputs, ranker.DefaultConfig())
	res.Ranked = ranked

	var ordered []types.DirectionalCandidate
	switch req.PulseDirection {
	case types.CycleDirectionUp:
		ordered = ranker.RankLong(req.RankInputs, ranker.DefaultConfig())
	case types.CycleDirectionDown:
		ordered = ranker.RankShort(req.RankInputs, ranker.DefaultConfig())
	default:
		ordered = ranked
	}

	pb, _ := s.registry.Get(types.PlaybookLeaderPullbackV1)
	lp, _ := pb.(*playbook.LeaderPullback)
	if lp == nil {
		lp = &playbook.LeaderPullback{}
	}

	tickets := make([]types.DecisionTicket, 0, s.cfg.MaxTicketsPerSession)
	matches := make([]types.PlaybookMatch, 0)

	for i, cand := range ordered {
		if len(tickets) >= s.cfg.MaxTicketsPerSession {
			break
		}
		symCycle := req.SymbolCycles[cand.Symbol]
		if symCycle.SchemaVersion == "" {
			symCycle = req.MarketCycle
		}
		in := playbook.LeaderPullbackInput{
			PlaybookContext: types.PlaybookContext{
				SessionID:      sess.ID,
				Symbol:         cand.Symbol,
				PulseState:     req.PulseState,
				PulseDirection: req.PulseDirection,
				MarketCycle:    req.MarketCycle,
				SymbolCycle:    symCycle,
				Candidate:      cand,
				AsOfUnix:       now,
			},
			Invalidations:  req.Invalidations[cand.Symbol],
			StructureValid: req.StructureValid[cand.Symbol],
			MidBox:         req.MidBox[cand.Symbol],
			LeaderRank:     i + 1,
		}
		if req.StructureValid != nil {
			in.StructureValid = req.StructureValid[cand.Symbol]
		}
		m := lp.MatchInput(in)
		matches = append(matches, m)
		if !m.Matched {
			continue
		}
		signalAt := req.SignalAt
		if signalAt == 0 {
			signalAt = now
		}
		t := decision.BuildTicket(decision.BuildInput{
			TicketID:         fmt.Sprintf("tkt-%s-%d", sess.ID, len(tickets)+1),
			Match:            m,
			Context:          in.PlaybookContext,
			Invalidations:    in.Invalidations,
			EntryPrice:       req.EntryPrice[cand.Symbol],
			StopPrice:        req.StopPrice[cand.Symbol],
			Equity:           s.cfg.Equity,
			Trigger:          "leader_pullback restart",
			CreatedAt:        now,
			SignalAt:         signalAt,
			EntryTriggeredAt: req.TriggeredAt[cand.Symbol],
		})
		if err := s.persistTicket(t); err != nil {
			return res, err
		}
		_ = s.persistOutcome(storage.CounterfactualOutcome{
			SchemaVersion: storage.CounterfactualSchemaVersion,
			TicketID:      t.ID,
			SessionID:     sess.ID,
			Symbol:        t.Symbol,
			Direction:     t.Direction,
			AsOfUnix:      now,
		})
		tickets = append(tickets, t)
	}

	res.Matches = matches
	res.Tickets = tickets
	sess.Status = types.OpportunitySessionWatching
	if len(tickets) == 0 {
		sess.Status = types.OpportunitySessionOpen
	}
	res.Session = sess
	_ = s.persistSession(sess)

	s.log.Info("opportunity evaluated",
		"session_id", sess.ID, "ranked", len(ranked), "tickets", len(tickets))
	return res, nil
}

// ApplyHumanDecision records accept/wait/reject/missed and updates ticket status.
func (s *Service) ApplyHumanDecision(ticketID string, d types.HumanDecision, reasons []string, note string) error {
	if s == nil || s.journal == nil {
		return fmt.Errorf("journal not configured")
	}
	t, ok, err := s.journal.GetTicket(ticketID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("ticket not found: %s", ticketID)
	}
	rec := decision.RecordDecision(ticketID, d, reasons, note, time.Now().Unix())
	if err := s.journal.SaveDecision(rec); err != nil {
		return err
	}
	switch d {
	case types.DecisionAccepted:
		t.Status = types.TicketStatusAccepted
	case types.DecisionWaiting:
		t.Status = types.TicketStatusWaiting
	case types.DecisionRejected:
		t.Status = types.TicketStatusRejected
	case types.DecisionMissed:
		t.Status = types.TicketStatusMissed
	default:
		return fmt.Errorf("unknown decision %q", d)
	}
	return s.journal.SaveTicket(t)
}

func (s *Service) persistSession(sess types.OpportunitySession) error {
	if s.journal == nil {
		return nil
	}
	return s.journal.SaveSession(sess)
}

func (s *Service) persistTicket(t types.DecisionTicket) error {
	if s.journal == nil {
		return nil
	}
	return s.journal.SaveTicket(t)
}

func (s *Service) persistOutcome(o storage.CounterfactualOutcome) error {
	if s.journal == nil {
		return nil
	}
	return s.journal.SaveOutcome(o)
}

func isPulseTradeEvent(eventType string) bool {
	switch eventType {
	case "market_impulse", "market_trend", "market_stress":
		return true
	default:
		return false
	}
}

func pulseDirectionFromEvent(evt types.AnomalyEvent) types.CycleDirection {
	d, _ := evt.Data["direction"].(string)
	d = strings.ToLower(d)
	switch d {
	case "up":
		return types.CycleDirectionUp
	case "down":
		return types.CycleDirectionDown
	default:
		// infer from state_to
		to, _ := evt.Data["state_to"].(string)
		to = strings.ToUpper(to)
		if strings.Contains(to, "UP") {
			return types.CycleDirectionUp
		}
		if strings.Contains(to, "DOWN") {
			return types.CycleDirectionDown
		}
		return types.CycleDirectionNeutral
	}
}

func pulseStateFromEvent(evt types.AnomalyEvent) types.MarketState {
	if to, ok := evt.Data["state_to"].(string); ok && to != "" {
		return types.MarketState(to)
	}
	switch evt.EventType {
	case "market_impulse":
		if pulseDirectionFromEvent(evt) == types.CycleDirectionDown {
			return types.MarketStateImpulseDown
		}
		return types.MarketStateImpulseUp
	case "market_trend":
		if pulseDirectionFromEvent(evt) == types.CycleDirectionDown {
			return types.MarketStateTrendingDown
		}
		return types.MarketStateTrendingUp
	case "market_stress":
		if pulseDirectionFromEvent(evt) == types.CycleDirectionDown {
			return types.MarketStateStressDown
		}
		return types.MarketStateStressUp
	default:
		return types.MarketStateQuiet
	}
}
