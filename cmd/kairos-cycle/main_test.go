package main

import "testing"

func TestDemoCycleRuns(t *testing.T) {
	if err := run([]string{"demo"}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"--json", "demo"}); err != nil {
		t.Fatal(err)
	}
}
