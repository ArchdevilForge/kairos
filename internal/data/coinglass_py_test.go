package data

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCoinGlassUsePython_Disabled(t *testing.T) {
	t.Setenv(envCoinGlassUsePy, "0")
	if coinGlassUsePython() {
		t.Fatal("expected disabled")
	}
}

func TestResolveCoinGlassDecryptRoot_FromEnv(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "decrypt.py"), []byte("# stub\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envCoinGlassDecrypt, root)
	got := resolveCoinGlassDecryptRoot()
	if got != root {
		t.Fatalf("got %q want %q", got, root)
	}
}

func TestFetchSpotRSIMap_LivePython(t *testing.T) {
	if os.Getenv("KAIROS_LIVE") == "" {
		t.Skip("set KAIROS_LIVE=1 to run")
	}
	decrypt := os.Getenv(envCoinGlassDecrypt)
	if decrypt == "" {
		decrypt = filepath.Clean(filepath.Join(findKairosRoot(), "..", "coinglass-decrypt"))
	}
	if _, err := os.Stat(filepath.Join(decrypt, "decrypt.py")); err != nil {
		t.Skip("coinglass-decrypt not available; set KAIROS_COINGLASS_DECRYPT")
	}
	t.Setenv(envCoinGlassDecrypt, decrypt)
	t.Setenv(envCoinGlassUsePy, "1")

	m, err := FetchSpotRSIMap(20 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if m["BTC"].RSI4h <= 0 {
		t.Fatalf("BTC rsi missing: %+v", m["BTC"])
	}
}
