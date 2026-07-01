package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/ArchdevilForge/kairos/internal/backtest"
	"github.com/ArchdevilForge/kairos/internal/exchange"
	"github.com/ArchdevilForge/kairos/internal/indicators"
)

func main() {
	symbols := flag.String("symbol", "", "trading pair(s), comma-separated e.g. BTC/USDT,ETH/USDT")
	start := flag.String("start", "", "start date YYYY-MM-DD")
	end := flag.String("end", "", "end date YYYY-MM-DD")
	timeframe := flag.String("timeframe", "4h", "OHLCV timeframe")
	exchangeName := flag.String("exchange", "okx", "exchange id")
	minBars := flag.Int("min-bars", 10, "minimum bars for box formation")
	fee := flag.Float64("fee", 0.04, "taker fee % per side")
	slippage := flag.Float64("slippage", 0.02, "slippage % per side")
	positionPct := flag.Float64("position-pct", 100.0, "capital % per trade")
	parallel := flag.Int("parallel", 2, "max parallel fetches for multi-symbol")
	flag.Parse()

	if *symbols == "" || *start == "" || *end == "" {
		fmt.Fprintln(os.Stderr, "usage: kairos-backtest --symbol BTC/USDT --start 2024-01-01 --end 2024-06-01")
		os.Exit(2)
	}

	symbolList := splitSymbols(*symbols)
	ex, err := exchange.New(*exchangeName)
	if err != nil {
		log.Fatalf("exchange: %v", err)
	}
	defer func() { _ = ex.Close() }()

	boxCfg := indicators.DefaultBoxDetectorConfig()
	boxCfg.MinBars = *minBars

	runner := backtest.NewWithConfig(ex, boxCfg)
	ctx := context.Background()

	btcData, _ := runner.FetchBtc1d(ctx, *start, *end)
	if btcData == nil && (len(symbolList) != 1 || symbolList[0] != "BTC/USDT") {
		log.Printf("warning: failed to fetch BTC 1d data, falling back to symbol data for cycle")
	}

	opt := backtest.RunOption{
		Timeframe:   *timeframe,
		FeePct:      *fee,
		SlippagePct: *slippage,
		PositionPct: *positionPct,
		BtcData:     btcData,
	}

	if len(symbolList) == 1 {
		result, err := runner.Run(ctx, symbolList[0], *start, *end, opt)
		if err != nil {
			log.Fatalf("backtest: %v", err)
		}
		printJSON(result)
		return
	}

	results := make(map[string]any, len(symbolList))
	var mu sync.Mutex
	workers := *parallel
	if workers < 1 {
		workers = 1
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup

	for _, sym := range symbolList {
		wg.Add(1)
		go func(symbol string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			r := backtest.NewWithConfig(ex, boxCfg)
			res, err := r.Run(ctx, symbol, *start, *end, opt)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				results[symbol] = map[string]any{"symbol": symbol, "error": err.Error()}
				return
			}
			results[symbol] = res
		}(sym)
	}
	wg.Wait()

	totalTrades := 0
	for _, raw := range results {
		if res, ok := raw.(*backtest.Result); ok {
			totalTrades += res.Summary.TotalTrades
		}
	}

	printJSON(map[string]any{
		"symbols": results,
		"combined_summary": map[string]any{
			"total_symbols": len(symbolList),
			"total_trades":  totalTrades,
		},
	})
}

func splitSymbols(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, item := range parts {
		if s := strings.TrimSpace(item); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func printJSON(v any) {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		log.Fatalf("json: %v", err)
	}
	fmt.Println(string(out))
}
