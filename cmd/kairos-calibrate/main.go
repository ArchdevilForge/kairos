// Command kairos-calibrate turns the MarketPulse event and outcome JSONL logs
// into the numbers needed to set thresholds from evidence instead of intuition.
//
// It joins each recorded outcome back to the event that produced it and reports
// how often an alert was followed by continued movement in the same direction,
// bucketed by how strong the triggering move was. That bucket table is the one
// that answers "where should minMedianReturnPct actually sit".
//
// Attention lift compares alert 5m continuation to a *non-alert directional
// baseline* built from 60s snapshots (alert windows excluded). Small-n output
// is labeled experimental — do not treat lift as a production gate until n is large.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/ArchdevilForge/kairos/internal/config"
	"github.com/ArchdevilForge/kairos/internal/storage"
)

type eventRecord struct {
	Timestamp       float64 `json:"timestamp"`
	EventType       string  `json:"event_type"`
	Direction       string  `json:"direction"`
	MedianReturn60s float64 `json:"median_return_60s"`
	Breadth         float64 `json:"breadth"`
	ValidSymbols    int     `json:"valid_symbols"`
	FreshRatio      float64 `json:"fresh_ratio"`
	ShadowMode      bool    `json:"shadow_mode"`
}

type outcomeRecord struct {
	EventTS          float64  `json:"event_ts"`
	SourceEvent      string   `json:"source_event"`
	Direction        string   `json:"direction"`
	MedianReturn1m   *float64 `json:"median_return_1m"`
	MedianReturn5m   *float64 `json:"median_return_5m"`
	MedianReturn15m  *float64 `json:"median_return_15m"`
	MFE              float64  `json:"mfe"`
	MAE              float64  `json:"mae"`
	Reversed         bool     `json:"reversed"`
	ImpulsePrecision bool     `json:"impulse_precision"`
	TrendPrecision   bool     `json:"trend_precision"`
	ShadowMode       bool     `json:"shadow_mode"`
}

// joined pairs an outcome with the event that produced it.
type joined struct {
	ev  eventRecord
	out outcomeRecord
}

const (
	// Alert windows are removed from the directional baseline so the treatment
	// group does not pollute the control group.
	excludePreSeconds  = 300.0
	excludePostSeconds = 900.0

	forwardHorizonSeconds = 300.0
	forwardMatchTol       = 45.0

	// Evidence tiers for lift (not trading gates — observation only).
	minAlertExploratory = 30
	minAlertProvisional = 100
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "config file used to locate the storage directory")
	eventsPath := flag.String("events", "", "override path to market-pulse-events.jsonl")
	outcomesPath := flag.String("outcomes", "", "override path to market-pulse-outcomes.jsonl")
	snapshotsPath := flag.String("snapshots", "", "override path to market-pulse-snapshots.jsonl")
	includeShadow := flag.Bool("include-shadow", true, "include events/outcomes recorded in shadow mode")
	noisePct := flag.Float64("noise-pct", 0.08, "|median_return_60s| floor for directional baseline samples")
	flag.Parse()

	evPath, outPath, snapPath := *eventsPath, *outcomesPath, *snapshotsPath
	if evPath == "" || outPath == "" || snapPath == "" {
		cfg, err := config.Load(*cfgPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "无法从配置定位存储目录: %v\n", err)
			fmt.Fprintln(os.Stderr, "可用 --events / --outcomes / --snapshots 直接指定文件路径。")
			os.Exit(2)
		}
		if evPath == "" {
			evPath = storage.MarketPulseEventsPath(cfg.Storage)
		}
		if outPath == "" {
			outPath = storage.MarketPulseOutcomesPath(cfg.Storage)
		}
		if snapPath == "" {
			snapPath = storage.MarketPulseSnapshotsPath(cfg.Storage)
		}
	}

	events, err := loadEvents(evPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取事件失败 %s: %v\n", evPath, err)
		os.Exit(1)
	}
	outcomes, err := loadOutcomes(outPath)
	if err != nil {
		// Outcomes are written 15 minutes after an event; a missing file is a
		// normal state early on, not a failure.
		fmt.Fprintf(os.Stderr, "读取校准结果失败 %s: %v\n", outPath, err)
	}
	snaps, err := loadSnapshots(snapPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取 snapshot 失败 %s: %v\n", snapPath, err)
	}

	if !*includeShadow {
		events = filterEvents(events, func(e eventRecord) bool { return !e.ShadowMode })
		outcomes = filterOutcomes(outcomes, func(o outcomeRecord) bool { return !o.ShadowMode })
	}

	fmt.Printf("事件文件: %s (%d 条)\n", evPath, len(events))
	fmt.Printf("结果文件: %s (%d 条)\n", outPath, len(outcomes))
	fmt.Printf("快照文件: %s (%d 条, 按分钟去重后)\n\n", snapPath, len(snaps))

	reportEvents(events)

	if len(outcomes) == 0 {
		fmt.Println("\n尚无 outcome 记录，无法评估精确率 / lift。")
		fmt.Println("outcome 在事件发生约 15 分钟后写入，需要进程在此期间持续运行。")
		if len(snaps) == 0 {
			fmt.Println("snapshot 亦为空：启用 MarketPulse 后会每 60s 写入一条轻量横截面样本。")
		}
		return
	}

	pairs := join(events, outcomes)
	reportOutcomes(outcomes, pairs)
	reportBuckets(pairs)
	reportLift(outcomes, events, snaps, *noisePct)
}

func loadEvents(path string) ([]eventRecord, error) {
	var out []eventRecord
	err := eachLine(path, func(line []byte) error {
		var r eventRecord
		if err := json.Unmarshal(line, &r); err != nil {
			return err
		}
		out = append(out, r)
		return nil
	})
	return out, err
}

func loadOutcomes(path string) ([]outcomeRecord, error) {
	var out []outcomeRecord
	err := eachLine(path, func(line []byte) error {
		// Outcomes are stored with the payload nested under "payload".
		var wrapper struct {
			Payload outcomeRecord `json:"payload"`
		}
		if err := json.Unmarshal(line, &wrapper); err == nil && wrapper.Payload.SourceEvent != "" {
			out = append(out, wrapper.Payload)
			return nil
		}
		var r outcomeRecord
		if err := json.Unmarshal(line, &r); err != nil {
			return err
		}
		out = append(out, r)
		return nil
	})
	return out, err
}

func eachLine(path string, fn func([]byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	n := 0
	for sc.Scan() {
		n++
		line := sc.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		if err := fn(line); err != nil {
			return fmt.Errorf("第 %d 行: %w", n, err)
		}
	}
	return sc.Err()
}

func filterEvents(in []eventRecord, keep func(eventRecord) bool) []eventRecord {
	var out []eventRecord
	for _, e := range in {
		if keep(e) {
			out = append(out, e)
		}
	}
	return out
}

func filterOutcomes(in []outcomeRecord, keep func(outcomeRecord) bool) []outcomeRecord {
	var out []outcomeRecord
	for _, o := range in {
		if keep(o) {
			out = append(out, o)
		}
	}
	return out
}

// join matches each outcome to its event by timestamp. The detector copies the
// event timestamp verbatim into event_ts, so an exact-ish match is safe.
func join(events []eventRecord, outcomes []outcomeRecord) []joined {
	byTS := make(map[int64]eventRecord, len(events))
	for _, e := range events {
		byTS[int64(math.Round(e.Timestamp))] = e
	}
	var out []joined
	for _, o := range outcomes {
		if e, ok := byTS[int64(math.Round(o.EventTS))]; ok {
			out = append(out, joined{ev: e, out: o})
		}
	}
	return out
}

func reportEvents(events []eventRecord) {
	if len(events) == 0 {
		fmt.Println("没有事件记录。")
		return
	}
	byType := map[string]int{}
	byDir := map[string]int{}
	var mags []float64
	for _, e := range events {
		byType[e.EventType]++
		byDir[e.Direction]++
		mags = append(mags, math.Abs(e.MedianReturn60s))
	}
	fmt.Println("── 事件分布 ──")
	for _, k := range sortedKeys(byType) {
		fmt.Printf("  %-16s %d\n", k, byType[k])
	}
	for _, k := range sortedKeys(byDir) {
		fmt.Printf("  方向 %-11s %d\n", k, byDir[k])
	}
	sort.Float64s(mags)
	fmt.Printf("  |median_60s|: 最小 %.3f%%  中位 %.3f%%  P90 %.3f%%  最大 %.3f%%\n",
		mags[0], pct(mags, 0.5), pct(mags, 0.9), mags[len(mags)-1])
}

func reportOutcomes(outcomes []outcomeRecord, pairs []joined) {
	fmt.Println("\n── 校准结果 ──")
	fmt.Printf("  可与事件对上的结果: %d / %d\n", len(pairs), len(outcomes))

	var impN, impOK, trN, trOK, revN int
	var have5m int
	var cont5m int
	for _, o := range outcomes {
		switch o.SourceEvent {
		case "market_impulse", "market_stress":
			impN++
			if o.ImpulsePrecision {
				impOK++
			}
		case "market_trend":
			trN++
			if o.TrendPrecision {
				trOK++
			}
		}
		if o.Reversed {
			revN++
		}
		if o.MedianReturn5m != nil {
			have5m++
			if continued(o.Direction, *o.MedianReturn5m) {
				cont5m++
			}
		}
	}
	printRate("  impulse/stress 精确率", impOK, impN)
	printRate("  trend 精确率        ", trOK, trN)
	printRate("  +5m 同向延续率      ", cont5m, have5m)
	printRate("  期间反向率          ", revN, len(outcomes))
	if have5m < len(outcomes) {
		fmt.Printf("  注意: %d 条结果缺少 +5m 样本（断流期间不采样，按缺失处理）\n",
			len(outcomes)-have5m)
	}
}

// reportBuckets is the table that should drive minMedianReturnPct: it shows how
// the payoff of an alert changes with the strength of the move that triggered it.
func reportBuckets(pairs []joined) {
	if len(pairs) == 0 {
		return
	}
	type bucket struct {
		label    string
		lo, hi   float64
		n        int
		cont     int
		haveCont int
		sumMFE   float64
		sumMAE   float64
	}
	buckets := []*bucket{
		{label: "0.15–0.25%", lo: 0.15, hi: 0.25},
		{label: "0.25–0.35%", lo: 0.25, hi: 0.35},
		{label: "0.35–0.50%", lo: 0.35, hi: 0.50},
		{label: "≥0.50%", lo: 0.50, hi: math.Inf(1)},
	}
	for _, p := range pairs {
		mag := math.Abs(p.ev.MedianReturn60s)
		for _, b := range buckets {
			if mag >= b.lo && mag < b.hi {
				b.n++
				b.sumMFE += p.out.MFE
				b.sumMAE += p.out.MAE
				if p.out.MedianReturn5m != nil {
					b.haveCont++
					if continued(p.out.Direction, *p.out.MedianReturn5m) {
						b.cont++
					}
				}
				break
			}
		}
	}
	fmt.Println("\n── 按触发强度分桶（用于定阈值）──")
	fmt.Printf("  %-12s %5s %14s %9s %9s\n", "触发 |med60|", "事件", "+5m 同向延续", "均值MFE", "均值MAE")
	for _, b := range buckets {
		if b.n == 0 {
			fmt.Printf("  %-12s %5d %14s %9s %9s\n", b.label, 0, "-", "-", "-")
			continue
		}
		contStr := "-"
		if b.haveCont > 0 {
			contStr = fmt.Sprintf("%d/%d (%.0f%%)", b.cont, b.haveCont,
				100*float64(b.cont)/float64(b.haveCont))
		}
		fmt.Printf("  %-12s %5d %14s %8.3f%% %8.3f%%\n",
			b.label, b.n, contStr, b.sumMFE/float64(b.n), b.sumMAE/float64(b.n))
	}
	fmt.Println("\n  读法: 若低强度桶的延续率明显低于高强度桶，说明阈值应上移到延续率开始稳定的那一档。")
}

// continued reports whether a post-event return extended the event direction.
func continued(direction string, ret float64) bool {
	if direction == "down" {
		return ret < 0
	}
	return ret > 0
}

type snapshotRecord struct {
	Timestamp        float64 `json:"timestamp"`
	DataOK           bool    `json:"data_ok"`
	MedianReturn60s  float64 `json:"median_return_60s"`
	MedianReturn300s float64 `json:"median_return_300s"`
	UpBreadth60s     float64 `json:"up_breadth_60s"`
	DownBreadth60s   float64 `json:"down_breadth_60s"`
}

func loadSnapshots(path string) ([]snapshotRecord, error) {
	var raw []snapshotRecord
	err := eachLine(path, func(line []byte) error {
		var r snapshotRecord
		if err := json.Unmarshal(line, &r); err != nil {
			return err
		}
		raw = append(raw, r)
		return nil
	})
	sort.Slice(raw, func(i, j int) bool { return raw[i].Timestamp < raw[j].Timestamp })
	return dedupeSnapshotsByMinute(raw), err
}

// dedupeSnapshotsByMinute keeps the last row per floor(ts/60) so process restarts
// that re-write the same minute do not double-weight the baseline.
func dedupeSnapshotsByMinute(in []snapshotRecord) []snapshotRecord {
	if len(in) == 0 {
		return in
	}
	sorted := append([]snapshotRecord(nil), in...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Timestamp < sorted[j].Timestamp })
	byMin := make(map[int64]snapshotRecord, len(sorted))
	order := make([]int64, 0, len(sorted))
	seen := make(map[int64]bool, len(sorted))
	for _, s := range sorted {
		k := int64(math.Floor(s.Timestamp / 60))
		if !seen[k] {
			seen[k] = true
			order = append(order, k)
		}
		byMin[k] = s // later timestamp in the same minute wins
	}
	out := make([]snapshotRecord, 0, len(order))
	for _, k := range order {
		out = append(out, byMin[k])
	}
	return out
}

type excludeWindow struct {
	lo, hi float64
}

func alertExcludeWindows(events []eventRecord, outcomes []outcomeRecord) []excludeWindow {
	tsSet := map[int64]struct{}{}
	for _, e := range events {
		if e.Timestamp > 0 {
			tsSet[int64(math.Round(e.Timestamp))] = struct{}{}
		}
	}
	for _, o := range outcomes {
		if o.EventTS > 0 {
			tsSet[int64(math.Round(o.EventTS))] = struct{}{}
		}
	}
	out := make([]excludeWindow, 0, len(tsSet))
	for ts := range tsSet {
		t := float64(ts)
		out = append(out, excludeWindow{lo: t - excludePreSeconds, hi: t + excludePostSeconds})
	}
	return out
}

func inExclude(ts float64, wins []excludeWindow) bool {
	for _, w := range wins {
		if ts >= w.lo && ts <= w.hi {
			return true
		}
	}
	return false
}

// intervalOverlapsExclude is true when [start, end] shares any time with an
// alert exclusion window. Baseline must drop samples whose whole +5m horizon
// leaks into alert buildup/aftermath, not only those whose start is inside.
func intervalOverlapsExclude(start, end float64, wins []excludeWindow) bool {
	if end < start {
		start, end = end, start
	}
	for _, w := range wins {
		if start <= w.hi && end >= w.lo {
			return true
		}
	}
	return false
}

type rateCell struct {
	n, cont int
}

func (c rateCell) rateLaplace() float64 {
	// Jeffreys/Laplace: avoid zero-rate cells killing stratification.
	return float64(c.cont+1) / float64(c.n+2)
}

// baselineSample is one non-alert directional minute used as a control.
type baselineSample struct {
	ts        float64
	hour      int
	bucket    int // med60 magnitude bucket
	direction string
	cont      bool
}

func collectBaseline(snaps []snapshotRecord, noisePct float64, wins []excludeWindow) []baselineSample {
	var out []baselineSample
	for i, s := range snaps {
		if !s.DataOK || math.Abs(s.MedianReturn60s) < noisePct {
			continue
		}
		fwd, ok := forwardSnapshot300(snaps, i, s.Timestamp+forwardHorizonSeconds, forwardMatchTol)
		if !ok {
			continue
		}
		// Drop if start *or* the +5m horizon overlaps an alert window
		// (buildup before the alert must not enter the control group).
		if intervalOverlapsExclude(s.Timestamp, fwd.Timestamp, wins) {
			continue
		}
		dir := "up"
		if s.MedianReturn60s < 0 {
			dir = "down"
		}
		out = append(out, baselineSample{
			ts:        s.Timestamp,
			hour:      hourUTC(s.Timestamp),
			bucket:    magBucket(math.Abs(s.MedianReturn60s)),
			direction: dir,
			cont:      continued(dir, fwd.MedianReturn300s),
		})
	}
	return out
}

// effectiveBaselineN counts non-overlapping 5m anchors among baseline samples
// (raw N overstates independence because consecutive minutes share ~80% of the horizon).
func effectiveBaselineN(samples []baselineSample) int {
	if len(samples) == 0 {
		return 0
	}
	n := 1
	last := samples[0].ts
	for _, s := range samples[1:] {
		if s.ts-last >= forwardHorizonSeconds {
			n++
			last = s.ts
		}
	}
	return n
}

func reportLift(outcomes []outcomeRecord, events []eventRecord, snaps []snapshotRecord, noisePct float64) {
	fmt.Println("\n── 注意力 lift（experimental KPI）──")
	fmt.Println("  对照名: non-alert directional baseline（非「真随机抽样」）")
	fmt.Printf("  排除窗: 事件 [ts-%.0fs, ts+%.0fs]；对照起点→+5m 终点整段不得与排除窗相交\n", excludePreSeconds, excludePostSeconds)

	alertN, alertCont := alertContinuation(outcomes)
	if alertN == 0 {
		fmt.Println("  告警侧无 +5m 样本，无法算 lift。")
		return
	}
	alertRate := float64(alertCont) / float64(alertN)
	alo, ahi := wilsonCI(alertCont, alertN, 0.95)
	fmt.Printf("  告警 +5m 同向延续: %d/%d (%.1f%%)  Wilson95%% [%.1f%%, %.1f%%]\n",
		alertCont, alertN, 100*alertRate, 100*alo, 100*ahi)

	if len(snaps) < 2 {
		fmt.Println("  snapshot 不足：需要 data_ok 的 60s 快照序列才能建对照。")
		fmt.Println("  experimental_lift_5m: n/a")
		return
	}

	wins := alertExcludeWindows(events, outcomes)
	base := collectBaseline(snaps, noisePct, wins)
	if len(base) == 0 {
		fmt.Println("  对照无可用样本（需 data_ok、|med60|≥noise、≈+300s 前瞻，且不在排除窗内）。")
		fmt.Println("  experimental_lift_5m: n/a")
		return
	}

	var baseCont int
	for _, s := range base {
		if s.cont {
			baseCont++
		}
	}
	baseRate := float64(baseCont) / float64(len(base))
	blo, bhi := wilsonCI(baseCont, len(base), 0.95)
	effN := effectiveBaselineN(base)
	fmt.Printf("  对照延续率:         %d/%d raw (%.1f%%)  Wilson95%% [%.1f%%, %.1f%%]\n",
		baseCont, len(base), 100*baseRate, 100*blo, 100*bhi)
	fmt.Printf("  对照有效 N (5m 锚点): %d  (raw 分钟重叠，勿当独立样本)\n", effN)
	fmt.Printf("  noise floor:         %.3f%%\n", noisePct)

	if baseRate <= 0 {
		fmt.Println("  对照延续率为 0，lift 无定义。")
		fmt.Println("  experimental_lift_5m: n/a")
		return
	}

	globalLift := alertRate / baseRate
	// Crude ratio bounds from Wilson endpoints (ignores dependence; exploratory only).
	liftLo, liftHi := ratioBounds(alo, ahi, blo, bhi)
	status := evidenceStatus(alertN)
	printLiftLine("experimental_lift_5m (global)", globalLift, liftLo, liftHi, status, alertN)

	// Hour×direction stratified: E[base | hour, dir] under alert mix.
	hourLift, hourUsed, hourBaseRate := stratifiedLift(outcomes, events, base, stratHourOnly)
	if hourUsed > 0 && hourBaseRate > 0 {
		hLo, hHi := ratioBounds(alo, ahi, hourBaseRate*0.8, math.Min(1, hourBaseRate*1.2)) // soft band only
		_ = hLo
		_ = hHi
		fmt.Printf("  experimental_lift_5m (hour×dir stratified): %.2f×  [alerts with pool=%d, E[base]=%.1f%%]\n",
			hourLift, hourUsed, 100*hourBaseRate)
	} else {
		fmt.Println("  experimental_lift_5m (hour×dir stratified): n/a")
	}

	// Hour × direction × |med60| matched baseline.
	matchLift, matchUsed, matchBaseRate := stratifiedLift(outcomes, events, base, stratHourMag)
	if matchUsed > 0 && matchBaseRate > 0 {
		fmt.Printf("  experimental_lift_5m (hour×dir×|med60| matched): %.2f×  [matched alerts=%d, E[base]=%.1f%%]\n",
			matchLift, matchUsed, 100*matchBaseRate)
	} else {
		fmt.Println("  experimental_lift_5m (hour×dir×|med60| matched): n/a  (缺事件 med60 或桶内无对照)")
	}

	fmt.Println("  状态门槛: alert_n<30 exploratory · 30–99 provisional · ≥100 watch stability")
	fmt.Println("  注意: 相邻分钟对照高度重叠；CI 为独立假设下的探索区间，非正式显著性检验。")
	fmt.Println("  读法: lift≈1 → 不比「有方向的非告警分钟」更有信息；勿在 exploratory 阶段调实盘。")
}

func printLiftLine(name string, lift, lo, hi float64, status string, alertN int) {
	fmt.Printf("  %s: %.2f×  approx95%% [%.2f, %.2f]  alert_n=%d  status=%s\n",
		name, lift, lo, hi, alertN, status)
}

func evidenceStatus(alertN int) string {
	switch {
	case alertN < minAlertExploratory:
		return "insufficient_evidence"
	case alertN < minAlertProvisional:
		return "provisional"
	default:
		return "watch_stability"
	}
}

// ratioBounds maps Wilson endpoints of two rates into a crude lift interval.
func ratioBounds(aLo, aHi, bLo, bHi float64) (float64, float64) {
	if bHi <= 0 {
		return 0, math.Inf(1)
	}
	lo := aLo / bHi
	hi := math.Inf(1)
	if bLo > 0 {
		hi = aHi / bLo
	}
	return lo, hi
}

// wilsonCI is a normal-approximation Wilson score interval for a binomial proportion.
func wilsonCI(k, n int, zLevel float64) (float64, float64) {
	if n <= 0 {
		return 0, 1
	}
	// z≈1.96 for 95%.
	z := 1.959963984540054
	if zLevel != 0.95 {
		z = 1.959963984540054
	}
	p := float64(k) / float64(n)
	zn := z * z / float64(n)
	den := 1 + zn
	center := p + zn/2
	margin := z * math.Sqrt(p*(1-p)/float64(n)+zn*zn/4)
	lo := (center - margin) / den
	hi := (center + margin) / den
	if lo < 0 {
		lo = 0
	}
	if hi > 1 {
		hi = 1
	}
	return lo, hi
}

func alertContinuation(outcomes []outcomeRecord) (n, cont int) {
	for _, o := range outcomes {
		if o.MedianReturn5m == nil {
			continue
		}
		n++
		if continued(o.Direction, *o.MedianReturn5m) {
			cont++
		}
	}
	return n, cont
}

type stratMode int

const (
	stratHourOnly stratMode = iota
	stratHourMag
)

// stratifiedLift = alertRate / E[baseline rate | alert covariates].
// Uses Laplace-smoothed cell rates so zero-success hours are not dropped.
func stratifiedLift(outcomes []outcomeRecord, events []eventRecord, base []baselineSample, mode stratMode) (lift float64, used int, expectedBase float64) {
	cells := map[string]*rateCell{}
	for _, s := range base {
		key := cellKey(s.hour, s.direction, s.bucket, mode)
		c := cells[key]
		if c == nil {
			c = &rateCell{}
			cells[key] = c
		}
		c.n++
		if s.cont {
			c.cont++
		}
	}

	medByTS := map[int64]float64{}
	for _, e := range events {
		medByTS[int64(math.Round(e.Timestamp))] = e.MedianReturn60s
	}

	alertN, alertCont := 0, 0
	var sumExpected float64
	for _, o := range outcomes {
		if o.MedianReturn5m == nil || o.EventTS <= 0 {
			continue
		}
		dir := o.Direction
		if dir != "up" && dir != "down" {
			continue
		}
		h := hourUTC(o.EventTS)
		b := 0
		if mode == stratHourMag {
			med, ok := medByTS[int64(math.Round(o.EventTS))]
			if !ok {
				continue
			}
			b = magBucket(math.Abs(med))
		}
		key := cellKey(h, dir, b, mode)
		c := cells[key]
		if c == nil || c.n == 0 {
			continue
		}
		alertN++
		if continued(dir, *o.MedianReturn5m) {
			alertCont++
		}
		sumExpected += c.rateLaplace()
	}
	if alertN == 0 {
		return 0, 0, 0
	}
	expectedBase = sumExpected / float64(alertN)
	if expectedBase <= 0 {
		return 0, alertN, expectedBase
	}
	alertRate := float64(alertCont) / float64(alertN)
	return alertRate / expectedBase, alertN, expectedBase
}

// cellKey matches alerts to baseline cells by hour×direction, optionally ×|med60|.
func cellKey(hour int, direction string, bucket int, mode stratMode) string {
	if mode == stratHourOnly {
		return fmt.Sprintf("h%d_%s", hour, direction)
	}
	return fmt.Sprintf("h%d_%s_b%d", hour, direction, bucket)
}

// magBucket bins |median_return_60s| for strength-matched baselines.
func magBucket(absMed float64) int {
	switch {
	case absMed < 0.20:
		return 0 // noise–0.20 (noise floor applied separately)
	case absMed < 0.35:
		return 1
	case absMed < 0.50:
		return 2
	default:
		return 3
	}
}

func hourUTC(ts float64) int {
	return int(ts/3600) % 24
}

// forwardSnapshot300 finds the DataOK snapshot nearest targetTS (≈ +5m from t0).
func forwardSnapshot300(snaps []snapshotRecord, fromIdx int, targetTS, tol float64) (snapshotRecord, bool) {
	best := -1
	bestDelta := tol + 1
	for j := fromIdx; j < len(snaps); j++ {
		d := math.Abs(snaps[j].Timestamp - targetTS)
		if snaps[j].Timestamp > targetTS+tol {
			break
		}
		if !snaps[j].DataOK {
			continue
		}
		if d <= tol && d < bestDelta {
			bestDelta = d
			best = j
		}
	}
	if best < 0 {
		return snapshotRecord{}, false
	}
	return snaps[best], true
}

func printRate(label string, ok, n int) {
	if n == 0 {
		fmt.Printf("%s: 无样本\n", label)
		return
	}
	fmt.Printf("%s: %d/%d (%.0f%%)\n", label, ok, n, 100*float64(ok)/float64(n))
}

func pct(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Round(q * float64(len(sorted)-1)))
	return sorted[idx]
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
