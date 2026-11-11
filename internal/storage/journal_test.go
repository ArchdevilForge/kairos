package storage

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestJournal_SessionTicketDecisionRoundTrip(t *testing.T) {
	j, err := NewJournal(types.StorageConfig{DatabasePath: filepath.Join(t.TempDir(), "k.db")})
	if err != nil {
		t.Fatal(err)
	}
	sess := types.OpportunitySession{
		SchemaVersion: types.OpportunitySessionSchemaVersion,
		ID:            "sess-1", EventID: "e1", Status: types.OpportunitySessionOpen,
		PulseDirection: types.CycleDirectionUp,
	}
	if err := j.SaveSession(sess); err != nil {
		t.Fatal(err)
	}
	tkt := types.DecisionTicket{
		SchemaVersion: types.DecisionTicketSchemaVersion,
		ID:            "t1", SessionID: "sess-1", Symbol: "BTC/USDT:USDT",
		Status: types.TicketStatusOpen, Grade: types.TicketGradeA,
	}
	if err := j.SaveTicket(tkt); err != nil {
		t.Fatal(err)
	}
	tkt.Status = types.TicketStatusAccepted
	if err := j.SaveTicket(tkt); err != nil {
		t.Fatal(err)
	}
	got, ok, err := j.GetTicket("t1")
	if err != nil || !ok || got.Status != types.TicketStatusAccepted {
		t.Fatalf("got=%+v ok=%v err=%v", got, ok, err)
	}
	if err := j.SaveDecision(types.DecisionRecord{
		SchemaVersion: types.DecisionRecordSchemaVersion,
		TicketID:      "t1", Decision: types.DecisionAccepted,
		ReasonCodes: []string{types.ReasonStructureGood}, DecidedAt: 1,
	}); err != nil {
		t.Fatal(err)
	}
	dec, ok, err := j.GetDecision("t1")
	if err != nil || !ok || dec.Decision != types.DecisionAccepted {
		t.Fatalf("dec=%+v", dec)
	}
	if err := j.SaveOutcome(CounterfactualOutcome{TicketID: "t1", SessionID: "sess-1", MFE: 1.2}); err != nil {
		t.Fatal(err)
	}
	outs, err := j.ListOutcomes()
	if err != nil || len(outs) != 1 || outs[0].MFE != 1.2 {
		t.Fatalf("outs=%v err=%v", outs, err)
	}
}

func TestJournalMetadataAttached(t *testing.T) {
	dir := t.TempDir()
	j, err := NewJournal(types.StorageConfig{DatabasePath: filepath.Join(dir, "k.db")})
	if err != nil {
		t.Fatal(err)
	}
	j.SetMetadata(ResearchMeta{
		GitSHA: "abc123", ConfigHash: "feedbeef",
		StrategyVersion: "v1", ExperimentID: "exp-1", Mode: "shadow",
	})
	if err := j.SaveTicket(types.DecisionTicket{ID: "t1"}); err != nil {
		t.Fatal(err)
	}
	rows, err := j.readAll()
	if err != nil || len(rows) == 0 {
		t.Fatalf("readAll: %v rows=%d", err, len(rows))
	}
	line := rows[0]
	if line.GitSHA != "abc123" || line.ConfigHash != "feedbeef" ||
		line.StrategyVersion != "v1" || line.ExperimentID != "exp-1" || line.Mode != "shadow" {
		t.Fatalf("metadata missing: %+v", line)
	}
}

func TestConfigHashOfStable(t *testing.T) {
	h1 := ConfigHashOf(map[string]any{"a": 1, "b": "x"})
	h2 := ConfigHashOf(map[string]any{"a": 1, "b": "x"})
	h3 := ConfigHashOf(map[string]any{"a": 1, "b": "y"})
	if h1 == "" || h1 != h2 || h1 == h3 {
		t.Fatalf("hash unstable: %q %q %q", h1, h2, h3)
	}
}

func TestJournal_SeasonRegimeAndWinterDefenseHint(t *testing.T) {
	dir := t.TempDir()
	j, err := NewJournal(types.StorageConfig{DatabasePath: filepath.Join(dir, "k.db")})
	if err != nil {
		t.Fatal(err)
	}

	// 1. WINTER ticket -> both season_regime and winter_defense_hint persisted
	if err := j.SaveTicket(types.DecisionTicket{
		SchemaVersion: types.DecisionTicketSchemaVersion,
		ID:            "t-win",
		SessionID:     "s1",
		Symbol:        "BTC/USDT:USDT",
		Status:        types.TicketStatusOpen,
		SeasonRegime:  types.SeasonRegimeWinter,
	}); err != nil {
		t.Fatal(err)
	}
	gotWin, ok, err := j.GetTicket("t-win")
	if err != nil || !ok {
		t.Fatalf("GetTicket(t-win): ok=%v err=%v", ok, err)
	}
	if gotWin.SeasonRegime != types.SeasonRegimeWinter || gotWin.WinterDefenseHint == "" {
		t.Fatalf("expected WINTER + hint, got regime=%q hint=%q", gotWin.SeasonRegime, gotWin.WinterDefenseHint)
	}

	// 2. SUMMER ticket -> season_regime=SUMMER, winter_defense_hint omitted
	if err := j.SaveTicket(types.DecisionTicket{
		SchemaVersion: types.DecisionTicketSchemaVersion,
		ID:            "t-sum",
		SessionID:     "s1",
		Symbol:        "ETH/USDT:USDT",
		Status:        types.TicketStatusOpen,
		SeasonRegime:  types.SeasonRegimeSummer,
	}); err != nil {
		t.Fatal(err)
	}
	gotSum, ok, err := j.GetTicket("t-sum")
	if err != nil || !ok {
		t.Fatalf("GetTicket(t-sum): ok=%v err=%v", ok, err)
	}
	if gotSum.SeasonRegime != types.SeasonRegimeSummer || gotSum.WinterDefenseHint != "" {
		t.Fatalf("expected SUMMER + empty hint, got regime=%q hint=%q", gotSum.SeasonRegime, gotSum.WinterDefenseHint)
	}

	// 3. No cross-sectional input (SeasonRegime == ""), even with MarketCycle winter:
	// BOTH season_regime and winter_defense_hint MUST be absent in struct and raw JSONL.
	if err := j.SaveTicket(types.DecisionTicket{
		SchemaVersion: types.DecisionTicketSchemaVersion,
		ID:            "t-unm",
		SessionID:     "s1",
		Symbol:        "SOL/USDT:USDT",
		Status:        types.TicketStatusOpen,
		MarketCycle: types.CycleMap{
			LegacyClimate: types.MarketPhaseWinter,
			Nodes: map[string]types.CycleNode{
				"15m": {Timeframe: "15m", Phase: types.WavePhaseWinter},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := j.SaveOutcome(CounterfactualOutcome{
		SchemaVersion: CounterfactualSchemaVersion,
		TicketID:      "t-unm",
		SessionID:     "s1",
		Symbol:        "SOL/USDT:USDT",
	}); err != nil {
		t.Fatal(err)
	}
	gotUnm, ok, err := j.GetTicket("t-unm")
	if err != nil || !ok {
		t.Fatalf("GetTicket(t-unm): ok=%v err=%v", ok, err)
	}
	if gotUnm.SeasonRegime != "" || gotUnm.WinterDefenseHint != "" {
		t.Fatalf("expected both fields empty when unmeasured, got regime=%q hint=%q",
			gotUnm.SeasonRegime, gotUnm.WinterDefenseHint)
	}
	outUnm, ok, err := j.GetOutcome("t-unm")
	if err != nil || !ok {
		t.Fatalf("GetOutcome(t-unm): ok=%v err=%v", ok, err)
	}
	if outUnm.SeasonRegime != "" || outUnm.WinterDefenseHint != "" {
		t.Fatalf("expected outcome fields empty when unmeasured, got regime=%q hint=%q",
			outUnm.SeasonRegime, outUnm.WinterDefenseHint)
	}
	rows, err := j.readAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ID == "t-unm" {
			raw := string(r.Payload)
			if strings.Contains(raw, `"season_regime"`) || strings.Contains(raw, `"winter_defense_hint"`) {
				t.Fatalf("journal %s payload for unmeasured record must omit both fields: %s", r.Kind, raw)
			}
		}
	}
}

func TestJournal_TrialBudgetAndRelapseHistoryStats(t *testing.T) {
	dir := t.TempDir()
	j, err := NewJournal(types.StorageConfig{DatabasePath: filepath.Join(dir, "k.db")})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_730_000_000, 0)

	// Rejected ticket does NOT count against trial budget; accepted/open/closed within 24h DO count; >24h old excluded.
	records := []types.DecisionTicket{
		{ID: "t-old", Symbol: "SOL/USDT:USDT", SetupPoint: types.SetupPoint1Breakout, Status: types.TicketStatusClosed, CreatedAt: now.Add(-26 * time.Hour).Unix()},
		{ID: "t-rej", Symbol: "SOL/USDT:USDT", SetupPoint: types.SetupPoint1Breakout, Status: types.TicketStatusRejected, CreatedAt: now.Add(-2 * time.Hour).Unix()},
		{ID: "t-1", Symbol: "SOL/USDT:USDT", SetupPoint: types.SetupPoint1Breakout, Status: types.TicketStatusClosed, Direction: types.CycleDirectionUp, ExitReason: types.ExitReasonProtectiveStop, ClosedAt: now.Add(-50 * time.Minute).Unix(), CreatedAt: now.Add(-55 * time.Minute).Unix()},
		{ID: "t-2", Symbol: "SOL/USDT:USDT", SetupPoint: types.SetupPoint1Breakout, Status: types.TicketStatusClosed, Direction: types.CycleDirectionDown, ExitReason: types.ExitReasonProtectiveStop, ClosedAt: now.Add(-15 * time.Minute).Unix(), CreatedAt: now.Add(-20 * time.Minute).Unix()},
		{ID: "t-3", Symbol: "SOL/USDT:USDT", SetupPoint: types.SetupPoint2MinorDiv, Status: types.TicketStatusOpen, Direction: types.CycleDirectionUp, CreatedAt: now.Add(-5 * time.Minute).Unix()},
	}
	for _, tk := range records {
		if err := j.SaveTicket(tk); err != nil {
			t.Fatal(err)
		}
	}
	for i, id := range []string{"t-old", "t-1", "t-2"} {
		r := 2.0 + float64(i)*0.2
		_ = j.SaveDecision(types.DecisionRecord{
			TicketID: id, Decision: types.DecisionAccepted,
			PlannedMaxLossUSD: &r, DecidedAt: now.Add(-time.Duration(60-i*20) * time.Minute).Unix(),
		})
	}

	sameSetup, sameSym, err := j.TrialBudgetStats("SOL/USDT:USDT", types.SetupPoint1Breakout, "t-cur", now, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if sameSetup != 2 || sameSym != 3 {
		t.Fatalf("TrialBudgetStats got sameSetup=%d sameSym=%d, want 2 and 3", sameSetup, sameSym)
	}

	rel, err := j.RelapseHistoryStats("SOL/USDT:USDT", "t-cur", now, 60*time.Minute, 10)
	if err != nil {
		t.Fatal(err)
	}
	if rel.GlobalConsecutiveStops != 2 || !rel.LastOutcomeWasLoss {
		t.Fatalf("RelapseHistoryStats stops=%d lastLoss=%v", rel.GlobalConsecutiveStops, rel.LastOutcomeWasLoss)
	}
	if rel.LastStopSameSymbolDirection != types.CycleDirectionDown {
		t.Fatalf("LastStopSameSymbolDirection=%q, want down", rel.LastStopSameSymbolDirection)
	}
	if rel.RecentTicketsInChurnWindow != 3 {
		t.Fatalf("RecentTicketsInChurnWindow=%d, want 3", rel.RecentTicketsInChurnWindow)
	}
	if len(rel.RecentPersonalRiskUSD) != 3 {
		t.Fatalf("RecentPersonalRiskUSD=%v, want 3 entries", rel.RecentPersonalRiskUSD)
	}
}
