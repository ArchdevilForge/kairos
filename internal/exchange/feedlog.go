package exchange

import (
	"log/slog"
	"sync"
	"time"
)

// feedLog reports WebSocket feed lifecycle for one exchange. Repeated failures
// are rate-limited per key so a reconnect loop shows up in the log instead of
// flooding it, and the repeats it swallowed are reported on the next line.
type feedLog struct {
	log    *slog.Logger
	minGap time.Duration
	now    func() time.Time

	mu         sync.Mutex
	lastWarn   map[string]time.Time
	suppressed map[string]int
}

func newFeedLog(exchange string) *feedLog {
	return &feedLog{
		log:        slog.Default().With("component", "exchange_feed", "exchange", exchange),
		minGap:     time.Minute,
		now:        time.Now,
		lastWarn:   make(map[string]time.Time),
		suppressed: make(map[string]int),
	}
}

// Info always logs at INFO.
func (f *feedLog) Info(msg string, args ...any) {
	f.log.Info(msg, args...)
}

// Warn logs at WARN, at most once per key per minGap. Suppressed repeats are
// counted (never fully silent) and attached to the next emitted line.
func (f *feedLog) Warn(key, msg string, args ...any) {
	f.mu.Lock()
	now := f.now()
	if last, ok := f.lastWarn[key]; ok && now.Sub(last) < f.minGap {
		f.suppressed[key]++
		f.mu.Unlock()
		return
	}
	f.lastWarn[key] = now
	repeats := f.suppressed[key]
	f.suppressed[key] = 0
	f.mu.Unlock()

	if repeats > 0 {
		args = append(args, "suppressed_repeats", repeats)
	}
	f.log.Warn(msg, args...)
}
