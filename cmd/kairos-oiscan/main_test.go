package main

import "testing"

func TestClassifyQuadrant(t *testing.T) {
	cases := []struct {
		oi, price float64
		want      string
	}{
		{+15, +8, "new_long"},
		{+15, -8, "new_short"},
		{-15, +8, "short_cover"},
		{-15, -8, "long_exit"},
		{+0.5, +0.2, "long_exit"}, // below thresholds → default (flat)
	}
	for _, c := range cases {
		got, _ := classifyQuadrant(c.oi, c.price)
		if got != c.want {
			t.Errorf("classifyQuadrant(%v, %v) = %q, want %q", c.oi, c.price, got, c.want)
		}
	}
}

func TestSortRowsByH1OI(t *testing.T) {
	rows := []marketRow{
		{Symbol: "A", H1OIChangePercent: 5},
		{Symbol: "B", H1OIChangePercent: -40},
		{Symbol: "C", H1OIChangePercent: 12},
	}
	sortRowsByH1OI(rows)
	if rows[0].Symbol != "B" || rows[2].Symbol != "A" {
		t.Fatalf("unexpected order: %+v", rows)
	}
}
