package auth

import (
	"testing"
	"time"
)

func newTestLoginGuard(clock *time.Time) *loginGuard {
	g := newLoginGuard()
	g.now = func() time.Time { return *clock }
	return g
}

func TestLoginGuardLocksAfterMaxFailures(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	g := newTestLoginGuard(&now)
	key := loginAttemptKey("10.0.0.1:5555", "admin")

	for i := 0; i < loginMaxFailures-1; i++ {
		if d, locked := g.recordFailure(key); locked {
			t.Fatalf("locked too early after %d failures (d=%s)", i+1, d)
		}
	}
	if _, locked := g.retryAfter(key); locked {
		t.Fatal("must not be locked before threshold")
	}

	d, locked := g.recordFailure(key)
	if !locked || d <= 0 {
		t.Fatalf("expected lockout at threshold, got locked=%v d=%s", locked, d)
	}
	if _, stillLocked := g.retryAfter(key); !stillLocked {
		t.Fatal("retryAfter must report locked immediately after lockout")
	}
}

func TestLoginGuardUnlocksAfterLockoutExpires(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	g := newTestLoginGuard(&now)
	key := loginAttemptKey("10.0.0.2:1", "admin")

	var lockout time.Duration
	for i := 0; i < loginMaxFailures; i++ {
		lockout, _ = g.recordFailure(key)
	}
	if lockout <= 0 {
		t.Fatal("expected a positive lockout duration")
	}

	// Just before expiry: still locked.
	now = now.Add(lockout - time.Second)
	if _, locked := g.retryAfter(key); !locked {
		t.Fatal("should still be locked before lockout expiry")
	}
	// After expiry: no longer locked.
	now = now.Add(2 * time.Second)
	if d, locked := g.retryAfter(key); locked {
		t.Fatalf("should be unlocked after expiry, got d=%s", d)
	}
}

func TestLoginGuardResetClearsFailures(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	g := newTestLoginGuard(&now)
	key := loginAttemptKey("10.0.0.3:9", "admin")

	for i := 0; i < loginMaxFailures-1; i++ {
		g.recordFailure(key)
	}
	g.reset(key)

	// After reset a fresh run of failures below the threshold must not lock.
	for i := 0; i < loginMaxFailures-1; i++ {
		if _, locked := g.recordFailure(key); locked {
			t.Fatalf("reset did not clear failure count (locked at %d)", i+1)
		}
	}
}

func TestLoginGuardWindowExpiryPreventsAccumulation(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	g := newTestLoginGuard(&now)
	key := loginAttemptKey("10.0.0.4:9", "admin")

	// Failures spread further apart than the window must never accumulate to a lockout.
	for i := 0; i < loginMaxFailures*3; i++ {
		if _, locked := g.recordFailure(key); locked {
			t.Fatalf("slow failures should not lock (i=%d)", i)
		}
		now = now.Add(loginFailureWindow + time.Second)
	}
}

func TestLoginGuardBackoffGrowsOnRepeatedLockouts(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	g := newTestLoginGuard(&now)
	key := loginAttemptKey("10.0.0.5:9", "admin")

	lockFor := func() time.Duration {
		var d time.Duration
		for i := 0; i < loginMaxFailures; i++ {
			d, _ = g.recordFailure(key)
		}
		return d
	}

	first := lockFor()
	now = now.Add(first + time.Second) // wait out the first lockout
	second := lockFor()
	if second <= first {
		t.Fatalf("expected exponential backoff to grow: first=%s second=%s", first, second)
	}
	if second > loginLockoutMax {
		t.Fatalf("lockout exceeded cap: %s > %s", second, loginLockoutMax)
	}
}

func TestLoginAttemptKeyNormalizesHostAndUsername(t *testing.T) {
	t.Parallel()

	withPort := loginAttemptKey("192.168.0.1:4444", "Admin")
	noPort := loginAttemptKey(" 192.168.0.1 ", "admin")
	if withPort != noPort {
		t.Fatalf("keys should match after normalization: %q vs %q", withPort, noPort)
	}
	if same := loginAttemptKey("192.168.0.2:1", "admin"); same == noPort {
		t.Fatal("different IPs must produce different keys")
	}
}
