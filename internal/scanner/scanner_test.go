package scanner

import (
	"testing"

	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestTrend(t *testing.T) {
	up := &types.OHLCVArrays{Closes: repeatFloat(100, 25)}
	up.Closes[len(up.Closes)-1] = 110
	if Trend(up) != "up" {
		t.Fatalf("expected up, got %s", Trend(up))
	}

	down := &types.OHLCVArrays{Closes: repeatFloat(100, 25)}
	down.Closes[len(down.Closes)-1] = 90
	if Trend(down) != "down" {
		t.Fatalf("expected down")
	}

	short := &types.OHLCVArrays{Closes: []float64{1, 2, 3}}
	if Trend(short) != "sideways" {
		t.Fatal("short series should be sideways")
	}
}

func TestVolumeConfirmed(t *testing.T) {
	vols := repeatFloat(1000, 20)
	vols[len(vols)-1] = 1500
	ohlcv := &types.OHLCVArrays{Volumes: vols}
	if !VolumeConfirmed(ohlcv) {
		t.Fatal("expected volume confirmed")
	}
	vols[len(vols)-1] = 1000
	if VolumeConfirmed(ohlcv) {
		t.Fatal("expected not confirmed")
	}
}

func TestComputeRiskBounds_ShortZeroTargets(t *testing.T) {
	s := NewMarketScanner(&types.Config{})
	risk := s.computeRiskBounds(
		types.DirectionShort,
		"ALT/USDT:USDT",
		map[string]any{"high": 2.0, "low": 1.0, "height": 1.0},
		0.99,
		"winter",
		"down",
	)
	if len(risk.Targets) != 0 {
		t.Fatalf("targets: %v", risk.Targets)
	}
	if risk.RiskReward != 0 {
		t.Fatalf("risk_reward: %v", risk.RiskReward)
	}
	if risk.RiskRewardTarget != nil {
		t.Fatal("expected nil risk_reward_target")
	}
}

func TestDedupeStrings(t *testing.T) {
	got := DedupeStrings([]string{"a", "b", "a", "c", "b"})
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("got %v", got)
	}
}

func TestOHLCVToArrays(t *testing.T) {
	candles := []types.Candle{
		{Timestamp: 1000, Open: 1, High: 2, Low: 0.5, Close: 1.5, Volume: 10},
	}
	arr := OHLCVToArrays(candles)
	if len(arr.Closes) != 1 || arr.Closes[0] != 1.5 {
		t.Fatalf("closes: %v", arr.Closes)
	}
}

func repeatFloat(v float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}
	return out
}
