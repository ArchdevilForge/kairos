package evaluation

import (
	"path/filepath"
	"testing"

	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestComputeOutcome_LongStopFirst(t *testing.T) {
	// entry 100, stop 97, path dips to 96 then rallies — stop first
	o := ComputeOutcome(PathInput{
		TicketID: "t1", Direction: types.CycleDirectionUp,
		Closes: []float64{100, 98, 96, 99, 105},
		Entry:  100, Stop: 97, Target: 110,
	}, DefaultHorizons())
	if !o.StopHitFirst {
		t.Fatalf("want stop first: %+v", o)
	}
	if o.MFE < 4 {
		t.Fatalf("mfe=%v", o.MFE)
	}
	if o.Return5m == nil {
		t.Fatal("missing 5m")
	}
}

func TestComputeOutcome_ShortMirror(t *testing.T) {
	o := ComputeOutcome(PathInput{
		Direction: types.CycleDirectionDown,
		Closes:    []float64{100, 99, 95, 94},
		Entry:     100, Stop: 103, Target: 90,
	}, DefaultHorizons())
	if o.MFE <= 0 {
		t.Fatalf("short mfe should be positive pct in favor: %+v", o)
	}
	if o.MaxRealizableR <= 0 {
		t.Fatalf("max R=%v", o.MaxRealizableR)
	}
}

func TestSummarize_SelectionAlpha(t *testing.T) {
	j, err := storage.NewJournal(types.StorageConfig{DatabasePath: filepath.Join(t.TempDir(), "k.db")})
	if err != nil {
		t.Fatal(err)
	}
	// two tickets: accept good R, reject bad R
	_ = j.SaveTicket(types.DecisionTicket{ID: "a", Status: types.TicketStatusAccepted, TradeClass: types.TradeClassAlignedLong, Grade: types.TicketGradeA, PlaybookID: "leader_pullback_v1", Direction: types.CycleDirectionUp})
	_ = j.SaveTicket(types.DecisionTicket{ID: "b", Status: types.TicketStatusRejected, TradeClass: types.TradeClassAlignedLong, Grade: types.TicketGradeB, PlaybookID: "leader_pullback_v1", Direction: types.CycleDirectionUp})
	_ = j.SaveDecision(types.DecisionRecord{TicketID: "a", Decision: types.DecisionAccepted})
	_ = j.SaveDecision(types.DecisionRecord{TicketID: "b", Decision: types.DecisionRejected})
	_ = j.SaveOutcome(storage.CounterfactualOutcome{TicketID: "a", MaxRealizableR: 2.0, Return5m: fp(1.5)})
	_ = j.SaveOutcome(storage.CounterfactualOutcome{TicketID: "b", MaxRealizableR: -1.0, Return5m: fp(-0.8)})

	rows, err := BuildRows(j)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
	rep := Summarize(rows)
	if rep.Accepted.N != 1 || rep.Rejected.N != 1 {
		t.Fatalf("rep=%+v", rep)
	}
	if rep.SelectionAlpha <= 0 {
		t.Fatalf("selection alpha should be >0, got %v (acc=%v all=%v)", rep.SelectionAlpha, rep.Accepted.MeanR, rep.AllQualified.MeanR)
	}
	if rep.RejectionMeanR >= 0 {
		t.Fatalf("rejected should be poor, meanR=%v", rep.RejectionMeanR)
	}
}

func fp(v float64) *float64 { return &v }
