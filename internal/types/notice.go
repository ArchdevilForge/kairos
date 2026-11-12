package types

import (
	"strings"
	"time"
)

// Schema versions for independent Notice persistence (Phase 3c).
const (
	NoticeSchemaVersion         = "notice.v1"
	NoticeFeedbackSchemaVersion = "notice_feedback.v1"
)

// NoticeDisclaimerLine is the mandatory fixed reminder attached to every Notice (§3 B1).
const NoticeDisclaimerLine = "场景提示，未验证信号，判断在你"

// NoticeSceneFlag enumerates the Phase 3a scene flags reused strictly as
// human-attention scene pointers (§3 B1), never as entry signals or scores.
type NoticeSceneFlag string

const (
	ScenePoint1Breakout NoticeSceneFlag = "SCENE_POINT_1_BREAKOUT"
	ScenePoint2MinorDiv NoticeSceneFlag = "SCENE_POINT_2_MINOR_DIV"
	ScenePoint4Wave2    NoticeSceneFlag = "SCENE_POINT_4_SECOND_WAVE"
	SceneResonanceLead  NoticeSceneFlag = "SCENE_RESONANCE_LEADER"
)

// NoticeSceneDetail pairs a scene flag enum with factual scene language.
type NoticeSceneDetail struct {
	Flag    NoticeSceneFlag `json:"flag"`
	Summary string          `json:"summary"`
}

// NoticeBoxState classifies where price sits relative to recent 4h consolidation box (§3 B3).
type NoticeBoxState string

const (
	NoticeBoxUnknown  NoticeBoxState = "unknown"
	NoticeBoxInside   NoticeBoxState = "inside_box"
	NoticeBoxBoundary NoticeBoxState = "boundary"
	NoticeBoxOutside  NoticeBoxState = "outside_box"
)

// NoticeBoxStateLabelZH returns a factual Chinese label for NoticeBoxState.
func NoticeBoxStateLabelZH(st NoticeBoxState) string {
	switch st {
	case NoticeBoxInside:
		return "箱体内部"
	case NoticeBoxBoundary:
		return "箱体沿口"
	case NoticeBoxOutside:
		return "箱体外部/突破"
	default:
		return "未判定"
	}
}

// NoticeDeliveryStatus tracks routing fate for each Notice (§3 B2/B4).
type NoticeDeliveryStatus string

const (
	NoticeStatusSent       NoticeDeliveryStatus = "sent"
	NoticeStatusSuppressed NoticeDeliveryStatus = "suppressed"
	NoticeStatusDryRun     NoticeDeliveryStatus = "dry_run"
	NoticeStatusFailed     NoticeDeliveryStatus = "failed"
)

// Accepted reports whether a Notice consumed the symbol cooldown and the daily
// budget. A Notice is accepted once it is journaled and marked for delivery, so
// a missing delivery channel (dry_run) or a failed send still counts: the owner
// spends the same attention either way, and a silent cooldown is how one symbol
// produced three notices in 3.5 hours in production.
func (s NoticeDeliveryStatus) Accepted() bool {
	switch s {
	case NoticeStatusSent, NoticeStatusDryRun, NoticeStatusFailed:
		return true
	default:
		return false
	}
}

// Suppression reason constants for Notice budget & box-interior gating (§3 B3/B4).
const (
	NoticeSuppressInsideBox      = "inside_box"
	NoticeSuppressSymbolCooldown = "symbol_cooldown"
	NoticeSuppressDailyCap       = "daily_cap"
	// NoticeSuppressDuplicateEvent marks a second velocity window (60s/120s)
	// firing on the same underlying move; the first accepted window represents it.
	NoticeSuppressDuplicateEvent = "duplicate_event"
)

// NoticeTriggerFacts records objective hard-data anomaly facts that triggered the Notice (§3 B2/B3).
type NoticeTriggerFacts struct {
	EventType         string   `json:"event_type"`
	EventID           string   `json:"event_id,omitempty"`
	Exchange          string   `json:"exchange,omitempty"`
	Price             float64  `json:"price,omitempty"`
	VolumeRatio       float64  `json:"volume_ratio,omitempty"`
	VolumeWindowMin   int      `json:"volume_window_min,omitempty"`
	PriceChangePct    float64  `json:"price_change_pct,omitempty"`
	VelocityWindowSec int      `json:"velocity_window_sec,omitempty"`
	OIChangePct       float64  `json:"oi_change_pct,omitempty"`
	OpenInterest      float64  `json:"open_interest,omitempty"`
	Dimensions        []string `json:"dimensions,omitempty"`
	DimensionCount    int      `json:"dimension_count,omitempty"`
	FactSummary       string   `json:"fact_summary"`
}

// NoticeEnvTags carries environment tags attached to a Notice (§3 B3).
// SeasonRegime is populated ONLY when genuinely computed by the Phase 2c causal classifier;
// when uncomputable, it remains empty and is omitted from output.
type NoticeEnvTags struct {
	SeasonRegime      SeasonRegime   `json:"season_regime,omitempty"`
	WinterDefenseHint string         `json:"winter_defense_hint,omitempty"`
	BoxState          NoticeBoxState `json:"box_state"`
	BoxWidthPct       float64        `json:"box_width_pct,omitempty"`
	BoxPos            float64        `json:"box_pos,omitempty"`
	BTCState          string         `json:"btc_state,omitempty"`
}

// Notice is an independent opportunity notice (distinct from DecisionTicket, §3 B2).
// It discovers scenes/anomalies and attaches environment tags without ever issuing trade advice.
type Notice struct {
	SchemaVersion    string               `json:"schema_version"`
	NoticeID         string               `json:"notice_id"`
	CreatedAt        time.Time            `json:"ts"`
	Symbol           string               `json:"symbol"`
	Exchange         string               `json:"exchange,omitempty"`
	TriggerFacts     NoticeTriggerFacts   `json:"trigger_facts"`
	Scenes           []NoticeSceneDetail  `json:"scenes,omitempty"`
	Env              NoticeEnvTags        `json:"env"`
	DeliveryStatus   NoticeDeliveryStatus `json:"delivery_status"`
	SuppressReason   string               `json:"suppress_reason,omitempty"`
	SuppressDetail   string               `json:"suppress_detail,omitempty"`
	DeliveryError    string               `json:"delivery_error,omitempty"`
	Disclaimer       string               `json:"disclaimer"`
	RenderedTelegram string               `json:"rendered_telegram,omitempty"`
}

// NoticeFeedbackAction enumerates the three owner feedback actions in kairos-desk (§3 B5).
type NoticeFeedbackAction string

const (
	NoticeFeedbackSeen    NoticeFeedbackAction = "seen"    // 看了
	NoticeFeedbackActed   NoticeFeedbackAction = "acted"   // 做了
	NoticeFeedbackIgnored NoticeFeedbackAction = "ignored" // 忽略
)

// ParseNoticeFeedbackAction parses Chinese or English CLI tokens into NoticeFeedbackAction.
func ParseNoticeFeedbackAction(raw string) (NoticeFeedbackAction, bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch s {
	case "seen", "看", "看了", "view", "viewed":
		return NoticeFeedbackSeen, true
	case "acted", "act", "做", "做了", "traded", "trade":
		return NoticeFeedbackActed, true
	case "ignored", "ignore", "忽略", "skip", "skipped":
		return NoticeFeedbackIgnored, true
	default:
		return "", false
	}
}

// NoticeFeedbackLabelZH returns the Chinese display label for NoticeFeedbackAction.
func NoticeFeedbackLabelZH(act NoticeFeedbackAction) string {
	switch act {
	case NoticeFeedbackSeen:
		return "看了"
	case NoticeFeedbackActed:
		return "做了"
	case NoticeFeedbackIgnored:
		return "忽略"
	default:
		return "待反馈"
	}
}

// NoticeFeedback records owner feedback on a specific Notice in notice-journal.jsonl (§3 B5).
type NoticeFeedback struct {
	SchemaVersion string               `json:"schema_version"`
	NoticeID      string               `json:"notice_id"`
	Action        NoticeFeedbackAction `json:"action"`
	TicketID      string               `json:"ticket_id,omitempty"`
	Note          string               `json:"note,omitempty"`
	RecordedAt    time.Time            `json:"recorded_at"`
}

// NoticeConfig configures the Phase 3c opportunity notice layer (§3 B3/B4).
type NoticeConfig struct {
	Enabled               bool `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	SymbolCooldownMinutes int  `mapstructure:"symbolCooldownMinutes" json:"symbolCooldownMinutes" yaml:"symbolCooldownMinutes"`
	MaxNoticesPerDay      int  `mapstructure:"maxNoticesPerDay" json:"maxNoticesPerDay" yaml:"maxNoticesPerDay"`
	// EventDedupSeconds collapses the same underlying event reported by several
	// velocity windows into one Notice (0 disables event-level dedup).
	EventDedupSeconds int   `mapstructure:"eventDedupSeconds" json:"eventDedupSeconds" yaml:"eventDedupSeconds"`
	SuppressInsideBox *bool `mapstructure:"suppressInsideBox" json:"suppressInsideBox,omitempty" yaml:"suppressInsideBox,omitempty"`
}

// ShouldSuppressInsideBox returns true unless explicitly set to false in config (§3 B3).
func (c NoticeConfig) ShouldSuppressInsideBox() bool {
	if c.SuppressInsideBox == nil {
		return true
	}
	return *c.SuppressInsideBox
}
