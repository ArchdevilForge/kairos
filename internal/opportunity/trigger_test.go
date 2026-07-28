package opportunity

import (
	"testing"

	"github.com/ArchdevilForge/kairos/internal/types"
)

func synthLongPullback() []types.Candle {
	// impulse up, pullback HL, restart
	var out []types.Candle
	px := 100.0
	// base grind
	for i := 0; i < 10; i++ {
		px += 0.1
		out = append(out, bar(px, px+0.3, px-0.3))
	}
	trough := px
	// impulse
	for i := 0; i < 8; i++ {
		px += 1.2
		out = append(out, bar(px, px+0.4, px-0.2))
	}
	peak := px
	// pullback not breaking trough
	for i := 0; i < 5; i++ {
		px -= 0.6
		out = append(out, bar(px, px+0.3, px-0.3))
	}
	if px <= trough {
		px = trough + 1
		out[len(out)-1] = bar(px, px+0.2, trough+0.5)
	}
	_ = peak
	// restart up
	for i := 0; i < 3; i++ {
		px += 0.8
		out = append(out, bar(px, px+0.3, px-0.2))
	}
	return out
}

func bar(c, h, l float64) types.Candle {
	if h < c {
		h = c
	}
	if l > c {
		l = c
	}
	return types.Candle{Open: c, Close: c, High: h, Low: l, Volume: 1000, Timestamp: 1}
}

func TestDetectPullbackTrigger_Long(t *testing.T) {
	r := DetectPullbackTrigger(types.CycleDirectionUp, synthLongPullback())
	if !r.OK {
		t.Fatalf("want OK, failures=%v", r.Failures)
	}
	if !r.HigherLow || r.Entry <= 0 || r.Stop >= r.Entry {
		t.Fatalf("%+v", r)
	}
	if r.PullbackDepthPct <= 0 {
		t.Fatal("depth")
	}
}

func TestDetectPullbackTrigger_NoImpulse(t *testing.T) {
	var flat []types.Candle
	for i := 0; i < 40; i++ {
		flat = append(flat, bar(100, 100.1, 99.9))
	}
	r := DetectPullbackTrigger(types.CycleDirectionUp, flat)
	if r.OK {
		t.Fatal("flat must fail")
	}
}

func TestDetectPullbackTrigger_ShortMirror(t *testing.T) {
	// mirror of long path
	long := synthLongPullback()
	base := long[0].Close
	var short []types.Candle
	for _, c := range long {
		// reflect around base
		cl := base - (c.Close - base)
		h := base - (c.Low - base)
		l := base - (c.High - base)
		if h < l {
			h, l = l, h
		}
		short = append(short, types.Candle{Open: cl, Close: cl, High: h, Low: l, Volume: 1000, Timestamp: c.Timestamp})
	}
	r := DetectPullbackTrigger(types.CycleDirectionDown, short)
	if !r.OK {
		t.Fatalf("short mirror failures=%v reasons=%v", r.Failures, r.Reasons)
	}
	if !r.LowerHigh {
		t.Fatal("want lower high")
	}
}
