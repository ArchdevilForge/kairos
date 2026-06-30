package main

// Kairos daemon — realtime anomaly watcher.
//
// Usage: kairosd [--config path]
// Reads config.yaml, connects to exchanges, runs detectors, sends Telegram alerts.
//
// Graceful shutdown on SIGINT/SIGTERM.
// - Load config
// - Create exchanges
// - Discover top symbols
// - Connect WebSocket feeds
// - Start detector pipeline
// - Wait for shutdown signal
// - Drain and exit

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/ArchdevilForge/kairos/internal/config"
	"github.com/ArchdevilForge/kairos/internal/exchange"
	"github.com/ArchdevilForge/kairos/internal/notify"
	"github.com/ArchdevilForge/kairos/internal/types"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config YAML file")
	flag.Parse()

	// Load config.
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config load: %v", err)
	}

	// Structured logger with source info.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		AddSource: true,
	})))

	slog.Info("starting kairosd", "exchange", cfg.Exchange, "timezone", cfg.NotificationTimezone)

	// --- Exchanges ---

	primary, err := exchange.New(cfg.Exchange)
	if err != nil {
		log.Fatalf("exchange %s: %v", cfg.Exchange, err)
	}

	var backups []exchange.Exchange
	for _, name := range cfg.Exchanges.Backups {
		ex, err := exchange.New(name)
		if err != nil {
			slog.Warn("backup exchange init skipped", "exchange", name, "error", err)
			continue
		}
		backups = append(backups, ex)
	}

	// --- Telegram notifier ---
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

	// --- Signal handling ---
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// --- Symbol discovery ---
	slog.Info("discovering top symbols", "count", cfg.DataManager.TopSymbols)
	symbols, err := discoverSymbols(ctx, primary, cfg.DataManager.TopSymbols)
	if err != nil {
		log.Fatalf("symbol discovery: %v", err)
	}
	slog.Info("symbols discovered", "count", len(symbols))

	// --- WebSocket feed ---
	tickerCh := make(chan types.Ticker, 1024)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		slog.Info("connecting WebSocket feed", "exchange", primary.Name(), "symbols", len(symbols))
		if err := primary.SubscribeTickers(ctx, symbols, tickerCh); err != nil && ctx.Err() == nil {
			slog.Error("ticker subscription failed", "exchange", primary.Name(), "error", err)
		}
	}()

	// --- Ticker consumer ---
	wg.Add(1)
	go func() {
		defer wg.Done()
		consumeTickers(ctx, tickerCh)
	}()

	// --- Detector pipeline (placeholder) ---
	// ponytail: detector pipeline stubs — wire real detectors when ready
	slog.Info("detector pipeline starting (Phase A — ticker collection)")

	// --- Periodic refresh of symbol universe ---
	refreshTicker := time.NewTicker(time.Duration(cfg.DataManager.RefreshIntervalHours) * time.Hour)
	defer refreshTicker.Stop()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-refreshTicker.C:
				slog.Info("refreshing symbol universe")
				newSymbols, err := discoverSymbols(ctx, primary, cfg.DataManager.TopSymbols)
				if err != nil {
					slog.Warn("symbol refresh failed", "error", err)
					continue
				}
				slog.Info("symbol universe refreshed", "count", len(newSymbols))
				// ponytail: restart subscription with new symbols when detector
				// pipeline supports hot-swap; for now log only.
			}
		}
	}()

	// --- Notify start ---
	if tg != nil {
		_ = tg.SendText("🟢 kairosd started — monitoring " + primary.Name())
	}

	slog.Info("kairosd running; waiting for shutdown signal")

	// --- Wait for shutdown ---
	<-ctx.Done()
	slog.Info("shutting down...")

	// --- Drain ---
	stop() // prevent double-notify
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Close all exchanges.
	closeEx := func(ex exchange.Exchange) {
		if err := ex.Close(); err != nil {
			slog.Warn("exchange close", "exchange", ex.Name(), "error", err)
		}
	}
	closeEx(primary)
	for _, b := range backups {
		closeEx(b)
	}

	// Wait for goroutines with a deadline.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		slog.Info("shutdown complete")
	case <-shutdownCtx.Done():
		slog.Warn("shutdown timed out, forcing exit")
	}

	if tg != nil {
		_ = tg.SendText("🔴 kairosd stopped")
	}
}

// ---------------------------------------------------------------------------
// Symbol discovery
// ---------------------------------------------------------------------------

// discoverSymbols fetches all tickers from the primary exchange, sorts by
// 24h quote volume descending, and returns the top N symbols.
func discoverSymbols(ctx context.Context, ex exchange.Exchange, topN int) ([]string, error) {
	tickers, err := ex.FetchTickers(ctx)
	if err != nil {
		return nil, err
	}

	type kv struct {
		symbol string
		vol    float64
	}
	var sorted []kv
	for sym, t := range tickers {
		v := 0.0
		if t != nil && t.QuoteVolume != nil {
			v = *t.QuoteVolume
		}
		sorted = append(sorted, kv{symbol: sym, vol: v})
	}

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].vol > sorted[j].vol
	})

	if len(sorted) > topN {
		sorted = sorted[:topN]
	}

	symbols := make([]string, len(sorted))
	for i, kv := range sorted {
		symbols[i] = kv.symbol
	}
	return symbols, nil
}

// ---------------------------------------------------------------------------
// Ticker consumer (placeholder for detector input)
// ---------------------------------------------------------------------------

// tickState tracks per-symbol ticker stats for health logging.
// ponytail: replace with detector state when pipeline exists.
type tickState struct {
	lastPrice float64
	count     int
}

// consumeTickers reads from the ticker channel and logs activity.
// ponytail: replace with detector pipeline routing when detectors exist.
func consumeTickers(ctx context.Context, ch <-chan types.Ticker) {
	state := make(map[string]*tickState)
	logTicker := time.NewTicker(30 * time.Second)
	defer logTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ch:
			s := state[t.Symbol]
			if s == nil {
				s = &tickState{}
				state[t.Symbol] = s
			}
			s.count++
			if t.LastPrice != nil {
				s.lastPrice = *t.LastPrice
			}
		case <-logTicker.C:
			slog.Info("ticker feed alive",
				"symbols", len(state),
				"total_ticks", totalTicks(state),
			)
		}
	}
}

func totalTicks(m map[string]*tickState) int {
	n := 0
	for _, s := range m {
		n += s.count
	}
	return n
}

// ---------------------------------------------------------------------------
// Small helpers
// ---------------------------------------------------------------------------

// parseChatID converts a Telegram chat ID string to int64.
func parseChatID(s string) (int64, error) {
	// ponytail: strconv.ParseInt handles both positive numeric strings and
	// negative IDs (group chats). No need for a regexp.
	var chatID int64
	_, err := fmt.Sscanf(s, "%d", &chatID)
	return chatID, err
}
