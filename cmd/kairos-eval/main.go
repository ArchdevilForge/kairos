// Command kairos-eval summarizes decision-desk EV from the trading journal.
//
//	kairos-eval summary
//	kairos-eval decisions
//	kairos-eval playbook [id]
//	kairos-eval long|short|countertrend
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ArchdevilForge/kairos/internal/config"
	"github.com/ArchdevilForge/kairos/internal/evaluation"
	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "kairos-eval: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("kairos-eval", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cfgPath := fs.String("config", "config.yaml", "config file")
	journalPath := fs.String("journal", "", "override journal path/dir")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	cmd := "summary"
	if len(rest) > 0 {
		cmd = rest[0]
	}

	j, err := openJournal(*cfgPath, *journalPath)
	if err != nil {
		return err
	}
	if cmd == "help" || cmd == "-h" {
		fmt.Println("kairos-eval summary|decisions|playbook|long|short|countertrend|outcomes")
		return nil
	}
	rows, err := evaluation.BuildRows(j)
	if err != nil {
		return err
	}
	rep := evaluation.Summarize(rows)

	switch cmd {
	case "summary":
		if *asJSON {
			return encode(rep)
		}
		fmt.Printf("tickets=%d with_outcome=%d complete=%d\n", rep.TicketsTotal, rep.WithOutcome, rep.Complete)
		fmt.Printf("accepted:   n=%d complete=%d mean_net_r=%.4f win=%.2f\n", rep.Accepted.N, rep.Accepted.NComplete, rep.Accepted.MeanNetR, rep.Accepted.WinRate)
		fmt.Printf("rejected:   n=%d complete=%d mean_net_r=%.4f win=%.2f\n", rep.Rejected.N, rep.Rejected.NComplete, rep.Rejected.MeanNetR, rep.Rejected.WinRate)
		fmt.Printf("all:        n=%d complete=%d mean_net_r=%.4f\n", rep.AllQualified.N, rep.AllQualified.NComplete, rep.AllQualified.MeanNetR)
		fmt.Printf("selection_alpha(net_r)=%.4f  rejection_mean_net_r=%.4f\n", rep.SelectionAlpha, rep.RejectionMeanR)
		return nil

	case "decisions":
		if *asJSON {
			return encode(rows)
		}
		for _, r := range rows {
			netR := 0.0
			if r.HasOut {
				netR = r.Outcome.NetR
			}
			fmt.Printf("%s  %s  dec=%s  grade=%s  class=%s  netR=%.2f  complete=%v\n",
				r.Ticket.ID, r.Ticket.Symbol, r.Decision, r.Ticket.Grade, r.Ticket.TradeClass, netR, r.HasOut && r.Outcome.Complete)
		}
		return nil

	case "playbook":
		id := types.PlaybookLeaderPullbackV1
		if len(rest) > 1 {
			id = rest[1]
		}
		g := rep.ByPlaybook[id]
		if *asJSON {
			return encode(map[string]any{"playbook": id, "stats": g})
		}
		fmt.Printf("playbook %s  n=%d complete=%d mean_net_r=%.4f win=%.2f\n", id, g.N, g.NComplete, g.MeanNetR, g.WinRate)
		return nil

	case "outcomes":
		outs, err := j.ListOutcomes()
		if err != nil {
			return err
		}
		if *asJSON {
			return encode(outs)
		}
		for _, o := range outs {
			r5 := 0.0
			if o.Return5m != nil {
				r5 = *o.Return5m
			}
			fmt.Printf("%s  %s  dec=%s  MFE=%.3f MAE=%.3f maxR=%.2f mechR=%.2f netR=%.2f complete=%v 5m=%.3f stop_first=%v\n",
				o.TicketID, o.Symbol, o.Decision, o.MFE, o.MAE, o.MaxRealizableR, o.MechanicalR, o.NetR, o.Complete, r5, o.StopHitFirst)
		}
		if len(outs) == 0 {
			fmt.Println("(no outcomes)")
		}
		return nil

	case "long", "short", "countertrend":
		filtered := rows[:0]
		for _, r := range rows {
			switch cmd {
			case "long":
				if r.Ticket.Direction == types.CycleDirectionUp {
					filtered = append(filtered, r)
				}
			case "short":
				if r.Ticket.Direction == types.CycleDirectionDown {
					filtered = append(filtered, r)
				}
			case "countertrend":
				if r.Ticket.TradeClass == types.TradeClassCounterTrendLong ||
					r.Ticket.TradeClass == types.TradeClassCounterTrendShort {
					filtered = append(filtered, r)
				}
			}
		}
		sub := evaluation.Summarize(filtered)
		if *asJSON {
			return encode(sub)
		}
		fmt.Printf("%s  n=%d mean_net_r=%.4f selection_alpha=%.4f\n", cmd, sub.TicketsTotal, sub.AllQualified.MeanNetR, sub.SelectionAlpha)
		return nil

	default:
		return fmt.Errorf("unknown command %q (summary|decisions|playbook|long|short|countertrend)", cmd)
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
		return storage.NewJournal(types.StorageConfig{DatabasePath: "data/kairos.db"})
	}
	return storage.NewJournal(cfg.Storage)
}

func encode(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
