package store

import (
	"database/sql"
	"testing"
)

func TestAPIKeyQuotaExceeded(t *testing.T) {
	t.Parallel()

	limited := func(limit, used float64) APIKey {
		return APIKey{QuotaLimit: sql.NullFloat64{Float64: limit, Valid: true}, QuotaUsed: used + 1000, QuotaTotalUsed: used}
	}

	cases := []struct {
		name string
		key  APIKey
		want bool
	}{
		{"under limit", limited(100, 40), false},
		{"at limit", limited(100, 100), true},
		{"over limit", limited(100, 140), true},
		{"unlimited ignores usage", APIKey{QuotaUnlimited: true, QuotaLimit: sql.NullFloat64{Float64: 100, Valid: true}, QuotaUsed: 999, QuotaTotalUsed: 999}, false},
		{"no limit set is not exceeded", APIKey{QuotaUsed: 50}, false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.key.QuotaExceeded(); got != tc.want {
				t.Fatalf("QuotaExceeded() = %v, want %v", got, tc.want)
			}
		})
	}
}
