package store

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	// last_used_at is written at most once per key per interval on the
	// authenticated hot path, trading a slightly stale timestamp for far fewer
	// row updates and less lock contention.
	apiKeyLastUsedInterval   = time.Minute
	apiKeyLastUsedMaxEntries = 50000
)

var apiKeyLastUsed = &lastUsedThrottle{seen: map[uuid.UUID]time.Time{}}

type lastUsedThrottle struct {
	mu   sync.Mutex
	seen map[uuid.UUID]time.Time
}

// shouldWrite reports whether last_used_at for id should be persisted now,
// allowing at most one write per apiKeyLastUsedInterval. The map is pruned of
// stale entries once it grows past a cap so it stays bounded.
func (t *lastUsedThrottle) shouldWrite(id uuid.UUID, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if last, ok := t.seen[id]; ok && now.Sub(last) >= 0 && now.Sub(last) < apiKeyLastUsedInterval {
		return false
	}
	if len(t.seen) >= apiKeyLastUsedMaxEntries {
		for k, v := range t.seen {
			if now.Sub(v) >= apiKeyLastUsedInterval {
				delete(t.seen, k)
			}
		}
	}
	t.seen[id] = now
	return true
}
