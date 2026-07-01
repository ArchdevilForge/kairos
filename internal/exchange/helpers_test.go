package exchange

import "testing"

func TestParseFloat(t *testing.T) {
	if parseFloat("1.5") != 1.5 {
		t.Fatal("parseFloat 1.5")
	}
	if parseFloat("") != 0 {
		t.Fatal("parseFloat empty")
	}
	if parseFloat("bad") != 0 {
		t.Fatal("parseFloat bad")
	}
}

func TestBinanceSymbol(t *testing.T) {
	if got := binanceSymbol("BTC/USDT:USDT"); got != "BTCUSDT" {
		t.Fatalf("got %q", got)
	}
}

func TestToCanonicalBinance(t *testing.T) {
	if got := toCanonicalBinance("ETHUSDT"); got != "ETH/USDT:USDT" {
		t.Fatalf("got %q", got)
	}
	if got := toCanonicalBinance("INVALID"); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestExchangeNew_Unknown(t *testing.T) {
	_, err := New("unknown")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestExchangeNew_Known(t *testing.T) {
	for _, name := range []string{"okx", "binance", "bybit"} {
		ex, err := New(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if ex.Name() != name {
			t.Fatalf("name: got %q", ex.Name())
		}
		_ = ex.Close()
	}
}
