package scanner

import (
	"testing"

	"github.com/ArchdevilForge/kairos/internal/indicators"
	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestMakeSignalEnvelope(t *testing.T) {
	env := makeSignalEnvelope(true, map[string]any{"x": 1}, "BTC/USDT:USDT", nil, nil, nil, nil)
	if !env.Success || env.Symbol == nil || *env.Symbol != "BTC/USDT:USDT" {
		t.Fatalf("envelope: %+v", env)
	}
}

func TestDetectSupportAtBoxLow_DoubleBottom(t *testing.T) {
	lows := make([]float64, 20)
	closes := make([]float64, 20)
	for i := range lows {
		lows[i] = 100
		closes[i] = 101
	}
	lows[len(lows)-1] = 100
	closes[len(closes)-2] = 100
	closes[len(closes)-1] = 102
	ohlcv := &types.OHLCVArrays{Lows: lows, Closes: closes}

	triggered, near := DetectSupportAtBoxLow(ohlcv, 100)
	if !near {
		t.Fatal("expected near support")
	}
	if !triggered {
		t.Fatal("expected double bottom trigger")
	}
	if triggered, near := DetectSupportAtBoxLow(nil, 100); triggered || near {
		t.Fatal("nil ohlcv")
	}
}

func TestCycleDetectorConfig(t *testing.T) {
	cfg := cycleDetectorConfig(types.ScoringConfig{
		CycleDetector: types.CycleDetectorYAMLConfig{
			SpringBtcChangeMin: 11, SummerBtcChangeMin: 31, AutumnBtcChangeMax: 49,
			WinterBtcChangeMax: -9, HighVolatilityThreshold: 6, LowVolatilityThreshold: 3,
			HighFundingThreshold: 0.06, LowFundingThreshold: -0.02,
		},
	})
	if cfg.SpringBtcChangeMin != 11 || cfg.HighFundingThreshold != 0.06 {
		t.Fatalf("cfg: %+v", cfg)
	}
	_ = indicators.DefaultCycleDetectorConfig()
}

func TestOHLCVToArraysAndCycleToDict(t *testing.T) {
	if OHLCVToArrays(nil) != nil {
		t.Fatal("empty candles")
	}
	arr := OHLCVToArrays([]types.Candle{{Timestamp: 1, Open: 1, High: 2, Low: 0.5, Close: 1.5, Volume: 10}})
	if len(arr.Closes) != 1 {
		t.Fatal("convert")
	}
	d := CycleToDict(types.MarketCycle{Phase: types.MarketPhaseSummer, Confidence: 0.8, BtcTrend: "up"})
	if d["phase"] != string(types.MarketPhaseSummer) {
		t.Fatal("cycle dict")
	}
}

func TestDedupeStringsAndFingerprint(t *testing.T) {
	got := DedupeStrings([]string{"a", "b", "a", "c"})
	if len(got) != 3 {
		t.Fatalf("dedupe: %v", got)
	}
	stop := 9.5
	fp := fingerprint("BTC/USDT:USDT", "long", "box_breakout", map[string]any{
		"timeframe": "4h", "high": 100.0, "low": 90.0,
	}, types.RiskBounds{
		EntryZone: []float64{100, 101}, StructuralStop: &stop, Targets: []float64{110},
	})
	if len(fp) != 24 {
		t.Fatalf("fingerprint len: %d", len(fp))
	}
}

func TestNumericAndMapHelpers(t *testing.T) {
	if meanVal([]float64{1, 3}) != 2 || maxVal([]float64{1, 3}) != 3 || minVal([]float64{1, 3}) != 1 {
		t.Fatal("numeric helpers")
	}
	if getWeight(nil, "x", 1.5) != 1.5 {
		t.Fatal("getWeight nil")
	}
	if getMapString(map[string]any{"k": "v"}, "k") != "v" {
		t.Fatal("getMapString")
	}
	if getMapFloat64(map[string]any{"n": 2}, "n") != 2 {
		t.Fatal("getMapFloat64")
	}
	if !getMapBool(map[string]any{"b": true}, "b") {
		t.Fatal("getMapBool")
	}
}

func TestDetermineActionStateAndThresholds(t *testing.T) {
	s := NewMarketScanner(testScannerConfig())
	state := s.determineActionState(8.0, 7.5, 2.5, 2.0, true, false, 3.0)
	if state != "trade_candidate" {
		t.Fatalf("state: %q", state)
	}
	if s.threshold(types.DirectionLong, "summer", "up") <= 0 {
		t.Fatal("threshold")
	}
	if rr := s.requiredRR(types.DirectionShort, "summer", "up"); rr < 0 {
		t.Fatalf("requiredRR: %v", rr)
	}
	if s.cycleComponent(types.DirectionLong, "summer") <= 0 {
		t.Fatal("cycle component")
	}
}
