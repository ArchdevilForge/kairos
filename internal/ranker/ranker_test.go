package ranker

import (
	"testing"

	"github.com/ArchdevilForge/kairos/internal/types"
)

func measured(sym string, change, market float64) Input {
	return Input{
		Symbol:             sym,
		ChangePct:          change,
		MarketMedianChange: market,
		BTCChange:          market * 0.5,
		BTCChangeSet:       true,
		QuoteVolume:        5_000_000,
		MinLiquidity:       1_000_000,
		PullbackDepthPct:   2,
		PullbackMeasured:   true,
		ReboundPct:         2,
		ReboundMeasured:    true,
		RoomUpPct:          8,
		RoomDownPct:        8,
		RoomMeasured:       true,
		LiquidityOK:        true,
		LiquidityMeasured:  true,
		SpreadOK:           true,
		SpreadMeasured:     true,
		DataOK:             true,
	}
}

func TestRank_LeaderLongAndLaggardShort(t *testing.T) {
	inputs := []Input{
		measured("LEADER/USDT:USDT", 8, 2),
		measured("MID/USDT:USDT", 2.5, 2),
		measured("LAGGARD/USDT:USDT", -6, 2),
	}
	cfg := DefaultConfig()
	longs := RankLong(inputs, cfg)
	if longs[0].Symbol != "LEADER/USDT:USDT" {
		t.Fatalf("long rank[0]=%s", longs[0].Symbol)
	}
	shorts := RankShort(inputs, cfg)
	if shorts[0].Symbol != "LAGGARD/USDT:USDT" {
		t.Fatalf("short rank[0]=%s", shorts[0].Symbol)
	}
}

func TestRank_FailClosedUnmeasured(t *testing.T) {
	in := measured("X/USDT:USDT", 5, 1)
	in.LiquidityMeasured = false
	out := Rank([]Input{in}, DefaultConfig())
	if len(out) != 0 {
		t.Fatal("unmeasured liquidity must fail closed")
	}
	in.LiquidityMeasured = true
	in.SpreadMeasured = false
	out = Rank([]Input{in}, DefaultConfig())
	if len(out) != 0 {
		t.Fatal("unmeasured spread must fail closed")
	}
}

func TestRank_SoftAllowsUnmeasured(t *testing.T) {
	in := Input{
		Symbol: "Y/USDT:USDT", ChangePct: 3, MarketMedianChange: 1, DataOK: true,
	}
	out := Rank([]Input{in}, SoftConfig())
	if len(out) != 1 {
		t.Fatal("soft rank should emit watch row")
	}
	if out[0].LiquidityOK || out[0].SpreadOK {
		t.Fatal("soft row must not claim liq/spread ok")
	}
}

func TestRank_NoLongOnlyBias(t *testing.T) {
	a, ok1 := scoreOne(measured("UP/USDT:USDT", 5, 0), DefaultConfig())
	b, ok2 := scoreOne(measured("DOWN/USDT:USDT", -5, 0), DefaultConfig())
	if !ok1 || !ok2 {
		t.Fatal("both should pass")
	}
	if abs(a.LongScore-b.ShortScore) > 0.2 {
		t.Fatalf("asymmetry long=%v short=%v", a.LongScore, b.ShortScore)
	}
	if a.SchemaVersion != types.DirectionalCandidateSchemaVersion {
		t.Fatal(a.SchemaVersion)
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
