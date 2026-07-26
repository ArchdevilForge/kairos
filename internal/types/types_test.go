package types

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAnomalyEvent_JSONRoundTrip(t *testing.T) {
	orig := AnomalyEvent{
		Symbol:    "BTC/USDT:USDT",
		EventType: "price_velocity",
		Severity:  SeverityMedium,
		Data:      map[string]any{"change_pct": 1.5},
		Timestamp: 1719446400,
	}
	raw, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var decoded AnomalyEvent
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Symbol != orig.Symbol || decoded.EventType != orig.EventType {
		t.Fatalf("decoded: %+v", decoded)
	}
}

func TestAlertEvent_JSONRoundTrip(t *testing.T) {
	orig := AlertEvent{
		Event:     "price_velocity",
		Symbol:    "BTC/USDT:USDT",
		Price:     65000,
		Condition: "30s_0.5pct",
		Severity:  SeverityMedium,
		ChangePct: 1.5,
		EventID:   "evt-1",
		Timestamp: "2026-06-27T00:00:00+00:00",
	}
	raw, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var decoded AlertEvent
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.EventID != "evt-1" || decoded.Price != 65000 {
		t.Fatalf("decoded: %+v", decoded)
	}
}

func TestSetup_JSONRoundTrip(t *testing.T) {
	st := Setup{
		Symbol:      "ETH/USDT:USDT",
		Direction:   "long",
		ActionState: "prepare",
		SetupScore:  6.2,
		Risk: RiskBounds{
			MaxPositionPct: 33,
			MaxLeverage:    5,
			EntryZone:      []float64{100, 101},
			RiskReward:     2.1,
		},
	}
	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Setup
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SetupScore != 6.2 || decoded.Risk.MaxLeverage != 5 {
		t.Fatalf("decoded: %+v", decoded)
	}
}

func TestBoxPattern_Methods(t *testing.T) {
	b := BoxPattern{High: 110, Low: 100, SecondTestHigh: true, ConvergencePct: 0.8}
	if b.Height() != 10 || b.HeightPct() != 10 || b.Midpoint() != 105 {
		t.Fatalf("geometry: h=%v pct=%v mid=%v", b.Height(), b.HeightPct(), b.Midpoint())
	}
	if !b.IsReady() {
		t.Fatal("expected ready box")
	}
	zero := BoxPattern{Low: 0}
	if zero.HeightPct() != 0 {
		t.Fatal("zero low")
	}
}

func TestAnomalyEventSerializationRoundTrip(t *testing.T) {
	evt := AnomalyEvent{
		Symbol:    "BTC/USDT:USDT",
		EventType: "price_velocity",
		Severity:  SeverityHigh,
		Data:      map[string]any{"change_pct": 1.2},
		Timestamp: 1753500000.5,
		Exchange:  "okx",
		EventID:   "okx:BTC/USDT:USDT:price_velocity:1753500000",
	}
	raw, err := json.Marshal(evt)
	if err != nil {
		t.Fatal(err)
	}
	var back AnomalyEvent
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Exchange != evt.Exchange || back.EventID != evt.EventID || back.Timestamp != evt.Timestamp {
		t.Fatalf("round trip lost provenance: %+v", back)
	}
}

func TestMarketSnapshotJSONTags(t *testing.T) {
	raw, err := json.Marshal(MarketSnapshot{Timestamp: 1, ValidSymbols: 2, DataOK: true})
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{`"timestamp"`, `"valid_symbols"`, `"data_ok"`, `"up_breadth_60s"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("snapshot JSON missing snake_case key %s: %s", want, s)
		}
	}
	if strings.Contains(s, `"ValidSymbols"`) {
		t.Fatalf("snapshot JSON leaked PascalCase keys: %s", s)
	}
}
