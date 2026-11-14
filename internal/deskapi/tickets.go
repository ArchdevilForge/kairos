package deskapi

import (
	"math"
	"sort"
	"time"

	"github.com/ArchdevilForge/kairos/internal/cache"
	"github.com/ArchdevilForge/kairos/internal/decision"
	"github.com/ArchdevilForge/kairos/internal/storage"
)

func (s *Server) buildTicketBundlesAndGateHealth(
	now time.Time,
	gcfg decision.BehaviorGateConfig,
	syncedTrades []cache.SyncedTrade,
	fills []cache.TradeFill,
) ([]TicketBundle, GateHealth) {
	health := GateHealth{
		Locked24hSymbols: []string{},
		ActiveCooldowns:  []CooldownEntry{},
	}
	j := s.journal()
	if j == nil {
		return []TicketBundle{}, health
	}

	tickets, err := j.ListTickets("")
	if err != nil {
		return []TicketBundle{}, health
	}
	decisions, _ := j.ListDecisions()
	outcomes, _ := j.ListOutcomes()
	annotations, _ := j.ListAnnotations()

	outcomeByID := make(map[string]storage.CounterfactualOutcome, len(outcomes))
	for _, o := range outcomes {
		outcomeByID[o.TicketID] = o
	}
	annsByID := make(map[string][]storage.GateAnnotation)
	for _, a := range annotations {
		annsByID[a.TicketID] = append(annsByID[a.TicketID], a)
	}
	tradeByTicket := make(map[string]cache.SyncedTrade)
	for _, st := range syncedTrades {
		if st.LinkedTicketID != "" {
			tradeByTicket[st.LinkedTicketID] = st
		}
	}
	fillsByTrade := groupFillsByTrade(fills)

	symSet := make(map[string]struct{})
	bundles := make([]TicketBundle, 0, len(tickets))
	for _, t := range tickets {
		symSet[t.Symbol] = struct{}{}
		b := TicketBundle{
			Ticket:      t,
			Annotations: annsByID[t.ID],
		}
		if b.Annotations == nil {
			b.Annotations = []storage.GateAnnotation{}
		}
		if d, ok := decisions[t.ID]; ok {
			dCopy := d
			b.Decision = &dCopy
		}
		if o, ok := outcomeByID[t.ID]; ok {
			oCopy := o
			b.Outcome = &oCopy
		}
		if st, ok := tradeByTicket[t.ID]; ok {
			stCopy := st
			b.LinkedTrade = &stCopy
			b.Fills = fillsByTrade[st.ID]
		}
		bundles = append(bundles, b)
	}

	cooldownDur := time.Duration(gcfg.CooldownMinutes) * time.Minute
	if cooldownDur <= 0 {
		cooldownDur = 45 * time.Minute
	}
	syms := make([]string, 0, len(symSet))
	for sym := range symSet {
		syms = append(syms, sym)
	}
	sort.Strings(syms)
	for _, sym := range syms {
		cdStats, err := j.SameSymbolCooldownStats(sym, now)
		if err != nil {
			continue
		}
		if cdStats.ConsecutiveStops24h >= 2 || cdStats.CumulativeTrialLoss24hR <= -1.5 {
			stillIn24h := cdStats.LastLossAt.IsZero() || now.Sub(cdStats.LastLossAt) < 24*time.Hour
			if stillIn24h {
				health.Locked24hSymbols = append(health.Locked24hSymbols, sym)
				continue
			}
		}
		if !cdStats.LastLossAt.IsZero() {
			rem := cooldownDur - now.Sub(cdStats.LastLossAt)
			if rem > 0 {
				health.ActiveCooldowns = append(health.ActiveCooldowns, CooldownEntry{
					Symbol:       sym,
					ExpiresAt:    cdStats.LastLossAt.Add(cooldownDur).Unix(),
					RemainingSec: int64(rem.Seconds()),
				})
			}
		}
	}
	if rel, err := j.RelapseHistoryStats("", "", now, time.Hour, 10); err == nil {
		health.ConsecutiveStops24h = rel.GlobalConsecutiveStops
	}

	return bundles, health
}

func groupFillsByTrade(fills []cache.TradeFill) map[string][]cache.TradeFill {
	out := make(map[string][]cache.TradeFill)
	for _, f := range fills {
		if f.TradeID != "" {
			out[f.TradeID] = append(out[f.TradeID], f)
		}
	}
	return out
}

func decisionConfigFromServer(s *Server) decision.BehaviorGateConfig {
	return decision.BehaviorGateConfigFrom(s.activeConfig().RiskGate)
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
