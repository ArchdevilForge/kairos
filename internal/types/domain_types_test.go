package types

import (
	"encoding/json"
	"testing"
)

func TestCycleMap_JSONRoundTrip_SchemaVersion(t *testing.T) {
	orig := CycleMap{
		SchemaVersion: CycleMapSchemaVersion,
		AsOfUnix:      1_700_000_000,
		LegacyClimate: MarketPhaseWinter,
		Nodes: map[string]CycleNode{
			"1d": {
				SchemaVersion: CycleNodeSchemaVersion,
				Timeframe:     "1d",
				Role:          TimeframeRoleContext,
				Direction:     CycleDirectionDown,
				Phase:         WavePhaseSummer,
				Confidence:    0.7,
				Evidence: []Evidence{{
					Code:        "lower_high",
					Description: "series of lower highs",
				}},
			},
			"5m": {
				SchemaVersion: CycleNodeSchemaVersion,
				Timeframe:     "5m",
				Role:          TimeframeRoleTrigger,
				Direction:     CycleDirectionUp,
				Phase:         WavePhaseSpring,
			},
		},
		PrimaryDirection: CycleDirectionDown,
		Alignment:        AlignmentCounterTrend,
		TradeClass:       TradeClassCounterTrendLong,
		Summary:          []string{"1d down summer, 5m up spring"},
	}

	raw, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var decoded CycleMap
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SchemaVersion != CycleMapSchemaVersion {
		t.Fatalf("schema_version=%q", decoded.SchemaVersion)
	}
	if decoded.LegacyClimate != MarketPhaseWinter {
		t.Fatalf("legacy_climate=%q", decoded.LegacyClimate)
	}
	if decoded.Nodes["1d"].Direction != CycleDirectionDown || decoded.Nodes["1d"].Phase != WavePhaseSummer {
		t.Fatalf("1d node=%+v", decoded.Nodes["1d"])
	}
	if decoded.TradeClass != TradeClassCounterTrendLong {
		t.Fatalf("trade_class=%q", decoded.TradeClass)
	}
	// ponytail: freeze — winter climate must not imply short trade class by itself
	if decoded.TradeClass == TradeClassAlignedShort && decoded.LegacyClimate == MarketPhaseWinter {
		t.Fatal("winter climate alone must not encode aligned short")
	}
}

func TestOpportunitySession_JSONRoundTrip(t *testing.T) {
	orig := OpportunitySession{
		SchemaVersion:  OpportunitySessionSchemaVersion,
		ID:             "sess-1",
		EventID:        "evt-1",
		CreatedAt:      100,
		ExpiresAt:      200,
		PulseState:     MarketStateImpulseUp,
		PulseDirection: CycleDirectionUp,
		MarketCycle: CycleMap{
			SchemaVersion:    CycleMapSchemaVersion,
			PrimaryDirection: CycleDirectionUp,
			Alignment:        AlignmentFull,
			TradeClass:       TradeClassAlignedLong,
		},
		Status: OpportunitySessionOpen,
	}
	raw, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var decoded OpportunitySession
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SchemaVersion != OpportunitySessionSchemaVersion || decoded.ID != "sess-1" {
		t.Fatalf("decoded=%+v", decoded)
	}
	if decoded.PulseState != MarketStateImpulseUp {
		t.Fatalf("pulse_state=%q", decoded.PulseState)
	}
}

func TestPlaybookMatch_JSONRoundTrip(t *testing.T) {
	orig := PlaybookMatch{
		SchemaVersion: PlaybookMatchSchemaVersion,
		PlaybookID:    PlaybookLeaderPullbackV1,
		SessionID:     "sess-1",
		Symbol:        "SOL/USDT:USDT",
		Matched:       true,
		Grade:         TicketGradeB,
		Direction:     CycleDirectionUp,
		TradeClass:    TradeClassCounterTrendLong,
		HardFailures:  nil,
		Reasons:       []string{"relative strength rank 1"},
		Warnings:      []string{"counter trend vs 1d"},
		RiskTemplate:  RiskTemplateCounterTrend,
	}
	raw, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var decoded PlaybookMatch
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.Matched || decoded.Grade != TicketGradeB || decoded.RiskTemplate != RiskTemplateCounterTrend {
		t.Fatalf("decoded=%+v", decoded)
	}
}

func TestDecisionTicketAndRecord_JSONRoundTrip(t *testing.T) {
	entry := 100.0
	stop := 97.0
	r := 1.0
	ticket := DecisionTicket{
		SchemaVersion: DecisionTicketSchemaVersion,
		ID:            "tkt-1",
		SessionID:     "sess-1",
		Symbol:        "ETH/USDT:USDT",
		PlaybookID:    PlaybookLeaderPullbackV1,
		Direction:     CycleDirectionUp,
		Grade:         TicketGradeA,
		TradeClass:    TradeClassAlignedLong,
		EntryPlan: EntryPlan{
			Mode:     "closed_bar",
			ZoneLow:  99,
			ZoneHigh: 101,
			Trigger:  "5m higher low restart",
		},
		RiskPlan: RiskPlan{
			TemplateID:        RiskTemplateAlignedSpring,
			RiskBudgetPct:     0.5,
			EntryPrice:        100,
			StopPrice:         97,
			StopDistancePct:   3,
			SuggestedNotional: 1000,
			MaxNotional:       2000,
			MaxLeverage:       5,
			AddOnAllowed:      true,
		},
		Reasons:       []string{"leader pullback"},
		Invalidations: []string{"5m swing low breaks"},
		Status:        TicketStatusOpen,
	}
	raw, err := json.Marshal(ticket)
	if err != nil {
		t.Fatal(err)
	}
	var decTicket DecisionTicket
	if err := json.Unmarshal(raw, &decTicket); err != nil {
		t.Fatal(err)
	}
	if decTicket.SchemaVersion != DecisionTicketSchemaVersion || !decTicket.RiskPlan.AddOnAllowed {
		t.Fatalf("ticket=%+v", decTicket)
	}
	if len(decTicket.Invalidations) != 1 {
		t.Fatalf("invalidations=%v", decTicket.Invalidations)
	}

	rec := DecisionRecord{
		SchemaVersion: DecisionRecordSchemaVersion,
		TicketID:      "tkt-1",
		Decision:      DecisionRejected,
		ReasonCodes:   []string{ReasonTooExtended, ReasonCounterTrend},
		Note:          "late",
		PlannedEntry:  &entry,
		PlannedStop:   &stop,
		PlannedRiskR:  &r,
		DecidedAt:     123,
	}
	raw, err = json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	var decRec DecisionRecord
	if err := json.Unmarshal(raw, &decRec); err != nil {
		t.Fatal(err)
	}
	if decRec.Decision != DecisionRejected || len(decRec.ReasonCodes) != 2 {
		t.Fatalf("record=%+v", decRec)
	}
	if decRec.PlannedEntry == nil || *decRec.PlannedEntry != 100 {
		t.Fatalf("planned_entry=%v", decRec.PlannedEntry)
	}
}

func TestMarketCycle_StillIntact(t *testing.T) {
	// Legacy type must remain usable; PR2 must not delete or rename it.
	c := MarketCycle{
		Phase:      MarketPhaseSpring,
		Confidence: 0.5,
		BtcTrend:   "up",
	}
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var decoded MarketCycle
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Phase != MarketPhaseSpring {
		t.Fatalf("legacy MarketCycle broken: %+v", decoded)
	}
}

func TestDirectionalCandidate_DualScores(t *testing.T) {
	c := DirectionalCandidate{
		SchemaVersion:    DirectionalCandidateSchemaVersion,
		Symbol:           "BTC/USDT:USDT",
		LongScore:        8,
		ShortScore:       2,
		RelativeStrength: 1.5,
		RelativeWeakness: 0.1,
		PullbackStrength: 0.8,
		ReboundWeakness:  0.2,
		LiquidityOK:      true,
		SpreadOK:         true,
		Reasons:          []string{"leads market"},
	}
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var decoded DirectionalCandidate
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.LongScore <= decoded.ShortScore {
		t.Fatalf("expected dual scores preserved: long=%v short=%v", decoded.LongScore, decoded.ShortScore)
	}
	if decoded.RelativeWeakness == 0 && decoded.RelativeStrength == 0 {
		t.Fatal("both relative fields zero after round-trip")
	}
}
