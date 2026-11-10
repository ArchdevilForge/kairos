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

		// 规则 7: liquidation ≠ stop
		{"no stop", func(in *GateInput) { in.StopPrice = 0 }, []string{GateNoStop}},
		{"stop equals liquidation", func(in *GateInput) {
			in.StopPrice = in.LiquidationPrice
		}, []string{GateNoStop}},
		{"stop within 0.5% of liquidation", func(in *GateInput) {
			in.StopPrice = in.LiquidationPrice * 1.004
		}, []string{GateNoStop}},
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

func TestAuditHoldingDuration(t *testing.T) {
	open := eveningTime(20, 0)
	if code := AuditHoldingDuration(open, open.Add(4*time.Minute)); code != GateScalpTooShort {
		t.Fatalf("4min hold want %s, got %q", GateScalpTooShort, code)
	}
	if code := AuditHoldingDuration(open, open.Add(6*time.Minute)); code != "" {
		t.Fatalf("6min hold want pass, got %q", code)
	}
	if code := AuditHoldingDuration(time.Time{}, open); code != "" {
		t.Fatalf("zero open time must not flag, got %q", code)
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

	seq := AuditExitSequence([]ExitAuditInput{
		{
			TicketID: "t1", HasValidTicket: true, Symbol: "SOL/USDT:USDT",
			Direction: types.CycleDirectionUp, OpenedAt: open, ClosedAt: open.Add(60 * time.Second),
			ExitReason: types.ExitReasonProtectiveStop, TouchedPresetStop: true, RealizedR: -1.0,
		},
		{
			TicketID: "t2", HasValidTicket: true, Symbol: "SOL/USDT:USDT",
			Direction: types.CycleDirectionDown, OpenedAt: open.Add(90 * time.Second), ClosedAt: open.Add(150 * time.Second),
			ExitReason: types.ExitReasonProtectiveStop, TouchedPresetStop: true, RealizedR: -1.0,
		},
		{
			TicketID: "t3", HasValidTicket: true, Symbol: "SOL/USDT:USDT",
			Direction: types.CycleDirectionUp, OpenedAt: open.Add(180 * time.Second), ClosedAt: open.Add(220 * time.Second),
			ExitReason: types.ExitReasonProtectiveStop, TouchedPresetStop: true, RealizedR: -1.0,
		},
	})
	if !seq[2].IsViolation || seq[2].Code != GateChurnFlipLt5m {
		t.Fatalf("2nd direction flip within <5m without structural reset must be churn violation: %+v", seq[2])
	}
}
