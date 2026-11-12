package notice

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ArchdevilForge/kairos/internal/cycle"
	"github.com/ArchdevilForge/kairos/internal/storage"
	"github.com/ArchdevilForge/kairos/internal/types"
)

type fakeSender struct {
	sent []string
	err  error
}

func (f *fakeSender) SendText(_ context.Context, text string) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, text)
	return nil
}

func makePoint1Candles() []types.Candle {
	// 16 closed bars: first 8 bars oscillate in [100, 108] (range 8),
	// next 7 bars converge in [102, 107.8] (range 5.8 <= 8*0.92), >=2 upper touches near 108,
	// 16th bar breaks out at 109.2 with 2.5x volume.
	var cs []types.Candle
	baseTS := int64(1_760_000_000)
	highs := []float64{107.9, 105.0, 108.0, 104.5, 107.2, 104.0, 106.5, 104.8, 107.6, 105.5, 107.8, 105.8, 107.5, 106.0, 107.4}
	lows := []float64{100.0, 101.2, 101.5, 100.2, 101.8, 100.5, 101.5, 101.0, 102.2, 102.8, 103.0, 103.2, 103.5, 103.8, 104.0}
	for i := 0; i < len(highs); i++ {
		mid := 0.5 * (highs[i] + lows[i])
		cs = append(cs, types.Candle{
			Timestamp: baseTS + int64(i*14400),
			Open:      mid - 0.3,
			High:      highs[i],
			Low:       lows[i],
			Close:     mid + 0.3,
			Volume:    1000,
			QuoteVol:  100_000,
		})
	}
	cs = append(cs, types.Candle{
		Timestamp: baseTS + int64(15*14400),
		Open:      107.4,
		High:      109.5,
		Low:       107.2,
		Close:     109.2,
		Volume:    2600,
		QuoteVol:  280_000,
	})
	return cs
}

func makePoint2Candles() []types.Candle {
	// 16 closed bars: fluent 13-bar uptrend from 100 -> 112 (high=112.5, ER high, vol=2000),
	// followed by 3 shallow pullback bars with low=110.2 (retrace ~2.04% from 112.5) and lower volume (1100).
	var cs []types.Candle
	baseTS := int64(1_760_000_000)
	for i := 0; i < 13; i++ {
		c := 100.0 + float64(i)*1.0
		h := c + 0.4
		if i == 12 {
			h = 112.5
		}
		cs = append(cs, types.Candle{
			Timestamp: baseTS + int64(i*14400),
			Open:      c - 0.5,
			High:      h,
			Low:       c - 0.6,
			Close:     c,
			Volume:    2000,
			QuoteVol:  200_000,
		})
	}
	pbCloses := []float64{111.4, 110.8, 111.5}
	pbLows := []float64{111.0, 110.2, 110.6}
	for j := 0; j < 3; j++ {
		cs = append(cs, types.Candle{
			Timestamp: baseTS + int64((13+j)*14400),
			Open:      pbCloses[j] + 0.2,
			High:      pbCloses[j] + 0.4,
			Low:       pbLows[j],
			Close:     pbCloses[j],
			Volume:    1100,
			QuoteVol:  110_000,
		})
	}
	return cs
}

func makePoint4Candles() []types.Candle {
	// 24 closed bars:
	// Bars 0..14: fluent Wave-1 from 100 (low 99.5) up to 118 (high 118.5, +19.1%, high ER).
	// Bars 15..22: 8 bars major divergence consolidation with lows down to 111.0 (~39.5% retrace of 19.0 amp) and highs ~115.5.
	// Bar 23: right-side restart at 116.2.
	var cs []types.Candle
	baseTS := int64(1_760_000_000)
	for i := 0; i <= 14; i++ {
		c := 100.0 + float64(i)*1.25
		lo := c - 0.5
		hi := c + 0.4
		if i == 0 {
			lo = 99.5
		}
		if i == 14 {
			hi = 118.5
		}
		cs = append(cs, types.Candle{
			Timestamp: baseTS + int64(i*14400),
			Open:      c - 0.4,
			High:      hi,
			Low:       lo,
			Close:     c,
			Volume:    1800,
			QuoteVol:  180_000,
		})
	}
	consolCloses := []float64{115.0, 113.2, 111.8, 113.5, 112.4, 114.2, 113.8, 114.8}
	consolHighs := []float64{116.0, 114.5, 113.0, 114.8, 113.8, 115.2, 114.9, 115.4}
	consolLows := []float64{114.0, 112.5, 111.0, 112.2, 111.6, 113.0, 112.8, 113.6}
	for j := 0; j < len(consolCloses); j++ {
		cs = append(cs, types.Candle{
			Timestamp: baseTS + int64((15+j)*14400),
			Open:      consolCloses[j] - 0.2,
			High:      consolHighs[j],
			Low:       consolLows[j],
			Close:     consolCloses[j],
			Volume:    1000,
			QuoteVol:  100_000,
		})
	}
	cs = append(cs, types.Candle{
		Timestamp: baseTS + int64(23*14400),
		Open:      114.8,
		High:      116.6,
		Low:       114.6,
		Close:     116.3,
		Volume:    2400,
		QuoteVol:  240_000,
	})
	return cs
}

func makeInsideBoxCandles() []types.Candle {
	// 16 closed bars oscillating sideways inside [100, 108] (width 8%, low ER, current price 104.0 -> pos 0.50).
	var cs []types.Candle
	baseTS := int64(1_760_000_000)
	closes := []float64{101.5, 106.8, 102.0, 107.0, 101.8, 106.5, 102.2, 106.9, 102.0, 106.4, 102.5, 106.2, 102.4, 106.0, 103.5, 104.0}
	for i, c := range closes {
		cs = append(cs, types.Candle{
			Timestamp: baseTS + int64(i*14400),
			Open:      104.0,
			High:      108.0,
			Low:       100.0,
			Close:     c,
			Volume:    1000,
			QuoteVol:  100_000,
		})
	}
	return cs
}

func TestDetectScenes_Table(t *testing.T) {
	tests := []struct {
		name     string
		input    SceneInput
		wantFlag types.NoticeSceneFlag
		wantNone bool
	}{
		{
			name: "Point 1 converging box breakout",
			input: SceneInput{
				Symbol:       "SOL/USDT:USDT",
				CurrentPrice: 109.2,
				TriggerFacts: types.NoticeTriggerFacts{EventType: "volume_spike", VolumeRatio: 6.2},
				Candles4h:    makePoint1Candles(),
			},
			wantFlag: types.ScenePoint1Breakout,
		},
		{
			name: "Point 2 minor divergence pullback",
			input: SceneInput{
				Symbol:       "ETH/USDT:USDT",
				CurrentPrice: 111.8,
				TriggerFacts: types.NoticeTriggerFacts{EventType: "price_velocity", PriceChangePct: 1.2},
				Candles4h:    makePoint2Candles(),
			},
			wantFlag: types.ScenePoint2MinorDiv,
		},
		{
			name: "Point 4 major divergence second wave",
			input: SceneInput{
				Symbol:       "SUI/USDT:USDT",
				CurrentPrice: 116.3,
				TriggerFacts: types.NoticeTriggerFacts{EventType: "open_interest_change", OIChangePct: 5.5},
				Candles4h:    makePoint4Candles(),
			},
			wantFlag: types.ScenePoint4Wave2,
		},
		{
			name: "Resonance scene without candles",
			input: SceneInput{
				Symbol:       "DOGE/USDT:USDT",
				CurrentPrice: 0.22,
				TriggerFacts: types.NoticeTriggerFacts{
					EventType:      "resonance",
					DimensionCount: 2,
					Dimensions:     []string{"price_velocity", "volume_spike"},
				},
			},
			wantFlag: types.SceneResonanceLead,
		},
		{
			name: "No candles and single non-resonance event fabricates zero scene flags",
			input: SceneInput{
				Symbol:       "ARB/USDT:USDT",
				CurrentPrice: 1.05,
				TriggerFacts: types.NoticeTriggerFacts{EventType: "volume_spike", VolumeRatio: 4.5},
			},
			wantNone: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scenes := DetectScenes(tc.input)
			if tc.wantNone {
				if len(scenes) != 0 {
					t.Fatalf("expected 0 scenes when uncomputable, got %+v", scenes)
				}
				return
			}
			found := false
			for _, sc := range scenes {
				if sc.Flag == tc.wantFlag {
					found = true
				}
				if bad, hit := ContainsForbiddenNoticeTerms(sc.Summary); hit {
					t.Fatalf("scene summary %q contains forbidden term %q", sc.Summary, bad)
				}
			}
			if !found {
				t.Fatalf("expected flag %s in %+v", tc.wantFlag, scenes)
			}
		})
	}
}

func TestExtractTriggerFacts_AndCopyRedLines_Table(t *testing.T) {
	events := []types.AnomalyEvent{
		{
			Symbol:    "SOL/USDT:USDT",
			EventType: "volume_spike",
			Exchange:  "okx",
			Data:      map[string]any{"price": 185.4, "ratio": 6.2, "window_minutes": 5},
		},
		{
			Symbol:    "ETH/USDT:USDT",
			EventType: "price_velocity",
			Exchange:  "okx",
			Data:      map[string]any{"price_to": 3120.5, "change_pct": 1.45, "window_seconds": 60},
		},
		{
			Symbol:    "SUI/USDT:USDT",
			EventType: "open_interest_change",
			Exchange:  "okx",
			Data:      map[string]any{"price": 3.42, "change_pct": 6.8, "open_interest": 85000000.0},
		},
		{
			Symbol:    "DOGE/USDT:USDT",
			EventType: "resonance",
			Exchange:  "okx",
			Data: map[string]any{
				"signal_score":        88.5, // MUST NOT leak into rendered notice!
				"dimension_count":     2,
				"dimensions":          []string{"volume_spike", "price_velocity"},
				"volume_spike_data":   map[string]any{"ratio": 7.1, "window_minutes": 10, "price": 0.25},
				"price_velocity_data": map[string]any{"change_pct": 1.8, "window_seconds": 60},
			},
		},
	}

	for _, evt := range events {
		t.Run(evt.EventType, func(t *testing.T) {
			facts, ok := ExtractTriggerFacts(evt)
			if !ok {
				t.Fatalf("expected supported trigger event %s", evt.EventType)
			}
			ntc := types.Notice{
				NoticeID:     "ntc-test-" + evt.EventType,
				Symbol:       evt.Symbol,
				Exchange:     evt.Exchange,
				TriggerFacts: facts,
				Scenes:       DetectScenes(SceneInput{Symbol: evt.Symbol, CurrentPrice: facts.Price, TriggerFacts: facts, Candles4h: makePoint1Candles()}),
				Env: types.NoticeEnvTags{
					SeasonRegime: types.SeasonRegimeWinter,
					BoxState:     types.NoticeBoxOutside,
					BTCState:     "BTC 24h +1.10%（4h 均线上方）",
				},
			}
			rendered := FormatNoticeTelegram(ntc)
			if !strings.Contains(rendered, types.NoticeDisclaimerLine) {
				t.Fatalf("missing mandatory disclaimer in:\n%s", rendered)
			}
			if !strings.Contains(rendered, "0.25x") {
				t.Fatalf("expected winter 0.25x hint in:\n%s", rendered)
			}
			if strings.Contains(rendered, "88.5") {
				t.Fatalf("resonance signal_score leaked into rendered notice:\n%s", rendered)
			}
			if bad, hit := ContainsForbiddenNoticeTerms(rendered); hit {
				t.Fatalf("rendered notice contains forbidden term %q:\n%s", bad, rendered)
			}
		})
	}
}

func TestEnvTags_SeasonOmittedWhenUncomputable_AndComputedFromCausal5m(t *testing.T) {
	// Case 1: Empty 5m universe -> SeasonRegime MUST be empty and absent from rendered HTML.
	envEmpty := EvaluateEnvTags(EnvInput{
		EventUnixSec: 1_760_100_000,
		CurrentPrice: 109.2,
		Candles4h:    makePoint1Candles(),
	})
	if envEmpty.SeasonRegime != "" {
		t.Fatalf("expected empty SeasonRegime when uncomputable, got %q", envEmpty.SeasonRegime)
	}
	htmlEmpty := FormatNoticeTelegram(types.Notice{
		NoticeID:     "ntc-noseason",
		Symbol:       "SOL/USDT:USDT",
		TriggerFacts: types.NoticeTriggerFacts{EventType: "volume_spike", FactSummary: "5 分钟量 = 窗口基准 6.20×"},
		Env:          envEmpty,
	})
	if strings.Contains(htmlEmpty, "季节标签") {
		t.Fatalf("uncomputable season must be omitted from rendered notice:\n%s", htmlEmpty)
	}

	// Case 2: Genuine 5m universe (>=4 symbols, >=30 bars) -> SeasonRegime computed causally.
	syms := []string{"BTC/USDT:USDT", "ETH/USDT:USDT", "SOL/USDT:USDT", "SUI/USDT:USDT", "DOGE/USDT:USDT"}
	u5m := make(map[string][]cycle.SeasonBar5m, len(syms))
	startTS := int64(1_760_000_000)
	for idx, sym := range syms {
		bars := make([]cycle.SeasonBar5m, 36)
		for b := 0; b < 36; b++ {
			px := 100.0 + float64(b)*(0.35+0.02*float64(idx))
			bars[b] = cycle.SeasonBar5m{
				CloseTS:  startTS + int64((b+1)*300),
				Open:     px - 0.2,
				High:     px + 0.3,
				Low:      px - 0.3,
				Close:    px,
				QuoteVol: 500_000 + float64(b)*20_000,
			}
		}
		u5m[sym] = bars
	}
	lastTS := startTS + 36*300
	envSeason := EvaluateEnvTags(EnvInput{
		EventUnixSec:   lastTS,
		CurrentPrice:   109.2,
		Candles4h:      makePoint1Candles(),
		UniverseBars5m: u5m,
	})
	if !types.IsValidSeasonRegime(envSeason.SeasonRegime) {
		t.Fatalf("expected valid SeasonRegime from causal 5m timeline, got %q", envSeason.SeasonRegime)
	}
}

func TestService_SuppressionCooldownAndDailyCap(t *testing.T) {
	dir := t.TempDir()
	nj, err := storage.NewNoticeJournal(types.StorageConfig{DatabasePath: dir + "/kairos.db"})
	if err != nil {
		t.Fatalf("NewNoticeJournal: %v", err)
	}
	sender := &fakeSender{}
	svc := NewService(types.NoticeConfig{
		Enabled:               true,
		SymbolCooldownMinutes: 240, // 4h
		MaxNoticesPerDay:      2,   // small cap to test daily_cap cleanly
	}, nj, sender)

	base := time.Date(2026, 11, 11, 8, 0, 0, 0, time.UTC)

	// 1. Inside-box event -> suppressed (reason=inside_box), never sent to Telegram, logged to journal.
	resBox, err := svc.ProcessAnomaly(context.Background(), ProcessRequest{
		Event: types.AnomalyEvent{
			Symbol:    "SOL/USDT:USDT",
			EventType: "volume_spike",
			Timestamp: float64(base.Unix()),
			Data:      map[string]any{"price": 104.0, "ratio": 6.5, "window_minutes": 5},
		},
		Candles4h: makeInsideBoxCandles(),
	})
	if err != nil {
		t.Fatalf("ProcessAnomaly inside_box: %v", err)
	}
	if resBox.DeliveryStatus != types.NoticeStatusSuppressed || resBox.SuppressReason != types.NoticeSuppressInsideBox {
		t.Fatalf("expected inside_box suppression, got %+v", resBox)
	}
	if len(sender.sent) != 0 {
		t.Fatalf("suppressed notice must not send to Telegram")
	}

	// 2. Breakout event on SOL -> sent (#1 of 2 daily cap).
	t1 := base.Add(10 * time.Minute)
	res1, err := svc.ProcessAnomaly(context.Background(), ProcessRequest{
		Event: types.AnomalyEvent{
			Symbol:    "SOL/USDT:USDT",
			EventType: "volume_spike",
			Timestamp: float64(t1.Unix()),
			Data:      map[string]any{"price": 109.2, "ratio": 6.2, "window_minutes": 5},
		},
		Candles4h: makePoint1Candles(),
	})
	if err != nil || res1.DeliveryStatus != types.NoticeStatusSent {
		t.Fatalf("expected sent #1, got %+v, err=%v", res1, err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("expected 1 Telegram message sent, got %d", len(sender.sent))
	}

	// 3. Repeat SOL 2 hours later (< 4h cooldown) -> suppressed (symbol_cooldown).
	t2 := t1.Add(2 * time.Hour)
	resCool, err := svc.ProcessAnomaly(context.Background(), ProcessRequest{
		Event: types.AnomalyEvent{
			Symbol:    "SOL/USDT:USDT",
			EventType: "price_velocity",
			Timestamp: float64(t2.Unix()),
			Data:      map[string]any{"price_to": 110.5, "change_pct": 1.6, "window_seconds": 60},
		},
		Candles4h: makePoint1Candles(),
	})
	if err != nil {
		t.Fatalf("ProcessAnomaly cooldown: %v", err)
	}
	if resCool.DeliveryStatus != types.NoticeStatusSuppressed || resCool.SuppressReason != types.NoticeSuppressSymbolCooldown {
		t.Fatalf("expected symbol_cooldown suppression, got %+v", resCool)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("cooldown suppressed notice must not send")
	}

	// 4. Different symbol ETH -> sent (#2 of 2 daily cap).
	t3 := t1.Add(3 * time.Hour)
	res2, err := svc.ProcessAnomaly(context.Background(), ProcessRequest{
		Event: types.AnomalyEvent{
			Symbol:    "ETH/USDT:USDT",
			EventType: "open_interest_change",
			Timestamp: float64(t3.Unix()),
			Data:      map[string]any{"price": 109.4, "change_pct": 6.0},
		},
		Candles4h: makePoint1Candles(),
	})
	if err != nil || res2.DeliveryStatus != types.NoticeStatusSent {
		t.Fatalf("expected sent #2, got %+v, err=%v", res2, err)
	}

	// 5. Third symbol SUI within same 24h -> suppressed by daily_cap (2/2).
	t4 := t1.Add(5 * time.Hour)
	resCap, err := svc.ProcessAnomaly(context.Background(), ProcessRequest{
		Event: types.AnomalyEvent{
			Symbol:    "SUI/USDT:USDT",
			EventType: "volume_spike",
			Timestamp: float64(t4.Unix()),
			Data:      map[string]any{"price": 109.5, "ratio": 7.0, "window_minutes": 5},
		},
		Candles4h: makePoint1Candles(),
	})
	if err != nil {
		t.Fatalf("ProcessAnomaly daily_cap: %v", err)
	}
	if resCap.DeliveryStatus != types.NoticeStatusSuppressed || resCap.SuppressReason != types.NoticeSuppressDailyCap {
		t.Fatalf("expected daily_cap suppression, got %+v", resCap)
	}
	if len(sender.sent) != 2 {
		t.Fatalf("daily_cap suppressed notice must not send, sent=%d", len(sender.sent))
	}

	// Verify all 5 notices (2 sent + 3 suppressed) are persisted in notice-journal.jsonl.
	stats, err := nj.BudgetStats("", t4, 24*time.Hour)
	if err != nil {
		t.Fatalf("BudgetStats: %v", err)
	}
	if stats.TotalNotices != 5 || stats.SentTotal != 2 || stats.SuppressedTotal != 3 {
		t.Fatalf("unexpected journal stats: %+v", stats)
	}
	if stats.SuppressedByReason[types.NoticeSuppressInsideBox] != 1 ||
		stats.SuppressedByReason[types.NoticeSuppressSymbolCooldown] != 1 ||
		stats.SuppressedByReason[types.NoticeSuppressDailyCap] != 1 {
		t.Fatalf("unexpected suppression breakdown: %+v", stats.SuppressedByReason)
	}
}

// Production evidence this table locks down (2026-11-11/12 kairosd run):
//   - notice.symbolCooldownMinutes=240 never fired without a Telegram channel:
//     AKE emitted at 04:30, 05:27 and 07:56.
//   - AAVE's single -2.12% move shipped twice in the same minute because the 60s
//     and 120s velocity windows both triggered.
func TestService_AcceptedBudgetAndEventDedup_Table(t *testing.T) {
	type step struct {
		symbol     string
		at         time.Time
		changePct  float64
		windowSecs int
	}
	base := time.Date(2026, 11, 11, 4, 30, 0, 0, time.UTC)

	cases := []struct {
		name       string
		cfg        types.NoticeConfig
		sender     TextSender
		steps      []step
		wantStatus []types.NoticeDeliveryStatus
		wantReason []string
		// wantAccepted is the accepted-notice count the journal must report, i.e.
		// how many notices the owner actually paid attention to.
		wantAccepted int
	}{
		{
			name: "no telegram channel still runs the 4h symbol cooldown",
			cfg: types.NoticeConfig{
				Enabled:               true,
				SymbolCooldownMinutes: 240,
				MaxNoticesPerDay:      12,
			},
			sender: nil,
			steps: []step{
				{symbol: "AKE/USDT:USDT", at: base, changePct: -2.40, windowSecs: 60},
				{symbol: "AKE/USDT:USDT", at: base.Add(57 * time.Minute), changePct: 2.10, windowSecs: 60},
				{symbol: "AKE/USDT:USDT", at: base.Add(3*time.Hour + 26*time.Minute), changePct: -1.90, windowSecs: 60},
			},
			wantStatus: []types.NoticeDeliveryStatus{
				types.NoticeStatusDryRun,
				types.NoticeStatusSuppressed,
				types.NoticeStatusSuppressed,
			},
			wantReason:   []string{"", types.NoticeSuppressSymbolCooldown, types.NoticeSuppressSymbolCooldown},
			wantAccepted: 1,
		},
		{
			name: "same move on 60s and 120s windows ships one notice",
			cfg: types.NoticeConfig{
				Enabled:               true,
				SymbolCooldownMinutes: 240,
				MaxNoticesPerDay:      12,
			},
			sender: &fakeSender{},
			steps: []step{
				{symbol: "AAVE/USDT:USDT", at: base, changePct: -2.12, windowSecs: 60},
				{symbol: "AAVE/USDT:USDT", at: base.Add(40 * time.Second), changePct: -2.12, windowSecs: 120},
			},
			wantStatus: []types.NoticeDeliveryStatus{
				types.NoticeStatusSent,
				types.NoticeStatusSuppressed,
			},
			wantReason:   []string{"", types.NoticeSuppressDuplicateEvent},
			wantAccepted: 1,
		},
		{
			name: "failed delivery still consumes cooldown",
			cfg: types.NoticeConfig{
				Enabled:               true,
				SymbolCooldownMinutes: 240,
				MaxNoticesPerDay:      12,
			},
			sender: &fakeSender{err: errors.New("telegram 500")},
			steps: []step{
				{symbol: "SUI/USDT:USDT", at: base, changePct: 2.50, windowSecs: 60},
				{symbol: "SUI/USDT:USDT", at: base.Add(30 * time.Minute), changePct: -2.60, windowSecs: 60},
			},
			wantStatus: []types.NoticeDeliveryStatus{
				types.NoticeStatusFailed,
				types.NoticeStatusSuppressed,
			},
			wantReason:   []string{"", types.NoticeSuppressSymbolCooldown},
			wantAccepted: 1,
		},
		{
			name: "daily cap counts accepted dry-run notices",
			cfg: types.NoticeConfig{
				Enabled:               true,
				SymbolCooldownMinutes: 240,
				MaxNoticesPerDay:      2,
			},
			sender: nil,
			steps: []step{
				{symbol: "SOL/USDT:USDT", at: base, changePct: 1.80, windowSecs: 60},
				{symbol: "ETH/USDT:USDT", at: base.Add(10 * time.Minute), changePct: -1.70, windowSecs: 60},
				{symbol: "XRP/USDT:USDT", at: base.Add(20 * time.Minute), changePct: 2.20, windowSecs: 60},
			},
			wantStatus: []types.NoticeDeliveryStatus{
				types.NoticeStatusDryRun,
				types.NoticeStatusDryRun,
				types.NoticeStatusSuppressed,
			},
			wantReason:   []string{"", "", types.NoticeSuppressDailyCap},
			wantAccepted: 2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			nj, err := storage.NewNoticeJournal(types.StorageConfig{DatabasePath: filepath.Join(dir, "kairos.db")})
			if err != nil {
				t.Fatalf("NewNoticeJournal: %v", err)
			}
			svc := NewService(tc.cfg, nj, tc.sender)

			last := base
			for i, st := range tc.steps {
				evt := types.AnomalyEvent{
					Symbol:    st.symbol,
					EventType: "price_velocity",
					Timestamp: float64(st.at.Unix()),
					Data: map[string]any{
						"price_to":       109.4,
						"change_pct":     st.changePct,
						"window_seconds": st.windowSecs,
					},
				}
				got, err := svc.ProcessAnomaly(context.Background(), ProcessRequest{
					Event:     evt,
					Candles4h: makePoint1Candles(),
				})
				// A failed send returns the notice together with the error; the
				// journal record is what matters for budget accounting.
				if err != nil && (got == nil || got.DeliveryStatus != types.NoticeStatusFailed) {
					t.Fatalf("step %d (%s): %v", i, st.symbol, err)
				}
				if got == nil {
					t.Fatalf("step %d (%s): nil notice", i, st.symbol)
				}
				if got.DeliveryStatus != tc.wantStatus[i] {
					t.Fatalf("step %d (%s): status = %q, want %q (reason=%q detail=%q)",
						i, st.symbol, got.DeliveryStatus, tc.wantStatus[i], got.SuppressReason, got.SuppressDetail)
				}
				if got.SuppressReason != tc.wantReason[i] {
					t.Fatalf("step %d (%s): suppress_reason = %q, want %q", i, st.symbol, got.SuppressReason, tc.wantReason[i])
				}
				if got.DeliveryStatus == types.NoticeStatusSuppressed && strings.TrimSpace(got.SuppressDetail) == "" {
					t.Fatalf("step %d (%s): suppressed notice must carry a journal-visible detail", i, st.symbol)
				}
				if st.at.After(last) {
					last = st.at
				}
			}

			stats, err := nj.BudgetStats("", last, 24*time.Hour)
			if err != nil {
				t.Fatalf("BudgetStats: %v", err)
			}
			if stats.AcceptedTotal != tc.wantAccepted {
				t.Fatalf("accepted notices = %d, want %d (stats=%+v)", stats.AcceptedTotal, tc.wantAccepted, stats)
			}
			if stats.TotalNotices != len(tc.steps) {
				t.Fatalf("journaled notices = %d, want %d (nothing may be dropped silently)", stats.TotalNotices, len(tc.steps))
			}
		})
	}
}

// The 120s record must name the accepted window so the comparison is auditable.
func TestService_DuplicateEventDetailNamesRepresentative(t *testing.T) {
	dir := t.TempDir()
	nj, err := storage.NewNoticeJournal(types.StorageConfig{DatabasePath: filepath.Join(dir, "kairos.db")})
	if err != nil {
		t.Fatalf("NewNoticeJournal: %v", err)
	}
	svc := NewService(types.NoticeConfig{Enabled: true, SymbolCooldownMinutes: 240, MaxNoticesPerDay: 12}, nj, &fakeSender{})
	base := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)

	first, err := svc.ProcessAnomaly(context.Background(), ProcessRequest{
		Event: types.AnomalyEvent{
			Symbol: "AAVE/USDT:USDT", EventType: "price_velocity", Timestamp: float64(base.Unix()),
			Data: map[string]any{"price_to": 109.4, "change_pct": -2.12, "window_seconds": 120},
		},
		Candles4h: makePoint1Candles(),
	})
	if err != nil || first.DeliveryStatus != types.NoticeStatusSent {
		t.Fatalf("first window: %+v err=%v", first, err)
	}
	second, err := svc.ProcessAnomaly(context.Background(), ProcessRequest{
		Event: types.AnomalyEvent{
			Symbol: "AAVE/USDT:USDT", EventType: "price_velocity", Timestamp: float64(base.Add(30 * time.Second).Unix()),
			Data: map[string]any{"price_to": 109.4, "change_pct": -1.95, "window_seconds": 60},
		},
		Candles4h: makePoint1Candles(),
	})
	if err != nil || second.DeliveryStatus != types.NoticeStatusSuppressed ||
		second.SuppressReason != types.NoticeSuppressDuplicateEvent {
		t.Fatalf("second window: %+v err=%v", second, err)
	}
	if !strings.Contains(second.SuppressDetail, first.NoticeID) ||
		!strings.Contains(second.SuppressDetail, "120 秒") ||
		!strings.Contains(second.SuppressDetail, "信息量更低") {
		t.Fatalf("duplicate detail must name the representative window/notice, got %q", second.SuppressDetail)
	}
}

func TestService_ConfigAllowInsideBox_BTCStateAndPlainFormat(t *testing.T) {
	allowInside := false
	sender := &fakeSender{}
	svc := NewService(types.NoticeConfig{
		Enabled:           true,
		SuppressInsideBox: &allowInside,
	}, nil, sender)

	btcCandles := makePoint2Candles()
	ntc, err := svc.ProcessAnomaly(context.Background(), ProcessRequest{
		Event: types.AnomalyEvent{
			Symbol:    "SOL/USDT:USDT",
			EventType: "volume_spike",
			Timestamp: 1_760_100_000,
			Data:      map[string]any{"price": 104.0, "ratio": 5.5, "window_minutes": 5},
		},
		Candles4h:    makeInsideBoxCandles(),
		BTCCandles4h: btcCandles,
		SeasonRegime: types.SeasonRegimeSummer,
	})
	if err != nil || ntc == nil || ntc.DeliveryStatus != types.NoticeStatusSent {
		t.Fatalf("expected inside_box notice allowed when suppressInsideBox=false, got %+v err=%v", ntc, err)
	}
	if !strings.Contains(ntc.Env.BTCState, "BTC 24h") {
		t.Fatalf("expected BTC 24h state, got %q", ntc.Env.BTCState)
	}
	plain := FormatNoticePlain(*ntc, &types.NoticeFeedback{
		NoticeID: ntc.NoticeID,
		Action:   types.NoticeFeedbackActed,
		TicketID: "tkt-1",
		Note:     "人工复盘",
	})
	if !strings.Contains(plain, "做了") || !strings.Contains(plain, types.NoticeDisclaimerLine) {
		t.Fatalf("unexpected plain output:\n%s", plain)
	}
}
