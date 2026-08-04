package catalog

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

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

func TestDevPostgresCatalogSmoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg, err := devPostgresCatalogSmokeConfig()
	if err != nil {
		t.Skipf("dev PostgreSQL smoke disabled: %v", err)
	}

	db, err := store.Open(ctx, cfg)
	if err != nil {
		t.Skipf("dev PostgreSQL unavailable: %s", redactCatalogDatabaseOpenError(err, cfg))
	}
	defer db.Close()

	service := NewService(db)
	items, err := service.List(ctx)
	if err != nil {
		t.Fatalf("list catalog models: %v", err)
	}
	assertCatalogItemsStable(t, items)

	_, err = service.Matrix(ctx, uuid.New())
	if err == nil {
		t.Fatal("expected missing canonical model matrix lookup to fail")
	}

	if _, err := service.BindSiteModel(ctx, uuid.New(), nil); err == nil {
		t.Fatal("expected missing site model bind lookup to fail")
	}
	nilCanonicalID := uuid.Nil
	if _, err := service.BindSiteModel(ctx, uuid.New(), &nilCanonicalID); err == nil {
		t.Fatal("expected missing nil canonical bind lookup to fail")
	}

	missingMatches, err := service.MatchSiteModels(ctx, uuid.New())
	if err != nil {
		t.Fatalf("match missing site models: %v", err)
	}
	if len(missingMatches) != 0 {
		t.Fatalf("missing site matched models length = %d, want 0", len(missingMatches))
	}

	for _, item := range items {
		if item.Model.ID == uuid.Nil {
			continue
		}
		matrix, err := service.Matrix(ctx, item.Model.ID)
		if err != nil {
			t.Fatalf("matrix for canonical model %s: %v", item.Model.ID, err)
		}
		if matrix.Model.ID != item.Model.ID {
			t.Fatalf("matrix model id = %s, want %s", matrix.Model.ID, item.Model.ID)
		}
		assertCatalogMatrixRowsStable(t, matrix.Rows)
		return
	}
}

func devPostgresCatalogSmokeConfig() (config.Config, error) {
	cfg := config.Config{
		AppEnv:           "test",
		DBConnectTimeout: 2 * time.Second,
		DBMinConns:       0,
		DBMaxConns:       2,
		PostgresDSN:      strings.TrimSpace(os.Getenv("POSTGRES_DSN")),
		DBHost:           catalogGetenvDefault("DB_HOST", "127.0.0.1"),
		DBPort:           5432,
		DBName:           catalogGetenvDefault("DB_NAME", "xlyra"),
		DBUser:           catalogGetenvDefault("DB_USER", "postgres"),
		DBPassword:       catalogGetenvDefault("DB_PASSWORD", "postgres"),
		DBSSLMode:        catalogGetenvDefault("DB_SSLMODE", "disable"),
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

func catalogGetenvDefault(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func redactCatalogDatabaseOpenError(err error, cfg config.Config) string {
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

func assertCatalogItemsStable(t *testing.T, items []CanonicalModelItem) {
	t.Helper()
	for index, item := range items {
		if index > 0 && item.Model.ModelKey < items[index-1].Model.ModelKey {
			t.Fatalf("catalog models are not sorted by model key at index %d", index)
		}
		if item.Model.SiteCount < 0 || item.Model.SiteModelCount < 0 {
			t.Fatalf("catalog model stats should be non-negative: %#v", item.Model)
		}
		for aliasIndex, alias := range item.Aliases {
			if alias.CanonicalModelID != item.Model.ID {
				t.Fatalf("alias %s belongs to canonical %s, want %s", alias.ID, alias.CanonicalModelID, item.Model.ID)
			}
			if aliasIndex > 0 && alias.Alias < item.Aliases[aliasIndex-1].Alias {
				t.Fatalf("aliases are not sorted by alias at index %d", aliasIndex)
			}
		}
	}
}

func assertCatalogMatrixRowsStable(t *testing.T, rows []store.CanonicalModelMatrixRow) {
	t.Helper()
	for index, row := range rows {
		if row.SiteID == uuid.Nil || row.SiteModelID == uuid.Nil {
			t.Fatalf("matrix row %d has nil identifiers: %#v", index, row)
		}
		if row.APIKeyCount < 0 || row.AvailableAPIKeyCount < 0 || row.AvailableAPIKeyCount > row.APIKeyCount {
			t.Fatalf("matrix row %d has invalid API key counts: %#v", index, row)
		}
		if index == 0 {
			continue
		}
		previous := rows[index-1]
		if previous.SiteName > row.SiteName ||
			(previous.SiteName == row.SiteName && previous.UpstreamModelName > row.UpstreamModelName) ||
			(previous.SiteName == row.SiteName && previous.UpstreamModelName == row.UpstreamModelName && previous.GroupName.String > row.GroupName.String) {
			t.Fatalf("matrix rows are not sorted at index %d", index)
		}
	}
}
