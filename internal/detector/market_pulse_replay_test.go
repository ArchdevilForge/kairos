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

// runReplayFixture drives one JSONL fixture (ticks at t=1000 and t=1060)
// through the detector and returns the final state plus emitted events.
func runReplayFixture(t *testing.T, name string) (types.MarketState, []types.AnomalyEvent) {
	t.Helper()
	ticks := loadReplay(t, name)
	cfg := testMPConfig()
	cfg.Stress.MinMedianReturnPct = 9 // fixtures are impulse-scale only
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

	return d.State(), drainEvents(d)
}

// Every acceptance fixture runs; each pins the expected end state and
// (optionally) an expected event with direction.
func TestReplay_AllFixtures(t *testing.T) {
	cases := []struct {
		fixture   string
		wantState types.MarketState
		wantEvent string // "" = no market event expected
		wantDir   string
	}{
		{"broad_rally.jsonl", types.MarketStateImpulseUp, "market_impulse", "up"},
		{"broad_selloff.jsonl", types.MarketStateImpulseDown, "market_impulse", "down"},
		{"btc_only_pump.jsonl", types.MarketStateQuiet, "", ""},
		{"quiet.jsonl", types.MarketStateQuiet, "", ""},
		{"smallcap_only_pump.jsonl", types.MarketStateQuiet, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			state, evts := runReplayFixture(t, tc.fixture)
			if state != tc.wantState {
				t.Fatalf("state=%s want %s", state, tc.wantState)
			}
			var got *types.AnomalyEvent
			for i, e := range evts {
				if e.EventType == tc.wantEvent && tc.wantEvent != "" {
					got = &evts[i]
				}
				if tc.wantEvent == "" && isMarketEventType(e.EventType) {
					t.Fatalf("fixture must stay quiet, got event %s", e.EventType)
				}
			}
			if tc.wantEvent != "" {
				if got == nil {
					t.Fatalf("expected %s event, got %d events", tc.wantEvent, len(evts))
				}
				if dir, _ := got.Data["direction"].(string); dir != tc.wantDir {
					t.Fatalf("direction=%q want %q", dir, tc.wantDir)
				}
			}
		})
	}
}

func isMarketEventType(et string) bool {
	switch et {
	case "market_impulse", "market_trend", "market_stress", "market_decay":
		return true
	}
	return false
}
