package alert

import (
	"strings"
	"testing"

	"github.com/ArchdevilForge/kairos/internal/types"
)

func scanWithSetups() *types.SignalEnvelope {
	return &types.SignalEnvelope{
		Success: true,
		Data: map[string]any{
			"setups": []map[string]any{
				{"symbol": "AAA/USDT:USDT", "action_state": "watch", "risk": map[string]any{}},
				{
					"symbol": "BBB/USDT:USDT", "direction": "long", "setup_type": "box_breakout",
					"action_state": "prepare", "setup_score": 5.8, "threshold": 5.5,
					"risk": map[string]any{
						"entry_zone": []any{10.0, 10.03}, "structural_stop": 9.5,
						"targets": []any{11.0, 12.0}, "risk_reward": 2.1,
						"max_position_pct": 33.0, "max_leverage": 5.0,
					},
					"reasons": []any{"4h structure is usable"}, "warnings": []any{},
				},
				{
					"symbol": "CCC/USDT:USDT", "direction": "short", "setup_type": "range_breakdown",
					"action_state": "trade_candidate", "setup_score": 7.8, "threshold": 7.5,
					"risk": map[string]any{
						"entry_zone": []any{20.0, 20.1}, "structural_stop": 21.0,
						"targets": []any{18.5}, "risk_reward": 2.4,
						"max_position_pct": 20.0, "max_leverage": 3.0,
					},
					"reasons": []any{
						"1d trend supports short",
						"BTC resonance supports direction",
						"risk/reward 2.40 meets requirement 2.20",
					},
					"warnings": []any{"15m volume confirmation missing"},
				},
			},
		},
	}
}

func TestSelectSetups_DefaultPrepare(t *testing.T) {
	selected := SelectSetups(scanWithSetups(), "prepare", 5)
	if len(selected) != 2 {
		t.Fatalf("expected 2 setups, got %d", len(selected))
	}
	if selected[0]["symbol"] != "CCC/USDT:USDT" {
		t.Fatalf("expected trade_candidate first, got %v", selected[0]["symbol"])
	}
}

func TestFormatAlert_DryRunShape(t *testing.T) {
	selected := SelectSetups(scanWithSetups(), "prepare", 5)
	text := FormatAlert(selected)
	checks := []string{
		"<b>Kairos 机会筛选</b> | <b>非指令</b> 仅供人工判断",
		"<b>[交易候选] CCC 做空</b> | 区间跌破 | 7.8/7.5",
		"<b>匹配</b>: 日线顺势、BTC共振、盈亏比达标",
		"<b>缺口</b>: 缺量能确认",
		"<b>位</b>: 入 20.0 - 20.1 | 损 21.0 | 目 18.5",
		"<b>RR/上限</b>: 2.4 | 20.0% / 3.0x",
	}
	for _, want := range checks {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "CCC/USDT:USDT") {
		t.Fatal("raw symbol should be stripped from alert text")
	}
}
