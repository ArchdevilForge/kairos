package exchange

import (
	"context"
	"testing"

	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestEnrichOKXFunding_EmptySymbols(t *testing.T) {
	tickers := map[string]*types.Ticker{
		"BTC/USDT:USDT": {Symbol: "BTC/USDT:USDT"},
	}
	enrichOKXFunding(context.Background(), newHTTPClient(), tickers, nil)
	if tickers["BTC/USDT:USDT"].FundingRate != nil {
		t.Fatal("expected no funding fetch for empty symbol list")
	}
}

func TestOKXEnrichFunding_Interface(t *testing.T) {
	var fe FundingEnricher = newOKX()
	tickers := map[string]*types.Ticker{
		"BTC/USDT:USDT": {Symbol: "BTC/USDT:USDT"},
	}
	// Live call optional; ensure it does not panic on missing network.
	fe.EnrichFunding(context.Background(), tickers, []string{"BTC/USDT:USDT"})
}
