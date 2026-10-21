package app

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/gateway"
	"xlyra/server/internal/jsplugin"
	"xlyra/server/internal/site"
)

func TestSiteProviderListModelsHookSyncsModels(t *testing.T) {
	st := openPluginProviderStore(t)
	svc := site.NewService(st, "test-master-key")
	logger, _ := discardLogger()
	catalog := &jsplugin.Catalog{}
	mgr := jsplugin.NewManager(st, catalog)
	const id = "acme.example"
	const siteType = "acme.example/chat"
	hosts := []string{"api.example.com"}
	ctx := context.Background()

	previousCurrent := siteProviderCurrent
	previousCatalog := jsplugin.DefaultCatalog().Current()
	siteProviderCurrent = nil
	t.Cleanup(func() {
		siteProviderCurrent = nil
		applySiteProviders(nil, svc, logger)
		siteProviderCurrent = previousCurrent
		site.ClearPluginSiteTypes()
		gateway.ClearRuntimeProviderSpecs()
		if previousCatalog != nil {
			jsplugin.DefaultCatalog().Replace(previousCatalog)
		} else {
			_ = jsplugin.DefaultCatalog().InitBuiltins()
		}
	})
	mgr.SetCatalogListener(func(registry *jsplugin.Registry) {
		applySiteProviders(registry, svc, logger)
	})

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jsplugin.AttachSignature(providerListModelsPackage(t), priv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Upload(ctx, "", raw); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.AddTrustedKey(ctx, "", "acme", base64.StdEncoding.EncodeToString(pub)); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Enable(ctx, id, "1.0.0", jsplugin.EnableOptions{ConfirmedHosts: hosts}); err != nil {
		t.Fatal(err)
	}
	registry := catalog.Registry()
	plugin, ok := registry.ByPluginID(id)
	if !ok || !plugin.HasHook("listModels") {
		t.Fatal("enabled package did not export listModels")
	}
	jsplugin.DefaultCatalog().Replace(&jsplugin.Snapshot{Gen: catalog.Generation(), Registry: registry})

	timeout := 800
	created, _, err := svc.Create(ctx, site.CreateSiteParams{
		Name: "Acme", Slug: "acme-" + uuid.NewString()[:8], SiteType: siteType,
		BaseURL: "https://api.example.com/v1", Enabled: true,
		GatewayConfig: &site.GatewayConfig{RequestTimeoutMS: &timeout},
		Credential:    &site.CredentialInput{Type: "api_key", Secret: "sk-plugin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.SyncModels(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.ModelCount != 1 || result.KeyErrors != 0 || len(result.Models) != 1 || result.Models[0].UpstreamName != "hook-vision" || result.Models[0].DisplayName != "Hook Vision" {
		t.Fatalf("sync = count %d errors %d models %#v", result.ModelCount, result.KeyErrors, result.Models)
	}
}

func providerListModelsPackage(t *testing.T) []byte {
	t.Helper()
	source := "export const meta = { apiVersion: 1, id: \"acme.example\", kind: \"site_provider\" };\n" +
		"export function listModels() { return { result: { models: [{ name: \"hook-vision\", displayName: \"Hook Vision\" }] } }; }\n"
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
			},
			Models: jsplugin.SiteProviderModels{Path: "/v1/models", Auth: "bearer"},
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
