package engine

import (
	"context"
	"time"

	"github.com/ArchdevilForge/kairos/internal/data"
)

func (p *Pipeline) loadMarketCaps(ctx context.Context) {
	if !p.cfg.AlertPolicy.LiquidityWeight.Enabled {
		return
	}
	select {
	case <-ctx.Done():
		return
	default:
	}

	entries, err := data.FetchMarketCapRankEntries(ctx, 30*time.Second)
	var caps map[string]float64
	if err == nil && len(entries) > 0 {
		caps = make(map[string]float64, len(entries))
		for k, e := range entries {
			if e.CapUSD > 0 {
				caps[k] = e.CapUSD
			}
		}
	} else {
		if err != nil {
			p.log.Warn("market cap rank entries fetch failed, falling back", "error", err)
		}
		caps, err = data.FetchMarketCapMap(ctx, 30*time.Second)
		if err != nil {
			p.log.Warn("market cap fetch failed", "error", err)
			return
		}
	}

	refCap, universeMax := marketCapReference(caps, p.allSymbolsSnapshot())
	if refCap <= 0 && len(entries) == 0 {
		p.log.Warn("market cap reference unavailable; liquidity weight disabled until next refresh")
		return
	}

	p.liqMu.Lock()
	if len(entries) > 0 {
		p.marketCapEntries = entries
	}
	if refCap > 0 {
		p.marketCapByCoin = caps
		p.marketCapRefUSD = refCap
	} else {
		p.log.Warn("market cap reference unavailable; liquidity weight disabled until next refresh")
	}
	p.liqMu.Unlock()

	p.log.Info("market caps loaded",
		"coins", len(caps),
		"entries", len(entries),
		"reference_usd", refCap,
		"universe_max_usd", universeMax,
	)
}
