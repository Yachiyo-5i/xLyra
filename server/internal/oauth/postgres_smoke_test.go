package oauth

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

func TestDevPostgresOAuthServiceReadSmoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg, err := devPostgresOAuthSmokeConfig()
	if err != nil {
		t.Skipf("dev PostgreSQL smoke disabled: %v", err)
	}

	db, err := store.Open(ctx, cfg)
	if err != nil {
		t.Skipf("dev PostgreSQL unavailable: %s", redactOAuthDatabaseOpenError(err, cfg))
	}
	defer db.Close()

	service := NewService(db, "master-key")
	connections, err := service.ListConnections(ctx)
	if err != nil {
		t.Fatalf("list oauth connections: %v", err)
	}
	assertOAuthServiceConnectionsStable(t, connections)

	missingConnectionID := uuid.New()
	_, err = service.ConnectionRecordByID(ctx, missingConnectionID)
	assertOAuthServiceRecordNotFound(t, err)
	_, err = service.ConnectionByID(ctx, missingConnectionID)
	assertOAuthServiceRecordNotFound(t, err)
	_, err = service.ConnectionBySiteID(ctx, uuid.New())
	assertOAuthServiceRecordNotFound(t, err)
}

func TestOAuthHTTPClientForConnectionUsesDefaultWithoutSiteLookup(t *testing.T) {
	t.Parallel()

	service := NewService(nil, "master-key")
	client, err := service.httpClientForConnection(context.Background(), store.OAuthConnection{})
	if err != nil {
		t.Fatalf("default connection client: %v", err)
	}
	if client == nil {
		t.Fatal("default connection client is nil")
	}

	siteID := uuid.Nil
	client, err = service.httpClientForConnection(context.Background(), store.OAuthConnection{SiteID: &siteID})
	if err != nil {
		t.Fatalf("nil site connection client: %v", err)
	}
	if client == nil {
		t.Fatal("nil site connection client is nil")
	}
}

func devPostgresOAuthSmokeConfig() (config.Config, error) {
	cfg := config.Config{
		AppEnv:           "test",
		DBConnectTimeout: 2 * time.Second,
		DBMinConns:       0,
		DBMaxConns:       2,
		PostgresDSN:      strings.TrimSpace(os.Getenv("POSTGRES_DSN")),
		DBHost:           oauthGetenvDefault("DB_HOST", "127.0.0.1"),
		DBPort:           5432,
		DBName:           oauthGetenvDefault("DB_NAME", "xlyra"),
		DBUser:           oauthGetenvDefault("DB_USER", "postgres"),
		DBPassword:       oauthGetenvDefault("DB_PASSWORD", "postgres"),
		DBSSLMode:        oauthGetenvDefault("DB_SSLMODE", "disable"),
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

func oauthGetenvDefault(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func redactOAuthDatabaseOpenError(err error, cfg config.Config) string {
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

func assertOAuthServiceRecordNotFound(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("error = %v, want gorm.ErrRecordNotFound", err)
	}
}

func assertOAuthServiceConnectionsStable(t *testing.T, connections []store.OAuthConnection) {
	t.Helper()
	for index, connection := range connections {
		if connection.ID == uuid.Nil {
			t.Fatalf("oauth connection %d has nil id", index)
		}
		if strings.TrimSpace(connection.Provider) == "" {
			t.Fatalf("oauth connection %s has blank provider", connection.ID)
		}
		if connection.SiteID == nil || *connection.SiteID == uuid.Nil {
			t.Fatalf("oauth connection %s is not site-bound", connection.ID)
		}
		if index == 0 {
			continue
		}
		previous := connections[index-1]
		if previous.CreatedAt.After(connection.CreatedAt) {
			t.Fatalf("oauth connections are not oldest first at index %d", index)
		}
		if previous.CreatedAt.Equal(connection.CreatedAt) && previous.ID.String() > connection.ID.String() {
			t.Fatalf("oauth connections with equal created_at are not sorted by id at index %d", index)
		}
	}
}
