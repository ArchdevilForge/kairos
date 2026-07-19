package detector

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ArchdevilForge/kairos/internal/types"
)

type replayTick struct {
	TS     float64 `json:"ts"`
	Symbol string  `json:"symbol"`
	Price  float64 `json:"price"`
}

func loadReplay(t *testing.T, name string) []replayTick {
	t.Helper()
	path := filepath.Join("testdata", name)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var out []replayTick
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var tk replayTick
		if err := json.Unmarshal(line, &tk); err != nil {
			t.Fatalf("json: %v line=%s", err, line)
		}
		out = append(out, tk)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestReplay_BroadRallyFixture(t *testing.T) {
	ticks := loadReplay(t, "broad_rally.jsonl")
	cfg := testMPConfig()
	cfg.Stress.MinMedianReturnPct = 9 // fixture is impulse-scale only
	d := NewMarketPulseDetector(cfg)

	// Universe from fixture symbols.
	seen := map[string]struct{}{}
	var syms []string
	for _, tk := range ticks {
		if _, ok := seen[tk.Symbol]; !ok {
			seen[tk.Symbol] = struct{}{}
			syms = append(syms, tk.Symbol)
		}
	}
	d.UpdateUniverse(syms)

	// First pass: load t=1000 prices with warmup in the past.
	for _, tk := range ticks {
		if tk.TS != 1000 {
			continue
		}
		ts := tk.TS
		px := tk.Price
		d.SetNowFunc(func() float64 { return ts })
		d.OnTicker(context.Background(), types.Ticker{Symbol: tk.Symbol, LastPrice: &px})
	}
	d.mu.Lock()
	for _, ser := range d.symbols {
		ser.warmupSince = 0
	}
	d.universeChangedAt = 0
	d.mu.Unlock()

	// Second pass at t=1060 and evaluate multiple samples for confirmation.
	for step := 0; step < 4; step++ {
		now := 1060.0 + float64(step)
		for _, tk := range ticks {
			if tk.TS != 1060 {
				continue
			}
			px := tk.Price
			d.SetNowFunc(func() float64 { return now })
			d.OnTicker(context.Background(), types.Ticker{Symbol: tk.Symbol, LastPrice: &px})
		}
		// Keep t=1000 as past by also re-asserting past points once.
		if step == 0 {
			for _, tk := range ticks {
				if tk.TS != 1000 {
					continue
				}
				px := tk.Price
				d.SetNowFunc(func() float64 { return 1000 })
				d.OnTicker(context.Background(), types.Ticker{Symbol: tk.Symbol, LastPrice: &px})
			}
			for _, tk := range ticks {
				if tk.TS != 1060 {
					continue
				}
				px := tk.Price
				d.SetNowFunc(func() float64 { return now })
				d.OnTicker(context.Background(), types.Ticker{Symbol: tk.Symbol, LastPrice: &px})
			}
		}
		d.mu.Lock()
		for _, ser := range d.symbols {
			ser.warmupSince = 0
		}
		d.universeChangedAt = 0
		d.mu.Unlock()
		d.SetNowFunc(func() float64 { return now })
		d.EvaluateAt(now)
	}

	if d.State() != types.MarketStateImpulseUp {
		t.Fatalf("fixture broad rally: state=%s snap=%+v", d.State(), d.LastSnapshot())
	}
	evts := drainEvents(d)
	found := false
	for _, e := range evts {
		if e.EventType == "market_impulse" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected market_impulse from fixture, events=%d", len(evts))
	}
}
