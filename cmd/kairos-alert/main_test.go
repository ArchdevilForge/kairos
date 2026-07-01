package main

import "testing"

func TestParseChatID_Alert(t *testing.T) {
	id, err := parseChatID("123456789")
	if err != nil || id != 123456789 {
		t.Fatalf("got (%d, %v)", id, err)
	}
}

func TestEnvIntOr(t *testing.T) {
	if got := envIntOr("KAIROS_TEST_MISSING_VAR_X", 7); got != 7 {
		t.Fatalf("got %d", got)
	}
}
