// Command kairos-cycle debugs CycleMap V2 on synthetic or JSON series.
//
//	kairos-cycle demo
//	kairos-cycle file prices.json
//
// prices.json:
//
//	{"timeframe":"1d","closes":[...],"highs":[...],"lows":[...],"volumes":[...]}
//	or {"series":[ {...}, ... ]}
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ArchdevilForge/kairos/internal/cycle"
	"github.com/ArchdevilForge/kairos/internal/types"
)

type seriesJSON struct {
	Timeframe string    `json:"timeframe"`
	Role      string    `json:"role"`
	Closes    []float64 `json:"closes"`
	Highs     []float64 `json:"highs"`
	Lows      []float64 `json:"lows"`
	Volumes   []float64 `json:"volumes"`
}

type fileJSON struct {
	Symbol string       `json:"symbol"`
	Series []seriesJSON `json:"series"`
	// single-series shorthand
	Timeframe string    `json:"timeframe"`
	Closes    []float64 `json:"closes"`
	Highs     []float64 `json:"highs"`
	Lows      []float64 `json:"lows"`
	Volumes   []float64 `json:"volumes"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "kairos-cycle: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("kairos-cycle", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	asJSON := fs.Bool("json", false, "JSON output")
	symbol := fs.String("symbol", "BTC/USDT:USDT", "symbol label")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 1 {
		return fmt.Errorf("usage: kairos-cycle [demo|file <path>]")
	}

	var series []cycle.Series
	switch rest[0] {
	case "demo":
		series = demoSeries()
	case "file":
		if len(rest) < 2 {
			return fmt.Errorf("usage: kairos-cycle file <path>")
		}
		raw, err := os.ReadFile(rest[1])
		if err != nil {
			return err
		}
		var fj fileJSON
		if err := json.Unmarshal(raw, &fj); err != nil {
			return err
		}
		if fj.Symbol != "" {
			*symbol = fj.Symbol
		}
		if len(fj.Series) > 0 {
			for _, s := range fj.Series {
				series = append(series, toSeries(s))
			}
		} else if len(fj.Closes) > 0 {
			series = append(series, toSeries(seriesJSON{
				Timeframe: fj.Timeframe, Closes: fj.Closes, Highs: fj.Highs, Lows: fj.Lows, Volumes: fj.Volumes,
			}))
		}
	default:
		// treat as bare symbol demo label
		*symbol = rest[0]
		series = demoSeries()
	}

	if len(series) == 0 {
		return fmt.Errorf("no series")
	}
	m := cycle.MapFromOHLCV(*symbol, 0, types.MarketPhaseSummer, series)
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(m)
	}
	fmt.Printf("symbol=%s primary=%s align=%s class=%s legacy=%s\n",
		*symbol, m.PrimaryDirection, m.Alignment, m.TradeClass, m.LegacyClimate)
	for _, line := range m.Summary {
		fmt.Printf("  %s\n", line)
	}
	for tf, n := range m.Nodes {
		fmt.Printf("  [%s] conf=%.2f evidence=%d\n", tf, n.Confidence, len(n.Evidence))
		for _, e := range n.Evidence {
			fmt.Printf("      - %s: %s\n", e.Code, e.Description)
		}
	}
	return nil
}

func toSeries(s seriesJSON) cycle.Series {
	tf := s.Timeframe
	if tf == "" {
		tf = "1d"
	}
	role := types.TimeframeRole(s.Role)
	if role == "" {
		switch strings.ToLower(tf) {
		case "1d", "4h":
			role = types.TimeframeRoleContext
		case "1h", "15m":
			role = types.TimeframeRoleSetup
		case "5m":
			role = types.TimeframeRoleTrigger
		default:
			role = types.TimeframeRoleSetup
		}
	}
	highs, lows := s.Highs, s.Lows
	if len(highs) == 0 {
		highs = s.Closes
	}
	if len(lows) == 0 {
		lows = s.Closes
	}
	vols := s.Volumes
	if len(vols) == 0 {
		vols = make([]float64, len(s.Closes))
		for i := range vols {
			vols[i] = 1000
		}
	}
	return cycle.Series{Timeframe: tf, Role: role, Closes: s.Closes, Highs: highs, Lows: lows, Volumes: vols}
}

func demoSeries() []cycle.Series {
	// strong uptrend shared across TFs
	n := 90
	closes := make([]float64, n)
	highs := make([]float64, n)
	lows := make([]float64, n)
	vols := make([]float64, n)
	px := 100.0
	for i := 0; i < n; i++ {
		px += 0.9
		if i%7 == 0 {
			px -= 0.2
		}
		closes[i] = px
		highs[i] = px * 1.002
		lows[i] = px * 0.998
		vols[i] = 1000 + float64(i)
	}
	mk := func(tf string, role types.TimeframeRole) cycle.Series {
		return cycle.Series{Timeframe: tf, Role: role, Closes: closes, Highs: highs, Lows: lows, Volumes: vols}
	}
	return []cycle.Series{
		mk("1d", types.TimeframeRoleContext),
		mk("4h", types.TimeframeRoleContext),
		mk("15m", types.TimeframeRoleSetup),
		mk("5m", types.TimeframeRoleTrigger),
	}
}
