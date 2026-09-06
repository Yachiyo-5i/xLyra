package oauth

import (
	"context"
	"database/sql"
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

func TestDevPostgresOAuthRefreshLeaseDoesNotHoldRowLockDuringRefresh(t *testing.T) {
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

	if !db.DB().Migrator().HasColumn(&store.OAuthConnection{}, "RefreshLeaseID") {
		t.Skip("oauth refresh lease migration is not applied")
	}

	connection := store.OAuthConnection{
		ID:                    uuid.New(),
		Provider:              codexProvider,
		Status:                "connected",
		Email:                 "xlyra-refresh-lease-" + uuid.NewString(),
		EncryptedAccessToken:  "access",
		MaskedAccessToken:     "access",
		EncryptedRefreshToken: "refresh",
		MaskedRefreshToken:    "refresh",
		EncryptedIDToken:      "id",
		MaskedIDToken:         "id",
		RefreshLeaseID:        "lease-1",
		RefreshLeaseUntil:     sql.NullTime{Time: time.Now().Add(time.Minute), Valid: true},
	}
	if err := db.DB().WithContext(ctx).Create(&connection).Error; err != nil {
		t.Fatalf("create oauth connection: %v", err)
	}
	defer db.DB().WithContext(context.Background()).Delete(&store.OAuthConnection{}, connection.ID)

	claimed := make(chan struct{})
	refreshFinished := make(chan struct{})
	go func() {
		err := db.WithinTx(ctx, func(tx store.Tx) error {
			repo := store.NewOAuthConnectionRepository(tx)
			current, err := repo.GetByIDForUpdate(ctx, connection.ID)
			if err != nil {
				return err
			}
			current.RefreshLeaseID = "lease-2"
			current.RefreshLeaseUntil = sql.NullTime{Time: time.Now().Add(time.Minute), Valid: true}
			if _, err := repo.Save(ctx, current); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			t.Errorf("claim refresh lease: %v", err)
			return
		}
		close(claimed)
		time.Sleep(300 * time.Millisecond)
		close(refreshFinished)
	}()

	select {
	case <-claimed:
	case <-ctx.Done():
		t.Fatal("timed out waiting for refresh lease claim")
	}

	start := time.Now()
	var observed store.OAuthConnection
	if err := db.WithinTx(ctx, func(tx store.Tx) error {
		var err error
		observed, err = store.NewOAuthConnectionRepository(tx).GetByIDForUpdate(ctx, connection.ID)
		return err
	}); err != nil {
		t.Fatalf("read connection while refresh is in progress: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("row lock remained held during refresh: waited %s", elapsed)
	}
	if observed.RefreshLeaseID != "lease-2" {
		t.Fatalf("refresh lease id = %q, want lease-2", observed.RefreshLeaseID)
	}
	select {
	case <-refreshFinished:
	case <-ctx.Done():
		t.Fatal("timed out waiting for refresh transaction")
	}
}
