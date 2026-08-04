package ratelimit

import (
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"xlyra/server/internal/store"
)

func TestOutputTokenReservationFallsThroughToLaterLimitKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload map[string]any
		want    int64
	}{
		{
			name: "max completion after invalid max tokens",
			payload: map[string]any{
				"max_tokens":            "bad",
				"max_completion_tokens": json.Number("77"),
				"max_output_tokens":     int64(88),
			},
			want: 77,
		},
		{
			name: "max output after non-positive earlier keys",
			payload: map[string]any{
				"max_tokens":            int64(0),
				"max_completion_tokens": int64(-1),
				"max_output_tokens":     float64(12),
			},
			want: 12,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := outputTokenReservation(tt.payload); got != tt.want {
				t.Fatalf("outputTokenReservation() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestReservationMetadataKeepsEmptyScopeResults(t *testing.T) {
	t.Parallel()

	reservation := &Reservation{
		WindowStart:     time.Date(2026, 6, 22, 9, 10, 0, 0, time.UTC),
		EstimatedTokens: 5,
		ReservedTokens:  0,
	}

	meta := reservation.Metadata(3)
	scopes := requireReservationMetadataScopes(t, meta)
	if len(scopes) != 0 {
		t.Fatalf("scope_results length = %d, want 0", len(scopes))
	}
}

func TestReservationMetadataIncludesWindowAndAllScopeCounters(t *testing.T) {
	t.Parallel()

	rpmLimit := int64(3)
	tpmLimit := int64(90)
	reservation := &Reservation{
		WindowStart:     time.Date(2026, 6, 22, 9, 10, 0, 0, time.UTC),
		EstimatedTokens: 45,
		ReservedTokens:  45,
		Scopes: []ReservationScope{
			{
				Scope:          store.RateLimitScopeGlobal,
				ScopeKey:       "global",
				RPMLimit:       &rpmLimit,
				TPMLimit:       &tpmLimit,
				RPMUsed:        2,
				TPMReserved:    40,
				TPMActual:      5,
				ReservedTokens: 45,
			},
		},
	}

	meta := reservation.Metadata(44)
	assertReservationMetadataTopLevel(t, meta, 45, 45, 44, "2026-06-22T09:10:00Z")

	scope := requireReservationMetadataScope(t, meta)
	want := map[string]any{
		"scope":           store.RateLimitScopeGlobal,
		"scope_key":       "global",
		"rpm_limit":       int64(3),
		"tpm_limit":       int64(90),
		"rpm_used":        int64(2),
		"tpm_reserved":    int64(40),
		"tpm_actual":      int64(5),
		"reserved_tokens": int64(45),
	}
	for key, wantValue := range want {
		if scope[key] != wantValue {
			t.Fatalf("scope[%q] = %#v, want %#v in %#v", key, scope[key], wantValue, scope)
		}
	}
}

func assertReservationMetadataTopLevel(t *testing.T, meta map[string]any, estimatedTokens int64, reservedTokens int64, actualTokens int64, windowStart string) {
	t.Helper()

	if meta["estimated_tokens"] != estimatedTokens ||
		meta["reserved_tokens"] != reservedTokens ||
		meta["actual_tokens"] != actualTokens ||
		meta["window_start"] != windowStart {
		t.Fatalf("unexpected top-level metadata: %#v", meta)
	}
}

func requireReservationMetadataScope(t *testing.T, meta map[string]any) map[string]any {
	t.Helper()

	scopes := requireReservationMetadataScopes(t, meta)
	if len(scopes) != 1 {
		t.Fatalf("scope_results = %#v, want one scope map", meta["scope_results"])
	}
	return scopes[0]
}

func requireReservationMetadataScopes(t *testing.T, meta map[string]any) []map[string]any {
	t.Helper()

	scopes, ok := meta["scope_results"].([]map[string]any)
	if !ok {
		t.Fatalf("scope_results = %#v, want []map[string]any", meta["scope_results"])
	}
	return scopes
}

func TestLimitErrorIsRejectsUnrelatedTargets(t *testing.T) {
	t.Parallel()

	target := errors.New("other error")
	if (LimitError{}).Is(target) {
		t.Fatal("LimitError.Is returned true for an unrelated target")
	}
	if errors.Is(LimitError{}, target) {
		t.Fatal("errors.Is matched LimitError against an unrelated target")
	}
}

func TestLimitErrorDefaultMessageStillMatchesSentinel(t *testing.T) {
	t.Parallel()

	err := LimitError{
		Scope:             store.RateLimitScopeGlobal,
		ScopeKey:          "global",
		LimitType:         "tpm",
		RetryAfterSeconds: 7,
		Message:           "   ",
	}
	if err.Error() != ErrLimited.Error() {
		t.Fatalf("LimitError.Error() = %q, want %q", err.Error(), ErrLimited.Error())
	}
	if !errors.Is(err, ErrLimited) {
		t.Fatal("LimitError should match ErrLimited sentinel")
	}
}

func TestNullInt64PointerDistinguishesValidZero(t *testing.T) {
	t.Parallel()

	value := nullInt64Pointer(sql.NullInt64{Int64: 0, Valid: true})
	if value == nil {
		t.Fatal("valid zero NullInt64 returned nil pointer")
	}
	if *value != 0 {
		t.Fatalf("valid zero NullInt64 pointer = %d, want 0", *value)
	}

	if value := nullInt64Pointer(sql.NullInt64{Int64: 99, Valid: false}); value != nil {
		t.Fatalf("invalid NullInt64 pointer = %#v, want nil", value)
	}
}
