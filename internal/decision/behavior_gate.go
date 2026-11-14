// Behavior Risk Gate — KAIROS_DOCTRINE §9b 七条行为硬规则的机械化。
//
// 依据 2026-08-09 交易画像(70+25 笔实测): 负 EV 来自行为(单币反复、逆势、
// 超短持仓、报复单),不是胜率。gate 在 ticket 资格判定处拦截,宁可误拦不可漏放;
// 被拦截的 ticket 保留 shadow 记录(counterfactual 数据),人工可覆盖但必须留痕。
package decision

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// Gate reason codes (§9b rule N ↔ code + Phase 1 & Phase 3b codes).
const (
	GateCrossMargin      = "GATE_CROSS_MARGIN"       // 1. Isolated only
	GateScalpTooShort    = "GATE_SCALP_LT_5M"        // 2. unexcused <5m impulse exit
	GateCooldown         = "GATE_COOLDOWN"           // 3a. loss → 同币 cooldown
	GateRigid24hLock     = "GATE_COOLDOWN_24H_LOCK"  // 3b. >=2 consecutive stops or <=-1.5R cumulative trial loss → rigid 24h lock (non-overridable)
	GateCounterCycle     = "GATE_COUNTER_CYCLE"      // 4. direction from CycleMap
	GateOutsideWindow    = "GATE_OUTSIDE_WINDOW"     // 5. evening live window
	GateRiskTooLarge     = "GATE_RISK_TOO_LARGE"     // 6. risk sizing
	GateNoStop           = "GATE_NO_STOP"            // 7a. liquidation ≠ stop
	GateNoStopBasis      = "GATE_NO_STOP_BASIS"      // 7b. Phase 3b A2: missing structural invalidation basis text
	GateLeverageExceeded = "GATE_LEVERAGE_EXCEEDED"  // 8. hard leverage cap (BTC/ETH <=10x, alts <=5x)
	GateNoValidTicket    = "GATE_NO_VALID_TICKET"    // 9. true churn: opening without a valid ticket
	GateChurnFlipLt5m    = "GATE_CHURN_FLIP_LT_5M"   // 10. true churn: >=2 long/short flips on same coin within <5m without structural reset
	GateSetupTrialBudget = "GATE_SETUP_TRIAL_BUDGET" // 11. Phase 3b A1: >=4th ticket on same (symbol, setup_point) within rolling window
)

// Warning codes (non-blocking: A1 unlabeled fallback + A4 STRATEGY §12 relapse patterns).
const (
	WarnUnlabeledTrialBudget = "WARN_UNLABELED_TRIAL_BUDGET"
	WarnTiltReverse          = "WARN_TILT_REVERSE"         // A4(i): 止损后短时同币反手 → 「上头反手」
	WarnLossStreakChurn      = "WARN_LOSS_STREAK_CHURN"    // A4(ii): 连损后短时高频开票 → 「连错刷单」
	WarnLossSizeEscalation   = "WARN_LOSS_SIZE_ESCALATION" // A4(iii): 亏后单票风险额显著高于个人近期中位数 → 「亏后加码」
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

	// Phase 3b A1: Same (symbol, setup_point) rolling trial budget (default max=3, window=24h).
	TrialBudgetMax         int
	TrialBudgetWindowHours float64

	// Phase 3b A4: STRATEGY §12 relapse warning thresholds (warning-only, never blocks).
	TiltReverseMinutes    int     // default 30
	ChurnConsecutiveStops int     // default 2
	ChurnWindowMinutes    int     // default 60
	ChurnTicketCount      int     // default 3 (including current ticket)
	SizeEscalationMult    float64 // default 1.5
	RecentRiskSampleSize  int     // default 10 (min 3 observations required)
}

// LeverageCapEnabled reports whether mechanical leverage caps are active (default ON when nil).
func (cfg BehaviorGateConfig) LeverageCapEnabled() bool {
	if cfg.EnforceLeverageCap == nil {
		return true
	}
	return *cfg.EnforceLeverageCap
}

// DefaultBehaviorGateConfig returns §9b + Phase 1 + Phase 3b freeze values.
func DefaultBehaviorGateConfig() BehaviorGateConfig {
	on := true
	return BehaviorGateConfig{
		Enabled:                true,
		CooldownMinutes:        45,
		WindowStartHour:        18,
		WindowEndHour:          1,
		MaxLossUSD:             4,
		EnforceLeverageCap:     &on,
		MaxMajorLeverage:       DefaultMajorLeverageCap,
		MaxAltLeverage:         DefaultAltLeverageCap,
		TrancheBaseUSD:         DefaultTrancheBaseUSD,
		TrialBudgetMax:         3,
		TrialBudgetWindowHours: 24.0,
		TiltReverseMinutes:     30,
		ChurnConsecutiveStops:  2,
		ChurnWindowMinutes:     60,
		ChurnTicketCount:       3,
		SizeEscalationMult:     1.5,
		RecentRiskSampleSize:   10,
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
	if c.TrialBudgetMax > 0 {
		out.TrialBudgetMax = c.TrialBudgetMax
	}
	if c.TrialBudgetWindowHours > 0 {
		out.TrialBudgetWindowHours = c.TrialBudgetWindowHours
	}
	if c.TiltReverseMinutes > 0 {
		out.TiltReverseMinutes = c.TiltReverseMinutes
	}
	if c.ChurnConsecutiveStops > 0 {
		out.ChurnConsecutiveStops = c.ChurnConsecutiveStops
	}
	if c.ChurnWindowMinutes > 0 {
		out.ChurnWindowMinutes = c.ChurnWindowMinutes
	}
	if c.ChurnTicketCount > 0 {
		out.ChurnTicketCount = c.ChurnTicketCount
	}
	if c.SizeEscalationMult > 0 {
		out.SizeEscalationMult = c.SizeEscalationMult
	}
	if c.RecentRiskSampleSize > 0 {
		out.RecentRiskSampleSize = c.RecentRiskSampleSize
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
	// StopBasis 为结构失效依据注释(Phase 3b A2: 空值/纯空白一律 reject)。
	StopPrice        float64
	LiquidationPrice float64
	StopBasis        string

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

	// Track (b): Hard floor that cannot be overridden — 2 consecutive stops on the same coin
	// within 24h, or same-coin cumulative trial loss <= -1.5R → rigid 24h lock.
	ConsecutiveStops24h     int
	CumulativeTrialLoss24hR float64

	// Phase 3b A1: Same-point trial budget inputs (prior tickets in rolling window, excluding current).
	// SetupPoint uses the ticket's fixed §3 enum; empty means unlabeled (counts by symbol & downgrades to warning).
	SetupPoint               types.SetupPoint
	SameSetupTrialsInWindow  int
	SameSymbolTrialsInWindow int

	// Phase 3b A4: STRATEGY §12 relapse pattern warning inputs (pure journal facts; warning-only, never blocks).
	LastStopSameSymbolAt        time.Time
	LastStopSameSymbolDirection types.CycleDirection
	GlobalConsecutiveStops      int
	RecentTicketsInChurnWindow  int
	LastOutcomeWasLoss          bool
	RecentPersonalRiskUSD       []float64
}

// GateResult is the verdict. LiveEligible=false 只拦 live 资格,shadow 照常记录。
type GateResult struct {
	LiveEligible   bool
	NonOverridable bool     // true when rigid 24h lock is active (Work Item 9b)
	Rejections     []string // 违反的规则 code(空 = 全部通过)
	Warnings       []string
}

// EvaluateBehaviorGate applies §9b + Phase 1 + Phase 3b rules at ticket time.
// Post-exit holding/exit audit is evaluated via AuditExit / AuditHoldingDuration.
func EvaluateBehaviorGate(cfg BehaviorGateConfig, in GateInput) GateResult {
	relapseWarnings := EvaluateRelapseWarnings(cfg, in)
	if !cfg.Enabled {
		warnings := append([]string{"behavior gate disabled"}, relapseWarnings...)
		return GateResult{LiveEligible: true, Warnings: warnings}
	}
	var rej []string
	var warnings []string
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

	// 7a. liquidation ≠ stop — 无止损,或止损贴着强平价(±0.5%)。
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

	// 7b. Phase 3b A2: 结构失效止损必须附带结构依据注释,空注释一律拒。
	if strings.TrimSpace(in.StopBasis) == "" {
		rej = append(rej, GateNoStopBasis)
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

	// 11. Phase 3b A1: 同点位试错预算 <=3 (第 4 张票硬拒;无点位标签票按同币计数降级为警告,绝不编造点位)。
	budgetMax := cfg.TrialBudgetMax
	if budgetMax <= 0 {
		budgetMax = 3
	}
	winHours := cfg.TrialBudgetWindowHours
	if winHours <= 0 {
		winHours = 24.0
	}
	normSetup := types.NormalizeSetupPoint(in.SetupPoint)
	if normSetup != "" {
		if in.SameSetupTrialsInWindow >= budgetMax {
			rej = append(rej, GateSetupTrialBudget)
		}
	} else if in.SameSymbolTrialsInWindow >= budgetMax {
		warnings = append(warnings, fmt.Sprintf(
			"%s: 同币 %s 无点位标签滚动 %.0fh 内第 %d 张票（同点位预算上限 %d 张，无标签降为警告）",
			WarnUnlabeledTrialBudget, in.Symbol, winHours, in.SameSymbolTrialsInWindow+1, budgetMax,
		))
	}

	// A4: STRATEGY §12 犯病图谱事实警告(只警告、永不拦截)。
	warnings = append(warnings, relapseWarnings...)

	return GateResult{
		LiveEligible:   len(rej) == 0,
		NonOverridable: nonOverridable,
		Rejections:     rej,
		Warnings:       warnings,
	}
}

// EvaluateRelapseWarnings computes STRATEGY §12 relapse warnings from journal-derived facts.
// Warning-only by contract: callers MUST NOT convert these warnings into rejections.
func EvaluateRelapseWarnings(cfg BehaviorGateConfig, in GateInput) []string {
	var warnings []string

	// (i) 止损后短时(默认 30m)同币反手开票 → 「上头反手」警告
	tiltMin := cfg.TiltReverseMinutes
	if tiltMin <= 0 {
		tiltMin = 30
	}
	if !in.LastStopSameSymbolAt.IsZero() && !in.Now.IsZero() &&
		isOppositeDirection(in.Direction, in.LastStopSameSymbolDirection) {
		elapsed := in.Now.Sub(in.LastStopSameSymbolAt)
		if elapsed >= 0 && elapsed <= time.Duration(tiltMin)*time.Minute {
			warnings = append(warnings, fmt.Sprintf(
				"%s [上头反手]: 同币 %s 止损后 %.1f 分钟内反手开票（前序 %s → 当前 %s，阈值 <=%d 分钟）",
				WarnTiltReverse, in.Symbol, elapsed.Minutes(),
				in.LastStopSameSymbolDirection, in.Direction, tiltMin,
			))
		}
	}

	// (ii) 连续止损后短时高频开票 → 「连错刷单」警告
	minConsec := cfg.ChurnConsecutiveStops
	if minConsec <= 0 {
		minConsec = 2
	}
	churnWin := cfg.ChurnWindowMinutes
	if churnWin <= 0 {
		churnWin = 60
	}
	churnCnt := cfg.ChurnTicketCount
	if churnCnt <= 0 {
		churnCnt = 3
	}
	totalInWin := in.RecentTicketsInChurnWindow + 1
	if in.GlobalConsecutiveStops >= minConsec && totalInWin >= churnCnt {
		warnings = append(warnings, fmt.Sprintf(
			"%s [连错刷单]: 连续止损 %d 笔后 %d 分钟内高频开出第 %d 张票（阈值：连损 >=%d 笔且 %d 分钟内 >=%d 张）",
			WarnLossStreakChurn, in.GlobalConsecutiveStops, churnWin, totalInWin,
			minConsec, churnWin, churnCnt,
		))
	}

	// (iii) 亏损后单票风险额较个人近期中位数显著放大 → 「亏后加码」警告
	mult := cfg.SizeEscalationMult
	if mult <= 0 {
		mult = 1.5
	}
	maxSample := cfg.RecentRiskSampleSize
	if maxSample <= 0 {
		maxSample = 10
	}
	var samples []float64
	for _, v := range in.RecentPersonalRiskUSD {
		if v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) {
			samples = append(samples, v)
		}
	}
	if len(samples) > maxSample {
		samples = samples[len(samples)-maxSample:]
	}
	// Require at least 3 genuine historical observations — never fabricate a median.
	if in.LastOutcomeWasLoss && in.MaxLossUSD > 0 && len(samples) >= 3 {
		med := medianFloat64(samples)
		if med > 0 && in.MaxLossUSD >= med*mult-1e-9 {
			ratio := in.MaxLossUSD / med
			warnings = append(warnings, fmt.Sprintf(
				"%s [亏后加码]: 前序亏损后单票风险额 $%.2f = 个人近期中位数 $%.2f 的 %.2f×（样本 n=%d，阈值 >=%.2f×）",
				WarnLossSizeEscalation, in.MaxLossUSD, med, ratio, len(samples), mult,
			))
		}
	}

	return warnings
}

func isOppositeDirection(a, b types.CycleDirection) bool {
	return (a == types.CycleDirectionUp && b == types.CycleDirectionDown) ||
		(a == types.CycleDirectionDown && b == types.CycleDirectionUp)
}

func medianFloat64(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	cp := append([]float64(nil), vals...)
	sort.Float64s(cp)
	mid := len(cp) / 2
	if len(cp)%2 == 1 {
		return cp[mid]
	}
	return (cp[mid-1] + cp[mid]) / 2
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
