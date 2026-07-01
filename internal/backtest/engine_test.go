package backtest

import (
	"math"
	"testing"
)

func TestComputeSummary_Empty(t *testing.T) {
	s := computeSummary(nil, "2024-01-01", "2024-06-01", 100)
	if s.TotalTrades != 0 {
		t.Fatalf("expected 0 trades, got %d", s.TotalTrades)
	}
}

func TestComputeSummary_Basic(t *testing.T) {
	trades := []Trade{
		{Symbol: "BTC/USDT", Direction: "long", PnlPct: 2.0, HoldingHours: 12},
		{Symbol: "BTC/USDT", Direction: "long", PnlPct: -1.0, HoldingHours: 8},
		{Symbol: "BTC/USDT", Direction: "short", PnlPct: 3.0, HoldingHours: 6},
		{Symbol: "BTC/USDT", Direction: "short", PnlPct: -0.5, HoldingHours: 4},
		{Symbol: "BTC/USDT", Direction: "long", PnlPct: 1.5, HoldingHours: 10},
	}
	s := computeSummary(trades, "2024-01-01", "2024-06-01", 100)

	if s.TotalTrades != 5 {
		t.Fatalf("expected 5 trades, got %d", s.TotalTrades)
	}
	if s.WinningTrades != 3 {
		t.Fatalf("expected 3 wins, got %d", s.WinningTrades)
	}
	if s.LosingTrades != 2 {
		t.Fatalf("expected 2 losses, got %d", s.LosingTrades)
	}
	if s.WinRatePct != 60.0 {
		t.Fatalf("expected 60%% win rate, got %.2f", s.WinRatePct)
	}

	// Avg PnL = (2 + (-1) + 3 + (-0.5) + 1.5) / 5 = 5.0 / 5 = 1.0
	if s.AvgPnlPct != 1.0 {
		t.Fatalf("expected avg pnl 1.0, got %.4f", s.AvgPnlPct)
	}

	// Total return (equity curve): 1.0 -> *1.02 -> *0.99 -> *1.03 -> *0.995 -> *1.015
	expectedEquity := 1.02 * 0.99 * 1.03 * 0.995 * 1.015
	expectedTotalReturn := (expectedEquity - 1) * 100
	if math.Abs(s.TotalPnlPct-expectedTotalReturn) > 0.001 {
		t.Fatalf("expected total return %.4f, got %.4f", expectedTotalReturn, s.TotalPnlPct)
	}

	// Max drawdown should be non-zero
	if s.MaxDrawdownPct <= 0 {
		t.Fatalf("expected positive max drawdown, got %.4f", s.MaxDrawdownPct)
	}

	// Sharpe should be a reasonable positive number (positive mean return)
	if s.SharpeRatio <= 0 {
		t.Fatalf("expected positive Sharpe, got %.4f", s.SharpeRatio)
	}

	// Calmar ratio
	if s.CalmarRatio <= 0 {
		t.Fatalf("expected positive Calmar, got %.4f", s.CalmarRatio)
	}

	// Profit factor: sum(wins)/abs(sum(losses)) = (2+3+1.5)/abs(-1-0.5) = 6.5/1.5 = 4.3333
	expectedPF := (2.0 + 3.0 + 1.5) / math.Abs(-1.0-0.5)
	if math.Abs(s.ProfitFactor-expectedPF) > 0.01 {
		t.Fatalf("expected profit factor %.4f, got %.4f", expectedPF, s.ProfitFactor)
	}

	// Avg holding hours: (12+8+6+4+10)/5 = 8.0
	if math.Abs(s.AvgHoldingHours-8.0) > 0.01 {
		t.Fatalf("expected avg holding hours 8.0, got %.2f", s.AvgHoldingHours)
	}

	// Avg win PnL: (2+3+1.5)/3 = 2.1667
	if math.Abs(s.AvgWinPnlPct-2.1667) > 0.01 {
		t.Fatalf("expected avg win 2.1667, got %.4f", s.AvgWinPnlPct)
	}

	// Avg loss PnL: (-1 + -0.5)/2 = -0.75
	if math.Abs(s.AvgLossPnlPct-(-0.75)) > 0.01 {
		t.Fatalf("expected avg loss -0.75, got %.4f", s.AvgLossPnlPct)
	}
}

func TestCalcPnl(t *testing.T) {
	if p := calcPnl("long", 100, 105); math.Abs(p-5.0) > 0.001 {
		t.Fatalf("long +5%%: got %.4f", p)
	}
	if p := calcPnl("long", 100, 95); math.Abs(p-(-5.0)) > 0.001 {
		t.Fatalf("long -5%%: got %.4f", p)
	}
	if p := calcPnl("short", 100, 95); math.Abs(p-5.0) > 0.001 {
		t.Fatalf("short +5%%: got %.4f", p)
	}
	if p := calcPnl("short", 100, 105); math.Abs(p-(-5.0)) > 0.001 {
		t.Fatalf("short -5%%: got %.4f", p)
	}
	if p := calcPnl("long", 0, 100); p != 0 {
		t.Fatalf("zero entry: got %.4f", p)
	}
}

func TestRound(t *testing.T) {
	cases := []struct {
		x      float64
		prec   int
		expect float64
	}{
		{1.23456, 2, 1.23},
		{1.23556, 2, 1.24},
		{1.23456, 4, 1.2346},
		{1.0, 0, 1},
		{0.000999, 3, 0.001},
	}
	for _, c := range cases {
		got := round(c.x, c.prec)
		if math.Abs(got-c.expect) > 0.0001 {
			t.Errorf("round(%v, %d) = %v, want %v", c.x, c.prec, got, c.expect)
		}
	}
}

func TestTsToISO(t *testing.T) {
	// 2024-01-01T00:00:00Z in ms
	got := tsToISO(1704067200000)
	expect := "2024-01-01T00:00:00Z"
	if got != expect {
		t.Fatalf("tsToISO(1704067200000) = %q, want %q", got, expect)
	}
}

func TestHoldingHours(t *testing.T) {
	hh := holdingHours("2024-01-01T00:00:00Z", "2024-01-01T12:30:00Z")
	if math.Abs(hh-12.5) > 0.01 {
		t.Fatalf("expected 12.5 hours, got %.2f", hh)
	}
	hh = holdingHours("", "")
	if hh != 0 {
		t.Fatalf("expected 0 on bad input, got %.2f", hh)
	}
}
