package main

import "testing"

func TestParseChatID(t *testing.T) {
	id, err := parseChatID("-1001234567890")
	if err != nil || id != -1001234567890 {
		t.Fatalf("got (%d, %v)", id, err)
	}
	_, err = parseChatID("not-a-number")
	if err == nil {
		t.Fatal("expected error")
	}
}
