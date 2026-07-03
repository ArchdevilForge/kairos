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

	caps, err := data.FetchMarketCapMap(30 * time.Second)
	if err != nil {
		p.log.Warn("market cap fetch failed", "error", err)
		return
	}

	refCap, universeMax := marketCapReference(caps, p.symbolsByExchange)
	if refCap <= 0 {
		p.log.Warn("market cap reference unavailable; liquidity weight disabled until next refresh")
		return
	}

	p.liqMu.Lock()
	p.marketCapByCoin = caps
	p.marketCapRefUSD = refCap
	p.liqMu.Unlock()

	p.log.Info("market caps loaded",
		"coins", len(caps),
		"reference_usd", refCap,
		"universe_max_usd", universeMax,
	)
}
