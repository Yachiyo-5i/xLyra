package site

import (
	"sync"

	"github.com/google/uuid"
)

// siteRefreshLocks serializes state- and model-mutating refresh operations per
// site. Full-site refresh, single-key refresh and key enable/disable each
// recompute site-model availability via MarkUnavailableExcept over the whole
// site; without a per-site lock two concurrent runs interleave their sweeps and
// leave models wrongly (un)available. It serializes rather than de-duplicates
// (unlike singleflight) because these are distinct operations with side effects.
type siteRefreshLocks struct {
	mu    sync.Mutex
	locks map[uuid.UUID]*sync.Mutex
}

func newSiteRefreshLocks() *siteRefreshLocks {
	return &siteRefreshLocks{locks: make(map[uuid.UUID]*sync.Mutex)}
}

// lock acquires the per-site mutex and returns the release function. A nil
// receiver (a Service built without the constructor, e.g. in tests) is a no-op.
func (l *siteRefreshLocks) lock(siteID uuid.UUID) func() {
	if l == nil {
		return func() {}
	}
	l.mu.Lock()
	m, ok := l.locks[siteID]
	if !ok {
		m = &sync.Mutex{}
		l.locks[siteID] = m
	}
	l.mu.Unlock()

	m.Lock()
	return m.Unlock
}
