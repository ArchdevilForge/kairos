package engine

import (
	"context"
	"time"

	"github.com/ArchdevilForge/kairos/internal/exchange"
	"github.com/ArchdevilForge/kairos/internal/notice"
	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
)

func (p *Pipeline) initNoticeService() {
	if p == nil || p.cfg == nil || !p.cfg.Notice.Enabled {
		return
	}
	nj, err := storage.NewNoticeJournal(p.cfg.Storage)
	if err != nil {
		p.log.Warn("notice journal disabled", "error", err)
		return
	}
	nj.SetMetadata(storage.ResearchMeta{
		GitSHA:          storage.DetectGitSHA(),
		ConfigHash:      storage.ConfigHashOf(p.cfg),
		StrategyVersion: "phase3c-notice-v1",
		Mode:            "live",
	})
	var sender notice.TextSender
	if p.tg != nil && p.tg.IsConfigured() {
		sender = p.tg
	}
	p.notice = notice.NewService(p.cfg.Notice, nj, sender)
	p.log.Info("notice layer ready",
		"path", nj.Path(),
		"symbol_cooldown_min", p.cfg.Notice.SymbolCooldownMinutes,
		"max_per_day", p.cfg.Notice.MaxNoticesPerDay,
		"suppress_inside_box", p.cfg.Notice.ShouldSuppressInsideBox(),
	)
}

// deliverNotice feeds qualifying detector anomalies into the independent Phase 3c
// Notice layer without modifying AlertEvent or DecisionTicket behavior.
func (p *Pipeline) deliverNotice(ctx context.Context, evt types.AnomalyEvent) {
	if p == nil || p.notice == nil {
		return
	}
	if !notice.IsSupportedTriggerEvent(evt.EventType) {
		return
	}
	if p.blacklist != nil && p.blacklist.IsBlocked(evt.Symbol) {
		return
	}

	var c4h, btc4h []types.Candle
	if ex := p.resolveNoticeExchange(evt.Exchange); ex != nil {
		fetchCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		c4h = fetchClosedCandles(fetchCtx, ex, evt.Symbol, "4h", 32)
		if evt.Symbol == "BTC/USDT:USDT" {
			btc4h = c4h
		} else {
			btc4h = fetchClosedCandles(fetchCtx, ex, "BTC/USDT:USDT", "4h", 25)
		}
		cancel()
	}

	_, err := p.notice.ProcessAnomaly(ctx, notice.ProcessRequest{
		Event:          evt,
		Candles4h:      c4h,
		BTCCandles4h:   btc4h,
		UniverseBars5m: p.seasonBarsSnapshot(),
	})
	if err != nil {
		p.log.Warn("notice delivery failed", "symbol", evt.Symbol, "event", evt.EventType, "error", err)
	}
}

func (p *Pipeline) resolveNoticeExchange(origin string) exchange.Exchange {
	if origin != "" {
		if ex := p.exchanges[origin]; ex != nil {
			return ex
		}
	}
	if primary := p.primaryExchange(); primary != "" {
		if ex := p.exchanges[primary]; ex != nil {
			return ex
		}
	}
	for _, ex := range p.exchanges {
		if ex != nil {
			return ex
		}
	}
	return nil
}

func fetchClosedCandles(ctx context.Context, ex exchange.Exchange, symbol, tf string, limit int) []types.Candle {
	candles, err := ex.FetchOHLCV(ctx, symbol, tf, limit, 0)
	if err != nil || len(candles) <= 1 {
		return nil
	}
	// Drop potentially still-forming last bar (strictly causal, same rule as opportunity/enrich.go).
	return candles[:len(candles)-1]
}
