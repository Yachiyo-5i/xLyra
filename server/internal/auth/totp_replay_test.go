package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTOTPReplayGuardRejectsReuseAndOlderCounters(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	g := newTOTPReplayGuard()
	g.now = func() time.Time { return now }
	admin := uuid.New()

	if !g.accept(admin, 100) {
		t.Fatal("first use of counter 100 should be accepted")
	}
	if g.accept(admin, 100) {
		t.Fatal("replay of the same counter must be rejected")
	}
	if g.accept(admin, 99) {
		t.Fatal("older counter must be rejected")
	}
	if !g.accept(admin, 101) {
		t.Fatal("newer counter should be accepted")
	}
	// A different admin is independent.
	if !g.accept(uuid.New(), 100) {
		t.Fatal("counter 100 for a different admin should be accepted")
	}
}

func TestTOTPReplayGuardPrunesStaleEntries(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	g := newTOTPReplayGuard()
	g.now = func() time.Time { return now }
	admin := uuid.New()

	g.accept(admin, 100)
	now = now.Add(totpReplayRetention + time.Minute)
	// After retention the entry is pruned; matchTOTPCounter would reject such an
	// old code anyway, so re-accepting the same counter is harmless.
	if !g.accept(admin, 100) {
		t.Fatal("stale entry should have been pruned")
	}
}

func TestNilTOTPReplayGuardFailsOpen(t *testing.T) {
	t.Parallel()

	var g *totpReplayGuard
	if !g.accept(uuid.New(), 1) {
		t.Fatal("nil guard must fail open")
	}
}
