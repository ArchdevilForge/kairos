// Command kairos-desk is the human decision CLI for OpportunitySessions / tickets.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ArchdevilForge/kairos/internal/config"
	"github.com/ArchdevilForge/kairos/internal/decision"
	"github.com/ArchdevilForge/kairos/internal/opportunity"
	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
)

var (
	closeInputReader io.Reader = os.Stdin
	deskNowFunc                = time.Now
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "kairos-desk: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	// Root flags only (config/journal/json). Subcommand flags parsed per-command
	// so `reject <id> --reason x` works (Go flag stops at first non-flag otherwise).
	fs := flag.NewFlagSet("kairos-desk", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cfgPath := fs.String("config", "config.yaml", "config file (storage path)")
	journalPath := fs.String("journal", "", "override path to trading-journal.jsonl (or its directory)")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 1 {
		return fmt.Errorf("usage: kairos-desk [-config] <sessions|tickets|candidates|show|gate|accept|wait|reject|missed|close>")
	}
	cmd := rest[0]
	cmdArgs := rest[1:]

	journal, err := openJournal(*cfgPath, *journalPath)
	if err != nil {
		return err
	}
	svc := opportunity.NewService(journal, opportunity.DefaultConfig())

	switch cmd {
	case "sessions":
		list, err := journal.ListSessions()
		if err != nil {
			return err
		}
		if *asJSON {
			return encode(list)
		}
		for _, s := range list {
			fmt.Printf("%s  event=%s  pulse=%s/%s  status=%s  created=%d\n",
				s.ID, s.EventID, s.PulseState, s.PulseDirection, s.Status, s.CreatedAt)
		}
		if len(list) == 0 {
			fmt.Println("(no sessions)")
		}
		return nil

	case "tickets":
		tfs := flag.NewFlagSet("tickets", flag.ContinueOnError)
		tfs.SetOutput(os.Stderr)
		sessionFilter := tfs.String("session", "", "filter by session id")
		if err := tfs.Parse(cmdArgs); err != nil {
			return err
		}
		list, err := journal.ListTickets(*sessionFilter)
		if err != nil {
			return err
		}
		if *asJSON {
			return encode(list)
		}
		for _, t := range list {
			fmt.Printf("%s  %s  %s  grade=%s  class=%s  status=%s  session=%s\n",
				t.ID, t.Symbol, t.Direction, t.Grade, t.TradeClass, t.Status, t.SessionID)
		}
		if len(list) == 0 {
			fmt.Println("(no tickets)")
		}
		return nil

	case "candidates":
		tfs := flag.NewFlagSet("candidates", flag.ContinueOnError)
		tfs.SetOutput(os.Stderr)
		sessionFilter := tfs.String("session", "", "session id")
		if err := tfs.Parse(cmdArgs); err != nil {
			return err
		}
		sid := *sessionFilter
		if sid == "" && tfs.NArg() > 0 {
			sid = tfs.Arg(0)
		}
		if sid == "" {
			return fmt.Errorf("usage: kairos-desk candidates --session <id>")
		}
		c, ok, err := journal.GetCandidates(sid)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Println("(no candidates)")
			return nil
		}
		if *asJSON {
			return encode(c)
		}
		for i, cand := range c.Candidates {
			fmt.Printf("%d  %s  long=%.2f short=%.2f RS=%.2f RW=%.2f liq=%v spread=%v warn=%v\n",
				i+1, cand.Symbol, cand.LongScore, cand.ShortScore, cand.RelativeStrength, cand.RelativeWeakness,
				cand.LiquidityOK, cand.SpreadOK, cand.Warnings)
		}
		return nil

	case "show":
		if len(cmdArgs) < 1 {
			return fmt.Errorf("usage: kairos-desk show <ticket-id>")
		}
		t, ok, err := journal.GetTicket(cmdArgs[0])
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("ticket not found: %s", cmdArgs[0])
		}
		if *asJSON {
			return encode(t)
		}
		warnings := computeShowWarnings(journal, *cfgPath, t)
		fmt.Println(decision.FormatTicketWithWarnings(t, warnings))
		if dec, ok, _ := journal.GetDecision(t.ID); ok {
			fmt.Printf("human: %s reasons=%v note=%q at=%d\n", dec.Decision, dec.ReasonCodes, dec.Note, dec.DecidedAt)
		}
		return nil

	case "accept", "wait", "reject", "missed":
		dfs := flag.NewFlagSet(cmd, flag.ContinueOnError)
		dfs.SetOutput(os.Stderr)
		reason := dfs.String("reason", "", "reason code (comma-separated)")
		note := dfs.String("note", "", "free-text note")
		// Behavior gate inputs (doctrine §9b + Phase 1 + Phase 3b) — accept only.
		margin := dfs.String("margin", "", "margin mode (isolated required for live)")
		maxLoss := dfs.Float64("max-loss", 0, "predefined max loss USD for this trade")
		stopBasis := dfs.String("stop-basis", "", "structural stop invalidation basis text (Phase 3b A2)")
		setupPointRaw := dfs.String("setup-point", "", "explicit §3 setup point enum or 1..6 (Phase 3b A1)")
		newSetup := dfs.String("new-setup", "", "new setup id (exempts same-symbol cooldown)")
		structConfirm := dfs.Bool("structural-confirm", false, "new independent 15m/1h structural confirmation (early cooldown unlock)")
		playbookSwitch := dfs.String("playbook-switch", "", "playbook switch (e.g. SECOND_WAVE for early cooldown unlock)")
		leverage := dfs.Float64("leverage", 0, "requested leverage (checked against BTC/ETH<=10x, alts<=5x)")
		notional := dfs.Float64("notional", 0, "requested notional USD")
		trancheBase := dfs.Float64("tranche-base", 0, "accounting tranche base USD")
		liq := dfs.Float64("liq", 0, "liquidation price (optional; stop近强平价则拒)")
		override := dfs.Bool("override", false, "override gate rejection (leaves annotation)")
		if err := dfs.Parse(cmdArgs); err != nil {
			return err
		}
		if dfs.NArg() < 1 {
			return fmt.Errorf("usage: kairos-desk %s --reason code [--note text] <ticket-id>", cmd)
		}
		ticketID := dfs.Arg(0)
		var parsedSetup types.SetupPoint
		if strings.TrimSpace(*setupPointRaw) != "" {
			sp, ok := types.ParseSetupPoint(*setupPointRaw)
			if !ok {
				return fmt.Errorf("unknown --setup-point %q (valid: 1..6 or %v)", *setupPointRaw, types.ValidSetupPoints())
			}
			parsedSetup = sp
		}
		d := map[string]types.HumanDecision{
			"accept": types.DecisionAccepted,
			"wait":   types.DecisionWaiting,
			"reject": types.DecisionRejected,
			"missed": types.DecisionMissed,
		}[cmd]
		var codes []string
		if *reason != "" {
			for _, c := range strings.Split(*reason, ",") {
				c = strings.TrimSpace(c)
				if c != "" {
					codes = append(codes, c)
				}
			}
		}
		// Canonical §8: reason codes are required for every decision (the
		// service layer enforces too — this is just the friendlier CLI error).
		if len(codes) == 0 {
			return fmt.Errorf("%s requires --reason <code> (e.g. too_extended)", cmd)
		}
		if cmd == "accept" {
			res, gateCodes, err := runGate(journal, *cfgPath, ticketID, gateFlags{
				Margin:            *margin,
				MaxLoss:           *maxLoss,
				StopBasis:         *stopBasis,
				SetupPoint:        parsedSetup,
				NewSetup:          *newSetup,
				StructuralConfirm: *structConfirm,
				PlaybookSwitch:    *playbookSwitch,
				Leverage:          *leverage,
				NotionalUSD:       *notional,
				TrancheBaseUSD:    *trancheBase,
				Liq:               *liq,
			})
			if err != nil {
				return err
			}
			for _, w := range res.Warnings {
				fmt.Printf("⚠ %s\n", w)
			}
			if !res.LiveEligible {
				allowOverride := *override && !res.NonOverridable
				ann := storage.GateAnnotation{
					TicketID: ticketID, Codes: gateCodes,
					Override: allowOverride, Note: *note, At: deskNowFunc().Unix(),
				}
				if err := journal.SaveAnnotation(ann); err != nil {
					return err
				}
				if res.NonOverridable {
					return fmt.Errorf("behavior gate hard-locked (%s) — 触发同币 24h 刚性锁,禁止 --override",
						strings.Join(gateCodes, ","))
				}
				if !*override {
					return fmt.Errorf("behavior gate rejected (%s) — 该笔不算合格交易;确要执行加 --override(留痕)",
						strings.Join(gateCodes, ","))
				}
				fmt.Printf("⚠ gate override recorded: %s\n", strings.Join(gateCodes, ","))
			}
		}
		if err := svc.ApplyHumanDecision(ticketID, d, codes, *note); err != nil {
			return err
		}
		if cmd == "accept" {
			if strings.TrimSpace(*stopBasis) != "" || parsedSetup != "" {
				if t, ok, err := journal.GetTicket(ticketID); err == nil && ok {
					if strings.TrimSpace(*stopBasis) != "" {
						t.StopBasis = strings.TrimSpace(*stopBasis)
					}
					if parsedSetup != "" {
						t.SetupPoint = parsedSetup
					}
					_ = journal.SaveTicket(t)
				}
			}
			if *maxLoss > 0 {
				if dec, ok, err := journal.GetDecision(ticketID); err == nil && ok {
					ml := *maxLoss
					dec.PlannedMaxLossUSD = &ml
					_ = journal.SaveDecision(dec)
				}
			}
		}
		fmt.Printf("ok %s %s\n", ticketID, d)
		return nil

	case "gate":
		gfs := flag.NewFlagSet("gate", flag.ContinueOnError)
		gfs.SetOutput(os.Stderr)
		margin := gfs.String("margin", "", "margin mode")
		maxLoss := gfs.Float64("max-loss", 0, "predefined max loss USD")
		stopBasis := gfs.String("stop-basis", "", "structural stop invalidation basis text")
		setupPointRaw := gfs.String("setup-point", "", "explicit §3 setup point enum or 1..6")
		newSetup := gfs.String("new-setup", "", "new setup id")
		structConfirm := gfs.Bool("structural-confirm", false, "new independent 15m/1h structural confirmation")
		playbookSwitch := gfs.String("playbook-switch", "", "playbook switch (e.g. SECOND_WAVE)")
		leverage := gfs.Float64("leverage", 0, "requested leverage")
		notional := gfs.Float64("notional", 0, "requested notional USD")
		trancheBase := gfs.Float64("tranche-base", 0, "accounting tranche base USD")
		liq := gfs.Float64("liq", 0, "liquidation price")
		if err := gfs.Parse(cmdArgs); err != nil {
			return err
		}
		if gfs.NArg() < 1 {
			return fmt.Errorf("usage: kairos-desk gate --margin isolated --max-loss 3 <ticket-id>")
		}
		var parsedSetup types.SetupPoint
		if strings.TrimSpace(*setupPointRaw) != "" {
			sp, ok := types.ParseSetupPoint(*setupPointRaw)
			if !ok {
				return fmt.Errorf("unknown --setup-point %q (valid: 1..6 or %v)", *setupPointRaw, types.ValidSetupPoints())
			}
			parsedSetup = sp
		}
		ticketID := gfs.Arg(0)
		res, gateCodes, err := runGate(journal, *cfgPath, ticketID, gateFlags{
			Margin:            *margin,
			MaxLoss:           *maxLoss,
			StopBasis:         *stopBasis,
			SetupPoint:        parsedSetup,
			NewSetup:          *newSetup,
			StructuralConfirm: *structConfirm,
			PlaybookSwitch:    *playbookSwitch,
			Leverage:          *leverage,
			NotionalUSD:       *notional,
			TrancheBaseUSD:    *trancheBase,
			Liq:               *liq,
		})
		if err != nil {
			return err
		}
		if *asJSON {
			return encode(res)
		}
		if t, ok, _ := journal.GetTicket(ticketID); ok {
			printDeskStopAndTranche(t, *stopBasis, *trancheBase, *leverage)
		}
		for _, w := range res.Warnings {
			fmt.Printf("⚠ %s\n", w)
		}
		if res.LiveEligible {
			fmt.Println("PASS live_eligible=true")
		} else {
			fmt.Printf("REJECT %s\n", strings.Join(gateCodes, ","))
		}
		return nil

	case "close":
		return runClose(journal, cmdArgs)

	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func runClose(journal *storage.Journal, cmdArgs []string) error {
	cfs := flag.NewFlagSet("close", flag.ContinueOnError)
	cfs.SetOutput(os.Stderr)
	exitReasonRaw := cfs.String("exit-reason", "", "exit reason: [1/p] PROTECTIVE_STOP | [2/f] FAKEOUT_ABORT | [3/s] STRUCTURAL_TRAIL | [4/t] TARGET_PARTIAL | [5/i] IMPULSE_EARLY")
	fundingUSD := cfs.Float64("funding-usd", 0, "realized accumulated funding cost in USD")
	exitPrice := cfs.Float64("exit-price", 0, "realized exit price")
	promptKey := cfs.Bool("prompt", false, "interactive single-key ExitReason selection prompt")

	// Allow `<ticket-id>` either as the first argument before flags or after flags.
	var ticketID string
	flagArgs := cmdArgs
	if len(cmdArgs) > 0 && !strings.HasPrefix(cmdArgs[0], "-") {
		ticketID = cmdArgs[0]
		flagArgs = cmdArgs[1:]
	}
	if err := cfs.Parse(flagArgs); err != nil {
		return err
	}
	if ticketID == "" && cfs.NArg() > 0 {
		ticketID = cfs.Arg(0)
	}
	if ticketID == "" {
		return fmt.Errorf("usage: kairos-desk close [--exit-reason 1..5|p|f|s|t|i|NAME] [--funding-usd X] <ticket-id>")
	}

	var exitReason types.ExitReason
	rawChoice := strings.TrimSpace(*exitReasonRaw)
	if rawChoice == "" && *promptKey {
		fmt.Fprintln(os.Stderr, "Select ExitReason: [1/p] PROTECTIVE_STOP  [2/f] FAKEOUT_ABORT  [3/s] STRUCTURAL_TRAIL  [4/t] TARGET_PARTIAL  [5/i] IMPULSE_EARLY")
		sc := bufio.NewScanner(closeInputReader)
		if sc.Scan() {
			rawChoice = strings.TrimSpace(sc.Text())
		}
	}
	if rawChoice != "" {
		parsed, ok := types.ParseExitReasonKey(rawChoice)
		if !ok {
			return fmt.Errorf("unknown exit reason %q (valid: 1/p=%s, 2/f=%s, 3/s=%s, 4/t=%s, 5/i=%s)",
				rawChoice,
				types.ExitReasonProtectiveStop,
				types.ExitReasonFakeoutAbort,
				types.ExitReasonStructuralTrail,
				types.ExitReasonTargetPartial,
				types.ExitReasonImpulseEarly,
			)
		}
		exitReason = parsed
	}

	t, ok, err := journal.GetTicket(ticketID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("ticket not found: %s", ticketID)
	}
	nowUnix := deskNowFunc().Unix()
	t.Status = types.TicketStatusClosed
	t.ClosedAt = nowUnix
	if exitReason != "" {
		t.ExitReason = exitReason
	}
	if *fundingUSD != 0 {
		t.RealizedFundingUSD = *fundingUSD
	}
	if *exitPrice > 0 {
		t.ExitPrice = *exitPrice
	}
	if err := journal.SaveTicket(t); err != nil {
		return err
	}

	if dec, hasDec, err := journal.GetDecision(ticketID); err == nil && hasDec {
		dec.ClosedAt = nowUnix
		if exitReason != "" {
			dec.ExitReason = exitReason
			if exitReason == types.ExitReasonProtectiveStop {
				dec.TouchedPresetStop = true
			}
			if exitReason == types.ExitReasonFakeoutAbort {
				dec.FakeoutConfirmed = true
			}
		}
		if *fundingUSD != 0 {
			dec.RealizedFundingUSD = *fundingUSD
		}
		if *exitPrice > 0 {
			dec.ExitPrice = *exitPrice
		}
		_ = journal.SaveDecision(dec)
	}

	if outs, err := journal.ListOutcomes(); err == nil {
		for _, o := range outs {
			if o.TicketID != ticketID {
				continue
			}
			if exitReason != "" {
				o.ExitReason = exitReason
				if exitReason == types.ExitReasonProtectiveStop {
					o.TouchedPresetStop = true
				}
				if exitReason == types.ExitReasonFakeoutAbort {
					o.FakeoutConfirmed = true
				}
			}
			if *fundingUSD != 0 {
				o.RealizedFundingUSD = *fundingUSD
			}
			if *exitPrice > 0 {
				o.ExitPrice = *exitPrice
			}
			_ = journal.SaveOutcome(o)
			break
		}
	}

	if exitReason != "" {
		fmt.Printf("ok %s closed exit_reason=%s funding_usd=%.2f\n", t.ID, exitReason, *fundingUSD)
	} else {
		fmt.Printf("ok %s closed\n", t.ID)
	}
	return nil
}

type gateFlags struct {
	Margin            string
	MaxLoss           float64
	StopBasis         string
	SetupPoint        types.SetupPoint
	NewSetup          string
	StructuralConfirm bool
	PlaybookSwitch    string
	Leverage          float64
	NotionalUSD       float64
	TrancheBaseUSD    float64
	Liq               float64
}

func loadGateConfig(cfgPath string) decision.BehaviorGateConfig {
	gcfg := decision.DefaultBehaviorGateConfig()
	if cfg, err := config.Load(cfgPath); err == nil {
		gcfg = decision.BehaviorGateConfigFrom(cfg.RiskGate)
	}
	return gcfg
}

func buildGateInput(journal *storage.Journal, gcfg decision.BehaviorGateConfig, t types.DecisionTicket, f gateFlags, now time.Time) (decision.GateInput, error) {
	cdStats, err := journal.SameSymbolCooldownStats(t.Symbol, now)
	if err != nil {
		return decision.GateInput{}, err
	}
	setupPoint := types.NormalizeSetupPoint(f.SetupPoint)
	if setupPoint == "" {
		setupPoint = types.NormalizeSetupPoint(t.SetupPoint)
	}
	winHours := gcfg.TrialBudgetWindowHours
	if winHours <= 0 {
		winHours = 24.0
	}
	trialWin := time.Duration(winHours * float64(time.Hour))
	sameSetup, sameSymbol, err := journal.TrialBudgetStats(t.Symbol, setupPoint, t.ID, now, trialWin)
	if err != nil {
		return decision.GateInput{}, err
	}
	churnMin := gcfg.ChurnWindowMinutes
	if churnMin <= 0 {
		churnMin = 60
	}
	relStats, err := journal.RelapseHistoryStats(
		t.Symbol,
		t.ID,
		now,
		time.Duration(churnMin)*time.Minute,
		gcfg.RecentRiskSampleSize,
	)
	if err != nil {
		return decision.GateInput{}, err
	}

	lev := f.Leverage
	if lev <= 0 {
		lev = t.RiskPlan.MaxLeverage
	}
	notional := f.NotionalUSD
	if notional <= 0 {
		notional = t.RiskPlan.SuggestedNotional
	}
	tranche := f.TrancheBaseUSD
	if tranche <= 0 {
		tranche = t.RiskPlan.TrancheBaseUSD
	}
	stopBasis := strings.TrimSpace(f.StopBasis)
	if stopBasis == "" {
		stopBasis = t.StopBasisText()
	}
	return decision.GateInput{
		Now:                         now,
		Symbol:                      t.Symbol,
		Direction:                   t.Direction,
		MarginMode:                  f.Margin,
		StopPrice:                   t.RiskPlan.StopPrice,
		LiquidationPrice:            f.Liq,
		StopBasis:                   stopBasis,
		MaxLossUSD:                  f.MaxLoss,
		Leverage:                    lev,
		NotionalUSD:                 notional,
		TrancheBaseUSD:              tranche,
		ContextDirection:            t.MarketCycle.PrimaryDirection,
		RankSupported:               t.PlaybookID != "" && t.Grade != types.TicketGradeD,
		LastLossSameSymbolAt:        cdStats.LastLossAt,
		NewSetupID:                  f.NewSetup,
		NewStructuralConfirm15m1h:   f.StructuralConfirm,
		PlaybookSwitch:              f.PlaybookSwitch,
		ConsecutiveStops24h:         cdStats.ConsecutiveStops24h,
		CumulativeTrialLoss24hR:     cdStats.CumulativeTrialLoss24hR,
		SetupPoint:                  setupPoint,
		SameSetupTrialsInWindow:     sameSetup,
		SameSymbolTrialsInWindow:    sameSymbol,
		LastStopSameSymbolAt:        relStats.LastStopSameSymbolAt,
		LastStopSameSymbolDirection: relStats.LastStopSameSymbolDirection,
		GlobalConsecutiveStops:      relStats.GlobalConsecutiveStops,
		RecentTicketsInChurnWindow:  relStats.RecentTicketsInChurnWindow,
		LastOutcomeWasLoss:          relStats.LastOutcomeWasLoss,
		RecentPersonalRiskUSD:       relStats.RecentPersonalRiskUSD,
	}, nil
}

// runGate evaluates the §9b + Phase 1 + Phase 3b behavior gate for a ticket at decision time.
// journal 注入 cooldown / 试错预算 / §12 犯病图谱历史;config 缺失时用 doctrine 默认值(gate 常开)。
func runGate(journal *storage.Journal, cfgPath, ticketID string, f gateFlags) (decision.GateResult, []string, error) {
	t, ok, err := journal.GetTicket(ticketID)
	if err != nil {
		return decision.GateResult{}, nil, err
	}
	if !ok {
		return decision.GateResult{}, nil, fmt.Errorf("ticket not found: %s", ticketID)
	}
	gcfg := loadGateConfig(cfgPath)
	in, err := buildGateInput(journal, gcfg, t, f, deskNowFunc())
	if err != nil {
		return decision.GateResult{}, nil, err
	}
	res := decision.EvaluateBehaviorGate(gcfg, in)
	return res, res.Rejections, nil
}

func computeShowWarnings(journal *storage.Journal, cfgPath string, t types.DecisionTicket) []string {
	gcfg := loadGateConfig(cfgPath)
	previewRisk := 0.0
	if dec, ok, _ := journal.GetDecision(t.ID); ok && dec.PlannedMaxLossUSD != nil && *dec.PlannedMaxLossUSD > 0 {
		previewRisk = *dec.PlannedMaxLossUSD
	} else if t.RiskPlan.SuggestedNotional > 0 && t.OneRPct() > 0 {
		previewRisk = t.RiskPlan.SuggestedNotional * (t.OneRPct() / 100.0)
	}
	in, err := buildGateInput(journal, gcfg, t, gateFlags{MaxLoss: previewRisk}, deskNowFunc())
	if err != nil {
		return nil
	}
	warnCfg := gcfg
	warnCfg.Enabled = true
	return decision.EvaluateBehaviorGate(warnCfg, in).Warnings
}

func printDeskStopAndTranche(t types.DecisionTicket, stopBasisOverride string, trancheOverride, levOverride float64) {
	basis := strings.TrimSpace(stopBasisOverride)
	if basis == "" {
		basis = t.StopBasisText()
	}
	if basis == "" {
		basis = "(missing)"
	}
	tranche := trancheOverride
	if tranche <= 0 {
		tranche = t.RiskPlan.TrancheBaseUSD
	}
	if tranche <= 0 {
		tranche = decision.DefaultTrancheBaseUSD
	}
	lev := levOverride
	if lev <= 0 {
		lev = t.RiskPlan.MaxLeverage
	}
	if lev <= 0 {
		lev = decision.MaxAssetLeverage(t.Symbol, decision.DefaultMajorLeverageCap, decision.DefaultAltLeverageCap)
	}
	fmt.Printf("stop: entry=%.4f  stop_price=%.4f  1R=%.4f (%.2f%%)  basis=%s\n",
		t.RiskPlan.EntryPrice, t.RiskPlan.StopPrice, t.OneRPriceDistance(), t.OneRPct(), basis)
	fmt.Printf("tranche: base=$%.2f  max_lev=%.1fx  suggested_notional=$%.2f  max_notional=$%.2f\n",
		tranche, lev, t.RiskPlan.SuggestedNotional, t.RiskPlan.MaxNotional)
}

func openJournal(cfgPath, override string) (*storage.Journal, error) {
	if override != "" {
		dbPath := override
		if strings.HasSuffix(override, ".jsonl") {
			dbPath = filepath.Join(filepath.Dir(override), "kairos.db")
		} else if fi, err := os.Stat(override); err == nil && fi.IsDir() {
			dbPath = filepath.Join(override, "kairos.db")
		}
		return storage.NewJournal(types.StorageConfig{DatabasePath: dbPath})
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		j, jerr := storage.NewJournal(types.StorageConfig{DatabasePath: "data/kairos.db"})
		if jerr != nil {
			return nil, fmt.Errorf("load config: %w (and default journal: %v)", err, jerr)
		}
		return j, nil
	}
	return storage.NewJournal(cfg.Storage)
}

func encode(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
