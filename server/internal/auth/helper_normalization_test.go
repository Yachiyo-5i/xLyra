package auth

import (
	"database/sql"
	"net/http"
	"testing"
	"time"

	"xlyra/server/internal/store"
)

func TestNormalizeAPIKeyPolicyBoundaryValues(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		input     string
		wantModel string
		wantSite  string
	}{
		{name: "empty", input: "", wantModel: "allow_all", wantSite: "allow_all"},
		{name: "whitespace allow list", input: "\tallow_list\n", wantModel: "allow_list", wantSite: "allow_list"},
		{name: "allow all literal", input: "allow_all", wantModel: "allow_all", wantSite: "allow_all"},
		{name: "case sensitive", input: "ALLOW_LIST", wantModel: "allow_all", wantSite: "allow_all"},
		{name: "unknown", input: "deny_all", wantModel: "allow_all", wantSite: "allow_all"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := normalizeModelPolicy(tc.input); got != tc.wantModel {
				t.Fatalf("normalizeModelPolicy(%q) = %q, want %q", tc.input, got, tc.wantModel)
			}
			if got := normalizeSitePolicy(tc.input); got != tc.wantSite {
				t.Fatalf("normalizeSitePolicy(%q) = %q, want %q", tc.input, got, tc.wantSite)
			}
		})
	}
}

func TestNormalizeRateLimitStatusBoundaryValues(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		input string
		want  string
	}{
		{name: "enabled", input: store.RateLimitStatusEnabled, want: store.RateLimitStatusEnabled},
		{name: "disabled", input: store.RateLimitStatusDisabled, want: store.RateLimitStatusDisabled},
		{name: "whitespace disabled", input: " \t" + store.RateLimitStatusDisabled + "\n", want: store.RateLimitStatusDisabled},
		{name: "blank defaults disabled", input: " \t\n ", want: store.RateLimitStatusDisabled},
		{name: "uppercase rejected", input: "ENABLED", want: ""},
		{name: "unknown rejected", input: "paused", want: ""},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := normalizeRateLimitStatus(tc.input); got != tc.want {
				t.Fatalf("normalizeRateLimitStatus(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestRateLimitInputFromStorePreservesValidZeroLimits(t *testing.T) {
	t.Parallel()

	got := rateLimitInputFromStore(store.GatewayRateLimit{
		Status:   " \t" + store.RateLimitStatusDisabled + "\n",
		RPMLimit: sql.NullInt64{Int64: 0, Valid: true},
		TPMLimit: sql.NullInt64{Int64: 0, Valid: true},
	})

	if got.Status != store.RateLimitStatusDisabled {
		t.Fatalf("status = %q, want %q", got.Status, store.RateLimitStatusDisabled)
	}
	if got.RPMLimit == nil || *got.RPMLimit != 0 {
		t.Fatalf("rpm limit = %#v, want pointer to zero", got.RPMLimit)
	}
	if got.TPMLimit == nil || *got.TPMLimit != 0 {
		t.Fatalf("tpm limit = %#v, want pointer to zero", got.TPMLimit)
	}
}

func TestNullablePointerHelpersBoundaryValues(t *testing.T) {
	t.Parallel()

	if got := nullInt64Ptr(sql.NullInt64{Int64: 0, Valid: true}); got == nil || *got != 0 {
		t.Fatalf("nullInt64Ptr valid zero = %#v, want pointer to zero", got)
	}
	if got := nullInt64Ptr(sql.NullInt64{Int64: 99, Valid: false}); got != nil {
		t.Fatalf("nullInt64Ptr invalid = %#v, want nil", got)
	}

	zeroFloat := 0.0
	if got := nullableFloat(&zeroFloat); got != zeroFloat {
		t.Fatalf("nullableFloat zero = %#v, want %v", got, zeroFloat)
	}
	if got := nullFloatAsAny(sql.NullFloat64{Float64: 0, Valid: true}); got != 0.0 {
		t.Fatalf("nullFloatAsAny valid zero = %#v, want 0", got)
	}
	if got := nullFloatAsAny(sql.NullFloat64{Float64: 42.5, Valid: false}); got != nil {
		t.Fatalf("nullFloatAsAny invalid = %#v, want nil", got)
	}

	now := time.Date(2026, 6, 22, 12, 30, 0, 0, time.UTC)
	if got := nullableTime(nil); got != nil {
		t.Fatalf("nullableTime nil = %#v, want nil", got)
	}
	if got := nullableTime(&now); got != now {
		t.Fatalf("nullableTime non-zero = %#v, want %v", got, now)
	}
	if got := timePtrAsAny(nil); got != nil {
		t.Fatalf("timePtrAsAny nil = %#v, want nil", got)
	}
}

func TestRequiresCSRFBoundaryMethods(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		method string
		want   bool
	}{
		{method: http.MethodGet, want: false},
		{method: http.MethodHead, want: false},
		{method: http.MethodOptions, want: false},
		{method: http.MethodTrace, want: false},
		{method: http.MethodPost, want: true},
		{method: http.MethodPatch, want: true},
		{method: "get", want: true},
		{method: "", want: true},
	} {
		tc := tc
		t.Run(tc.method, func(t *testing.T) {
			t.Parallel()

			if got := requiresCSRF(tc.method); got != tc.want {
				t.Fatalf("requiresCSRF(%q) = %v, want %v", tc.method, got, tc.want)
			}
		})
	}
}

func TestCSRFSecretBoundaryValues(t *testing.T) {
	t.Parallel()

	// Empty master key falls back to a process-random secret (not a predictable
	// constant): non-empty, stable within the process, and unequal to the master key.
	var nilService *Service
	fallback := nilService.csrfSecret()
	if fallback == "" || fallback == "xlyra-default-csrf-secret" {
		t.Fatalf("nil service csrfSecret must be a random fallback, got %q", fallback)
	}
	if got := (&Service{}).csrfSecret(); got != fallback {
		t.Fatalf("empty master key csrfSecret should be stable, got %q vs %q", got, fallback)
	}
	if got := (&Service{masterKey: "test-master-key"}).csrfSecret(); got != "test-master-key" {
		t.Fatalf("configured csrfSecret = %q, want test-master-key", got)
	}
}
