package main

import (
	"path/filepath"
	"testing"

	"github.com/ArchdevilForge/kairos/internal/opportunity"
	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestOpenJournalAndRejectFlow(t *testing.T) {
	dir := t.TempDir()
	j, err := storage.NewJournal(types.StorageConfig{DatabasePath: filepath.Join(dir, "kairos.db")})
	if err != nil {
		t.Fatal(err)
	}
	svc := opportunity.NewService(j, opportunity.DefaultConfig())
	if _, err := svc.HandlePulseEvent(types.AnomalyEvent{
		EventType: "market_impulse", EventID: "desk-e1", Timestamp: 1,
		Data: map[string]any{"direction": "up", "state_to": "IMPULSE_UP"},
	}); err != nil {
		t.Fatal(err)
	}
	// manual ticket for CLI decision path
	tkt := types.DecisionTicket{
		SchemaVersion: types.DecisionTicketSchemaVersion,
		ID:            "t-desk-1", SessionID: "sess-desk-e1", Symbol: "ETH/USDT:USDT",
		Status: types.TicketStatusOpen, Grade: types.TicketGradeB,
	}
	if err := j.SaveTicket(tkt); err != nil {
		t.Fatal(err)
	}
	if err := svc.ApplyHumanDecision("t-desk-1", types.DecisionRejected, []string{types.ReasonTooExtended}, ""); err != nil {
		t.Fatal(err)
	}
	got, ok, err := j.GetTicket("t-desk-1")
	if err != nil || !ok || got.Status != types.TicketStatusRejected {
		t.Fatalf("got=%+v ok=%v err=%v", got, ok, err)
	}

	// openJournal with dir override
	j2, err := openJournal("missing.yaml", dir)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := j2.ListSessions()
	if err != nil || len(sessions) < 1 {
		t.Fatalf("sessions via openJournal: %d err=%v", len(sessions), err)
	}

	// CLI: flags before ticket id (Go FlagSet stops at first positional)
	_ = j.SaveTicket(types.DecisionTicket{ID: "t-desk-2", SessionID: "s", Status: types.TicketStatusOpen, SchemaVersion: types.DecisionTicketSchemaVersion})
	if err := run([]string{"-journal", dir, "reject", "--reason", "too_extended", "t-desk-2"}); err != nil {
		t.Fatalf("cli reject: %v", err)
	}
}
