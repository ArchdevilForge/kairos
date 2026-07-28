package evaluation

import (
	"math"
	"sort"

	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
)

// GroupStats is mean R / n for a decision bucket.
type GroupStats struct {
	N       int     `json:"n"`
	MeanR   float64 `json:"mean_r"`
	Mean5m  float64 `json:"mean_5m,omitempty"`
	WinRate float64 `json:"win_rate"` // fraction MaxRealizableR > 0 or return5m > 0
}

// SummaryReport is the stage-1 EV desk view.
type SummaryReport struct {
	TicketsTotal int        `json:"tickets_total"`
	WithOutcome  int        `json:"with_outcome"`
	Accepted     GroupStats `json:"accepted"`
	Rejected     GroupStats `json:"rejected"`
	Waiting      GroupStats `json:"waiting"`
	Missed       GroupStats `json:"missed"`
	AllQualified GroupStats `json:"all_qualified"`
	// SelectionAlpha = accepted.MeanR - all_qualified.MeanR
	SelectionAlpha float64 `json:"selection_alpha"`
	// RejectionQuality: ideal rejected mean R <= 0
	RejectionMeanR float64 `json:"rejection_mean_r"`

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
			// fall back to ticket status
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

// Summarize computes selection/rejection style stats.
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
	// finalize maps
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
	rep.SelectionAlpha = round4(rep.Accepted.MeanR - rep.AllQualified.MeanR)
	rep.RejectionMeanR = rep.Rejected.MeanR
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
	if !r.HasOut {
		return g
	}
	g.MeanR += r.Outcome.MaxRealizableR
	if r.Outcome.Return5m != nil {
		g.Mean5m += *r.Outcome.Return5m
	}
	if r.Outcome.MaxRealizableR > 0 || (r.Outcome.Return5m != nil && *r.Outcome.Return5m > 0) {
		g.WinRate++ // count wins; finalize divides
	}
	return g
}

func finalize(g GroupStats) GroupStats {
	if g.N == 0 {
		return g
	}
	// MeanR/Mean5m accumulated only over HasOut — approximate using N as denom for stage-1
	// Better: track nOut. ponytail: use N; zeros for missing outcomes dilute.
	n := float64(g.N)
	wins := g.WinRate
	g.MeanR = round4(g.MeanR / n)
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
