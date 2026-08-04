package auth

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

func TestDevPostgresAuthServiceReadOnlySmoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg, err := authDevPostgresSmokeConfig()
	if err != nil {
		t.Skipf("dev PostgreSQL smoke disabled: %v", err)
	}

	db, err := store.Open(ctx, cfg)
	if err != nil {
		t.Skipf("dev PostgreSQL unavailable: %s", authRedactDatabaseOpenError(err, cfg))
	}
	defer db.Close()

	service := NewService(db.DB(), "test-master-key")
	missingID := uuid.New()

	status, err := service.BootstrapStatus(ctx)
	if err != nil {
		t.Fatalf("bootstrap status: %v", err)
	}
	if status.AdminCount < 0 {
		t.Fatalf("bootstrap admin count = %d, want non-negative", status.AdminCount)
	}
	if status.Initialized != (status.AdminCount > 0) {
		t.Fatalf("bootstrap initialized = %v, admin count = %d", status.Initialized, status.AdminCount)
	}

	apiKeys, err := service.ListAPIKeys(ctx)
	if err != nil {
		t.Fatalf("list api keys: %v", err)
	}
	assertAuthSmokeAPIKeysNewestFirst(t, apiKeys)

	if len(apiKeys) > 0 {
		first := apiKeys[0]
		got, err := service.GetAPIKey(ctx, first.ID)
		if err != nil {
			t.Fatalf("get existing api key: %v", err)
		}
		if got.ID != first.ID {
			t.Fatalf("api key id = %s, want %s", got.ID, first.ID)
		}

		rateLimit, err := service.APIKeyRateLimit(ctx, first.ID)
		if err != nil {
			t.Fatalf("api key rate limit: %v", err)
		}
		assertAuthSmokeRateLimitInputStable(t, rateLimit)

		sites, err := service.APIKeySites(ctx, first.ID)
		if err != nil {
			t.Fatalf("api key sites: %v", err)
		}
		assertAuthSmokeAPIKeySitesStable(t, sites)

		allowedSiteIDs, err := service.AllowedSiteIDs(ctx, first.ID)
		if err != nil {
			t.Fatalf("allowed site ids: %v", err)
		}
		assertAuthSmokeUUIDsNonNil(t, allowedSiteIDs)
		if first.SitePolicy == "allow_all" && allowedSiteIDs != nil {
			t.Fatalf("allow_all api key allowed site ids = %v, want nil", allowedSiteIDs)
		}
	}

	_, err = service.GetAPIKey(ctx, missingID)
	assertAuthSmokeRecordNotFound(t, err)

	missingRateLimit, err := service.APIKeyRateLimit(ctx, missingID)
	if err != nil {
		t.Fatalf("missing api key rate limit: %v", err)
	}
	if missingRateLimit.Status != store.RateLimitStatusDisabled {
		t.Fatalf("missing api key rate limit status = %q, want %q", missingRateLimit.Status, store.RateLimitStatusDisabled)
	}
	if missingRateLimit.RPMLimit != nil || missingRateLimit.TPMLimit != nil {
		t.Fatalf("missing api key rate limit limits = rpm %v tpm %v, want nil limits", missingRateLimit.RPMLimit, missingRateLimit.TPMLimit)
	}

	missingSites, err := service.APIKeySites(ctx, missingID)
	if err != nil {
		t.Fatalf("missing api key sites: %v", err)
	}
	if len(missingSites) != 0 {
		t.Fatalf("missing api key sites length = %d, want 0", len(missingSites))
	}

	_, err = service.AllowedSiteIDs(ctx, missingID)
	assertAuthSmokeRecordNotFound(t, err)

	auditLogs, total, err := service.ListAuditLogs(ctx, store.AdminAuditLogFilters{})
	if err != nil {
		t.Fatalf("list audit logs empty filters: %v", err)
	}
	if total < 0 {
		t.Fatalf("audit log total = %d, want non-negative", total)
	}
	if len(auditLogs) > 50 {
		t.Fatalf("audit log default page length = %d, want at most 50", len(auditLogs))
	}
	if int64(len(auditLogs)) > total {
		t.Fatalf("audit log page length = %d, total = %d", len(auditLogs), total)
	}
	assertAuthSmokeAuditLogsNewestFirst(t, auditLogs)
}

func authDevPostgresSmokeConfig() (config.Config, error) {
	cfg := config.Config{
		AppEnv:           "test",
		DBConnectTimeout: 2 * time.Second,
		DBMinConns:       0,
		DBMaxConns:       2,
		PostgresDSN:      strings.TrimSpace(os.Getenv("POSTGRES_DSN")),
		DBHost:           authGetenvDefault("DB_HOST", "127.0.0.1"),
		DBPort:           5432,
		DBName:           authGetenvDefault("DB_NAME", "xlyra"),
		DBUser:           authGetenvDefault("DB_USER", "postgres"),
		DBPassword:       authGetenvDefault("DB_PASSWORD", "postgres"),
		DBSSLMode:        authGetenvDefault("DB_SSLMODE", "disable"),
	}
	if value := strings.TrimSpace(os.Getenv("DB_PORT")); value != "" {
		port, err := strconv.Atoi(value)
		if err != nil {
			return config.Config{}, fmt.Errorf("invalid DB_PORT %q", value)
		}
		cfg.DBPort = port
	}
	return cfg, nil
}

func authGetenvDefault(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func authRedactDatabaseOpenError(err error, cfg config.Config) string {
	msg := err.Error()
	secrets := []string{cfg.DBPassword}
	if parsed, parseErr := url.Parse(strings.TrimSpace(cfg.PostgresDSN)); parseErr == nil && parsed.User != nil {
		if password, ok := parsed.User.Password(); ok {
			secrets = append(secrets, password)
		}
	}
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		msg = strings.ReplaceAll(msg, secret, "[redacted]")
		msg = strings.ReplaceAll(msg, url.QueryEscape(secret), "[redacted]")
		msg = strings.ReplaceAll(msg, url.PathEscape(secret), "[redacted]")
	}
	return msg
}

func assertAuthSmokeRecordNotFound(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("error = %v, want record not found", err)
	}
}

func assertAuthSmokeAPIKeysNewestFirst(t *testing.T, apiKeys []store.APIKey) {
	t.Helper()
	for i := 1; i < len(apiKeys); i++ {
		if apiKeys[i].CreatedAt.After(apiKeys[i-1].CreatedAt) {
			t.Fatalf("api keys are not newest first at index %d", i)
		}
	}
}

func assertAuthSmokeRateLimitInputStable(t *testing.T, input RateLimitInput) {
	t.Helper()
	switch input.Status {
	case store.RateLimitStatusEnabled, store.RateLimitStatusDisabled:
	default:
		t.Fatalf("rate limit status = %q, want enabled or disabled", input.Status)
	}
	if input.RPMLimit != nil && *input.RPMLimit < 0 {
		t.Fatalf("rate limit rpm = %d, want non-negative", *input.RPMLimit)
	}
	if input.TPMLimit != nil && *input.TPMLimit < 0 {
		t.Fatalf("rate limit tpm = %d, want non-negative", *input.TPMLimit)
	}
}

func assertAuthSmokeAPIKeySitesStable(t *testing.T, sites []store.APIKeySitePermission) {
	t.Helper()
	for _, site := range sites {
		if site.ID == uuid.Nil {
			t.Fatalf("api key site permission has nil id: %#v", site)
		}
		if site.APIKeyID == uuid.Nil {
			t.Fatalf("api key site permission has nil api key id: %#v", site)
		}
		if site.SiteID == uuid.Nil {
			t.Fatalf("api key site permission has nil site id: %#v", site)
		}
	}
}

func assertAuthSmokeUUIDsNonNil(t *testing.T, ids []uuid.UUID) {
	t.Helper()
	for _, id := range ids {
		if id == uuid.Nil {
			t.Fatal("uuid list included nil id")
		}
	}
}

func assertAuthSmokeAuditLogsNewestFirst(t *testing.T, logs []store.AdminAuditLog) {
	t.Helper()
	for i := 1; i < len(logs); i++ {
		if logs[i].CreatedAt.After(logs[i-1].CreatedAt) {
			t.Fatalf("audit logs are not newest first at index %d", i)
		}
	}
}
