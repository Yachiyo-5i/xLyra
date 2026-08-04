package auth

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestAuthValidationMasksRepositoryReadErrorsAsPublicErrors(t *testing.T) {
	t.Parallel()

	readErr := errors.New("auth repository read failed")
	service := authServiceWithQueryCallback(t, func(tx *gorm.DB) {
		tx.AddError(readErr)
	})

	_, err := service.ValidateAdminSession(context.Background(), "session-token")
	assertAuthErrorString(t, "ValidateAdminSession", err, "invalid session")
	_, err = service.ValidateAdminAccessToken(context.Background(), "access-token", "agent", "127.0.0.1")
	assertAuthErrorString(t, "ValidateAdminAccessToken", err, "invalid access token")
	_, err = service.ValidateAPIKey(context.Background(), apiKeyPublicPrefix+strings.Repeat("a", apiKeySecretLength))
	assertAuthErrorString(t, "ValidateAPIKey", err, "invalid api key")
}

func TestValidateAPIKeyUsesGeneratedAliasAfterCompatiblePrefixMiss(t *testing.T) {
	t.Parallel()

	readErr := errors.New("api key repository read failed")
	readAttempts := 0
	service := authServiceWithQueryCallback(t, func(tx *gorm.DB) {
		readAttempts++
		tx.AddError(readErr)
	})

	key := apiKeyCompatiblePrefix + strings.Repeat("b", apiKeySecretLength)
	_, err := service.ValidateAPIKey(context.Background(), key)
	assertAuthErrorString(t, "ValidateAPIKey compatible alias", err, "invalid api key")
	if readAttempts != 2 {
		t.Fatalf("repository read attempts = %d, want original lookup plus generated alias fallback", readAttempts)
	}
}

func TestAPIKeyRateLimitPropagatesRepositoryReadError(t *testing.T) {
	t.Parallel()

	readErr := errors.New("rate limit repository read failed")
	service := authServiceWithQueryCallback(t, func(tx *gorm.DB) {
		tx.AddError(readErr)
	})

	rateLimit, err := service.APIKeyRateLimit(context.Background(), uuid.New())
	if rateLimit != (RateLimitInput{}) {
		t.Fatalf("APIKeyRateLimit result = %#v, want zero on repository error", rateLimit)
	}
	assertAuthErrorIs(t, "APIKeyRateLimit", err, readErr)
}

func TestServiceReadMethodsPropagateRepositoryErrors(t *testing.T) {
	t.Parallel()

	readErr := errors.New("service repository read failed")
	service := authServiceWithQueryCallback(t, func(tx *gorm.DB) {
		tx.AddError(readErr)
	})
	ctx := context.Background()

	if status, err := service.BootstrapStatus(ctx); status != (BootstrapStatus{}) {
		t.Fatalf("BootstrapStatus = %#v, %v; want zero result on repository read error", status, err)
	} else {
		assertAuthErrorIs(t, "BootstrapStatus", err, readErr)
	}
	if apiKey, err := service.ConsumeQuota(ctx, uuid.New(), 0); apiKey.ID != uuid.Nil {
		t.Fatalf("ConsumeQuota = %#v, %v; want zero result on repository read error", apiKey, err)
	} else {
		assertAuthErrorIs(t, "ConsumeQuota", err, readErr)
	}
	if apiKey, err := service.UpdateAPIKey(ctx, uuid.New(), UpdateAPIKeyInput{}); apiKey.ID != uuid.Nil {
		t.Fatalf("UpdateAPIKey = %#v, %v; want zero result on repository read error", apiKey, err)
	} else {
		assertAuthErrorIs(t, "UpdateAPIKey", err, readErr)
	}
}

func authServiceWithQueryCallback(t *testing.T, callback func(*gorm.DB)) *Service {
	t.Helper()

	return authServiceWithOptionalQueryCallback(t, "test-master-key", callback)
}

func authServiceWithOptionalQueryCallback(t *testing.T, masterKey string, callback func(*gorm.DB)) *Service {
	t.Helper()

	service := NewService(authPostgresGorm(t), masterKey)
	if callback == nil {
		return service
	}
	authReplaceQueryCallback(t, service.db, callback)
	authReplaceRowCallback(t, service.db, func(tx *gorm.DB) {
		callback(tx)
	})
	return service
}

func authServiceWithRepositoryError(t *testing.T, err error) *Service {
	t.Helper()

	return authServiceWithQueryCallback(t, func(tx *gorm.DB) {
		tx.AddError(err)
	})
}

func authPostgresGorm(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  "host=localhost user=xlyra dbname=xlyra sslmode=disable",
		PreferSimpleProtocol: true,
	}), &gorm.Config{
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
		Logger:                 gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open offline gorm db: %v", err)
	}
	return db
}

func authReplaceQueryCallback(t *testing.T, db *gorm.DB, callback func(*gorm.DB)) {
	t.Helper()

	if err := db.Callback().Query().Replace("gorm:query", callback); err != nil {
		t.Fatalf("replace query callback: %v", err)
	}
}

func authReplaceRowCallback(t *testing.T, db *gorm.DB, callback func(*gorm.DB)) {
	t.Helper()

	if err := db.Callback().Row().Replace("gorm:row", callback); err != nil {
		t.Fatalf("replace row callback: %v", err)
	}
}

func authReplaceCreateCallback(t *testing.T, db *gorm.DB, callback func(*gorm.DB)) {
	t.Helper()

	if err := db.Callback().Create().Replace("gorm:create", callback); err != nil {
		t.Fatalf("replace create callback: %v", err)
	}
}

func authReplaceUpdateCallback(t *testing.T, db *gorm.DB, callback func(*gorm.DB)) {
	t.Helper()

	if err := db.Callback().Update().Replace("gorm:update", callback); err != nil {
		t.Fatalf("replace update callback: %v", err)
	}
}

func authReplaceDeleteCallback(t *testing.T, db *gorm.DB, callback func(*gorm.DB)) {
	t.Helper()

	if err := db.Callback().Delete().Replace("gorm:delete", callback); err != nil {
		t.Fatalf("replace delete callback: %v", err)
	}
}
