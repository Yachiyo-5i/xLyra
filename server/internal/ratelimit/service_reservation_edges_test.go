package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

func TestAcquireReturnsNoReservationForNilServiceOrAPIKey(t *testing.T) {
	t.Parallel()

	if reservation, err := (*Service)(nil).Acquire(context.Background(), AcquireInput{APIKeyID: uuid.New()}); err != nil || reservation != nil {
		t.Fatalf("nil service Acquire = (%#v, %v), want nil nil", reservation, err)
	}

	service := NewService(nil)
	if reservation, err := service.Acquire(context.Background(), AcquireInput{APIKeyID: uuid.Nil}); err != nil || reservation != nil {
		t.Fatalf("nil key Acquire = (%#v, %v), want nil nil", reservation, err)
	}

	if reservation, err := (&Service{db: &store.Store{}}).Acquire(context.Background(), AcquireInput{
		APIKeyID:    uuid.New(),
		RequestedAt: time.Date(2026, 6, 22, 9, 10, 11, 0, time.UTC),
		EstimateTPM: true,
		Payload:     map[string]any{"prompt": "hello", "max_tokens": int64(8)},
	}); err != nil || reservation != nil {
		t.Fatalf("uninitialized store Acquire = (%#v, %v), want nil nil", reservation, err)
	}
}

func TestSettleIgnoresNilAndEmptyReservations(t *testing.T) {
	t.Parallel()

	service := NewService(nil)
	if err := service.Settle(context.Background(), nil, 12); err != nil {
		t.Fatalf("Settle nil reservation returned error: %v", err)
	}
	if err := service.Settle(context.Background(), &Reservation{Scopes: nil}, -5); err != nil {
		t.Fatalf("Settle empty reservation returned error: %v", err)
	}
}

func TestReservationMetadataIncludesNilLimitsAndActualTokens(t *testing.T) {
	t.Parallel()

	windowStart := reservationEdgesWindowStart()
	reservation := &Reservation{
		WindowStart:     windowStart,
		EstimatedTokens: 20,
		ReservedTokens:  15,
		Scopes: []ReservationScope{
			{
				Scope:          store.RateLimitScopeGlobal,
				ScopeKey:       "global",
				RPMUsed:        3,
				TPMReserved:    7,
				TPMActual:      11,
				ReservedTokens: 15,
			},
		},
	}

	metadata := reservation.Metadata(9)
	assertReservationMetadataTopLevel(t, metadata, 20, 15, 9, windowStart.Format(time.RFC3339))
	scope := requireReservationMetadataScope(t, metadata)
	if scope["rpm_limit"] != nil || scope["tpm_limit"] != nil {
		t.Fatalf("nil limits metadata = %#v", scope)
	}
	if (*Reservation)(nil).Metadata(1) != nil {
		t.Fatal("nil reservation metadata should be nil")
	}
}

func reservationEdgesWindowStart() time.Time {
	return time.Date(2026, 6, 23, 12, 34, 0, 0, time.UTC)
}
