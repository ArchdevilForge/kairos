package main

import (
	"path/filepath"
	"testing"

	"github.com/ArchdevilForge/kairos/internal/evaluation"
	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestEvalSummaryFromJournal(t *testing.T) {
	dir := t.TempDir()
	j, err := storage.NewJournal(types.StorageConfig{DatabasePath: filepath.Join(dir, "kairos.db")})
	if err != nil {
		t.Fatal(err)
	}
	_ = j.SaveTicket(types.DecisionTicket{
		ID: "t1", Status: types.TicketStatusAccepted, Direction: types.CycleDirectionUp,
		TradeClass: types.TradeClassAlignedLong, Grade: types.TicketGradeA, PlaybookID: types.PlaybookLeaderPullbackV1,
	})
	_ = j.SaveDecision(types.DecisionRecord{TicketID: "t1", Decision: types.DecisionAccepted})
	r5 := 1.2
	_ = j.SaveOutcome(storage.CounterfactualOutcome{TicketID: "t1", MaxRealizableR: 1.5, Return5m: &r5})

	j2, err := openJournal("nope.yaml", dir)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := evaluation.BuildRows(j2)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
	rep := evaluation.Summarize(rows)
	if rep.Accepted.N != 1 {
		t.Fatalf("%+v", rep)
	}
	if rep.MaxAbsIdentityResidual != 0 {
		t.Fatalf("MaxAbsIdentityResidual=%v want 0", rep.MaxAbsIdentityResidual)
	}
}

func TestEvalGateReauditZeroMisclassificationOnProtectiveStop(t *testing.T) {
	dir := t.TempDir()
	j, err := storage.NewJournal(types.StorageConfig{DatabasePath: filepath.Join(dir, "kairos.db")})
	if err != nil {
		t.Fatal(err)
	}
	// Historical <5m exit that touched its preset stop must have 0% violation misclassification.
	_ = j.SaveTicket(types.DecisionTicket{
		ID: "t-stop-fast", Symbol: "SOL/USDT:USDT", Status: types.TicketStatusAccepted,
		Direction: types.CycleDirectionUp, TradeClass: types.TradeClassAlignedLong,
		Grade: types.TicketGradeA, PlaybookID: types.PlaybookLeaderPullbackV1,
		EntryTriggeredAt: 1_700_000_000,
		RiskPlan:         types.RiskPlan{EntryPrice: 100.0, StopPrice: 98.0},
	})
	_ = j.SaveDecision(types.DecisionRecord{TicketID: "t-stop-fast", Decision: types.DecisionAccepted})
	_ = j.SaveOutcome(storage.CounterfactualOutcome{
		TicketID:     "t-stop-fast",
		Symbol:       "SOL/USDT:USDT",
		Decision:     types.DecisionAccepted,
		ExitPrice:    97.95,
		PathEndUnix:  1_700_000_120, // 2m (<5m) hold
		StopHitFirst: true,
		NetR:         -1.08,
		MechanicalR:  -1.08,
		Complete:     true,
	})

	if err := run([]string{"-journal", dir, "-json", "gate"}); err != nil {
		t.Fatalf("gate run failed: %v", err)
	}
	if err := run([]string{"-journal", dir, "summary"}); err != nil {
		t.Fatalf("summary run failed: %v", err)
	}
}
