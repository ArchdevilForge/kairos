package engine

import (
	"context"
	"testing"
	"time"

	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestPipeline_NoticeHook_LogsIndependentNoticeJournal(t *testing.T) {
	dir := t.TempDir()
	cfg := &types.Config{
		Storage: types.StorageConfig{DatabasePath: dir + "/kairos.db"},
		Notice: types.NoticeConfig{
			Enabled:               true,
			SymbolCooldownMinutes: 240,
			MaxNoticesPerDay:      12,
		},
	}

	p := NewPipeline(cfg, nil)
	if p.notice == nil {
		t.Fatalf("expected p.notice initialized when cfg.Notice.Enabled=true")
	}

	now := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	p.deliverEvent(context.Background(), types.AnomalyEvent{
		Symbol:    "SOL/USDT:USDT",
		EventType: "volume_spike",
		Exchange:  "okx",
		Timestamp: float64(now.Unix()),
		Data:      map[string]any{"price": 188.5, "ratio": 6.4, "window_minutes": 5},
	})

	nj, err := storage.NewNoticeJournal(cfg.Storage)
	if err != nil {
		t.Fatalf("NewNoticeJournal: %v", err)
	}
	notices, err := nj.ListNotices()
	if err != nil {
		t.Fatalf("ListNotices: %v", err)
	}
	if len(notices) != 1 {
		t.Fatalf("expected 1 notice persisted in notice-journal.jsonl, got %d", len(notices))
	}
	if notices[0].Symbol != "SOL/USDT:USDT" || notices[0].DeliveryStatus != types.NoticeStatusDryRun {
		t.Fatalf("unexpected persisted notice: %+v", notices[0])
	}
}
