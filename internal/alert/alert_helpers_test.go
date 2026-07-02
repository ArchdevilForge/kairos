package alert

import (
	"strings"
	"testing"

	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestExtractSetupMaps_Variants(t *testing.T) {
	fromTyped := extractSetupMaps(map[string]any{
		"setups": []map[string]any{{"symbol": "A"}},
	})
	if len(fromTyped) != 1 {
		t.Fatalf("typed setups: %d", len(fromTyped))
	}

	fromAny := extractSetupMaps(map[string]any{
		"setups": []any{map[string]any{"symbol": "B"}, "skip"},
	})
	if len(fromAny) != 1 || fromAny[0]["symbol"] != "B" {
		t.Fatalf("any setups: %+v", fromAny)
	}

	fromQualified := extractSetupMaps(map[string]any{
		"qualified_setups": []any{map[string]any{"symbol": "C"}},
	})
	if len(fromQualified) != 1 {
		t.Fatalf("qualified: %+v", fromQualified)
	}

	if got := extractSetupMaps(nil); got != nil {
		t.Fatalf("nil data: %+v", got)
	}
}

func TestSelectSetups_InvalidMinStateAndLimit(t *testing.T) {
	scan := scanWithSetups()
	got := SelectSetups(scan, "invalid_state", 0)
	if len(got) != 1 {
		t.Fatalf("limit<1 clamps to 1, got %d", len(got))
	}
	if SelectSetups(nil, "prepare", 5) != nil {
		t.Fatal("nil scan")
	}
}

func TestFmtListAndFormatAnyFloat(t *testing.T) {
	if got := fmtList([]float64{1.5, 2.0, 3.0, 4.0}, " - "); got != "1.5 - 2.0 - 3.0" {
		t.Fatalf("float64 slice: %q", got)
	}
	if got := fmtList([]any{1.5, float32(2.0), "x"}, "/"); got != "1.5/2.0/x" {
		t.Fatalf("any slice: %q", got)
	}
	if fmtList(nil, ",") != "-" {
		t.Fatal("nil list")
	}
	if formatAnyFloat(3) != "3.0" {
		t.Fatalf("int: %q", formatAnyFloat(3))
	}
	if formatAnyFloat("n/a") != "n/a" {
		t.Fatalf("string: %q", formatAnyFloat("n/a"))
	}
}

func TestStringListAndJoinOrDash(t *testing.T) {
	if got := stringList([]string{"a", "b"}); len(got) != 2 {
		t.Fatalf("[]string: %v", got)
	}
	if got := stringList([]any{"x", 1}); len(got) != 2 {
		t.Fatalf("[]any: %v", got)
	}
	if joinOrDash(nil, "、") != "-" {
		t.Fatal("joinOrDash empty")
	}
}

func TestStrategyPoints_AllBranches(t *testing.T) {
	matched, _ := strategyPoints(
		[]string{
			"1d trend supports long",
			"4h structure is usable",
			"BTC resonance supports direction",
			"15m trigger is active",
			"15m volume confirms move",
			"risk/reward 2.4 meets requirement 2.2",
			"cycle component=summer",
		},
		nil,
	)
	if len(matched) != 5 {
		t.Fatalf("matched capped to 5: %v", matched)
	}
	_, missing := strategyPoints(nil, []string{
		"1d trend conflicts",
		"4h structure is not usable",
		"BTC resonance conflicts",
		"15m volume confirmation missing",
		"risk/reward 1.5 below requirement 2.0",
		"cycle does not support",
	})
	if len(missing) != 4 {
		t.Fatalf("missing capped to 4: %v", missing)
	}
	_, missing = strategyPoints(nil, []string{"BTC resonance is neutral", "15m price is near trigger"})
	if len(missing) != 1 || missing[0] != "BTC中性" {
		t.Fatalf("neutral btc: %v", missing)
	}
}

func TestDisplayAndTranslations(t *testing.T) {
	if displaySymbol("ETH/USDT:USDT") != "ETH" {
		t.Fatal("displaySymbol")
	}
	cases := map[string]string{
		"long": "做多", "SHORT": "做空", "prepare": "准备",
		"box_breakout": "箱体突破", "unknown": "unknown",
	}
	if directionZH("long") != cases["long"] || stateZH("prepare") != cases["prepare"] {
		t.Fatal("zh maps")
	}
	if setupTypeZH("box_breakout") != cases["box_breakout"] || setupTypeZH("box_support") != "箱体支撑" {
		t.Fatal("setup type")
	}
}

func TestFormatAlert_WatchState(t *testing.T) {
	scan := &types.SignalEnvelope{
		Success: true,
		Data: map[string]any{
			"setups": []map[string]any{
				{
					"symbol": "X/USDT:USDT", "direction": "long", "setup_type": "range_breakout",
					"action_state": "watch", "setup_score": 4.0, "threshold": 5.0,
					"risk": map[string]any{"entry_zone": []float64{1, 2}},
					"reasons": []any{"15m price is near trigger"},
				},
			},
		},
	}
	text := FormatAlert(SelectSetups(scan, "watch", 5))
	if !strings.Contains(text, "[观察]") {
		t.Fatalf("missing watch state: %s", text)
	}
}
