package opportunity

import (
	"github.com/ArchdevilForge/kairos/internal/ranker"
	"github.com/ArchdevilForge/kairos/internal/types"
)

// RankInputsFromPulse builds *soft* ranker inputs from pulse payload only.
//
// Honesty rule: pulse gives returns/relative — it does NOT measure liquidity,
// spread, pullback depth, or room. Those flags stay unmeasured (fail closed
// for hard rank / playbook). SoftRank boards may still list leaders for watch.
func RankInputsFromPulse(evt types.AnomalyEvent) []ranker.Input {
	if evt.Data == nil {
		return nil
	}
	median := asFloat(evt.Data["median_return_60s_pct"])
	btc := asFloat(evt.Data["btc_return_pct"])
	btcSet := evt.Data["btc_return_pct"] != nil

	seen := map[string]ranker.Input{}

	addMove := func(m types.SymbolMove) {
		change := m.ReturnPct
		if change == 0 && m.RelativePct != 0 {
			change = median + m.RelativePct
		}
		// Only return-relative fields are real here.
		seen[m.Symbol] = ranker.Input{
			Symbol:             m.Symbol,
			ChangePct:          change,
			MarketMedianChange: median,
			BTCChange:          btc,
			BTCChangeSet:       btcSet,
			// Explicitly unmeasured — do NOT invent 5e6 volume / spread OK / depth=2.
			DataOK:            true, // symbol identity + return present
			LiquidityMeasured: false,
			SpreadMeasured:    false,
			PullbackMeasured:  false,
			ReboundMeasured:   false,
			RoomMeasured:      false,
		}
	}

	for _, key := range []string{"leaders_detail", "laggards_detail"} {
		raw, ok := evt.Data[key]
		if !ok {
			continue
		}
		switch arr := raw.(type) {
		case []any:
			for _, item := range arr {
				if mv, ok := moveFromAny(item); ok {
					addMove(mv)
				}
			}
		case []map[string]any:
			for _, m := range arr {
				if mv, ok := moveFromAny(m); ok {
					addMove(mv)
				}
			}
		}
	}

	if len(seen) == 0 {
		addNames := func(key string, sign float64) {
			for i, sym := range asStringSlice(evt.Data[key]) {
				// ordinal edge from list order only (still return-based soft signal)
				edge := sign * (3.0 - float64(i)*0.5)
				addMove(types.SymbolMove{Symbol: sym, ReturnPct: median + edge, RelativePct: edge})
			}
		}
		addNames("leaders", +1)
		addNames("laggards", -1)
	}

	out := make([]ranker.Input, 0, len(seen))
	for _, in := range seen {
		out = append(out, in)
	}
	return out
}

func moveFromAny(v any) (types.SymbolMove, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return types.SymbolMove{}, false
	}
	sym, _ := m["symbol"].(string)
	if sym == "" {
		return types.SymbolMove{}, false
	}
	return types.SymbolMove{
		Symbol:      sym,
		ReturnPct:   asFloat(m["return_pct"]),
		RelativePct: asFloat(m["relative_pct"]),
		ZScore:      asFloat(m["z_score"]),
	}, true
}

func asFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	default:
		return 0
	}
}

func asStringSlice(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
