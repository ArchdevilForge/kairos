package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/ArchdevilForge/kairos/internal/alert"
	"github.com/ArchdevilForge/kairos/internal/config"
	"github.com/ArchdevilForge/kairos/internal/notify"
	"github.com/ArchdevilForge/kairos/internal/scanner"
	"github.com/ArchdevilForge/kairos/internal/types"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config YAML file")
	exchange := flag.String("exchange", "", "exchange override, defaults to config primary exchange")
	minState := flag.String("min-state", envOr("KAIROS_ALERT_MIN_STATE", "prepare"), "minimum action_state")
	limit := flag.Int("limit", envIntOr("KAIROS_ALERT_LIMIT", 5), "max setups to include")
	dryRun := flag.Bool("dry-run", false, "print the message instead of sending Telegram")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config load: %v", err)
	}

	os.Exit(runOnce(context.Background(), cfg, *exchange, *minState, *limit, *dryRun))
}

// scanMarketFn is overridden in tests.
var scanMarketFn = func(ctx context.Context, cfg *types.Config, exchange string) *types.SignalEnvelope {
	return scanner.NewMarketScanner(cfg).ScanMarket(ctx, exchange)
}

func runOnce(ctx context.Context, cfg *types.Config, exchangeName, minState string, limit int, dryRun bool) int {
	scan := scanMarketFn(ctx, cfg, exchangeName)
	if scan == nil || !scan.Success {
		errs := []string{"scan failed"}
		if scan != nil && len(scan.Errors) > 0 {
			errs = scan.Errors
		}
		fmt.Fprintln(os.Stderr, strings.Join(errs, "; "))
		return 1
	}

	setups := alert.SelectSetups(scan, minState, limit)
	if len(setups) == 0 {
		fmt.Printf("没有达到 %s 以上的候选。\n", minState)
		return 0
	}

	text := alert.FormatAlert(setups)
	if dryRun {
		fmt.Println(text)
		return 0
	}

	chatID, err := parseChatID(cfg.Telegram.ChatID)
	if err != nil || cfg.Telegram.BotToken == "" || chatID == 0 {
		fmt.Fprintln(os.Stderr, "需要设置 TELEGRAM_BOT_TOKEN 和 TELEGRAM_CHAT_ID。")
		return 2
	}
	tg, err := notify.NewTelegramClient(cfg.Telegram.BotToken, chatID)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if err := tg.SendText(text); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Printf("已发送 %d 个候选。\n", len(setups))
	return 0
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envIntOr(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func parseChatID(s string) (int64, error) {
	var chatID int64
	_, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &chatID)
	return chatID, err
}
