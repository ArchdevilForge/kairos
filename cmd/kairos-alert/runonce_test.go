package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/ArchdevilForge/kairos/internal/config"
	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestRunOnce_DryRunWithSetups(t *testing.T) {
	old := scanMarketFn
	defer func() { scanMarketFn = old }()

	scanMarketFn = func(_ context.Context, _ *types.Config, _ string) *types.SignalEnvelope {
		return &types.SignalEnvelope{
			Success: true,
			Data: map[string]any{
				"setups": []map[string]any{
					{
						"symbol": "BTC/USDT:USDT", "direction": "long", "setup_type": "box_breakout",
						"action_state": "prepare", "setup_score": 6.0, "threshold": 5.5,
						"risk": map[string]any{"entry_zone": []any{1.0}, "structural_stop": 0.9, "targets": []any{1.2}},
					},
				},
			},
		}
	}

	cfg, err := config.LoadString("")
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	oldOut := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	code := runOnce(context.Background(), cfg, "", "prepare", 5, true)
	w.Close()
	os.Stdout = oldOut
	_, _ = io.Copy(&buf, r)

	if code != 0 {
		t.Fatalf("exit code: %d", code)
	}
	if !strings.Contains(buf.String(), "Kairos 机会筛选") {
		t.Fatalf("output: %s", buf.String())
	}
}

func TestRunOnce_ScanFailed(t *testing.T) {
	old := scanMarketFn
	defer func() { scanMarketFn = old }()
	scanMarketFn = func(context.Context, *types.Config, string) *types.SignalEnvelope {
		return &types.SignalEnvelope{Success: false, Errors: []string{"boom"}}
	}
	cfg, _ := config.LoadString("")
	if code := runOnce(context.Background(), cfg, "", "prepare", 5, true); code != 1 {
		t.Fatalf("code: %d", code)
	}
}

func TestRunOnce_NoSetups(t *testing.T) {
	old := scanMarketFn
	defer func() { scanMarketFn = old }()
	scanMarketFn = func(context.Context, *types.Config, string) *types.SignalEnvelope {
		return &types.SignalEnvelope{
			Success: true,
			Data: map[string]any{
				"setups": []map[string]any{
					{"symbol": "X/USDT:USDT", "action_state": "watch"},
				},
			},
		}
	}
	cfg, _ := config.LoadString("")
	if code := runOnce(context.Background(), cfg, "", "prepare", 5, true); code != 0 {
		t.Fatalf("code: %d", code)
	}
}

func TestRunOnce_MissingTelegram(t *testing.T) {
	old := scanMarketFn
	defer func() { scanMarketFn = old }()
	scanMarketFn = func(context.Context, *types.Config, string) *types.SignalEnvelope {
		return &types.SignalEnvelope{
			Success: true,
			Data: map[string]any{
				"setups": []map[string]any{
					{"symbol": "BTC/USDT:USDT", "action_state": "prepare", "risk": map[string]any{}},
				},
			},
		}
	}
	cfg, _ := config.LoadString("")
	if code := runOnce(context.Background(), cfg, "", "prepare", 5, false); code != 2 {
		t.Fatalf("code: %d", code)
	}
}

func TestEnvOr(t *testing.T) {
	if envOr("KAIROS_TEST_MISSING_ENV_X", "fallback") != "fallback" {
		t.Fatal("fallback")
	}
}
