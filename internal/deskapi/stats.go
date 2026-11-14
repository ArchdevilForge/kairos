package deskapi

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/ArchdevilForge/kairos/internal/cache"
	"github.com/ArchdevilForge/kairos/internal/decision"
	"github.com/ArchdevilForge/kairos/internal/evaluation"
	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
)

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := s.activeConfig()
	gcfg := decision.BehaviorGateConfigFrom(cfg.RiskGate)
	loc := decision.ResolveLocation(gcfg.Timezone)

	bankroll := gcfg.TradingBankrollUSD
	if bankroll <= 0 {
		bankroll = 40.0
	}

	j := s.journal()
	rows, err := evaluation.BuildRows(j)
	if err != nil {
		rows = nil
	}
	summary := evaluation.Summarize(rows)
	if summary.ByTradeClass == nil {
		summary.ByTradeClass = map[string]evaluation.GroupStats{}
	}
	if summary.ByGrade == nil {
		summary.ByGrade = map[string]evaluation.GroupStats{}
	}
	if summary.ByPlaybook == nil {
		summary.ByPlaybook = map[string]evaluation.GroupStats{}
	}
	if summary.ByDirection == nil {
		summary.ByDirection = map[string]evaluation.GroupStats{}
	}

	var trades []cache.SyncedTrade
	if s.cache != nil {
		trades, _ = s.cache.ListSyncedTrades(ctx, "")
	}

	rowByTicket := make(map[string]evaluation.TicketRow, len(rows))
	for _, row := range rows {
		rowByTicket[row.Ticket.ID] = row
	}

	// Build closed trades sorted chronologically for equity curve and R stats.
	closedTrades := make([]cache.SyncedTrade, 0, len(trades))
	var totalRealized, totalFee, totalFunding, totalNet float64
	unplannedCount := 0
	for _, tr := range trades {
		totalRealized += tr.RealizedPnL
		totalFee += tr.FeeUSD
		totalFunding += tr.FundingUSD
		totalNet += tr.NetPnL
		if tr.LinkedTicketID == "" || tr.GateCode == decision.GateNoValidTicket {
			unplannedCount++
		}
		if tr.Status == "closed" {
			closedTrades = append(closedTrades, tr)
		}
	}
	sort.Slice(closedTrades, func(i, k int) bool {
		ti := closedTrades[i].ExitTimeMs
		if ti == 0 {
			ti = closedTrades[i].EntryTimeMs
		}
		tk := closedTrades[k].ExitTimeMs
		if tk == 0 {
			tk = closedTrades[k].EntryTimeMs
		}
		if ti != tk {
			return ti < tk
		}
		return closedTrades[i].ID < closedTrades[k].ID
	})

	// Helper: resolve real 1R USD risk for a trade (respecting leverage & stop).
	tradeOneRUSD := func(tr cache.SyncedTrade) (float64, bool) {
		var row *evaluation.TicketRow
		if tr.LinkedTicketID != "" {
			if r, ok := rowByTicket[tr.LinkedTicketID]; ok {
				row = &r
			}
		}
		if row != nil && row.DecisionRecord != nil && row.DecisionRecord.PlannedMaxLossUSD != nil && *row.DecisionRecord.PlannedMaxLossUSD > 0 {
			return *row.DecisionRecord.PlannedMaxLossUSD, true
		}
		stopPx := 0.0
		if row != nil {
			if row.DecisionRecord != nil && row.DecisionRecord.PlannedStop != nil && *row.DecisionRecord.PlannedStop > 0 {
				stopPx = *row.DecisionRecord.PlannedStop
			} else if row.Ticket.RiskPlan.StopPrice > 0 {
				stopPx = row.Ticket.RiskPlan.StopPrice
			}
		}
		if stopPx > 0 && tr.EntryPrice > 0 && tr.Qty > 0 {
			// Leveraged dollar risk: |entry - stop| * qty == (margin * leverage) * |entry - stop|/entry.
			r1 := math.Abs(tr.EntryPrice-stopPx) * tr.Qty
			if r1 > 0 {
				return r1, true
			}
		}
		return 0, false
	}

	acct := s.accountStatus(ctx)
	expectedBankroll := round4(bankroll + totalNet)
	discrepancy := 0.0
	if acct.LastSyncAt > 0 {
		discrepancy = round4(acct.WalletBalanceUSD - expectedBankroll)
	}
	reconciliation := WalletReconciliation{
		TradingBankrollUSD:  round4(bankroll),
		RealizedPnLUSD:      round4(totalRealized),
		FeeUSD:              round4(totalFee),
		FundingUSD:          round4(totalFunding),
		NetPnLUSD:           round4(totalNet),
		ExpectedBankrollUSD: expectedBankroll,
		WalletBalanceUSD:    round4(acct.WalletBalanceUSD),
		UnrealizedPnLUSD:    round4(acct.UnrealizedPnLUSD),
		DiscrepancyUSD:      discrepancy,
		LastSyncAt:          acct.LastSyncAt,
	}

	// Equity curve & R-multiple stats.
	equityCurve := make([]EquityPoint, 0, len(closedTrades)+1)
	curEq := bankroll
	peakEq := bankroll
	cumR := 0.0
	var rValues []float64
	withoutStop := 0
	var posPnL, negPnL float64
	winCount := 0

	buckets := []RBucket{
		{Label: "<-2R", MinR: -1e9, MaxR: -2},
		{Label: "-2R..-1R", MinR: -2, MaxR: -1},
		{Label: "-1R..0R", MinR: -1, MaxR: 0},
		{Label: "0R..1R", MinR: 0, MaxR: 1},
		{Label: "1R..2R", MinR: 1, MaxR: 2},
		{Label: "2R..3R", MinR: 2, MaxR: 3},
		{Label: ">3R", MinR: 3, MaxR: 1e9},
	}

	hourlyAcc := make([]struct {
		count  int
		wins   int
		netPnL float64
		sumR   float64
		rCount int
	}, 24)

	tradeRByID := make(map[string]float64, len(closedTrades))
	tradeHasR := make(map[string]bool, len(closedTrades))

	for idx, tr := range closedTrades {
		curEq += tr.NetPnL
		if curEq > peakEq {
			peakEq = curEq
		}
		ddPct := 0.0
		if peakEq > 0 {
			ddPct = (peakEq - curEq) / peakEq * 100.0
		}
		if tr.NetPnL > 0 {
			winCount++
			posPnL += tr.NetPnL
		} else if tr.NetPnL < 0 {
			negPnL += math.Abs(tr.NetPnL)
		}

		r1, hasStop := tradeOneRUSD(tr)
		if hasStop && r1 > 0 {
			rVal := tr.NetPnL / r1
			cumR += rVal
			rValues = append(rValues, rVal)
			tradeRByID[tr.ID] = rVal
			tradeHasR[tr.ID] = true
			for bi := range buckets {
				b := &buckets[bi]
				if (bi == 0 && rVal < b.MaxR) ||
					(bi == len(buckets)-1 && rVal >= b.MinR) ||
					(bi > 0 && bi < len(buckets)-1 && rVal >= b.MinR && rVal < b.MaxR) {
					b.Count++
					b.NetPnLUSD = round4(b.NetPnLUSD + tr.NetPnL)
					break
				}
			}
		} else {
			withoutStop++
		}

		tsSec := tr.ExitTimeMs / 1000
		if tsSec <= 0 {
			tsSec = tr.EntryTimeMs / 1000
		}
		var walletPtr *float64
		if idx == len(closedTrades)-1 && acct.LastSyncAt > 0 {
			wVal := round4(acct.WalletBalanceUSD)
			walletPtr = &wVal
		}
		equityCurve = append(equityCurve, EquityPoint{
			Time:        tsSec,
			BankrollUSD: round4(curEq),
			WalletUSD:   walletPtr,
			TradePnLUSD: round4(tr.NetPnL),
			CumulativeR: round4(cumR),
			DrawdownPct: round4(ddPct),
		})

		entrySec := tr.EntryTimeMs / 1000
		if entrySec > 0 {
			h := time.Unix(entrySec, 0).In(loc).Hour()
			hourlyAcc[h].count++
			hourlyAcc[h].netPnL += tr.NetPnL
			if tr.NetPnL > 0 {
				hourlyAcc[h].wins++
			}
			if tradeHasR[tr.ID] {
				hourlyAcc[h].sumR += tradeRByID[tr.ID]
				hourlyAcc[h].rCount++
			}
		}
	}

	if len(equityCurve) == 0 {
		var walletPtr *float64
		if acct.LastSyncAt > 0 {
			wVal := round4(acct.WalletBalanceUSD)
			walletPtr = &wVal
		}
		equityCurve = append(equityCurve, EquityPoint{
			Time:        time.Now().Unix(),
			BankrollUSD: round4(bankroll),
			WalletUSD:   walletPtr,
		})
	}

	meanR, medianR := 0.0, 0.0
	if len(rValues) > 0 {
		sumR := 0.0
		for _, v := range rValues {
			sumR += v
		}
		meanR = round4(sumR / float64(len(rValues)))
		cp := append([]float64(nil), rValues...)
		sort.Float64s(cp)
		mid := len(cp) / 2
		if len(cp)%2 == 1 {
			medianR = round4(cp[mid])
		} else {
			medianR = round4((cp[mid-1] + cp[mid]) / 2)
		}
	}
	winRate := 0.0
	if len(closedTrades) > 0 {
		winRate = round4(float64(winCount) / float64(len(closedTrades)))
	}
	pf := 0.0
	if negPnL > 0 {
		pf = round4(posPnL / negPnL)
	} else if posPnL > 0 {
		pf = 99.0
	}

	rDist := RDistribution{
		Buckets:      buckets,
		TotalTrades:  len(closedTrades),
		WithoutStop:  withoutStop,
		MeanR:        meanR,
		MedianR:      medianR,
		WinRate:      winRate,
		ProfitFactor: pf,
	}

	// Gate compliance report.
	gateCompliance := computeGateCompliance(j, rows, unplannedCount)

	// Hourly UTC+8 breakdown.
	hourly := make([]HourlyBucket, 24)
	for h := 0; h < 24; h++ {
		acc := hourlyAcc[h]
		wr := 0.0
		if acc.count > 0 {
			wr = round4(float64(acc.wins) / float64(acc.count))
		}
		mr := 0.0
		if acc.rCount > 0 {
			mr = round4(acc.sumR / float64(acc.rCount))
		}
		probe := time.Date(2026, 1, 1, h, 0, 0, 0, loc)
		hourly[h] = HourlyBucket{
			Hour:         h,
			Label:        fmt.Sprintf("%02d:00", h),
			Count:        acc.count,
			Wins:         acc.wins,
			WinRate:      wr,
			NetPnLUSD:    round4(acc.netPnL),
			MeanR:        mr,
			InLiveWindow: gcfg.InLiveWindow(probe),
		}
	}

	tagBreakdown := TagBreakdown{
		SetupTags: []TagMetricRow{},
		ErrorTags: []TagMetricRow{},
		Grades:    []TagMetricRow{},
		Emotions:  []TagMetricRow{},
	}

	writeJSON(w, http.StatusOK, StatsPayload{
		EquityCurve:          equityCurve,
		WalletReconciliation: reconciliation,
		Summary:              summary,
		RDistribution:        rDist,
		GateCompliance:       gateCompliance,
		HourlyUTC8:           hourly,
		TagBreakdown:         tagBreakdown,
	})
}

func computeGateCompliance(j interface {
	ListAnnotations() ([]storage.GateAnnotation, error)
}, rows []evaluation.TicketRow, unplannedCount int) GateComplianceReport {
	rep := GateComplianceReport{
		RejectionsByCode:    map[string]int{},
		UnplannedTradeCount: unplannedCount,
		ComplianceScore:     100,
	}
	if j != nil {
		if anns, err := j.ListAnnotations(); err == nil {
			rep.TotalAnnotations = len(anns)
			for _, a := range anns {
				for _, c := range a.Codes {
					rep.RejectionsByCode[c]++
				}
				if a.Override {
					rep.Overrides++
				}
			}
		}
	}
	for _, r := range rows {
		if r.Decision != types.DecisionAccepted || !r.HasOut {
			continue
		}
		openTS, endTS := r.Ticket.EntryTriggeredAt, r.Outcome.PathEndUnix
		if openTS == 0 || endTS == 0 {
			continue
		}
		exitReason := r.Outcome.ExitReason
		if exitReason == "" {
			exitReason = r.Ticket.ExitReason
		}
		touchedStop := r.Outcome.TouchedPresetStop || r.Outcome.StopHitFirst
		res := decision.AuditExit(decision.ExitAuditInput{
			TicketID:           r.Ticket.ID,
			HasValidTicket:     r.Ticket.ID != "",
			Symbol:             r.Ticket.Symbol,
			Direction:          r.Ticket.Direction,
			OpenedAt:           time.Unix(openTS, 0),
			ClosedAt:           time.Unix(endTS, 0),
			ExitReason:         exitReason,
			RealizedR:          r.Outcome.NetR,
			PresetStopPrice:    r.Ticket.RiskPlan.StopPrice,
			ExitPrice:          r.Outcome.ExitPrice,
			TouchedPresetStop:  touchedStop,
			FakeoutConfirmed:   r.Outcome.FakeoutConfirmed,
			MarketStressActive: r.Outcome.MarketStressActive,
			StructureIntact:    r.Outcome.StructureIntact,
		})
		switch {
		case res.IsViolation:
			rep.ScalpViolations++
		case res.Verdict == decision.VerdictProtectiveStopExempt:
			rep.ProtectiveStopExemptions++
		case res.Verdict == decision.VerdictEarlyProfitNote:
			rep.EarlyProfitNotes++
		}
	}
	penalty := float64(rep.Overrides*10 + rep.ScalpViolations*15 + rep.UnplannedTradeCount*15)
	bonus := float64(rep.ProtectiveStopExemptions * 2)
	score := math.Max(0, math.Min(100, 100-penalty+bonus))
	rep.ComplianceScore = round2(score)
	return rep
}

func round4(v float64) float64 {
	r := math.Round(v*10000) / 10000
	if r == 0 {
		return 0
	}
	return r
}
