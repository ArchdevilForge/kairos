package evaluation

import (
	"testing"

	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
)

// mkRow builds a completed accepted ticket with given NetR/MechanicalR.
func mkAlphaRow(netR, mechR float64) TicketRow {
	return TicketRow{
		Ticket:   types.DecisionTicket{ID: "t", Status: types.TicketStatusAccepted},
		Decision: types.DecisionAccepted,
		Outcome: storage.CounterfactualOutcome{
			NetR: netR, MechanicalR: mechR, Complete: true, Finalized: true,
		},
		HasOut: true,
	}
}

func TestHumanAlpha(t *testing.T) {
	// accepted: net +0.4, mech +0.2 → human alpha +0.2
	// rejected row must not pollute accepted group
	rows := []TicketRow{
		mkAlphaRow(0.4, 0.2),
		mkAlphaRow(0.2, 0.3), // human underperformed here: -0.1
		{Ticket: types.DecisionTicket{ID: "r", Status: types.TicketStatusRejected},
			Decision: types.DecisionRejected,
			Outcome:  storage.CounterfactualOutcome{NetR: 1.0, MechanicalR: 0.0, Complete: true, Finalized: true},
			HasOut:   true},
	}
	rep := Summarize(rows)
	want := (0.4+0.2)/2 - (0.2+0.3)/2 // 0.3 - 0.25 = 0.05
	if rep.HumanAlpha != want {
		t.Fatalf("HumanAlpha = %v, want %v", rep.HumanAlpha, want)
	}
	if rep.SelectionAlpha != 0.3-0.5333 { // sanity: selection computed on all
		// mean all = (0.4+0.2+1.0)/3 = 0.5333; accepted = 0.3
		got := rep.SelectionAlpha
		wantSel := 0.3 - 0.5333333333
		if diff := got - wantSel; diff > 0.0001 || diff < -0.0001 {
			t.Fatalf("SelectionAlpha = %v, want ~%v", got, wantSel)
		}
	}
}
