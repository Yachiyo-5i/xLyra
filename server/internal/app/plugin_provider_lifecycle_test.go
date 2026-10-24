package app

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"xlyra/server/internal/admin"
	"xlyra/server/internal/auth"
	"xlyra/server/internal/gateway"
	"xlyra/server/internal/jsplugin"
	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

func TestPluginSiteTypeDropsOutOfRoutingUntilSitesAreGone(t *testing.T) {
	st := openPluginProviderStore(t)
	svc := site.NewService(st, "test-master-key")
	logger, _ := discardLogger()
	mgr := jsplugin.NewManager(st, &jsplugin.Catalog{})
	const id = "acme.example"
	const siteType = "acme.example/chat"
	hosts := []string{"api.example.com"}
	ctx := context.Background()

	previousCurrent := siteProviderCurrent
	siteProviderCurrent = nil
	t.Cleanup(func() {
		siteProviderCurrent = nil
		applySiteProviders(nil, svc, logger)
		siteProviderCurrent = previousCurrent
		site.ClearPluginSiteTypes()
		gateway.ClearRuntimeProviderSpecs()
	})
	mgr.SetCatalogListener(func(registry *jsplugin.Registry) {
		applySiteProviders(registry, svc, logger)
	})

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jsplugin.AttachSignature(providerSyncPackage(t), priv)
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
	if !site.SiteTypeAvailable(siteType) || site.ProviderStatus(siteType) != "enabled" {
		t.Fatalf("available=%v status=%s", site.SiteTypeAvailable(siteType), site.ProviderStatus(siteType))
	}

	pluginSite, _, err := svc.Create(ctx, site.CreateSiteParams{
		Name: "Acme", Slug: "acme-" + uuid.NewString()[:8], SiteType: siteType,
		BaseURL: "https://api.example.com/v1", Enabled: true,
		Credential: &site.CredentialInput{Type: "api_key", Secret: "sk-plugin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	builtinSite, _, err := svc.Create(ctx, site.CreateSiteParams{
		Name: "OpenAI", Slug: "openai-" + uuid.NewString()[:8], SiteType: "openai",
		BaseURL: "https://api.openai.com/v1", Enabled: true,
		Credential: &site.CredentialInput{Type: "api_key", Secret: "sk-builtin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	pluginOnly := ensureCanonical(t, st, "plugin-only")
	builtinOnly := ensureCanonical(t, st, "builtin-only")
	shared := ensureCanonical(t, st, "shared-model")
	attachSiteModel(t, st, pluginSite.ID, pluginOnly)
	attachSiteModel(t, st, builtinSite.ID, builtinOnly)
	attachSiteModel(t, st, pluginSite.ID, shared)
	attachSiteModel(t, st, builtinSite.ID, shared)

	routerService := routeengine.NewService(st)
	routerService.SetSiteTypeFilter(site.SiteTypeAvailable)
	authService := auth.NewService(st.DB(), "test-master-key")
	gatewayHandler := gateway.NewHandler(logger, authService, routerService, st, "test-master-key")
	adminHandler := admin.NewHandler(logger, nil, svc, nil, nil, nil, nil, nil, nil, nil, nil).WithJSPlugins(mgr)

	if got := candidateSiteTypes(t, routerService, "plugin-only"); got != siteType {
		t.Fatalf("plugin candidates = %s", got)
	}
	if got := candidateSiteTypes(t, routerService, "shared-model"); !strings.Contains(got, siteType) || !strings.Contains(got, "openai") {
		t.Fatalf("shared candidates = %s", got)
	}
	assertModelCatalog(t, gatewayHandler, map[string]string{
		"plugin-only":  siteType,
		"builtin-only": "openai",
		"shared-model": siteType + ",openai",
	})
	assertProviderStatus(t, adminHandler, pluginSite.ID, builtinSite.ID, "enabled")

	if err := mgr.Disable(ctx, id); err != nil {
		t.Fatal(err)
	}
	if site.SiteTypeAvailable(siteType) || site.ProviderStatus(siteType) != "unavailable" {
		t.Fatalf("after disable available=%v status=%s", site.SiteTypeAvailable(siteType), site.ProviderStatus(siteType))
	}
	if _, err := store.NewSiteRepository(st.DB()).GetByID(ctx, pluginSite.ID); err != nil {
		t.Fatalf("disabled provider removed the site: %v", err)
	}
	if got := candidateSiteTypes(t, routerService, "plugin-only"); got != "" {
		t.Fatalf("plugin candidates after disable = %s", got)
	}
	if _, err := routerService.Select(ctx, routeengine.CandidateQuery{ModelKey: "plugin-only"}); !errors.Is(err, routeengine.ErrNoRouteCandidates) {
		t.Fatalf("Select after disable = %v", err)
	}
	if got := candidateSiteTypes(t, routerService, "builtin-only"); got != "openai" {
		t.Fatalf("builtin candidates after disable = %s", got)
	}
	if got := candidateSiteTypes(t, routerService, "shared-model"); got != "openai" {
		t.Fatalf("shared candidates after disable = %s", got)
	}
	assertModelCatalog(t, gatewayHandler, map[string]string{
		"builtin-only": "openai",
		"shared-model": "openai",
	})
	assertModelAbsent(t, gatewayHandler, "plugin-only")
	assertProviderStatus(t, adminHandler, pluginSite.ID, builtinSite.ID, "unavailable")

	rec := performAdmin(adminHandler.DeleteJSPlugin, "/", map[string]string{"id": id})
	if rec.Code != http.StatusConflict || adminErrorCode(t, rec) != "js_plugin_in_use" {
		t.Fatalf("delete plugin: %d %s", rec.Code, rec.Body.String())
	}
	if err := store.NewSiteRepository(st.DB()).Delete(ctx, pluginSite.ID); err != nil {
		t.Fatal(err)
	}
	rec = performAdmin(adminHandler.DeleteJSPlugin, "/", map[string]string{"id": id})
	if rec.Code != http.StatusOK {
		t.Fatalf("delete plugin after site delete: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := store.NewSiteRepository(st.DB()).GetByID(ctx, builtinSite.ID); err != nil {
		t.Fatalf("builtin site was removed: %v", err)
	}
}

func ensureCanonical(t *testing.T, st *store.Store, modelKey string) store.CanonicalModel {
	t.Helper()
	model, err := store.NewCanonicalModelRepository(st.DB()).Create(context.Background(), store.UpsertCanonicalModelParams{
		ModelKey: modelKey, DisplayName: modelKey, Status: "active", Category: "chat",
	})
	if err != nil {
		t.Fatal(err)
	}
	if model.ID == uuid.Nil {
		t.Fatal("canonical model id was not returned")
	}
	return model
}

func attachSiteModel(t *testing.T, st *store.Store, siteID uuid.UUID, canonical store.CanonicalModel) {
	t.Helper()
	ctx := context.Background()
	model, err := store.NewSiteModelRepository(st.DB()).Upsert(ctx, store.UpsertSiteModelParams{
		SiteID: siteID, UpstreamName: canonical.ModelKey, DisplayName: canonical.ModelKey, Status: "active",
		Capabilities: store.JSON(`{"supported_endpoint_types":["openai"]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.NewSiteModelRepository(st.DB()).UpdateCanonical(ctx, model.ID, canonical.ID, "manual", 100, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func candidateSiteTypes(t *testing.T, routerService *routeengine.Service, modelKey string) string {
	t.Helper()
	list, err := routerService.Candidates(context.Background(), routeengine.CandidateQuery{ModelKey: modelKey})
	if err != nil {
		t.Fatal(err)
	}
	types := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		types = append(types, strings.ToLower(strings.TrimSpace(item.Site.SiteType)))
	}
	return strings.Join(types, ",")
}

func assertModelCatalog(t *testing.T, h gateway.Handler, want map[string]string) {
	t.Helper()
	got := gatewayModelSiteTypes(t, h)
	for id, siteTypes := range want {
		if got[id] != siteTypes {
			t.Fatalf("model %s site types = %q, want %q; catalog=%v", id, got[id], siteTypes, got)
		}
	}
}

func assertModelAbsent(t *testing.T, h gateway.Handler, modelID string) {
	t.Helper()
	if _, ok := gatewayModelSiteTypes(t, h)[modelID]; ok {
		t.Fatalf("model %s still in catalog", modelID)
	}
}

func gatewayModelSiteTypes(t *testing.T, h gateway.Handler) map[string]string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req = req.WithContext(auth.WithAPIKey(req.Context(), store.APIKey{
		ID: uuid.New(), SitePolicy: "allow_all", ModelPolicy: "allow_all",
	}))
	rec := httptest.NewRecorder()
	h.Models(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("models: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data []struct {
			ID       string `json:"id"`
			Metadata struct {
				SiteTypes []string `json:"site_types"`
			} `json:"metadata"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, item := range body.Data {
		out[item.ID] = strings.Join(item.Metadata.SiteTypes, ",")
	}
	return out
}

func assertProviderStatus(t *testing.T, h admin.Handler, pluginSiteID, builtinSiteID uuid.UUID, want string) {
	t.Helper()
	rec := performAdmin(h.ListSites, "/api/admin/sites", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list sites: %d %s", rec.Code, rec.Body.String())
	}
	var listed struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	seenPlugin, seenBuiltin := false, false
	for _, item := range listed.Items {
		switch item["id"] {
		case pluginSiteID.String():
			seenPlugin = true
			if item["provider_status"] != want {
				t.Fatalf("list provider_status = %#v", item["provider_status"])
			}
		case builtinSiteID.String():
			seenBuiltin = true
			if _, ok := item["provider_status"]; ok {
				t.Fatalf("builtin list payload has provider_status: %#v", item["provider_status"])
			}
		}
	}
	if !seenPlugin || !seenBuiltin {
		t.Fatalf("list missing sites plugin=%v builtin=%v", seenPlugin, seenBuiltin)
	}

	rec = performAdmin(h.GetSite, "/api/admin/sites/"+pluginSiteID.String(), map[string]string{"siteID": pluginSiteID.String()})
	if rec.Code != http.StatusOK {
		t.Fatalf("get plugin site: %d %s", rec.Code, rec.Body.String())
	}
	var detail struct {
		Site map[string]any `json:"site"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Site["provider_status"] != want {
		t.Fatalf("detail provider_status = %#v", detail.Site["provider_status"])
	}
	rec = performAdmin(h.GetSite, "/api/admin/sites/"+builtinSiteID.String(), map[string]string{"siteID": builtinSiteID.String()})
	if rec.Code != http.StatusOK {
		t.Fatalf("get builtin site: %d %s", rec.Code, rec.Body.String())
	}
	detail = struct {
		Site map[string]any `json:"site"`
	}{}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(detail.Site)
	if _, ok := detail.Site["provider_status"]; ok || strings.Contains(string(raw), "provider_status") {
		t.Fatalf("builtin detail payload changed: %s", raw)
	}
}

func performAdmin(handler func(http.ResponseWriter, *http.Request), target string, vars map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if vars != nil {
		routeCtx := chi.NewRouteContext()
		for key, value := range vars {
			routeCtx.URLParams.Add(key, value)
		}
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func adminErrorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	return payload.Error.Code
}
