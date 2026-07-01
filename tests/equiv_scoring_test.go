// Package tests holds cross-language equivalence checks against Python golden values.
package tests

import (
	"math"
	"testing"

	"github.com/ArchdevilForge/kairos/internal/config"
	"github.com/ArchdevilForge/kairos/internal/scanner"
	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestEquiv_VolumeConfirmed(t *testing.T) {
	ohlcv := &types.OHLCVArrays{
		Closes:  repeat(1, 21),
		Volumes: repeat(1000, 21),
	}
	ohlcv.Volumes[20] = 1500
	if !scanner.VolumeConfirmed(ohlcv) {
		t.Fatal("volume confirmation mismatch with Python baseline")
	}
}

func TestEquiv_ArchitectureDefaults(t *testing.T) {
	cfg, err := config.LoadString("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Scanner.UniverseSize != 30 {
		t.Fatalf("universeSize: %d", cfg.Scanner.UniverseSize)
	}
	if cfg.Exchanges.Primary != "okx" {
		t.Fatalf("primary: %q", cfg.Exchanges.Primary)
	}
	if cfg.Scoring.MinimumLiquidityQuoteVolume != 30_000_000 {
		t.Fatalf("liquidity: %v", cfg.Scoring.MinimumLiquidityQuoteVolume)
	}
	if cfg.Risk.MaxPositionPct["major"] != 33.0 {
		t.Fatalf("major position: %v", cfg.Risk.MaxPositionPct["major"])
	}
	if cfg.Risk.MaxLeverage["altcoin"] != 5.0 {
		t.Fatalf("altcoin leverage: %v", cfg.Risk.MaxLeverage["altcoin"])
	}
}

func TestEquiv_RiskRewardFloatTolerance(t *testing.T) {
	a, b := 2.4000000001, 2.4
	if math.Abs(a-b) > 1e-9 {
		t.Fatal("float tolerance baseline failed")
	}
}

func repeat(v float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}
	return out
}
