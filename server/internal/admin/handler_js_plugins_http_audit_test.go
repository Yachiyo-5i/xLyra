package admin

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/jsplugin"
	"xlyra/server/internal/store"
)

const auditPluginSecret = "TOKEN-plugin-secret-9f3c"

func TestJSPluginRoutesRequireAdminSession(t *testing.T) {
	authService := auth.NewService(nil, adminTestMasterKey)
	router := jsPluginAuditRouter(Handler{}, authService)
	routes := []struct {
		method string
		target string
	}{
		{http.MethodGet, "/api/v1/js-plugins"},
		{http.MethodPost, "/api/v1/js-plugins"},
		{http.MethodGet, "/api/v1/js-plugins/acme.example"},
		{http.MethodPost, "/api/v1/js-plugins/acme.example/versions/1.0.0/enable"},
		{http.MethodPost, "/api/v1/js-plugins/acme.example/disable"},
		{http.MethodDelete, "/api/v1/js-plugins/acme.example"},
		{http.MethodDelete, "/api/v1/js-plugins/acme.example/versions/1.0.0"},
		{http.MethodPost, "/api/v1/js-plugins/acme.example/try"},
	}
	for _, route := range routes {
		t.Run(route.method+" "+route.target, func(t *testing.T) {
			req := httptest.NewRequest(route.method, route.target, strings.NewReader(`{}`))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			assertAuditPluginError(t, rec, http.StatusUnauthorized, "unauthorized")
		})
	}
}

func TestJSPluginHandlersUnavailableWithoutAManager(t *testing.T) {
	h := Handler{}
	cases := []struct {
		name   string
		method string
		fn     func(http.ResponseWriter, *http.Request)
		vars   map[string]string
	}{
		{name: "list", method: http.MethodGet, fn: h.ListJSPlugins},
		{name: "upload", method: http.MethodPost, fn: h.UploadJSPlugin},
		{name: "get", method: http.MethodGet, fn: h.GetJSPlugin, vars: map[string]string{"id": "acme.example"}},
		{name: "try", method: http.MethodPost, fn: h.TryJSPlugin, vars: map[string]string{"id": "acme.example"}},
		{name: "enable", method: http.MethodPost, fn: h.EnableJSPluginVersion, vars: map[string]string{"id": "acme.example", "version": "1.0.0"}},
		{name: "disable", method: http.MethodPost, fn: h.DisableJSPlugin, vars: map[string]string{"id": "acme.example"}},
		{name: "delete", method: http.MethodDelete, fn: h.DeleteJSPlugin, vars: map[string]string{"id": "acme.example"}},
		{name: "delete version", method: http.MethodDelete, fn: h.DeleteJSPluginVersion, vars: map[string]string{"id": "acme.example", "version": "1.0.0"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := adminRequestWithRouteVars(adminTestRequest(tc.method, "/", ""), tc.vars)
			rec := adminPerform(tc.fn, req)
			assertAuditPluginError(t, rec, http.StatusServiceUnavailable, "js_plugin_unavailable")
		})
	}
}

func TestJSPluginHTTPUploadListGetTryAndLifecycle(t *testing.T) {
	st := openAdminJSPluginStore(t)
	ctx := context.Background()
	manager := jsplugin.NewManager(st, nil)
	authService := auth.NewService(st.DB(), adminTestMasterKey)
	h := Handler{auth: authService}.WithTrafficFlowStore(st).WithJSPlugins(manager)
	router := jsPluginAuditRouter(h, authService)
	session, err := authService.BootstrapAdmin(ctx, "owner", "StrongPass123!", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	csrf := authService.CSRFTokenForSession(session.Token)
	var seen []string
	call := func(method, target string, body []byte, contentType string) *httptest.ResponseRecorder {
		t.Helper()
		rec := jsPluginAuthed(t, router, session.Token, csrf, method, target, body, contentType)
		seen = append(seen, rec.Body.String())
		return rec
	}

	seed := bytes.Repeat([]byte{0x42}, ed25519.SeedSize)
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	privText := base64.StdEncoding.EncodeToString(priv)
	seedText := base64.StdEncoding.EncodeToString(seed)

	assertAuditPluginError(t, call(http.MethodPost, "/api/v1/js-plugins", []byte("not-a-zip"), "application/octet-stream"), http.StatusBadRequest, "js_plugin_package_invalid")
	assertAuditPluginError(t, call(http.MethodPost, "/api/v1/js-plugins", bytes.Repeat([]byte{0xab}, jsplugin.MaxPackageBytes+1), "application/octet-stream"), http.StatusRequestEntityTooLarge, "request_body_too_large")
	badManifest, err := jsplugin.BuildPackage(map[string][]byte{
		"manifest.json": []byte("{"),
		"plugin.js":     []byte("export const meta = {};\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertAuditPluginError(t, call(http.MethodPost, "/api/v1/js-plugins", badManifest, "application/octet-stream"), http.StatusBadRequest, "js_plugin_package_invalid")

	probe := auditQuotaProbePackage(t, auditPluginSecret)
	uploaded := call(http.MethodPost, "/api/v1/js-plugins", probe, "application/octet-stream")
	if uploaded.Code != http.StatusOK || !strings.Contains(uploaded.Body.String(), `"plugin_id":"acme-probe"`) || !strings.Contains(uploaded.Body.String(), `"version":"1.0.0"`) {
		t.Fatalf("upload = %d %s", uploaded.Code, uploaded.Body.String())
	}
	other := auditQuotaProbePackage(t, "other-source")
	assertAuditPluginError(t, call(http.MethodPost, "/api/v1/js-plugins", other, "application/octet-stream"), http.StatusConflict, "js_plugin_version_conflict")

	listed := call(http.MethodGet, "/api/v1/js-plugins", nil, "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"id":"acme-probe"`) || !strings.Contains(listed.Body.String(), `"kind":"quota_probe"`) {
		t.Fatalf("list = %d %s", listed.Code, listed.Body.String())
	}
	got := call(http.MethodGet, "/api/v1/js-plugins/acme-probe", nil, "")
	if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"version":"1.0.0"`) || !strings.Contains(got.Body.String(), `"signed":false`) {
		t.Fatalf("get = %d %s", got.Code, got.Body.String())
	}
	assertAuditPluginError(t, call(http.MethodGet, "/api/v1/js-plugins/missing.example", nil, ""), http.StatusNotFound, "not_found")
	assertAuditPluginError(t, call(http.MethodPost, "/api/v1/js-plugins/acme-probe/try", nil, ""), http.StatusBadRequest, "version_required")
	assertAuditPluginError(t, call(http.MethodPost, "/api/v1/js-plugins/acme-probe/try", []byte("{"), "application/json"), http.StatusBadRequest, "invalid_json")
	tried := call(http.MethodPost, "/api/v1/js-plugins/acme-probe/try", []byte(`{"version":"1.0.0"}`), "application/json")
	if tried.Code != http.StatusOK || !strings.Contains(tried.Body.String(), `"ok":true`) {
		t.Fatalf("try = %d %s", tried.Code, tried.Body.String())
	}
	assertAuditPluginError(t, call(http.MethodPost, "/api/v1/js-plugins/missing.example/try", []byte(`{"version":"9.9.9"}`), "application/json"), http.StatusBadRequest, "js_plugin_try_failed")

	if _, err := manager.Upload(ctx, "", lifecycleProviderPackage(t, "1.0.0", "acme.example/example", "// unsigned\n")); err != nil {
		t.Fatal(err)
	}
	signed, err := jsplugin.AttachSignature(lifecycleProviderPackage(t, "1.1.0", "acme.example/example", "// signed\n"), priv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Upload(ctx, "", signed); err != nil {
		t.Fatal(err)
	}
	renamed, err := jsplugin.AttachSignature(lifecycleProviderPackage(t, "1.2.0", "acme.example/other", "// renamed\n"), priv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Upload(ctx, "", renamed); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.AddTrustedKey(ctx, "", "acme", base64.StdEncoding.EncodeToString(pub)); err != nil {
		t.Fatal(err)
	}
	hosts := []byte(`{"confirm_untrusted":true,"confirm_hosts":["api.example.com"]}`)
	assertAuditPluginError(t, call(http.MethodPost, "/api/v1/js-plugins/acme.example/versions/1.0.0/enable", hosts, "application/json"), http.StatusForbidden, "js_plugin_untrusted_not_allowed")
	hostsRequired := call(http.MethodPost, "/api/v1/js-plugins/acme.example/versions/1.1.0/enable", []byte(`{}`), "application/json")
	assertAuditPluginError(t, hostsRequired, http.StatusBadRequest, "js_plugin_hosts_required")
	if !strings.Contains(hostsRequired.Body.String(), "api.example.com") {
		t.Fatalf("hosts error = %s", hostsRequired.Body.String())
	}
	if _, err := store.NewSiteRepository(st.DB()).Create(ctx, store.CreateSiteParams{
		Name: "Acme", Slug: "acme-" + uuid.NewString()[:8], SiteType: "acme.example/example",
		BaseURL: "https://api.example.com", Status: "active", Enabled: true, RoutingPriority: 1,
	}); err != nil {
		t.Fatal(err)
	}
	enabled := call(http.MethodPost, "/api/v1/js-plugins/acme.example/versions/1.1.0/enable", []byte(`{"confirm_hosts":["api.example.com"]}`), "application/json")
	if enabled.Code != http.StatusOK || !strings.Contains(enabled.Body.String(), `"ok":true`) {
		t.Fatalf("enable = %d %s", enabled.Code, enabled.Body.String())
	}
	assertAuditPluginError(t, call(http.MethodPost, "/api/v1/js-plugins/acme.example/versions/1.2.0/enable", []byte(`{"confirm_hosts":["api.example.com"]}`), "application/json"), http.StatusConflict, "js_plugin_site_type_locked")
	assertAuditPluginError(t, call(http.MethodDelete, "/api/v1/js-plugins/acme.example/versions/1.1.0", nil, ""), http.StatusBadRequest, "js_plugin_version_enabled")
	disabled := call(http.MethodPost, "/api/v1/js-plugins/acme.example/disable", nil, "")
	if disabled.Code != http.StatusOK || !strings.Contains(disabled.Body.String(), `"ok":true`) {
		t.Fatalf("disable = %d %s", disabled.Code, disabled.Body.String())
	}
	assertAuditPluginError(t, call(http.MethodDelete, "/api/v1/js-plugins/acme.example", nil, ""), http.StatusConflict, "js_plugin_in_use")
	assertAuditPluginError(t, call(http.MethodDelete, "/api/v1/js-plugins/missing.example", nil, ""), http.StatusNotFound, "not_found")
	removed := call(http.MethodDelete, "/api/v1/js-plugins/acme-probe", nil, "")
	if removed.Code != http.StatusOK {
		t.Fatalf("delete probe = %d %s", removed.Code, removed.Body.String())
	}
	assertAuditPluginError(t, call(http.MethodGet, "/api/v1/js-plugins/acme-probe", nil, ""), http.StatusNotFound, "not_found")

	for i, body := range seen {
		if strings.Contains(body, auditPluginSecret) || strings.Contains(body, privText) || strings.Contains(body, seedText) {
			t.Fatalf("response %d contains package or signing material: %s", i, body)
		}
	}
	logs, _, err := store.NewAdminAuditLogRepository(st.DB()).List(ctx, store.AdminAuditLogFilters{Page: 1, PageSize: 200})
	if err != nil {
		t.Fatal(err)
	}
	sawUpload := false
	for _, item := range logs {
		text := item.Action + " " + item.ErrorCode + " " + string(item.Metadata)
		if strings.Contains(text, auditPluginSecret) || strings.Contains(text, privText) || strings.Contains(text, seedText) {
			t.Fatalf("audit contains package or signing material: %s", text)
		}
		if item.Action == "js_plugin.upload" && item.Success && strings.Contains(string(item.Metadata), "package_sha256") {
			sawUpload = true
		}
	}
	if !sawUpload {
		t.Fatalf("upload was not audited: %#v", logs)
	}
}

func jsPluginAuditRouter(h Handler, authService *auth.Service) http.Handler {
	router := chi.NewRouter()
	router.Route("/api/v1", func(v1 chi.Router) {
		v1.Use(authService.RequireAdminSession)
		v1.Use(authService.RequireAdminCSRF)
		v1.Get("/js-plugins", h.ListJSPlugins)
		v1.Post("/js-plugins", h.UploadJSPlugin)
		v1.Get("/js-plugins/{id}", h.GetJSPlugin)
		v1.Post("/js-plugins/{id}/versions/{version}/enable", h.EnableJSPluginVersion)
		v1.Post("/js-plugins/{id}/disable", h.DisableJSPlugin)
		v1.Delete("/js-plugins/{id}", h.DeleteJSPlugin)
		v1.Delete("/js-plugins/{id}/versions/{version}", h.DeleteJSPluginVersion)
		v1.Post("/js-plugins/{id}/try", h.TryJSPlugin)
	})
	return router
}

func jsPluginAuthed(t *testing.T, router http.Handler, token, csrf, method, target string, body []byte, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.AddCookie(&http.Cookie{Name: "xlyra_admin_session", Value: token})
	if method != http.MethodGet && method != http.MethodHead {
		req.Header.Set(auth.CSRFHeaderName, csrf)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func assertAuditPluginError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, status, rec.Body.String())
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error.Code != code {
		t.Fatalf("error = %v %s, want %s", err, rec.Body.String(), code)
	}
}

func auditQuotaProbePackage(t *testing.T, marker string) []byte {
	t.Helper()
	source := "export const meta = { apiVersion: 1, id: \"acme-probe\", kind: \"quota_probe\" };\nexport function probe() { return { error: \"empty\" }; }\n// " + marker + "\n"
	manifest := jsplugin.Manifest{
		ID: "acme-probe", Name: "Acme", Version: "1.0.0", APIVersion: 1, HostAPI: 1,
		Kind:       jsplugin.KindQuotaProbe,
		QuotaProbe: jsplugin.QuotaProbeSection{BaseURLMode: "as_is"},
		SHA256:     map[string]string{"plugin.js": jsplugin.HashSource(source)},
	}
	return mustPluginPackage(t, manifest, source, map[string][]byte{
		"fixtures/ok.json": []byte(`{"name":"ok","ctx":{"siteType":"newapi","baseURL":"https://example.com","credentialType":"api_key"},"responses":[],"expect":{"error":"empty"}}`),
	})
}
