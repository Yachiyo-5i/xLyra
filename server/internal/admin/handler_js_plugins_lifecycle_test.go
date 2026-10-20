package admin

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/jsplugin"
	"xlyra/server/internal/store"
)

func TestJSPluginSiteTypeLifecycleHTTPErrors(t *testing.T) {
	st := openAdminJSPluginStore(t)
	manager := jsplugin.NewManager(st, nil)
	h := Handler{}.WithJSPlugins(manager)
	ctx := context.Background()
	const id = "acme.example"
	const siteType = "acme.example/example"
	const otherType = "acme.example/other"

	if _, err := manager.Upload(ctx, "", quotaProbePackage(t)); err != nil {
		t.Fatal(err)
	}
	rec := deletePlugin(t, h, "acme-probe")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete other kind: %d %s", rec.Code, rec.Body.String())
	}

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Upload(ctx, "", lifecycleProviderPackage(t, "1.0.0", siteType, "// v1\n")); err != nil {
		t.Fatal(err)
	}
	signed, err := jsplugin.AttachSignature(lifecycleProviderPackage(t, "1.1.0", siteType, "// v1.1\n"), priv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Upload(ctx, "", signed); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.AddTrustedKey(ctx, "", "acme", base64.StdEncoding.EncodeToString(pub)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.NewSiteRepository(st.DB()).Create(ctx, store.CreateSiteParams{
		Name: "Acme", Slug: "acme-" + uuid.NewString()[:8], SiteType: siteType,
		BaseURL: "https://api.example.com", Status: "active", Enabled: true, RoutingPriority: 1,
	}); err != nil {
		t.Fatal(err)
	}

	rec = deletePluginVersion(t, h, id, "1.0.0")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete non-last version: %d %s", rec.Code, rec.Body.String())
	}
	rec = deletePluginVersion(t, h, id, "1.1.0")
	body := decodeLifecycleError(t, rec)
	if rec.Code != http.StatusConflict || body.Error.Code != "js_plugin_in_use" || body.Error.SiteType != siteType || body.Error.Count != 1 {
		t.Fatalf("delete last version: %d %#v", rec.Code, body.Error)
	}

	rec = enablePluginVersion(t, h, id, "1.1.0", map[string]any{"confirm_hosts": []string{"api.example.com"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("enable same type: %d %s", rec.Code, rec.Body.String())
	}
	next, err := jsplugin.AttachSignature(lifecycleProviderPackage(t, "1.2.0", otherType, "// v1.2\n"), priv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Upload(ctx, "", next); err != nil {
		t.Fatal(err)
	}
	rec = enablePluginVersion(t, h, id, "1.2.0", map[string]any{"confirm_hosts": []string{"api.example.com"}})
	body = decodeLifecycleError(t, rec)
	if rec.Code != http.StatusConflict || body.Error.Code != "js_plugin_site_type_locked" || body.Error.From != siteType || body.Error.To != otherType || body.Error.Count != 1 {
		t.Fatalf("lock site type: %d %#v", rec.Code, body.Error)
	}

	rec = disablePlugin(t, h, id)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable: %d %s", rec.Code, rec.Body.String())
	}
	rec = deletePlugin(t, h, id)
	body = decodeLifecycleError(t, rec)
	if rec.Code != http.StatusConflict || body.Error.Code != "js_plugin_in_use" || body.Error.SiteType != siteType || body.Error.Count != 1 {
		t.Fatalf("delete plugin: %d %#v", rec.Code, body.Error)
	}
}

func lifecycleProviderPackage(t *testing.T, version, siteType, extra string) []byte {
	t.Helper()
	source := "export const meta = { apiVersion: 1, id: \"acme.example\", kind: \"site_provider\" };\n" + extra
	manifest := jsplugin.Manifest{
		ID: "acme.example", Name: "Example", Version: version, APIVersion: 1, HostAPI: 1,
		Kind: jsplugin.KindSiteProvider,
		SiteProvider: jsplugin.SiteProviderSection{
			SiteType:   siteType,
			Display:    jsplugin.SiteProviderDisplay{Name: jsplugin.LocalizedText{"zh": "示例", "en": "Example"}},
			Hosts:      []string{"api.example.com"},
			Credential: jsplugin.SiteProviderCredential{Model: "api_key"},
			Protocol: jsplugin.SiteProviderProtocol{
				Kind: "openai_chat", Path: "/v1/chat/completions", OfficialBaseURL: "https://api.example.com",
				DefaultEndpointTypes: []string{"openai"},
			},
		},
		SHA256: map[string]string{"plugin.js": jsplugin.HashSource(source)},
	}
	return mustPluginPackage(t, manifest, source, nil)
}

type lifecycleErrorBody struct {
	Error struct {
		Code     string `json:"code"`
		Message  string `json:"message"`
		SiteType string `json:"site_type"`
		Count    int    `json:"count"`
		From     string `json:"from"`
		To       string `json:"to"`
	} `json:"error"`
}

func decodeLifecycleError(t *testing.T, rec *httptestResponse) lifecycleErrorBody {
	t.Helper()
	var body lifecycleErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	return body
}

func deletePlugin(t *testing.T, h Handler, id string) *httptestResponse {
	t.Helper()
	req := adminRequestWithRouteVars(adminTestRequest(http.MethodDelete, "/", ""), map[string]string{"id": id})
	rec := adminPerform(h.DeleteJSPlugin, req)
	return &httptestResponse{Code: rec.Code, Body: rec.Body}
}

func deletePluginVersion(t *testing.T, h Handler, id, version string) *httptestResponse {
	t.Helper()
	req := adminRequestWithRouteVars(adminTestRequest(http.MethodDelete, "/", ""), map[string]string{"id": id, "version": version})
	rec := adminPerform(h.DeleteJSPluginVersion, req)
	return &httptestResponse{Code: rec.Code, Body: rec.Body}
}

func disablePlugin(t *testing.T, h Handler, id string) *httptestResponse {
	t.Helper()
	req := adminRequestWithRouteVars(adminTestRequest(http.MethodPost, "/", ""), map[string]string{"id": id})
	rec := adminPerform(h.DisableJSPlugin, req)
	return &httptestResponse{Code: rec.Code, Body: rec.Body}
}
