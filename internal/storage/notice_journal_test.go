package storage

import (
	"os"
	"testing"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestNoticeJournal_PersistenceBudgetAndFeedback(t *testing.T) {
	dir := t.TempDir()
	cfg := types.StorageConfig{DatabasePath: dir + "/kairos.db"}

	nj, err := NewNoticeJournal(cfg)
	if err != nil {
		t.Fatalf("NewNoticeJournal: %v", err)
	}
	if nj.Path() == JournalPath(cfg) {
		t.Fatalf("NoticeJournal must use an independent file, got same path %s", nj.Path())
	}

	base := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	n1 := types.Notice{
		NoticeID:  "ntc-001",
		CreatedAt: base.Add(-2 * time.Hour),
		Symbol:    "SOL/USDT:USDT",
		Exchange:  "okx",
		TriggerFacts: types.NoticeTriggerFacts{
			EventType:       "volume_spike",
			Price:           185.4,
			VolumeRatio:     6.2,
			VolumeWindowMin: 10,
			FactSummary:     "10 分钟量 = 均值 6.2×",
		},
		Env: types.NoticeEnvTags{
			SeasonRegime: types.SeasonRegimeWinter,
			BoxState:     types.NoticeBoxOutside,
			BTCState:     "BTC +0.85% (4h 均线上方)",
		},
		DeliveryStatus: types.NoticeStatusSent,
	}
	n2 := types.Notice{
		NoticeID:  "ntc-002",
		CreatedAt: base.Add(-30 * time.Minute),
		Symbol:    "SOL/USDT:USDT",
		Exchange:  "okx",
		TriggerFacts: types.NoticeTriggerFacts{
			EventType:   "price_velocity",
			FactSummary: "60 秒涨跌 +1.50%",
		},
		Env: types.NoticeEnvTags{
			BoxState: types.NoticeBoxInside,
		},
		DeliveryStatus: types.NoticeStatusSuppressed,
		SuppressReason: types.NoticeSuppressInsideBox,
	}

	if err := nj.SaveNotice(n1); err != nil {
		t.Fatalf("SaveNotice n1: %v", err)
	}
	if err := nj.SaveNotice(n2); err != nil {
		t.Fatalf("SaveNotice n2: %v", err)
	}

	// Trading journal file must still not exist (100% independent stream).
	if _, err := os.Stat(JournalPath(cfg)); !os.IsNotExist(err) {
		t.Fatalf("trading-journal.jsonl should remain untouched, stat err=%v", err)
	}

	got1, err := nj.GetNotice("ntc-001")
	if err != nil || got1 == nil {
		t.Fatalf("GetNotice ntc-001: %v, err=%v", got1, err)
	}
	if got1.Env.WinterDefenseHint == "" {
		t.Fatalf("expected WinterDefenseHint auto-populated for WINTER notice")
	}
	if got1.Disclaimer != types.NoticeDisclaimerLine {
		t.Fatalf("unexpected disclaimer: %q", got1.Disclaimer)
	}

	stats, err := nj.BudgetStats("SOL/USDT:USDT", base, 24*time.Hour)
	if err != nil {
		t.Fatalf("BudgetStats: %v", err)
	}
	if stats.TotalNotices != 2 || stats.SentTotal != 1 || stats.SuppressedTotal != 1 {
		t.Fatalf("unexpected totals: %+v", stats)
	}
	if stats.SentInDayWindow != 1 || !stats.LastSentSymbolAt.Equal(n1.CreatedAt) {
		t.Fatalf("unexpected sent window stats: %+v", stats)
	}
	if stats.SuppressedByReason[types.NoticeSuppressInsideBox] != 1 {
		t.Fatalf("expected inside_box suppression count=1, got %+v", stats.SuppressedByReason)
	}

	// Feedback lifecycle (看了 -> 做了 overwrite by notice_id).
	if err := nj.SaveFeedback(types.NoticeFeedback{
		NoticeID:   "ntc-001",
		Action:     "看了",
		RecordedAt: base.Add(-10 * time.Minute),
	}); err != nil {
		t.Fatalf("SaveFeedback seen: %v", err)
	}
	if err := nj.SaveFeedback(types.NoticeFeedback{
		NoticeID:   "ntc-001",
		Action:     "做了",
		TicketID:   "tkt-abc",
		Note:       "回踩结构确认后人工开票",
		RecordedAt: base,
	}); err != nil {
		t.Fatalf("SaveFeedback acted: %v", err)
	}

	fb, err := nj.GetFeedback("ntc-001")
	if err != nil || fb == nil {
		t.Fatalf("GetFeedback: %v, err=%v", fb, err)
	}
	if fb.Action != types.NoticeFeedbackActed || fb.TicketID != "tkt-abc" {
		t.Fatalf("expected latest feedback acted/tkt-abc, got %+v", fb)
	}
}

// Dedup lookups must stay inside [since, until] and must ignore suppressed
// records (a suppressed window never represents an event).
func TestNoticeJournal_LastAcceptedEventWindow(t *testing.T) {
	dir := t.TempDir()
	nj, err := NewNoticeJournal(types.StorageConfig{DatabasePath: dir + "/kairos.db"})
	if err != nil {
		t.Fatalf("NewNoticeJournal: %v", err)
	}
	base := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	accepted := types.Notice{
		NoticeID:       "ntc-future",
		CreatedAt:      base.Add(10 * time.Minute),
		Symbol:         "SOL/USDT:USDT",
		TriggerFacts:   types.NoticeTriggerFacts{EventType: "price_velocity"},
		DeliveryStatus: types.NoticeStatusDryRun,
	}
	suppressed := types.Notice{
		NoticeID:       "ntc-dup",
		CreatedAt:      base.Add(5 * time.Minute),
		Symbol:         "SOL/USDT:USDT",
		TriggerFacts:   types.NoticeTriggerFacts{EventType: "price_velocity"},
		DeliveryStatus: types.NoticeStatusSuppressed,
		SuppressReason: types.NoticeSuppressDuplicateEvent,
	}
	for _, n := range []types.Notice{accepted, suppressed} {
		if err := nj.SaveNotice(n); err != nil {
			t.Fatalf("SaveNotice %s: %v", n.NoticeID, err)
		}
	}

	// Candidate older than the accepted record: the future record must not match,
	// otherwise an out-of-order event gets silently deduped by an unrelated move.
	if _, found, err := nj.LastAcceptedEvent("SOL/USDT:USDT", "price_velocity", base.Add(-5*time.Minute), base); err != nil || found {
		t.Fatalf("upper bound ignored: found=%v err=%v", found, err)
	}
	// Inclusive window catches the accepted record; the suppressed one is skipped.
	got, found, err := nj.LastAcceptedEvent("SOL/USDT:USDT", "price_velocity", base, base.Add(15*time.Minute))
	if err != nil || !found || got.NoticeID != accepted.NoticeID {
		t.Fatalf("window lookup: got=%+v found=%v err=%v", got, found, err)
	}
	// Only a suppressed candidate inside the window must report "not found".
	if _, found, err := nj.LastAcceptedEvent("SOL/USDT:USDT", "price_velocity", base.Add(4*time.Minute), base.Add(6*time.Minute)); err != nil || found {
		t.Fatalf("suppressed record must not dedup: found=%v err=%v", found, err)
	}
}
