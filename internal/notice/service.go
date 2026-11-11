package notice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/ArchdevilForge/kairos/internal/cycle"
	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
)

const (
	DefaultSymbolCooldownMinutes = 240 // 4h (§3 B4)
	DefaultMaxNoticesPerDay      = 12  // 12/day (§3 B4)
)

// TextSender abstracts the Telegram HTML delivery channel (satisfied by *notify.TelegramClient).
type TextSender interface {
	SendText(ctx context.Context, text string) error
}

// Service evaluates detector anomalies into independent Notices, applies box-interior
// and attention-budget suppression, logs to notice-journal.jsonl, and routes to Telegram.
type Service struct {
	mu      sync.Mutex
	cfg     types.NoticeConfig
	journal *storage.NoticeJournal
	sender  TextSender
	nowFunc func() time.Time
}

// NewService constructs a Phase 3c Notice routing service.
func NewService(cfg types.NoticeConfig, journal *storage.NoticeJournal, sender TextSender) *Service {
	if cfg.SymbolCooldownMinutes <= 0 {
		cfg.SymbolCooldownMinutes = DefaultSymbolCooldownMinutes
	}
	if cfg.MaxNoticesPerDay <= 0 {
		cfg.MaxNoticesPerDay = DefaultMaxNoticesPerDay
	}
	return &Service{
		cfg:     cfg,
		journal: journal,
		sender:  sender,
		nowFunc: func() time.Time { return time.Now().UTC() },
	}
}

// SetNowFunc overrides the wall clock for deterministic testing.
func (s *Service) SetNowFunc(fn func() time.Time) {
	if s != nil && fn != nil {
		s.mu.Lock()
		s.nowFunc = fn
		s.mu.Unlock()
	}
}

// ProcessRequest bundles a detector AnomalyEvent with optional causal OHLCV and Season context.
type ProcessRequest struct {
	Event           types.AnomalyEvent
	Candles4h       []types.Candle
	BTCCandles4h    []types.Candle
	SeasonRegime    types.SeasonRegime
	UniverseBars5m  map[string][]cycle.SeasonBar5m
	UniverseFunding map[string]map[int64]float64
}

// ProcessAnomaly converts a qualifying detector AnomalyEvent into a Notice, checks
// suppression gates (inside_box -> symbol 4h cooldown -> 12/day cap), persists to
// notice-journal.jsonl, and sends via Telegram only when unsuppressed (§3 B2/B3/B4).
func (s *Service) ProcessAnomaly(ctx context.Context, req ProcessRequest) (*types.Notice, error) {
	if s == nil || !s.cfg.Enabled {
		return nil, nil
	}
	facts, ok := ExtractTriggerFacts(req.Event)
	if !ok || req.Event.Symbol == "" || req.Event.Symbol == "MARKET" {
		return nil, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.nowFunc().UTC()
	if req.Event.Timestamp > 0 {
		sec := int64(req.Event.Timestamp)
		nsec := int64((req.Event.Timestamp - float64(sec)) * 1e9)
		now = time.Unix(sec, nsec).UTC()
	}

	env := EvaluateEnvTags(EnvInput{
		EventUnixSec:    now.Unix(),
		CurrentPrice:    facts.Price,
		Candles4h:       req.Candles4h,
		BTCCandles4h:    req.BTCCandles4h,
		SeasonRegime:    req.SeasonRegime,
		UniverseBars5m:  req.UniverseBars5m,
		UniverseFunding: req.UniverseFunding,
	})
	scenes := DetectScenes(SceneInput{
		Symbol:       req.Event.Symbol,
		CurrentPrice: facts.Price,
		TriggerFacts: facts,
		Candles4h:    req.Candles4h,
		BTCCandles4h: req.BTCCandles4h,
	})

	ntc := types.Notice{
		SchemaVersion: types.NoticeSchemaVersion,
		NoticeID:      buildNoticeID(req.Event, now),
		CreatedAt:     now,
		Symbol:        req.Event.Symbol,
		Exchange:      req.Event.Exchange,
		TriggerFacts:  facts,
		Scenes:        scenes,
		Env:           env,
		Disclaimer:    types.NoticeDisclaimerLine,
	}
	ntc.RenderedTelegram = FormatNoticeTelegram(ntc)

	// Gate 1: Box-interior suppression (default true, configurable via notice.suppressInsideBox, §3 B3).
	if s.cfg.ShouldSuppressInsideBox() && env.BoxState == types.NoticeBoxInside {
		ntc.DeliveryStatus = types.NoticeStatusSuppressed
		ntc.SuppressReason = types.NoticeSuppressInsideBox
		ntc.SuppressDetail = fmt.Sprintf("4h 箱体内部抑制 (宽度 %.1f%%, 位置 %.2f)", env.BoxWidthPct, env.BoxPos)
		return s.persistOnly(ntc)
	}

	// Gate 2 & 3: Per-symbol cooldown (default 4h) & daily cap (default 12) from notice-journal.jsonl (§3 B4).
	if s.journal != nil {
		stats, err := s.journal.BudgetStats(ntc.Symbol, now, 24*time.Hour)
		if err != nil {
			return nil, fmt.Errorf("notice budget stats: %w", err)
		}
		cooldown := time.Duration(s.cfg.SymbolCooldownMinutes) * time.Minute
		if cooldown > 0 && !stats.LastSentSymbolAt.IsZero() {
			elapsed := now.Sub(stats.LastSentSymbolAt)
			if elapsed >= 0 && elapsed < cooldown {
				ntc.DeliveryStatus = types.NoticeStatusSuppressed
				ntc.SuppressReason = types.NoticeSuppressSymbolCooldown
				ntc.SuppressDetail = fmt.Sprintf("同币冷却未满 (%s < %s)", elapsed.Round(time.Minute), cooldown)
				return s.persistOnly(ntc)
			}
		}
		if s.cfg.MaxNoticesPerDay > 0 && stats.SentInDayWindow >= s.cfg.MaxNoticesPerDay {
			ntc.DeliveryStatus = types.NoticeStatusSuppressed
			ntc.SuppressReason = types.NoticeSuppressDailyCap
			ntc.SuppressDetail = fmt.Sprintf("24h 通知预算已满 (%d/%d)", stats.SentInDayWindow, s.cfg.MaxNoticesPerDay)
			return s.persistOnly(ntc)
		}
	}

	if s.sender == nil {
		ntc.DeliveryStatus = types.NoticeStatusDryRun
		return s.persistOnly(ntc)
	}

	if err := s.sender.SendText(ctx, ntc.RenderedTelegram); err != nil {
		ntc.DeliveryStatus = types.NoticeStatusFailed
		ntc.DeliveryError = err.Error()
		if s.journal != nil {
			_ = s.journal.SaveNotice(ntc)
		}
		return &ntc, fmt.Errorf("send notice telegram: %w", err)
	}

	ntc.DeliveryStatus = types.NoticeStatusSent
	return s.persistOnly(ntc)
}

func (s *Service) persistOnly(ntc types.Notice) (*types.Notice, error) {
	if s.journal != nil {
		if err := s.journal.SaveNotice(ntc); err != nil {
			return &ntc, err
		}
	}
	return &ntc, nil
}

func buildNoticeID(evt types.AnomalyEvent, ts time.Time) string {
	seed := fmt.Sprintf("%s|%s|%s|%s|%d",
		evt.Exchange, evt.Symbol, evt.EventType, evt.EventID, ts.UnixNano())
	sum := sha256.Sum256([]byte(seed))
	return fmt.Sprintf("ntc-%s-%s", ts.UTC().Format("20060102"), hex.EncodeToString(sum[:4]))
}
