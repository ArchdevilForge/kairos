package engine

import (
	"testing"

	"github.com/ArchdevilForge/kairos/internal/config"
	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestNewPipeline_Defaults(t *testing.T) {
	cfg, err := config.LoadString("")
	if err != nil {
		t.Fatal(err)
	}
	p := NewPipeline(cfg, nil)
	if p == nil {
		t.Fatal("nil pipeline")
	}
	if p.minSeverityRank != severityRank("MEDIUM") {
		t.Fatalf("min severity rank: %d", p.minSeverityRank)
	}
}

func TestSeverityRank(t *testing.T) {
	if severityRank("HIGH") <= severityRank("MEDIUM") {
		t.Fatal("HIGH should outrank MEDIUM")
	}
	if severityRank("LOW") >= severityRank("MEDIUM") {
		t.Fatal("LOW should rank below MEDIUM")
	}
}

func TestDiffSymbols(t *testing.T) {
	added := diffSymbols([]string{"A", "B", "C"}, []string{"A", "B"})
	if len(added) != 1 || added[0] != "C" {
		t.Fatalf("added: %v", added)
	}
}

func TestFloatFromMap(t *testing.T) {
	m := map[string]any{"x": 1.5, "y": 2.5}
	v, ok := floatFromMap(m, "x")
	if !ok || v != 1.5 {
		t.Fatalf("x: %v %v", v, ok)
	}
	v, ok = floatFromMap(m, "y")
	if !ok || v != 2.5 {
		t.Fatalf("y: %v %v", v, ok)
	}
	if def := floatFromMapDefault(m, "missing", 9); def != 9 {
		t.Fatalf("default: %v", def)
	}
}

func TestBuildCondition_OpenInterest(t *testing.T) {
	evt := types.AnomalyEvent{
		EventType: "open_interest_change",
		Data: map[string]any{
			"open_interest":          100.0,
			"previous_open_interest": 90.0,
			"change_pct":             11.1,
		},
	}
	cond := buildCondition(evt)
	if cond == "" {
		t.Fatal("expected non-empty condition")
	}
}
