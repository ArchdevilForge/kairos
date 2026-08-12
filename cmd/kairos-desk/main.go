// Command kairos-desk is the human decision CLI for OpportunitySessions / tickets.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
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
		fmt.Println(decision.FormatTicket(t))
		if dec, ok, _ := journal.GetDecision(t.ID); ok {
			fmt.Printf("human: %s reasons=%v note=%q at=%d\n", dec.Decision, dec.ReasonCodes, dec.Note, dec.DecidedAt)
		}
		return nil

	case "accept", "wait", "reject", "missed":
		dfs := flag.NewFlagSet(cmd, flag.ContinueOnError)
		dfs.SetOutput(os.Stderr)
		reason := dfs.String("reason", "", "reason code (comma-separated)")
		note := dfs.String("note", "", "free-text note")
		// Behavior gate inputs (doctrine §9b) — accept only.
		margin := dfs.String("margin", "", "margin mode (isolated required for live)")
		maxLoss := dfs.Float64("max-loss", 0, "predefined max loss USD for this trade")
		newSetup := dfs.String("new-setup", "", "new setup id (exempts same-symbol cooldown)")
		liq := dfs.Float64("liq", 0, "liquidation price (optional; stop近强平价则拒)")
		override := dfs.Bool("override", false, "override gate rejection (leaves annotation)")
		if err := dfs.Parse(cmdArgs); err != nil {
			return err
		}
		if dfs.NArg() < 1 {
			return fmt.Errorf("usage: kairos-desk %s --reason code [--note text] <ticket-id>", cmd)
		}
		ticketID := dfs.Arg(0)
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
				Margin: *margin, MaxLoss: *maxLoss, NewSetup: *newSetup, Liq: *liq,
			})
			if err != nil {
				return err
			}
			if !res.LiveEligible {
				ann := storage.GateAnnotation{
					TicketID: ticketID, Codes: gateCodes,
					Override: *override, Note: *note, At: time.Now().Unix(),
				}
				if err := journal.SaveAnnotation(ann); err != nil {
					return err
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
		fmt.Printf("ok %s %s\n", ticketID, d)
		return nil

	case "gate":
		gfs := flag.NewFlagSet("gate", flag.ContinueOnError)
		gfs.SetOutput(os.Stderr)
		margin := gfs.String("margin", "", "margin mode")
		maxLoss := gfs.Float64("max-loss", 0, "predefined max loss USD")
		newSetup := gfs.String("new-setup", "", "new setup id")
		liq := gfs.Float64("liq", 0, "liquidation price")
		if err := gfs.Parse(cmdArgs); err != nil {
			return err
		}
		if gfs.NArg() < 1 {
			return fmt.Errorf("usage: kairos-desk gate --margin isolated --max-loss 3 <ticket-id>")
		}
		res, gateCodes, err := runGate(journal, *cfgPath, gfs.Arg(0), gateFlags{
			Margin: *margin, MaxLoss: *maxLoss, NewSetup: *newSetup, Liq: *liq,
		})
		if err != nil {
			return err
		}
		if *asJSON {
			return encode(res)
		}
		if res.LiveEligible {
			fmt.Println("PASS live_eligible=true")
		} else {
			fmt.Printf("REJECT %s\n", strings.Join(gateCodes, ","))
		}
		return nil

	case "close":
		if len(cmdArgs) < 1 {
			return fmt.Errorf("usage: kairos-desk close <ticket-id>")
		}
		t, ok, err := journal.GetTicket(cmdArgs[0])
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("ticket not found: %s", cmdArgs[0])
		}
		t.Status = types.TicketStatusClosed
		if err := journal.SaveTicket(t); err != nil {
			return err
		}
		fmt.Printf("ok %s closed\n", t.ID)
		return nil

	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

type gateFlags struct {
	Margin   string
	MaxLoss  float64
	NewSetup string
	Liq      float64
}

// runGate evaluates the §9b behavior gate for a ticket at decision time.
// journal 注入 cooldown 历史;config 缺失时用 doctrine 默认值(gate 常开)。
func runGate(journal *storage.Journal, cfgPath, ticketID string, f gateFlags) (decision.GateResult, []string, error) {
	t, ok, err := journal.GetTicket(ticketID)
	if err != nil {
		return decision.GateResult{}, nil, err
	}
	if !ok {
		return decision.GateResult{}, nil, fmt.Errorf("ticket not found: %s", ticketID)
	}
	gcfg := decision.DefaultBehaviorGateConfig()
	if cfg, err := config.Load(cfgPath); err == nil {
		gcfg = decision.BehaviorGateConfigFrom(cfg.RiskGate)
	}
	lastLoss, err := journal.LastLossAt(t.Symbol)
	if err != nil {
		return decision.GateResult{}, nil, err
	}
	in := decision.GateInput{
		Now:                  time.Now(),
		Symbol:               t.Symbol,
		Direction:            t.Direction,
		MarginMode:           f.Margin,
		StopPrice:            t.RiskPlan.StopPrice,
		LiquidationPrice:     f.Liq,
		MaxLossUSD:           f.MaxLoss,
		ContextDirection:     t.MarketCycle.PrimaryDirection,
		RankSupported:        t.PlaybookID != "" && t.Grade != types.TicketGradeD,
		LastLossSameSymbolAt: lastLoss,
		NewSetupID:           f.NewSetup,
	}
	res := decision.EvaluateBehaviorGate(gcfg, in)
	return res, res.Rejections, nil
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
