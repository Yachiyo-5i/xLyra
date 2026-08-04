package ratelimit

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

func TestGetAPIKeyReturnsDefaultConfigForNilAPIKey(t *testing.T) {
	t.Parallel()

	cfg, err := NewService(nil).GetAPIKey(t.Context(), uuid.Nil)
	if err != nil {
		t.Fatalf("GetAPIKey returned error: %v", err)
	}
	if cfg.Status != DefaultConfig().Status || cfg.RPMLimit != nil || cfg.TPMLimit != nil {
		t.Fatalf("GetAPIKey nil key = %#v, want default config", cfg)
	}
}

func TestNumericLimitParsingIgnoresUnsupportedTypes(t *testing.T) {
	t.Parallel()

	if value, ok := int64FromAny(uint(12)); ok || value != 0 {
		t.Fatalf("uint conversion = (%d, %v), want (0, false)", value, ok)
	}
	if value, ok := int64FromAny(nil); ok || value != 0 {
		t.Fatalf("nil conversion = (%d, %v), want (0, false)", value, ok)
	}
	if got := outputTokenReservation(map[string]any{"max_tokens": uint(64)}); got != defaultReserveOutputTokens {
		t.Fatalf("uint output reservation = %d, want default %d", got, defaultReserveOutputTokens)
	}
}

func TestSetRejectsInvalidPureInputsBeforeRepositoryUse(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service := &Service{db: &store.Store{}}
	positiveLimit := int64(1)

	tests := []struct {
		name    string
		call    func() error
		wantErr string
	}{
		{
			name: "global without store db",
			call: func() error {
				_, err := service.SetGlobal(ctx, ConfigInput{Status: store.RateLimitStatusEnabled, RPMLimit: &positiveLimit})
				return err
			},
			wantErr: "store is not initialized",
		},
		{
			name: "api key requires id before store",
			call: func() error {
				_, err := service.SetAPIKey(ctx, uuid.Nil, ConfigInput{Status: store.RateLimitStatusEnabled})
				return err
			},
			wantErr: "api_key_id is required",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.call()
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}
