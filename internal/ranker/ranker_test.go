package ranker

import (
	"testing"

	"github.com/ArchdevilForge/kairos/internal/types"
)

func base(sym string, change, market float64) Input {
	return Input{
		Symbol:             sym,
		ChangePct:          change,
		MarketMedianChange: market,
		BTCChange:          market * 0.5,
		BTCChangeSet:       true,
		QuoteVolume:        5_000_000,
		MinLiquidity:       1_000_000,
		PullbackDepthPct:   2,
		ReboundPct:         2,
		RoomUpPct:          8,
		RoomDownPct:        8,
		LiquidityOK:        true,
		SpreadOK:           true,
		DataOK:             true,
	}
}

func TestRank_LeaderLongAndLaggardShort(t *testing.T) {
	inputs := []Input{
		base("LEADER/USDT:USDT", 8, 2),   // strong lead
		base("MID/USDT:USDT", 2.5, 2),    // average
		base("LAGGARD/USDT:USDT", -6, 2), // weak
	}
	cfg := DefaultConfig()

	longs := RankLong(inputs, cfg)
	if longs[0].Symbol != "LEADER/USDT:USDT" {
		t.Fatalf("long rank[0]=%s scores=%v", longs[0].Symbol, scorePairs(longs))
	}
	if longs[0].LongScore <= longs[0].ShortScore {
		t.Fatalf("leader should favor long: %+v", longs[0])
	}
	if longs[0].RelativeStrength <= 0 {
		t.Fatalf("leader RS=%v", longs[0].RelativeStrength)
	}

	shorts := RankShort(inputs, cfg)
	if shorts[0].Symbol != "LAGGARD/USDT:USDT" {
		t.Fatalf("short rank[0]=%s scores=%v", shorts[0].Symbol, scorePairs(shorts))
	}
	if shorts[0].ShortScore <= shorts[0].LongScore {
		t.Fatalf("laggard should favor short: %+v", shorts[0])
	}
	if shorts[0].RelativeWeakness <= 0 {
		t.Fatalf("laggard RW=%v", shorts[0].RelativeWeakness)
	}
}

func TestRank_MirrorSymmetry(t *testing.T) {
	// Universe up: A leads
	up := []Input{
		base("A/USDT:USDT", 6, 1),
		base("B/USDT:USDT", 1, 1),
	}
	// Mirror: returns negated, market negated → A is laggard
	down := []Input{
		base("A/USDT:USDT", -6, -1),
		base("B/USDT:USDT", -1, -1),
	}
	upRank := RankLong(up, DefaultConfig())
	downRank := RankShort(down, DefaultConfig())
	if upRank[0].Symbol != "A/USDT:USDT" || downRank[0].Symbol != "A/USDT:USDT" {
		t.Fatalf("mirror leader/laggard: up=%s down=%s", upRank[0].Symbol, downRank[0].Symbol)
	}
	// scores should be approximately symmetric
	if diff := abs(upRank[0].LongScore - downRank[0].ShortScore); diff > 0.5 {
		t.Fatalf("score asymmetry long=%v short=%v", upRank[0].LongScore, downRank[0].ShortScore)
	}
}

func TestRank_HardFilterLiquidity(t *testing.T) {
	in := base("THIN/USDT:USDT", 10, 1)
	in.QuoteVolume = 100
	in.MinLiquidity = 1_000_000
	in.LiquidityOK = false
	out := Rank([]Input{in}, DefaultConfig())
	if len(out) != 0 {
		t.Fatalf("thin book should filter out, got %d", len(out))
	}
}

func TestRank_NoLongOnlyBias(t *testing.T) {
	// Equal magnitude lead up vs lag down should produce comparable side scores
	lead := base("UP/USDT:USDT", 5, 0)
	lag := base("DOWN/USDT:USDT", -5, 0)
	a, ok1 := scoreOne(lead, DefaultConfig())
	b, ok2 := scoreOne(lag, DefaultConfig())
	if !ok1 || !ok2 {
		t.Fatal("both should pass")
	}
	if abs(a.LongScore-b.ShortScore) > 0.2 {
		t.Fatalf("long-only bias? long=%v short=%v", a.LongScore, b.ShortScore)
	}
	if a.SchemaVersion != types.DirectionalCandidateSchemaVersion {
		t.Fatalf("schema %s", a.SchemaVersion)
	}
}

func scorePairs(cs []types.DirectionalCandidate) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Symbol
	}
	return out
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
