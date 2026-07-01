package notify

import (
	"strings"
	"testing"

	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestFormatEvent_HumanDecisionOnly(t *testing.T) {
	text := formatEvent(types.AlertEvent{
		Event:     "open_interest_change",
		Symbol:    "ETH/USDT:USDT",
		Price:     3000,
		Condition: "oi_change=6%",
		Severity:  types.SeverityHigh,
		ChangePct: 6,
		Timestamp: "2026-06-27T01:02:03+00:00",
	})
	if !strings.Contains(text, "<b>非指令</b> 仅供人工判断") {
		t.Fatalf("missing disclaimer: %s", text)
	}
	if !strings.Contains(text, "<b>[高] ETH 持仓量异动</b>") {
		t.Fatalf("missing header: %s", text)
	}
	if !strings.Contains(text, "<b>价/变</b>: 3000.00 / +6.00%") {
		t.Fatalf("missing price line: %s", text)
	}
	if strings.Contains(text, "ETH/USDT:USDT") {
		t.Fatal("raw symbol should be stripped")
	}
}

func TestNewTelegramClient_MissingToken(t *testing.T) {
	_, err := NewTelegramClient("", 123)
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestNewTelegramClient_MissingChatID(t *testing.T) {
	_, err := NewTelegramClient("token", 0)
	if err == nil {
		t.Fatal("expected error for zero chat id")
	}
}
