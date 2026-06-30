package data

import (
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// CoinGlass encrypted API client.
// Ported from src/kairos/data/coinglass_client.py.
// ---------------------------------------------------------------------------

const coinglassBase = "https://capi.coinglass.com"

// keyTable maps encryption version identifiers to key seeds.
var keyTable = map[string]string{
	"55": "170b070da9654622",
	"66": "d6537d845a964081",
	"77": "863f08689c97435b",
}

// ---------------------------------------------------------------------------
// Error types
// ---------------------------------------------------------------------------

// CoinGlassError is the base error for CoinGlass client failures.
type CoinGlassError struct {
	msg string
}

func (e *CoinGlassError) Error() string { return "coinglass: " + e.msg }

// CoinGlassAPIError is returned when the CoinGlass HTTP request fails.
type CoinGlassAPIError struct{ CoinGlassError }

// CoinGlassDecodeError is returned when decryption or response parsing fails.
type CoinGlassDecodeError struct{ CoinGlassError }

// ---------------------------------------------------------------------------
// ECB block mode (Go's crypto/cipher omits ECB for security reasons, but
// CoinGlass requires it for compatibility).
// ---------------------------------------------------------------------------

type ecb struct {
	b   cipher.Block
	bs  int
}

func newECB(b cipher.Block) *ecb {
	return &ecb{b: b, bs: b.BlockSize()}
}

type ecbDecryptor ecb

// NewECBDecryptor returns a BlockMode that decrypts in ECB mode.
func NewECBDecryptor(b cipher.Block) cipher.BlockMode {
	return (*ecbDecryptor)(newECB(b))
}

func (x *ecbDecryptor) BlockSize() int { return x.bs }

func (x *ecbDecryptor) CryptBlocks(dst, src []byte) {
	if len(src)%x.bs != 0 {
		panic("crypto/cipher: input not full blocks")
	}
	if len(dst) < len(src) {
		panic("crypto/cipher: output smaller than input")
	}
	for i := 0; i < len(src); i += x.bs {
		x.b.Decrypt(dst[i:i+x.bs], src[i:i+x.bs])
	}
}

// ---------------------------------------------------------------------------
// PKCS7 unpadding
// ---------------------------------------------------------------------------

func unpadPKCS7(b []byte) ([]byte, error) {
	if len(b) == 0 {
		return nil, fmt.Errorf("empty data")
	}
	n := int(b[len(b)-1])
	if n == 0 || n > len(b) {
		return nil, fmt.Errorf("invalid PKCS7 padding length %d", n)
	}
	for _, v := range b[len(b)-n:] {
		if v != byte(n) {
			return nil, fmt.Errorf("invalid PKCS7 padding")
		}
	}
	return b[:len(b)-n], nil
}

// ---------------------------------------------------------------------------
// Core decryption
// ---------------------------------------------------------------------------

// aesDecrypt performs AES-ECB decryption with PKCS7 unpadding.
func aesDecrypt(ciphertext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	if len(ciphertext)%block.BlockSize() != 0 {
		return nil, fmt.Errorf("ciphertext length %d is not a multiple of block size %d", len(ciphertext), block.BlockSize())
	}
	mode := NewECBDecryptor(block)
	plain := make([]byte, len(ciphertext))
	mode.CryptBlocks(plain, ciphertext)
	return unpadPKCS7(plain)
}

// DecryptCoinGlassResponse decrypts an encrypted CoinGlass response body.
// Ported from coinglass_client.decrypt_coinglass_response.
func DecryptCoinGlassResponse(encryptedBody []byte, userTokenB64, v, reqURL string) (any, error) {
	// Parse outer JSON to get the base64-encoded payload.
	outer := make(map[string]any)
	if err := json.Unmarshal(encryptedBody, &outer); err != nil {
		return nil, &CoinGlassDecodeError{CoinGlassError{msg: fmt.Sprintf("json decode outer: %v", err)}}
	}
	dataStr, ok := outer["data"].(string)
	if !ok {
		return nil, &CoinGlassDecodeError{CoinGlassError{msg: "missing or non-string 'data' field in response"}}
	}
	payload, err := base64.StdEncoding.DecodeString(dataStr)
	if err != nil {
		return nil, &CoinGlassDecodeError{CoinGlassError{msg: fmt.Sprintf("base64 decode payload: %v", err)}}
	}
	token, err := base64.StdEncoding.DecodeString(userTokenB64)
	if err != nil {
		return nil, &CoinGlassDecodeError{CoinGlassError{msg: fmt.Sprintf("base64 decode user token: %v", err)}}
	}

	// Derive key0: for v=1, key is derived from the URL path.
	var key0 string
	if v == "1" {
		parsed, err := url.Parse(reqURL)
		if err == nil {
			path := parsed.Path
			if path == "" {
				path = strings.SplitN(reqURL, "?", 2)[0]
			}
			key0 = base64.StdEncoding.EncodeToString([]byte(path))[:16]
		}
	}
	if key0 == "" {
		seed, ok := keyTable[v]
		if !ok {
			return nil, &CoinGlassDecodeError{CoinGlassError{msg: fmt.Sprintf("unknown encryption version: %s", v)}}
		}
		key0 = base64.StdEncoding.EncodeToString([]byte(seed))[:16]
	}

	// Step 1: decrypt token using key0 to get the actual AES key.
	step1, err := aesDecrypt(token, []byte(key0))
	if err != nil {
		return nil, &CoinGlassDecodeError{CoinGlassError{msg: fmt.Sprintf("step1 decrypt: %v", err)}}
	}
	gzr1, err := gzip.NewReader(strings.NewReader(string(step1)))
	if err != nil {
		return nil, &CoinGlassDecodeError{CoinGlassError{msg: fmt.Sprintf("step1 gunzip: %v", err)}}
	}
	actualKey, err := io.ReadAll(gzr1)
	gzr1.Close()
	if err != nil {
		return nil, &CoinGlassDecodeError{CoinGlassError{msg: fmt.Sprintf("step1 gunzip read: %v", err)}}
	}

	// Step 2: decrypt payload using the actual key.
	step2, err := aesDecrypt(payload, actualKey)
	if err != nil {
		return nil, &CoinGlassDecodeError{CoinGlassError{msg: fmt.Sprintf("step2 decrypt: %v", err)}}
	}
	gzr2, err := gzip.NewReader(strings.NewReader(string(step2)))
	if err != nil {
		return nil, &CoinGlassDecodeError{CoinGlassError{msg: fmt.Sprintf("step2 gunzip: %v", err)}}
	}
	plaintext, err := io.ReadAll(gzr2)
	gzr2.Close()
	if err != nil {
		return nil, &CoinGlassDecodeError{CoinGlassError{msg: fmt.Sprintf("step2 gunzip read: %v", err)}}
	}

	var result any
	if err := json.Unmarshal(plaintext, &result); err != nil {
		return nil, &CoinGlassDecodeError{CoinGlassError{msg: fmt.Sprintf("json decode result: %v", err)}}
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// HTTP fetch
// ---------------------------------------------------------------------------

// FetchCoinGlassEndpoint fetches a CoinGlass endpoint and handles decryption.
// Ported from coinglass_client.fetch_coinglass_endpoint.
func FetchCoinGlassEndpoint(path string, params map[string]string, timeout time.Duration) (any, error) {
	baseURL := coinglassBase
	if strings.HasPrefix(path, "http") {
		baseURL = ""
	}
	u, err := url.Parse(baseURL + "/" + strings.TrimLeft(path, "/"))
	if err != nil {
		return nil, &CoinGlassError{msg: fmt.Sprintf("invalid url: %v", err)}
	}
	if len(params) > 0 {
		q := u.Query()
		for k, v := range params {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
	}

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, &CoinGlassError{msg: fmt.Sprintf("create request: %v", err)}
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("cache-ts-v2", fmt.Sprintf("%d", time.Now().UnixMilli()))
	req.Header.Set("encryption", "true")
	req.Header.Set("language", "en")
	req.Header.Set("Origin", "https://www.coinglass.com")
	req.Header.Set("Referer", "https://www.coinglass.com")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) Chrome/125.0.0.0 Safari/537.36")

	cli := &http.Client{Timeout: timeout}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, &CoinGlassAPIError{CoinGlassError{msg: fmt.Sprintf("request failed for %s: %v", path, err)}}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &CoinGlassAPIError{CoinGlassError{msg: fmt.Sprintf("read response for %s: %v", path, err)}}
	}

	// If encrypted headers are present, decrypt.
	user := resp.Header.Get("user")
	version := resp.Header.Get("v")
	if user != "" && version != "" {
		return DecryptCoinGlassResponse(body, user, version, resp.Request.URL.String())
	}

	// Otherwise, try plain JSON.
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, &CoinGlassAPIError{CoinGlassError{msg: fmt.Sprintf("non-json response for %s: %v", path, err)}}
	}
	// If it's a dict with a "data" key and few keys, unwrap.
	if m, ok := payload.(map[string]any); ok {
		if d, has := m["data"]; has && len(m) <= 4 {
			return d, nil
		}
	}
	return payload, nil
}

// ---------------------------------------------------------------------------
// Symbol normalization
// ---------------------------------------------------------------------------

// NormalizeCoinSymbol strips exchange suffixes to get the base coin name.
// Ported from coinglass_client.normalize_coin_symbol.
func NormalizeCoinSymbol(symbol string) (string, error) {
	value := strings.ToUpper(strings.TrimSpace(symbol))
	if value == "" {
		return "", &CoinGlassError{msg: "symbol is required"}
	}
	// Strip after ":".
	if idx := strings.Index(value, ":"); idx >= 0 {
		value = value[:idx]
	}
	// Strip after first separator.
	for _, sep := range []string{"/", "-", "_"} {
		if idx := strings.Index(value, sep); idx >= 0 {
			value = value[:idx]
			break
		}
	}
	// Strip known suffixes.
	for _, suffix := range []string{"USDT", "USDC", "USD", "PERP"} {
		if strings.HasSuffix(value, suffix) && len(value) > len(suffix) {
			return value[:len(value)-len(suffix)], nil
		}
	}
	return value, nil
}
