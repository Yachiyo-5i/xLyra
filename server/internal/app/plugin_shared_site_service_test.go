package app

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/config"
	"xlyra/server/internal/jsplugin"
	oauthsvc "xlyra/server/internal/oauth"
	"xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

func TestSharedSiteServiceReceivesPluginProviders(t *testing.T) {
	st := openPluginProviderStore(t)
	const (
		masterKey = "test-master-key"
		poolID    = "acme.example"
		poolType  = "acme.example/pool"
		poolHost  = "api.example.test"
		keyID     = "acme.sample"
		keyType   = "acme.sample/chat"
		keyHost   = "api.example.com"
	)
	enableSignedProviders(t, st, []signedProvider{
		{id: poolID, hosts: []string{poolHost}, raw: chainPackage(t, poolID, poolType, poolHost)},
		{id: keyID, hosts: []string{keyHost}, raw: apiKeyProviderPackage(t, keyID, keyType, keyHost)},
	})

	oauth := oauthsvc.NewService(st, masterKey)
	shared := site.NewServiceWithOAuthService(st, masterKey, config.ResolveTimeZone(), oauth)
	startRouterWithSiteService(t, st, oauth, shared)

	module, ok := shared.ModuleForSiteType(poolType)
	if !ok || module == nil || !moduleListsSiteType(module, poolType) {
		t.Fatalf("pool module = %#v, %v", module, ok)
	}
	provider, ok := shared.AccountProviderFor(poolType)
	if !ok || provider == nil {
		t.Fatal("pool account runtime was not registered on the shared site service")
	}
	keyModule, ok := shared.ModuleForSiteType(keyType)
	if !ok || keyModule == nil || !moduleListsSiteType(keyModule, keyType) {
		t.Fatalf("api key module = %#v, %v", keyModule, ok)
	}
	if _, ok := shared.AccountProviderFor(keyType); ok {
		t.Fatal("api key plugin exposed an account provider")
	}

	poolSite := createPluginSite(t, shared, "Acme Pool", poolType, "https://"+poolHost)
	assertZeroCredentialSiteStaysNormal(t, shared, poolSite.ID, poolType)
}

func TestNilSharedSiteServiceLeavesCallerUnregistered(t *testing.T) {
	st := openPluginProviderStore(t)
	ctx := context.Background()
	const (
		masterKey = "test-master-key"
		poolID    = "acme.example"
		poolType  = "acme.example/pool"
		poolHost  = "api.example.test"
	)
	enableSignedProviders(t, st, []signedProvider{
		{id: poolID, hosts: []string{poolHost}, raw: chainPackage(t, poolID, poolType, poolHost)},
	})

	oauth := oauthsvc.NewService(st, masterKey)
	external := site.NewServiceWithOAuthService(st, masterKey, config.ResolveTimeZone(), oauth)
	startRouterWithSiteService(t, st, oauth, nil)

	if _, ok := site.LookupSiteType(poolType); !ok {
		t.Fatal("nil shared site service did not publish the plugin site type from the router")
	}
	if _, ok := external.ModuleForSiteType(poolType); ok {
		t.Fatal("nil shared site service registered the module on the caller's instance")
	}
	if _, ok := external.AccountProviderFor(poolType); ok {
		t.Fatal("nil shared site service registered the account runtime on the caller's instance")
	}
	_, _, err := external.Create(ctx, site.CreateSiteParams{
		Name: "Acme", Slug: "acme-nil-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12],
		SiteType: poolType, BaseURL: "https://" + poolHost, Enabled: true,
	})
	if err == nil || !strings.Contains(err.Error(), `unsupported site_type "acme.example/pool"`) {
		t.Fatalf("create on the unshared service = %v", err)
	}
}

type signedProvider struct {
	id    string
	hosts []string
	raw   []byte
}

func enableSignedProviders(t *testing.T, st *store.Store, items []signedProvider) {
	t.Helper()
	ctx := context.Background()
	mgr := jsplugin.NewManager(st, &jsplugin.Catalog{})
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		signed, err := jsplugin.AttachSignature(item.raw, priv)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := mgr.Upload(ctx, "", signed); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := mgr.AddTrustedKey(ctx, "", "acme", base64.StdEncoding.EncodeToString(pub)); err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if err := mgr.Enable(ctx, item.id, "1.0.0", jsplugin.EnableOptions{ConfirmedHosts: item.hosts}); err != nil {
			t.Fatal(err)
		}
	}
}

func startRouterWithSiteService(t *testing.T, st *store.Store, oauth *oauthsvc.Service, shared *site.Service) {
	t.Helper()
	logger, _ := discardLogger()
	bg, cancel := context.WithCancel(context.Background())
	var tasks sync.WaitGroup
	siteProviderMu.Lock()
	prevCurrent := siteProviderCurrent
	siteProviderMu.Unlock()
	prevCatalog := jsplugin.DefaultCatalog().Current()
	t.Cleanup(func() {
		cancel()
		tasks.Wait()
		siteProviderMu.Lock()
		siteProviderCurrent = nil
		siteProviderMu.Unlock()
		target := shared
		if target == nil {
			target = site.NewService(nil, "test-master-key")
		}
		applySiteProviders(nil, target, logger)
		if prevCatalog != nil {
			jsplugin.DefaultCatalog().Replace(prevCatalog)
		}
		siteProviderMu.Lock()
		siteProviderCurrent = prevCurrent
		siteProviderMu.Unlock()
	})
	_, _ = NewRouterWithGatewayWithOAuth(bg, &tasks, testConfig(), logger, st, nil, "test-master-key", oauth, shared)
}

func createPluginSite(t *testing.T, svc *site.Service, name, siteType, baseURL string) store.Site {
	t.Helper()
	created, _, err := svc.Create(context.Background(), site.CreateSiteParams{
		Name: name, Slug: strings.ReplaceAll(uuid.NewString(), "-", "")[:12],
		SiteType: siteType, BaseURL: baseURL, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create %s: %v", siteType, err)
	}
	return created
}

func assertZeroCredentialSiteStaysNormal(t *testing.T, svc *site.Service, siteID uuid.UUID, siteType string) {
	t.Helper()
	ctx := context.Background()
	refreshed, err := svc.RefreshState(ctx, siteID)
	if err != nil {
		t.Fatalf("refresh %s: %v", siteType, err)
	}
	if !refreshed.Site.Enabled || refreshed.Site.Status != "active" || refreshed.State.SyncStatus != "synced" || refreshed.State.APIKeyCount != 0 {
		t.Fatalf("refresh %s enabled=%v status=%s sync=%s count=%d message=%v", siteType, refreshed.Site.Enabled, refreshed.Site.Status, refreshed.State.SyncStatus, refreshed.State.APIKeyCount, refreshed.State.SyncMessage)
	}
	if refreshed.State.SyncMessage.Valid && strings.Contains(refreshed.State.SyncMessage.String, "unsupported site_type") {
		t.Fatalf("refresh message %s", refreshed.State.SyncMessage.String)
	}
	health, err := svc.CheckSiteHealth(ctx, siteID, "manual")
	if err != nil {
		t.Fatalf("health %s: %v", siteType, err)
	}
	if !health.Snapshot.Success || health.State.Status != "healthy" {
		t.Fatalf("health %s success=%v status=%s type=%v message=%v", siteType, health.Snapshot.Success, health.State.Status, health.Snapshot.ErrorType, health.Snapshot.ErrorMessage)
	}
	if strings.Contains(health.Snapshot.ErrorType.String+health.Snapshot.ErrorMessage.String, "unsupported site_type") {
		t.Fatalf("health text type=%v message=%v", health.Snapshot.ErrorType, health.Snapshot.ErrorMessage)
	}
	for _, snapshot := range health.Recent {
		if !snapshot.Success || strings.Contains(snapshot.ErrorType.String+snapshot.ErrorMessage.String, "unsupported site_type") {
			t.Fatalf("failure snapshot success=%v type=%v message=%v", snapshot.Success, snapshot.ErrorType, snapshot.ErrorMessage)
		}
	}
	got, err := svc.Get(ctx, siteID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.Status != "active" {
		t.Fatalf("site %s after checks enabled=%v status=%s", siteType, got.Enabled, got.Status)
	}
}

func moduleListsSiteType(module interface{ SiteTypes() []string }, siteType string) bool {
	for _, item := range module.SiteTypes() {
		if item == siteType {
			return true
		}
	}
	return false
}

func apiKeyProviderPackage(t *testing.T, pluginID, siteType, host string) []byte {
	t.Helper()
	source := "export const meta = { apiVersion: 1, id: " + strconv.Quote(pluginID) + ", kind: \"site_provider\" };\n"
	manifest := jsplugin.Manifest{
		ID: pluginID, Name: "Example", Version: "1.0.0", APIVersion: 1, HostAPI: 1, Kind: jsplugin.KindSiteProvider,
		SiteProvider: jsplugin.SiteProviderSection{
			SiteType:   siteType,
			Display:    jsplugin.SiteProviderDisplay{Name: jsplugin.LocalizedText{"en": "Example"}, Subtitle: jsplugin.LocalizedText{"en": "API"}},
			Hosts:      []string{host},
			Credential: jsplugin.SiteProviderCredential{Model: "api_key"},
			Protocol: jsplugin.SiteProviderProtocol{
				Kind: "openai_chat", Path: "/v1/chat/completions", OfficialBaseURL: "https://" + host + "/v1",
				DefaultEndpointTypes: []string{"openai"},
			},
			Models: jsplugin.SiteProviderModels{Path: "/v1/models", Auth: "bearer"},
		},
		SHA256: map[string]string{"plugin.js": jsplugin.HashSource(source)},
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jsplugin.BuildPackage(map[string][]byte{"manifest.json": encoded, "plugin.js": []byte(source)})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
