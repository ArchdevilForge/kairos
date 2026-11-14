package notice

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/ArchdevilForge/kairos/internal/cycle"
	"github.com/ArchdevilForge/kairos/internal/types"
	"github.com/ArchdevilForge/kairos/internal/utils"
)

// IsSupportedTriggerEvent reports whether an AnomalyEvent type drives Phase 3c Notices (§3 B2).
func IsSupportedTriggerEvent(eventType string) bool {
	switch eventType {
	case "volume_spike", "price_velocity", "open_interest_change", "resonance":
		return true
	default:
		return false
	}
}

// ExtractTriggerFacts converts a detector AnomalyEvent into objective NoticeTriggerFacts (§3 B2/B3).
// Deliberately strips any composite score (such as resonance signal_score) so scores never leak.
func ExtractTriggerFacts(evt types.AnomalyEvent) (types.NoticeTriggerFacts, bool) {
	if !IsSupportedTriggerEvent(evt.EventType) {
		return types.NoticeTriggerFacts{}, false
	}
	data := evt.Data
	facts := types.NoticeTriggerFacts{
		EventType: evt.EventType,
		EventID:   evt.EventID,
		Exchange:  evt.Exchange,
		Price:     bestPriceFromMap(data),
	}

	switch evt.EventType {
	case "volume_spike":
		facts.VolumeRatio = numFromMap(data, "ratio")
		facts.VolumeWindowMin = int(math.Round(numFromMap(data, "window_minutes")))
		if facts.VolumeWindowMin <= 0 {
			facts.VolumeWindowMin = 5
		}
		facts.FactSummary = fmt.Sprintf("%d 分钟量 = 窗口基准 %.2f×", facts.VolumeWindowMin, facts.VolumeRatio)

	case "price_velocity":
		facts.PriceChangePct = numFromMap(data, "change_pct")
		facts.VelocityWindowSec = int(math.Round(numFromMap(data, "window_seconds")))
		if facts.VelocityWindowSec <= 0 {
			facts.VelocityWindowSec = 60
		}
		facts.FactSummary = fmt.Sprintf("%d 秒价格变动 %+.2f%%", facts.VelocityWindowSec, facts.PriceChangePct)

	case "open_interest_change":
		facts.OIChangePct = numFromMap(data, "change_pct")
		facts.OpenInterest = numFromMap(data, "open_interest")
		facts.FactSummary = fmt.Sprintf("持仓量 OI 变动 %+.2f%%", facts.OIChangePct)

	case "resonance":
		facts.DimensionCount = int(math.Round(numFromMap(data, "dimension_count")))
		facts.Dimensions = stringSliceFromMap(data, "dimensions")
		if facts.DimensionCount <= 0 {
			facts.DimensionCount = len(facts.Dimensions)
		}
		var subFacts []string
		if sub, ok := data["volume_spike_data"].(map[string]any); ok {
			r := numFromMap(sub, "ratio")
			wm := int(math.Round(numFromMap(sub, "window_minutes")))
			if wm <= 0 {
				wm = 5
			}
			if r > 0 {
				facts.VolumeRatio = r
				facts.VolumeWindowMin = wm
				subFacts = append(subFacts, fmt.Sprintf("%d 分钟量 %.2f×", wm, r))
			}
			if facts.Price <= 0 {
				facts.Price = bestPriceFromMap(sub)
			}
		}
		if sub, ok := data["price_velocity_data"].(map[string]any); ok {
			chg := numFromMap(sub, "change_pct")
			ws := int(math.Round(numFromMap(sub, "window_seconds")))
			if ws <= 0 {
				ws = 60
			}
			if chg != 0 {
				facts.PriceChangePct = chg
				facts.VelocityWindowSec = ws
				subFacts = append(subFacts, fmt.Sprintf("%d 秒涨跌 %+.2f%%", ws, chg))
			}
			if facts.Price <= 0 {
				facts.Price = bestPriceFromMap(sub)
			}
		}
		if sub, ok := data["open_interest_change_data"].(map[string]any); ok {
			oi := numFromMap(sub, "change_pct")
			if oi != 0 {
				facts.OIChangePct = oi
				subFacts = append(subFacts, fmt.Sprintf("OI %+.2f%%", oi))
			}
			if facts.Price <= 0 {
				facts.Price = bestPriceFromMap(sub)
			}
		}
		dimText := strings.Join(facts.Dimensions, ", ")
		if len(subFacts) > 0 {
			facts.FactSummary = fmt.Sprintf("%d 维异动共振（%s）", facts.DimensionCount, strings.Join(subFacts, "；"))
		} else if dimText != "" {
			facts.FactSummary = fmt.Sprintf("%d 维异动共振（%s）", facts.DimensionCount, dimText)
		} else {
			facts.FactSummary = fmt.Sprintf("%d 维异动共振", facts.DimensionCount)
		}
	}

	return facts, true
}

// EnvInput supplies strictly causal market context for NoticeEnvTags (§3 B3).
type EnvInput struct {
	EventUnixSec    int64
	CurrentPrice    float64
	Candles4h       []types.Candle
	BTCCandles4h    []types.Candle
	SeasonRegime    types.SeasonRegime
	UniverseBars5m  map[string][]cycle.SeasonBar5m
	UniverseFunding map[string]map[int64]float64
}

// EvaluateEnvTags computes genuine environment tags (§3 B3).
// SeasonRegime is populated ONLY when genuinely computed by Phase 2c causal timeline
// (or explicitly passed from an already-computed causal classifier); otherwise it stays empty.
func EvaluateEnvTags(in EnvInput) types.NoticeEnvTags {
	season := types.NormalizeSeasonRegime(in.SeasonRegime)
	if season == "" && len(in.UniverseBars5m) >= cycle.DefaultSeasonMinValidSymbols && in.EventUnixSec > 0 {
		tl := cycle.BuildCausalSeasonTimelineWithConfig(
			in.UniverseBars5m,
			in.UniverseFunding,
			cycle.DefaultSeasonTimelineConfig(),
		)
		if obs, ok := cycle.LatestSeasonObservationAtOrBefore(tl, in.EventUnixSec); ok {
			season = types.NormalizeSeasonRegime(obs.SeasonRegime)
		}
	}

	boxState, boxWidthPct, boxPos := classifyBoxState(in.CurrentPrice, in.Candles4h)
	btcState := classifyBTCState(in.BTCCandles4h)

	return types.NoticeEnvTags{
		SeasonRegime:      season,
		WinterDefenseHint: types.WinterDefenseHintFor(season),
		BoxState:          boxState,
		BoxWidthPct:       boxWidthPct,
		BoxPos:            boxPos,
		BTCState:          btcState,
	}
}

// classifyBoxState determines whether current price sits inside a 4h consolidation box,
// near its boundary, or outside/breaking out (§3 B3).
func classifyBoxState(currentPrice float64, c4h []types.Candle) (types.NoticeBoxState, float64, float64) {
	if len(c4h) < 12 {
		return types.NoticeBoxUnknown, 0, 0
	}
	win := c4h
	if len(win) > 19 {
		win = win[len(win)-19:]
	}
	ref := win
	if len(ref) > 12 {
		ref = ref[:len(ref)-1]
	}
	price := currentPrice
	if price <= 0 {
		price = win[len(win)-1].Close
	}
	hi, lo := candleHighLow(ref)
	if lo <= 0 || hi <= lo || price <= 0 {
		return types.NoticeBoxUnknown, 0, 0
	}

	widthPct := math.Round((hi-lo)/lo*10000) / 100
	pos := (price - lo) / (hi - lo)
	posRounded := math.Round(pos*1000) / 1000
	er := efficiencyRatio(ref)

	// Clear breakout outside prior 4h range or strong directional trend -> outside_box.
	if price > hi*1.002 || price < lo*0.998 || er > 0.42 {
		return types.NoticeBoxOutside, widthPct, posRounded
	}
	// Consolidating box: interior [0.22, 0.78] is inside_box (suppressed by default per §3 B3).
	if widthPct >= 2.0 && er <= 0.35 && pos >= 0.22 && pos <= 0.78 {
		return types.NoticeBoxInside, widthPct, posRounded
	}
	return types.NoticeBoxBoundary, widthPct, posRounded
}

func classifyBTCState(btc4h []types.Candle) string {
	if len(btc4h) < 7 {
		return ""
	}
	nowClose := btc4h[len(btc4h)-1].Close
	prev24hClose := btc4h[len(btc4h)-7].Close
	if nowClose <= 0 || prev24hClose <= 0 {
		return ""
	}
	chg24h := (nowClose - prev24hClose) / prev24hClose * 100.0

	maLen := 20
	if len(btc4h) < maLen {
		maLen = len(btc4h)
	}
	sum := 0.0
	for _, c := range btc4h[len(btc4h)-maLen:] {
		sum += c.Close
	}
	ma := sum / float64(maLen)
	rel := "4h 均线上方"
	if nowClose < ma {
		rel = "4h 均线下方"
	}
	return fmt.Sprintf("BTC 24h %+.2f%%（%s）", chg24h, rel)
}

func bestPriceFromMap(m map[string]any) float64 {
	for _, k := range []string{"price_to", "price", "last_price"} {
		if v := numFromMap(m, k); v > 0 {
			return v
		}
	}
	return 0
}

func numFromMap(m map[string]any, key string) float64 {
	return utils.FloatFromMap(m, key)
}

func stringSliceFromMap(m map[string]any, key string) []string {
	if m == nil {
		return nil
	}
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	switch s := v.(type) {
	case []string:
		cp := make([]string, len(s))
		copy(cp, s)
		sort.Strings(cp)
		return cp
	case []any:
		out := make([]string, 0, len(s))
		for _, item := range s {
			if str, ok := item.(string); ok && str != "" {
				out = append(out, str)
			}
		}
		sort.Strings(out)
		return out
	default:
		return nil
	}
}
