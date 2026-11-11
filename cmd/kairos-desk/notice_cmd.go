package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ArchdevilForge/kairos/internal/config"
	"github.com/ArchdevilForge/kairos/internal/notice"
	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
)

func openNoticeJournal(cfgPath, noticeOverride, journalOverride string) (*storage.NoticeJournal, error) {
	if noticeOverride != "" {
		if strings.HasSuffix(noticeOverride, ".jsonl") {
			return storage.NewNoticeJournalAt(noticeOverride)
		}
		return storage.NewNoticeJournal(types.StorageConfig{DatabasePath: filepath.Join(noticeOverride, "kairos.db")})
	}
	if journalOverride != "" {
		dir := journalOverride
		if strings.HasSuffix(journalOverride, ".jsonl") {
			dir = filepath.Dir(journalOverride)
		}
		return storage.NewNoticeJournal(types.StorageConfig{DatabasePath: filepath.Join(dir, "kairos.db")})
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return storage.NewNoticeJournal(types.StorageConfig{DatabasePath: "data/kairos.db"})
	}
	return storage.NewNoticeJournal(cfg.Storage)
}

func runNotices(nj *storage.NoticeJournal, cmdArgs []string, asJSON bool) error {
	nfs := flag.NewFlagSet("notices", flag.ContinueOnError)
	nfs.SetOutput(os.Stderr)
	showAll := nfs.Bool("all", false, "include suppressed notices")

	var noticeID string
	flagArgs := cmdArgs
	if len(cmdArgs) > 0 && !strings.HasPrefix(cmdArgs[0], "-") {
		noticeID = cmdArgs[0]
		flagArgs = cmdArgs[1:]
	}
	if err := nfs.Parse(flagArgs); err != nil {
		return err
	}
	if noticeID == "" && nfs.NArg() > 0 {
		noticeID = nfs.Arg(0)
	}

	if noticeID != "" {
		ntc, err := nj.GetNotice(noticeID)
		if err != nil {
			return err
		}
		if ntc == nil {
			return fmt.Errorf("notice not found: %s", noticeID)
		}
		fb, _ := nj.GetFeedback(noticeID)
		if asJSON {
			return encode(map[string]any{
				"notice":   ntc,
				"feedback": fb,
			})
		}
		fmt.Print(notice.FormatNoticePlain(*ntc, fb))
		return nil
	}

	list, err := nj.ListNotices()
	if err != nil {
		return err
	}
	fbs, err := nj.ListFeedbacks()
	if err != nil {
		return err
	}

	var filtered []types.Notice
	suppressedCount := 0
	for _, n := range list {
		if n.DeliveryStatus == types.NoticeStatusSuppressed {
			suppressedCount++
			if !*showAll {
				continue
			}
		}
		filtered = append(filtered, n)
	}

	if asJSON {
		return encode(map[string]any{
			"notices":          filtered,
			"feedbacks":        fbs,
			"suppressed_total": suppressedCount,
		})
	}

	if len(filtered) == 0 {
		fmt.Printf("(no notices; suppressed=%d)\n", suppressedCount)
		return nil
	}
	for _, n := range filtered {
		fbLabel := "待反馈"
		if fb, ok := fbs[n.NoticeID]; ok {
			fbLabel = types.NoticeFeedbackLabelZH(fb.Action)
		}
		fmt.Printf("%s  %s  %-16s  status=%-10s  feedback=%s  %s\n",
			n.NoticeID,
			n.CreatedAt.UTC().Format("2006-01-02T15:04Z"),
			n.Symbol,
			n.DeliveryStatus,
			fbLabel,
			n.TriggerFacts.FactSummary,
		)
	}
	fmt.Printf("summary: shown=%d  suppressed=%d  total=%d\n", len(filtered), suppressedCount, len(list))
	return nil
}

func runNoticeFeedback(nj *storage.NoticeJournal, cmdArgs []string, asJSON bool) error {
	ffs := flag.NewFlagSet("feedback", flag.ContinueOnError)
	ffs.SetOutput(os.Stderr)
	actionRaw := ffs.String("action", "", "feedback action: 看了|做了|忽略 (or seen|acted|ignored)")
	ticketID := ffs.String("ticket", "", "optional linked DecisionTicket id when action=做了/acted")
	note := ffs.String("note", "", "optional note")

	// Support both `feedback <notice-id> --action 看了` and `feedback --action 看了 <notice-id>`.
	var noticeID string
	var positional []string
	var flagOnly []string
	for i := 0; i < len(cmdArgs); i++ {
		arg := cmdArgs[i]
		if strings.HasPrefix(arg, "-") {
			flagOnly = append(flagOnly, arg)
			if !strings.Contains(arg, "=") && i+1 < len(cmdArgs) && !strings.HasPrefix(cmdArgs[i+1], "-") {
				flagOnly = append(flagOnly, cmdArgs[i+1])
				i++
			}
		} else {
			positional = append(positional, arg)
		}
	}
	if err := ffs.Parse(flagOnly); err != nil {
		return err
	}
	if len(positional) > 0 {
		noticeID = positional[0]
		if strings.TrimSpace(*actionRaw) == "" && len(positional) > 1 {
			*actionRaw = positional[1]
		}
	}
	if noticeID == "" {
		return fmt.Errorf("usage: kairos-desk feedback --action <看了|做了|忽略> [--ticket <id>] [--note <text>] <notice-id>")
	}
	act, ok := types.ParseNoticeFeedbackAction(*actionRaw)
	if !ok {
		return fmt.Errorf("invalid --action %q (valid: 看了|做了|忽略 or seen|acted|ignored)", *actionRaw)
	}

	ntc, err := nj.GetNotice(noticeID)
	if err != nil {
		return err
	}
	if ntc == nil {
		return fmt.Errorf("notice not found: %s", noticeID)
	}

	fb := types.NoticeFeedback{
		SchemaVersion: types.NoticeFeedbackSchemaVersion,
		NoticeID:      noticeID,
		Action:        act,
		TicketID:      strings.TrimSpace(*ticketID),
		Note:          strings.TrimSpace(*note),
		RecordedAt:    deskNowFunc().UTC(),
	}
	if err := nj.SaveFeedback(fb); err != nil {
		return err
	}
	if asJSON {
		return encode(fb)
	}
	fmt.Printf("ok %s feedback=%s (%s)\n", noticeID, act, types.NoticeFeedbackLabelZH(act))
	return nil
}
