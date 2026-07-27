// Command kairos-calibrate turns the MarketPulse event and outcome JSONL logs
// into the numbers needed to set thresholds from evidence instead of intuition.
//
// It joins each recorded outcome back to the event that produced it and reports
// how often an alert was followed by continued movement in the same direction,
// bucketed by how strong the triggering move was. That bucket table is the one
// that answers "where should minMedianReturnPct actually sit".
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

func main() {
	cfgPath := flag.String("config", "config.yaml", "config file used to locate the storage directory")
	eventsPath := flag.String("events", "", "override path to market-pulse-events.jsonl")
	outcomesPath := flag.String("outcomes", "", "override path to market-pulse-outcomes.jsonl")
	snapshotsPath := flag.String("snapshots", "", "override path to market-pulse-snapshots.jsonl")
	includeShadow := flag.Bool("include-shadow", true, "include events recorded in shadow mode")
	noisePct := flag.Float64("noise-pct", 0.08, "|median_return_60s| floor for random directional samples")
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
	}

	fmt.Printf("事件文件: %s (%d 条)\n", evPath, len(events))
	fmt.Printf("结果文件: %s (%d 条)\n", outPath, len(outcomes))
	fmt.Printf("快照文件: %s (%d 条)\n\n", snapPath, len(snaps))

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
	reportLift(outcomes, snaps, *noisePct)
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
}

func loadSnapshots(path string) ([]snapshotRecord, error) {
	var out []snapshotRecord
	err := eachLine(path, func(line []byte) error {
		var r snapshotRecord
		if err := json.Unmarshal(line, &r); err != nil {
			return err
		}
		out = append(out, r)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp < out[j].Timestamp })
	return out, err
}

// reportLift prints attention lift: alert 5m continuation vs random same-hour baseline.
// Random forward 5m uses MedianReturn300s at ~t0+300 (trailing 300s ending then ≈ [t0,t0+300]).
func reportLift(outcomes []outcomeRecord, snaps []snapshotRecord, noisePct float64) {
	fmt.Println("\n── 注意力 lift（主 KPI）──")
	alertN, alertCont := alertContinuation(outcomes)
	if alertN == 0 {
		fmt.Println("  告警侧无 +5m 样本，无法算 lift。")
		return
	}
	alertRate := float64(alertCont) / float64(alertN)
	fmt.Printf("  告警 +5m 同向延续: %d/%d (%.1f%%)\n", alertCont, alertN, 100*alertRate)

	if len(snaps) < 2 {
		fmt.Println("  snapshot 不足：需要 data_ok 的 60s 快照序列才能建随机对照。")
		fmt.Println("  lift_5m: n/a")
		return
	}

	randomN, randomCont := randomContinuation(snaps, noisePct)
	if randomN == 0 {
		fmt.Println("  随机对照无可用样本（需要 data_ok 且 |med60|≥noise，并存在 ≈+300s 快照）。")
		fmt.Println("  lift_5m: n/a")
		return
	}
	randomRate := float64(randomCont) / float64(randomN)
	fmt.Printf("  随机同时段对照:   %d/%d (%.1f%%)  [noise=%.3f%%]\n",
		randomCont, randomN, 100*randomRate, noisePct)

	hourLift, hourN := sameHourLift(outcomes, snaps, noisePct)
	if randomRate <= 0 {
		fmt.Println("  随机延续率为 0，lift 无定义（对照过严或样本异常）。")
		fmt.Println("  lift_5m: n/a")
		return
	}
	lift := alertRate / randomRate
	fmt.Printf("  lift_5m (全局):    %.2f×  (目标起步 >1.5×)\n", lift)
	if hourN > 0 {
		fmt.Printf("  lift_5m (同时段):  %.2f×  [按告警 UTC 小时匹配随机池, %d 个有对照的告警]\n",
			hourLift, hourN)
	} else {
		fmt.Println("  lift_5m (同时段):  n/a  (告警小时在随机池中无样本)")
	}
	fmt.Println("  读法: lift≈1 表示提醒不比随机看盘更有信息；>1.5 且日打扰在预算内再谈扩维/执行。")
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

// randomContinuation scores non-alert times: direction = sign(med60 at t0),
// forward ≈ MedianReturn300s at t0+300.
func randomContinuation(snaps []snapshotRecord, noisePct float64) (n, cont int) {
	const horizon = 300.0
	const tol = 45.0
	for i, s := range snaps {
		if !s.DataOK || math.Abs(s.MedianReturn60s) < noisePct {
			continue
		}
		fwd, ok := forwardMedian300(snaps, i, s.Timestamp+horizon, tol)
		if !ok {
			continue
		}
		dir := "up"
		if s.MedianReturn60s < 0 {
			dir = "down"
		}
		n++
		if continued(dir, fwd) {
			cont++
		}
	}
	return n, cont
}

// sameHourLift averages (alert cont / hour random cont) over alerts that have a
// random pool in the same UTC hour-of-day.
func sameHourLift(outcomes []outcomeRecord, snaps []snapshotRecord, noisePct float64) (lift float64, used int) {
	type rate struct{ n, cont int }
	byHour := map[int]*rate{}
	const horizon = 300.0
	const tol = 45.0
	for i, s := range snaps {
		if !s.DataOK || math.Abs(s.MedianReturn60s) < noisePct {
			continue
		}
		fwd, ok := forwardMedian300(snaps, i, s.Timestamp+horizon, tol)
		if !ok {
			continue
		}
		dir := "up"
		if s.MedianReturn60s < 0 {
			dir = "down"
		}
		h := hourUTC(s.Timestamp)
		r := byHour[h]
		if r == nil {
			r = &rate{}
			byHour[h] = r
		}
		r.n++
		if continued(dir, fwd) {
			r.cont++
		}
	}
	var sum float64
	for _, o := range outcomes {
		if o.MedianReturn5m == nil || o.EventTS <= 0 {
			continue
		}
		r := byHour[hourUTC(o.EventTS)]
		if r == nil || r.n == 0 || r.cont == 0 {
			// cont==0 → hour random rate 0; skip to avoid div-by-zero inflation
			continue
		}
		alertCont := 0.0
		if continued(o.Direction, *o.MedianReturn5m) {
			alertCont = 1
		}
		randomRate := float64(r.cont) / float64(r.n)
		sum += alertCont / randomRate
		used++
	}
	if used == 0 {
		return 0, 0
	}
	return sum / float64(used), used
}

func hourUTC(ts float64) int {
	return int(ts/3600) % 24
}

// forwardMedian300 finds MedianReturn300s near targetTS (≈ forward 5m from t0).
func forwardMedian300(snaps []snapshotRecord, fromIdx int, targetTS, tol float64) (float64, bool) {
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
		return 0, false
	}
	return snaps[best].MedianReturn300s, true
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
