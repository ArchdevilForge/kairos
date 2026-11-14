package evaluation

import (
	"math"

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

	// HumanAlpha = accepted.MeanNetR - accepted.MeanMechR:
	// 人工入场+退出 vs 机械规则(Entry+Exit 合并)。正 = 人工干预加分,负 = 拖后腿。
	HumanAlpha float64 `json:"human_alpha"`

	// Work Item 13: Orthogonal per-trade HumanAlpha decomposition
	// NetR_actual = PoolMean_mech + SelectionAlpha + TimingAlpha + ManagementAlpha (residual === 0.000R).
	TradeAlpha             []TradeAlphaDecomposition `json:"trade_alpha,omitempty"`
	MeanPoolMechR          float64                   `json:"mean_pool_mech_r"`
	MeanSelectionAlpha     float64                   `json:"mean_selection_alpha"`
	MeanTimingAlpha        float64                   `json:"mean_timing_alpha"`
	MeanManagementAlpha    float64                   `json:"mean_management_alpha"`
	MaxAbsIdentityResidual float64                   `json:"max_abs_identity_residual"`

	ByTradeClass map[string]GroupStats `json:"by_trade_class,omitempty"`
	ByGrade      map[string]GroupStats `json:"by_grade,omitempty"`
	ByPlaybook   map[string]GroupStats `json:"by_playbook,omitempty"`
	ByDirection  map[string]GroupStats `json:"by_direction,omitempty"`
}

// TradeAlphaDecomposition holds the exact orthogonal decomposition for one trade (Work Item 13):
//
//	NetRActual = PoolMeanMech + SelectionAlpha + TimingAlpha + ManagementAlpha (Residual == 0.000R)
type TradeAlphaDecomposition struct {
	TicketID        string  `json:"ticket_id"`
	WaveClusterID   string  `json:"wave_cluster_id"`
	Symbol          string  `json:"symbol"`
	NetRActual      float64 `json:"net_r_actual"`
	PoolMeanMech    float64 `json:"pool_mean_mech"`
	TicketMechR     float64 `json:"ticket_mech_r"`
	HumanEntryMechR float64 `json:"human_entry_mech_r"`
	SelectionAlpha  float64 `json:"selection_alpha"`
	TimingAlpha     float64 `json:"timing_alpha"`
	ManagementAlpha float64 `json:"management_alpha"`
	Residual        float64 `json:"residual"`
}

// TicketRow joins ticket + decision + outcome for attribution.
type TicketRow struct {
	Ticket         types.DecisionTicket
	Decision       types.HumanDecision
	DecisionRecord *types.DecisionRecord
	Outcome        storage.CounterfactualOutcome
	HasOut         bool
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
			dCopy := d
			row.DecisionRecord = &dCopy
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

// Summarize computes selection/rejection stats and orthogonal HumanAlpha decomposition on complete outcomes.
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
			if r.Outcome.Complete || r.Outcome.Finalized {
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
	rep.HumanAlpha = round4(rep.Accepted.MeanNetR - rep.Accepted.MeanMechR)

	decomps := ComputeOrthogonalHumanAlpha(rows)
	rep.TradeAlpha = decomps
	if len(decomps) > 0 {
		var sumPool, sumSel, sumTim, sumMgt, maxRes float64
		for _, d := range decomps {
			sumPool += d.PoolMeanMech
			sumSel += d.SelectionAlpha
			sumTim += d.TimingAlpha
			sumMgt += d.ManagementAlpha
			if absRes := math.Abs(d.Residual); absRes > maxRes {
				maxRes = absRes
			}
		}
		nf := float64(len(decomps))
		rep.MeanPoolMechR = round3(sumPool / nf)
		rep.MeanSelectionAlpha = round3(sumSel / nf)
		rep.MeanTimingAlpha = round3(sumTim / nf)
		rep.MeanManagementAlpha = round3(sumMgt / nf)
		rep.MaxAbsIdentityResidual = round3(maxRes)
	}
	return rep
}

// ComputeOrthogonalHumanAlpha computes the per-trade orthogonal HumanAlpha identity (Work Item 13):
//
//	NetR_actual = PoolMean_mech + SelectionAlpha + TimingAlpha + ManagementAlpha
//
// where PoolMean_mech is the equal-risk mechanical execution mean of all gate-passing candidates
// in the same wave_cluster_id, and each ticket's mechanical counterfactual uses only the stop
// frozen at ticket creation + the standard wave tracking rule.
// Residual is guaranteed to be 0.000R on every trade.
func ComputeOrthogonalHumanAlpha(rows []TicketRow) []TradeAlphaDecomposition {
	if len(rows) == 0 {
		return nil
	}
	clusterByTicket := resolveWaveClustersForRows(rows)

	// 1. Collect gate-passing candidates per wave_cluster_id to compute PoolMean_mech.
	poolSums := make(map[string]float64)
	poolCounts := make(map[string]int)
	mechByTicket := make(map[string]float64, len(rows))

	for _, r := range rows {
		if !isCompleteOutcome(r) {
			continue
		}
		cid := clusterByTicket[r.Ticket.ID]
		mechR := ticketMechanicalWaveR(r)
		mechByTicket[r.Ticket.ID] = mechR
		if r.Ticket.Grade != types.TicketGradeD {
			poolSums[cid] += mechR
			poolCounts[cid]++
		}
	}

	var out []TradeAlphaDecomposition
	for _, r := range rows {
		if r.Decision != types.DecisionAccepted || !isCompleteOutcome(r) {
			continue
		}
		cid := clusterByTicket[r.Ticket.ID]
		tMech := mechByTicket[r.Ticket.ID]
		poolMean := tMech
		if cnt := poolCounts[cid]; cnt > 0 {
			poolMean = poolSums[cid] / float64(cnt)
		}
		hEntryMech := humanEntryMechanicalR(r, tMech)

		// Quantize the 4 telescope anchor levels (A0, A1, A2, A3) to 0.001R so the
		// orthogonal identity holds with an exact 0.000R residual.
		a0 := round3(poolMean)
		a1 := round3(tMech)
		a2 := round3(hEntryMech)
		a3 := round3(r.Outcome.NetR)

		selAlpha := round3(a1 - a0)
		timAlpha := round3(a2 - a1)
		mgtAlpha := round3(a3 - (a0 + selAlpha + timAlpha))
		recon := round3(a0 + selAlpha + timAlpha + mgtAlpha)
		residual := round3(a3 - recon)
		if residual == 0 {
			residual = 0.0
		}

		out = append(out, TradeAlphaDecomposition{
			TicketID:        r.Ticket.ID,
			WaveClusterID:   cid,
			Symbol:          r.Ticket.Symbol,
			NetRActual:      a3,
			PoolMeanMech:    a0,
			TicketMechR:     a1,
			HumanEntryMechR: a2,
			SelectionAlpha:  selAlpha,
			TimingAlpha:     timAlpha,
			ManagementAlpha: mgtAlpha,
			Residual:        residual,
		})
	}
	return out
}

func resolveWaveClustersForRows(rows []TicketRow) map[string]string {
	out := make(map[string]string, len(rows))
	var unlabeled []WakeEventTimestamp
	for _, r := range rows {
		cid := r.Outcome.WaveClusterID
		if cid == "" {
			cid = r.Ticket.WaveClusterID
		}
		if cid != "" {
			out[r.Ticket.ID] = cid
			continue
		}
		ts := r.Ticket.SignalAt
		if ts == 0 {
			ts = r.Ticket.CreatedAt
		}
		if ts == 0 {
			ts = r.Outcome.PathStartUnix
		}
		if ts == 0 {
			ts = r.Outcome.AsOfUnix
		}
		unlabeled = append(unlabeled, WakeEventTimestamp{ID: r.Ticket.ID, Timestamp: ts})
	}
	if len(unlabeled) > 0 {
		inferred := BuildWaveClusters(unlabeled)
		for id, cid := range inferred {
			out[id] = cid
		}
	}
	return out
}

func isCompleteOutcome(r TicketRow) bool {
	return r.HasOut && (r.Outcome.Finalized || r.Outcome.Complete)
}

func ticketMechanicalWaveR(r TicketRow) float64 {
	if r.Outcome.PolicyWaveR != nil {
		return *r.Outcome.PolicyWaveR
	}
	// When an outcome was not accepted by the human, its recorded NetR is already the mechanical counterfactual.
	if r.Decision != types.DecisionAccepted {
		return r.Outcome.NetR
	}
	// Estimate friction in R from the ticket's risk plan + default friction if MechanicalR is present.
	entry := r.Ticket.RiskPlan.EntryPrice
	stop := r.Ticket.RiskPlan.StopPrice
	if entry > 0 && stop > 0 {
		p := DefaultFrictionParams()
		s0 := BaseSlippageFrac(r.Ticket.Symbol, p)
		r1Pct := Initial1RPct(entry, stop, s0)
		roundTripFrac := 2.0 * (p.TakerFeeFrac + s0)
		costR := 0.0
		if r1Pct > 0 {
			costR = roundTripFrac / r1Pct
		}
		return r.Outcome.MechanicalR - costR
	}
	return r.Outcome.MechanicalR
}

func humanEntryMechanicalR(r TicketRow, ticketMechR float64) float64 {
	if r.Outcome.MechHumanEntryR != nil {
		return *r.Outcome.MechHumanEntryR
	}
	humanEntry := r.Outcome.HumanEntryPrice
	if humanEntry <= 0 && r.DecisionRecord != nil && r.DecisionRecord.PlannedEntry != nil {
		humanEntry = *r.DecisionRecord.PlannedEntry
	}
	tktEntry := r.Ticket.RiskPlan.EntryPrice
	frozenStop := r.Ticket.RiskPlan.StopPrice
	if humanEntry <= 0 || tktEntry <= 0 || frozenStop <= 0 || math.Abs(humanEntry-tktEntry) < 1e-9 {
		return ticketMechR
	}
	sgn := 1.0
	if r.Ticket.Direction == types.CycleDirectionDown {
		sgn = -1.0
	}
	tktRisk := math.Abs(tktEntry - frozenStop)
	humanRisk := math.Abs(humanEntry - frozenStop)
	if tktRisk <= 0 || humanRisk <= 0 {
		return ticketMechR
	}
	// Re-anchor the mechanical wave exit price onto humanEntry with the frozen ticket stop.
	impliedExit := tktEntry + sgn*ticketMechR*tktRisk
	return sgn * (impliedExit - humanEntry) / humanRisk
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
	if !r.HasOut || !r.Outcome.Finalized {
		// fall back to Complete for older journal rows
		if !r.HasOut || !r.Outcome.Complete {
			return g
		}
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

func round3(v float64) float64 {
	r := math.Round(v*1000) / 1000
	if r == 0 {
		return 0
	}
	return r
}

func round4(v float64) float64 {
	r := math.Round(v*10000) / 10000
	if r == 0 {
		return 0
	}
	return r
}
