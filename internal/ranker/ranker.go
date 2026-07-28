// Package ranker scores dual-sided directional candidates.
// Unknown features fail closed — never invent liquidity/spread/pullback.
package ranker

import (
	"fmt"
	"math"
	"sort"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// Input is one symbol's cross-section features for ranking.
// Measurement flags must be set when the corresponding field is real.
type Input struct {
	Symbol string

	ChangePct          float64
	MarketMedianChange float64
	BTCChange          float64
	BTCChangeSet       bool

	QuoteVolume  float64
	MinLiquidity float64

	// PullbackDepthPct / ReboundPct only used when *Measured is true.
	PullbackDepthPct float64
	PullbackMeasured bool
	ReboundPct       float64
	ReboundMeasured  bool

	RoomUpPct    float64
	RoomDownPct  float64
	RoomMeasured bool
	RequireRoom  bool
	MinRoomPct   float64

	// Hard gates — must be explicitly true after real checks.
	LiquidityOK       bool
	LiquidityMeasured bool
	SpreadOK          bool
	SpreadMeasured    bool
	DataOK            bool
}

// Config tunes ranker behaviour.
type Config struct {
	MinLiquidity float64
	MinRoomPct   float64
	RequireRoom  bool
	MaxResults   int
	// SoftRank allows return-only ranking for watch boards without hard gates.
	// Playbook path must use SoftRank=false (default).
	SoftRank bool
}

// DefaultConfig returns hard-gate defaults for ticket generation.
func DefaultConfig() Config {
	return Config{
		MinLiquidity: 1_000_000,
		MinRoomPct:   1.0,
		RequireRoom:  false,
		MaxResults:   0,
		SoftRank:     false,
	}
}

// SoftConfig is for pulse watch boards (display only, not playbook authority).
func SoftConfig() Config {
	c := DefaultConfig()
	c.SoftRank = true
	return c
}

// Rank applies hard filters then scores long/short symmetrically.
func Rank(inputs []Input, cfg Config) []types.DirectionalCandidate {
	out := make([]types.DirectionalCandidate, 0, len(inputs))
	for _, in := range inputs {
		if c, ok := scoreOne(in, cfg); ok {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ei := math.Max(out[i].LongScore, out[i].ShortScore)
		ej := math.Max(out[j].LongScore, out[j].ShortScore)
		if ei != ej {
			return ei > ej
		}
		return out[i].Symbol < out[j].Symbol
	})
	if cfg.MaxResults > 0 && len(out) > cfg.MaxResults {
		out = out[:cfg.MaxResults]
	}
	return out
}

// RankLong orders by LongScore desc.
func RankLong(inputs []Input, cfg Config) []types.DirectionalCandidate {
	all := Rank(inputs, cfg)
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].LongScore != all[j].LongScore {
			return all[i].LongScore > all[j].LongScore
		}
		return all[i].RelativeStrength > all[j].RelativeStrength
	})
	return all
}

// RankShort orders by ShortScore desc.
func RankShort(inputs []Input, cfg Config) []types.DirectionalCandidate {
	all := Rank(inputs, cfg)
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].ShortScore != all[j].ShortScore {
			return all[i].ShortScore > all[j].ShortScore
		}
		return all[i].RelativeWeakness > all[j].RelativeWeakness
	})
	return all
}

func scoreOne(in Input, cfg Config) (types.DirectionalCandidate, bool) {
	c := types.DirectionalCandidate{
		SchemaVersion: types.DirectionalCandidateSchemaVersion,
		Symbol:        in.Symbol,
	}

	if !in.DataOK {
		c.Warnings = append(c.Warnings, "data_incomplete")
		if !cfg.SoftRank {
			return c, false
		}
	}

	minLiq := cfg.MinLiquidity
	if in.MinLiquidity > 0 {
		minLiq = in.MinLiquidity
	}

	if !cfg.SoftRank {
		if !in.LiquidityMeasured || !in.LiquidityOK {
			c.Warnings = append(c.Warnings, "liquidity_unmeasured_or_fail")
			return c, false
		}
		if minLiq > 0 && in.QuoteVolume < minLiq {
			c.Warnings = append(c.Warnings, "liquidity_below_minimum")
			return c, false
		}
		if !in.SpreadMeasured || !in.SpreadOK {
			c.Warnings = append(c.Warnings, "spread_unmeasured_or_fail")
			return c, false
		}
		minRoom := cfg.MinRoomPct
		if in.MinRoomPct > 0 {
			minRoom = in.MinRoomPct
		}
		requireRoom := cfg.RequireRoom || in.RequireRoom
		if requireRoom {
			if !in.RoomMeasured {
				c.Warnings = append(c.Warnings, "room_unmeasured")
				return c, false
			}
			if in.RoomUpPct < minRoom && in.RoomDownPct < minRoom {
				c.Warnings = append(c.Warnings, "insufficient_room")
				return c, false
			}
		}
	} else {
		// soft: surface warnings, still emit for watch board
		if !in.LiquidityMeasured {
			c.Warnings = append(c.Warnings, "liquidity_unmeasured")
		}
		if !in.SpreadMeasured {
			c.Warnings = append(c.Warnings, "spread_unmeasured")
		}
	}

	c.LiquidityOK = in.LiquidityMeasured && in.LiquidityOK
	c.SpreadOK = in.SpreadMeasured && in.SpreadOK

	rel := in.ChangePct - in.MarketMedianChange
	c.RelativeStrength = round2(rel)
	c.RelativeWeakness = round2(-rel)

	if in.PullbackMeasured {
		c.PullbackStrength = round2(pullbackScore(in.PullbackDepthPct))
	} else {
		c.Warnings = append(c.Warnings, "pullback_unmeasured")
	}
	if in.ReboundMeasured {
		c.ReboundWeakness = round2(pullbackScore(in.ReboundPct))
	} else {
		c.Warnings = append(c.Warnings, "rebound_unmeasured")
	}

	longScore, shortScore := 0.0, 0.0

	if rel > 0 {
		comp := math.Min(3.0, rel/2.0)
		longScore += comp
		c.Reasons = append(c.Reasons, fmt.Sprintf("leads market by %.2f%%", rel))
	} else if rel < 0 {
		comp := math.Min(3.0, -rel/2.0)
		shortScore += comp
		c.Reasons = append(c.Reasons, fmt.Sprintf("lags market by %.2f%%", -rel))
	}

	if in.ChangePct > 0 {
		longScore += math.Min(2.0, in.ChangePct/4.0)
	} else if in.ChangePct < 0 {
		shortScore += math.Min(2.0, -in.ChangePct/4.0)
	}

	if in.BTCChangeSet {
		vsBTC := in.ChangePct - in.BTCChange
		if vsBTC > 0 {
			longScore += math.Min(1.5, vsBTC/4.0)
			c.Reasons = append(c.Reasons, fmt.Sprintf("beats BTC by %.2f%%", vsBTC))
		} else if vsBTC < 0 {
			shortScore += math.Min(1.5, -vsBTC/4.0)
			c.Reasons = append(c.Reasons, fmt.Sprintf("underperforms BTC by %.2f%%", -vsBTC))
		}
	}

	if in.PullbackMeasured {
		longScore += c.PullbackStrength
		if c.PullbackStrength >= 1 {
			c.Reasons = append(c.Reasons, "shallow pullback measured")
		}
	}
	if in.ReboundMeasured {
		shortScore += c.ReboundWeakness
		if c.ReboundWeakness >= 1 {
			c.Reasons = append(c.Reasons, "weak rebound measured")
		}
	}

	if in.RoomMeasured {
		minRoom := cfg.MinRoomPct
		if in.MinRoomPct > 0 {
			minRoom = in.MinRoomPct
		}
		if in.RoomUpPct >= minRoom {
			longScore += math.Min(1.0, in.RoomUpPct/10.0)
		}
		if in.RoomDownPct >= minRoom {
			shortScore += math.Min(1.0, in.RoomDownPct/10.0)
		}
	}

	if in.LiquidityMeasured && minLiq > 0 && in.QuoteVolume >= minLiq {
		boost := math.Min(1.0, in.QuoteVolume/minLiq/4.0)
		longScore += boost
		shortScore += boost
	}

	c.LongScore = round2(longScore)
	c.ShortScore = round2(shortScore)
	return c, true
}

func pullbackScore(depthPct float64) float64 {
	if depthPct < 0 {
		depthPct = 0
	}
	return math.Max(0, 2.0-depthPct/5.0)
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
