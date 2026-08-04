package ratelimit

import (
	"database/sql"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

func TestActiveConfigFromStoreRejectsUnknownStatusWithLimits(t *testing.T) {
	t.Parallel()

	if cfg, ok := activeConfigFromStore(store.GatewayRateLimit{
		Scope:    store.RateLimitScopeGlobal,
		Status:   "paused",
		TPMLimit: sql.NullInt64{Int64: 100, Valid: true},
	}, "global"); ok {
		t.Fatalf("activeConfigFromStore returned active config %#v, want inactive", cfg)
	}
}

func TestActiveConfigFromStoreCarriesSingleLimits(t *testing.T) {
	t.Parallel()

	rpmOnly, ok := activeConfigFromStore(store.GatewayRateLimit{
		Scope:    store.RateLimitScopeGlobal,
		Status:   store.RateLimitStatusEnabled,
		RPMLimit: sql.NullInt64{Int64: 60, Valid: true},
	}, "global")
	if !ok {
		t.Fatal("expected rpm-only config to be active")
	}
	if rpmOnly.RPMLimit == nil || *rpmOnly.RPMLimit != 60 {
		t.Fatalf("rpm limit = %#v, want 60", rpmOnly.RPMLimit)
	}
	if rpmOnly.TPMLimit != nil {
		t.Fatalf("tpm limit = %#v, want nil", rpmOnly.TPMLimit)
	}

	tpmOnly, ok := activeConfigFromStore(store.GatewayRateLimit{
		Scope:    store.RateLimitScopeAPIKey,
		Status:   store.RateLimitStatusEnabled,
		TPMLimit: sql.NullInt64{Int64: 6000, Valid: true},
	}, "api_key:"+uuid.Nil.String())
	if !ok {
		t.Fatal("expected tpm-only config to be active")
	}
	if tpmOnly.RPMLimit != nil {
		t.Fatalf("rpm limit = %#v, want nil", tpmOnly.RPMLimit)
	}
	if tpmOnly.TPMLimit == nil || *tpmOnly.TPMLimit != 6000 {
		t.Fatalf("tpm limit = %#v, want 6000", tpmOnly.TPMLimit)
	}
}
