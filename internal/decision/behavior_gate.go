// Behavior Risk Gate — KAIROS_DOCTRINE §9b 七条行为硬规则的机械化。
//
// 依据 2026-08-09 交易画像(70+25 笔实测): 负 EV 来自行为(单币反复、逆势、
// 超短持仓、报复单),不是胜率。gate 在 ticket 资格判定处拦截,宁可误拦不可漏放;
// 被拦截的 ticket 保留 shadow 记录(counterfactual 数据),人工可覆盖但必须留痕。
package decision

import (
	"math"
	"strings"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// Gate reason codes (§9b rule N ↔ code + Phase 1 calibration codes).
const (
	GateCrossMargin      = "GATE_CROSS_MARGIN"      // 1. Isolated only
	GateScalpTooShort    = "GATE_SCALP_LT_5M"       // 2. unexcused <5m impulse exit
	GateCooldown         = "GATE_COOLDOWN"          // 3a. loss → 同币 cooldown
	GateRigid24hLock     = "GATE_COOLDOWN_24H_LOCK" // 3b. >=2 consecutive stops or <=-1.5R cumulative trial loss → rigid 24h lock (non-overridable)
	GateCounterCycle     = "GATE_COUNTER_CYCLE"     // 4. direction from CycleMap
	GateOutsideWindow    = "GATE_OUTSIDE_WINDOW"    // 5. evening live window
	GateRiskTooLarge     = "GATE_RISK_TOO_LARGE"    // 6. risk sizing
	GateNoStop           = "GATE_NO_STOP"           // 7. liquidation ≠ stop
	GateLeverageExceeded = "GATE_LEVERAGE_EXCEEDED" // 8. hard leverage cap (BTC/ETH <=10x, alts <=5x)
	GateNoValidTicket    = "GATE_NO_VALID_TICKET"   // 9. true churn: opening without a valid ticket
	GateChurnFlipLt5m    = "GATE_CHURN_FLIP_LT_5M"  // 10. true churn: >=2 long/short flips on same coin within <5m without structural reset
)

// Post-exit audit codes (Work Item 8: Duration × ExitReason × RealizedR matrix).
const (
	AuditCompliant            = "AUDIT_COMPLIANT"
	AuditProtectiveStopExempt = "AUDIT_PROTECTIVE_STOP_EXEMPT"
	NoteEarlyProfitLt5m       = "NOTE_EARLY_PROFIT_LT_5M"
)

// MarginModeIsolated is the only margin mode allowed for live tickets.
const MarginModeIsolated = "isolated"

// BehaviorGateConfig carries the numeric knobs (config-owned, §9b defaults).
type BehaviorGateConfig struct {
	Enabled         bool
	CooldownMinutes int     // 同币亏损后冷却(30–60,默认 45)
	WindowStartHour int     // live 窗口开始(本地时区小时,默认 18)
	WindowEndHour   int     // live 窗口结束(默认 1,跨午夜)
	MaxLossUSD      float64 // 单笔预设最大损失上限(默认 4)

	// Work Item 7: Leverage hard caps (default ON: BTC/ETH <=10x, alts <=5x).
	EnforceLeverageCap *bool
	MaxMajorLeverage   float64
	MaxAltLeverage     float64
	TrancheBaseUSD     float64
}

// LeverageCapEnabled reports whether mechanical leverage caps are active (default ON when nil).
func (cfg BehaviorGateConfig) LeverageCapEnabled() bool {
	if cfg.EnforceLeverageCap == nil {
		return true
	}
	return *cfg.EnforceLeverageCap
}

// DefaultBehaviorGateConfig returns §9b + Phase 1 freeze values.
func DefaultBehaviorGateConfig() BehaviorGateConfig {
	on := true
	return BehaviorGateConfig{
		Enabled:            true,
		CooldownMinutes:    45,
		WindowStartHour:    18,
		WindowEndHour:      1,
		MaxLossUSD:         4,
		EnforceLeverageCap: &on,
		MaxMajorLeverage:   DefaultMajorLeverageCap,
		MaxAltLeverage:     DefaultAltLeverageCap,
		TrancheBaseUSD:     DefaultTrancheBaseUSD,
	}
}

// BehaviorGateConfigFrom maps the config-file section onto gate knobs,
// falling back to §9b defaults for unset numeric fields.
func BehaviorGateConfigFrom(c types.RiskGateConfig) BehaviorGateConfig {
	out := DefaultBehaviorGateConfig()
	out.Enabled = c.Enabled
	if c.CooldownMinutes > 0 {
		out.CooldownMinutes = c.CooldownMinutes
	}
	if c.WindowStartHour > 0 || c.WindowEndHour > 0 {
		out.WindowStartHour = c.WindowStartHour
		out.WindowEndHour = c.WindowEndHour
	}
	if c.MaxLossUSD > 0 {
		out.MaxLossUSD = c.MaxLossUSD
	}
	if c.EnforceLeverageCap != nil {
		v := *c.EnforceLeverageCap
		out.EnforceLeverageCap = &v
	}
	if c.MaxMajorLeverage > 0 {
		out.MaxMajorLeverage = c.MaxMajorLeverage
	}
	if c.MaxAltLeverage > 0 {
		out.MaxAltLeverage = c.MaxAltLeverage
	}
	if c.TrancheBaseUSD > 0 {
		out.TrancheBaseUSD = c.TrancheBaseUSD
	}
	return out
}

// GateInput is everything the gate needs. 纯数据: journal 查询由调用方完成注入,
// gate 本身无 IO,可表驱动单测。
type GateInput struct {
	Now time.Time // 判定时刻(带时区;窗口按 Now.Hour() 判)

	Symbol    string
	Direction types.CycleDirection

	// MarginMode: "isolated" 之外(含空值/未知)一律 reject —— 宁可误拦。
	MarginMode string

	// StopPrice 必须在进入前设定;LiquidationPrice>0 且 stop 贴着强平价视为无止损。
	StopPrice        float64
	LiquidationPrice float64

	// MaxLossUSD 该笔预设最大损失(0/负 = 未定义 → reject)。
	MaxLossUSD float64

	// Work Item 7 & 10: Leverage & notional sizing inputs.
	Leverage       float64
	NotionalUSD    float64
	TrancheBaseUSD float64

	// Context: CycleMap 主方向与 rank 支持(规则 4: 禁止"涨多了感觉该空")。
	ContextDirection types.CycleDirection
	RankSupported    bool

	// LastLossSameSymbolAt: 最近一次同币亏损平仓时间(zero = 无记录)。
	LastLossSameSymbolAt time.Time
	// NewSetupID 非空 = 产生了新 Setup,豁免常规 cooldown(§9b 规则 3 但书)。
	NewSetupID string

	// Work Item 9: Dual-track same-coin cooldown inputs.
	// Track (a): New independent 15m/1h structural confirmation or SECOND_WAVE playbook switch
	// unlocks a 2nd ticket early after a small trial loss or completed wave.
	NewStructuralConfirm15m1h bool
	PlaybookSwitch            string // e.g. "SECOND_WAVE"
	CompletedPriorWave        bool

	// Track (b): Hard floor that cannot be overridden — 2 consecutive stops on the same coin
	// within 24h, or same-coin cumulative trial loss <= -1.5R → rigid 24h lock.
	ConsecutiveStops24h     int
	CumulativeTrialLoss24hR float64
}

// GateResult is the verdict. LiveEligible=false 只拦 live 资格,shadow 照常记录。
type GateResult struct {
	LiveEligible   bool
	NonOverridable bool     // true when rigid 24h lock is active (Work Item 9b)
	Rejections     []string // 违反的规则 code(空 = 全部通过)
	Warnings       []string
}

// EvaluateBehaviorGate applies §9b + Phase 1 rules at ticket time.
// Post-exit holding/exit audit is evaluated via AuditExit / AuditHoldingDuration.
func EvaluateBehaviorGate(cfg BehaviorGateConfig, in GateInput) GateResult {
	if !cfg.Enabled {
		return GateResult{LiveEligible: true, Warnings: []string{"behavior gate disabled"}}
	}
	var rej []string
	nonOverridable := false

	// 1. Isolated only — Cross 共享保证金 = 一笔爆全爆。
	if in.MarginMode != MarginModeIsolated {
		rej = append(rej, GateCrossMargin)
	}

	// 3. Same-coin cooldown → dual track (Work Item 9):
	// Track (b): Hard floor (non-overridable) — >=2 consecutive stops on same coin within 24h
	// OR same-coin cumulative trial loss <= -1.5R → rigid 24h lock.
	hardLockTriggered := in.ConsecutiveStops24h >= 2 || in.CumulativeTrialLoss24hR <= -1.5
	if hardLockTriggered {
		stillIn24h := in.LastLossSameSymbolAt.IsZero() || in.Now.Sub(in.LastLossSameSymbolAt) < 24*time.Hour
		if stillIn24h {
			rej = append(rej, GateRigid24hLock)
			nonOverridable = true
		}
	} else if !in.LastLossSameSymbolAt.IsZero() {
		// Track (a): Early unlock inside CooldownMinutes when a new independent 15m/1h
		// structural confirmation, NewSetupID, or SECOND_WAVE playbook switch is present.
		earlyUnlock := in.NewSetupID != "" ||
			in.NewStructuralConfirm15m1h ||
			isSecondWaveSwitch(in.PlaybookSwitch)
		if !earlyUnlock {
			cooldown := time.Duration(cfg.CooldownMinutes) * time.Minute
			if in.Now.Sub(in.LastLossSameSymbolAt) < cooldown {
				rej = append(rej, GateCooldown)
			}
		}
	}

	// 4. direction from CycleMap — 方向必须与 context 一致且有 rank 支持。
	counterCycle := (in.Direction == types.CycleDirectionUp && in.ContextDirection != types.CycleDirectionUp) ||
		(in.Direction == types.CycleDirectionDown && in.ContextDirection != types.CycleDirectionDown)
	if counterCycle || !in.RankSupported {
		rej = append(rej, GateCounterCycle)
	}

	// 5. evening live window(画像: 18:00–24:00 PF 0.96 vs 12:00–18:00 PF 0.21)。
	if !inLiveWindow(in.Now.Hour(), cfg.WindowStartHour, cfg.WindowEndHour) {
		rej = append(rej, GateOutsideWindow)
	}

	// 6. risk sizing — 未定义(≤0)或超预算都拒。
	if in.MaxLossUSD <= 0 || in.MaxLossUSD > cfg.MaxLossUSD {
		rej = append(rej, GateRiskTooLarge)
	}

	// 7. liquidation ≠ stop — 无止损,或止损贴着强平价(±0.5%)。
	if in.StopPrice <= 0 {
		rej = append(rej, GateNoStop)
	} else if in.LiquidationPrice > 0 {
		dist := in.StopPrice - in.LiquidationPrice
		if dist < 0 {
			dist = -dist
		}
		if dist/in.LiquidationPrice < 0.005 {
			rej = append(rej, GateNoStop)
		}
	}

	// 8. Work Item 7: Leverage hard caps (default ON: BTC/ETH <=10x, alts <=5x).
	if cfg.LeverageCapEnabled() {
		capLev := MaxAssetLeverage(in.Symbol, cfg.MaxMajorLeverage, cfg.MaxAltLeverage)
		tranche := in.TrancheBaseUSD
		if tranche <= 0 {
			tranche = cfg.TrancheBaseUSD
		}
		effLev := in.Leverage
		if effLev <= 0 && in.NotionalUSD > 0 && tranche > 0 {
			effLev = in.NotionalUSD / tranche
		}
		notionalExceeded := in.NotionalUSD > 0 && tranche > 0 && in.NotionalUSD > tranche*capLev+1e-6
		if effLev > capLev+1e-9 || notionalExceeded {
			rej = append(rej, GateLeverageExceeded)
		}
	}

	return GateResult{
		LiveEligible:   len(rej) == 0,
		NonOverridable: nonOverridable,
		Rejections:     rej,
	}
}

func isSecondWaveSwitch(s string) bool {
	u := strings.ToUpper(strings.TrimSpace(s))
	return u == "SECOND_WAVE" || strings.Contains(u, "SECOND_WAVE")
}

// inLiveWindow reports whether hour is inside [start, end) with midnight wrap
// (start=18 end=1 → 18..23 与 0 点允许)。start==end 视为全天开。
func inLiveWindow(hour, start, end int) bool {
	if start == end {
		return true
	}
	if start < end {
		return hour >= start && hour < end
	}
	return hour >= start || hour < end
}

// ExitAuditVerdict classifies a closed trade under the (Duration × ExitReason × RealizedR) matrix (Work Item 8).
type ExitAuditVerdict string

const (
	VerdictCompliant            ExitAuditVerdict = "COMPLIANT"
	VerdictProtectiveStopExempt ExitAuditVerdict = "PROTECTIVE_STOP_EXEMPT"
	VerdictEarlyProfitNote      ExitAuditVerdict = "EARLY_PROFIT_NOTE"
	VerdictTrueChurn            ExitAuditVerdict = "TRUE_CHURN"
)

// ExitAuditInput provides the inputs for the (Duration × ExitReason × RealizedR) audit matrix (Work Item 8).
type ExitAuditInput struct {
	TicketID           string
	HasValidTicket     bool
	Symbol             string
	Direction          types.CycleDirection
	OpenedAt           time.Time
	ClosedAt           time.Time
	EntryPrice         float64
	ExitPrice          float64
	PresetStopPrice    float64
	RealizedR          float64
	ExitReason         types.ExitReason
	TouchedPresetStop  bool
	FakeoutConfirmed   bool // 1m/5m reverse volume pin-bar/engulfing fakeout confirmation
	MarketStressActive bool // market_stress circuit breaker active
	StructureIntact    bool // true when 15m/1h structure remained intact at exit
	SameCoinFlipsLt5m  int  // count of long/short flips on same coin within <5m
	StructuralReset    bool // true when a structural reset occurred between flips
}

// ExitAuditResult is the structured verdict from AuditExit (Work Item 8).
type ExitAuditResult struct {
	Verdict     ExitAuditVerdict `json:"verdict"`
	Code        string           `json:"code,omitempty"`
	IsViolation bool             `json:"is_violation"`
	ScoreDelta  float64          `json:"score_delta"`
	Suggestion  string           `json:"suggestion,omitempty"`
}

// AuditExit replaces the blanket "no <5m" rule with the (Duration × ExitReason × RealizedR) matrix (Work Item 8):
//   - Protective-stop exemption (compliant, scores positive +1.0): exit <5m AND any of —
//     touched ticket's preset stop_price; 1m/5m reverse volume pin-bar/engulfing fakeout confirmation;
//     market_stress circuit breaker active.
//   - Early-profit note (review suggestion only, NOT a violation): exit <5m with 0 ~ +0.5R profit
//     while structure intact.
//   - True churn (red line violation, scores -1.0): opening without a valid ticket; or >=2 long/short
//     flips on the same coin within <5m without a structural reset; or unexcused <5m impulse exit.
func AuditExit(in ExitAuditInput) ExitAuditResult {
	hasTicket := in.HasValidTicket || strings.TrimSpace(in.TicketID) != ""
	if !hasTicket {
		return ExitAuditResult{
			Verdict:     VerdictTrueChurn,
			Code:        GateNoValidTicket,
			IsViolation: true,
			ScoreDelta:  -1.0,
			Suggestion:  "position opened without a valid decision ticket",
		}
	}

	if in.SameCoinFlipsLt5m >= 2 && !in.StructuralReset {
		return ExitAuditResult{
			Verdict:     VerdictTrueChurn,
			Code:        GateChurnFlipLt5m,
			IsViolation: true,
			ScoreDelta:  -1.0,
			Suggestion:  ">=2 long/short flips on same coin within <5m without structural reset",
		}
	}

	if in.OpenedAt.IsZero() || in.ClosedAt.IsZero() || !in.ClosedAt.After(in.OpenedAt) {
		return ExitAuditResult{
			Verdict:     VerdictCompliant,
			Code:        AuditCompliant,
			IsViolation: false,
			ScoreDelta:  0,
		}
	}

	hold := in.ClosedAt.Sub(in.OpenedAt)
	if hold >= 5*time.Minute {
		return ExitAuditResult{
			Verdict:     VerdictCompliant,
			Code:        AuditCompliant,
			IsViolation: false,
			ScoreDelta:  0,
		}
	}

	// Duration is <5m: check protective-stop / fakeout / market_stress exemption first.
	if isProtectiveStopExempt(in) {
		return ExitAuditResult{
			Verdict:     VerdictProtectiveStopExempt,
			Code:        AuditProtectiveStopExempt,
			IsViolation: false,
			ScoreDelta:  1.0,
			Suggestion:  "protective stop / fakeout abort / market_stress exemption (<5m discipline)",
		}
	}

	// Early-profit note: exit <5m with 0 ~ +0.5R profit while structure intact (review suggestion only, NOT a violation).
	if in.RealizedR >= 0 && in.RealizedR <= 0.5 && in.StructureIntact {
		return ExitAuditResult{
			Verdict:     VerdictEarlyProfitNote,
			Code:        NoteEarlyProfitLt5m,
			IsViolation: false,
			ScoreDelta:  0,
			Suggestion:  "early exit (<5m) with 0~+0.5R profit while structure intact; consider holding for wave structure",
		}
	}

	// Profitable > +0.5R structural/target exit <5m (when not an impulse early exit) is compliant.
	if in.RealizedR > 0.5 && in.ExitReason != types.ExitReasonImpulseEarly {
		return ExitAuditResult{
			Verdict:     VerdictCompliant,
			Code:        AuditCompliant,
			IsViolation: false,
			ScoreDelta:  0,
		}
	}

	return ExitAuditResult{
		Verdict:     VerdictTrueChurn,
		Code:        GateScalpTooShort,
		IsViolation: true,
		ScoreDelta:  -1.0,
		Suggestion:  "unexcused <5m impulse exit without preset-stop touch or fakeout confirmation",
	}
}

func isProtectiveStopExempt(in ExitAuditInput) bool {
	if in.TouchedPresetStop || in.FakeoutConfirmed || in.MarketStressActive {
		return true
	}
	if in.ExitReason == types.ExitReasonProtectiveStop || in.ExitReason == types.ExitReasonFakeoutAbort {
		return true
	}
	if in.PresetStopPrice > 0 && in.ExitPrice > 0 {
		switch in.Direction {
		case types.CycleDirectionUp:
			if in.ExitPrice <= in.PresetStopPrice*1.0005 {
				return true
			}
		case types.CycleDirectionDown:
			if in.ExitPrice >= in.PresetStopPrice*0.9995 {
				return true
			}
		default:
			if math.Abs(in.ExitPrice-in.PresetStopPrice)/in.PresetStopPrice <= 0.001 {
				return true
			}
		}
	}
	return false
}

// AuditExitSequence evaluates a chronological sequence of trades, automatically tracking
// <5m same-coin long/short direction flips without a structural reset (Work Item 8).
func AuditExitSequence(trades []ExitAuditInput) []ExitAuditResult {
	results := make([]ExitAuditResult, len(trades))
	for i := range trades {
		cur := trades[i]
		if cur.SameCoinFlipsLt5m == 0 && cur.Symbol != "" && !cur.OpenedAt.IsZero() && !cur.StructuralReset {
			flips := 0
			lastDir := cur.Direction
			for j := i - 1; j >= 0; j-- {
				prev := trades[j]
				if prev.Symbol != cur.Symbol || prev.OpenedAt.IsZero() {
					continue
				}
				refTime := prev.ClosedAt
				if refTime.IsZero() {
					refTime = prev.OpenedAt
				}
				if cur.OpenedAt.Sub(refTime) >= 5*time.Minute {
					break
				}
				if prev.Direction != "" && lastDir != "" && prev.Direction != lastDir {
					flips++
					lastDir = prev.Direction
				}
				if prev.StructuralReset {
					break
				}
			}
			cur.SameCoinFlipsLt5m = flips
		}
		results[i] = AuditExit(cur)
	}
	return results
}

// AuditHoldingDuration is the legacy §9b rule 2 wrapper around AuditExit.
// Returns the violation code if IsViolation is true, or "" when compliant/note.
func AuditHoldingDuration(openedAt, closedAt time.Time) string {
	res := AuditExit(ExitAuditInput{
		TicketID:       "legacy",
		HasValidTicket: true,
		OpenedAt:       openedAt,
		ClosedAt:       closedAt,
		RealizedR:      -0.2,
	})
	if res.IsViolation {
		return res.Code
	}
	return ""
}
