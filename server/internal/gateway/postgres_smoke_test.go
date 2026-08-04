package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/config"
	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

func TestDevPostgresSiteGatewayConfigSmoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg, err := devPostgresGatewaySmokeConfig()
	if err != nil {
		t.Skipf("dev PostgreSQL smoke disabled: %v", err)
	}

	db, err := store.Open(ctx, cfg)
	if err != nil {
		t.Skipf("dev PostgreSQL unavailable: %s", redactGatewayDatabaseOpenError(err, cfg))
	}
	defer db.Close()

	items, err := store.NewSiteRepository(db.DB()).List(ctx)
	if err != nil {
		t.Fatalf("list sites: %v", err)
	}
	if len(items) == 0 {
		return
	}

	handler := Handler{db: db}
	gotConfig, gotHeaders, gotProxyID, err := handler.siteGatewayConfig(ctx, items[0].ID)
	if err != nil {
		t.Fatalf("site gateway config for site %s: %v", items[0].ID, err)
	}

	wantConfig := site.GatewayConfigFromSiteMeta(items[0].Meta)
	wantHeaders := site.RequestHeadersFromSiteMeta(items[0].Meta)
	wantProxyID := gatewaySmokeProxyIDFromMeta(items[0].Meta)
	if !reflect.DeepEqual(gotConfig, wantConfig) {
		t.Fatalf("gateway config = %#v, want %#v", gotConfig, wantConfig)
	}
	if !reflect.DeepEqual(gotHeaders, wantHeaders) {
		t.Fatalf("request headers = %#v, want %#v", gotHeaders, wantHeaders)
	}
	if gotProxyID != wantProxyID {
		t.Fatalf("proxy id = %q, want %q", gotProxyID, wantProxyID)
	}
}

func TestDevPostgresGatewayModelsSmoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg, err := devPostgresGatewaySmokeConfig()
	if err != nil {
		t.Skipf("dev PostgreSQL smoke disabled: %v", err)
	}

	db, err := store.Open(ctx, cfg)
	if err != nil {
		t.Skipf("dev PostgreSQL unavailable: %s", redactGatewayDatabaseOpenError(err, cfg))
	}
	defer db.Close()

	apiKey, ok, err := gatewaySmokeUsableAPIKey(ctx, db)
	if err != nil {
		t.Fatalf("load gateway API keys: %v", err)
	}
	if !ok {
		t.Skip("dev PostgreSQL has no active API key with usable quota for gateway models smoke")
	}

	handler := NewHandler(nil, auth.NewService(db.DB(), "test-master-key"), routeengine.NewService(db), db, "test-master-key")
	handler.rateLimits = nil
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req = req.WithContext(auth.WithAPIKey(req.Context(), apiKey))
	rec := httptest.NewRecorder()

	handler.Models(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("gateway models status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("ETag") == "" {
		t.Fatalf("gateway models cache headers missing: %#v", rec.Header())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("gateway models Cache-Control = %q, want no-store", got)
	}
	var body struct {
		Object string           `json:"object"`
		Data   []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode gateway models response: %v", err)
	}
	if body.Object != "list" {
		t.Fatalf("gateway models object = %q, want list", body.Object)
	}
	assertGatewayModelsPayloadStable(t, body.Data)

	etagReq := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	etagReq.Header.Set("If-None-Match", rec.Header().Get("ETag"))
	etagReq = etagReq.WithContext(auth.WithAPIKey(etagReq.Context(), apiKey))
	etagRec := httptest.NewRecorder()

	handler.Models(etagRec, etagReq)

	if etagRec.Code != http.StatusNotModified {
		t.Fatalf("gateway models etag status = %d, want 304; body=%s", etagRec.Code, etagRec.Body.String())
	}
}

func devPostgresGatewaySmokeConfig() (config.Config, error) {
	cfg := config.Config{
		AppEnv:           "test",
		DBConnectTimeout: 2 * time.Second,
		DBMinConns:       0,
		DBMaxConns:       2,
		PostgresDSN:      strings.TrimSpace(os.Getenv("POSTGRES_DSN")),
		DBHost:           gatewayGetenvDefault("DB_HOST", "127.0.0.1"),
		DBPort:           5432,
		DBName:           gatewayGetenvDefault("DB_NAME", "xlyra"),
		DBUser:           gatewayGetenvDefault("DB_USER", "postgres"),
		DBPassword:       gatewayGetenvDefault("DB_PASSWORD", "postgres"),
		DBSSLMode:        gatewayGetenvDefault("DB_SSLMODE", "disable"),
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

func gatewayGetenvDefault(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func redactGatewayDatabaseOpenError(err error, cfg config.Config) string {
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

func gatewaySmokeProxyIDFromMeta(raw []byte) string {
	meta := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &meta)
	}
	proxyID, _ := meta["proxy_id"].(string)
	return proxyID
}

func gatewaySmokeUsableAPIKey(ctx context.Context, db *store.Store) (store.APIKey, bool, error) {
	now := time.Now()
	apiKeys, err := store.NewAPIKeyRepository(db.DB()).List(ctx)
	if err != nil {
		return store.APIKey{}, false, err
	}
	for _, apiKey := range apiKeys {
		if apiKey.ID == uuid.Nil || apiKey.Status != "active" {
			continue
		}
		if apiKey.ExpiresAt != nil && !apiKey.ExpiresAt.After(now) {
			continue
		}
		if !apiKey.QuotaUnlimited && (!apiKey.QuotaLimit.Valid || apiKey.QuotaUsed >= apiKey.QuotaLimit.Float64) {
			continue
		}
		return apiKey, true, nil
	}
	return store.APIKey{}, false, nil
}

func assertGatewayModelsPayloadStable(t *testing.T, rows []map[string]any) {
	t.Helper()
	for index, row := range rows {
		if row["object"] != "model" {
			t.Fatalf("model row %d object = %#v, want model", index, row["object"])
		}
		if value, ok := row["id"].(string); !ok || strings.TrimSpace(value) == "" {
			t.Fatalf("model row %d id is empty: %#v", index, row)
		}
		if _, ok := row["created"].(float64); !ok {
			t.Fatalf("model row %d created is not numeric: %#v", index, row["created"])
		}
		metadata, ok := row["metadata"].(map[string]any)
		if !ok {
			t.Fatalf("model row %d metadata is not an object: %#v", index, row["metadata"])
		}
		if value, ok := metadata["canonical_model_id"].(string); !ok || strings.TrimSpace(value) == "" {
			t.Fatalf("model row %d canonical_model_id is empty: %#v", index, metadata)
		}
	}
}
