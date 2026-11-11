package cycle

import (
	"encoding/json"
	"math"
	"os"
	"testing"

	"github.com/ArchdevilForge/kairos/internal/types"
)

func TestClassifySeasonFromAggregates_RulesAndBoundaries(t *testing.T) {
	cases := []struct {
		name        string
		breadthFast float64
		breadthSlow float64
		volRatio    float64
		want        types.SeasonRegime
	}{
		{
			name:        "summer exact boundary",
			breadthFast: 0.58,
			breadthSlow: 0.50,
			volRatio:    0.98,
			want:        types.SeasonRegimeSummer,
		},
		{
			name:        "autumn from breadth momentum decay",
			breadthFast: 0.50,
			breadthSlow: 0.55, // b_mom = -0.05 < -0.02
			volRatio:    1.10,
			want:        types.SeasonRegimeAutumn,
		},
		{
			name:        "autumn from volume fade",
			breadthFast: 0.55,
			breadthSlow: 0.54, // b_mom = +0.01 >= -0.02, but volRatio < 0.98
			volRatio:    0.90,
			want:        types.SeasonRegimeAutumn,
		},
		{
			name:        "spring recovery",
			breadthFast: 0.48,
			breadthSlow: 0.45, // b_mom = +0.03 >= -0.02
			volRatio:    0.90, // >= 0.85 winter_vol_max
			want:        types.SeasonRegimeSpring,
		},
		{
			name:        "winter low breadth",
			breadthFast: 0.30,
			breadthSlow: 0.35,
			volRatio:    1.05,
			want:        types.SeasonRegimeWinter,
		},
		{
			name:        "winter contracted volume",
			breadthFast: 0.50,
			breadthSlow: 0.45,
			volRatio:    0.80, // < 0.85 winter_vol_max
			want:        types.SeasonRegimeWinter,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifySeasonFromAggregates(tc.breadthFast, tc.breadthSlow, tc.volRatio)
			if got != tc.want {
				t.Fatalf("ClassifySeasonFromAggregates(%v, %v, %v) = %s, want %s",
					tc.breadthFast, tc.breadthSlow, tc.volRatio, got, tc.want)
			}
		})
	}
}

func TestBuild5mBarsFrom1m_AndHysteresisTimeline(t *testing.T) {
	syms := []string{
		"BTC/USDT:USDT",
		"ETH/USDT:USDT",
		"SOL/USDT:USDT",
		"XRP/USDT:USDT",
		"DOGE/USDT:USDT",
		"SUI/USDT:USDT",
	}
	baseTS := int64(1_762_646_400)
	bars5mBySym := make(map[string][]SeasonBar5m, len(syms))
	fundingBySym := make(map[string]map[int64]float64, len(syms))

	for sIdx, sym := range syms {
		var b1m []SeasonBar1m
		px := 100.0 + float64(sIdx*10)
		// 40 5m bars = 200 1m bars
		for bar5 := 0; bar5 < 40; bar5++ {
			for m := 0; m < 5; m++ {
				ts := baseTS + int64(bar5*300+m*60)
				open := px
				// Bars 0..14: flat/down chop (WINTER)
				// Bars 15..29: broad expansion with high volume (SUMMER)
				// Bars 30..39: breadth/volume decay (AUTUMN/WINTER)
				var step, qv float64
				switch {
				case bar5 < 15:
					step = -0.02
					qv = 50_000
				case bar5 < 30:
					step = 0.25
					qv = 150_000
				default:
					if sIdx < 3 {
						step = 0.05
					} else {
						step = -0.15
					}
					qv = 90_000
				}
				closePx := open + step
				b1m = append(b1m, SeasonBar1m{
					TS:       ts,
					Open:     open,
					High:     math.Max(open, closePx) + 0.05,
					Low:      math.Min(open, closePx) - 0.05,
					Close:    closePx,
					QuoteVol: qv,
				})
				px = closePx
			}
		}
		b5 := Build5mBarsFrom1m(b1m)
		if len(b5) != 40 {
			t.Fatalf("%s: expected 40 5m bars, got %d", sym, len(b5))
		}
		bars5mBySym[sym] = b5
		fundingBySym[sym] = map[int64]float64{
			baseTS:        0.0001,
			baseTS + 1800: 0.0002,
			baseTS + 3600: 0.0003,
		}
	}

	timeline := BuildCausalSeasonTimeline(bars5mBySym, fundingBySym)
	if len(timeline) == 0 {
		t.Fatal("expected non-empty causal season timeline")
	}

	// Early chop bar (bar index 10 -> closeTS = baseTS + 11*300) must be WINTER.
	earlyTS := baseTS + 11*300
	obsEarly, ok := timeline[earlyTS]
	if !ok {
		t.Fatalf("missing early observation at %d", earlyTS)
	}
	if obsEarly.SeasonRegime != types.SeasonRegimeWinter {
		t.Fatalf("early regime = %s, want WINTER", obsEarly.SeasonRegime)
	}

	// Expansion bar after >=2 confirm bars (bar index 22 -> closeTS = baseTS + 23*300) must be SUMMER.
	summerTS := baseTS + 23*300
	obsSummer, ok := timeline[summerTS]
	if !ok {
		t.Fatalf("missing summer observation at %d", summerTS)
	}
	if obsSummer.SeasonRegime != types.SeasonRegimeSummer {
		t.Fatalf("summer regime = %s (bFast=%.2f, bSlow=%.2f, vRatio=%.2f), want SUMMER",
			obsSummer.SeasonRegime, obsSummer.BreadthFast, obsSummer.BreadthSlow, obsSummer.VolumeRatio)
	}

	// LatestSeasonObservationAtOrBefore must work for both exact and intermediate timestamps.
	latest, ok := LatestSeasonObservationAtOrBefore(timeline, summerTS+120)
	if !ok || latest.TS != summerTS || latest.SeasonRegime != types.SeasonRegimeSummer {
		t.Fatalf("LatestSeasonObservationAtOrBefore = %+v (ok=%v), want TS=%d SUMMER", latest, ok, summerTS)
	}

	// CalibrateOOSSeasonRegime with >= 20 prior observations.
	var prior []SeasonObservation
	for bar5 := 3; bar5 < 25; bar5++ {
		if o, exists := timeline[baseTS+int64(bar5+1)*300]; exists {
			prior = append(prior, o)
		}
	}
	oosRegime := CalibrateOOSSeasonRegime(obsSummer, prior)
	if !types.IsValidSeasonRegime(oosRegime) {
		t.Fatalf("invalid OOS regime: %s", oosRegime)
	}
}

type consistencyDump struct {
	Bars1mBySym  map[string][]SeasonBar1m     `json:"bars_1m_by_sym"`
	FundingBySym map[string]map[int64]float64 `json:"funding_by_sym"`
	PyTimeline   map[int64]SeasonObservation  `json:"py_timeline"`
	PyTickets    []struct {
		SignalID        string             `json:"signal_id"`
		WaveClusterID   string             `json:"wave_cluster_id"`
		Symbol          string             `json:"symbol"`
		EntryTS         int64              `json:"entry_ts"`
		FeatureTS       int64              `json:"feature_ts"`
		SeasonRegime    types.SeasonRegime `json:"season_regime"`
		SeasonRegimeOOS types.SeasonRegime `json:"season_regime_oos"`
		BreadthFast     float64            `json:"breadth_fast"`
		BreadthSlow     float64            `json:"breadth_slow"`
		VolumeRatio     float64            `json:"volume_ratio"`
	} `json:"py_tickets"`
}

func TestPhase2bConsistencyWithPython(t *testing.T) {
	dumpPath := os.Getenv("KAIROS_PHASE2B_DUMP")
	if dumpPath == "" {
		dumpPath = "/tmp/phase2b_consistency_dump.json"
	}
	raw, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Skipf("skipping live Phase 2b dump comparison (%s not present: %v)", dumpPath, err)
	}

	var dump consistencyDump
	if err := json.Unmarshal(raw, &dump); err != nil {
		t.Fatalf("unmarshal dump: %v", err)
	}

	bars5mBySym := make(map[string][]SeasonBar5m, len(dump.Bars1mBySym))
	for sym, b1m := range dump.Bars1mBySym {
		if len(b1m) == 0 {
			continue
		}
		bars5mBySym[sym] = Build5mBarsFrom1m(b1m)
	}

	goTimeline := BuildCausalSeasonTimeline(bars5mBySym, dump.FundingBySym)
	if len(goTimeline) != len(dump.PyTimeline) {
		t.Fatalf("timeline len mismatch: Go=%d vs Python=%d", len(goTimeline), len(dump.PyTimeline))
	}

	timelineMatches := 0
	var timelineMismatches []string
	for ts, pyObs := range dump.PyTimeline {
		goObs, ok := goTimeline[ts]
		if !ok {
			timelineMismatches = append(timelineMismatches, "missing ts")
			continue
		}
		if goObs.SeasonRegime == pyObs.SeasonRegime &&
			math.Abs(goObs.BreadthFast-pyObs.BreadthFast) < 1e-12 &&
			math.Abs(goObs.BreadthSlow-pyObs.BreadthSlow) < 1e-12 &&
			math.Abs(goObs.VolumeRatio-pyObs.VolumeRatio) < 1e-12 &&
			goObs.BTCNearHigh == pyObs.BTCNearHigh &&
			math.Abs(goObs.CrowdingPct-pyObs.CrowdingPct) < 1e-12 {
			timelineMatches++
		} else {
			t.Errorf("mismatch at ts=%d: Go=%+v vs Py=%+v", ts, goObs, pyObs)
			timelineMismatches = append(timelineMismatches, string(goObs.SeasonRegime))
		}
	}

	ticketMatches := 0
	for _, tk := range dump.PyTickets {
		goObs, ok := goTimeline[tk.EntryTS]
		if !ok {
			t.Errorf("ticket %s missing timeline at entry_ts=%d", tk.SignalID, tk.EntryTS)
			continue
		}
		if goObs.SeasonRegime == tk.SeasonRegime &&
			math.Abs(goObs.BreadthFast-tk.BreadthFast) < 1e-12 &&
			math.Abs(goObs.BreadthSlow-tk.BreadthSlow) < 1e-12 &&
			math.Abs(goObs.VolumeRatio-tk.VolumeRatio) < 1e-12 {
			ticketMatches++
		} else {
			t.Errorf("ticket %s mismatch at entry_ts=%d: Go=%s vs Py=%s",
				tk.SignalID, tk.EntryTS, goObs.SeasonRegime, tk.SeasonRegime)
		}
	}

	timelineRate := float64(timelineMatches) / float64(len(dump.PyTimeline)) * 100.0
	ticketRate := float64(ticketMatches) / float64(len(dump.PyTickets)) * 100.0
	t.Logf("Phase 2b consistency: timeline=%d/%d (%.4f%%), tickets=%d/%d (%.4f%%), mismatches=%d",
		timelineMatches, len(dump.PyTimeline), timelineRate,
		ticketMatches, len(dump.PyTickets), ticketRate, len(timelineMismatches))

	if timelineRate < 99.9 || ticketRate < 99.9 {
		t.Fatalf("consistency below 99.9%% bar: timeline=%.4f%% tickets=%.4f%%", timelineRate, ticketRate)
	}
}

func TestSeasonBar5mFromCandle_MatchesBuild5mBarsFrom1mConvention(t *testing.T) {
	openTS := int64(1_762_646_400) // aligned 5m open timestamp
	b1m := []SeasonBar1m{
		{TS: openTS + 0, Open: 100.0, High: 101.0, Low: 99.5, Close: 100.4, QuoteVol: 10_000},
		{TS: openTS + 60, Open: 100.4, High: 102.5, Low: 100.2, Close: 102.0, QuoteVol: 20_000},
		{TS: openTS + 120, Open: 102.0, High: 102.2, Low: 99.0, Close: 99.8, QuoteVol: 30_000},
		{TS: openTS + 180, Open: 99.8, High: 101.5, Low: 99.7, Close: 101.2, QuoteVol: 15_000},
		{TS: openTS + 240, Open: 101.2, High: 101.8, Low: 100.9, Close: 101.5, QuoteVol: 25_000},
	}
	built := Build5mBarsFrom1m(b1m)
	if len(built) != 1 {
		t.Fatalf("expected 1 aggregated 5m bar, got %d", len(built))
	}

	candleSec := types.Candle{
		Timestamp: openTS,
		Open:      100.0,
		High:      102.5,
		Low:       99.0,
		Close:     101.5,
		Volume:    999, // base/contract volume; must NOT be mixed into QuoteVol
		QuoteVol:  100_000,
	}
	gotSec := SeasonBar5mFromCandle(candleSec)
	if gotSec != built[0] {
		t.Fatalf("SeasonBar5mFromCandle(sec) = %+v, want exact match with Build5mBarsFrom1m %+v", gotSec, built[0])
	}
	if gotSec.CloseTS != openTS+300 {
		t.Fatalf("CloseTS = %d, want %d", gotSec.CloseTS, openTS+300)
	}

	// Millisecond timestamp defensive normalization must yield identical CloseTS.
	candleMs := candleSec
	candleMs.Timestamp = openTS * 1000
	if gotMs := SeasonBar5mFromCandle(candleMs); gotMs != built[0] {
		t.Fatalf("SeasonBar5mFromCandle(ms) = %+v, want %+v", gotMs, built[0])
	}
}

func TestBuildCausalSeasonTimeline_FundingDoesNotAffectSeasonRegime(t *testing.T) {
	syms := []string{
		"BTC/USDT:USDT",
		"ETH/USDT:USDT",
		"SOL/USDT:USDT",
		"XRP/USDT:USDT",
		"DOGE/USDT:USDT",
	}
	baseClose := int64(1_762_646_700)
	bars := make(map[string][]SeasonBar5m, len(syms))
	funding := make(map[string]map[int64]float64, len(syms))
	for sIdx, sym := range syms {
		series := make([]SeasonBar5m, 30)
		px := 100.0
		for i := range series {
			step := -0.2
			qv := 50_000.0
			if i >= 12 {
				step = 0.6
				qv = 140_000.0
			}
			series[i] = SeasonBar5m{
				CloseTS:  baseClose + int64(i*300),
				Open:     px,
				High:     px + 0.8,
				Low:      px - 0.5,
				Close:    px + step,
				QuoteVol: qv + float64(sIdx*1000),
			}
			px += step
		}
		bars[sym] = series
		funding[sym] = map[int64]float64{
			baseClose:        0.0010,
			baseClose + 1200: 0.0025,
			baseClose + 2400: 0.0050,
			baseClose + 3600: -0.0020,
		}
	}

	tlNil := BuildCausalSeasonTimeline(bars, nil)
	tlWithFunding := BuildCausalSeasonTimeline(bars, funding)
	if len(tlNil) == 0 || len(tlNil) != len(tlWithFunding) {
		t.Fatalf("timeline len mismatch: nil=%d vs funding=%d", len(tlNil), len(tlWithFunding))
	}
	for ts, obsNil := range tlNil {
		obsF, ok := tlWithFunding[ts]
		if !ok {
			t.Fatalf("missing ts=%d in funded timeline", ts)
		}
		if obsNil.SeasonRegime != obsF.SeasonRegime ||
			obsNil.BreadthFast != obsF.BreadthFast ||
			obsNil.BreadthSlow != obsF.BreadthSlow ||
			obsNil.VolumeRatio != obsF.VolumeRatio {
			t.Fatalf("SeasonRegime changed with funding at ts=%d: nil=%+v vs funded=%+v", ts, obsNil, obsF)
		}
	}
}
