package storage

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
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
	JournalKindCandidates = "candidates"
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
	meta ResearchMeta
}

// SetMetadata attaches reproducibility metadata to all subsequently appended
// lines. Call once at startup (engine wires git_sha/config_hash/strategy).
func (j *Journal) SetMetadata(m ResearchMeta) {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.meta = m
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

	// Research metadata (P1): 保证半年后能复现"哪个版本的策略产生了这行记录"。
	GitSHA          string `json:"git_sha,omitempty"`
	ConfigHash      string `json:"config_hash,omitempty"`
	StrategyVersion string `json:"strategy_version,omitempty"`
	ExperimentID    string `json:"experiment_id,omitempty"`
	Mode            string `json:"mode,omitempty"` // shadow | paper | live
}

// ResearchMeta is the reproducibility metadata attached to every journal line.
type ResearchMeta struct {
	GitSHA          string
	ConfigHash      string
	StrategyVersion string
	ExperimentID    string
	Mode            string
}

func (j *Journal) append(kind, id string, payload any) error {
	if j == nil {
		return nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	rec := journalLine{
		Kind: kind, ID: id, At: time.Now().UTC(), Payload: raw,
		GitSHA: j.meta.GitSHA, ConfigHash: j.meta.ConfigHash,
		StrategyVersion: j.meta.StrategyVersion,
		ExperimentID:    j.meta.ExperimentID, Mode: j.meta.Mode,
	}
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

	TicketID      string `json:"ticket_id"`
	SessionID     string `json:"session_id"`
	WaveClusterID string `json:"wave_cluster_id,omitempty"`
	Symbol        string `json:"symbol"`

	Direction types.CycleDirection `json:"direction"`
	Decision  types.HumanDecision  `json:"decision,omitempty"`

	Return5m  *float64 `json:"return_5m,omitempty"`
	Return15m *float64 `json:"return_15m,omitempty"`
	Return1h  *float64 `json:"return_1h,omitempty"`
	Return4h  *float64 `json:"return_4h,omitempty"`

	// Path diagnostics (high/low based) — not tradable EV.
	MFE float64 `json:"mfe"`
	MAE float64 `json:"mae"`

	StopHitFirst   bool `json:"stop_hit_first"`
	TargetHitFirst bool `json:"target_hit_first"`

	// MaxRealizableR = best MFE in R units (path diagnosis only).
	MaxRealizableR float64 `json:"max_realizable_r"`
	// MechanicalR = result under stop/target/fixed-time-exit rules.
	MechanicalR float64 `json:"mechanical_r"`
	// NetR = canonical full-friction NetR (both-side fees + dynamic slippage + accumulated funding / pre-entry 1R).
	NetR float64 `json:"net_r"`

	// Additive Phase 1 close-out, friction, and counterfactual fields (Work Items 11, 12, 13).
	ExitReason          types.ExitReason `json:"exit_reason,omitempty"`
	RealizedFundingUSD  float64          `json:"realized_funding_usd,omitempty"`
	RealizedFeeUSD      float64          `json:"realized_fee_usd,omitempty"`
	RealizedSlippageUSD float64          `json:"realized_slippage_usd,omitempty"`
	Initial1RPct        float64          `json:"initial_1r_pct,omitempty"`
	ExitPrice           float64          `json:"exit_price,omitempty"`
	HumanEntryPrice     float64          `json:"human_entry_price,omitempty"`
	MechHumanEntryR     *float64         `json:"mech_human_entry_r,omitempty"`
	PolicyWaveR         *float64         `json:"policy_wave_r,omitempty"`
	Policy5mR           *float64         `json:"policy_5m_r,omitempty"`
	Policy30mR          *float64         `json:"policy_30m_r,omitempty"`
	PolicyBracketR      *float64         `json:"policy_bracket_r,omitempty"`
	TouchedPresetStop   bool             `json:"touched_preset_stop,omitempty"`
	FakeoutConfirmed    bool             `json:"fakeout_confirmed,omitempty"`
	MarketStressActive  bool             `json:"market_stress_active,omitempty"`
	StructureIntact     bool             `json:"structure_intact,omitempty"`

	Complete5m  bool `json:"complete_5m"`
	Complete15m bool `json:"complete_15m"`
	Complete1h  bool `json:"complete_1h"`
	Complete4h  bool `json:"complete_4h"`
	// Finalized: stop/target hit or fixed 1h time-exit reached — use for Selection Alpha.
	Finalized bool `json:"finalized"`
	// Complete is an alias of Finalized for older readers.
	Complete bool `json:"complete"`

	PathStartUnix int64 `json:"path_start_unix,omitempty"`
	PathEndUnix   int64 `json:"path_end_unix,omitempty"`
	AsOfUnix      int64 `json:"as_of_unix"`
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

// GateAnnotation records a behavior-gate verdict on a ticket (doctrine §9b):
// rejected tickets stay as shadow/counterfactual records; overrides must leave
// a trace with the violated codes.
type GateAnnotation struct {
	SchemaVersion string `json:"schema_version"`

	TicketID string   `json:"ticket_id"`
	Codes    []string `json:"codes"` // GATE_* reason codes
	Override bool     `json:"override"`
	Note     string   `json:"note,omitempty"`
	At       int64    `json:"at"`
}

// GateAnnotationSchemaVersion is the annotation contract version.
const GateAnnotationSchemaVersion = "gate_annotation.v1"

// SaveAnnotation persists a gate annotation.
func (j *Journal) SaveAnnotation(a GateAnnotation) error {
	if a.SchemaVersion == "" {
		a.SchemaVersion = GateAnnotationSchemaVersion
	}
	return j.append(JournalKindAnnotation, a.TicketID, a)
}

// ListAnnotations returns all gate annotations in append order.
func (j *Journal) ListAnnotations() ([]GateAnnotation, error) {
	lines, err := j.readAll()
	if err != nil {
		return nil, err
	}
	var out []GateAnnotation
	for _, ln := range lines {
		if ln.Kind != JournalKindAnnotation {
			continue
		}
		var a GateAnnotation
		if err := json.Unmarshal(ln.Payload, &a); err != nil || a.TicketID == "" {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

// CooldownStats summarizes same-symbol recent loss/stop history for dual-track cooldown (Work Item 9).
type CooldownStats struct {
	LastLossAt              time.Time
	ConsecutiveStops24h     int
	CumulativeTrialLoss24hR float64
}

// SameSymbolCooldownStats computes the dual-track cooldown inputs for symbol as of now (Work Item 9).
func (j *Journal) SameSymbolCooldownStats(symbol string, now time.Time) (CooldownStats, error) {
	outs, err := j.ListOutcomes()
	if err != nil {
		return CooldownStats{}, err
	}
	type timedOutcome struct {
		end int64
		out CounterfactualOutcome
	}
	var accepted []timedOutcome
	for _, o := range outs {
		if o.Symbol != symbol || o.Decision != types.DecisionAccepted || !o.Finalized {
			continue
		}
		end := o.PathEndUnix
		if end == 0 {
			end = o.AsOfUnix
		}
		accepted = append(accepted, timedOutcome{end: end, out: o})
	}
	sort.Slice(accepted, func(i, k int) bool {
		return accepted[i].end < accepted[k].end
	})

	var stats CooldownStats
	var latestLoss int64
	cutoff24h := now.Add(-24 * time.Hour).Unix()

	for _, item := range accepted {
		if item.out.NetR < 0 && item.end > latestLoss {
			latestLoss = item.end
		}
		if !now.IsZero() && item.end >= cutoff24h && item.end <= now.Unix() {
			if item.out.NetR < 0 {
				stats.CumulativeTrialLoss24hR += item.out.NetR
			}
		}
	}
	// Count consecutive stops from the most recent accepted outcome within 24h backwards.
	for idx := len(accepted) - 1; idx >= 0; idx-- {
		item := accepted[idx]
		if !now.IsZero() && (item.end < cutoff24h || item.end > now.Unix()) {
			continue
		}
		isStopLoss := item.out.NetR < 0 ||
			item.out.StopHitFirst ||
			item.out.ExitReason == types.ExitReasonProtectiveStop ||
			item.out.ExitReason == types.ExitReasonFakeoutAbort
		if isStopLoss {
			stats.ConsecutiveStops24h++
		} else {
			break
		}
	}
	if latestLoss > 0 {
		stats.LastLossAt = time.Unix(latestLoss, 0)
	}
	return stats, nil
}

// LastLossAt returns the end time of the most recent finalized losing outcome
// for symbol (accepted trades only — gate cooldown input). Zero time = none.
func (j *Journal) LastLossAt(symbol string) (time.Time, error) {
	st, err := j.SameSymbolCooldownStats(symbol, time.Time{})
	if err != nil {
		return time.Time{}, err
	}
	return st.LastLossAt, nil
}

// SessionCandidates is the ranked board attached to a session (may have 0 tickets).
type SessionCandidates struct {
	SessionID  string                       `json:"session_id"`
	EventID    string                       `json:"event_id"`
	Candidates []types.DirectionalCandidate `json:"candidates"`
}

// SaveCandidates stores the dual-sided rank board for a session.
func (j *Journal) SaveCandidates(c SessionCandidates) error {
	return j.append(JournalKindCandidates, c.SessionID, c)
}

// GetCandidates returns the latest candidate board for a session.
func (j *Journal) GetCandidates(sessionID string) (SessionCandidates, bool, error) {
	lines, err := j.readAll()
	if err != nil {
		return SessionCandidates{}, false, err
	}
	var found SessionCandidates
	ok := false
	for _, ln := range lines {
		if ln.Kind != JournalKindCandidates {
			continue
		}
		var c SessionCandidates
		if err := json.Unmarshal(ln.Payload, &c); err != nil {
			continue
		}
		if c.SessionID != sessionID && ln.ID != sessionID {
			continue
		}
		found = c
		ok = true
	}
	return found, ok, nil
}

// GetOutcome returns the latest counterfactual row for a ticket.
func (j *Journal) GetOutcome(ticketID string) (CounterfactualOutcome, bool, error) {
	lines, err := j.readAll()
	if err != nil {
		return CounterfactualOutcome{}, false, err
	}
	var found CounterfactualOutcome
	ok := false
	for _, ln := range lines {
		if ln.Kind != JournalKindOutcome {
			continue
		}
		var o CounterfactualOutcome
		if err := json.Unmarshal(ln.Payload, &o); err != nil {
			continue
		}
		if o.TicketID != ticketID {
			continue
		}
		found = o
		ok = true
	}
	return found, ok, nil
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

// DetectGitSHA returns the current commit revision: build-info vcs.revision
// (release builds) or .git/HEAD (dev checkout). Empty when undetectable.
func DetectGitSHA() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" && s.Value != "" {
				return s.Value
			}
		}
	}
	// Dev fallback: read .git/HEAD one level up from the working dir.
	raw, err := os.ReadFile(filepath.Join("..", ".git", "HEAD"))
	if err != nil {
		return ""
	}
	ref := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(ref, "ref: ") {
		return ref[:min(len(ref), 12)]
	}
	b, err := os.ReadFile(filepath.Join("..", ".git", strings.TrimPrefix(ref, "ref: ")))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))[:12]
}

// ConfigHashOf returns a short sha256 fingerprint of the marshaled config —
// enough to distinguish "which config ran this session".
func ConfigHashOf(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%x", sum[:6])
}
