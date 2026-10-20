package app

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/config"
	"xlyra/server/internal/gateway"
	"xlyra/server/internal/jsplugin"
	"xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

func TestApplySiteProvidersEnableDisableAndPeerSync(t *testing.T) {
	st := openPluginProviderStore(t)
	svc := site.NewService(nil, "test-master-key")
	logger, _ := discardLogger()
	primary := jsplugin.NewManager(st, &jsplugin.Catalog{})
	peer := jsplugin.NewManager(st, &jsplugin.Catalog{})
	const id = "acme.example"
	const siteType = "acme.example/chat"
	hosts := []string{"api.example.com"}
	official := "https://api.example.com/v1"

	var peerCalls atomic.Int32
	primary.SetCatalogListener(func(registry *jsplugin.Registry) {
		applySiteProviders(registry, svc, logger)
	})
	peer.SetCatalogListener(func(registry *jsplugin.Registry) {
		peerCalls.Add(1)
		applySiteProviders(registry, svc, logger)
	})

	syncCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		if done := peer.CatalogSyncDone(); done != nil {
			select {
			case <-done:
			case <-time.After(2 * time.Second):
			}
		}
		applySiteProviders(nil, svc, logger)
		site.ClearPluginSiteTypes()
		gateway.ClearRuntimeProviderSpecs()
	})

	ctx := context.Background()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jsplugin.AttachSignature(providerSyncPackage(t), priv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := primary.Upload(ctx, "", raw); err != nil {
		t.Fatal(err)
	}
	if _, err := primary.AddTrustedKey(ctx, "", "acme", base64.StdEncoding.EncodeToString(pub)); err != nil {
		t.Fatal(err)
	}
	if err := primary.Enable(ctx, id, "1.0.0", jsplugin.EnableOptions{ConfirmedHosts: hosts}); err != nil {
		t.Fatal(err)
	}
	assertProviderInstalled(t, svc, siteType, "示例", "API", id, hosts, official)
	if err := primary.Disable(ctx, id); err != nil {
		t.Fatal(err)
	}
	assertProviderCleared(t, svc, siteType)
	if _, ok := site.LookupSiteType("openai"); !ok {
		t.Fatal("disable dropped builtin site types")
	}

	before := peerCalls.Load()
	peer.StartSync(syncCtx, 20*time.Millisecond)
	waitForProviderSync(t, func() bool { return peerCalls.Load() > before })
	assertProviderCleared(t, svc, siteType)

	enabledCalls := peerCalls.Load()
	if err := primary.Enable(ctx, id, "1.0.0", jsplugin.EnableOptions{ConfirmedHosts: append([]string(nil), hosts...)}); err != nil {
		t.Fatal(err)
	}
	assertProviderInstalled(t, svc, siteType, "示例", "API", id, hosts, official)
	waitForProviderSync(t, func() bool {
		registry := peer.Catalog().Registry()
		if registry == nil || peerCalls.Load() <= enabledCalls {
			return false
		}
		_, ok := registry.ByPluginID(id)
		return ok
	})
	assertProviderInstalled(t, svc, siteType, "示例", "API", id, hosts, official)

	if err := primary.Disable(ctx, id); err != nil {
		t.Fatal(err)
	}
	assertProviderCleared(t, svc, siteType)
	waitForProviderSync(t, func() bool {
		registry := peer.Catalog().Registry()
		if registry == nil {
			return false
		}
		_, ok := registry.ByPluginID(id)
		return !ok
	})
	assertProviderCleared(t, svc, siteType)
}

func providerSyncPackage(t *testing.T) []byte {
	t.Helper()
	source := "export const meta = { apiVersion: 1, id: \"acme.example\", kind: \"site_provider\" };\n"
	manifest := jsplugin.Manifest{
		ID: "acme.example", Name: "Example", Version: "1.0.0", APIVersion: 1, HostAPI: 1,
		Kind: jsplugin.KindSiteProvider,
		SiteProvider: jsplugin.SiteProviderSection{
			SiteType: "acme.example/chat",
			Display: jsplugin.SiteProviderDisplay{
				Name:     jsplugin.LocalizedText{"zh": "示例", "en": "Example"},
				Subtitle: jsplugin.LocalizedText{"en": "API"},
			},
			Hosts:      []string{"api.example.com"},
			Credential: jsplugin.SiteProviderCredential{Model: "api_key"},
			Protocol: jsplugin.SiteProviderProtocol{
				Kind:                 "openai_chat",
				Path:                 "/v1/chat/completions",
				OfficialBaseURL:      "https://api.example.com/v1",
				DefaultEndpointTypes: []string{"openai"},
				RequestParams:        json.RawMessage(`{"unsupported":["logit_bias"]}`),
			},
			Models: jsplugin.SiteProviderModels{Path: "/v1/models", Auth: "header:X-Api-Key"},
		},
		SHA256: map[string]string{"plugin.js": jsplugin.HashSource(source)},
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jsplugin.BuildPackage(map[string][]byte{
		"manifest.json": encoded,
		"plugin.js":     []byte(source),
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func waitForProviderSync(t *testing.T, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if done() {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("timed out waiting for catalog sync")
}

func openPluginProviderStore(t *testing.T) *store.Store {
	t.Helper()
	cfg := config.Config{
		AppEnv: "test", DBConnectTimeout: 2 * time.Second, DBMaxConns: 4,
		PostgresDSN: strings.TrimSpace(os.Getenv("POSTGRES_DSN")),
		DBHost:      pluginProviderEnv("DB_HOST", "127.0.0.1"), DBPort: 5432,
		DBName: pluginProviderEnv("DB_NAME", "xlyra"), DBUser: pluginProviderEnv("DB_USER", "postgres"),
		DBPassword: pluginProviderEnv("DB_PASSWORD", "postgres"), DBSSLMode: pluginProviderEnv("DB_SSLMODE", "disable"),
	}
	if value := strings.TrimSpace(os.Getenv("DB_PORT")); value != "" {
		port, err := strconv.Atoi(value)
		if err != nil {
			t.Skipf("invalid DB_PORT %q", value)
		}
		cfg.DBPort = port
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	base, err := store.Open(ctx, cfg)
	if err != nil {
		t.Skipf("dev PostgreSQL unavailable: %v", err)
	}
	schema := "xlyra_app_provider_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := base.DB().Exec(fmt.Sprintf("CREATE SCHEMA %q", schema)).Error; err != nil {
		base.Close()
		t.Skipf("cannot create a test schema: %v", err)
	}
	t.Cleanup(func() {
		_ = base.DB().Exec(fmt.Sprintf("DROP SCHEMA %q CASCADE", schema)).Error
		base.Close()
	})
	parsed, err := url.Parse(cfg.DatabaseDSN())
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	cfg.PostgresDSN = parsed.String()
	var migrateErr error
	for attempt := 0; attempt < 5; attempt++ {
		if migrateErr = store.EnsureDatabaseInitialized(ctx, cfg); migrateErr == nil {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if migrateErr != nil {
		t.Fatalf("migrate the test schema: %v", migrateErr)
	}
	st, err := store.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open the test schema: %v", err)
	}
	t.Cleanup(st.Close)
	return st
}

func pluginProviderEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
