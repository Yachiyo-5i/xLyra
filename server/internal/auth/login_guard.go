package auth

import (
	"net"
	"strings"
	"sync"
	"time"
)

const (
	loginMaxFailures     = 5
	loginFailureWindow   = 15 * time.Minute
	loginLockoutBase     = 1 * time.Minute
	loginLockoutMax      = 30 * time.Minute
	loginGuardMaxEntries = 20000
)

// LockoutError is returned by Login when an IP+username pair has failed too many
// times and is temporarily locked out. RetryAfter carries the remaining wait.
type LockoutError struct {
	RetryAfter time.Duration
}

func (e *LockoutError) Error() string {
	return "too many failed login attempts"
}

type loginAttemptState struct {
	failures    int
	windowStart time.Time
	lockedUntil time.Time
	lockCount   int
}

// loginGuard is an in-memory brute-force guard keyed by IP+username. It counts
// failed attempts within a sliding window and locks the key with exponential
// backoff once the threshold is crossed, throttling both password guessing and
// online TOTP enumeration. A nil guard disables limiting (fail open).
type loginGuard struct {
	mu          sync.Mutex
	entries     map[string]*loginAttemptState
	maxFailures int
	window      time.Duration
	lockoutBase time.Duration
	lockoutMax  time.Duration
	now         func() time.Time
}

func newLoginGuard() *loginGuard {
	return &loginGuard{
		entries:     make(map[string]*loginAttemptState),
		maxFailures: loginMaxFailures,
		window:      loginFailureWindow,
		lockoutBase: loginLockoutBase,
		lockoutMax:  loginLockoutMax,
		now:         time.Now,
	}
}

// retryAfter reports the remaining lockout for key, if it is currently locked.
func (g *loginGuard) retryAfter(key string) (time.Duration, bool) {
	if g == nil || key == "" {
		return 0, false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	st, ok := g.entries[key]
	if !ok {
		return 0, false
	}
	now := g.now()
	if now.Before(st.lockedUntil) {
		return st.lockedUntil.Sub(now), true
	}
	return 0, false
}

// recordFailure counts a failed attempt for key and locks it once the failure
// threshold is reached within the window. It returns the lockout duration when
// the attempt tips the key into (or is already under) a lockout.
func (g *loginGuard) recordFailure(key string) (time.Duration, bool) {
	if g == nil || key == "" {
		return 0, false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	g.sweepLocked(now)
	st, ok := g.entries[key]
	if !ok {
		st = &loginAttemptState{windowStart: now}
		g.entries[key] = st
	}
	if now.Before(st.lockedUntil) {
		return st.lockedUntil.Sub(now), true
	}
	if now.Sub(st.windowStart) > g.window {
		st.failures = 0
		st.windowStart = now
	}
	st.failures++
	if st.failures >= g.maxFailures {
		lockout := g.lockoutBase << st.lockCount
		if lockout <= 0 || lockout > g.lockoutMax {
			lockout = g.lockoutMax
		}
		st.lockedUntil = now.Add(lockout)
		st.lockCount++
		st.failures = 0
		st.windowStart = now
		return lockout, true
	}
	return 0, false
}

// reset clears the failure state for key after a successful login.
func (g *loginGuard) reset(key string) {
	if g == nil || key == "" {
		return
	}
	g.mu.Lock()
	delete(g.entries, key)
	g.mu.Unlock()
}

// sweepLocked drops fully-expired entries when the map grows past its cap so a
// spray of distinct IP+username pairs cannot grow it without bound. Caller holds mu.
func (g *loginGuard) sweepLocked(now time.Time) {
	if len(g.entries) < loginGuardMaxEntries {
		return
	}
	for k, st := range g.entries {
		if now.After(st.lockedUntil) && now.Sub(st.windowStart) > g.window {
			delete(g.entries, k)
		}
	}
}

// loginAttemptKey derives the guard key from the remote address and username.
func loginAttemptKey(remoteAddr string, username string) string {
	ip := strings.TrimSpace(remoteAddr)
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}
	return strings.ToLower(ip) + "\x00" + strings.ToLower(strings.TrimSpace(username))
}
