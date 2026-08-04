package ratelimit

import (
	"context"
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

func TestDevPostgresRateLimitReadOnlySmoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg, err := devPostgresRateLimitSmokeConfig()
	if err != nil {
		t.Skipf("dev PostgreSQL smoke disabled: %v", err)
	}

	db, err := store.Open(ctx, cfg)
	if err != nil {
		t.Skipf("dev PostgreSQL unavailable: %s", redactRateLimitDatabaseOpenError(err, cfg))
	}
	defer db.Close()

	service := NewService(db)
	global, err := service.GetGlobal(ctx)
	if err != nil {
		t.Fatalf("get global rate limit: %v", err)
	}
	assertRateLimitSmokeConfigStable(t, global)

	missingAPIKeyConfig, err := service.GetAPIKey(ctx, uuid.New())
	if err != nil {
		t.Fatalf("get missing api key rate limit: %v", err)
	}
	assertRateLimitSmokeDefaultConfig(t, missingAPIKeyConfig)

	for _, item := range rateLimitSmokeRows(t, ctx, db.DB()) {
		switch item.Scope {
		case store.RateLimitScopeGlobal:
			cfg, err := service.GetGlobal(ctx)
			if err != nil {
				t.Fatalf("get stored global rate limit: %v", err)
			}
			assertRateLimitSmokeConfigMatchesStore(t, cfg, item)
		case store.RateLimitScopeAPIKey:
			if !item.APIKeyID.Valid {
				t.Fatalf("api key rate limit has no api key id: %#v", item)
			}
			cfg, err := service.GetAPIKey(ctx, item.APIKeyID.UUID)
			if err != nil {
				t.Fatalf("get stored api key rate limit: %v", err)
			}
			assertRateLimitSmokeConfigMatchesStore(t, cfg, item)
		default:
			t.Fatalf("unexpected rate limit scope %q in dev data", item.Scope)
		}
	}
}

func rateLimitSmokeRows(t *testing.T, ctx context.Context, db *gorm.DB) []store.GatewayRateLimit {
	t.Helper()

	var rows []store.GatewayRateLimit
	if err := db.WithContext(ctx).Find(&rows).Error; err != nil {
		t.Fatalf("list rate limit rows for smoke: %v", err)
	}
	return rows
}

func assertRateLimitSmokeDefaultConfig(t *testing.T, cfg Config) {
	t.Helper()

	if cfg.Status != store.RateLimitStatusDisabled {
		t.Fatalf("default config status = %q, want disabled", cfg.Status)
	}
	if cfg.RPMLimit != nil || cfg.TPMLimit != nil {
		t.Fatalf("default config limits = %+v, want nil limits", cfg)
	}
}

func assertRateLimitSmokeConfigStable(t *testing.T, cfg Config) {
	t.Helper()

	switch cfg.Status {
	case store.RateLimitStatusEnabled, store.RateLimitStatusDisabled:
	default:
		t.Fatalf("unexpected normalized rate limit status %q", cfg.Status)
	}
	if cfg.RPMLimit != nil && *cfg.RPMLimit <= 0 {
		t.Fatalf("rpm limit should be positive when present: %+v", cfg)
	}
	if cfg.TPMLimit != nil && *cfg.TPMLimit <= 0 {
		t.Fatalf("tpm limit should be positive when present: %+v", cfg)
	}
}

func assertRateLimitSmokeConfigMatchesStore(t *testing.T, cfg Config, item store.GatewayRateLimit) {
	t.Helper()

	assertRateLimitSmokeConfigStable(t, cfg)
	want := configFromStore(item)
	if cfg.Status != want.Status {
		t.Fatalf("config status = %q, want %q from store row %#v", cfg.Status, want.Status, item)
	}
	assertRateLimitSmokeLimitPointer(t, "rpm", cfg.RPMLimit, want.RPMLimit)
	assertRateLimitSmokeLimitPointer(t, "tpm", cfg.TPMLimit, want.TPMLimit)
}

func assertRateLimitSmokeLimitPointer(t *testing.T, name string, got *int64, want *int64) {
	t.Helper()

	if got == nil || want == nil {
		if got != want {
			t.Fatalf("%s limit pointer = %v, want %v", name, got, want)
		}
		return
	}
	if *got != *want {
		t.Fatalf("%s limit = %d, want %d", name, *got, *want)
	}
}

func devPostgresRateLimitSmokeConfig() (config.Config, error) {
	cfg := config.Config{
		AppEnv:           "test",
		DBConnectTimeout: 2 * time.Second,
		DBMinConns:       0,
		DBMaxConns:       2,
		PostgresDSN:      strings.TrimSpace(os.Getenv("POSTGRES_DSN")),
		DBHost:           rateLimitGetenvDefault("DB_HOST", "127.0.0.1"),
		DBPort:           5432,
		DBName:           rateLimitGetenvDefault("DB_NAME", "xlyra"),
		DBUser:           rateLimitGetenvDefault("DB_USER", "postgres"),
		DBPassword:       rateLimitGetenvDefault("DB_PASSWORD", "postgres"),
		DBSSLMode:        rateLimitGetenvDefault("DB_SSLMODE", "disable"),
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

func rateLimitGetenvDefault(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func redactRateLimitDatabaseOpenError(err error, cfg config.Config) string {
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
