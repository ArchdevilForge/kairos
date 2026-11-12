package exchange

import (
	"sync"
	"time"
)

// FeedStats is a point-in-time view of one ticker feed, used by the pipeline's
// silent-feed watchdog to tell "never connected" apart from "connected but
// silent". Both look identical from the consumer side, and only the second one
// is a feed failure.
type FeedStats struct {
	Subscribed    bool
	SubscribedAt  time.Time
	LastMessageAt time.Time
}

// FeedStatus is implemented by adapters that can report ticker-feed liveness.
type FeedStatus interface {
	FeedStats() FeedStats
}

// feedState is the shared, race-safe liveness record embedded by adapters.
type feedState struct {
	mu      sync.Mutex
	subAt   time.Time
	lastMsg time.Time
}

func (f *feedState) markSubscribed(now time.Time) {
	f.mu.Lock()
	f.subAt = now
	f.lastMsg = time.Time{}
	f.mu.Unlock()
}

func (f *feedState) markMessage(now time.Time) {
	f.mu.Lock()
	f.lastMsg = now
	f.mu.Unlock()
}

func (f *feedState) stats() FeedStats {
	f.mu.Lock()
	defer f.mu.Unlock()
	return FeedStats{
		Subscribed:    !f.subAt.IsZero(),
		SubscribedAt:  f.subAt,
		LastMessageAt: f.lastMsg,
	}
}
