package opportunity

import (
	"fmt"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// TriggerResult is a real leader-pullback / laggard-bounce detection result.
type TriggerResult struct {
	OK bool

	Entry float64
	Stop  float64

	PullbackDepthPct float64
	ReboundPct       float64

	HigherLow bool
	LowerHigh bool

	Invalidations []string
	Reasons       []string
	Failures      []string

	// EntryTriggeredAt is unix seconds of the restart (trigger) bar open/time.
	EntryTriggeredAt int64
	// PullbackAt is unix seconds of the pullback extreme bar.
	PullbackAt int64
	// ImpulseAt is unix seconds of the impulse extreme bar.
	ImpulseAt int64
}

// TriggerOpts constrains pattern timing relative to MarketPulse / session.
type TriggerOpts struct {
	// MinRestartAt: restart bar timestamp must be >= this (pulse/session time).
	MinRestartAt int64
	// MinPullbackAt: pullback extreme should be >= this when >0 (prefer in-session pullback).
	MinPullbackAt int64
	// RequireInSessionPullback: if true, fail when pullback extreme is before MinPullbackAt.
	RequireInSessionPullback bool
}

// DetectPullbackTrigger implements leader_pullback on closed bars.
//
// Long: impulse → pullback HL → restart. Short: mirror LH.
// opts.MinRestartAt enforces "after MarketPulse", not a pre-event fossil pattern.
func DetectPullbackTrigger(dir types.CycleDirection, candles []types.Candle, opts TriggerOpts) TriggerResult {
	var r TriggerResult
	if len(candles) < 25 {
		r.Failures = append(r.Failures, "insufficient_trigger_bars")
		return r
	}
	n := len(candles)
	switch dir {
	case types.CycleDirectionDown:
		r = detectShortBounce(candles[:n])
	default:
		r = detectLongPullback(candles[:n])
	}
	if !r.OK {
		return r
	}
	// normalize timestamps (ms → s)
	r.EntryTriggeredAt = normTS(r.EntryTriggeredAt)
	r.PullbackAt = normTS(r.PullbackAt)
	r.ImpulseAt = normTS(r.ImpulseAt)

	if opts.MinRestartAt > 0 && r.EntryTriggeredAt < opts.MinRestartAt {
		r.OK = false
		r.Failures = append(r.Failures, "restart_before_pulse")
		r.Reasons = nil
		return r
	}
	if opts.RequireInSessionPullback && opts.MinPullbackAt > 0 && r.PullbackAt > 0 && r.PullbackAt < opts.MinPullbackAt {
		r.OK = false
		r.Failures = append(r.Failures, "pullback_before_session")
		r.Reasons = nil
		return r
	}
	return r
}

func normTS(ts int64) int64 {
	if ts > 1_000_000_000_000 {
		return ts / 1000
	}
	return ts
}

func detectLongPullback(c []types.Candle) TriggerResult {
	var r TriggerResult
	n := len(c)
	lo, hi := n-30, n-8
	if lo < 1 {
		lo = 1
	}
	if hi <= lo+3 {
		r.Failures = append(r.Failures, "window_too_small")
		return r
	}
	peakIdx := lo
	for i := lo; i <= hi; i++ {
		if c[i].High >= c[peakIdx].High {
			peakIdx = i
		}
	}
	troughIdx := lo
	for i := lo; i < peakIdx; i++ {
		if c[i].Low <= c[troughIdx].Low {
			troughIdx = i
		}
	}
	peak := c[peakIdx].High
	trough := c[troughIdx].Low
	if trough <= 0 || peak <= trough {
		r.Failures = append(r.Failures, "no_impulse_up")
		return r
	}
	impulsePct := (peak - trough) / trough * 100
	if impulsePct < 1.2 {
		r.Failures = append(r.Failures, "impulse_too_weak")
		return r
	}
	r.Reasons = append(r.Reasons, fmt.Sprintf("impulse_up %.2f%%", impulsePct))
	r.ImpulseAt = c[peakIdx].Timestamp

	if peakIdx >= n-3 {
		r.Failures = append(r.Failures, "no_room_for_pullback")
		return r
	}
	pbLowIdx := peakIdx + 1
	for i := peakIdx + 1; i < n-2; i++ {
		if c[i].Low <= c[pbLowIdx].Low {
			pbLowIdx = i
		}
	}
	pbLow := c[pbLowIdx].Low
	if pbLow >= peak {
		r.Failures = append(r.Failures, "no_pullback")
		return r
	}
	if pbLow < trough*0.999 {
		r.Failures = append(r.Failures, "pullback_broke_structure")
		return r
	}
	if pbLow > trough {
		r.HigherLow = true
		r.Reasons = append(r.Reasons, "higher_low")
	} else {
		r.Failures = append(r.Failures, "no_higher_low")
		return r
	}
	r.PullbackAt = c[pbLowIdx].Timestamp
	r.PullbackDepthPct = (peak - pbLow) / peak * 100
	if r.PullbackDepthPct < 0.3 {
		r.Failures = append(r.Failures, "pullback_too_shallow_noise")
		return r
	}
	if r.PullbackDepthPct > 12 {
		r.Failures = append(r.Failures, "pullback_too_deep")
		return r
	}
	r.Reasons = append(r.Reasons, fmt.Sprintf("pullback_depth %.2f%%", r.PullbackDepthPct))

	last := c[n-1]
	mid := (peak + pbLow) / 2
	if last.Close <= pbLow {
		r.Failures = append(r.Failures, "no_restart_still_on_low")
		return r
	}
	prior := c[n-2].Close
	if last.Close < mid && last.Close <= prior {
		r.Failures = append(r.Failures, "no_restart_up")
		return r
	}
	r.Reasons = append(r.Reasons, "restart_up")
	if last.Close > peak*1.02 {
		r.Failures = append(r.Failures, "too_extended_past_peak")
		return r
	}

	r.OK = true
	r.Entry = last.Close
	r.Stop = pbLow
	if r.Stop >= r.Entry {
		r.Stop = r.Entry * 0.985
	}
	r.Invalidations = []string{
		fmt.Sprintf("5m pullback low %.6g breaks", r.Stop),
		"15m flips DOWN with structure break",
	}
	r.EntryTriggeredAt = last.Timestamp
	return r
}

func detectShortBounce(c []types.Candle) TriggerResult {
	var r TriggerResult
	n := len(c)
	lo, hi := n-30, n-8
	if lo < 1 {
		lo = 1
	}
	if hi <= lo+3 {
		r.Failures = append(r.Failures, "window_too_small")
		return r
	}
	troughIdx := lo
	for i := lo; i <= hi; i++ {
		if c[i].Low <= c[troughIdx].Low {
			troughIdx = i
		}
	}
	peakIdx := lo
	for i := lo; i < troughIdx; i++ {
		if c[i].High >= c[peakIdx].High {
			peakIdx = i
		}
	}
	trough := c[troughIdx].Low
	peak := c[peakIdx].High
	if peak <= 0 || trough >= peak {
		r.Failures = append(r.Failures, "no_impulse_down")
		return r
	}
	impulsePct := (peak - trough) / peak * 100
	if impulsePct < 1.2 {
		r.Failures = append(r.Failures, "impulse_too_weak")
		return r
	}
	r.Reasons = append(r.Reasons, fmt.Sprintf("impulse_down %.2f%%", impulsePct))
	r.ImpulseAt = c[troughIdx].Timestamp

	if troughIdx >= n-3 {
		r.Failures = append(r.Failures, "no_room_for_bounce")
		return r
	}
	bhIdx := troughIdx + 1
	for i := troughIdx + 1; i < n-2; i++ {
		if c[i].High >= c[bhIdx].High {
			bhIdx = i
		}
	}
	bounceHigh := c[bhIdx].High
	if bounceHigh <= trough {
		r.Failures = append(r.Failures, "no_bounce")
		return r
	}
	if bounceHigh > peak*1.001 {
		r.Failures = append(r.Failures, "bounce_broke_structure")
		return r
	}
	if bounceHigh < peak {
		r.LowerHigh = true
		r.Reasons = append(r.Reasons, "lower_high")
	} else {
		r.Failures = append(r.Failures, "no_lower_high")
		return r
	}
	r.PullbackAt = c[bhIdx].Timestamp
	r.ReboundPct = (bounceHigh - trough) / trough * 100
	if r.ReboundPct < 0.3 {
		r.Failures = append(r.Failures, "bounce_too_shallow_noise")
		return r
	}
	if r.ReboundPct > 12 {
		r.Failures = append(r.Failures, "bounce_too_deep")
		return r
	}
	r.Reasons = append(r.Reasons, fmt.Sprintf("bounce_depth %.2f%%", r.ReboundPct))

	last := c[n-1]
	mid := (trough + bounceHigh) / 2
	prior := c[n-2].Close
	if last.Close >= bounceHigh {
		r.Failures = append(r.Failures, "no_restart_still_on_high")
		return r
	}
	if last.Close > mid && last.Close >= prior {
		r.Failures = append(r.Failures, "no_restart_down")
		return r
	}
	r.Reasons = append(r.Reasons, "restart_down")
	if last.Close < trough*0.98 {
		r.Failures = append(r.Failures, "too_extended_past_trough")
		return r
	}

	r.OK = true
	r.Entry = last.Close
	r.Stop = bounceHigh
	if r.Stop <= r.Entry {
		r.Stop = r.Entry * 1.015
	}
	r.Invalidations = []string{
		fmt.Sprintf("5m bounce high %.6g breaks", r.Stop),
		"15m flips UP with structure break",
	}
	r.EntryTriggeredAt = last.Timestamp
	return r
}
