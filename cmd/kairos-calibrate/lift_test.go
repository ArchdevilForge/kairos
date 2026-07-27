package main

import (
	"os"
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
		{Direction: "up"},                        // missing 5m
		{Direction: "up", MedianReturn5m: &flat}, // flat is not >0 → not continued
	}
	n, cont := alertContinuation(outs)
	if n != 4 || cont != 2 {
		t.Fatalf("n=%d cont=%d want 4/2", n, cont)
	}
}

func TestRandomContinuationAndLift(t *testing.T) {
	// t=0: med60=+0.2 (up). t=300: med300=+0.3 → continued
	// t=60: med60=-0.2 (down). t=360: med300=+0.1 → not continued
	// t=120: med60=+0.01 below noise → skip
	snaps := []snapshotRecord{
		{Timestamp: 0, DataOK: true, MedianReturn60s: 0.20, MedianReturn300s: 0.05},
		{Timestamp: 60, DataOK: true, MedianReturn60s: -0.20, MedianReturn300s: -0.05},
		{Timestamp: 120, DataOK: true, MedianReturn60s: 0.01, MedianReturn300s: 0.02},
		{Timestamp: 300, DataOK: true, MedianReturn60s: 0.10, MedianReturn300s: 0.30},
		{Timestamp: 360, DataOK: true, MedianReturn60s: 0.05, MedianReturn300s: 0.10},
	}
	n, cont := randomContinuation(snaps, 0.08)
	if n != 2 {
		t.Fatalf("random n=%d want 2", n)
	}
	if cont != 1 {
		t.Fatalf("random cont=%d want 1", cont)
	}

	up := 0.4
	outcomes := []outcomeRecord{
		{EventTS: 10, Direction: "up", MedianReturn5m: &up},
	}
	// hour 0 pool: from snaps at 0 and 60 → 1/2 continued; alert continues → lift = 1 / 0.5 = 2
	lift, used := sameHourLift(outcomes, snaps, 0.08)
	if used != 1 {
		t.Fatalf("used=%d want 1", used)
	}
	if lift < 1.9 || lift > 2.1 {
		t.Fatalf("lift=%v want ~2", lift)
	}
}

func TestForwardMedian300(t *testing.T) {
	snaps := []snapshotRecord{
		{Timestamp: 0, DataOK: true, MedianReturn300s: 0.1},
		{Timestamp: 290, DataOK: true, MedianReturn300s: 0.5},
		{Timestamp: 400, DataOK: true, MedianReturn300s: 0.9},
	}
	v, ok := forwardMedian300(snaps, 0, 300, 45)
	if !ok || v != 0.5 {
		t.Fatalf("got %v %v want 0.5 true", v, ok)
	}
	if _, ok := forwardMedian300(snaps, 0, 900, 45); ok {
		t.Fatal("expected miss far target")
	}
}

func TestLoadSnapshotsAndHourUTC(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/snaps.jsonl"
	body := "{\"timestamp\":100,\"data_ok\":true,\"median_return_60s\":0.2,\"median_return_300s\":0.1}\n" +
		"{\"timestamp\":50,\"data_ok\":false,\"median_return_60s\":0.0,\"median_return_300s\":0.0}\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	snaps, err := loadSnapshots(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 2 || snaps[0].Timestamp != 50 || snaps[1].Timestamp != 100 {
		t.Fatalf("sorted load: %+v", snaps)
	}
	// Unix 0 is 1970-01-01 00:00 UTC → hour 0; +3600 → hour 1.
	if hourUTC(0) != 0 || hourUTC(3600) != 1 {
		t.Fatalf("hourUTC 0=%d 3600=%d", hourUTC(0), hourUTC(3600))
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
	// Exercises print paths (no assertions beyond non-panic).
	reportLift(nil, nil, 0.08)
	up := 0.3
	outcomes := []outcomeRecord{{EventTS: 10, Direction: "up", MedianReturn5m: &up}}
	reportLift(outcomes, nil, 0.08)
	snaps := []snapshotRecord{
		{Timestamp: 0, DataOK: true, MedianReturn60s: 0.2, MedianReturn300s: 0.05},
		{Timestamp: 300, DataOK: true, MedianReturn60s: 0.1, MedianReturn300s: 0.25},
	}
	reportLift(outcomes, snaps, 0.08)
}
