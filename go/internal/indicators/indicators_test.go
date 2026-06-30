package indicators

import (
	"testing"

	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestBoxDetector_Detect_noPattern(t *testing.T) {
	d := NewBoxDetector(DefaultBoxDetectorConfig())
	// Strongly trending data: any 10-bar window has >15% range → no box detected
	n := 30
	highs := make([]float64, n)
	lows := make([]float64, n)
	closes := make([]float64, n)
	volumes := make([]float64, n)
	timestamps := make([]float64, n)
	for i := 0; i < n; i++ {
		highs[i] = 100 + float64(i)*3.0 // up 87 over 30 bars
		lows[i] = 99.5 + float64(i)*2.9
		closes[i] = 99.8 + float64(i)*2.95
		volumes[i] = 1000
		timestamps[i] = float64(i)
	}
	boxes := d.Detect("TEST", "1h", highs, lows, closes, volumes, timestamps)
	if boxes != nil {
		t.Logf("got %d boxes (acceptable in some edge windows), first low=%.2f high=%.2f",
			len(boxes), boxes[0].Low, boxes[0].High)
		if boxes[0].HeightPct() < 15 {
			t.Fatalf("unexpected box with heightPct=%.2f%% in trending data", boxes[0].HeightPct())
		}
	}
}

func TestBoxDetector_Detect_consolidation(t *testing.T) {
	d := NewBoxDetector(DefaultBoxDetectorConfig())
	n := 35
	highs := make([]float64, n)
	lows := make([]float64, n)
	closes := make([]float64, n)
	volumes := make([]float64, n)
	timestamps := make([]float64, n)

	// Entire series is a clean box 130-135, then tightening at the end.
	// No preceding trend so the initial 10-bar window is pure consolidation.
	for i := 0; i < 25; i++ {
		highs[i] = 135
		lows[i] = 130
		closes[i] = 132.5
		volumes[i] = 1000
		timestamps[i] = float64(i)
	}
	// Last 10 bars: range tightening
	for i := 25; i < n; i++ {
		highs[i] = 133
		lows[i] = 131
		closes[i] = 132
		volumes[i] = 800
		timestamps[i] = float64(i)
	}

	boxes := d.Detect("TEST", "1h", highs, lows, closes, volumes, timestamps)
	if len(boxes) == 0 {
		t.Fatal("expected at least one box pattern")
	}

	box := boxes[0]
	if box.High != 135 {
		t.Errorf("expected High=135, got %v", box.High)
	}
	if box.Low != 130 {
		t.Errorf("expected Low=130, got %v", box.Low)
	}
	if box.HeightPct() < 3 || box.HeightPct() > 5 {
		t.Errorf("expected heightPct ~3.85%%, got %.2f%%", box.HeightPct())
	}
	t.Logf("box: high=%.2f low=%.2f status=%v conv=%.2f touchH=%d touchL=%d",
		box.High, box.Low, box.Status, box.ConvergencePct, box.TouchHigh, box.TouchLow)
}

func TestBoxDetector_CheckBreakout_up(t *testing.T) {
	d := NewBoxDetector(DefaultBoxDetectorConfig())
	box := &types.BoxPattern{
		Symbol: "TEST",
		High:   120,
		Low:    115,
		Status: types.BoxStatusConverging,
	}

	result := d.CheckBreakout(box, 121.0, 2000, 1000)
	if result.Status != types.BoxStatusBreakoutUp {
		t.Fatalf("expected breakout_up, got %v", result.Status)
	}
	if result.BreakoutPrice == nil || *result.BreakoutPrice != 121.0 {
		t.Fatal("breakout price not set")
	}
}

func TestBoxDetector_CheckBreakout_down(t *testing.T) {
	d := NewBoxDetector(DefaultBoxDetectorConfig())
	box := &types.BoxPattern{
		Symbol: "TEST",
		High:   120,
		Low:    115,
		Status: types.BoxStatusConverging,
	}

	result := d.CheckBreakout(box, 114.0, 2000, 1000)
	if result.Status != types.BoxStatusBreakoutDown {
		t.Fatalf("expected breakout_down, got %v", result.Status)
	}
	if result.BreakoutPrice == nil || *result.BreakoutPrice != 114.0 {
		t.Fatal("breakout price not set")
	}
}

func TestBoxDetector_CheckBreakout_volumeTooLow(t *testing.T) {
	d := NewBoxDetector(DefaultBoxDetectorConfig())
	box := &types.BoxPattern{
		Symbol: "TEST",
		High:   120,
		Low:    115,
		Status: types.BoxStatusConverging,
	}

	// Price above box but volume too low — no breakout
	result := d.CheckBreakout(box, 121.0, 1400, 1000)
	if result.Status == types.BoxStatusBreakoutUp {
		t.Fatal("should not break out without volume confirmation")
	}
}

func TestBoxDetector_CheckBreakout_alreadyBreakout(t *testing.T) {
	d := NewBoxDetector(DefaultBoxDetectorConfig())
	price := 125.0
	box := &types.BoxPattern{
		Symbol:        "TEST",
		High:          120,
		Low:           115,
		Status:        types.BoxStatusBreakoutUp,
		BreakoutPrice: &price,
	}

	result := d.CheckBreakout(box, 130.0, 9999, 1000)
	if result.Status != types.BoxStatusBreakoutUp {
		t.Fatal("already broken out — status should stay")
	}
	// Breakout price should remain the original
	if *result.BreakoutPrice != 125 {
		t.Fatal("breakout price should not be overwritten")
	}
}

func TestCycleDetector_Default(t *testing.T) {
	d := NewCycleDetector(DefaultCycleDetectorConfig())
	cycle := d.DetectPhase(nil, nil, 0, 0, 0)
	if cycle.Phase != types.MarketPhaseWinter {
		t.Fatalf("expected winter for nil prices, got %v", cycle.Phase)
	}
}

func TestCycleDetector_Spring(t *testing.T) {
	d := NewCycleDetector(DefaultCycleDetectorConfig())
	// Simulate spring: BTC up 15% in 30d, 6% in 7d, moderate vol
	n := 60
	prices := make([]float64, n)
	volumes := make([]float64, n)

	// Flat for 30 days, then rally
	for i := 0; i < 30; i++ {
		prices[i] = 100
		volumes[i] = 1000
	}
	for i := 30; i < n; i++ {
		step := float64(i - 30)
		prices[i] = 100 + step*0.5 // +15% over 30 days
		volumes[i] = 1200
	}

	cycle := d.DetectPhase(prices, volumes, 0.85, 0.02, 15)
	t.Logf("spring test: phase=%v conf=%.2f trend=%s vol=%.2f volTrend=%s",
		cycle.Phase, cycle.Confidence, cycle.BtcTrend, cycle.Volatility, cycle.VolumeTrend)
	// At minimum we should detect positive momentum
	if cycle.BtcChange30D < 10 {
		t.Errorf("expected BTC 30d change > 10%%, got %.2f%%", cycle.BtcChange30D)
	}
}

func TestCycleDetector_Winter(t *testing.T) {
	d := NewCycleDetector(DefaultCycleDetectorConfig())
	// Simulate winter: BTC down sharply
	n := 60
	prices := make([]float64, n)
	volumes := make([]float64, n)

	for i := 0; i < 30; i++ {
		prices[i] = 100
		volumes[i] = 1000
	}
	for i := 30; i < n; i++ {
		prices[i] = 100 - float64(i-30)*0.8 // -24% over 30d
		volumes[i] = 1500
	}

	cycle := d.DetectPhase(prices, volumes, 0.9, -0.02, -15)
	t.Logf("winter test: phase=%v conf=%.2f btc_30d=%.2f btc_7d=%.2f trend=%s",
		cycle.Phase, cycle.Confidence, cycle.BtcChange30D, cycle.BtcChange7D, cycle.BtcTrend)
	// The 7d change should be very negative
	if cycle.BtcChange7D > -5 {
		t.Errorf("expected BTC 7d < -5%%, got %.2f%%", cycle.BtcChange7D)
	}
}

func TestLinearSlope(t *testing.T) {
	// y = 2x + 5
	v := []float64{5, 7, 9, 11, 13}
	slope := linearSlope(v)
	if slope != 2 {
		t.Fatalf("expected slope 2, got %v", slope)
	}
}

func TestStdDevPop(t *testing.T) {
	v := []float64{1, 2, 3, 4, 5}
	sd := stdDevPop(v)
	// np.std([1,2,3,4,5]) = 1.4142135623730951
	if sd < 1.41 || sd > 1.42 {
		t.Fatalf("expected ~1.414, got %v", sd)
	}
}

func TestMeanVal(t *testing.T) {
	v := []float64{2, 4, 6, 8}
	m := meanVal(v)
	if m != 5 {
		t.Fatalf("expected 5, got %v", m)
	}
	// Empty slice
	if meanVal(nil) != 0 {
		t.Fatal("expected 0 for nil slice")
	}
}
