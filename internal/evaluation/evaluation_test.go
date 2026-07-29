package evaluation

import (
	"path/filepath"
	"testing"

	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestComputeOutcome_LongStopFirst(t *testing.T) {
	o := ComputeOutcome(PathInput{
		TicketID: "t1", Direction: types.CycleDirectionUp,
		Bars: []Bar{
			{TS: 1, Open: 100, High: 100.5, Low: 99, Close: 99.5},
			{TS: 2, Open: 99, High: 99.5, Low: 96, Close: 96.5}, // stop 97
			{TS: 3, Open: 96, High: 105, Low: 96, Close: 104},
		},
		Entry: 100, Stop: 97, Target: 110,
	}, DefaultHorizons())
	if !o.StopHitFirst {
		t.Fatalf("want stop first: %+v", o)
	}
	if o.MechanicalR != -1 {
		t.Fatalf("mech R=%v", o.MechanicalR)
	}
	if o.MFE < 4 { // high 105
		t.Fatalf("mfe=%v", o.MFE)
	}
}

func TestHorizonOffByOne(t *testing.T) {
	// 12 bars after entry → Complete1h and Return1h at Bars[11]
	bars := make([]Bar, 12)
	for i := range bars {
		px := 100 + float64(i)*0.1
		bars[i] = Bar{TS: int64(i + 1), Open: px, High: px, Low: px, Close: px}
	}
	o := ComputeOutcome(PathInput{
		Direction: types.CycleDirectionUp, Bars: bars,
		Entry: 100, Stop: 97, TimeExitBars: 12,
	}, DefaultHorizons())
	if !o.Complete1h || o.Return1h == nil {
		t.Fatalf("1h should complete with 12 bars: complete=%v ret=%v", o.Complete1h, o.Return1h)
	}
	if !o.Finalized {
		t.Fatal("12 bars = 1h time exit should finalize")
	}
	// 11 bars → not complete 1h
	o2 := ComputeOutcome(PathInput{
		Direction: types.CycleDirectionUp, Bars: bars[:11],
		Entry: 100, Stop: 97, TimeExitBars: 12,
	}, DefaultHorizons())
	if o2.Complete1h || o2.Return1h != nil {
		t.Fatalf("11 bars must not claim 1h: %+v", o2)
	}
}

func TestComputeOutcome_NetRUsesCost(t *testing.T) {
	o := ComputeOutcome(PathInput{
		Direction: types.CycleDirectionUp,
		Bars: []Bar{
			{TS: 1, High: 102, Low: 99.5, Close: 101, Open: 100},
			{TS: 2, High: 103, Low: 100, Close: 102, Open: 101},
		},
		Entry: 100, Stop: 97, Target: 0, CostR: 0.1,
	}, DefaultHorizons())
	if o.NetR >= o.MechanicalR {
		t.Fatalf("net should be mech-cost: net=%v mech=%v", o.NetR, o.MechanicalR)
	}
}

func TestSummarize_SelectionAlphaOnNetR(t *testing.T) {
	j, err := storage.NewJournal(types.StorageConfig{DatabasePath: filepath.Join(t.TempDir(), "k.db")})
	if err != nil {
		t.Fatal(err)
	}
	_ = j.SaveTicket(types.DecisionTicket{ID: "a", Status: types.TicketStatusAccepted, TradeClass: types.TradeClassAlignedLong, Grade: types.TicketGradeA, PlaybookID: "leader_pullback_v1", Direction: types.CycleDirectionUp})
	_ = j.SaveTicket(types.DecisionTicket{ID: "b", Status: types.TicketStatusRejected, TradeClass: types.TradeClassAlignedLong, Grade: types.TicketGradeB, PlaybookID: "leader_pullback_v1", Direction: types.CycleDirectionUp})
	_ = j.SaveDecision(types.DecisionRecord{TicketID: "a", Decision: types.DecisionAccepted})
	_ = j.SaveDecision(types.DecisionRecord{TicketID: "b", Decision: types.DecisionRejected})
	_ = j.SaveOutcome(storage.CounterfactualOutcome{TicketID: "a", Complete: true, NetR: 1.5, MechanicalR: 1.6, MaxRealizableR: 2.0, Return5m: fp(1.2)})
	_ = j.SaveOutcome(storage.CounterfactualOutcome{TicketID: "b", Complete: true, NetR: -1.0, MechanicalR: -1.0, MaxRealizableR: 0.5, Return5m: fp(-0.8)})

	rows, err := BuildRows(j)
	if err != nil {
		t.Fatal(err)
	}
	rep := Summarize(rows)
	if rep.SelectionAlpha <= 0 {
		t.Fatalf("alpha=%v acc=%v all=%v", rep.SelectionAlpha, rep.Accepted.MeanNetR, rep.AllQualified.MeanNetR)
	}
	if rep.RejectionMeanR >= 0 {
		t.Fatalf("rej=%v", rep.RejectionMeanR)
	}
	// incomplete should not dilute
	_ = j.SaveTicket(types.DecisionTicket{ID: "c", Status: types.TicketStatusOpen})
	_ = j.SaveOutcome(storage.CounterfactualOutcome{TicketID: "c", Complete: false, NetR: 99})
	rows, _ = BuildRows(j)
	rep2 := Summarize(rows)
	if rep2.AllQualified.NComplete != 2 {
		t.Fatalf("complete n=%d", rep2.AllQualified.NComplete)
	}
}

func fp(v float64) *float64 { return &v }
