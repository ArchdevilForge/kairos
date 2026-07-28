package evaluation

import (
	"math"
	"sort"

	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
)

// GroupStats is mean NetR / n for a decision bucket (complete outcomes only).
type GroupStats struct {
	N         int     `json:"n"`
	NComplete int     `json:"n_complete"`
	MeanNetR  float64 `json:"mean_net_r"`
	MeanMechR float64 `json:"mean_mechanical_r"`
	MeanMaxR  float64 `json:"mean_max_r"` // diagnosis only
	Mean5m    float64 `json:"mean_5m,omitempty"`
	WinRate   float64 `json:"win_rate"` // NetR > 0 among complete
}

// SummaryReport is the stage-1 EV desk view.
type SummaryReport struct {
	TicketsTotal int `json:"tickets_total"`
	WithOutcome  int `json:"with_outcome"`
	Complete     int `json:"complete_outcomes"`

	Accepted     GroupStats `json:"accepted"`
	Rejected     GroupStats `json:"rejected"`
	Waiting      GroupStats `json:"waiting"`
	Missed       GroupStats `json:"missed"`
	AllQualified GroupStats `json:"all_qualified"`

	// SelectionAlpha = accepted.MeanNetR - all_qualified.MeanNetR (complete only)
	SelectionAlpha float64 `json:"selection_alpha"`
	RejectionMeanR float64 `json:"rejection_mean_net_r"`

	ByTradeClass map[string]GroupStats `json:"by_trade_class,omitempty"`
	ByGrade      map[string]GroupStats `json:"by_grade,omitempty"`
	ByPlaybook   map[string]GroupStats `json:"by_playbook,omitempty"`
	ByDirection  map[string]GroupStats `json:"by_direction,omitempty"`
}

// TicketRow joins ticket + decision + outcome for attribution.
type TicketRow struct {
	Ticket   types.DecisionTicket
	Decision types.HumanDecision
	Outcome  storage.CounterfactualOutcome
	HasOut   bool
}

// BuildRows loads journal state into join rows.
func BuildRows(j *storage.Journal) ([]TicketRow, error) {
	if j == nil {
		return nil, nil
	}
	tickets, err := j.ListTickets("")
	if err != nil {
		return nil, err
	}
	out := make([]TicketRow, 0, len(tickets))
	for _, t := range tickets {
		row := TicketRow{Ticket: t}
		if d, ok, _ := j.GetDecision(t.ID); ok {
			row.Decision = d.Decision
		} else {
			switch t.Status {
			case types.TicketStatusAccepted:
				row.Decision = types.DecisionAccepted
			case types.TicketStatusRejected:
				row.Decision = types.DecisionRejected
			case types.TicketStatusWaiting:
				row.Decision = types.DecisionWaiting
			case types.TicketStatusMissed:
				row.Decision = types.DecisionMissed
			}
		}
		if o, ok, _ := j.GetOutcome(t.ID); ok {
			row.Outcome = o
			row.HasOut = true
		}
		out = append(out, row)
	}
	return out, nil
}

// Summarize computes selection/rejection stats on complete outcomes only.
func Summarize(rows []TicketRow) SummaryReport {
	rep := SummaryReport{
		TicketsTotal: len(rows),
		ByTradeClass: map[string]GroupStats{},
		ByGrade:      map[string]GroupStats{},
		ByPlaybook:   map[string]GroupStats{},
		ByDirection:  map[string]GroupStats{},
	}
	var all, acc, rej, wait, miss []TicketRow
	for _, r := range rows {
		if r.HasOut {
			rep.WithOutcome++
			if r.Outcome.Complete {
				rep.Complete++
			}
		}
		all = append(all, r)
		switch r.Decision {
		case types.DecisionAccepted:
			acc = append(acc, r)
		case types.DecisionRejected:
			rej = append(rej, r)
		case types.DecisionWaiting:
			wait = append(wait, r)
		case types.DecisionMissed:
			miss = append(miss, r)
		}
		rep.ByTradeClass[string(r.Ticket.TradeClass)] = fold(rep.ByTradeClass[string(r.Ticket.TradeClass)], r)
		rep.ByGrade[string(r.Ticket.Grade)] = fold(rep.ByGrade[string(r.Ticket.Grade)], r)
		rep.ByPlaybook[r.Ticket.PlaybookID] = fold(rep.ByPlaybook[r.Ticket.PlaybookID], r)
		rep.ByDirection[string(r.Ticket.Direction)] = fold(rep.ByDirection[string(r.Ticket.Direction)], r)
	}
	for k, g := range rep.ByTradeClass {
		rep.ByTradeClass[k] = finalize(g)
	}
	for k, g := range rep.ByGrade {
		rep.ByGrade[k] = finalize(g)
	}
	for k, g := range rep.ByPlaybook {
		rep.ByPlaybook[k] = finalize(g)
	}
	for k, g := range rep.ByDirection {
		rep.ByDirection[k] = finalize(g)
	}

	rep.AllQualified = statsOf(all)
	rep.Accepted = statsOf(acc)
	rep.Rejected = statsOf(rej)
	rep.Waiting = statsOf(wait)
	rep.Missed = statsOf(miss)
	rep.SelectionAlpha = round4(rep.Accepted.MeanNetR - rep.AllQualified.MeanNetR)
	rep.RejectionMeanR = rep.Rejected.MeanNetR
	return rep
}

func statsOf(rows []TicketRow) GroupStats {
	g := GroupStats{}
	for _, r := range rows {
		g = fold(g, r)
	}
	return finalize(g)
}

func fold(g GroupStats, r TicketRow) GroupStats {
	g.N++
	if !r.HasOut || !r.Outcome.Complete {
		return g
	}
	g.NComplete++
	g.MeanNetR += r.Outcome.NetR
	g.MeanMechR += r.Outcome.MechanicalR
	g.MeanMaxR += r.Outcome.MaxRealizableR
	if r.Outcome.Return5m != nil {
		g.Mean5m += *r.Outcome.Return5m
	}
	if r.Outcome.NetR > 0 {
		g.WinRate++
	}
	return g
}

func finalize(g GroupStats) GroupStats {
	if g.NComplete == 0 {
		g.MeanNetR, g.MeanMechR, g.MeanMaxR, g.Mean5m, g.WinRate = 0, 0, 0, 0, 0
		return g
	}
	n := float64(g.NComplete)
	wins := g.WinRate
	g.MeanNetR = round4(g.MeanNetR / n)
	g.MeanMechR = round4(g.MeanMechR / n)
	g.MeanMaxR = round4(g.MeanMaxR / n)
	g.Mean5m = round4(g.Mean5m / n)
	g.WinRate = round4(wins / n)
	return g
}

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}

// SortedKeys helper for stable CLI output.
func SortedKeys(m map[string]GroupStats) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		if k == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
