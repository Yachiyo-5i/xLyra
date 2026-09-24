package oauth

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

func TestQuotaWindowResetDetected(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	resetAt := now.Add(-time.Minute)
	nextReset := now.Add(5 * time.Hour)

	current := store.OAuthQuotaWindowRound{
		LatestRemainingPercent: nullFloatFromPtr(floatPtr(12)),
		LatestResetAt:          nullTimeFromPtr(&resetAt),
	}
	observation := quotaWindowObservation{
		RemainingPercent: floatPtr(100),
		ResetAt:          &nextReset,
		Present:          true,
	}
	if !quotaWindowResetDetected(current, observation, now) {
		t.Fatal("expected reset when remaining refills past threshold and reset_at advances")
	}

	observation.RemainingPercent = floatPtr(15)
	if quotaWindowResetDetected(current, observation, now) {
		t.Fatal("small remaining increase should not count as reset")
	}
}

func TestQuotaUsedPercentDelta(t *testing.T) {
	t.Parallel()

	current := store.OAuthQuotaWindowRound{
		LatestUsedPercent:      nullFloatFromPtr(floatPtr(20)),
		LatestRemainingPercent: nullFloatFromPtr(floatPtr(80)),
	}
	got := quotaUsedPercentDelta(current, quotaWindowObservation{
		UsedPercent:      floatPtr(25),
		RemainingPercent: floatPtr(75),
		Present:          true,
	})
	if got != 5 {
		t.Fatalf("used delta = %v, want 5", got)
	}
}

func TestQuotaWindowViewIncludesPreviousTotal(t *testing.T) {
	t.Parallel()

	started := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	updated := started.Add(2 * time.Hour)
	current := store.OAuthQuotaWindowRound{
		StartedAt:              started,
		UpdatedAt:              updated,
		LatestRemainingPercent: nullFloatFromPtr(floatPtr(50)),
		EstimatedTotal:         nullFloatFromPtr(floatPtr(20)),
		CumulativeSystemCost:   10,
		Confidence:             store.OAuthQuotaConfidenceReady,
		Currency:               "USD",
	}
	previous := store.OAuthQuotaWindowRound{
		ID:             uuid.New(),
		EstimatedTotal: nullFloatFromPtr(floatPtr(18)),
	}
	view := quotaWindowView(current, previous)
	if view.EstimatedTotal == nil || *view.EstimatedTotal != 20 {
		t.Fatalf("estimated total = %#v", view.EstimatedTotal)
	}
	if view.EstimatedRemaining == nil || *view.EstimatedRemaining != 10 {
		t.Fatalf("estimated remaining = %#v", view.EstimatedRemaining)
	}
	if view.PreviousTotal == nil || *view.PreviousTotal != 18 {
		t.Fatalf("previous total = %#v", view.PreviousTotal)
	}
	if view.SpendRatePerHour == nil || *view.SpendRatePerHour != 5 {
		t.Fatalf("spend rate = %#v, want 5", view.SpendRatePerHour)
	}
}

func floatPtr(value float64) *float64 { return &value }
