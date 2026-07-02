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

func TestComputeRiskBounds_InverseCycleMultiplier(t *testing.T) {
	cfg := &types.Config{
		Risk: types.RiskConfig{
			MaxPositionPct:                 map[string]float64{"altcoin": 33.0},
			MaxLeverage:                      map[string]float64{"altcoin": 5.0},
			InverseCyclePositionMultiplier:   0.5,
			ShortPositionMultiplier:          0.75,
			WeakCyclePositionMultiplier:      0.5,
		},
	}
	s := NewMarketScanner(cfg)
	structure := map[string]any{"high": 110, "low": 100, "height": 10}

	summerLong := s.computeRiskBounds(types.DirectionLong, "ETH/USDT:USDT", structure, 111, "summer", "up")
	winterLong := s.computeRiskBounds(types.DirectionLong, "ETH/USDT:USDT", structure, 111, "winter", "down")
	if winterLong.MaxPositionPct >= summerLong.MaxPositionPct {
		t.Fatalf("inverse cycle should reduce position: summer=%v winter=%v", summerLong.MaxPositionPct, winterLong.MaxPositionPct)
	}
}

func TestSortSetupsByCandidateOrder(t *testing.T) {
	setups := []types.Setup{
		{Symbol: "ETH/USDT:USDT"},
		{Symbol: "BTC/USDT:USDT"},
	}
	candidates := []types.Candidate{
		{Symbol: "BTC/USDT:USDT"},
		{Symbol: "ETH/USDT:USDT"},
	}
	sortSetupsByCandidateOrder(setups, candidates)
	if setups[0].Symbol != "BTC/USDT:USDT" || setups[1].Symbol != "ETH/USDT:USDT" {
		t.Fatalf("order: %+v", setups)
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

func TestDetectSupportAtBoxLow(t *testing.T) {
	lows := []float64{10, 9.5, 9.2, 9.0, 9.1, 9.0, 9.05, 9.02, 9.01, 9.0, 9.02, 9.05}
	closes := []float64{10, 9.6, 9.3, 9.1, 9.2, 9.05, 9.1, 9.08, 9.06, 9.05, 9.08, 9.12}
	ohlcv := &types.OHLCVArrays{Lows: lows, Closes: closes}
	triggered, near := DetectSupportAtBoxLow(ohlcv, 9.0)
	if !near {
		t.Fatal("expected near support")
	}
	if !triggered {
		t.Fatal("expected double-bottom trigger")
	}
}

func TestApplyStrategyActionGate(t *testing.T) {
	s := NewMarketScanner(&types.Config{})

	state, w := s.ApplyStrategyActionGate(string(types.ActionStateTradeCandidate), types.DirectionLong, "winter", "down")
	if state != string(types.ActionStatePrepare) || len(w) == 0 {
		t.Fatalf("winter long: state=%s warnings=%v", state, w)
	}

	state, w = s.ApplyStrategyActionGate(string(types.ActionStateTradeCandidate), types.DirectionShort, "winter", "down")
	if state != string(types.ActionStateTradeCandidate) || len(w) != 0 {
		t.Fatalf("winter short should pass: state=%s warnings=%v", state, w)
	}

	state, w = s.ApplyStrategyActionGate(string(types.ActionStateTradeCandidate), types.DirectionLong, "autumn", "sideways")
	if state != string(types.ActionStatePrepare) || len(w) == 0 {
		t.Fatalf("autumn non-resonance long: state=%s warnings=%v", state, w)
	}

	state, _ = s.ApplyStrategyActionGate(string(types.ActionStateWatch), types.DirectionLong, "winter", "down")
	if state != string(types.ActionStateWatch) {
		t.Fatalf("non-candidate states unchanged: %s", state)
	}
}

func repeatFloat(v float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}
	return out
}
