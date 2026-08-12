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

// TestAcceptGateFlow: §9b gate 拦截 accept(cross margin+无 stop 等多重违规),
// --override 放行但必须留 annotation。
func TestAcceptGateFlow(t *testing.T) {
	dir := t.TempDir()
	j, err := storage.NewJournal(types.StorageConfig{DatabasePath: filepath.Join(dir, "kairos.db")})
	if err != nil {
		t.Fatal(err)
	}
	tkt := types.DecisionTicket{
		SchemaVersion: types.DecisionTicketSchemaVersion,
		ID:            "t-gate-1", SessionID: "s", Symbol: "ZEC/USDT:USDT",
		Status: types.TicketStatusOpen, Grade: types.TicketGradeB,
		Direction: types.CycleDirectionUp,
	}
	if err := j.SaveTicket(tkt); err != nil {
		t.Fatal(err)
	}

	// 无 --margin/--max-loss/stop → gate 必拒(missing.yaml → doctrine 默认 gate 常开)
	err = run([]string{"-journal", dir, "accept", "--reason", "structure_good", "t-gate-1"})
	if err == nil {
		t.Fatal("gate must reject non-compliant accept")
	}

	// 被拒后 annotation 已留痕
	anns, err := j.ListAnnotations()
	if err != nil || len(anns) != 1 || anns[0].Override {
		t.Fatalf("want 1 non-override annotation, got %+v err=%v", anns, err)
	}

	// --override 放行,再留一条 override annotation
	if err := run([]string{"-journal", dir, "accept", "--reason", "manual_override",
		"--note", "test override", "--override", "t-gate-1"}); err != nil {
		t.Fatalf("override accept: %v", err)
	}
	got, ok, _ := j.GetTicket("t-gate-1")
	if !ok || got.Status != types.TicketStatusAccepted {
		t.Fatalf("ticket not accepted after override: %+v", got)
	}
	anns, _ = j.ListAnnotations()
	if len(anns) != 2 || !anns[1].Override {
		t.Fatalf("want override annotation, got %+v", anns)
	}

	// gate 子命令: 干跑不改状态
	if err := run([]string{"-journal", dir, "gate", "--margin", "isolated", "--max-loss", "3", "t-gate-1"}); err != nil {
		t.Fatalf("gate subcommand: %v", err)
	}
}
