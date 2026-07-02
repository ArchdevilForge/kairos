package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

func TestPrintJSON(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	printJSON(map[string]any{"ok": true, "n": 1})
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["ok"] != true {
		t.Fatalf("decoded: %+v", decoded)
	}
}

func TestSplitSymbols_EmptyParts(t *testing.T) {
	got := splitSymbols(" , , ")
	if len(got) != 0 {
		t.Fatalf("got %v", got)
	}
	if !strings.Contains(strings.Join(splitSymbols("A,B"), ","), "A") {
		t.Fatal("basic split")
	}
}
