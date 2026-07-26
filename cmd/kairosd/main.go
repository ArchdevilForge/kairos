package main

import (
	"context"
	"errors"
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

// pipelineRunner is the slice of engine.Pipeline that the daemon lifecycle
// needs; run is tested against a fake implementation.
type pipelineRunner interface {
	Start(ctx context.Context) error
	Stop()
	Close()
}

// shutdownGrace bounds how long run waits for Pipeline.Start to return after
// a stop request before forcing exit.
const shutdownGrace = 15 * time.Second

// errPipelineExited marks a pipeline that returned without a shutdown
// request — the daemon must exit non-zero so the supervisor restarts it
// instead of leaving a falsely-healthy process behind.
var errPipelineExited = errors.New("pipeline exited while daemon was expected to keep running")

// run owns the daemon lifecycle: it starts the pipeline, waits for either an
// OS shutdown signal (ctx cancelled) or an early pipeline exit, and shuts
// down in order: stop → wait for goroutines → close exchanges.
func run(ctx context.Context, p pipelineRunner) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- p.Start(ctx)
	}()

	var startErr error
	var early bool

	select {
	case <-ctx.Done():
		slog.Info("shutdown signal received")
		p.Stop()
		select {
		case startErr = <-errCh:
		case <-time.After(shutdownGrace):
			p.Close()
			return fmt.Errorf("shutdown timed out after %s", shutdownGrace)
		}
	case startErr = <-errCh:
		// Pipeline returned on its own: never a healthy state for a daemon.
		early = true
		p.Stop()
	}

	p.Close()

	if startErr != nil && !errors.Is(startErr, context.Canceled) {
		return startErr
	}
	if early {
		return errPipelineExited
	}
	return nil
}

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

	slog.Info("starting kairosd", "primary", cfg.Exchanges.Primary, "realtime", cfg.DataManager.Exchanges)

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

	var ding *notify.DingTalkClient
	if cfg.DingTalk.Enabled && cfg.DingTalk.WebhookURL != "" {
		var err error
		ding, err = notify.NewDingTalkClient(cfg.DingTalk.WebhookURL, cfg.DingTalk.Secret)
		if err != nil {
			slog.Warn("dingtalk disabled", "error", err)
			ding = nil
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pipeline := engine.NewPipeline(cfg, tg)
	if ding != nil {
		pipeline.SetDingTalk(ding)
	}
	startMsg := "🟢 kairosd started — monitoring pipeline"
	if tg != nil {
		_ = tg.SendText(ctx, startMsg)
	}
	if ding != nil {
		_ = ding.SendText(ctx, startMsg)
	}

	runErr := run(ctx, pipeline)

	stopMsg := "🔴 kairosd stopped"
	if runErr != nil {
		stopMsg = fmt.Sprintf("🔴 kairosd stopped: %v", runErr)
	}
	// The signal context is already cancelled at this point; use a short
	// independent context so the stop notification can still go out.
	msgCtx, msgCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer msgCancel()
	if tg != nil {
		_ = tg.SendText(msgCtx, stopMsg)
	}
	if ding != nil {
		_ = ding.SendText(msgCtx, stopMsg)
	}

	if runErr != nil {
		slog.Error("kairosd exit", "error", runErr)
		os.Exit(1)
	}
	slog.Info("shutdown complete")
}

func parseChatID(s string) (int64, error) {
	var chatID int64
	_, err := fmt.Sscanf(s, "%d", &chatID)
	return chatID, err
}
