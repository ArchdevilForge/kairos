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
}
