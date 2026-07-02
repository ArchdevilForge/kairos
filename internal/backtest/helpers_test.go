package backtest

import (
	"context"
	"math"
	"testing"

	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestDetectBreakoutAndEntry(t *testing.T) {
	box := &types.BoxPattern{High: 100, Low: 90}
	vols := repeatFloat(100, 25)
	vols[24] = 500 // spike on last bar

	if dir := detectBreakout(box, 101, 99, vols, 24); dir != types.DirectionLong {
		t.Fatalf("long breakout: %q", dir)
	}
	if dir := detectBreakout(box, 99, 89, vols, 24); dir != types.DirectionShort {
		t.Fatalf("short breakout: %q", dir)
	}
	if detectBreakout(box, 100, 95, repeatFloat(100, 25), 24) != "" {
		t.Fatal("no breakout without volume")
	}

	entry, stop, target := calcEntryPrice(100, 90, types.DirectionLong)
	if entry <= 100 || stop >= 90 || target <= entry {
		t.Fatalf("long entry/stop/target: %v %v %v", entry, stop, target)
	}
	entry, stop, target = calcEntryPrice(100, 90, types.DirectionShort)
	if entry >= 90 || stop <= 100 || target >= entry {
		t.Fatalf("short entry/stop/target: %v %v %v", entry, stop, target)
	}
}

func TestCheckExit_LongAndShort(t *testing.T) {
	pos := &position{
		direction:  types.DirectionLong,
		entryPrice: 100,
		stop:       95,
		target:     110,
		entryTime:  "2024-01-01T00:00:00Z",
		entryTimeMs: 0,
	}
	if tr := checkExit(pos, 105, 96, "2024-01-01T04:00:00Z", 1, 0.12); tr != nil {
		t.Fatal("no exit inside range")
	}
	if tr := checkExit(pos, 111, 100, "2024-01-01T08:00:00Z", 2, 0.12); tr == nil || tr.ExitReason != string(ExitReasonTakeProfit) {
		t.Fatalf("tp: %+v", tr)
	}
	if tr := checkExit(pos, 100, 94, "2024-01-01T12:00:00Z", 3, 0.12); tr == nil || tr.ExitReason != string(ExitReasonStopLoss) {
		t.Fatalf("sl: %+v", tr)
	}

	short := &position{
		direction: types.DirectionShort, entryPrice: 100, stop: 105, target: 90,
		entryTime: "2024-01-01T00:00:00Z",
	}
	if tr := checkExit(short, 106, 100, "2024-01-02T00:00:00Z", 4, 0); tr == nil || tr.ExitReason != string(ExitReasonStopLoss) {
		t.Fatalf("short sl: %+v", tr)
	}
	if tr := checkExit(short, 100, 89, "2024-01-03T00:00:00Z", 5, 0); tr == nil || tr.ExitReason != string(ExitReasonTakeProfit) {
		t.Fatalf("short tp: %+v", tr)
	}
}

func TestCycleSupports(t *testing.T) {
	if !cycleSupports(types.MarketPhaseSummer, types.DirectionLong) {
		t.Fatal("summer long")
	}
	if cycleSupports(types.MarketPhaseWinter, types.DirectionLong) {
		t.Fatal("winter long blocked")
	}
	if !cycleSupports(types.MarketPhaseAutumn, types.DirectionShort) {
		t.Fatal("autumn both")
	}
}

func TestDefaultRunOptionAndHelpers(t *testing.T) {
	opt := defaultRunOption()
	if opt.Timeframe != "4h" || opt.FeePct != 0.04 {
		t.Fatalf("defaults: %+v", opt)
	}
	if mean([]float64{1, 2, 3}) != 2 || sum([]float64{1, 2}) != 3 {
		t.Fatal("mean/sum")
	}
	if abs(-1) != 1 || max(1, 2) != 2 || maxInt(1, 2) != 2 || minInt(1, 2) != 1 {
		t.Fatal("math helpers")
	}
}

func TestCandlesToArraysAndParseDate(t *testing.T) {
	candles := []types.Candle{
		{Timestamp: 100, Open: 1, High: 2, Low: 0.5, Close: 1.5, Volume: 10},
		{Timestamp: 200, Open: 1.5, High: 2.5, Low: 1, Close: 2, Volume: 20},
	}
	arr := candlesToArrays(candles)
	if len(arr.Closes) != 2 || arr.Closes[1] != 2 {
		t.Fatalf("arrays: %+v", arr)
	}
	ms := parseDate("2024-01-01")
	if ms <= 0 {
		t.Fatal("parseDate")
	}
}

func TestRun_InsufficientData(t *testing.T) {
	endMs := parseDate("2024-03-01")
	ex := &stubExchange{
		pages: map[int64][]types.Candle{
			endMs: {{Timestamp: endMs / 1000, Close: 1}},
		},
	}
	runner := New(ex)
	_, err := runner.Run(context.Background(), "BTC/USDT", "2024-01-01", "2024-03-01", RunOption{})
	if err == nil {
		t.Fatal("expected insufficient data error")
	}
}

func TestRun_FlatSeries(t *testing.T) {
	endMs := parseDate("2024-03-01")
	candles := make([]types.Candle, 60)
	baseTs := parseDate("2024-01-01") / 1000
	for i := range candles {
		price := 100.0
		candles[i] = types.Candle{
			Timestamp: baseTs + int64(i)*3600*4,
			Open: price, High: price + 0.5, Low: price - 0.5, Close: price,
			Volume: 1000,
		}
	}
	ex := &stubExchange{pages: map[int64][]types.Candle{endMs: candles}}
	runner := New(ex)
	res, err := runner.Run(context.Background(), "BTC/USDT", "2024-01-01", "2024-03-01", RunOption{})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.Summary.TotalTrades < 0 {
		t.Fatalf("result: %+v", res)
	}
	if math.IsNaN(res.Summary.SharpeRatio) {
		t.Fatal("sharpe should not be NaN")
	}
}

func repeatFloat(v float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}
	return out
}
