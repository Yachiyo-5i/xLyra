package ratelimit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

func TestDefaultConfigDisablesRateLimits(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Status != store.RateLimitStatusDisabled {
		t.Fatalf("expected disabled default config, got %q", cfg.Status)
	}
	if cfg.RPMLimit != nil || cfg.TPMLimit != nil {
		t.Fatalf("expected no limits by default, got %+v", cfg)
	}
}

func TestServiceDefaultsWithoutStore(t *testing.T) {
	ctx := context.Background()
	cfg, err := (*Service)(nil).GetGlobal(ctx)
	if err != nil {
		t.Fatalf("nil service global config: %v", err)
	}
	if cfg.Status != store.RateLimitStatusDisabled {
		t.Fatalf("expected nil service to return disabled config, got %+v", cfg)
	}

	cfg, err = NewService(nil).GetAPIKey(ctx, uuid.New())
	if err != nil {
		t.Fatalf("nil store api key config: %v", err)
	}
	if cfg.Status != store.RateLimitStatusDisabled {
		t.Fatalf("expected nil store to return disabled config, got %+v", cfg)
	}

	cfg, err = (&Service{}).GetAPIKey(ctx, uuid.Nil)
	if err != nil {
		t.Fatalf("nil api key config: %v", err)
	}
	if cfg.Status != store.RateLimitStatusDisabled {
		t.Fatalf("expected nil api key to return disabled config, got %+v", cfg)
	}
}

func TestServiceRejectsSetWithoutStoreOrAPIKey(t *testing.T) {
	ctx := context.Background()
	if _, err := NewService(nil).SetGlobal(ctx, ConfigInput{Status: store.RateLimitStatusEnabled}); err == nil {
		t.Fatal("expected SetGlobal without store to fail")
	}
	if _, err := NewService(nil).SetAPIKey(ctx, uuid.Nil, ConfigInput{Status: store.RateLimitStatusEnabled}); err == nil {
		t.Fatal("expected SetAPIKey without api key to fail")
	}
}

func TestEstimateTokensUsesExplicitOutputLimit(t *testing.T) {
	got := EstimateTokens(map[string]any{
		"model":      "gpt-5.4",
		"messages":   []map[string]any{{"role": "user", "content": "hello"}},
		"max_tokens": float64(128),
	})

	if got < 129 || got > 220 {
		t.Fatalf("expected prompt estimate plus max_tokens reservation, got %d", got)
	}
}

func TestEstimateTokensAcceptsPromptShapesAndOutputLimitKeys(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]any
		wantMin int64
		wantMax int64
	}{
		{
			name: "string prompt max completion",
			payload: map[string]any{
				"model":                 "gpt-5.4",
				"prompt":                "hello",
				"max_completion_tokens": int64(64),
			},
			wantMin: 65,
			wantMax: 140,
		},
		{
			name: "array content max output",
			payload: map[string]any{
				"model": "gpt-5.4",
				"messages": []any{
					map[string]any{
						"role": "user",
						"content": []any{
							map[string]any{"type": "text", "text": "hello"},
						},
					},
				},
				"max_output_tokens": json.Number("96"),
			},
			wantMin: 97,
			wantMax: 180,
		},
		{
			name: "float output cap",
			payload: map[string]any{
				"model":      "gpt-5.4",
				"messages":   []map[string]any{{"role": "user", "content": "hello"}},
				"max_tokens": float32(32),
			},
			wantMin: 33,
			wantMax: 120,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EstimateTokens(tt.payload)
			if got < tt.wantMin || got > tt.wantMax {
				t.Fatalf("expected estimate in [%d,%d], got %d", tt.wantMin, tt.wantMax, got)
			}
		})
	}
}

func TestEstimateTokensDefaultsOutputReservation(t *testing.T) {
	got := EstimateTokens(map[string]any{
		"model":    "gpt-5.4",
		"messages": []map[string]any{{"role": "user", "content": "hello"}},
	})

	if got < defaultReserveOutputTokens+1 {
		t.Fatalf("expected default output reservation, got %d", got)
	}
}

func TestEstimateTokensHandlesNilAndUnmarshalablePayload(t *testing.T) {
	t.Parallel()

	if got := EstimateTokens(nil); got != defaultReserveOutputTokens {
		t.Fatalf("nil payload estimate = %d, want %d", got, defaultReserveOutputTokens)
	}

	got := EstimateTokens(map[string]any{
		"model":      func() {},
		"max_tokens": int64(2),
	})
	if got != 3 {
		t.Fatalf("unmarshalable payload estimate = %d, want prompt fallback plus requested output", got)
	}
}

func TestOutputTokenReservationIgnoresInvalidAndNonPositiveLimits(t *testing.T) {
	t.Parallel()

	if got := outputTokenReservation(map[string]any{"max_tokens": int64(0), "max_output_tokens": int64(-1)}); got != defaultReserveOutputTokens {
		t.Fatalf("non-positive output reservation = %d, want default %d", got, defaultReserveOutputTokens)
	}
	if got := outputTokenReservation(map[string]any{"max_tokens": "128"}); got != defaultReserveOutputTokens {
		t.Fatalf("string output reservation = %d, want default %d", got, defaultReserveOutputTokens)
	}
	if got := outputTokenReservation(map[string]any{"max_tokens": int64(64), "max_output_tokens": int64(128)}); got != 64 {
		t.Fatalf("first valid output reservation = %d, want 64", got)
	}
}

func TestConfigFromStoreNormalizesStatusAndPointers(t *testing.T) {
	cfg := configFromStore(store.GatewayRateLimit{
		Status:   "unknown",
		RPMLimit: sql.NullInt64{Int64: 60, Valid: true},
		TPMLimit: sql.NullInt64{},
	})

	if cfg.Status != store.RateLimitStatusDisabled {
		t.Fatalf("expected unknown status to normalize to disabled, got %q", cfg.Status)
	}
	if cfg.RPMLimit == nil || *cfg.RPMLimit != 60 {
		t.Fatalf("expected rpm pointer, got %+v", cfg.RPMLimit)
	}
	if cfg.TPMLimit != nil {
		t.Fatalf("expected nil tpm pointer, got %+v", cfg.TPMLimit)
	}
}

func TestNormalizeStatus(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: store.RateLimitStatusEnabled, want: store.RateLimitStatusEnabled},
		{input: " " + store.RateLimitStatusDisabled + " ", want: store.RateLimitStatusDisabled},
		{input: "", want: store.RateLimitStatusDisabled},
		{input: "paused", want: ""},
	}

	for _, tt := range tests {
		if got := normalizeStatus(tt.input); got != tt.want {
			t.Fatalf("normalizeStatus(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestLimitErrorMatchesErrLimited(t *testing.T) {
	err := LimitError{Scope: store.RateLimitScopeAPIKey, LimitType: "rpm"}
	if !errors.Is(err, ErrLimited) {
		t.Fatal("LimitError should match ErrLimited")
	}
}

func TestLimitErrorUsesCustomOrDefaultMessage(t *testing.T) {
	t.Parallel()

	custom := LimitError{Message: "api key rpm exceeded"}
	if custom.Error() != "api key rpm exceeded" {
		t.Fatalf("custom limit error message = %q", custom.Error())
	}

	if got := (LimitError{}).Error(); got != ErrLimited.Error() {
		t.Fatalf("default limit error message = %q, want %q", got, ErrLimited.Error())
	}
}

func TestAcquireAndSettleSkipWhenStoreOrAPIKeyUnavailable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service := NewService(nil)
	reservation, err := service.Acquire(ctx, AcquireInput{
		APIKeyID:    uuid.New(),
		EstimateTPM: true,
		Payload:     map[string]any{"max_tokens": int64(10)},
	})
	if err != nil {
		t.Fatalf("Acquire without store returned error: %v", err)
	}
	if reservation != nil {
		t.Fatalf("Acquire without store = %#v, want nil", reservation)
	}

	reservation, err = (&Service{}).Acquire(ctx, AcquireInput{APIKeyID: uuid.Nil})
	if err != nil || reservation != nil {
		t.Fatalf("Acquire without api key = %#v, err=%v; want nil nil", reservation, err)
	}

	if err := service.Settle(ctx, &Reservation{Scopes: []ReservationScope{{ReservedTokens: 10}}}, -1); err != nil {
		t.Fatalf("Settle without store returned error: %v", err)
	}
	if err := (*Service)(nil).Settle(ctx, nil, 10); err != nil {
		t.Fatalf("nil service Settle returned error: %v", err)
	}
}

func TestActiveConfigFromStoreRequiresEnabledLimits(t *testing.T) {
	t.Parallel()

	rpm := sql.NullInt64{Int64: 60, Valid: true}
	tpm := sql.NullInt64{Int64: 6000, Valid: true}
	cfg, ok := activeConfigFromStore(store.GatewayRateLimit{
		Scope:    store.RateLimitScopeAPIKey,
		Status:   store.RateLimitStatusEnabled,
		RPMLimit: rpm,
		TPMLimit: tpm,
	}, "api_key:one")
	if !ok {
		t.Fatal("expected enabled config with limits to be active")
	}
	if cfg.Scope != store.RateLimitScopeAPIKey || cfg.ScopeKey != "api_key:one" {
		t.Fatalf("unexpected active config identity: %#v", cfg)
	}
	if cfg.RPMLimit == nil || *cfg.RPMLimit != 60 || cfg.TPMLimit == nil || *cfg.TPMLimit != 6000 {
		t.Fatalf("unexpected active config limits: %#v", cfg)
	}

	if _, ok := activeConfigFromStore(store.GatewayRateLimit{Status: store.RateLimitStatusDisabled, RPMLimit: rpm}, "global"); ok {
		t.Fatal("disabled config should not be active")
	}
	if _, ok := activeConfigFromStore(store.GatewayRateLimit{Status: store.RateLimitStatusEnabled}, "global"); ok {
		t.Fatal("enabled config without limits should not be active")
	}
}

func TestReservationMetadataIncludesScopes(t *testing.T) {
	rpm := int64(60)
	tpm := int64(1000)
	reservation := Reservation{
		WindowStart:     time.Date(2026, 5, 8, 12, 34, 0, 0, time.UTC),
		EstimatedTokens: 200,
		ReservedTokens:  200,
		Scopes: []ReservationScope{
			{
				Scope:          store.RateLimitScopeAPIKey,
				ScopeKey:       "api_key:example",
				RPMLimit:       &rpm,
				TPMLimit:       &tpm,
				RPMUsed:        3,
				TPMReserved:    200,
				ReservedTokens: 200,
			},
		},
	}

	meta := reservation.Metadata(42)
	assertReservationMetadataTopLevel(t, meta, 200, 200, 42, "2026-05-08T12:34:00Z")
	scope := requireReservationMetadataScope(t, meta)
	if scope["scope"] != store.RateLimitScopeAPIKey || scope["limit_type"] != nil {
		t.Fatalf("unexpected scope result: %#v", scope)
	}
}

func TestReservationMetadataNilAndUnlimitedScope(t *testing.T) {
	if meta := (*Reservation)(nil).Metadata(0); meta != nil {
		t.Fatalf("expected nil reservation metadata, got %#v", meta)
	}

	reservation := Reservation{
		WindowStart:     time.Date(2026, 5, 8, 12, 34, 0, 0, time.UTC),
		EstimatedTokens: 1,
		ReservedTokens:  0,
		Scopes: []ReservationScope{
			{Scope: store.RateLimitScopeGlobal, ScopeKey: "global"},
		},
	}
	meta := reservation.Metadata(0)
	scope := requireReservationMetadataScope(t, meta)
	if scope["rpm_limit"] != nil || scope["tpm_limit"] != nil {
		t.Fatalf("expected nil limit fields, got %#v", scope)
	}
}

func TestInt64Helpers(t *testing.T) {
	value := int64(123)
	if got := int64PointerValue(&value); got != value {
		t.Fatalf("unexpected pointer value: %#v", got)
	}
	if got := int64PointerValue(nil); got != nil {
		t.Fatalf("expected nil pointer value, got %#v", got)
	}
	if got := int64PointerAsAny(&value); got != value {
		t.Fatalf("unexpected pointer any: %#v", got)
	}
	if got := int64PointerAsAny(nil); got != nil {
		t.Fatalf("expected nil pointer any, got %#v", got)
	}

	inputs := []any{int(7), int32(8), int64(9), float32(10), float64(11), json.Number("12")}
	for _, input := range inputs {
		if _, ok := int64FromAny(input); !ok {
			t.Fatalf("expected %T to convert", input)
		}
	}
	if _, ok := int64FromAny("12"); ok {
		t.Fatal("expected string to be rejected")
	}
	if _, ok := int64FromAny(json.Number("12.5")); ok {
		t.Fatal("expected non-integer json number to be rejected")
	}
}
