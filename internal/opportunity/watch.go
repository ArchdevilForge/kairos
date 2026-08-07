package opportunity

import (
	"context"
	"time"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// WatchAndEnrich keeps re-checking for a post-pulse pullback until tickets appear,
// session TTL elapses, or ctx is cancelled.
//
// This is the MarketPulse → wait for future pullback → trigger chain.
func (s *Service) WatchAndEnrich(ctx context.Context, req EnrichRequest) {
	if s == nil || !s.cfg.Enabled || req.Fetcher == nil {
		return
	}
	cfg := req.Config
	if cfg.WatchInterval <= 0 {
		cfg.WatchInterval = 5 * time.Minute
		req.Config = cfg
	}
	ttl := time.Duration(s.cfg.SessionTTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = time.Hour
	}
	deadline := time.Now().Add(ttl)

	try := func() bool {
		res, err := s.EnrichAndEvaluate(ctx, req)
		if err != nil {
			s.log.Warn("watch enrich failed", "error", err)
			return false
		}
		if len(res.Tickets) > 0 {
			s.log.Info("watch enrich produced tickets",
				"session", res.Session.ID, "tickets", len(res.Tickets))
			return true
		}
		return false
	}

	// immediate attempt (may fail if pullback not yet formed — correct)
	if try() {
		return
	}

	t := time.NewTicker(cfg.WatchInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if time.Now().After(deadline) {
				s.log.Info("watch enrich expired", "event", req.Event.EventID)
				// mark session expired if present
				if s.journal != nil && req.Event.EventID != "" {
					s.mu.Lock()
					sid := s.openEventIDs[req.Event.EventID]
					s.mu.Unlock()
					if sid != "" {
						_ = s.journal.SaveSession(types.OpportunitySession{
							SchemaVersion:  types.OpportunitySessionSchemaVersion,
							ID:             sid,
							EventID:        req.Event.EventID,
							Status:         types.OpportunitySessionExpired,
							PulseDirection: pulseDirectionFromEvent(req.Event),
							PulseState:     pulseStateFromEvent(req.Event),
							CreatedAt:      int64(req.Event.Timestamp),
							ExpiresAt:      time.Now().Unix(),
						})
					}
				}
				return
			}
			if try() {
				return
			}
		}
	}
}
