package storage

import (
	"path/filepath"
	"testing"

	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestJournal_SessionTicketDecisionRoundTrip(t *testing.T) {
	j, err := NewJournal(types.StorageConfig{DatabasePath: filepath.Join(t.TempDir(), "k.db")})
	if err != nil {
		t.Fatal(err)
	}
	sess := types.OpportunitySession{
		SchemaVersion: types.OpportunitySessionSchemaVersion,
		ID:            "sess-1", EventID: "e1", Status: types.OpportunitySessionOpen,
		PulseDirection: types.CycleDirectionUp,
	}
	if err := j.SaveSession(sess); err != nil {
		t.Fatal(err)
	}
	tkt := types.DecisionTicket{
		SchemaVersion: types.DecisionTicketSchemaVersion,
		ID:            "t1", SessionID: "sess-1", Symbol: "BTC/USDT:USDT",
		Status: types.TicketStatusOpen, Grade: types.TicketGradeA,
	}
	if err := j.SaveTicket(tkt); err != nil {
		t.Fatal(err)
	}
	tkt.Status = types.TicketStatusAccepted
	if err := j.SaveTicket(tkt); err != nil {
		t.Fatal(err)
	}
	got, ok, err := j.GetTicket("t1")
	if err != nil || !ok || got.Status != types.TicketStatusAccepted {
		t.Fatalf("got=%+v ok=%v err=%v", got, ok, err)
	}
	if err := j.SaveDecision(types.DecisionRecord{
		SchemaVersion: types.DecisionRecordSchemaVersion,
		TicketID:      "t1", Decision: types.DecisionAccepted,
		ReasonCodes: []string{types.ReasonStructureGood}, DecidedAt: 1,
	}); err != nil {
		t.Fatal(err)
	}
	dec, ok, err := j.GetDecision("t1")
	if err != nil || !ok || dec.Decision != types.DecisionAccepted {
		t.Fatalf("dec=%+v", dec)
	}
	if err := j.SaveOutcome(CounterfactualOutcome{TicketID: "t1", SessionID: "sess-1", MFE: 1.2}); err != nil {
		t.Fatal(err)
	}
	outs, err := j.ListOutcomes()
	if err != nil || len(outs) != 1 || outs[0].MFE != 1.2 {
		t.Fatalf("outs=%v err=%v", outs, err)
	}
}

func TestJournalMetadataAttached(t *testing.T) {
	dir := t.TempDir()
	j, err := NewJournal(types.StorageConfig{DatabasePath: filepath.Join(dir, "k.db")})
	if err != nil {
		t.Fatal(err)
	}
	j.SetMetadata(ResearchMeta{
		GitSHA: "abc123", ConfigHash: "feedbeef",
		StrategyVersion: "v1", ExperimentID: "exp-1", Mode: "shadow",
	})
	if err := j.SaveTicket(types.DecisionTicket{ID: "t1"}); err != nil {
		t.Fatal(err)
	}
	rows, err := j.readAll()
	if err != nil || len(rows) == 0 {
		t.Fatalf("readAll: %v rows=%d", err, len(rows))
	}
	line := rows[0]
	if line.GitSHA != "abc123" || line.ConfigHash != "feedbeef" ||
		line.StrategyVersion != "v1" || line.ExperimentID != "exp-1" || line.Mode != "shadow" {
		t.Fatalf("metadata missing: %+v", line)
	}
}

func TestConfigHashOfStable(t *testing.T) {
	h1 := ConfigHashOf(map[string]any{"a": 1, "b": "x"})
	h2 := ConfigHashOf(map[string]any{"a": 1, "b": "x"})
	h3 := ConfigHashOf(map[string]any{"a": 1, "b": "y"})
	if h1 == "" || h1 != h2 || h1 == h3 {
		t.Fatalf("hash unstable: %q %q %q", h1, h2, h3)
	}
}
