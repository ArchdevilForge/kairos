// Command kairos-market-replay replays JSONL ticker fixtures through MarketPulseDetector.
//
// Input line format:
//
//	{"ts": 1000, "symbol": "BTC/USDT:USDT", "price": 60000}
//
// Usage:
//
//	go run ./cmd/kairos-market-replay --input internal/detector/testdata/broad_rally.jsonl
//	go run ./cmd/kairos-market-replay --input ticks.jsonl --config config/config.yaml --eval-every 5
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"

	"github.com/ArchdevilForge/kairos/internal/config"
	"github.com/ArchdevilForge/kairos/internal/detector"
	"github.com/ArchdevilForge/kairos/internal/types"
)

type tick struct {
	TS     float64 `json:"ts"`
	Symbol string  `json:"symbol"`
	Price  float64 `json:"price"`
}

func main() {
	input := flag.String("input", "", "path to JSONL ticks (required)")
	cfgPath := flag.String("config", "", "optional config YAML (uses marketPulse section)")
	evalEvery := flag.Float64("eval-every", 5, "evaluate snapshot every N seconds of replay time")
	warmup := flag.Int("warmup", 1, "override warmupSeconds for fixtures (default 1)")
	minValid := flag.Int("min-valid", 15, "override minValidSymbols")
	confirmRepeats := flag.Int("confirm-repeats", 4, "re-evaluate final market state N times to satisfy confirmation windows")
	flag.Parse()

	if *input == "" {
		log.Fatal("--input is required")
	}

	cfg := types.MarketPulseConfig{Enabled: true, ShadowMode: true}
	if *cfgPath != "" {
		full, err := config.Load(*cfgPath)
		if err != nil {
			log.Fatalf("config: %v", err)
		}
		cfg = full.MarketPulse
		cfg.Enabled = true
	}
	if *warmup > 0 {
		cfg.WarmupSeconds = *warmup
	}
	if *minValid > 0 {
		cfg.MinValidSymbols = *minValid
	}
	// Stress often steals fixture impulse; keep high unless config sets it.
	if cfg.Stress.MinMedianReturnPct <= 0 {
		cfg.Stress.MinMedianReturnPct = 9
	}

	ticks, err := loadTicks(*input)
	if err != nil {
		log.Fatal(err)
	}
	if len(ticks) == 0 {
		log.Fatal("no ticks")
	}

	syms := uniqueSymbols(ticks)
	d := detector.NewMarketPulseDetector(cfg)
	// Clock must be set BEFORE UpdateUniverse so warmupSince uses fixture time.
	d.SetNowFunc(func() float64 { return ticks[0].TS })
	d.UpdateUniverse(syms)

	var (
		lastEval = -1e18
		states   []string
		prev     = d.State()
	)
	states = append(states, string(prev))

	ctx := context.Background()
	for _, tk := range ticks {
		ts := tk.TS
		px := tk.Price
		d.SetNowFunc(func() float64 { return ts })
		d.OnTicker(ctx, types.Ticker{Symbol: tk.Symbol, LastPrice: &px})

		if ts-lastEval >= *evalEvery {
			d.EvaluateAt(ts)
			lastEval = ts
			if st := d.State(); st != prev {
				states = append(states, string(st))
				prev = st
				fmt.Printf("state %s @ ts=%.0f med60=%.4f upB=%.3f downB=%.3f\n",
					st, ts, d.LastSnapshot().MedianReturn60s,
					d.LastSnapshot().UpBreadth60s, d.LastSnapshot().DownBreadth60s)
			}
		}
	}
	// Final eval at last tick, then repeat to fill confirmation windows
	// (fixtures often only have 2 timestamps).
	lastTS := ticks[len(ticks)-1].TS
	lastBySym := map[string]float64{}
	for _, tk := range ticks {
		if tk.TS == lastTS {
			lastBySym[tk.Symbol] = tk.Price
		}
	}
	for i := 0; i < *confirmRepeats; i++ {
		ts := lastTS + float64(i)
		for sym, px := range lastBySym {
			p := px
			d.SetNowFunc(func() float64 { return ts })
			// Keep 60s history: also re-assert past prices from first timestamp.
			d.OnTicker(ctx, types.Ticker{Symbol: sym, LastPrice: &p})
		}
		// Re-seed past points once so returns stay valid as clock advances.
		if i == 0 {
			firstTS := ticks[0].TS
			for _, tk := range ticks {
				if tk.TS != firstTS {
					continue
				}
				p := tk.Price
				d.SetNowFunc(func() float64 { return firstTS })
				d.OnTicker(ctx, types.Ticker{Symbol: tk.Symbol, LastPrice: &p})
			}
			for sym, px := range lastBySym {
				p := px
				d.SetNowFunc(func() float64 { return ts })
				d.OnTicker(ctx, types.Ticker{Symbol: sym, LastPrice: &p})
			}
		}
		d.SetNowFunc(func() float64 { return ts })
		d.EvaluateAt(ts)
		if st := d.State(); st != prev {
			states = append(states, string(st))
			prev = st
			fmt.Printf("state %s @ ts=%.0f med60=%.4f upB=%.3f downB=%.3f\n",
				st, ts, d.LastSnapshot().MedianReturn60s,
				d.LastSnapshot().UpBreadth60s, d.LastSnapshot().DownBreadth60s)
		}
	}

	// Drain events.
	var events []types.AnomalyEvent
	for {
		select {
		case e := <-d.Events():
			events = append(events, e)
		default:
			goto done
		}
	}
done:
	fmt.Printf("symbols=%d ticks=%d final_state=%s events=%d\n",
		len(syms), len(ticks), d.State(), len(events))
	for _, e := range events {
		fmt.Printf("event type=%s dir=%v from=%v to=%v\n",
			e.EventType, e.Data["direction"], e.Data["state_from"], e.Data["state_to"])
	}
	fmt.Printf("state_path=%v\n", states)
	snap := d.LastSnapshot()
	fmt.Printf("last_snap valid=%d fresh=%.3f med60=%.4f med300=%.4f upB=%.3f downB=%.3f data_ok=%v gate=%s\n",
		snap.ValidSymbols, snap.FreshRatio, snap.MedianReturn60s, snap.MedianReturn300s,
		snap.UpBreadth60s, snap.DownBreadth60s, snap.DataOK, snap.GateReason)
}

func loadTicks(path string) ([]tick, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []tick
	sc := bufio.NewScanner(f)
	// 1MB lines should be enough.
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var t tick
		if err := json.Unmarshal(line, &t); err != nil {
			return nil, fmt.Errorf("json line: %w", err)
		}
		if t.Symbol == "" || t.Price <= 0 {
			continue
		}
		out = append(out, t)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TS == out[j].TS {
			return out[i].Symbol < out[j].Symbol
		}
		return out[i].TS < out[j].TS
	})
	return out, nil
}

func uniqueSymbols(ticks []tick) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, t := range ticks {
		if _, ok := seen[t.Symbol]; ok {
			continue
		}
		seen[t.Symbol] = struct{}{}
		out = append(out, t.Symbol)
	}
	return out
}
