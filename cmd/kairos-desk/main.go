// Command kairos-desk is the human decision CLI for OpportunitySessions / tickets.
//
//	kairos-desk sessions
//	kairos-desk tickets [--session id]
//	kairos-desk show <ticket-id>
//	kairos-desk accept|wait|reject|missed <ticket-id> [--reason code] [--note text]
//	kairos-desk close <ticket-id>
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	fs := flag.NewFlagSet("kairos-desk", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cfgPath := fs.String("config", "config.yaml", "config file (storage path)")
	journalPath := fs.String("journal", "", "override path to trading-journal.jsonl (or its directory)")
	sessionFilter := fs.String("session", "", "filter tickets by session id")
	reason := fs.String("reason", "", "reason code (comma-separated)")
	note := fs.String("note", "", "free-text note")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 1 {
		return fmt.Errorf("usage: kairos-desk [-config] <sessions|tickets|candidates|show|accept|wait|reject|missed|close> ...")
	}
	cmd := rest[0]

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
		sid := *sessionFilter
		if sid == "" && len(rest) > 1 {
			sid = rest[1]
		}
		if sid == "" {
			return fmt.Errorf("usage: kairos-desk candidates --session <id> | candidates <session-id>")
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
			fmt.Printf("%d  %s  long=%.2f short=%.2f RS=%.2f RW=%.2f\n",
				i+1, cand.Symbol, cand.LongScore, cand.ShortScore, cand.RelativeStrength, cand.RelativeWeakness)
		}
		return nil

	case "show":
		if len(rest) < 2 {
			return fmt.Errorf("usage: kairos-desk show <ticket-id>")
		}
		t, ok, err := journal.GetTicket(rest[1])
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("ticket not found: %s", rest[1])
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
		if len(rest) < 2 {
			return fmt.Errorf("usage: kairos-desk %s <ticket-id> [--reason code] [--note text]", cmd)
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
		if cmd == "reject" && len(codes) == 0 {
			return fmt.Errorf("reject requires --reason <code> (e.g. too_extended)")
		}
		if err := svc.ApplyHumanDecision(rest[1], d, codes, *note); err != nil {
			return err
		}
		fmt.Printf("ok %s %s\n", rest[1], d)
		return nil

	case "close":
		if len(rest) < 2 {
			return fmt.Errorf("usage: kairos-desk close <ticket-id>")
		}
		t, ok, err := journal.GetTicket(rest[1])
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("ticket not found: %s", rest[1])
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
