package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ArchdevilForge/kairos/internal/cache"
	"github.com/ArchdevilForge/kairos/internal/config"
	"github.com/ArchdevilForge/kairos/internal/deskapi"
	"github.com/ArchdevilForge/kairos/internal/engine"
	"github.com/ArchdevilForge/kairos/internal/notify"
	"github.com/ArchdevilForge/kairos/internal/types"
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
	deskListen := flag.String("desk-listen", "", "local loopback HTTP/WS listen address for Kairos Desk (e.g. 127.0.0.1:0)")
	deskToken := flag.String("desk-token", "", "bearer token for Kairos Desk API (32-char hex generated if omitted)")
	flag.Parse()

	cfg, err := loadDaemonConfig(*cfgPath, strings.TrimSpace(*deskListen) != "")
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

	var (
		cacheStore *cache.Store
		deskSrv    *deskapi.Server
		httpSrv    *http.Server
	)
	if listenAddr := strings.TrimSpace(*deskListen); listenAddr != "" {
		if err := pipeline.EnsureDeskStorage(); err != nil {
			log.Fatalf("desk storage init: %v", err)
		}
		cacheStore, err = cache.OpenFromStorageConfig(cfg.Storage)
		if err != nil {
			log.Fatalf("desk cache init: %v", err)
		}
		defer func() { _ = cacheStore.Close() }()
		pipeline.SetCacheStore(cacheStore)

		token := strings.TrimSpace(*deskToken)
		if token == "" {
			token, err = randomHexToken(16)
			if err != nil {
				log.Fatalf("generate desk token: %v", err)
			}
		}

		deskSrv = deskapi.NewServer(pipeline, cacheStore, token)
		pipeline.SetDesktopObserver(deskSrv.Observer())

		ln, err := net.Listen("tcp", listenAddr)
		if err != nil {
			log.Fatalf("desk listen %s: %v", listenAddr, err)
		}
		httpSrv = &http.Server{
			Handler:           deskSrv.Handler(),
			ReadHeaderTimeout: 10 * time.Second,
		}
		go func() {
			if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("deskapi server error", "error", err)
			}
		}()

		bound := normalizeBoundAddr(ln.Addr().String())
		readyJSON, err := json.Marshal(map[string]string{
			"url":    "http://" + bound,
			"ws_url": "ws://" + bound + "/api/v1/ws",
			"token":  token,
		})
		if err != nil {
			log.Fatalf("marshal KAIROSD_DESK_READY: %v", err)
		}
		fmt.Printf("KAIROSD_DESK_READY %s\n", readyJSON)
		_ = os.Stdout.Sync()
	}

	startMsg := "🟢 kairosd started — monitoring pipeline"
	if tg != nil {
		_ = tg.SendText(ctx, startMsg)
	}
	if ding != nil {
		_ = ding.SendText(ctx, startMsg)
	}

	runErr := run(ctx, pipeline)

	if httpSrv != nil {
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = httpSrv.Shutdown(shutCtx)
		shutCancel()
	}
	if deskSrv != nil {
		deskSrv.Close()
	}

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

func applyDesktopDaemonProfile(cfg *types.Config) {
	if cfg == nil {
		return
	}
	cfg.MarketPulse.Enabled = true
	cfg.MarketPulse.ShadowMode = false
	cfg.MarketPulse.GateIndividualAlertsWhenQuiet = true
	if cfg.MarketPulse.Stress.MinMedianReturnPct < 0.85 {
		cfg.MarketPulse.Stress.MinMedianReturnPct = 0.85
	}
	if cfg.MarketPulse.Leaders.Limit > 3 || cfg.MarketPulse.Leaders.Limit <= 0 {
		cfg.MarketPulse.Leaders.Limit = 3
	}
	cfg.Opportunity.Enabled = true
	cfg.Notice.Enabled = true
}

func loadDaemonConfig(cfgPath string, deskFallback bool) (*types.Config, error) {
	if deskFallback {
		if _, err := os.Stat(cfgPath); errors.Is(err, os.ErrNotExist) {
			cfg, err := config.LoadString("")
			if err != nil {
				return nil, err
			}
			applyDesktopDaemonProfile(cfg)
			return cfg, nil
		}
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, err
	}
	if deskFallback {
		applyDesktopDaemonProfile(cfg)
	}
	return cfg, nil
}

func randomHexToken(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func normalizeBoundAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}
