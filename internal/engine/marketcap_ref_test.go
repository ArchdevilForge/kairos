package engine

import "testing"

func TestMarketCapReference(t *testing.T) {
	caps := map[string]float64{
		"BTC":  1_700_000_000_000,
		"ETH":  400_000_000_000,
		"GOLD": 29_000_000_000_000,
		"DOGE": 30_000_000_000,
	}
	symbols := map[string][]string{
		"okx": {"DOGE/USDT:USDT", "WLD/USDT:USDT"},
	}
	ref, uni := marketCapReference(caps, symbols)
	if ref != 1_700_000_000_000 {
		t.Fatalf("ref=%v want BTC cap", ref)
	}
	if uni != 30_000_000_000 {
		t.Fatalf("universeMax=%v want DOGE", uni)
	}

	capsNoBTC := map[string]float64{"ETH": 400_000_000_000, "GOLD": 29_000_000_000_000}
	ref, _ = marketCapReference(capsNoBTC, nil)
	if ref != 400_000_000_000 {
		t.Fatalf("ref without BTC=%v want ETH", ref)
	}
}
