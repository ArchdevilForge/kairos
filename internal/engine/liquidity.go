package engine

import (
	"math"
	"strings"

	"github.com/ArchdevilForge/kairos/internal/data"
	"github.com/ArchdevilForge/kairos/internal/types"
)

func (p *Pipeline) isMajorSymbol(symbol string) bool {
	for _, maj := range p.cfg.AlertPolicy.LiquidityWeight.MajorSymbols {
		if symbol == maj {
			return true
		}
	}
	if strings.HasPrefix(symbol, "BTC/") || strings.HasPrefix(symbol, "ETH/") {
		return true
	}
	return false
}

// IsVettedSymbol reports whether sym belongs to the vetted Top-100 market-cap
// universe. Returns true for "MARKET", empty, BTC/ETH majors, or before rank data loads.
func (p *Pipeline) IsVettedSymbol(sym string) bool {
	if p == nil {
		return true
	}
	s := strings.TrimSpace(sym)
	if s == "" || strings.EqualFold(s, "MARKET") {
		return true
	}
	if p.isMajorSymbol(s) {
		return true
	}
	base := data.CanonicalMarketCapBase(s)
	if base == "BTC" || base == "ETH" {
		return true
	}

	p.liqMu.RLock()
	entries := p.marketCapEntries
	n := len(entries)
	entry, ok := entries[base]
	p.liqMu.RUnlock()

	if n == 0 {
		return true
	}
	return ok && entry.Rank > 0 && entry.Rank <= 100
}

func (p *Pipeline) isTop20ExtremeBreakout(evt types.AnomalyEvent) bool {
	if p == nil || evt.EventType != "price_velocity" {
		return false
	}
	base := data.CanonicalMarketCapBase(evt.Symbol)
	if base == "" {
		return false
	}
	isBTCETH := base == "BTC" || base == "ETH"

	p.liqMu.RLock()
	entry, hasEntry := p.marketCapEntries[base]
	p.liqMu.RUnlock()

	inTop20 := isBTCETH || p.isMajorSymbol(evt.Symbol) || (hasEntry && entry.Rank > 0 && entry.Rank <= 20)
	if !inTop20 {
		return false
	}

	changePct := math.Abs(floatFromMapDefault(evt.Data, "change_pct", 0.0))
	winSec := floatFromMapDefault(evt.Data, "window_seconds", 60.0)
	if winSec <= 0 {
		winSec = 60.0
	}
	shortWin := winSec <= 60.0

	if isBTCETH {
		if shortWin {
			return changePct >= 1.8
		}
		return changePct >= 2.4
	}
	if shortWin {
		return changePct >= 2.2
	}
	return changePct >= 3.0
}

func (p *Pipeline) symbolMarketCap(symbol string) float64 {
	base := data.CanonicalMarketCapBase(symbol)
	if base == "" {
		norm, err := data.NormalizeCoinSymbol(symbol)
		if err != nil || norm == "" {
			return 0
		}
		base = strings.ToUpper(norm)
	}
	p.liqMu.RLock()
	defer p.liqMu.RUnlock()
	return p.marketCapByCoin[base]
}

// liquidityWeight returns 0..1 from cached CoinGlass market cap (fetched at subscribe/refresh).
func (p *Pipeline) liquidityWeight(symbol string) float64 {
	lw := p.cfg.AlertPolicy.LiquidityWeight
	if !lw.Enabled {
		return 1
	}
	if p.isMajorSymbol(symbol) {
		return 1
	}

	minW := lw.MinWeight
	if minW <= 0 || minW > 1 {
		minW = 0.3
	}

	cap := p.symbolMarketCap(symbol)
	p.liqMu.RLock()
	capsLoaded := len(p.marketCapByCoin) > 0
	refCap := p.marketCapRefUSD
	p.liqMu.RUnlock()

	if !capsLoaded || refCap <= 0 {
		return 1
	}
	if cap <= 0 {
		return minW
	}
	ratio := cap / refCap
	if ratio > 1 {
		ratio = 1
	}
	return minW + (1-minW)*math.Sqrt(ratio)
}

func liquidityStrictness(weight float64) float64 {
	if weight <= 0 {
		return 1
	}
	if weight >= 1 {
		return 1
	}
	return 1 / weight
}

func liquiditySeverityPenalty(weight float64) int {
	if weight >= 1 {
		return 0
	}
	return int(math.Ceil((1 - weight) * 2))
}
