package notice

import (
	"fmt"
	"math"
	"strings"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// SceneInput supplies strictly causal market data at notice evaluation time (§3 B1).
// All candle slices must contain closed bars prior to or at evaluation time (oldest -> newest).
type SceneInput struct {
	Symbol       string
	CurrentPrice float64
	TriggerFacts types.NoticeTriggerFacts
	Candles4h    []types.Candle
	BTCCandles4h []types.Candle
}

// DetectScenes evaluates Phase 3a operational definitions strictly as scene flags (§3 B1).
// It never fabricates a scene when candles are missing or conditions do not match,
// and never outputs scores, win-rates, or directional trade instructions.
func DetectScenes(in SceneInput) []types.NoticeSceneDetail {
	var scenes []types.NoticeSceneDetail

	price := in.CurrentPrice
	if price <= 0 && len(in.Candles4h) > 0 {
		price = in.Candles4h[len(in.Candles4h)-1].Close
	}

	if s, ok := detectPoint1Breakout(price, in.TriggerFacts, in.Candles4h); ok {
		scenes = append(scenes, s)
	}
	if s, ok := detectPoint2MinorDiv(price, in.TriggerFacts, in.Candles4h); ok {
		scenes = append(scenes, s)
	}
	if s, ok := detectPoint4SecondWave(price, in.TriggerFacts, in.Candles4h); ok {
		scenes = append(scenes, s)
	}
	if s, ok := detectResonanceOrLeader(in.TriggerFacts, in.Candles4h, in.BTCCandles4h); ok {
		scenes = append(scenes, s)
	}

	return scenes
}

// detectPoint1Breakout operationalizes Phase 3a Point 1:
// converging 4h box/platform (width 1.5%..18%, >=2 boundary touches, converging range)
// with current anomaly breaking outside the platform boundary on expanded activity.
func detectPoint1Breakout(price float64, facts types.NoticeTriggerFacts, c4h []types.Candle) (types.NoticeSceneDetail, bool) {
	if price <= 0 || len(c4h) < 14 {
		return types.NoticeSceneDetail{}, false
	}
	// Use up to 18 closed bars as the prior platform window; if the last closed bar is
	// itself the breakout candle, exclude it from the platform bounds.
	window := c4h
	if len(window) > 19 {
		window = window[len(window)-19:]
	}
	platform := window[:len(window)-1]
	if len(platform) < 12 {
		return types.NoticeSceneDetail{}, false
	}

	boxHigh, boxLow := candleHighLow(platform)
	if boxLow <= 0 || boxHigh <= boxLow {
		return types.NoticeSceneDetail{}, false
	}
	widthPct := (boxHigh - boxLow) / boxLow * 100.0
	if widthPct < 1.5 || widthPct > 18.0 {
		return types.NoticeSceneDetail{}, false
	}

	// Convergence check: second half range <= 0.92 * first half range.
	mid := len(platform) / 2
	h1, l1 := candleHighLow(platform[:mid])
	h2, l2 := candleHighLow(platform[mid:])
	if (h1-l1) <= 0 || (h2-l2) > (h1-l1)*0.92 {
		return types.NoticeSceneDetail{}, false
	}

	// Require >= 2 boundary probes on the side being broken.
	tol := (boxHigh - boxLow) * 0.15
	upperTouches, lowerTouches := 0, 0
	for _, c := range platform {
		if boxHigh-c.High <= tol {
			upperTouches++
		}
		if c.Low-boxLow <= tol {
			lowerTouches++
		}
	}

	// Volume / momentum confirmation from trigger facts or breakout bar volume.
	medVol := medianVolume(platform)
	lastBarVolOK := medVol > 0 && window[len(window)-1].Volume >= medVol*1.5
	factVolOK := facts.VolumeRatio >= 2.0 || math.Abs(facts.PriceChangePct) >= 0.8 || math.Abs(facts.OIChangePct) >= 3.0 || facts.EventType == "resonance"
	if !lastBarVolOK && !factVolOK {
		return types.NoticeSceneDetail{}, false
	}

	if price > boxHigh*1.001 && upperTouches >= 2 {
		return types.NoticeSceneDetail{
			Flag:    types.ScenePoint1Breakout,
			Summary: fmt.Sprintf("疑似点位 1 场景：放量突破收敛箱体上沿（箱体宽度 %.1f%%，上沿 %.4g）", widthPct, boxHigh),
		}, true
	}
	if price < boxLow*0.999 && lowerTouches >= 2 {
		return types.NoticeSceneDetail{
			Flag:    types.ScenePoint1Breakout,
			Summary: fmt.Sprintf("疑似点位 1 场景：放量跌破收敛箱体下沿（箱体宽度 %.1f%%，下沿 %.4g）", widthPct, boxLow),
		}, true
	}
	return types.NoticeSceneDetail{}, false
}

// detectPoint2MinorDiv operationalizes Phase 3a Point 2:
// established 4h advance + shallow minor-divergence pullback (0.4%..3.8% from swing high,
// contracting pullback volume, structural low intact) + restart anomaly.
func detectPoint2MinorDiv(price float64, facts types.NoticeTriggerFacts, c4h []types.Candle) (types.NoticeSceneDetail, bool) {
	if price <= 0 || len(c4h) < 16 {
		return types.NoticeSceneDetail{}, false
	}
	recent := c4h[len(c4h)-16:]
	startClose := recent[0].Close
	if startClose <= 0 {
		return types.NoticeSceneDetail{}, false
	}

	// Find swing high in recent[:-2] (leaving room for 1-3 bars of shallow pullback).
	peakIdx := -1
	peakHigh := 0.0
	for i := 4; i < len(recent)-1; i++ {
		if recent[i].High > peakHigh {
			peakHigh = recent[i].High
			peakIdx = i
		}
	}
	if peakIdx < 4 || peakHigh <= 0 {
		return types.NoticeSceneDetail{}, false
	}
	advancePct := (peakHigh - startClose) / startClose * 100.0
	if advancePct < 5.0 || efficiencyRatio(recent[:peakIdx+1]) < 0.35 {
		return types.NoticeSceneDetail{}, false
	}

	pullbackBars := recent[peakIdx+1:]
	if len(pullbackBars) == 0 || len(pullbackBars) > 5 {
		return types.NoticeSceneDetail{}, false
	}
	_, pbLow := candleHighLow(pullbackBars)
	retraceFromHighPct := (peakHigh - pbLow) / peakHigh * 100.0
	if retraceFromHighPct < 0.4 || retraceFromHighPct > 3.8 {
		return types.NoticeSceneDetail{}, false
	}

	// Pullback volume should contract relative to impulse bars.
	impStart := peakIdx - 3
	if impStart < 0 {
		impStart = 0
	}
	impMeanVol := meanVolume(recent[impStart : peakIdx+1])
	pbMeanVol := meanVolume(pullbackBars)
	if impMeanVol > 0 && pbMeanVol > impMeanVol*0.95 {
		return types.NoticeSceneDetail{}, false
	}

	// Current price recovering near/above pullback close with positive/active trigger.
	if facts.PriceChangePct < -0.5 {
		return types.NoticeSceneDetail{}, false
	}
	if price < pbLow*1.002 {
		return types.NoticeSceneDetail{}, false
	}

	return types.NoticeSceneDetail{
		Flag:    types.ScenePoint2MinorDiv,
		Summary: fmt.Sprintf("疑似点位 2 场景：主升途中缩量小分歧回踩（前高回撤 %.2f%% 后异动）", retraceFromHighPct),
	}, true
}

// detectPoint4SecondWave operationalizes Phase 3a Point 4:
// fluent Wave-1 (>=10% rise, ER >= 0.35) + >=5 bars major divergence consolidation
// (retracement 20%..70% of Wave-1, holding above Wave-1 origin) + right-side breakout.
func detectPoint4SecondWave(price float64, facts types.NoticeTriggerFacts, c4h []types.Candle) (types.NoticeSceneDetail, bool) {
	if price <= 0 || len(c4h) < 22 {
		return types.NoticeSceneDetail{}, false
	}
	win := c4h
	if len(win) > 36 {
		win = win[len(win)-36:]
	}

	// Locate Wave-1 origin low in first half and Wave-1 peak before the last 5 bars.
	half := len(win) / 2
	originIdx := 0
	originLow := win[0].Low
	for i := 1; i < half; i++ {
		if win[i].Low < originLow {
			originLow = win[i].Low
			originIdx = i
		}
	}
	if originLow <= 0 {
		return types.NoticeSceneDetail{}, false
	}

	peakIdx := -1
	peakHigh := 0.0
	for i := originIdx + 3; i <= len(win)-6; i++ {
		if win[i].High > peakHigh {
			peakHigh = win[i].High
			peakIdx = i
		}
	}
	if peakIdx == -1 || peakIdx-originIdx < 3 {
		return types.NoticeSceneDetail{}, false
	}

	wave1Amp := peakHigh - originLow
	wave1Pct := wave1Amp / originLow * 100.0
	if wave1Pct < 10.0 || efficiencyRatio(win[originIdx:peakIdx+1]) < 0.35 {
		return types.NoticeSceneDetail{}, false
	}

	consol := win[peakIdx+1:]
	if len(consol) < 5 {
		return types.NoticeSceneDetail{}, false
	}
	// Exclude the last bar if it is the breakout bar itself when computing consolidation high.
	consolRef := consol
	if len(consolRef) > 5 {
		consolRef = consolRef[:len(consolRef)-1]
	}
	consolHigh, consolLow := candleHighLow(consolRef)
	if consolLow <= originLow {
		return types.NoticeSceneDetail{}, false
	}
	retraceRatio := (peakHigh - consolLow) / wave1Amp
	if retraceRatio < 0.20 || retraceRatio > 0.70 {
		return types.NoticeSceneDetail{}, false
	}

	if facts.PriceChangePct < -0.5 {
		return types.NoticeSceneDetail{}, false
	}
	// Right-side restart: breaking above consolidation resistance or upper 85% of consolidation box.
	triggerLevel := consolLow + 0.85*(consolHigh-consolLow)
	if price < triggerLevel {
		return types.NoticeSceneDetail{}, false
	}

	return types.NoticeSceneDetail{
		Flag:    types.ScenePoint4Wave2,
		Summary: fmt.Sprintf("疑似点位 4 场景：首波 +%.1f%% 后大分歧整理 %d 根 4h K 线再启动", wave1Pct, len(consol)),
	}, true
}

// detectResonanceOrLeader flags multi-dimensional anomaly resonance or clear 4h relative strength vs BTC.
func detectResonanceOrLeader(facts types.NoticeTriggerFacts, c4h, btc4h []types.Candle) (types.NoticeSceneDetail, bool) {
	var parts []string
	if facts.EventType == "resonance" && facts.DimensionCount >= 2 {
		dims := strings.Join(facts.Dimensions, ", ")
		if dims != "" {
			parts = append(parts, fmt.Sprintf("%d 维异动同向共振（%s）", facts.DimensionCount, dims))
		} else {
			parts = append(parts, fmt.Sprintf("%d 维异动同向共振", facts.DimensionCount))
		}
	}

	if len(c4h) >= 7 && len(btc4h) >= 7 {
		symBase := c4h[len(c4h)-7].Close
		symNow := c4h[len(c4h)-1].Close
		btcBase := btc4h[len(btc4h)-7].Close
		btcNow := btc4h[len(btc4h)-1].Close
		if symBase > 0 && btcBase > 0 {
			symRet := (symNow - symBase) / symBase * 100.0
			btcRet := (btcNow - btcBase) / btcBase * 100.0
			rs := symRet - btcRet
			if rs >= 3.5 && btcRet >= -2.5 {
				parts = append(parts, fmt.Sprintf("24h 相对 BTC 强势 +%.2f%%", rs))
			}
		}
	}

	if len(parts) == 0 {
		return types.NoticeSceneDetail{}, false
	}
	return types.NoticeSceneDetail{
		Flag:    types.SceneResonanceLead,
		Summary: "共振/龙头上下文：" + strings.Join(parts, "；"),
	}, true
}

func candleHighLow(cs []types.Candle) (float64, float64) {
	if len(cs) == 0 {
		return 0, 0
	}
	hi, lo := cs[0].High, cs[0].Low
	for _, c := range cs[1:] {
		if c.High > hi {
			hi = c.High
		}
		if c.Low < lo {
			lo = c.Low
		}
	}
	return hi, lo
}

func efficiencyRatio(cs []types.Candle) float64 {
	if len(cs) < 2 {
		return 0
	}
	net := math.Abs(cs[len(cs)-1].Close - cs[0].Close)
	path := 0.0
	for i := 1; i < len(cs); i++ {
		path += math.Abs(cs[i].Close - cs[i-1].Close)
	}
	if path <= 0 {
		return 0
	}
	return net / path
}

func meanVolume(cs []types.Candle) float64 {
	if len(cs) == 0 {
		return 0
	}
	sum := 0.0
	for _, c := range cs {
		v := c.QuoteVol
		if v <= 0 {
			v = c.Volume
		}
		sum += v
	}
	return sum / float64(len(cs))
}

func medianVolume(cs []types.Candle) float64 {
	if len(cs) == 0 {
		return 0
	}
	vals := make([]float64, len(cs))
	for i, c := range cs {
		v := c.QuoteVol
		if v <= 0 {
			v = c.Volume
		}
		vals[i] = v
	}
	// Insertion sort on small slice (<= 36 bars).
	for i := 1; i < len(vals); i++ {
		x := vals[i]
		j := i - 1
		for j >= 0 && vals[j] > x {
			vals[j+1] = vals[j]
			j--
		}
		vals[j+1] = x
	}
	mid := len(vals) / 2
	if len(vals)%2 == 1 {
		return vals[mid]
	}
	return 0.5 * (vals[mid-1] + vals[mid])
}
