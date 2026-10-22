package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/jsplugin"
	"xlyra/server/internal/store"
)

func TestGetPluginIcon(t *testing.T) {
	st := openAdminJSPluginStore(t)
	manager := jsplugin.NewManager(st, &jsplugin.Catalog{})
	handler := Handler{}.WithJSPlugins(manager)
	authService := auth.NewService(st.DB(), adminTestMasterKey)
	ctx := context.Background()

	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 9}
	if _, err := manager.Upload(ctx, "", pluginIconPackage(t, "acme.example", "1.0.0", "assets/icon.png", png)); err != nil {
		t.Fatalf("upload icon: %v", err)
	}
	if _, err := manager.Upload(ctx, "", pluginIconPackage(t, "plain.example", "1.0.0", "", nil)); err != nil {
		t.Fatalf("upload plain: %v", err)
	}

	router := chi.NewRouter()
	router.Route("/api/v1", func(v1 chi.Router) {
		v1.Use(authService.RequireAdminSession)
		v1.Use(authService.RequireAdminCSRF)
		v1.Get("/plugin-icons/{pluginID}", handler.GetPluginIcon)
	})

	anon := httptest.NewRequest(http.MethodGet, "/api/v1/plugin-icons/acme.example", nil)
	anonRec := httptest.NewRecorder()
	router.ServeHTTP(anonRec, anon)
	if anonRec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, body=%s", anonRec.Code, anonRec.Body.String())
	}

	session, err := authService.BootstrapAdmin(ctx, "owner", "StrongPass123!", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}

	rec := pluginIconRequest(t, router, session.Token, "/api/v1/plugin-icons/acme.example", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), png) {
		t.Fatalf("body = %q", rec.Body.Bytes())
	}
	sum := pluginIconDigest(t, pluginIconPackage(t, "acme.example", "1.0.0", "assets/icon.png", png))
	etag := `"` + sum + `"`
	if rec.Header().Get("Content-Type") != "image/png" ||
		rec.Header().Get("ETag") != etag ||
		rec.Header().Get("Cache-Control") != "private, max-age=86400" ||
		rec.Header().Get("X-Content-Type-Options") != "nosniff" ||
		rec.Header().Get("Content-Security-Policy") != "default-src 'none'" {
		t.Fatalf("headers = %#v", rec.Header())
	}

	notModified := pluginIconRequest(t, router, session.Token, "/api/v1/plugin-icons/acme.example", etag)
	if notModified.Code != http.StatusNotModified || notModified.Body.Len() != 0 || notModified.Header().Get("ETag") != etag {
		t.Fatalf("not modified = %d %q etag=%q", notModified.Code, notModified.Body.Bytes(), notModified.Header().Get("ETag"))
	}

	for _, query := range []string{"?v=0000000000000000", "?v=ffffffffffffffff"} {
		again := pluginIconRequest(t, router, session.Token, "/api/v1/plugin-icons/acme.example"+query, "")
		if again.Code != http.StatusOK || !bytes.Equal(again.Body.Bytes(), png) || again.Header().Get("ETag") != etag {
			t.Fatalf("query %s = %d etag=%q", query, again.Code, again.Header().Get("ETag"))
		}
	}

	for _, path := range []string{"/api/v1/plugin-icons/plain.example", "/api/v1/plugin-icons/missing.example"} {
		missing := pluginIconRequest(t, router, session.Token, path, "")
		if missing.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, body=%s", path, missing.Code, missing.Body.String())
		}
		var body struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(missing.Body.Bytes(), &body); err != nil || body.Error.Code != "not_found" {
			t.Fatalf("%s error = %v %s", path, err, missing.Body.String())
		}
	}

	repo := store.NewJSPluginRepository(st.DB())
	if err := repo.SetVersionStatus(ctx, "acme.example", "1.0.0", store.JSPluginStatusEnabled, nil); err != nil {
		t.Fatal(err)
	}
	enabled := pluginIconRequest(t, router, session.Token, "/api/v1/plugin-icons/acme.example", "")
	if enabled.Code != http.StatusOK || !bytes.Equal(enabled.Body.Bytes(), png) || enabled.Header().Get("ETag") != etag {
		t.Fatalf("enabled status = %d etag=%q", enabled.Code, enabled.Header().Get("ETag"))
	}
	if err := manager.Disable(ctx, "acme.example"); err != nil {
		t.Fatal(err)
	}
	disabled := pluginIconRequest(t, router, session.Token, "/api/v1/plugin-icons/acme.example", "")
	if disabled.Code != http.StatusOK || !bytes.Equal(disabled.Body.Bytes(), png) || disabled.Header().Get("ETag") != etag {
		t.Fatalf("disabled status = %d etag=%q", disabled.Code, disabled.Header().Get("ETag"))
	}

	if err := manager.DeletePlugin(ctx, "acme.example"); err != nil {
		t.Fatal(err)
	}
	deleted := pluginIconRequest(t, router, session.Token, "/api/v1/plugin-icons/acme.example", "")
	if deleted.Code != http.StatusNotFound {
		t.Fatalf("deleted status = %d, body=%s", deleted.Code, deleted.Body.String())
	}
}

func pluginIconRequest(t *testing.T, router http.Handler, token, target, ifNoneMatch string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.AddCookie(&http.Cookie{Name: "xlyra_admin_session", Value: token})
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func pluginIconPackage(t *testing.T, id, version, iconName string, icon []byte) []byte {
	t.Helper()
	source := "export const meta = { apiVersion: 1, id: \"" + id + "\", kind: \"site_provider\" };\n"
	manifest := jsplugin.Manifest{
		ID: id, Name: id, Version: version, APIVersion: 1, HostAPI: 1, Kind: jsplugin.KindSiteProvider,
		SiteProvider: jsplugin.SiteProviderSection{
			SiteType: id + "/chat",
			Display: jsplugin.SiteProviderDisplay{
				Name: jsplugin.LocalizedText{"zh": "示例"},
				Icon: iconName,
			},
			Hosts:      []string{"api.example.com"},
			Credential: jsplugin.SiteProviderCredential{Model: "api_key"},
			Protocol: jsplugin.SiteProviderProtocol{
				Kind: "openai_chat", Path: "/v1/chat/completions", OfficialBaseURL: "https://api.example.com",
				DefaultEndpointTypes: []string{"openai"},
			},
		},
		SHA256: map[string]string{"plugin.js": jsplugin.HashSource(source)},
	}
	var extra map[string][]byte
	if iconName != "" {
		extra = map[string][]byte{iconName: icon}
	}
	return mustPluginPackage(t, manifest, source, extra)
}

func pluginIconDigest(t *testing.T, raw []byte) string {
	t.Helper()
	pkg, err := jsplugin.ReadPackage(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	return pkg.SHA256
}
