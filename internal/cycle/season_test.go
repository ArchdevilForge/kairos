package cycle

import (
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
			got := ClassifySeasonWithThresholds(tc.breadthFast, tc.breadthSlow, tc.volRatio, DefaultSeasonThresholds())
			if got != tc.want {
				t.Fatalf("ClassifySeasonWithThresholds(%v, %v, %v) = %s, want %s",
					tc.breadthFast, tc.breadthSlow, tc.volRatio, got, tc.want)
			}
		})
	}
}

func TestHysteresisTimeline(t *testing.T) {
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
		b5 := make([]SeasonBar5m, 0, 40)
		px := 100.0 + float64(sIdx*10)
		for bar5 := 0; bar5 < 40; bar5++ {
			open := px
			var step, qv float64
			switch {
			case bar5 < 15:
				step = -0.10
				qv = 250_000
			case bar5 < 30:
				step = 1.25
				qv = 750_000
			default:
				if sIdx < 3 {
					step = 0.25
				} else {
					step = -0.75
				}
				qv = 450_000
			}
			closePx := open + step
			hi := open + 0.05
			lo := closePx - 0.05
			if closePx > open {
				hi = closePx + 0.05
				lo = open - 0.05
			}
			b5 = append(b5, SeasonBar5m{
				CloseTS:  baseTS + int64((bar5+1)*300),
				Open:     open,
				High:     hi,
				Low:      lo,
				Close:    closePx,
				QuoteVol: qv,
			})
			px = closePx
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
}

func TestSeasonBar5mFromCandle_CloseTSAndQuoteVol(t *testing.T) {
	openTS := int64(1_762_646_400) // aligned 5m open timestamp
	want := SeasonBar5m{
		CloseTS:  openTS + 300,
		Open:     100.0,
		High:     102.5,
		Low:      99.0,
		Close:    101.5,
		QuoteVol: 100_000,
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
	if gotSec != want {
		t.Fatalf("SeasonBar5mFromCandle(sec) = %+v, want %+v", gotSec, want)
	}
	if gotSec.CloseTS != openTS+300 {
		t.Fatalf("CloseTS = %d, want %d", gotSec.CloseTS, openTS+300)
	}

	// Millisecond timestamp defensive normalization must yield identical CloseTS.
	candleMs := candleSec
	candleMs.Timestamp = openTS * 1000
	if gotMs := SeasonBar5mFromCandle(candleMs); gotMs != want {
		t.Fatalf("SeasonBar5mFromCandle(ms) = %+v, want %+v", gotMs, want)
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
