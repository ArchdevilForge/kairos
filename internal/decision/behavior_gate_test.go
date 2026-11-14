package decision

import (
	"slices"
	"testing"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// eveningTime returns a UTC+8 wall-clock time at the given hour/min.
func eveningTime(hour, minute int) time.Time {
	loc := time.FixedZone("UTC+8", 8*3600)
	return time.Date(2026, 8, 12, hour, minute, 0, 0, loc)
}

// passInput is a fully compliant baseline; each case mutates one dimension.
func passInput() GateInput {
	return GateInput{
		Now:              eveningTime(20, 0),
		Symbol:           "ETHUSDT",
		Direction:        types.CycleDirectionUp,
		MarginMode:       MarginModeIsolated,
		StopPrice:        3500,
		LiquidationPrice: 3100,
		StopBasis:        "15m HL structure low",
		MaxLossUSD:       3,
		ContextDirection: types.CycleDirectionUp,
		RankSupported:    true,
	}
}

func TestEvaluateBehaviorGate_Table(t *testing.T) {
	cfg := DefaultBehaviorGateConfig()

	cases := []struct {
		name   string
		mutate func(*GateInput)
		want   []string // 期望的 rejection codes(空 = pass)
	}{
		{"all compliant", func(in *GateInput) {}, nil},

		// 规则 1: Isolated only
		{"cross margin", func(in *GateInput) { in.MarginMode = "cross" }, []string{GateCrossMargin}},
		{"unknown margin rejected too", func(in *GateInput) { in.MarginMode = "" }, []string{GateCrossMargin}},

		// 规则 3: 同币亏损 cooldown
		{"reentry 10min after loss", func(in *GateInput) {
			in.LastLossSameSymbolAt = in.Now.Add(-10 * time.Minute)
		}, []string{GateCooldown}},
		{"reentry after cooldown passes", func(in *GateInput) {
			in.LastLossSameSymbolAt = in.Now.Add(-46 * time.Minute)
		}, nil},
		{"new setup id exempts cooldown", func(in *GateInput) {
			in.LastLossSameSymbolAt = in.Now.Add(-10 * time.Minute)
			in.NewSetupID = "setup-42"
		}, nil},

		// 规则 4: direction from CycleMap
		{"short against up context", func(in *GateInput) {
			in.Direction = types.CycleDirectionDown
		}, []string{GateCounterCycle}},
		{"long against neutral context", func(in *GateInput) {
			in.ContextDirection = types.CycleDirectionNeutral
		}, []string{GateCounterCycle}},
		{"aligned but no rank support", func(in *GateInput) {
			in.RankSupported = false
		}, []string{GateCounterCycle}},
		{"short with down context and rank", func(in *GateInput) {
			in.Direction = types.CycleDirectionDown
			in.ContextDirection = types.CycleDirectionDown
		}, nil},

		// 规则 5: evening live window 18:00–01:00
		{"noon rejected", func(in *GateInput) { in.Now = eveningTime(12, 30) }, []string{GateOutsideWindow}},
		{"17:59 rejected", func(in *GateInput) { in.Now = eveningTime(17, 59) }, []string{GateOutsideWindow}},
		{"18:00 allowed", func(in *GateInput) { in.Now = eveningTime(18, 0) }, nil},
		{"00:30 allowed (midnight wrap)", func(in *GateInput) { in.Now = eveningTime(0, 30) }, nil},
		{"01:00 rejected", func(in *GateInput) { in.Now = eveningTime(1, 0) }, []string{GateOutsideWindow}},

		// 规则 6: risk sizing
		{"loss budget over cap", func(in *GateInput) { in.MaxLossUSD = 5 }, []string{GateRiskTooLarge}},
		{"loss budget undefined", func(in *GateInput) { in.MaxLossUSD = 0 }, []string{GateRiskTooLarge}},

		// 规则 7a: liquidation ≠ stop
		{"no stop", func(in *GateInput) { in.StopPrice = 0 }, []string{GateNoStop}},
		{"stop equals liquidation", func(in *GateInput) {
			in.StopPrice = in.LiquidationPrice
		}, []string{GateNoStop}},
		{"stop within 0.5% of liquidation", func(in *GateInput) {
			in.StopPrice = in.LiquidationPrice * 1.004
		}, []string{GateNoStop}},

		// 规则 7b (Phase 3b A2): 无结构依据注释也拒绝
		{"missing stop basis rejected", func(in *GateInput) {
			in.StopBasis = ""
		}, []string{GateNoStopBasis}},
		{"whitespace-only stop basis rejected", func(in *GateInput) {
			in.StopBasis = "   \t\n "
		}, []string{GateNoStopBasis}},

		// 规则 11 (Phase 3b A1): 同点位试错预算 <= 3 (第 4 张硬拒)
		{"3rd same-setup ticket passes", func(in *GateInput) {
			in.SetupPoint = types.SetupPoint1Breakout
			in.SameSetupTrialsInWindow = 2
		}, nil},
		{"4th same-setup ticket hard rejected", func(in *GateInput) {
			in.SetupPoint = types.SetupPoint1Breakout
			in.SameSetupTrialsInWindow = 3
		}, []string{GateSetupTrialBudget}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := passInput()
			tc.mutate(&in)
			got := EvaluateBehaviorGate(cfg, in)
			if len(tc.want) == 0 {
				if !got.LiveEligible || len(got.Rejections) != 0 {
					t.Fatalf("want pass, got rejections %v", got.Rejections)
				}
				return
			}
			if got.LiveEligible {
				t.Fatalf("want reject %v, got live eligible", tc.want)
			}
			for _, code := range tc.want {
				if !slices.Contains(got.Rejections, code) {
					t.Fatalf("want code %s in %v", code, got.Rejections)
				}
			}
		})
	}
}

func TestEvaluateBehaviorGate_Disabled(t *testing.T) {
	cfg := DefaultBehaviorGateConfig()
	cfg.Enabled = false
	in := passInput()
	in.MarginMode = "cross" // 违规也放行,但要有 warning
	got := EvaluateBehaviorGate(cfg, in)
	if !got.LiveEligible || len(got.Warnings) == 0 {
		t.Fatalf("disabled gate must pass with warning, got %+v", got)
	}
}

// TestZECRevengeReplay 回放交易画像(2026-08-09)的已知案例:
// "同币亏损平仓后 30min 内重进 4 次,下一笔累计 -29.35" — 四笔必须全部被
// GATE_COOLDOWN 拦截。fixture 时间取画像中 ZEC 高频时段(晚间窗口内,
// 排除窗口规则干扰,单独验证 cooldown)。
func TestZECRevengeReplay(t *testing.T) {
	cfg := DefaultBehaviorGateConfig()
	lossAt := eveningTime(20, 0) // 亏损平仓
	reentries := []time.Time{
		eveningTime(20, 5),  // +5min
		eveningTime(20, 12), // +12min
		eveningTime(20, 21), // +21min
		eveningTime(20, 29), // +29min
	}
	for i, at := range reentries {
		in := passInput()
		in.Symbol = "ZECUSDT"
		in.Now = at
		in.LastLossSameSymbolAt = lossAt
		got := EvaluateBehaviorGate(cfg, in)
		if got.LiveEligible {
			t.Fatalf("reentry #%d at +%s must be rejected", i+1, at.Sub(lossAt))
		}
		if !slices.Contains(got.Rejections, GateCooldown) {
			t.Fatalf("reentry #%d want GATE_COOLDOWN, got %v", i+1, got.Rejections)
		}
	}
}

// TestAfternoonWindowReplay 回放画像结论 5: 12:00–18:00 PF 0.21 时段
// 的 ticket 必须 live_eligible=false。
func TestAfternoonWindowReplay(t *testing.T) {
	cfg := DefaultBehaviorGateConfig()
	for _, hour := range []int{12, 14, 16, 17} {
		in := passInput()
		in.Now = eveningTime(hour, 0)
		got := EvaluateBehaviorGate(cfg, in)
		if got.LiveEligible {
			t.Fatalf("hour %d must not be live eligible", hour)
		}
		if !slices.Contains(got.Rejections, GateOutsideWindow) {
			t.Fatalf("hour %d want GATE_OUTSIDE_WINDOW, got %v", hour, got.Rejections)
		}
	}
}

func TestPhase1LeverageHardCaps(t *testing.T) {
	cfg := DefaultBehaviorGateConfig()

	// BTC 10x passes, 12x rejected
	inBTC := passInput()
	inBTC.Symbol = "BTC/USDT:USDT"
	inBTC.Leverage = 10
	if got := EvaluateBehaviorGate(cfg, inBTC); !got.LiveEligible {
		t.Fatalf("BTC 10x must pass: %+v", got)
	}
	inBTC.Leverage = 12
	if got := EvaluateBehaviorGate(cfg, inBTC); got.LiveEligible || !slices.Contains(got.Rejections, GateLeverageExceeded) {
		t.Fatalf("BTC 12x must reject with %s, got %+v", GateLeverageExceeded, got)
	}

	// Altcoin 5x passes, 6x rejected
	inAlt := passInput()
	inAlt.Symbol = "SOL/USDT:USDT"
	inAlt.Leverage = 5
	if got := EvaluateBehaviorGate(cfg, inAlt); !got.LiveEligible {
		t.Fatalf("SOL 5x must pass: %+v", got)
	}
	inAlt.Leverage = 6
	if got := EvaluateBehaviorGate(cfg, inAlt); got.LiveEligible || !slices.Contains(got.Rejections, GateLeverageExceeded) {
		t.Fatalf("SOL 6x must reject with %s, got %+v", GateLeverageExceeded, got)
	}
}

func TestPhase1DualTrackCooldown(t *testing.T) {
	cfg := DefaultBehaviorGateConfig()

	// Track A: early unlock via NewStructuralConfirm15m1h or SECOND_WAVE
	inUnlock := passInput()
	inUnlock.LastLossSameSymbolAt = inUnlock.Now.Add(-10 * time.Minute)
	inUnlock.NewStructuralConfirm15m1h = true
	if got := EvaluateBehaviorGate(cfg, inUnlock); !got.LiveEligible {
		t.Fatalf("15m/1h structural confirmation should unlock 2nd ticket early: %+v", got)
	}

	inSecondWave := passInput()
	inSecondWave.LastLossSameSymbolAt = inSecondWave.Now.Add(-10 * time.Minute)
	inSecondWave.PlaybookSwitch = "SECOND_WAVE"
	if got := EvaluateBehaviorGate(cfg, inSecondWave); !got.LiveEligible {
		t.Fatalf("SECOND_WAVE playbook switch should unlock 2nd ticket early: %+v", got)
	}

	// Track B: non-overridable 24h lock on 2 consecutive stops or <= -1.5R cumulative trial loss
	inConsec := passInput()
	inConsec.ConsecutiveStops24h = 2
	inConsec.NewStructuralConfirm15m1h = true // cannot override rigid floor!
	gotConsec := EvaluateBehaviorGate(cfg, inConsec)
	if gotConsec.LiveEligible || !gotConsec.NonOverridable || !slices.Contains(gotConsec.Rejections, GateRigid24hLock) {
		t.Fatalf("2 consecutive stops must trigger rigid 24h lock: %+v", gotConsec)
	}

	inCumLoss := passInput()
	inCumLoss.CumulativeTrialLoss24hR = -1.55
	inCumLoss.PlaybookSwitch = "SECOND_WAVE" // cannot override rigid floor!
	gotCum := EvaluateBehaviorGate(cfg, inCumLoss)
	if gotCum.LiveEligible || !gotCum.NonOverridable || !slices.Contains(gotCum.Rejections, GateRigid24hLock) {
		t.Fatalf("cumulative trial loss <= -1.5R must trigger rigid 24h lock: %+v", gotCum)
	}
}

func TestPhase1AuditExitMatrix(t *testing.T) {
	open := eveningTime(20, 0)

	// 1. Protective stop <5m => compliant, 0% violation misclassification, scores +1.0
	resStop := AuditExit(ExitAuditInput{
		TicketID:          "t-stop",
		HasValidTicket:    true,
		Symbol:            "SOL/USDT:USDT",
		Direction:         types.CycleDirectionUp,
		OpenedAt:          open,
		ClosedAt:          open.Add(90 * time.Second),
		ExitReason:        types.ExitReasonProtectiveStop,
		RealizedR:         -1.08,
		PresetStopPrice:   99.0,
		ExitPrice:         98.95,
		TouchedPresetStop: true,
	})
	if resStop.IsViolation || resStop.Verdict != VerdictProtectiveStopExempt || resStop.ScoreDelta <= 0 {
		t.Fatalf("protective stop <5m must be compliant with positive score: %+v", resStop)
	}

	// 2. Early profit <5m with 0~+0.5R and intact structure => review note, NOT a violation
	resEarly := AuditExit(ExitAuditInput{
		TicketID:        "t-early",
		HasValidTicket:  true,
		Symbol:          "SOL/USDT:USDT",
		Direction:       types.CycleDirectionUp,
		OpenedAt:        open,
		ClosedAt:        open.Add(3 * time.Minute),
		ExitReason:      types.ExitReasonImpulseEarly,
		RealizedR:       0.25,
		StructureIntact: true,
	})
	if resEarly.IsViolation || resEarly.Verdict != VerdictEarlyProfitNote || resEarly.Code != NoteEarlyProfitLt5m {
		t.Fatalf("early profit <5m with intact structure must be review note only: %+v", resEarly)
	}

	// 3. True churn: opening without a valid ticket OR >=2 flips within <5m
	resNoTicket := AuditExit(ExitAuditInput{
		HasValidTicket: false,
		Symbol:         "SOL/USDT:USDT",
		Direction:      types.CycleDirectionUp,
		OpenedAt:       open,
		ClosedAt:       open.Add(10 * time.Minute),
	})
	if !resNoTicket.IsViolation || resNoTicket.Code != GateNoValidTicket {
		t.Fatalf("opening without valid ticket must be violation: %+v", resNoTicket)
	}

	// 4. BehaviorGateConfigFrom mapping + short preset stop price touch + >0.5R structural exit + unexcused <5m loss
	on := true
	mapped := BehaviorGateConfigFrom(types.RiskGateConfig{
		Enabled:                true,
		CooldownMinutes:        30,
		WindowStartHour:        19,
		WindowEndHour:          2,
		MaxLossUSD:             10,
		EnforceLeverageCap:     &on,
		MaxMajorLeverage:       10,
		MaxAltLeverage:         5,
		TrancheBaseUSD:         10_000,
		TrialBudgetMax:         3,
		TrialBudgetWindowHours: 24,
		TiltReverseMinutes:     25,
		ChurnConsecutiveStops:  2,
		ChurnWindowMinutes:     45,
		ChurnTicketCount:       3,
		SizeEscalationMult:     1.6,
		RecentRiskSampleSize:   12,
	})
	if !mapped.LeverageCapEnabled() || mapped.MaxMajorLeverage != 10 || mapped.MaxAltLeverage != 5 || mapped.TiltReverseMinutes != 25 {
		t.Fatalf("BehaviorGateConfigFrom=%+v", mapped)
	}

	resShortPriceStop := AuditExit(ExitAuditInput{
		TicketID:        "t-short-stop",
		HasValidTicket:  true,
		Symbol:          "ETH/USDT:USDT",
		Direction:       types.CycleDirectionDown,
		OpenedAt:        open,
		ClosedAt:        open.Add(2 * time.Minute),
		PresetStopPrice: 102.0,
		ExitPrice:       102.1,
		RealizedR:       -1.05,
	})
	if resShortPriceStop.IsViolation || resShortPriceStop.Verdict != VerdictProtectiveStopExempt {
		t.Fatalf("short exit >= preset stop price must be protective stop exempt: %+v", resShortPriceStop)
	}

	resTargetFast := AuditExit(ExitAuditInput{
		TicketID:       "t-target-fast",
		HasValidTicket: true,
		Symbol:         "ETH/USDT:USDT",
		Direction:      types.CycleDirectionUp,
		OpenedAt:       open,
		ClosedAt:       open.Add(3 * time.Minute),
		ExitReason:     types.ExitReasonTargetPartial,
		RealizedR:      0.85,
	})
	if resTargetFast.IsViolation || resTargetFast.Verdict != VerdictCompliant {
		t.Fatalf(">0.5R target exit <5m must be compliant: %+v", resTargetFast)
	}
}

// TestPhase3b_A1UnlabeledDowngradeAndA4RelapseWarnings verifies:
//   - A1 unlabeled ticket (>=4th on same symbol without SetupPoint) downgrades to warning only, never blocks.
//   - A4 STRATEGY §12 relapse patterns (上头反手 / 连错刷单 / 亏后加码) emit factual warnings and NEVER block.
func TestPhase3b_A1UnlabeledDowngradeAndA4RelapseWarnings(t *testing.T) {
	cfg := DefaultBehaviorGateConfig()

	cases := []struct {
		name         string
		mutate       func(*GateInput)
		wantWarnings []string
		forbidWarn   []string
	}{
		{
			name: "A1 unlabeled 4th ticket on same symbol warns without blocking",
			mutate: func(in *GateInput) {
				in.SetupPoint = "" // must NOT fabricate setup identity
				in.SameSymbolTrialsInWindow = 3
			},
			wantWarnings: []string{WarnUnlabeledTrialBudget, "第 4 张票"},
		},
		{
			name: "A4(i) tilt reverse within 30m after stop warns without blocking",
			mutate: func(in *GateInput) {
				in.Direction = types.CycleDirectionUp
				in.ContextDirection = types.CycleDirectionUp
				in.LastStopSameSymbolAt = in.Now.Add(-15 * time.Minute)
				in.LastStopSameSymbolDirection = types.CycleDirectionDown
			},
			wantWarnings: []string{WarnTiltReverse, "上头反手", "15.0 分钟"},
		},
		{
			name: "A4(i) same-direction after stop does NOT warn tilt reverse",
			mutate: func(in *GateInput) {
				in.LastStopSameSymbolAt = in.Now.Add(-15 * time.Minute)
				in.LastStopSameSymbolDirection = types.CycleDirectionUp
			},
			forbidWarn: []string{WarnTiltReverse},
		},
		{
			name: "A4(i) reverse after >30m does NOT warn tilt reverse",
			mutate: func(in *GateInput) {
				in.LastStopSameSymbolAt = in.Now.Add(-31 * time.Minute)
				in.LastStopSameSymbolDirection = types.CycleDirectionDown
			},
			forbidWarn: []string{WarnTiltReverse},
		},
		{
			name: "A4(ii) consecutive stops + high-frequency tickets warns 连错刷单 without blocking",
			mutate: func(in *GateInput) {
				in.GlobalConsecutiveStops = 2
				in.RecentTicketsInChurnWindow = 2 // +1 current = 3rd ticket in 60m
			},
			wantWarnings: []string{WarnLossStreakChurn, "连错刷单", "连续止损 2 笔", "第 3 张票"},
		},
		{
			name: "A4(iii) post-loss risk >= 1.5x personal median warns 亏后加码 without blocking",
			mutate: func(in *GateInput) {
				in.LastOutcomeWasLoss = true
				in.MaxLossUSD = 3.6
				in.RecentPersonalRiskUSD = []float64{2.0, 2.0, 2.2, 1.8} // median = 2.0 -> 3.6 = 1.80x
			},
			wantWarnings: []string{WarnLossSizeEscalation, "亏后加码", "1.80×"},
		},
		{
			name: "A4(iii) <3 historical samples does NOT fabricate median",
			mutate: func(in *GateInput) {
				in.LastOutcomeWasLoss = true
				in.MaxLossUSD = 3.9
				in.RecentPersonalRiskUSD = []float64{1.0, 1.0}
			},
			forbidWarn: []string{WarnLossSizeEscalation},
		},
		{
			name: "A4(iii) prior outcome win does NOT warn 亏后加码",
			mutate: func(in *GateInput) {
				in.LastOutcomeWasLoss = false
				in.MaxLossUSD = 3.9
				in.RecentPersonalRiskUSD = []float64{1.5, 1.5, 1.5}
			},
			forbidWarn: []string{WarnLossSizeEscalation},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := passInput()
			tc.mutate(&in)
			got := EvaluateBehaviorGate(cfg, in)
			if !got.LiveEligible || len(got.Rejections) != 0 {
				t.Fatalf("warnings must NEVER block: got LiveEligible=%v Rejections=%v", got.LiveEligible, got.Rejections)
			}
			joined := ""
			for _, w := range got.Warnings {
				joined += w + "\n"
			}
			for _, sub := range tc.wantWarnings {
				if !slices.ContainsFunc(got.Warnings, func(w string) bool { return containsStr(w, sub) }) {
					t.Fatalf("missing warning substring %q in %q", sub, joined)
				}
			}
			for _, sub := range tc.forbidWarn {
				if slices.ContainsFunc(got.Warnings, func(w string) bool { return containsStr(w, sub) }) {
					t.Fatalf("unexpected warning substring %q in %q", sub, joined)
				}
			}
		})
	}
}

func containsStr(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
