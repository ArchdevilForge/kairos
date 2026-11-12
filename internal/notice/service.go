package notice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/ArchdevilForge/kairos/internal/cycle"
	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
)

const (
	DefaultSymbolCooldownMinutes = 240 // 4h (§3 B4)
	DefaultMaxNoticesPerDay      = 12  // 12/day (§3 B4)
	// DefaultEventDedupSeconds collapses one underlying move reported by several
	// velocity windows (60s then 120s) into one Notice. It is only the config
	// default (config setDefaults): notice.eventDedupSeconds=0 disables dedup.
	DefaultEventDedupSeconds = 300 // 5m
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

	// coalesce holds candidates briefly so competing windows of the same
	// symbol+event type collapse into the most informative one; pending is keyed
	// by "symbol|eventType".
	coalesce time.Duration
	pending  map[string]*pendingNotice
}

// NewService constructs a Phase 3c Notice routing service.
func NewService(cfg types.NoticeConfig, journal *storage.NoticeJournal, sender TextSender) *Service {
	if cfg.SymbolCooldownMinutes <= 0 {
		cfg.SymbolCooldownMinutes = DefaultSymbolCooldownMinutes
	}
	if cfg.MaxNoticesPerDay <= 0 {
		cfg.MaxNoticesPerDay = DefaultMaxNoticesPerDay
	}
	if cfg.EventDedupSeconds < 0 {
		cfg.EventDedupSeconds = 0
	}
	if cfg.EventCoalesceSeconds < 0 {
		cfg.EventCoalesceSeconds = 0
	}
	return &Service{
		cfg:      cfg,
		journal:  journal,
		sender:   sender,
		nowFunc:  func() time.Time { return time.Now().UTC() },
		coalesce: time.Duration(cfg.EventCoalesceSeconds) * time.Second,
		pending:  make(map[string]*pendingNotice),
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
// suppression gates (inside_box -> same-event coalescing/dedup -> symbol 4h cooldown
// -> 12/day cap), persists to notice-journal.jsonl, and sends via Telegram only when
// unsuppressed (§3 B2/B3/B4).
//
// With notice.eventCoalesceSeconds > 0 the gates and delivery run when the
// coalescing bucket flushes (Service.Close flushes on shutdown), so the returned
// Notice may carry types.NoticeStatusCoalescing instead of a final status.
//
// Cooldown and the daily cap are consumed by every accepted Notice — journaled and
// marked for delivery — so they hold with no Telegram channel configured and after a
// failed send, not only on successful delivery.
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

	// Gate 2: Same-event coalescing (notice.eventCoalesceSeconds). Several velocity
	// windows (60s then 120s) report the same underlying move; holding the notice
	// briefly lets the most informative window represent it instead of whichever
	// fired first. Displaced windows are journaled as suppressed, never dropped.
	if s.journal != nil && s.coalesce > 0 {
		return s.coalesceCandidate(ctx, ntc)
	}
	return s.deliverLocked(ctx, ntc, now)
}

// deliverLocked applies the same-event dedup, symbol cooldown and daily cap gates
// to ntc and delivers it. The caller must hold s.mu.
func (s *Service) deliverLocked(ctx context.Context, ntc types.Notice, now time.Time) (*types.Notice, error) {
	// Gate 3: Same-event dedup — one underlying move reported by several velocity
	// windows (60s and 120s) becomes one Notice; the rest stay in the journal as
	// suppressed so the accounting is auditable instead of silently dropped.
	if s.journal != nil && s.cfg.EventDedupSeconds > 0 {
		since := now.Add(-time.Duration(s.cfg.EventDedupSeconds) * time.Second)
		rep, found, err := s.journal.LastAcceptedEvent(ntc.Symbol, ntc.TriggerFacts.EventType, since, now)
		if err != nil {
			return nil, fmt.Errorf("notice event dedup: %w", err)
		}
		if found {
			loser := suppressedDuplicate(ntc, rep)
			return s.persistOnly(loser)
		}
	}

	// Gate 4 & 5: Per-symbol cooldown (default 4h) & daily cap (default 12) from
	// notice-journal.jsonl (§3 B4). Both count accepted notices, not deliveries.
	if s.journal != nil {
		stats, err := s.journal.BudgetStats(ntc.Symbol, now, 24*time.Hour)
		if err != nil {
			return nil, fmt.Errorf("notice budget stats: %w", err)
		}
		cooldown := time.Duration(s.cfg.SymbolCooldownMinutes) * time.Minute
		if cooldown > 0 && !stats.LastAcceptedSymbolAt.IsZero() {
			elapsed := now.Sub(stats.LastAcceptedSymbolAt)
			if elapsed >= 0 && elapsed < cooldown {
				ntc.DeliveryStatus = types.NoticeStatusSuppressed
				ntc.SuppressReason = types.NoticeSuppressSymbolCooldown
				ntc.SuppressDetail = fmt.Sprintf("同币冷却未满 (%s < %s)", elapsed.Round(time.Minute), cooldown)
				return s.persistOnly(ntc)
			}
		}
		if s.cfg.MaxNoticesPerDay > 0 && stats.AcceptedInDayWindow >= s.cfg.MaxNoticesPerDay {
			ntc.DeliveryStatus = types.NoticeStatusSuppressed
			ntc.SuppressReason = types.NoticeSuppressDailyCap
			ntc.SuppressDetail = fmt.Sprintf("24h 通知预算已满 (%d/%d)", stats.AcceptedInDayWindow, s.cfg.MaxNoticesPerDay)
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

// pendingNotice is one coalescing bucket: the most informative candidate so far
// plus the windows it displaced. Nothing is journaled until the bucket flushes.
type pendingNotice struct {
	best   types.Notice
	extras []types.Notice
	timer  *time.Timer
}

// coalesceCandidate buckets ntc by symbol+event type for the coalescing interval.
// A more informative window replaces the current representative; the replaced one
// is kept and journaled as suppressed when the bucket flushes.
func (s *Service) coalesceCandidate(ctx context.Context, ntc types.Notice) (*types.Notice, error) {
	key := ntc.Symbol + "|" + ntc.TriggerFacts.EventType
	g, ok := s.pending[key]
	if !ok {
		g = &pendingNotice{best: ntc}
		s.pending[key] = g
		g.timer = time.AfterFunc(s.coalesce, func() { s.flush(key, ctx) })
		ntc.DeliveryStatus = types.NoticeStatusCoalescing
		return &ntc, nil
	}
	if moreInformative(ntc.TriggerFacts, g.best.TriggerFacts) {
		g.extras = append(g.extras, suppressedDuplicate(g.best, ntc))
		g.best = ntc
		ntc.DeliveryStatus = types.NoticeStatusCoalescing
		return &ntc, nil
	}
	loser := suppressedDuplicate(ntc, g.best)
	g.extras = append(g.extras, loser)
	return &loser, nil
}

// flush delivers the coalesced representative and journals the windows it displaced.
func (s *Service) flush(key string, ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.pending[key]
	if !ok {
		return
	}
	delete(s.pending, key)
	if g.timer != nil {
		g.timer.Stop()
	}
	for _, loser := range g.extras {
		if s.journal != nil {
			_ = s.journal.SaveNotice(loser)
		}
	}
	// Gates are evaluated against the representative window event time, exactly
	// as the immediate path does.
	_, _ = s.deliverLocked(ctx, g.best, g.best.CreatedAt)
}

// Close flushes every pending coalescing bucket so shutdown never drops a notice
// silently.
func (s *Service) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	keys := make([]string, 0, len(s.pending))
	for k := range s.pending {
		keys = append(keys, k)
	}
	s.mu.Unlock()

	if len(keys) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, k := range keys {
		s.flush(k, ctx)
	}
}

// suppressedDuplicate marks candidate as the suppressed twin of rep and keeps the
// window comparison in the journal instead of dropping the record.
func suppressedDuplicate(candidate, rep types.Notice) types.Notice {
	candidate.DeliveryStatus = types.NoticeStatusSuppressed
	candidate.SuppressReason = types.NoticeSuppressDuplicateEvent
	candidate.SuppressDetail = duplicateEventDetail(candidate.TriggerFacts, rep)
	return candidate
}

func (s *Service) persistOnly(ntc types.Notice) (*types.Notice, error) {
	if s.journal != nil {
		if err := s.journal.SaveNotice(ntc); err != nil {
			return &ntc, err
		}
	}
	return &ntc, nil
}

// duplicateEventDetail explains why a candidate Notice is a duplicate and which
// window carried more information, so the journal shows the comparison instead of
// a bare drop.
func duplicateEventDetail(facts types.NoticeTriggerFacts, rep types.Notice) string {
	hint := "，本窗口信息量更低"
	switch {
	case moreInformative(facts, rep.TriggerFacts):
		// The earlier window already shipped; a Notice cannot be recalled, so the
		// accepted one keeps representing the event and this stays journal-only.
		hint = "，本窗口信息量更高（已按实时序计入代表 Notice）"
	case eventMagnitude(facts) == eventMagnitude(rep.TriggerFacts) && facts.VelocityWindowSec == rep.TriggerFacts.VelocityWindowSec:
		hint = "，两窗口信息量相当"
	}
	return fmt.Sprintf("同事件重复：本窗口 %s，代表 Notice %s（%s）%s",
		triggerWindowLabel(facts), rep.NoticeID, triggerWindowLabel(rep.TriggerFacts), hint)
}

// triggerWindowLabel renders the window/magnitude a Notice was triggered by.
func triggerWindowLabel(f types.NoticeTriggerFacts) string {
	switch f.EventType {
	case "volume_spike":
		return fmt.Sprintf("%d 分钟量 %.2f×", f.VolumeWindowMin, f.VolumeRatio)
	case "price_velocity":
		return fmt.Sprintf("%d 秒 %+.2f%%", f.VelocityWindowSec, f.PriceChangePct)
	case "open_interest_change":
		return fmt.Sprintf("OI %+.2f%%", f.OIChangePct)
	case "resonance":
		return fmt.Sprintf("共振 %d 维", f.DimensionCount)
	default:
		return f.EventType
	}
}

// eventMagnitude is the information content of a trigger, independent of window.
func eventMagnitude(f types.NoticeTriggerFacts) float64 {
	switch f.EventType {
	case "volume_spike":
		return f.VolumeRatio
	case "open_interest_change":
		return math.Abs(f.OIChangePct)
	case "resonance":
		return float64(f.DimensionCount)
	default:
		return math.Abs(f.PriceChangePct)
	}
}

// moreInformative ranks two triggers by magnitude first, longer window second.
func moreInformative(a, b types.NoticeTriggerFacts) bool {
	am, bm := eventMagnitude(a), eventMagnitude(b)
	if am != bm {
		return am > bm
	}
	return a.VelocityWindowSec > b.VelocityWindowSec
}

func buildNoticeID(evt types.AnomalyEvent, ts time.Time) string {
	seed := fmt.Sprintf("%s|%s|%s|%s|%d",
		evt.Exchange, evt.Symbol, evt.EventType, evt.EventID, ts.UnixNano())
	sum := sha256.Sum256([]byte(seed))
	return fmt.Sprintf("ntc-%s-%s", ts.UTC().Format("20060102"), hex.EncodeToString(sum[:4]))
}
