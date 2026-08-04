package store

import (
	"testing"
	"time"
)

func TestAdminSessionActiveIncludesNeverExpiringSessions(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	if !adminSessionActive(AdminSession{}, now) {
		t.Fatal("expected session without expires_at to be active")
	}

	future := now.Add(time.Hour)
	if !adminSessionActive(AdminSession{ExpiresAt: &future}, now) {
		t.Fatal("expected future session to be active")
	}

	exact := now
	if adminSessionActive(AdminSession{ExpiresAt: &exact}, now) {
		t.Fatal("expected session expiring exactly now to be inactive")
	}

	past := now.Add(-time.Hour)
	if adminSessionActive(AdminSession{ExpiresAt: &past}, now) {
		t.Fatal("expected expired session to be inactive")
	}
}
