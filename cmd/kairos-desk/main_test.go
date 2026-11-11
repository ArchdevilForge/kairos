package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ArchdevilForge/kairos/internal/opportunity"
	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
)

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	runErr := fn()
	_ = w.Close()
	os.Stdout = orig
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	_ = r.Close()
	return buf.String(), runErr
}

func TestOpenJournalAndRejectFlow(t *testing.T) {
	dir := t.TempDir()
	j, err := storage.NewJournal(types.StorageConfig{DatabasePath: filepath.Join(dir, "kairos.db")})
	if err != nil {
		t.Fatal(err)
	}
	svc := opportunity.NewService(j, opportunity.DefaultConfig())
	if _, err := svc.HandlePulseEvent(types.AnomalyEvent{
		EventType: "market_impulse", EventID: "desk-e1", Timestamp: 1,
		Data: map[string]any{"direction": "up", "state_to": "IMPULSE_UP"},
	}); err != nil {
		t.Fatal(err)
	}
	// manual ticket for CLI decision path
	tkt := types.DecisionTicket{
		SchemaVersion: types.DecisionTicketSchemaVersion,
		ID:            "t-desk-1", SessionID: "sess-desk-e1", Symbol: "ETH/USDT:USDT",
		Status: types.TicketStatusOpen, Grade: types.TicketGradeB,
	}
	if err := j.SaveTicket(tkt); err != nil {
		t.Fatal(err)
	}
	if err := svc.ApplyHumanDecision("t-desk-1", types.DecisionRejected, []string{types.ReasonTooExtended}, ""); err != nil {
		t.Fatal(err)
	}
	got, ok, err := j.GetTicket("t-desk-1")
	if err != nil || !ok || got.Status != types.TicketStatusRejected {
		t.Fatalf("got=%+v ok=%v err=%v", got, ok, err)
	}

	// openJournal with dir override
	j2, err := openJournal("missing.yaml", dir)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := j2.ListSessions()
	if err != nil || len(sessions) < 1 {
		t.Fatalf("sessions via openJournal: %d err=%v", len(sessions), err)
	}

	// CLI: flags before ticket id (Go FlagSet stops at first positional)
	_ = j.SaveTicket(types.DecisionTicket{ID: "t-desk-2", SessionID: "s", Status: types.TicketStatusOpen, SchemaVersion: types.DecisionTicketSchemaVersion})
	if err := run([]string{"-journal", dir, "reject", "--reason", "too_extended", "t-desk-2"}); err != nil {
		t.Fatalf("cli reject: %v", err)
	}
}

// TestAcceptGateFlow: §9b gate 拦截 accept(cross margin+无 stop 等多重违规),
// --override 放行但必须留 annotation。
func TestAcceptGateFlow(t *testing.T) {
	dir := t.TempDir()
	j, err := storage.NewJournal(types.StorageConfig{DatabasePath: filepath.Join(dir, "kairos.db")})
	if err != nil {
		t.Fatal(err)
	}
	tkt := types.DecisionTicket{
		SchemaVersion: types.DecisionTicketSchemaVersion,
		ID:            "t-gate-1", SessionID: "s", Symbol: "ZEC/USDT:USDT",
		Status: types.TicketStatusOpen, Grade: types.TicketGradeB,
		Direction: types.CycleDirectionUp,
	}
	if err := j.SaveTicket(tkt); err != nil {
		t.Fatal(err)
	}

	// 无 --margin/--max-loss/stop → gate 必拒(missing.yaml → doctrine 默认 gate 常开)
	err = run([]string{"-journal", dir, "accept", "--reason", "structure_good", "t-gate-1"})
	if err == nil {
		t.Fatal("gate must reject non-compliant accept")
	}

	// 被拒后 annotation 已留痕
	anns, err := j.ListAnnotations()
	if err != nil || len(anns) != 1 || anns[0].Override {
		t.Fatalf("want 1 non-override annotation, got %+v err=%v", anns, err)
	}

	// --override 放行,再留一条 override annotation
	if err := run([]string{"-journal", dir, "accept", "--reason", "manual_override",
		"--note", "test override", "--override", "t-gate-1"}); err != nil {
		t.Fatalf("override accept: %v", err)
	}
	got, ok, _ := j.GetTicket("t-gate-1")
	if !ok || got.Status != types.TicketStatusAccepted {
		t.Fatalf("ticket not accepted after override: %+v", got)
	}
	anns, _ = j.ListAnnotations()
	if len(anns) != 2 || !anns[1].Override {
		t.Fatalf("want override annotation, got %+v", anns)
	}

	// gate 子命令: 干跑不改状态
	if err := run([]string{"-journal", dir, "gate", "--margin", "isolated", "--max-loss", "3", "t-gate-1"}); err != nil {
		t.Fatalf("gate subcommand: %v", err)
	}
}

func TestCloseWithSingleKeyExitReasonAndFunding(t *testing.T) {
	dir := t.TempDir()
	j, err := storage.NewJournal(types.StorageConfig{DatabasePath: filepath.Join(dir, "kairos.db")})
	if err != nil {
		t.Fatal(err)
	}
	tkt := types.DecisionTicket{
		SchemaVersion: types.DecisionTicketSchemaVersion,
		ID:            "t-close-1", SessionID: "s", Symbol: "SOL/USDT:USDT",
		Status: types.TicketStatusAccepted, Grade: types.TicketGradeA,
		Direction:        types.CycleDirectionUp,
		EntryTriggeredAt: 1_700_000_000,
		RiskPlan:         types.RiskPlan{EntryPrice: 100, StopPrice: 98, SuggestedNotional: 10_000},
	}
	if err := j.SaveTicket(tkt); err != nil {
		t.Fatal(err)
	}
	if err := j.SaveDecision(types.DecisionRecord{TicketID: "t-close-1", Decision: types.DecisionAccepted}); err != nil {
		t.Fatal(err)
	}

	// Single-key 'p' -> PROTECTIVE_STOP + funding-usd
	if err := run([]string{
		"-journal", dir, "close",
		"--exit-price", "98.0",
		"--funding-usd", "2.5",
		"--exit-reason", "p",
		"t-close-1",
	}); err != nil {
		t.Fatalf("close subcommand failed: %v", err)
	}

	gotTkt, ok, err := j.GetTicket("t-close-1")
	if err != nil || !ok {
		t.Fatalf("GetTicket: ok=%v err=%v", ok, err)
	}
	if gotTkt.ExitReason != types.ExitReasonProtectiveStop || gotTkt.RealizedFundingUSD != 2.5 {
		t.Fatalf("ticket ExitReason=%q RealizedFundingUSD=%v", gotTkt.ExitReason, gotTkt.RealizedFundingUSD)
	}
}

func TestPhase3b_DeskGateShowAcceptFlow(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*3600)
	fixedNow := time.Date(2026, 11, 11, 20, 30, 0, 0, loc)
	origNow := deskNowFunc
	deskNowFunc = func() time.Time { return fixedNow }
	defer func() { deskNowFunc = origNow }()

	dir := t.TempDir()
	j, err := storage.NewJournal(types.StorageConfig{DatabasePath: filepath.Join(dir, "kairos.db")})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Ticket missing stop basis -> rejected with GATE_NO_STOP_BASIS; passes when --stop-basis supplied.
	tNoBasis := types.DecisionTicket{
		SchemaVersion: types.DecisionTicketSchemaVersion,
		ID:            "t-nobasis",
		SessionID:     "s1",
		Symbol:        "ETH/USDT:USDT",
		PlaybookID:    types.PlaybookLeaderPullbackV1,
		Grade:         types.TicketGradeA,
		Direction:     types.CycleDirectionUp,
		Status:        types.TicketStatusOpen,
		MarketCycle:   types.CycleMap{PrimaryDirection: types.CycleDirectionUp},
		RiskPlan: types.RiskPlan{
			EntryPrice:        2500,
			StopPrice:         2450,
			StopDistancePct:   2.0,
			SuggestedNotional: 150,
			MaxNotional:       200,
			MaxLeverage:       10,
			TrancheBaseUSD:    10_000,
		},
		CreatedAt: fixedNow.Unix(),
	}
	if err := j.SaveTicket(tNoBasis); err != nil {
		t.Fatal(err)
	}
	outRej, err := captureStdout(t, func() error {
		return run([]string{"-journal", dir, "gate", "--margin", "isolated", "--max-loss", "3", "t-nobasis"})
	})
	if err != nil || !strings.Contains(outRej, "REJECT GATE_NO_STOP_BASIS") {
		t.Fatalf("expected REJECT GATE_NO_STOP_BASIS, got out=%q err=%v", outRej, err)
	}
	outPass, err := captureStdout(t, func() error {
		return run([]string{"-journal", dir, "gate", "--margin", "isolated", "--max-loss", "3", "--stop-basis", "15m HL swing low", "t-nobasis"})
	})
	if err != nil || !strings.Contains(outPass, "PASS live_eligible=true") ||
		!strings.Contains(outPass, "basis=15m HL swing low") || !strings.Contains(outPass, "tranche: base=$10000.00") {
		t.Fatalf("expected PASS + stop/tranche display, got out=%q err=%v", outPass, err)
	}

	// 2. A1 trial budget: 3 prior POINT_1_BREAKOUT tickets on SOL -> 4th rejected with GATE_SETUP_TRIAL_BUDGET.
	for i := 1; i <= 3; i++ {
		prior := types.DecisionTicket{
			SchemaVersion: types.DecisionTicketSchemaVersion,
			ID:            "sol-p1-" + string(rune('0'+i)),
			SessionID:     "s1",
			Symbol:        "SOL/USDT:USDT",
			PlaybookID:    types.PlaybookLeaderPullbackV1,
			SetupPoint:    types.SetupPoint1Breakout,
			Grade:         types.TicketGradeA,
			Direction:     types.CycleDirectionUp,
			Status:        types.TicketStatusAccepted,
			CreatedAt:     fixedNow.Add(-time.Duration(i) * time.Hour).Unix(),
		}
		if err := j.SaveTicket(prior); err != nil {
			t.Fatal(err)
		}
		ml := 2.0
		_ = j.SaveDecision(types.DecisionRecord{
			TicketID: prior.ID, Decision: types.DecisionAccepted,
			PlannedMaxLossUSD: &ml, DecidedAt: prior.CreatedAt,
		})
	}
	tSol4 := types.DecisionTicket{
		SchemaVersion: types.DecisionTicketSchemaVersion,
		ID:            "sol-p1-4",
		SessionID:     "s1",
		Symbol:        "SOL/USDT:USDT",
		PlaybookID:    types.PlaybookLeaderPullbackV1,
		Grade:         types.TicketGradeA,
		Direction:     types.CycleDirectionUp,
		Status:        types.TicketStatusOpen,
		MarketCycle:   types.CycleMap{PrimaryDirection: types.CycleDirectionUp},
		Invalidations: []string{"box high breaks"},
		RiskPlan:      types.RiskPlan{EntryPrice: 100, StopPrice: 98, StopDistancePct: 2, SuggestedNotional: 180, MaxLeverage: 5, TrancheBaseUSD: 10_000},
		CreatedAt:     fixedNow.Unix(),
	}
	_ = j.SaveTicket(tSol4)

	// Unlabeled: warns WARN_UNLABELED_TRIAL_BUDGET, does NOT block!
	outUnlabeled, err := captureStdout(t, func() error {
		return run([]string{"-journal", dir, "gate", "--margin", "isolated", "--max-loss", "3", "sol-p1-4"})
	})
	if err != nil || !strings.Contains(outUnlabeled, "PASS live_eligible=true") ||
		!strings.Contains(outUnlabeled, "WARN_UNLABELED_TRIAL_BUDGET") {
		t.Fatalf("unlabeled 4th ticket must warn without blocking, got out=%q err=%v", outUnlabeled, err)
	}

	// Labeled via --setup-point 1: hard-blocks with GATE_SETUP_TRIAL_BUDGET!
	outLabeled, err := captureStdout(t, func() error {
		return run([]string{"-journal", dir, "gate", "--margin", "isolated", "--max-loss", "3", "--setup-point", "1", "sol-p1-4"})
	})
	if err != nil || !strings.Contains(outLabeled, "REJECT GATE_SETUP_TRIAL_BUDGET") {
		t.Fatalf("labeled 4th ticket must hard-reject with GATE_SETUP_TRIAL_BUDGET, got out=%q err=%v", outLabeled, err)
	}

	// 3. A4 relapse warnings on `show` & `accept`: prior short stop 10m ago on BTC + 3 risk samples ($2) + current long $3.5
	// (with --structural-confirm unlocking normal cooldown) -> emits 上头反手 & 亏后加码 warnings WITHOUT blocking accept!
	tPriorStop := types.DecisionTicket{
		SchemaVersion: types.DecisionTicketSchemaVersion,
		ID:            "btc-stop-1",
		SessionID:     "s1",
		Symbol:        "BTC/USDT:USDT",
		PlaybookID:    types.PlaybookLeaderPullbackV1,
		Grade:         types.TicketGradeA,
		Direction:     types.CycleDirectionDown,
		Status:        types.TicketStatusClosed,
		ExitReason:    types.ExitReasonProtectiveStop,
		ClosedAt:      fixedNow.Add(-10 * time.Minute).Unix(),
		CreatedAt:     fixedNow.Add(-20 * time.Minute).Unix(),
	}
	_ = j.SaveTicket(tPriorStop)
	_ = j.SaveOutcome(storage.CounterfactualOutcome{
		TicketID:     "btc-stop-1",
		SessionID:    "s1",
		Symbol:       "BTC/USDT:USDT",
		Direction:    types.CycleDirectionDown,
		Decision:     types.DecisionAccepted,
		NetR:         -1.0,
		StopHitFirst: true,
		Finalized:    true,
		PathEndUnix:  fixedNow.Add(-10 * time.Minute).Unix(),
	})
	tBtcRev := types.DecisionTicket{
		SchemaVersion: types.DecisionTicketSchemaVersion,
		ID:            "btc-rev-2",
		SessionID:     "s1",
		Symbol:        "BTC/USDT:USDT",
		PlaybookID:    types.PlaybookLeaderPullbackV1,
		Grade:         types.TicketGradeA,
		Direction:     types.CycleDirectionUp,
		Status:        types.TicketStatusOpen,
		MarketCycle:   types.CycleMap{PrimaryDirection: types.CycleDirectionUp},
		Invalidations: []string{"15m HL 68000"},
		RiskPlan:      types.RiskPlan{EntryPrice: 69000, StopPrice: 68000, StopDistancePct: 1.45, SuggestedNotional: 250, MaxLeverage: 10, TrancheBaseUSD: 10_000},
		CreatedAt:     fixedNow.Unix(),
	}
	_ = j.SaveTicket(tBtcRev)

	outShow, err := captureStdout(t, func() error {
		return run([]string{"-journal", dir, "show", "btc-rev-2"})
	})
	if err != nil || !strings.Contains(outShow, "WARN_TILT_REVERSE [上头反手]") || !strings.Contains(outShow, "WARN_LOSS_SIZE_ESCALATION [亏后加码]") {
		t.Fatalf("show must render A4 warnings, got out=%q err=%v", outShow, err)
	}

	outAccept, err := captureStdout(t, func() error {
		return run([]string{
			"-journal", dir, "accept",
			"--reason", "structure_good",
			"--margin", "isolated",
			"--max-loss", "3.5",
			"--structural-confirm",
			"--setup-point", "2",
			"btc-rev-2",
		})
	})
	if err != nil {
		t.Fatalf("A4 warnings must NEVER block accept: %v (out=%q)", err, outAccept)
	}
	if !strings.Contains(outAccept, "WARN_TILT_REVERSE [上头反手]") ||
		!strings.Contains(outAccept, "WARN_LOSS_SIZE_ESCALATION [亏后加码]") ||
		!strings.Contains(outAccept, "ok btc-rev-2 accepted") {
		t.Fatalf("unexpected accept output: %q", outAccept)
	}
}
