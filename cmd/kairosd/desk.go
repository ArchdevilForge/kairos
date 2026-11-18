package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/ArchdevilForge/kairos/internal/cache"
	"github.com/ArchdevilForge/kairos/internal/deskapi"
	"github.com/ArchdevilForge/kairos/internal/engine"
	"github.com/ArchdevilForge/kairos/internal/notify"
	"github.com/ArchdevilForge/kairos/internal/types"
)

// deskRouter admits requests only while a pipeline is active. Each instance can
// cancel and drain its requests before the next one starts using the same stores.
type deskRouter struct {
	mu      sync.Mutex
	current *deskInstance
}

type deskInstance struct {
	ctx     context.Context
	handler http.Handler
	active  sync.WaitGroup
}

func (r *deskRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	instance := r.current
	if instance != nil {
		instance.active.Add(1)
	}
	r.mu.Unlock()
	if instance == nil {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		if req.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Retry-After", "1")
		http.Error(w, "market data source is restarting", http.StatusServiceUnavailable)
		return
	}
	defer instance.active.Done()
	ctx, cancel := context.WithCancel(req.Context())
	stop := context.AfterFunc(instance.ctx, cancel)
	defer stop()
	defer cancel()
	instance.handler.ServeHTTP(w, req.WithContext(ctx))
}

func runDesk(ctx context.Context, base *types.Config, tg *notify.TelegramClient, ding *notify.DingTalkClient, addr, token string, notifyStarted func()) error {
	store, err := cache.OpenFromStorageConfig(base.Storage)
	if err != nil {
		return fmt.Errorf("desk cache init: %w", err)
	}
	defer func() { _ = store.Close() }()
	if token == "" {
		token, err = randomHexToken(16)
		if err != nil {
			return fmt.Errorf("generate desk token: %w", err)
		}
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("desk listen %s: %w", addr, err)
	}
	defer func() { _ = ln.Close() }()
	router := &deskRouter{}
	httpSrv := &http.Server{Handler: router, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = httpSrv.Serve(ln) }()
	defer func() { _ = httpSrv.Close() }()

	changed := make(chan struct{}, 1)
	signalChange := func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	}
	bound := normalizeBoundAddr(ln.Addr().String())
	announceReady := true
	for ctx.Err() == nil {
		source, err := store.GetAccountMeta(ctx, deskapi.MarketSourceSetting)
		if err != nil {
			return fmt.Errorf("read market source: %w", err)
		}
		cfg := *base
		switch source {
		case "binance", "okx":
			cfg.Exchange = source
			cfg.Exchanges.Primary = source
			cfg.DataManager.Exchanges = []string{source}
		default:
			source = ""
		}
		slog.Info("starting kairosd desk pipeline", "primary", cfg.Exchanges.Primary, "realtime", cfg.DataManager.Exchanges)
		pipeline := engine.NewPipeline(&cfg, tg)
		pipeline.SetLegacyTicketExchange(base.Exchanges.Primary)
		if ding != nil {
			pipeline.SetDingTalk(ding)
		}
		if err := pipeline.EnsureDeskStorage(); err != nil {
			pipeline.Close()
			return fmt.Errorf("desk storage init: %w", err)
		}
		pipeline.SetCacheStore(store)
		api := deskapi.NewServer(pipeline, store, token)
		api.SetMarketSourceSettings(source, signalChange)
		pipeline.SetDesktopObserver(api.Observer())

		instanceCtx, cancel := context.WithCancel(ctx)
		instance := &deskInstance{ctx: instanceCtx, handler: api.Handler()}
		router.mu.Lock()
		router.current = instance
		router.mu.Unlock()

		done := make(chan error, 1)
		go func() { done <- run(instanceCtx, pipeline) }()
		if announceReady {
			ready, _ := json.Marshal(map[string]string{
				"url": "http://" + bound, "ws_url": "ws://" + bound + "/api/v1/ws", "token": token,
			})
			fmt.Printf("KAIROSD_DESK_READY %s\n", ready)
			_ = os.Stdout.Sync()
			announceReady = false
			go notifyStarted()
		}

		var runErr error
		finished := false
	waitForSwitch:
		for {
			select {
			case <-ctx.Done():
				break waitForSwitch
			case runErr = <-done:
				finished = true
				break waitForSwitch
			case <-changed:
				next, err := store.GetAccountMeta(ctx, deskapi.MarketSourceSetting)
				if err != nil || next != source {
					break waitForSwitch
				}
			}
		}

		router.mu.Lock()
		router.current = nil
		router.mu.Unlock()
		cancel()
		instance.active.Wait()
		api.Close()
		if !finished {
			runErr = <-done
		}
		if runErr != nil {
			return runErr
		}
	}
	return nil
}
