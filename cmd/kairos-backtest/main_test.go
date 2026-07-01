package main

import "testing"

func TestSplitSymbols(t *testing.T) {
	got := splitSymbols(" BTC/USDT , ETH/USDT ")
	if len(got) != 2 || got[0] != "BTC/USDT" || got[1] != "ETH/USDT" {
		t.Fatalf("got %v", got)
	}
}
