// Behavior Risk Gate — KAIROS_DOCTRINE §9b 七条行为硬规则的机械化。
//
// 依据 2026-08-09 交易画像(70+25 笔实测): 负 EV 来自行为(单币反复、逆势、
// 超短持仓、报复单),不是胜率。gate 在 ticket 资格判定处拦截,宁可误拦不可漏放;
// 被拦截的 ticket 保留 shadow 记录(counterfactual 数据),人工可覆盖但必须留痕。
package decision

import (
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// Gate reason codes (§9b rule N ↔ code).
const (
	GateCrossMargin   = "GATE_CROSS_MARGIN"   // 1. Isolated only
	GateScalpTooShort = "GATE_SCALP_LT_5M"    // 2. no <5m discretionary(事后审计)
	GateCooldown      = "GATE_COOLDOWN"       // 3. loss → 同币 cooldown
	GateCounterCycle  = "GATE_COUNTER_CYCLE"  // 4. direction from CycleMap
	GateOutsideWindow = "GATE_OUTSIDE_WINDOW" // 5. evening live window
	GateRiskTooLarge  = "GATE_RISK_TOO_LARGE" // 6. risk sizing
	GateNoStop        = "GATE_NO_STOP"        // 7. liquidation ≠ stop
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
}

// DefaultBehaviorGateConfig returns §9b freeze values.
func DefaultBehaviorGateConfig() BehaviorGateConfig {
	return BehaviorGateConfig{
		Enabled:         true,
		CooldownMinutes: 45,
		WindowStartHour: 18,
		WindowEndHour:   1,
		MaxLossUSD:      4,
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

	// Context: CycleMap 主方向与 rank 支持(规则 4: 禁止"涨多了感觉该空")。
	ContextDirection types.CycleDirection
	RankSupported    bool

	// LastLossSameSymbolAt: 最近一次同币亏损平仓时间(zero = 无记录)。
	LastLossSameSymbolAt time.Time
	// NewSetupID 非空 = 产生了新 Setup,豁免 cooldown(§9b 规则 3 但书)。
	NewSetupID string
}

// GateResult is the verdict. LiveEligible=false 只拦 live 资格,shadow 照常记录。
type GateResult struct {
	LiveEligible bool
	Rejections   []string // 违反的规则 code(空 = 全部通过)
	Warnings     []string
}

// EvaluateBehaviorGate applies §9b rules 1,3,4,5,6,7 at ticket time.
// 规则 2(<5m 持仓)入场时不可判定,见 AuditHoldingDuration。
func EvaluateBehaviorGate(cfg BehaviorGateConfig, in GateInput) GateResult {
	if !cfg.Enabled {
		return GateResult{LiveEligible: true, Warnings: []string{"behavior gate disabled"}}
	}
	var rej []string

	// 1. Isolated only — Cross 共享保证金 = 一笔爆全爆。
	if in.MarginMode != MarginModeIsolated {
		rej = append(rej, GateCrossMargin)
	}

	// 3. loss → 同币 cooldown(报复单: 画像实测 30min 内重进 4 次累计 -29.35)。
	if !in.LastLossSameSymbolAt.IsZero() && in.NewSetupID == "" {
		cooldown := time.Duration(cfg.CooldownMinutes) * time.Minute
		if in.Now.Sub(in.LastLossSameSymbolAt) < cooldown {
			rej = append(rej, GateCooldown)
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

	return GateResult{LiveEligible: len(rej) == 0, Rejections: rej}
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

// AuditHoldingDuration is §9b rule 2 的事后审计: <5m 人工剥头皮
// (画像实测 6 笔 0 胜 -29.76)。返回违规 code,合规返回空串。
func AuditHoldingDuration(openedAt, closedAt time.Time) string {
	if openedAt.IsZero() || closedAt.IsZero() || !closedAt.After(openedAt) {
		return ""
	}
	if closedAt.Sub(openedAt) < 5*time.Minute {
		return GateScalpTooShort
	}
	return ""
}
