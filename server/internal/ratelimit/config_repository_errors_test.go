package ratelimit

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

func TestConfigLookupsReturnRepositoryErrors(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("rate limit query stopped")
	service := NewService(ratelimitStoreWithGorm(t, ratelimitGormWithQueryCallback(t, func(tx *gorm.DB) {
		tx.AddError(queryErr)
	})))

	if _, err := service.GetGlobal(context.Background()); !errors.Is(err, queryErr) {
		t.Fatalf("GetGlobal error = %v, want query error", err)
	}
	if _, err := service.GetAPIKey(context.Background(), uuid.New()); !errors.Is(err, queryErr) {
		t.Fatalf("GetAPIKey error = %v, want query error", err)
	}
}

func TestGetGlobalReturnsDefaultConfigWhenRepositoryReportsNotFound(t *testing.T) {
	t.Parallel()

	service := NewService(ratelimitStoreWithGorm(t, ratelimitGormWithQueryCallback(t, func(tx *gorm.DB) {
		tx.AddError(gorm.ErrRecordNotFound)
	})))
	cfg, err := service.GetGlobal(context.Background())
	if err != nil {
		t.Fatalf("GetGlobal returned error: %v", err)
	}
	if cfg != DefaultConfig() {
		t.Fatalf("GetGlobal config = %#v, want default disabled config", cfg)
	}
}

func TestConfigUpdatesValidateInputsBeforeSaving(t *testing.T) {
	t.Parallel()

	service := NewService(ratelimitStoreWithGorm(t, ratelimitPostgresGorm(t)))
	ctx := context.Background()

	tests := []struct {
		name    string
		call    func() error
		wantErr string
	}{
		{
			name: "unknown status",
			call: func() error {
				_, err := service.SetGlobal(ctx, ConfigInput{Status: "paused"})
				return err
			},
			wantErr: "rate_limit.status must be enabled or disabled",
		},
		{
			name: "zero rpm",
			call: func() error {
				_, err := service.SetGlobal(ctx, ConfigInput{
					Status:   store.RateLimitStatusEnabled,
					RPMLimit: ratelimitInt64(0),
				})
				return err
			},
			wantErr: "rate_limit.rpm_limit must be greater than 0",
		},
		{
			name: "negative tpm",
			call: func() error {
				_, err := service.SetAPIKey(ctx, uuid.New(), ConfigInput{
					Status:   store.RateLimitStatusEnabled,
					TPMLimit: ratelimitInt64(-1),
				})
				return err
			},
			wantErr: "rate_limit.tpm_limit must be greater than 0",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.call()
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestActiveConfigsHandlesMissingAndFailedRepositoryLookups(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	apiKeyID := uuid.New()

	t.Run("missing configs are skipped", func(t *testing.T) {
		t.Parallel()

		service := NewService(ratelimitStoreWithGorm(t, ratelimitGormWithQueryCallback(t, func(tx *gorm.DB) {
			tx.AddError(gorm.ErrRecordNotFound)
		})))
		configs, err := service.activeConfigsCached(ctx, apiKeyID, time.Now())
		if err != nil {
			t.Fatalf("activeConfigsCached returned error: %v", err)
		}
		if len(configs) != 0 {
			t.Fatalf("activeConfigsCached = %#v, want empty", configs)
		}
	})

	t.Run("global error stops immediately", func(t *testing.T) {
		t.Parallel()

		queryErr := errors.New("global lookup failed")
		service := NewService(ratelimitStoreWithGorm(t, ratelimitGormWithQueryCallback(t, func(tx *gorm.DB) {
			tx.AddError(queryErr)
		})))
		if _, err := service.activeConfigsCached(ctx, apiKeyID, time.Now()); !errors.Is(err, queryErr) {
			t.Fatalf("activeConfigsCached error = %v, want global query error", err)
		}
	})

	t.Run("api key error is returned after missing global", func(t *testing.T) {
		t.Parallel()

		queryErr := errors.New("api key lookup failed")
		callCount := 0
		service := NewService(ratelimitStoreWithGorm(t, ratelimitGormWithQueryCallback(t, func(tx *gorm.DB) {
			callCount++
			if callCount == 1 {
				tx.AddError(gorm.ErrRecordNotFound)
				return
			}
			tx.AddError(queryErr)
		})))
		if _, err := service.activeConfigsCached(ctx, apiKeyID, time.Now()); !errors.Is(err, queryErr) {
			t.Fatalf("activeConfigsCached error = %v, want api key query error", err)
		}
		if callCount != 2 {
			t.Fatalf("query callback count = %d, want 2", callCount)
		}
	})
}

func ratelimitInt64(value int64) *int64 {
	return &value
}
