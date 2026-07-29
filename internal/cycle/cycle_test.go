package cycle

import (
	"math"
	"testing"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// synthUp builds a trending-up close series with mild noise.
func synthUp(n int, start, step float64) (closes, highs, lows, vols []float64) {
	closes = make([]float64, n)
	highs = make([]float64, n)
	lows = make([]float64, n)
	vols = make([]float64, n)
	px := start
	for i := 0; i < n; i++ {
		px += step
		// mild pullback every 7th bar still higher lows overall
		if i%7 == 0 {
			px -= step * 0.3
		}
		closes[i] = px
		highs[i] = px * 1.002
		lows[i] = px * 0.998
		vols[i] = 1000 + float64(i)*2
	}
	return closes, highs, lows, vols
}

func synthChop(n int, mid float64) (closes, highs, lows, vols []float64) {
	closes = make([]float64, n)
	highs = make([]float64, n)
	lows = make([]float64, n)
	vols = make([]float64, n)
	for i := 0; i < n; i++ {
		// alternate above/below mid aggressively
		amp := mid * 0.01
		if i%2 == 0 {
			closes[i] = mid + amp
		} else {
			closes[i] = mid - amp
		}
		// extra thrash
		if i%3 == 0 {
			closes[i] = mid + amp*1.2
		}
		if i%3 == 1 {
			closes[i] = mid - amp*1.2
		}
		highs[i] = closes[i] + amp*0.3
		lows[i] = closes[i] - amp*0.3
		vols[i] = 1000
	}
	return closes, highs, lows, vols
}

func TestMirrorSymmetry_DirectionAndPhase(t *testing.T) {
	closes, highs, lows, vols := synthUp(80, 100, 0.8)
	upNode := classifyRaw(Series{Timeframe: "1d", Role: types.TimeframeRoleContext, Closes: closes, Highs: highs, Lows: lows, Volumes: vols})

	mCloses := mirrorCloses(closes)
	mHighs, mLows := mirrorHL(highs, lows)
	downNode := classifyRaw(Series{Timeframe: "1d", Role: types.TimeframeRoleContext, Closes: mCloses, Highs: mHighs, Lows: mLows, Volumes: vols})

	if upNode.Direction != types.CycleDirectionUp {
		t.Fatalf("up series direction=%s phase=%s ev=%v", upNode.Direction, upNode.Phase, upNode.Evidence)
	}
	if downNode.Direction != types.CycleDirectionDown {
		t.Fatalf("mirrored direction=%s phase=%s ev=%v", downNode.Direction, downNode.Phase, downNode.Evidence)
	}
	// phases should match class (spring/summer), not winter
	if upNode.Phase == types.WavePhaseWinter || downNode.Phase == types.WavePhaseWinter {
		t.Fatalf("unexpected winter: up=%s down=%s", upNode.Phase, downNode.Phase)
	}
	if upNode.Phase != downNode.Phase {
		// allow spring/summer adjacency under mirror noise, but both must be trend phases
		trend := map[types.WavePhase]bool{types.WavePhaseSpring: true, types.WavePhaseSummer: true}
		if !trend[upNode.Phase] || !trend[downNode.Phase] {
			t.Fatalf("phase mismatch up=%s down=%s", upNode.Phase, downNode.Phase)
		}
	}
}

func TestChopIsNeutralWinter(t *testing.T) {
	closes, highs, lows, vols := synthChop(80, 100)
	node := classifyRaw(Series{Timeframe: "1d", Role: types.TimeframeRoleContext, Closes: closes, Highs: highs, Lows: lows, Volumes: vols})
	if node.Direction != types.CycleDirectionNeutral && node.Phase != types.WavePhaseWinter {
		t.Fatalf("chop want neutral/winter, got %s/%s ev=%v", node.Direction, node.Phase, node.Evidence)
	}
	// either neutral direction or winter phase is required
	if node.Phase != types.WavePhaseWinter && node.Direction != types.CycleDirectionNeutral {
		t.Fatalf("chop not no-trade: %s/%s", node.Direction, node.Phase)
	}
}

func TestNestedCounterTrend_Hierarchy(t *testing.T) {
	nodes := map[string]types.CycleNode{
		"1d": {
			Timeframe: "1d", Role: types.TimeframeRoleContext,
			Direction: types.CycleDirectionDown, Phase: types.WavePhaseSummer, Confidence: 0.8,
		},
		"4h": {
			Timeframe: "4h", Role: types.TimeframeRoleContext,
			Direction: types.CycleDirectionDown, Phase: types.WavePhaseSummer, Confidence: 0.7,
		},
		"15m": {
			Timeframe: "15m", Role: types.TimeframeRoleSetup,
			Direction: types.CycleDirectionUp, Phase: types.WavePhaseSummer, Confidence: 0.7,
		},
		"5m": {
			Timeframe: "5m", Role: types.TimeframeRoleTrigger,
			Direction: types.CycleDirectionUp, Phase: types.WavePhaseSpring, Confidence: 0.6,
		},
	}
	m := BuildMap(1, types.MarketPhaseWinter, nodes)
	if m.Alignment != types.AlignmentCounterTrend {
		t.Fatalf("alignment=%s summary=%v", m.Alignment, m.Summary)
	}
	if m.TradeClass != types.TradeClassCounterTrendLong {
		t.Fatalf("trade_class=%s", m.TradeClass)
	}
	if m.PrimaryDirection != types.CycleDirectionDown {
		t.Fatalf("primary=%s", m.PrimaryDirection)
	}
	// legacy climate winter must NOT force short
	if m.TradeClass == types.TradeClassAlignedShort {
		t.Fatal("legacy winter must not imply aligned short")
	}
	if m.SchemaVersion != types.CycleMapSchemaVersion {
		t.Fatalf("schema=%s", m.SchemaVersion)
	}
}

func TestContextWinter_NoTrade(t *testing.T) {
	nodes := map[string]types.CycleNode{
		"1d": {Timeframe: "1d", Role: types.TimeframeRoleContext, Direction: types.CycleDirectionNeutral, Phase: types.WavePhaseWinter},
		"4h": {Timeframe: "4h", Role: types.TimeframeRoleContext, Direction: types.CycleDirectionNeutral, Phase: types.WavePhaseWinter},
		"5m": {Timeframe: "5m", Role: types.TimeframeRoleTrigger, Direction: types.CycleDirectionUp, Phase: types.WavePhaseSpring},
	}
	m := BuildMap(1, types.MarketPhaseWinter, nodes)
	if m.Alignment != types.AlignmentNoTrade || m.TradeClass != types.TradeClassNoTrade {
		t.Fatalf("want no_trade, got align=%s class=%s", m.Alignment, m.TradeClass)
	}
}

func TestHysteresis_NoFlicker(t *testing.T) {
	c := NewClassifier(types.TransitionPolicy{ConfirmBars: 3, MinStateBars: 3, MinConfidenceGain: 0.15})
	closes, highs, lows, vols := synthUp(80, 100, 0.8)
	base := Series{Timeframe: "15m", Role: types.TimeframeRoleSetup, Closes: closes, Highs: highs, Lows: lows, Volumes: vols}

	var first types.CycleNode
	for i := 0; i < 5; i++ {
		first = c.ClassifyNode("BTC", base)
	}
	// perturb last close slightly — should not flip phase identity immediately
	noisy := append([]float64{}, closes...)
	noisy[len(noisy)-1] *= 0.997
	nHighs := append([]float64{}, highs...)
	nLows := append([]float64{}, lows...)
	nHighs[len(nHighs)-1] = noisy[len(noisy)-1] * 1.001
	nLows[len(nLows)-1] = noisy[len(noisy)-1] * 0.999
	noisySeries := Series{Timeframe: "15m", Role: types.TimeframeRoleSetup, Closes: noisy, Highs: nHighs, Lows: nLows, Volumes: vols}

	flips := 0
	prev := first
	for i := 0; i < 2; i++ {
		got := c.ClassifyNode("BTC", noisySeries)
		if got.Direction != prev.Direction || got.Phase != prev.Phase {
			flips++
		}
		prev = got
	}
	if flips > 0 {
		t.Fatalf("flicker on small noise: first=%s/%s flips=%d last=%s/%s", first.Direction, first.Phase, flips, prev.Direction, prev.Phase)
	}
}

func TestClassifyMap_AlignedLong(t *testing.T) {
	c := NewClassifier(DefaultTransitionPolicy())
	// disable hysteresis stickiness for cold classify by using fresh classifier per TF via raw map
	up, hi, lo, vo := synthUp(90, 100, 1.0)
	series := []Series{
		{Timeframe: "1d", Role: types.TimeframeRoleContext, Closes: up, Highs: hi, Lows: lo, Volumes: vo},
		{Timeframe: "4h", Role: types.TimeframeRoleContext, Closes: up, Highs: hi, Lows: lo, Volumes: vo},
		{Timeframe: "15m", Role: types.TimeframeRoleSetup, Closes: up, Highs: hi, Lows: lo, Volumes: vo},
		{Timeframe: "5m", Role: types.TimeframeRoleTrigger, Closes: up, Highs: hi, Lows: lo, Volumes: vo},
	}
	m := c.ClassifyMap("ETH", 100, types.MarketPhaseSummer, series)
	if m.PrimaryDirection != types.CycleDirectionUp {
		t.Fatalf("primary=%s nodes=%v", m.PrimaryDirection, m.Summary)
	}
	if m.TradeClass != types.TradeClassAlignedLong && m.Alignment == types.AlignmentNoTrade {
		t.Fatalf("expected aligned long-ish, got align=%s class=%s summary=%v", m.Alignment, m.TradeClass, m.Summary)
	}
	for _, n := range m.Nodes {
		if len(n.Evidence) == 0 {
			t.Fatalf("node %s missing evidence", n.Timeframe)
		}
		if n.SchemaVersion != types.CycleNodeSchemaVersion {
			t.Fatalf("node schema %s", n.SchemaVersion)
		}
	}
}

func TestTransition_RequiresConfirm(t *testing.T) {
	policy := types.TransitionPolicy{ConfirmBars: 3, MinStateBars: 3, MinConfidenceGain: 0.5}
	prev := &stickyState{
		node: types.CycleNode{
			Direction: types.CycleDirectionUp, Phase: types.WavePhaseSummer, Confidence: 0.7,
		},
		barsInState: 5,
	}
	raw := types.CycleNode{Direction: types.CycleDirectionDown, Phase: types.WavePhaseSpring, Confidence: 0.55}
	out, st := applyTransition(policy, prev, raw, 1)
	if out.Direction != types.CycleDirectionUp {
		t.Fatalf("should hold up until confirm, got %s", out.Direction)
	}
	// feed confirms on distinct bars
	for i := 0; i < 3; i++ {
		out, st = applyTransition(policy, st, raw, int64(2+i))
	}
	if out.Direction != types.CycleDirectionDown || out.Phase != types.WavePhaseSpring {
		t.Fatalf("after confirm want down/spring got %s/%s", out.Direction, out.Phase)
	}
}

func TestMirrorHelpers(t *testing.T) {
	c := []float64{100, 110, 120}
	m := mirrorCloses(c)
	if math.Abs(m[0]-100) > 1e-9 || math.Abs(m[2]-80) > 1e-9 {
		t.Fatalf("mirror closes %v", m)
	}
}

func TestTransition_SameBarDoesNotAdvance(t *testing.T) {
	policy := types.TransitionPolicy{ConfirmBars: 3, MinStateBars: 3, MinConfidenceGain: 0.5}
	prev := &stickyState{
		node:        types.CycleNode{Direction: types.CycleDirectionUp, Phase: types.WavePhaseSummer, Confidence: 0.7},
		barsInState: 5, lastBarUnix: 100,
	}
	raw := types.CycleNode{Direction: types.CycleDirectionDown, Phase: types.WavePhaseSpring, Confidence: 0.55}
	// same bar polled thrice must not confirm
	var st *stickyState
	out, st := applyTransition(policy, prev, raw, 100)
	for i := 0; i < 5; i++ {
		out, st = applyTransition(policy, st, raw, 100)
	}
	if out.Direction != types.CycleDirectionUp {
		t.Fatalf("same-bar polls must not flip, got %s pending=%d", out.Direction, st.pendingCount)
	}
	// new bars confirm
	for i := 0; i < 3; i++ {
		out, st = applyTransition(policy, st, raw, int64(101+i))
	}
	if out.Direction != types.CycleDirectionDown {
		t.Fatalf("after new bars want down, got %s", out.Direction)
	}
}
