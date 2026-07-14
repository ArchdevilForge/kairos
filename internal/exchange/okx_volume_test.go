package exchange

import "testing"

func TestOKXUSDTQuoteVolume(t *testing.T) {
	if got := okxUSDTQuoteVolume(100, 65000); got != 6_500_000 {
		t.Fatalf("got %v want 6500000", got)
	}
	if okxUSDTQuoteVolume(0, 65000) != 0 || okxUSDTQuoteVolume(100, 0) != 0 {
		t.Fatal("zero input should return 0")
	}
}

func TestOKXIsCryptoUSDTSwap(t *testing.T) {
	if !okxIsCryptoUSDTSwap("BTC-USDT-SWAP", nil) {
		t.Fatal("nil set should allow all")
	}
	set := map[string]struct{}{"BTC-USDT-SWAP": {}}
	if !okxIsCryptoUSDTSwap("BTC-USDT-SWAP", set) {
		t.Fatal("crypto should pass")
	}
	if okxIsCryptoUSDTSwap("AAPL-USDT-SWAP", set) {
		t.Fatal("stock should fail")
	}
}
