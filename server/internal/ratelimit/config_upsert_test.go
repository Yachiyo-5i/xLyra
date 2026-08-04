package ratelimit

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

func TestConfigUpsertPropagatesCreateError(t *testing.T) {
	t.Parallel()

	createErr := errors.New("create rate limit stopped")
	db := ratelimitPostgresGormWithCallbacks(t, ratelimitGormCallbacks{
		query: func(tx *gorm.DB) {
			tx.AddError(gorm.ErrRecordNotFound)
		},
		create: func(tx *gorm.DB) {
			tx.AddError(createErr)
		},
	})

	service := NewService(ratelimitStoreWithGorm(t, db))
	_, err := service.SetGlobal(context.Background(), ConfigInput{Status: store.RateLimitStatusEnabled})
	if !errors.Is(err, createErr) {
		t.Fatalf("SetGlobal error = %v, want create error", err)
	}
	if err == nil || !strings.Contains(err.Error(), "upsert gateway rate limit") {
		t.Fatalf("SetGlobal error = %v, want upsert context", err)
	}
}

func TestConfigUpsertNormalizesDefaults(t *testing.T) {
	t.Parallel()

	apiKeyID := uuid.New()
	db := ratelimitPostgresGormWithCallbacks(t, ratelimitGormCallbacks{
		query: func(tx *gorm.DB) {
			if item, ok := tx.Statement.Dest.(*store.GatewayRateLimit); ok {
				*item = store.GatewayRateLimit{
					ID:       uuid.New(),
					Scope:    store.RateLimitScopeAPIKey,
					APIKeyID: uuid.NullUUID{UUID: apiKeyID, Valid: true},
					Status:   store.RateLimitStatusEnabled,
				}
				tx.Statement.RowsAffected = 1
				return
			}
			tx.AddError(gorm.ErrRecordNotFound)
		},
		update: func(tx *gorm.DB) {
			tx.Statement.RowsAffected = 1
		},
	})

	service := NewService(ratelimitStoreWithGorm(t, db))
	cfg, err := service.SetAPIKey(context.Background(), apiKeyID, ConfigInput{})
	if err != nil {
		t.Fatalf("SetAPIKey returned error: %v", err)
	}
	if cfg.Status != store.RateLimitStatusDisabled {
		t.Fatalf("SetAPIKey status = %q, want disabled default", cfg.Status)
	}
	if cfg.RPMLimit != nil || cfg.TPMLimit != nil {
		t.Fatalf("SetAPIKey limits = %#v/%#v, want nil limits", cfg.RPMLimit, cfg.TPMLimit)
	}
}

func TestConfigUpsertPropagatesUpdateError(t *testing.T) {
	t.Parallel()

	updateErr := errors.New("update rate limit stopped")
	db := ratelimitPostgresGormWithCallbacks(t, ratelimitGormCallbacks{
		query: func(tx *gorm.DB) {
			if item, ok := tx.Statement.Dest.(*store.GatewayRateLimit); ok {
				*item = store.GatewayRateLimit{
					ID:     uuid.New(),
					Scope:  store.RateLimitScopeGlobal,
					Status: store.RateLimitStatusEnabled,
				}
				tx.Statement.RowsAffected = 1
				return
			}
			tx.AddError(gorm.ErrRecordNotFound)
		},
		update: func(tx *gorm.DB) {
			tx.AddError(updateErr)
		},
	})

	service := NewService(ratelimitStoreWithGorm(t, db))
	_, err := service.SetGlobal(context.Background(), ConfigInput{Status: store.RateLimitStatusDisabled})
	if !errors.Is(err, updateErr) {
		t.Fatalf("SetGlobal error = %v, want update error", err)
	}
	if err == nil || !strings.Contains(err.Error(), "upsert gateway rate limit") {
		t.Fatalf("SetGlobal error = %v, want upsert context", err)
	}
}

func TestConfigUpsertCreatesEnabledGlobalConfigWithLimits(t *testing.T) {
	t.Parallel()

	db := ratelimitPostgresGormWithCallbacks(t, ratelimitGormCallbacks{
		query: func(tx *gorm.DB) {
			tx.AddError(gorm.ErrRecordNotFound)
		},
		create: func(tx *gorm.DB) {
			tx.Statement.RowsAffected = 1
		},
	})

	rpmLimit := int64(12)
	tpmLimit := int64(3456)
	service := NewService(ratelimitStoreWithGorm(t, db))
	cfg, err := service.SetGlobal(context.Background(), ConfigInput{
		Status:   store.RateLimitStatusEnabled,
		RPMLimit: &rpmLimit,
		TPMLimit: &tpmLimit,
	})
	if err != nil {
		t.Fatalf("SetGlobal returned error: %v", err)
	}
	if cfg.Status != store.RateLimitStatusEnabled {
		t.Fatalf("SetGlobal status = %q, want enabled", cfg.Status)
	}
	if cfg.RPMLimit == nil || *cfg.RPMLimit != rpmLimit || cfg.TPMLimit == nil || *cfg.TPMLimit != tpmLimit {
		t.Fatalf("SetGlobal limits = %#v/%#v, want %d/%d", cfg.RPMLimit, cfg.TPMLimit, rpmLimit, tpmLimit)
	}
}
