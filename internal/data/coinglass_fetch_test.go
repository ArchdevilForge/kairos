package data

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchCoinGlassEndpoint_PlainJSON(t *testing.T) {
	t.Setenv(envCoinGlassUsePy, "0")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"ok": true}})
	}))
	defer srv.Close()

	got, err := fetchCoinGlassNative(srv.URL, nil, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := got.(map[string]any)
	if !ok || m["ok"] != true {
		t.Fatalf("payload: %+v", got)
	}
}

func TestFetchCoinGlassEndpoint_Encrypted(t *testing.T) {
	t.Setenv(envCoinGlassUsePy, "0")
	expectedJSON := `{"rsi":55.5}`
	actualKey := "0123456789abcdef0123456789abcdef"
	body, userToken, err := buildCoinGlassResponse(expectedJSON, actualKey, keyTable["55"], "55")
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("user", userToken)
		w.Header().Set("v", "55")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	got, err := fetchCoinGlassNative(srv.URL, nil, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("type: %T", got)
	}
	if m["rsi"] != 55.5 {
		t.Fatalf("decrypted: %+v", m)
	}
}

func TestEncodeCoinGlassParams(t *testing.T) {
	if encodeCoinGlassParams(nil) != "" {
		t.Fatal("nil params")
	}
	got := encodeCoinGlassParams(map[string]string{"a": "1", "b": "2"})
	if got != "a=1,b=2" && got != "b=2,a=1" {
		t.Fatalf("params: %q", got)
	}
}

func TestUnwrapCoinGlassPayload(t *testing.T) {
	inner := unwrapCoinGlassPayload(map[string]any{"data": map[string]any{"x": 1}, "code": "0"})
	if m, ok := inner.(map[string]any); !ok || m["x"] != 1 {
		t.Fatalf("unwrap: %+v", inner)
	}
}
