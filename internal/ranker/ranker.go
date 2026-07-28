// Package ranker scores dual-sided directional candidates.
// Long and short are mirrors; positive change alone is not enough to win rank.
package ranker

import (
	"fmt"
	"math"
	"sort"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// Input is one symbol's cross-section features for ranking.
type Input struct {
	Symbol string

	// ChangePct is the symbol return over the ranking window (e.g. impulse 60s or 24h).
	ChangePct float64
	// MarketMedianChange is the universe median return over the same window.
	MarketMedianChange float64
	// BTCChange is BTC return over the same window (optional; 0 + BTCChangeSet=false to skip).
	BTCChange    float64
	BTCChangeSet bool

	QuoteVolume  float64
	MinLiquidity float64

	// PullbackDepthPct: giveback from local high (positive number). Smaller → better long.
	PullbackDepthPct float64
	// ReboundPct: bounce from local low (positive number). Smaller → better short.
	ReboundPct float64

	// Room in trade direction as percent (0 if unknown — fails hard filter when required).
	RoomUpPct   float64
	RoomDownPct float64
	RequireRoom bool
	MinRoomPct  float64

	LiquidityOK bool
	SpreadOK    bool
	// DataOK false → hard-filtered out.
	DataOK bool
}

// Config tunes ranker behaviour.
type Config struct {
	MinLiquidity float64
	MinRoomPct   float64
	RequireRoom  bool
	// MaxResults caps output after sort (0 = all that pass filters).
	MaxResults int
}

// DefaultConfig returns sane defaults.
func DefaultConfig() Config {
	return Config{
		MinLiquidity: 1_000_000,
		MinRoomPct:   1.0,
		RequireRoom:  false,
		MaxResults:   0,
	}
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
		// primary: best one-sided edge (max of long/short)
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

// RankLong orders by LongScore desc (among filter passers).
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
		Reasons:       nil,
		Warnings:      nil,
	}

	minLiq := cfg.MinLiquidity
	if in.MinLiquidity > 0 {
		minLiq = in.MinLiquidity
	}
	minRoom := cfg.MinRoomPct
	if in.MinRoomPct > 0 {
		minRoom = in.MinRoomPct
	}
	requireRoom := cfg.RequireRoom || in.RequireRoom

	liqOK := in.LiquidityOK
	if in.QuoteVolume > 0 && minLiq > 0 {
		liqOK = in.QuoteVolume >= minLiq
	}
	// if caller set LiquidityOK explicitly false with volume, honor false
	if in.QuoteVolume == 0 && !in.LiquidityOK && !in.DataOK {
		liqOK = false
	}
	spreadOK := in.SpreadOK
	// default spread ok when not specified as failed — callers should set SpreadOK
	if !in.DataOK {
		c.Warnings = append(c.Warnings, "data incomplete")
		return c, false
	}
	if !liqOK {
		c.Warnings = append(c.Warnings, "liquidity below minimum")
		return c, false
	}
	if !spreadOK {
		// treat unset SpreadOK as true only when DataOK; require explicit false to fail
		// Convention: SpreadOK must be true to pass (callers set true by default).
		c.Warnings = append(c.Warnings, "spread not ok")
		return c, false
	}
	if requireRoom && in.RoomUpPct < minRoom && in.RoomDownPct < minRoom {
		c.Warnings = append(c.Warnings, "insufficient room both sides")
		return c, false
	}

	c.LiquidityOK = true
	c.SpreadOK = true

	rel := in.ChangePct - in.MarketMedianChange
	// Relative strength: how much it beats the market (can be negative).
	c.RelativeStrength = round2(rel)
	// Relative weakness: how much it lags the market (positive when weaker).
	c.RelativeWeakness = round2(-rel)

	// Pullback quality for longs: shallower pullback → higher strength (0..1-ish scale pts).
	c.PullbackStrength = round2(pullbackScore(in.PullbackDepthPct))
	// Rebound weakness for shorts: shallower bounce → higher score.
	c.ReboundWeakness = round2(pullbackScore(in.ReboundPct))

	longScore := 0.0
	shortScore := 0.0

	// Relative vs market (symmetric)
	if rel > 0 {
		comp := math.Min(3.0, rel/2.0)
		longScore += comp
		c.Reasons = append(c.Reasons, fmt.Sprintf("leads market by %.2f%%", rel))
	} else if rel < 0 {
		comp := math.Min(3.0, -rel/2.0)
		shortScore += comp
		c.Reasons = append(c.Reasons, fmt.Sprintf("lags market by %.2f%%", -rel))
	}

	// Absolute direction component (symmetric) — |change| contributes to the side it favors
	if in.ChangePct > 0 {
		comp := math.Min(2.0, in.ChangePct/4.0)
		longScore += comp
	} else if in.ChangePct < 0 {
		comp := math.Min(2.0, -in.ChangePct/4.0)
		shortScore += comp
	}

	// BTC relative (symmetric)
	if in.BTCChangeSet {
		vsBTC := in.ChangePct - in.BTCChange
		if vsBTC > 0 {
			comp := math.Min(1.5, vsBTC/4.0)
			longScore += comp
			c.Reasons = append(c.Reasons, fmt.Sprintf("beats BTC by %.2f%%", vsBTC))
		} else if vsBTC < 0 {
			comp := math.Min(1.5, -vsBTC/4.0)
			shortScore += comp
			c.Reasons = append(c.Reasons, fmt.Sprintf("underperforms BTC by %.2f%%", -vsBTC))
		}
	}

	// Pullback / rebound quality
	longScore += c.PullbackStrength
	shortScore += c.ReboundWeakness
	if c.PullbackStrength >= 1.0 {
		c.Reasons = append(c.Reasons, "shallow pullback")
	}
	if c.ReboundWeakness >= 1.0 {
		c.Reasons = append(c.Reasons, "weak rebound")
	}

	// Room bonus (non-compensating for hard filter; soft rank only)
	if in.RoomUpPct >= minRoom {
		longScore += math.Min(1.0, in.RoomUpPct/10.0)
	} else if requireRoom {
		c.Warnings = append(c.Warnings, "limited room up")
	}
	if in.RoomDownPct >= minRoom {
		shortScore += math.Min(1.0, in.RoomDownPct/10.0)
	} else if requireRoom {
		c.Warnings = append(c.Warnings, "limited room down")
	}

	// Liquidity soft boost
	if minLiq > 0 && in.QuoteVolume >= minLiq {
		boost := math.Min(1.0, in.QuoteVolume/minLiq/4.0)
		longScore += boost
		shortScore += boost
	}

	c.LongScore = round2(longScore)
	c.ShortScore = round2(shortScore)
	return c, true
}

// pullbackScore maps depth% to 0..2 (0% depth → 2, 10%+ → ~0).
func pullbackScore(depthPct float64) float64 {
	if depthPct < 0 {
		depthPct = 0
	}
	return math.Max(0, 2.0-depthPct/5.0)
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
