package notice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
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

	// coalesce holds candidates briefly so competing windows of the same
	// symbol+event type collapse into the most informative one; pending is keyed
	// by "symbol|eventType".
	coalesce time.Duration
	pending  map[string]*pendingNotice
	log      *slog.Logger
	// save is the journal write seam (defaults to journal.SaveNotice) so tests can
	// inject write failures and prove nothing is dropped silently.
	save func(types.Notice) error
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
	svc := &Service{
		cfg:      cfg,
		journal:  journal,
		sender:   sender,
		nowFunc:  func() time.Time { return time.Now().UTC() },
		coalesce: time.Duration(cfg.EventCoalesceSeconds) * time.Second,
		pending:  make(map[string]*pendingNotice),
		log:      slog.Default().With("component", "notice"),
	}
	if journal != nil {
		svc.save = journal.SaveNotice
	}
	return svc
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
		return s.coalesceCandidate(ntc)
	}
	return s.deliverNow(ctx, ntc, now)
}

// deliverOutcome separates "the record is durable" from "a delivery was attempted".
// A bucket may retry the journal write, but must never resend a notice that a
// previous flush already handed to the delivery channel.
type deliverOutcome struct {
	notice    types.Notice // record to journal
	attempted bool         // gate/dry-run/send decision already ran; do not recompute
	journaled bool         // the record durably reached the journal
	err       error
}

// deliverNow is the synchronous entry point: it delivers and maps the outcome back
// onto the (*Notice, error) contract used by callers.
func (s *Service) deliverNow(ctx context.Context, ntc types.Notice, now time.Time) (*types.Notice, error) {
	out := s.deliverLocked(ctx, ntc, now)
	if !out.attempted {
		return nil, out.err
	}
	return &out.notice, out.err
}

// deliverLocked applies the same-event dedup, symbol cooldown and daily cap gates
// to ntc and delivers it. The caller must hold s.mu.
func (s *Service) deliverLocked(ctx context.Context, ntc types.Notice, now time.Time) deliverOutcome {
	// Gate 3: Same-event dedup — one underlying move reported by several velocity
	// windows (60s and 120s) becomes one Notice; the rest stay in the journal as
	// suppressed so the accounting is auditable instead of silently dropped.
	if s.journal != nil && s.cfg.EventDedupSeconds > 0 {
		since := now.Add(-time.Duration(s.cfg.EventDedupSeconds) * time.Second)
		rep, found, err := s.journal.LastAcceptedEvent(ntc.Symbol, ntc.TriggerFacts.EventType, since, now)
		if err != nil {
			// Nothing was decided or sent: the caller may safely retry from scratch.
			return deliverOutcome{err: fmt.Errorf("notice event dedup: %w", err)}
		}
		if found {
			return s.recordOutcome(suppressedDuplicate(ntc, rep))
		}
	}

	// Gate 4 & 5: Per-symbol cooldown (default 4h) & daily cap (default 12) from
	// notice-journal.jsonl (§3 B4). Both count accepted notices, not deliveries.
	if s.journal != nil {
		stats, err := s.journal.BudgetStats(ntc.Symbol, now, 24*time.Hour)
		if err != nil {
			return deliverOutcome{err: fmt.Errorf("notice budget stats: %w", err)}
		}
		cooldown := time.Duration(s.cfg.SymbolCooldownMinutes) * time.Minute
		if cooldown > 0 && !stats.LastAcceptedSymbolAt.IsZero() {
			elapsed := now.Sub(stats.LastAcceptedSymbolAt)
			if elapsed >= 0 && elapsed < cooldown {
				ntc.DeliveryStatus = types.NoticeStatusSuppressed
				ntc.SuppressReason = types.NoticeSuppressSymbolCooldown
				ntc.SuppressDetail = fmt.Sprintf("同币冷却未满 (%s < %s)", elapsed.Round(time.Minute), cooldown)
				return s.recordOutcome(ntc)
			}
		}
		if s.cfg.MaxNoticesPerDay > 0 && stats.AcceptedInDayWindow >= s.cfg.MaxNoticesPerDay {
			ntc.DeliveryStatus = types.NoticeStatusSuppressed
			ntc.SuppressReason = types.NoticeSuppressDailyCap
			ntc.SuppressDetail = fmt.Sprintf("24h 通知预算已满 (%d/%d)", stats.AcceptedInDayWindow, s.cfg.MaxNoticesPerDay)
			return s.recordOutcome(ntc)
		}
	}

	if s.sender == nil {
		ntc.DeliveryStatus = types.NoticeStatusDryRun
		return s.recordOutcome(ntc)
	}

	if err := s.sender.SendText(ctx, ntc.RenderedTelegram); err != nil {
		ntc.DeliveryStatus = types.NoticeStatusFailed
		ntc.DeliveryError = err.Error()
		out := s.recordOutcome(ntc)
		out.err = errors.Join(fmt.Errorf("send notice telegram: %w", err), out.err)
		return out
	}

	ntc.DeliveryStatus = types.NoticeStatusSent
	return s.recordOutcome(ntc)
}

// recordOutcome journals ntc and reports whether the write actually landed.
func (s *Service) recordOutcome(ntc types.Notice) deliverOutcome {
	if err := s.saveNotice(ntc); err != nil {
		return deliverOutcome{notice: ntc, attempted: true, journaled: false, err: err}
	}
	return deliverOutcome{notice: ntc, attempted: true, journaled: true}
}

// pendingNotice is one coalescing bucket: the most informative candidate so far
// plus the windows it displaced. Nothing is journaled until the bucket flushes.
type pendingNotice struct {
	best   types.Notice
	extras []types.Notice // displaced candidates not yet journaled
	timer  *time.Timer

	// repAttempted/repJournaled track the representative independently of the
	// displaced records: a retry may write the journal again, but must never
	// resend a notice that already reached the delivery channel.
	repAttempted bool
	repJournaled bool
	repRecord    *types.Notice
}

// coalesceCandidate buckets ntc by symbol+event type for the coalescing interval.
// A more informative window replaces the current representative; the replaced one
// is kept and journaled as suppressed when the bucket flushes.
func (s *Service) coalesceCandidate(ntc types.Notice) (*types.Notice, error) {
	key := ntc.Symbol + "|" + ntc.TriggerFacts.EventType
	g, ok := s.pending[key]
	if !ok {
		g = &pendingNotice{best: ntc}
		s.pending[key] = g
		// The timer uses its own bounded context: the caller's request context may
		// be cancelled long before the bucket flushes.
		g.timer = time.AfterFunc(s.coalesce, func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := s.flush(key, ctx); err != nil {
				s.log.Error("notice coalesce flush failed", "key", key, "error", err)
			}
		})
		ntc.DeliveryStatus = types.NoticeStatusCoalescing
		return &ntc, nil
	}
	// Freeze the representative once its delivery decision ran (even a failed
	// flush keeps the bucket for a journal retry). Replacing it then would leave
	// the attempted record and the new candidate in an inconsistent settlement.
	if g.repAttempted {
		loser := suppressedDuplicate(ntc, g.representative())
		g.extras = append(g.extras, ntc)
		return &loser, nil
	}
	if moreInformative(ntc.TriggerFacts, g.best.TriggerFacts) {
		// Keep the raw displaced candidate: its duplicate detail is rendered at
		// flush time against the FINAL representative, so a chain A -> B -> C never
		// names a window that is itself suppressed.
		g.extras = append(g.extras, g.best)
		g.best = ntc
		ntc.DeliveryStatus = types.NoticeStatusCoalescing
		return &ntc, nil
	}
	loser := suppressedDuplicate(ntc, g.best)
	g.extras = append(g.extras, ntc)
	return &loser, nil
}

// representative is the settled record when one exists, otherwise the pending best.
func (g *pendingNotice) representative() types.Notice {
	if g.repRecord != nil {
		return *g.repRecord
	}
	return g.best
}

// flush delivers the coalesced representative and journals the windows it displaced.
// Errors are returned (timer callbacks log them, Close aggregates them) so a failed
// journal write or delivery is never silent.
func (s *Service) flush(key string, ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.pending[key]
	if !ok {
		return nil
	}
	if g.timer != nil {
		g.timer.Stop()
	}

	var errs []error

	// 1. Retry the displaced records; keep the ones that still fail so the next
	// flush (or Close) can persist them instead of losing the trace.
	if len(g.extras) > 0 {
		remaining := make([]types.Notice, 0, len(g.extras))
		for _, loser := range g.extras {
			if err := s.saveNotice(suppressedDuplicate(loser, g.best)); err != nil {
				errs = append(errs, fmt.Errorf("journal displaced notice %s: %w", loser.NoticeID, err))
				remaining = append(remaining, loser)
			}
		}
		g.extras = remaining
	}

	// 2. Settle the representative exactly once. Gates are evaluated against the
	// representative window event time, as the immediate path does.
	switch {
	case !g.repAttempted:
		out := s.deliverLocked(ctx, g.best, g.best.CreatedAt)
		if out.attempted {
			g.repAttempted = true
			rec := out.notice
			g.repRecord = &rec
			g.repJournaled = out.journaled
		}
		if out.err != nil {
			errs = append(errs, fmt.Errorf("deliver notice %s: %w", g.best.NoticeID, out.err))
		}
	case !g.repJournaled && g.repRecord != nil:
		// Delivery already happened; only the journal write is outstanding.
		if err := s.saveNotice(*g.repRecord); err != nil {
			errs = append(errs, fmt.Errorf("journal notice %s: %w", g.best.NoticeID, err))
		} else {
			g.repJournaled = true
		}
	}

	if g.repJournaled && len(g.extras) == 0 {
		delete(s.pending, key)
	}
	return errors.Join(errs...)
}

// Close flushes every pending coalescing bucket so shutdown never drops a notice
// silently. Buckets flush oldest-event-first: Go map order would let a later
// bucket be accepted first and, because BudgetStats ignores records newer than the
// evaluated notice, push both past the daily cap.
func (s *Service) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	type bucket struct {
		key string
		at  time.Time
		id  string
	}
	buckets := make([]bucket, 0, len(s.pending))
	for k, g := range s.pending {
		buckets = append(buckets, bucket{key: k, at: g.best.CreatedAt, id: g.best.NoticeID})
	}
	s.mu.Unlock()

	if len(buckets) == 0 {
		return nil
	}
	sort.Slice(buckets, func(i, j int) bool {
		if !buckets[i].at.Equal(buckets[j].at) {
			return buckets[i].at.Before(buckets[j].at)
		}
		return buckets[i].id < buckets[j].id
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var errs []error
	for _, b := range buckets {
		if err := s.flush(b.key, ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
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
	if err := s.saveNotice(ntc); err != nil {
		return &ntc, err
	}
	return &ntc, nil
}

// saveNotice writes to the notice journal through the injectable seam.
func (s *Service) saveNotice(ntc types.Notice) error {
	if s.save == nil {
		return nil
	}
	return s.save(ntc)
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
