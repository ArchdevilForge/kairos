package engine

import (
	"strings"

	"github.com/ArchdevilForge/kairos/internal/data"
)

// marketCapReference picks BTC (then ETH, then watched-universe max) as weight denominator.
func marketCapReference(caps map[string]float64, symbolsByExchange map[string][]string) (refUSD, universeMax float64) {
	for _, symbols := range symbolsByExchange {
		for _, sym := range symbols {
			base, err := data.NormalizeCoinSymbol(sym)
			if err != nil {
				continue
			}
			if cap := caps[strings.ToUpper(base)]; cap > universeMax {
				universeMax = cap
			}
		}
	}
	refUSD = caps["BTC"]
	if refUSD <= 0 {
		refUSD = caps["ETH"]
	}
	if refUSD <= 0 {
		refUSD = universeMax
	}
	return refUSD, universeMax
}
