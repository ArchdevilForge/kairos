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

// Every real adapter must expose feed liveness, otherwise the pipeline watchdog
// silently falls back to consumer-side guessing for that exchange.
var (
	_ FeedStatus = (*okxExchange)(nil)
	_ FeedStatus = (*binanceExchange)(nil)
	_ FeedStatus = (*bybitExchange)(nil)
)

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

// markDisconnected clears the subscription when the socket ends. Without it a
// dropped feed keeps reporting subscribed, and the watchdog would report "feed
// silent" over the connect/retry WARNs instead of letting them own that state.
func (f *feedState) markDisconnected() {
	f.mu.Lock()
	f.subAt = time.Time{}
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
