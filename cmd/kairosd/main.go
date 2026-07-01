package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ArchdevilForge/kairos/internal/config"
	"github.com/ArchdevilForge/kairos/internal/engine"
	"github.com/ArchdevilForge/kairos/internal/notify"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config YAML file")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config load: %v", err)
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: true,
	})))

	slog.Info("starting kairosd", "exchange", cfg.Exchange, "timezone", cfg.NotificationTimezone)

	var tg *notify.TelegramClient
	if cfg.Telegram.Enabled && cfg.Telegram.BotToken != "" && cfg.Telegram.ChatID != "" {
		chatID, err := parseChatID(cfg.Telegram.ChatID)
		if err != nil {
			slog.Warn("telegram disabled: invalid chat ID", "error", err)
		} else {
			tg, err = notify.NewTelegramClient(cfg.Telegram.BotToken, chatID)
			if err != nil {
				slog.Warn("telegram disabled", "error", err)
			}
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pipeline := engine.NewPipeline(cfg, tg)
	if tg != nil {
		_ = tg.SendText("🟢 kairosd started — monitoring pipeline")
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- pipeline.Start(ctx)
	}()

	slog.Info("kairosd running; waiting for shutdown signal")
	<-ctx.Done()
	slog.Info("shutting down...")
	stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pipeline.Stop()
	pipeline.Close()

	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			slog.Error("pipeline exit", "error", err)
		}
	case <-shutdownCtx.Done():
		slog.Warn("shutdown timed out, forcing exit")
	}

	if tg != nil {
		_ = tg.SendText("🔴 kairosd stopped")
	}
	slog.Info("shutdown complete")
}

func parseChatID(s string) (int64, error) {
	var chatID int64
	_, err := fmt.Sscanf(s, "%d", &chatID)
	return chatID, err
}
