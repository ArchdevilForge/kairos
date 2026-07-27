package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAlertContinuation(t *testing.T) {
	up := 0.2
	down := -0.1
	flat := 0.0
	outs := []outcomeRecord{
		{Direction: "up", MedianReturn5m: &up},
		{Direction: "up", MedianReturn5m: &down},
		{Direction: "down", MedianReturn5m: &down},
		{Direction: "up"},
		{Direction: "up", MedianReturn5m: &flat},
	}
	n, cont := alertContinuation(outs)
	if n != 4 || cont != 2 {
		t.Fatalf("n=%d cont=%d want 4/2", n, cont)
	}
}

func TestFilterOutcomesShadow(t *testing.T) {
	in := []outcomeRecord{
		{SourceEvent: "market_impulse", ShadowMode: true},
		{SourceEvent: "market_impulse", ShadowMode: false},
	}
	out := filterOutcomes(in, func(o outcomeRecord) bool { return !o.ShadowMode })
	if len(out) != 1 || out[0].ShadowMode {
		t.Fatalf("%+v", out)
	}
}

func TestDedupeSnapshotsByMinute(t *testing.T) {
	in := []snapshotRecord{
		{Timestamp: 100, MedianReturn60s: 0.1},
		{Timestamp: 130, MedianReturn60s: 0.2},  // same minute as 100 if floor/60: 100/60=1, 130/60=2
		{Timestamp: 125, MedianReturn60s: 0.15}, // minute 2 with 130
	}
	// 100 → min 1, 125 → min 2, 130 → min 2 (last wins 0.2)
	out := dedupeSnapshotsByMinute(in)
	if len(out) != 2 {
		t.Fatalf("len=%d %+v", len(out), out)
	}
	if out[1].MedianReturn60s != 0.2 {
		t.Fatalf("want last of minute, got %+v", out[1])
	}
}

func TestExcludeWindows(t *testing.T) {
	events := []eventRecord{{Timestamp: 1000}}
	wins := alertExcludeWindows(events, nil)
	if len(wins) != 1 {
		t.Fatal(wins)
	}
	if !inExclude(1000, wins) || !inExclude(1000-300, wins) || !inExclude(1000+900, wins) {
		t.Fatal("bounds should be excluded")
	}
	if inExclude(1000-301, wins) || inExclude(1000+901, wins) {
		t.Fatal("outside window")
	}
	// Interval that starts outside but ends inside the alert window must count as overlap.
	if !intervalOverlapsExclude(650, 950, wins) {
		t.Fatal("650→950 should overlap exclude [700,1900]")
	}
	if intervalOverlapsExclude(100, 400, wins) {
		t.Fatal("100→400 is fully before exclude")
	}
}

// P1-1: baseline start outside exclude, but +5m end inside → must not enter control.
func TestCollectBaselineExcludesForwardHorizonLeak(t *testing.T) {
	// Alert @1000 → exclude [700, 1900].
	// Start 650 is outside; forward ~950 is inside → whole sample is contaminated.
	snaps := []snapshotRecord{
		{Timestamp: 650, DataOK: true, MedianReturn60s: 0.25, MedianReturn300s: 0.05},
		{Timestamp: 950, DataOK: true, MedianReturn60s: 0.10, MedianReturn300s: 0.40},
		// Clean pair far from the alert window.
		{Timestamp: 3000, DataOK: true, MedianReturn60s: 0.20, MedianReturn300s: 0.05},
		{Timestamp: 3300, DataOK: true, MedianReturn60s: 0.10, MedianReturn300s: 0.30},
	}
	wins := alertExcludeWindows([]eventRecord{{Timestamp: 1000}}, nil)
	base := collectBaseline(snaps, 0.08, wins)
	for _, s := range base {
		if s.ts == 650 {
			t.Fatalf("forward-horizon leak: start 650 end 950 must be excluded, got %+v", base)
		}
	}
	if len(base) != 1 || base[0].ts != 3000 {
		t.Fatalf("want only clean t=3000, got %+v", base)
	}
}

func TestCollectBaselineExcludesAlertWindow(t *testing.T) {
	// Alert at t=1000. Snap at 1000 would be in window; snap at 0 is free.
	snaps := []snapshotRecord{
		{Timestamp: 0, DataOK: true, MedianReturn60s: 0.20, MedianReturn300s: 0.05},
		{Timestamp: 300, DataOK: true, MedianReturn60s: 0.10, MedianReturn300s: 0.30}, // forward for t=0
		{Timestamp: 1000, DataOK: true, MedianReturn60s: 0.50, MedianReturn300s: 0.10},
		{Timestamp: 1300, DataOK: true, MedianReturn60s: 0.10, MedianReturn300s: 0.40},
	}
	wins := alertExcludeWindows([]eventRecord{{Timestamp: 1000}}, nil)
	base := collectBaseline(snaps, 0.08, wins)
	for _, s := range base {
		if s.ts == 1000 {
			t.Fatalf("alert minute must not enter baseline: %+v", base)
		}
	}
	if len(base) != 1 || base[0].ts != 0 {
		t.Fatalf("want only t=0 sample, got %+v", base)
	}
	if !base[0].cont {
		t.Fatal("t=0 should continue (fwd med300=0.30)")
	}
}

func TestStratifiedHourLift(t *testing.T) {
	// Hour 0 / up baseline: 1 cont / 2 → Laplace (1+1)/(2+2)=0.5
	base := []baselineSample{
		{ts: 0, hour: 0, bucket: 1, direction: "up", cont: true},
		{ts: 60, hour: 0, bucket: 1, direction: "up", cont: false},
	}
	up := 0.4
	outcomes := []outcomeRecord{{EventTS: 10, Direction: "up", MedianReturn5m: &up}}
	// alertRate=1, expectedBase=0.5 → lift=2
	lift, used, exp := stratifiedLift(outcomes, nil, base, stratHourOnly)
	if used != 1 || exp < 0.49 || exp > 0.51 {
		t.Fatalf("used=%d exp=%v", used, exp)
	}
	if lift < 1.9 || lift > 2.1 {
		t.Fatalf("lift=%v", lift)
	}
}

// P1-2: up alerts must not match down baseline cells (and vice versa).
func TestStratifiedLiftMatchesDirection(t *testing.T) {
	base := []baselineSample{
		{ts: 0, hour: 0, bucket: 1, direction: "down", cont: true},
		{ts: 60, hour: 0, bucket: 1, direction: "down", cont: true},
	}
	up := 0.4
	outcomes := []outcomeRecord{{EventTS: 10, Direction: "up", MedianReturn5m: &up}}
	lift, used, _ := stratifiedLift(outcomes, nil, base, stratHourOnly)
	if used != 0 || lift != 0 {
		t.Fatalf("up alert must not use down baseline cell, used=%d lift=%v", used, lift)
	}
	// Same hour+bucket down cell still must not match under mag mode either.
	events := []eventRecord{{Timestamp: 10, MedianReturn60s: 0.25}} // bucket 1
	lift, used, _ = stratifiedLift(outcomes, events, base, stratHourMag)
	if used != 0 || lift != 0 {
		t.Fatalf("hour×dir×|med60|: up must not match down, used=%d lift=%v", used, lift)
	}
}

func TestStratifiedHourMagRequiresEventMed(t *testing.T) {
	base := []baselineSample{{ts: 0, hour: 0, bucket: 2, direction: "up", cont: true}}
	up := 0.2
	outcomes := []outcomeRecord{{EventTS: 10, Direction: "up", MedianReturn5m: &up}}
	// no events → cannot match mag bucket
	lift, used, _ := stratifiedLift(outcomes, nil, base, stratHourMag)
	if used != 0 || lift != 0 {
		t.Fatalf("want no match without event med60, used=%d lift=%v", used, lift)
	}
	events := []eventRecord{{Timestamp: 10, MedianReturn60s: 0.40}} // bucket 2
	lift, used, exp := stratifiedLift(outcomes, events, base, stratHourMag)
	if used != 1 {
		t.Fatalf("used=%d", used)
	}
	// Laplace on n=1 cont=1 → (2)/(3)≈0.667, alertRate=1 → lift≈1.5
	if exp < 0.6 || exp > 0.7 || lift < 1.4 || lift > 1.6 {
		t.Fatalf("lift=%v exp=%v", lift, exp)
	}
}

func TestEvidenceStatus(t *testing.T) {
	if evidenceStatus(1) != "insufficient_evidence" {
		t.Fatal(evidenceStatus(1))
	}
	if evidenceStatus(30) != "provisional" {
		t.Fatal(evidenceStatus(30))
	}
	if evidenceStatus(100) != "watch_stability" {
		t.Fatal(evidenceStatus(100))
	}
}

func TestWilsonAndRatioBounds(t *testing.T) {
	lo, hi := wilsonCI(5, 10, 0.95)
	if lo >= 0.5 || hi <= 0.5 || lo < 0 || hi > 1 {
		t.Fatalf("wilson %v %v", lo, hi)
	}
	rLo, rHi := ratioBounds(0.4, 0.6, 0.2, 0.3)
	// 0.4/0.3 .. 0.6/0.2
	if rLo < 1.3 || rLo > 1.4 || rHi < 2.9 || rHi > 3.1 {
		t.Fatalf("ratio %v %v", rLo, rHi)
	}
}

func TestEffectiveBaselineN(t *testing.T) {
	// 0,60,120,300 → anchors at 0 and 300 (gap≥300)
	s := []baselineSample{{ts: 0}, {ts: 60}, {ts: 120}, {ts: 300}}
	if n := effectiveBaselineN(s); n != 2 {
		t.Fatalf("n=%d", n)
	}
}

func TestForwardSnapshot300(t *testing.T) {
	snaps := []snapshotRecord{
		{Timestamp: 0, DataOK: true, MedianReturn300s: 0.1},
		{Timestamp: 290, DataOK: true, MedianReturn300s: 0.5},
		{Timestamp: 400, DataOK: true, MedianReturn300s: 0.9},
	}
	s, ok := forwardSnapshot300(snaps, 0, 300, 45)
	if !ok || s.MedianReturn300s != 0.5 || s.Timestamp != 290 {
		t.Fatalf("got %+v %v want ts=290 med=0.5", s, ok)
	}
	if _, ok := forwardSnapshot300(snaps, 0, 900, 45); ok {
		t.Fatal("expected miss far target")
	}
}

func TestLoadSnapshotsDedupe(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snaps.jsonl")
	body := strings.Join([]string{
		`{"timestamp":100,"data_ok":true,"median_return_60s":0.1,"median_return_300s":0.0}`,
		`{"timestamp":130,"data_ok":true,"median_return_60s":0.2,"median_return_300s":0.0}`,
		`{"timestamp":50,"data_ok":false,"median_return_60s":0.0,"median_return_300s":0.0}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	snaps, err := loadSnapshots(path)
	if err != nil {
		t.Fatal(err)
	}
	// minutes: 50→0, 100→1, 130→2 → 3 rows after dedupe (all different minutes)
	if len(snaps) != 3 {
		t.Fatalf("%+v", snaps)
	}
	if hourUTC(0) != 0 || hourUTC(3600) != 1 {
		t.Fatalf("hourUTC")
	}
}

func TestContinued(t *testing.T) {
	if !continued("up", 0.1) || continued("up", -0.1) || continued("up", 0) {
		t.Fatal("up")
	}
	if !continued("down", -0.1) || continued("down", 0.1) {
		t.Fatal("down")
	}
}

func TestReportLiftSmoke(t *testing.T) {
	reportLift(nil, nil, nil, 0.08)
	up := 0.3
	outcomes := []outcomeRecord{{EventTS: 10, Direction: "up", MedianReturn5m: &up, ShadowMode: false}}
	events := []eventRecord{{Timestamp: 10, MedianReturn60s: 0.4, ShadowMode: false}}
	reportLift(outcomes, events, nil, 0.08)
	snaps := []snapshotRecord{
		{Timestamp: 0, DataOK: true, MedianReturn60s: 0.2, MedianReturn300s: 0.05},
		{Timestamp: 300, DataOK: true, MedianReturn60s: 0.1, MedianReturn300s: 0.25},
		// far from alert at 10 so not excluded (exclude is [10-300,10+900]=[-290,910] — oh 0 and 300 ARE in window for event 10!
	}
	// Use event far away so baseline has samples.
	outcomes = []outcomeRecord{{EventTS: 10000, Direction: "up", MedianReturn5m: &up}}
	events = []eventRecord{{Timestamp: 10000, MedianReturn60s: 0.4}}
	reportLift(outcomes, events, snaps, 0.08)
}

func TestMagBucket(t *testing.T) {
	if magBucket(0.1) != 0 || magBucket(0.25) != 1 || magBucket(0.4) != 2 || magBucket(0.6) != 3 {
		t.Fatal("buckets")
	}
}
