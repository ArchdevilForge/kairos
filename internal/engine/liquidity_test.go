package engine

import (
	"testing"

	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestLiquidityWeight_MarketCapTiers(t *testing.T) {
	p := NewPipeline(&types.Config{
		AlertPolicy: types.AlertPolicyConfig{
			LiquidityWeight: types.LiquidityWeightConfig{
				Enabled:   true,
				MinWeight: 0.3,
			},
		},
	}, nil)

	p.liqMu.Lock()
	p.marketCapByCoin = map[string]float64{
		"BTC":  1_700_000_000_000,
		"DOGE": 25_000_000_000,
		"MEME": 500_000_000,
	}
	p.marketCapRefUSD = 1_700_000_000_000
	p.liqMu.Unlock()

	if w := p.liquidityWeight("BTC/USDT:USDT"); w != 1 {
		t.Fatalf("BTC weight: %v", w)
	}
	memeW := p.liquidityWeight("MEME/USDT:USDT")
	if memeW >= 0.5 || memeW < 0.3 {
		t.Fatalf("meme weight: %v want ~0.3-0.5", memeW)
	}
}

func TestPassesAlertPolicy_LiquidityDownweight(t *testing.T) {
	p := NewPipeline(&types.Config{
		AlertPolicy: types.AlertPolicyConfig{
			Enabled:           true,
			MinSeverity:       "HIGH",
			MinPriceChangePct: 1.0,
			LiquidityWeight: types.LiquidityWeightConfig{
				Enabled:   true,
				MinWeight: 0.3,
			},
		},
	}, nil)
	p.liqMu.Lock()
	p.marketCapByCoin = map[string]float64{
		"BTC":  1_000_000_000_000,
		"DOGE": 25_000_000_000,
		"MEME": 250_000_000_000,
	}
	p.marketCapRefUSD = 1_000_000_000_000
	p.liqMu.Unlock()

	btc := types.AnomalyEvent{
		Symbol: "BTC/USDT:USDT", EventType: "price_velocity",
		Severity: types.SeverityHigh, Data: map[string]any{"change_pct": 1.5},
	}
	if !p.passesAlertPolicy(btc) {
		t.Fatal("BTC high should pass")
	}

	meme := types.AnomalyEvent{
		Symbol: "DOGE/USDT:USDT", EventType: "price_velocity",
		Severity: types.SeverityHigh, Data: map[string]any{"change_pct": 1.5},
	}
	if p.passesAlertPolicy(meme) {
		t.Fatal("low-cap DOGE should be filtered at same severity/threshold")
	}

	memeStrong := types.AnomalyEvent{
		Symbol: "MEME/USDT:USDT", EventType: "price_velocity",
		Severity: types.SeverityHigh, Data: map[string]any{"change_pct": 2.0},
	}
	p.minSeverityRank = severityRank("MEDIUM")
	if !p.passesAlertPolicy(memeStrong) {
		t.Fatal("mid-cap symbol with strong move should pass MEDIUM gate")
	}
}

func TestLiquidityStrictness(t *testing.T) {
	if liquidityStrictness(1) != 1 {
		t.Fatal("full weight")
	}
	if liquidityStrictness(0.5) != 2 {
		t.Fatal("half weight doubles strictness")
	}
}
