package auth

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// A TOTP code is valid across a small window (±1 time-step); retaining the last
// accepted counter for a few minutes is enough to reject reuse, since a code
// older than the window is rejected by matchTOTPCounter anyway.
const totpReplayRetention = 5 * time.Minute

type totpReplayEntry struct {
	counter int64
	seenAt  time.Time
}

// totpReplayGuard remembers the most recent successfully-used TOTP time-step per
// admin so the same (or an older) code cannot be replayed within its validity
// window.
type totpReplayGuard struct {
	mu   sync.Mutex
	used map[uuid.UUID]totpReplayEntry
	now  func() time.Time
}

func newTOTPReplayGuard() *totpReplayGuard {
	return &totpReplayGuard{used: map[uuid.UUID]totpReplayEntry{}, now: time.Now}
}

// accept reports whether a successful verification at the given time-step counter
// is fresh for admin, recording it when so. A counter at or below the last
// accepted one is a replay and is rejected. A nil guard fails open (a Service
// built without the constructor, e.g. in tests).
func (g *totpReplayGuard) accept(adminID uuid.UUID, counter int64) bool {
	if g == nil {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	g.pruneLocked(now)
	if entry, ok := g.used[adminID]; ok && counter <= entry.counter {
		return false
	}
	g.used[adminID] = totpReplayEntry{counter: counter, seenAt: now}
	return true
}

func (g *totpReplayGuard) pruneLocked(now time.Time) {
	for id, entry := range g.used {
		if now.Sub(entry.seenAt) > totpReplayRetention {
			delete(g.used, id)
		}
	}
}
