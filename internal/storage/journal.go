package storage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// Journal record kinds (one JSONL, last-write-wins per id where applicable).
const (
	JournalKindSession    = "session"
	JournalKindTicket     = "ticket"
	JournalKindDecision   = "decision"
	JournalKindOutcome    = "outcome"
	JournalKindAnnotation = "annotation"
)

// JournalPath is the trading desk research log (sessions/tickets/decisions).
func JournalPath(cfg types.StorageConfig) string {
	return filepath.Join(Dir(cfg), "trading-journal.jsonl")
}

// Journal is an append-only JSONL store for the decision desk.
// ponytail: JSONL not SQLite until query load needs it.
type Journal struct {
	path string
	mu   sync.Mutex
}

// NewJournal opens or creates the trading journal.
func NewJournal(cfg types.StorageConfig) (*Journal, error) {
	dir := Dir(cfg)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("journal mkdir: %w", err)
	}
	return &Journal{path: JournalPath(cfg)}, nil
}

// Path returns the journal file path.
func (j *Journal) Path() string {
	if j == nil {
		return ""
	}
	return j.path
}

type journalLine struct {
	Kind    string          `json:"kind"`
	ID      string          `json:"id,omitempty"`
	At      time.Time       `json:"at"`
	Payload json.RawMessage `json:"payload"`
}

func (j *Journal) append(kind, id string, payload any) error {
	if j == nil {
		return nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	rec := journalLine{Kind: kind, ID: id, At: time.Now().UTC(), Payload: raw}
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	f, err := os.OpenFile(j.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.Write(append(line, '\n'))
	return err
}

// SaveSession persists an opportunity session.
func (j *Journal) SaveSession(s types.OpportunitySession) error {
	return j.append(JournalKindSession, s.ID, s)
}

// SaveTicket persists a decision ticket (re-append updates status).
func (j *Journal) SaveTicket(t types.DecisionTicket) error {
	return j.append(JournalKindTicket, t.ID, t)
}

// SaveDecision persists a human decision.
func (j *Journal) SaveDecision(d types.DecisionRecord) error {
	return j.append(JournalKindDecision, d.TicketID, d)
}

// CounterfactualOutcome tracks post-ticket path whether or not the human traded.
type CounterfactualOutcome struct {
	SchemaVersion string `json:"schema_version"`

	TicketID  string `json:"ticket_id"`
	SessionID string `json:"session_id"`
	Symbol    string `json:"symbol"`

	Direction types.CycleDirection `json:"direction"`
	Decision  types.HumanDecision  `json:"decision,omitempty"`

	Return5m  *float64 `json:"return_5m,omitempty"`
	Return15m *float64 `json:"return_15m,omitempty"`
	Return1h  *float64 `json:"return_1h,omitempty"`
	Return4h  *float64 `json:"return_4h,omitempty"`

	MFE float64 `json:"mfe"`
	MAE float64 `json:"mae"`

	StopHitFirst   bool    `json:"stop_hit_first"`
	TargetHitFirst bool    `json:"target_hit_first"`
	MaxRealizableR float64 `json:"max_realizable_r"`

	AsOfUnix int64 `json:"as_of_unix"`
}

// CounterfactualSchemaVersion is the outcome contract version.
const CounterfactualSchemaVersion = "counterfactual_outcome.v1"

// SaveOutcome persists a counterfactual/actual path outcome.
func (j *Journal) SaveOutcome(o CounterfactualOutcome) error {
	if o.SchemaVersion == "" {
		o.SchemaVersion = CounterfactualSchemaVersion
	}
	return j.append(JournalKindOutcome, o.TicketID, o)
}

func (j *Journal) readAll() ([]journalLine, error) {
	if j == nil {
		return nil, nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	f, err := os.Open(j.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var lines []journalLine
	sc := bufio.NewScanner(f)
	// tickets can be large; raise buffer
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 2*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec journalLine
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		lines = append(lines, rec)
	}
	return lines, sc.Err()
}

// ListSessions returns sessions, last-write-wins by id, newest-first.
func (j *Journal) ListSessions() ([]types.OpportunitySession, error) {
	lines, err := j.readAll()
	if err != nil {
		return nil, err
	}
	latest := map[string]types.OpportunitySession{}
	order := make([]string, 0)
	for _, ln := range lines {
		if ln.Kind != JournalKindSession {
			continue
		}
		var s types.OpportunitySession
		if err := json.Unmarshal(ln.Payload, &s); err != nil || s.ID == "" {
			continue
		}
		if _, ok := latest[s.ID]; !ok {
			order = append(order, s.ID)
		}
		latest[s.ID] = s
	}
	out := make([]types.OpportunitySession, 0, len(order))
	for i := len(order) - 1; i >= 0; i-- {
		out = append(out, latest[order[i]])
	}
	return out, nil
}

// ListTickets returns tickets last-write-wins, newest-first. sessionID filters if non-empty.
func (j *Journal) ListTickets(sessionID string) ([]types.DecisionTicket, error) {
	lines, err := j.readAll()
	if err != nil {
		return nil, err
	}
	latest := map[string]types.DecisionTicket{}
	order := make([]string, 0)
	for _, ln := range lines {
		if ln.Kind != JournalKindTicket {
			continue
		}
		var t types.DecisionTicket
		if err := json.Unmarshal(ln.Payload, &t); err != nil || t.ID == "" {
			continue
		}
		if sessionID != "" && t.SessionID != sessionID {
			continue
		}
		if _, ok := latest[t.ID]; !ok {
			order = append(order, t.ID)
		}
		latest[t.ID] = t
	}
	out := make([]types.DecisionTicket, 0, len(order))
	for i := len(order) - 1; i >= 0; i-- {
		out = append(out, latest[order[i]])
	}
	return out, nil
}

// GetTicket returns the latest ticket by id (last append wins).
func (j *Journal) GetTicket(id string) (types.DecisionTicket, bool, error) {
	lines, err := j.readAll()
	if err != nil {
		return types.DecisionTicket{}, false, err
	}
	var found types.DecisionTicket
	ok := false
	for _, ln := range lines {
		if ln.Kind != JournalKindTicket {
			continue
		}
		var t types.DecisionTicket
		if err := json.Unmarshal(ln.Payload, &t); err != nil {
			continue
		}
		if t.ID != id {
			continue
		}
		found = t
		ok = true
	}
	return found, ok, nil
}

// GetDecision returns the latest human decision for a ticket.
func (j *Journal) GetDecision(ticketID string) (types.DecisionRecord, bool, error) {
	lines, err := j.readAll()
	if err != nil {
		return types.DecisionRecord{}, false, err
	}
	var found types.DecisionRecord
	ok := false
	for _, ln := range lines {
		if ln.Kind != JournalKindDecision || ln.ID != ticketID {
			continue
		}
		var d types.DecisionRecord
		if err := json.Unmarshal(ln.Payload, &d); err != nil {
			continue
		}
		found = d
		ok = true
	}
	return found, ok, nil
}

// ListOutcomes returns latest outcome per ticket id.
func (j *Journal) ListOutcomes() ([]CounterfactualOutcome, error) {
	lines, err := j.readAll()
	if err != nil {
		return nil, err
	}
	latest := map[string]CounterfactualOutcome{}
	order := []string{}
	for _, ln := range lines {
		if ln.Kind != JournalKindOutcome {
			continue
		}
		var o CounterfactualOutcome
		if err := json.Unmarshal(ln.Payload, &o); err != nil || o.TicketID == "" {
			continue
		}
		if _, ok := latest[o.TicketID]; !ok {
			order = append(order, o.TicketID)
		}
		latest[o.TicketID] = o
	}
	out := make([]CounterfactualOutcome, 0, len(order))
	for i := len(order) - 1; i >= 0; i-- {
		out = append(out, latest[order[i]])
	}
	return out, nil
}
