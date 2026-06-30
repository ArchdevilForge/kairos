package data

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"encoding/base64"
	"encoding/json"
	"testing"
)

// ---------------------------------------------------------------------------
// Encrypt helpers (mirror Python's _aes_decrypt for round-trip tests)
// ---------------------------------------------------------------------------

func pkcs7Pad(b []byte, blockSize int) []byte {
	n := blockSize - len(b)%blockSize
	pad := bytes.Repeat([]byte{byte(n)}, n)
	return append(b, pad...)
}

func aesEncryptECB(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	bs := block.BlockSize()
	padded := pkcs7Pad(plaintext, bs)
	out := make([]byte, len(padded))
	for i := 0; i < len(padded); i += bs {
		block.Encrypt(out[i:i+bs], padded[i:i+bs])
	}
	return out, nil
}

func gzipCompress(data []byte) []byte {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	w.Write(data)
	w.Close()
	return buf.Bytes()
}

// buildCoinGlassResponse creates a synthetic encrypted CoinGlass response
// for testing.  Returns the raw JSON body and the user token (base64).
func buildCoinGlassResponse(finalJSON, actualKey string, key0Seed, v string) ([]byte, string, error) {
	key0 := base64.StdEncoding.EncodeToString([]byte(key0Seed))[:16]

	// Step 1: gzip actual_key → AES-ECB with key0
	compressedKey := gzipCompress([]byte(actualKey))
	encryptedToken, err := aesEncryptECB(compressedKey, []byte(key0))
	if err != nil {
		return nil, "", err
	}
	userTokenB64 := base64.StdEncoding.EncodeToString(encryptedToken)

	// Step 2: gzip finalJSON → AES-ECB with actual_key
	compressedData := gzipCompress([]byte(finalJSON))
	encryptedPayload, err := aesEncryptECB(compressedData, []byte(actualKey))
	if err != nil {
		return nil, "", err
	}
	payloadB64 := base64.StdEncoding.EncodeToString(encryptedPayload)

	// Outer JSON envelope
	outer, _ := json.Marshal(map[string]any{
		"data": payloadB64,
	})
	return outer, userTokenB64, nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestAESDecryptRoundTrip(t *testing.T) {
	key := []byte("MTcwYjA3MGRhOTAx") // 16 bytes, valid AES-128
	plaintext := []byte("hello-aes-ecb-12345678")

	encrypted, err := aesEncryptECB(plaintext, key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	decrypted, err := aesDecrypt(encrypted, key)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(decrypted) != string(plaintext) {
		t.Fatalf("round-trip mismatch: got %q, want %q", decrypted, plaintext)
	}
}

func TestDecryptCoinGlassResponse_Version55(t *testing.T) {
	expectedJSON := `{"symbol":"BTC","price":65432.10,"openInterest":123456789}`
	actualKey := "0123456789abcdef0123456789abcdef" // 32 bytes, AES-256
	key0Seed := keyTable["55"]

	body, userToken, err := buildCoinGlassResponse(expectedJSON, actualKey, key0Seed, "55")
	if err != nil {
		t.Fatalf("build response: %v", err)
	}

	result, err := DecryptCoinGlassResponse(body, userToken, "55", "")
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result is not map, got %T", result)
	}
	if m["symbol"] != "BTC" {
		t.Errorf("symbol = %v, want BTC", m["symbol"])
	}
	if m["price"] != 65432.10 {
		t.Errorf("price = %v, want 65432.10", m["price"])
	}
}

func TestDecryptCoinGlassResponse_Version66(t *testing.T) {
	expectedJSON := `{"data":[{"symbol":"ETH","longRate":75.5,"shortRate":24.5}]}`
	actualKey := "0123456789abcdef0123456789abcdef"
	key0Seed := keyTable["66"]

	body, userToken, err := buildCoinGlassResponse(expectedJSON, actualKey, key0Seed, "66")
	if err != nil {
		t.Fatalf("build response: %v", err)
	}

	result, err := DecryptCoinGlassResponse(body, userToken, "66", "")
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result is not map, got %T", result)
	}
	data, ok := m["data"].([]any)
	if !ok {
		t.Fatalf("data is not []any, got %T", m["data"])
	}
	if len(data) != 1 {
		t.Fatalf("data len = %d, want 1", len(data))
	}
	entry := data[0].(map[string]any)
	if entry["symbol"] != "ETH" {
		t.Errorf("symbol = %v, want ETH", entry["symbol"])
	}
}

func TestDecryptCoinGlassResponse_Version77(t *testing.T) {
	expectedJSON := `{"result":"ok","total":42}`
	actualKey := "0123456789abcdef0123456789abcdef"
	key0Seed := keyTable["77"]

	body, userToken, err := buildCoinGlassResponse(expectedJSON, actualKey, key0Seed, "77")
	if err != nil {
		t.Fatalf("build response: %v", err)
	}

	result, err := DecryptCoinGlassResponse(body, userToken, "77", "")
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result is not map, got %T", result)
	}
	if m["result"] != "ok" {
		t.Errorf("result = %v, want ok", m["result"])
	}
}

func TestDecryptCoinGlassResponse_Version1_URLPath(t *testing.T) {
	expectedJSON := `{"status":"success"}`
	actualKey := "0123456789abcdef0123456789abcdef"
	urlPath := "/api/v2/longShortRatio"

	// For v=1, key0Seed is the URL path itself (buildCoinGlassResponse
	// will base64-encode it and trim to 16 bytes).
	body, userToken, err := buildCoinGlassResponse(expectedJSON, actualKey, urlPath, "1")
	if err != nil {
		t.Fatalf("build response: %v", err)
	}

	result, err := DecryptCoinGlassResponse(body, userToken, "1", "https://capi.coinglass.com"+urlPath)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result is not map, got %T", result)
	}
	if m["status"] != "success" {
		t.Errorf("status = %v, want success", m["status"])
	}
}

func TestDecryptCoinGlassResponse_InvalidVersion(t *testing.T) {
	body := []byte(`{"data":"dGVzdA=="}`)
	_, err := DecryptCoinGlassResponse(body, "dXNlcg==", "99", "")
	if err == nil {
		t.Fatal("expected error for unknown version, got nil")
	}
	if _, ok := err.(*CoinGlassDecodeError); !ok {
		t.Errorf("expected *CoinGlassDecodeError, got %T", err)
	}
}

func TestDecryptCoinGlassResponse_MissingData(t *testing.T) {
	body := []byte(`{"foo":"bar"}`)
	_, err := DecryptCoinGlassResponse(body, "dXNlcg==", "55", "")
	if err == nil {
		t.Fatal("expected error for missing data, got nil")
	}
	if _, ok := err.(*CoinGlassDecodeError); !ok {
		t.Errorf("expected *CoinGlassDecodeError, got %T", err)
	}
}

func TestDecryptCoinGlassResponse_InvalidBase64(t *testing.T) {
	body := []byte(`{"data":"!!!invalid-base64!!!"}`)
	_, err := DecryptCoinGlassResponse(body, "dXNlcg==", "55", "")
	if err == nil {
		t.Fatal("expected error for invalid base64, got nil")
	}
	if _, ok := err.(*CoinGlassDecodeError); !ok {
		t.Errorf("expected *CoinGlassDecodeError, got %T", err)
	}
}

// ---------------------------------------------------------------------------
// NormalizeCoinSymbol tests
// ---------------------------------------------------------------------------

func TestNormalizeCoinSymbol(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"BTC/USDT:USDT", "BTC"},
		{"ETH-USDT", "ETH"},
		{"SOL_USDT", "SOL"},
		{"XRPUSDT", "XRP"},
		{"ADA/USDC", "ADA"},
		{"DOT-USD", "DOT"},
		{"AVAXPERP", "AVAX"},
		{"BTC:USDT", "BTC"},
		{"  ETH-USDT  ", "ETH"},
	}
	for _, tt := range tests {
		got, err := NormalizeCoinSymbol(tt.input)
		if err != nil {
			t.Errorf("NormalizeCoinSymbol(%q): %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("NormalizeCoinSymbol(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeCoinSymbol_Empty(t *testing.T) {
	_, err := NormalizeCoinSymbol("")
	if err == nil {
		t.Fatal("expected error for empty symbol")
	}
	_, err = NormalizeCoinSymbol("  ")
	if err == nil {
		t.Fatal("expected error for whitespace-only symbol")
	}
}

func TestNormalizeCoinSymbol_NoSuffix(t *testing.T) {
	// Symbol with no recognized suffix returns the stripped value as-is.
	got, err := NormalizeCoinSymbol("SUSHI")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "SUSHI" {
		t.Errorf("got %q, want SUSHI", got)
	}
}
