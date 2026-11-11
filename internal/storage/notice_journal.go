package storage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// Independent Notice journal record kinds (§3 B2/B4/B5).
const (
	NoticeJournalKindNotice   = "notice"
	NoticeJournalKindFeedback = "notice_feedback"
)

// NoticeJournalPath returns the dedicated JSONL path for Phase 3c notices & feedback,
// completely separate from trading-journal.jsonl (§3 B2).
func NoticeJournalPath(cfg types.StorageConfig) string {
	return filepath.Join(Dir(cfg), "notice-journal.jsonl")
}

// NoticeJournal is an append-only JSONL store for Opportunity Notices and owner feedback.
type NoticeJournal struct {
	path string
	mu   sync.Mutex
	meta ResearchMeta
}

// NewNoticeJournal opens or creates the notice journal directory and handle.
func NewNoticeJournal(cfg types.StorageConfig) (*NoticeJournal, error) {
	dir := Dir(cfg)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("notice journal mkdir: %w", err)
	}
	return &NoticeJournal{path: NoticeJournalPath(cfg)}, nil
}

// NewNoticeJournalAt opens a NoticeJournal at an explicit file path (for CLI/research/tests).
func NewNoticeJournalAt(path string) (*NoticeJournal, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("notice journal mkdir: %w", err)
	}
	return &NoticeJournal{path: path}, nil
}

// SetMetadata attaches reproducibility metadata to subsequent lines.
func (nj *NoticeJournal) SetMetadata(m ResearchMeta) {
	if nj == nil {
		return
	}
	nj.mu.Lock()
	defer nj.mu.Unlock()
	nj.meta = m
}

// Path returns the underlying JSONL file path.
func (nj *NoticeJournal) Path() string {
	if nj == nil {
		return ""
	}
	return nj.path
}

func (nj *NoticeJournal) append(kind, id string, at time.Time, payload any) error {
	if nj == nil {
		return nil
	}
	if at.IsZero() {
		at = time.Now().UTC()
	} else {
		at = at.UTC()
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	nj.mu.Lock()
	defer nj.mu.Unlock()
	rec := journalLine{
		Kind:            kind,
		ID:              id,
		At:              at,
		Payload:         raw,
		GitSHA:          nj.meta.GitSHA,
		ConfigHash:      nj.meta.ConfigHash,
		StrategyVersion: nj.meta.StrategyVersion,
		ExperimentID:    nj.meta.ExperimentID,
		Mode:            nj.meta.Mode,
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(nj.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.Write(append(line, '\n'))
	return err
}

// SaveNotice persists a Notice record (sent, suppressed, dry_run, or failed).
func (nj *NoticeJournal) SaveNotice(n types.Notice) error {
	if strings.TrimSpace(n.NoticeID) == "" {
		return fmt.Errorf("notice_id required")
	}
	if n.SchemaVersion == "" {
		n.SchemaVersion = types.NoticeSchemaVersion
	}
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now().UTC()
	} else {
		n.CreatedAt = n.CreatedAt.UTC()
	}
	n.Env.SeasonRegime = types.NormalizeSeasonRegime(n.Env.SeasonRegime)
	n.Env.WinterDefenseHint = types.WinterDefenseHintFor(n.Env.SeasonRegime)
	if n.Env.BoxState == "" {
		n.Env.BoxState = types.NoticeBoxUnknown
	}
	if n.Disclaimer == "" {
		n.Disclaimer = types.NoticeDisclaimerLine
	}
	return nj.append(NoticeJournalKindNotice, n.NoticeID, n.CreatedAt, n)
}

// SaveFeedback persists an owner feedback record (看了 / 做了 / 忽略) keyed by notice_id (§3 B5).
func (nj *NoticeJournal) SaveFeedback(fb types.NoticeFeedback) error {
	if strings.TrimSpace(fb.NoticeID) == "" {
		return fmt.Errorf("notice_id required")
	}
	act, ok := types.ParseNoticeFeedbackAction(string(fb.Action))
	if !ok {
		return fmt.Errorf("invalid notice feedback action %q", fb.Action)
	}
	fb.Action = act
	if fb.SchemaVersion == "" {
		fb.SchemaVersion = types.NoticeFeedbackSchemaVersion
	}
	if fb.RecordedAt.IsZero() {
		fb.RecordedAt = time.Now().UTC()
	} else {
		fb.RecordedAt = fb.RecordedAt.UTC()
	}
	return nj.append(NoticeJournalKindFeedback, fb.NoticeID, fb.RecordedAt, fb)
}

func (nj *NoticeJournal) readAll() ([]types.Notice, map[string]types.NoticeFeedback, error) {
	if nj == nil {
		return nil, nil, nil
	}
	nj.mu.Lock()
	defer nj.mu.Unlock()

	f, err := os.Open(nj.path)
	if os.IsNotExist(err) {
		return nil, map[string]types.NoticeFeedback{}, nil
	}
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = f.Close() }()

	byNoticeID := make(map[string]types.Notice)
	feedbacks := make(map[string]types.NoticeFeedback)

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		raw := strings.TrimSpace(sc.Text())
		if raw == "" {
			continue
		}
		var line journalLine
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			continue
		}
		switch line.Kind {
		case NoticeJournalKindNotice:
			var n types.Notice
			if err := json.Unmarshal(line.Payload, &n); err == nil && n.NoticeID != "" {
				byNoticeID[n.NoticeID] = n
			}
		case NoticeJournalKindFeedback:
			var fb types.NoticeFeedback
			if err := json.Unmarshal(line.Payload, &fb); err == nil && fb.NoticeID != "" {
				feedbacks[fb.NoticeID] = fb
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, nil, err
	}

	notices := make([]types.Notice, 0, len(byNoticeID))
	for _, n := range byNoticeID {
		notices = append(notices, n)
	}
	sort.Slice(notices, func(i, j int) bool {
		if notices[i].CreatedAt.Equal(notices[j].CreatedAt) {
			return notices[i].NoticeID < notices[j].NoticeID
		}
		return notices[i].CreatedAt.Before(notices[j].CreatedAt)
	})
	return notices, feedbacks, nil
}

// ListNotices returns all persisted notices ordered chronologically.
func (nj *NoticeJournal) ListNotices() ([]types.Notice, error) {
	notices, _, err := nj.readAll()
	return notices, err
}

// GetNotice looks up a single Notice by notice_id.
func (nj *NoticeJournal) GetNotice(noticeID string) (*types.Notice, error) {
	notices, _, err := nj.readAll()
	if err != nil {
		return nil, err
	}
	for i := range notices {
		if notices[i].NoticeID == noticeID {
			cp := notices[i]
			return &cp, nil
		}
	}
	return nil, nil
}

// ListFeedbacks returns the latest owner feedback per notice_id.
func (nj *NoticeJournal) ListFeedbacks() (map[string]types.NoticeFeedback, error) {
	_, fbs, err := nj.readAll()
	return fbs, err
}

// GetFeedback looks up the latest owner feedback for noticeID.
func (nj *NoticeJournal) GetFeedback(noticeID string) (*types.NoticeFeedback, error) {
	_, fbs, err := nj.readAll()
	if err != nil {
		return nil, err
	}
	fb, ok := fbs[noticeID]
	if !ok {
		return nil, nil
	}
	return &fb, nil
}

// NoticeBudgetStats summarizes delivery & suppression counts from notice-journal.jsonl (§3 B4).
type NoticeBudgetStats struct {
	LastSentSymbolAt   time.Time
	SentInDayWindow    int
	TotalNotices       int
	SentTotal          int
	SuppressedTotal    int
	SuppressedByReason map[string]int
}

// BudgetStats computes symbol cooldown timestamp, rolling day-window sent count,
// and suppression counters from notice-journal.jsonl.
func (nj *NoticeJournal) BudgetStats(symbol string, now time.Time, dayWindow time.Duration) (NoticeBudgetStats, error) {
	stats := NoticeBudgetStats{
		SuppressedByReason: make(map[string]int),
	}
	notices, _, err := nj.readAll()
	if err != nil {
		return stats, err
	}
	if dayWindow <= 0 {
		dayWindow = 24 * time.Hour
	}
	cutoff := now.Add(-dayWindow)
	stats.TotalNotices = len(notices)
	for _, n := range notices {
		switch n.DeliveryStatus {
		case types.NoticeStatusSent:
			stats.SentTotal++
			if !n.CreatedAt.After(now) && !n.CreatedAt.Before(cutoff) {
				stats.SentInDayWindow++
			}
			if symbol != "" && n.Symbol == symbol && !n.CreatedAt.After(now) && n.CreatedAt.After(stats.LastSentSymbolAt) {
				stats.LastSentSymbolAt = n.CreatedAt
			}
		case types.NoticeStatusSuppressed:
			stats.SuppressedTotal++
			reason := n.SuppressReason
			if reason == "" {
				reason = "unspecified"
			}
			stats.SuppressedByReason[reason]++
		}
	}
	return stats, nil
}
