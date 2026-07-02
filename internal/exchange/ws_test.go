package exchange

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestBinanceSubscribeTickers_ClosedAndCancelled(t *testing.T) {
	b := newBinance()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.SubscribeTickers(ctx, []string{"BTC/USDT:USDT"}, make(chan types.Ticker)); err == nil {
		t.Fatal("expected ctx error")
	}
	_ = b.Close()
	if err := b.SubscribeTickers(context.Background(), []string{"BTC/USDT:USDT"}, make(chan types.Ticker)); err == nil {
		t.Fatal("expected closed error")
	}
	if err := b.SubscribeTickers(context.Background(), nil, make(chan types.Ticker)); err == nil {
		t.Fatal("expected no symbols error")
	}
}

func TestOKXSubscribeTickers_ClosedAndCancelled(t *testing.T) {
	o := newOKX()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := o.SubscribeTickers(ctx, []string{"BTC/USDT:USDT"}, make(chan types.Ticker)); err == nil {
		t.Fatal("expected ctx error")
	}
	_ = o.Close()
	if err := o.SubscribeTickers(context.Background(), []string{"BTC/USDT:USDT"}, make(chan types.Ticker)); err == nil {
		t.Fatal("expected closed error")
	}
}

func TestBybitSubscribeTickers_ClosedAndCancelled(t *testing.T) {
	b := newBybit()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.SubscribeTickers(ctx, []string{"BTC/USDT:USDT"}, make(chan types.Ticker)); err == nil {
		t.Fatal("expected ctx error")
	}
	_ = b.Close()
	if err := b.SubscribeTickers(context.Background(), []string{"BTC/USDT:USDT"}, make(chan types.Ticker)); err == nil {
		t.Fatal("expected closed error")
	}
}

func TestOKXEnrichFunding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "funding-rate") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": "0",
				"data": []map[string]string{{"fundingRate": "0.0003"}},
			})
		}
	}))
	defer srv.Close()
	o := newOKX()
	o.httpCli = testHTTPClient(srv)
	vol := 1.0
	tickers := map[string]*types.Ticker{
		"BTC/USDT:USDT": {Symbol: "BTC/USDT:USDT", LastPrice: &vol},
	}
	o.EnrichFunding(context.Background(), tickers, []string{"BTC/USDT:USDT"})
	if tickers["BTC/USDT:USDT"].FundingRate == nil {
		t.Fatal("expected funding rate")
	}
}

func TestMapTimeframeAndDenormalize(t *testing.T) {
	if mapTimeframe("2h") != "2H" {
		t.Fatal("mapTimeframe")
	}
	if denormalizeSymbol("BTC-USDT-SWAP") != "BTC/USDT:USDT" {
		t.Fatal("denormalize")
	}
	if normalizeSymbol("BTC/USDT") == "" {
		t.Fatal("normalize")
	}
}
