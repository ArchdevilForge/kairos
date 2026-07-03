package engine

import (
	"math"
	"strings"

	"github.com/ArchdevilForge/kairos/internal/data"
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

func (p *Pipeline) symbolMarketCap(symbol string) float64 {
	base, err := data.NormalizeCoinSymbol(symbol)
	if err != nil || base == "" {
		return 0
	}
	p.liqMu.RLock()
	defer p.liqMu.RUnlock()
	return p.marketCapByCoin[strings.ToUpper(base)]
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
