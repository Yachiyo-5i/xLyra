package admin

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/config"
	"xlyra/server/internal/jsplugin"
	"xlyra/server/internal/store"
)

func TestEnableJSPluginVersionConfirmHosts(t *testing.T) {
	st := openAdminJSPluginStore(t)
	manager := jsplugin.NewManager(st, nil)
	h := Handler{}.WithJSPlugins(manager)
	ctx := context.Background()

	if _, err := manager.Upload(ctx, "", quotaProbePackage(t)); err != nil {
		t.Fatalf("upload quota probe: %v", err)
	}
	if _, err := manager.Upload(ctx, "", siteProviderAdminPackage(t, "1.0.0", "", nil)); err != nil {
		t.Fatalf("upload unsigned site provider: %v", err)
	}
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := jsplugin.AttachSignature(siteProviderAdminPackage(t, "1.1.0", "// signed\n", priv), priv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Upload(ctx, "", signed); err != nil {
		t.Fatalf("upload signed site provider: %v", err)
	}
	if _, err := manager.AddTrustedKey(ctx, "", "acme", base64.StdEncoding.EncodeToString(pub)); err != nil {
		t.Fatal(err)
	}

	rec := enablePluginVersion(t, h, "acme-probe", "1.0.0", nil)
	body := decodeEnableError(t, rec)
	if rec.Code != http.StatusBadRequest || body.Error.Code != "js_plugin_confirm_required" || !strings.Contains(body.Error.Message, "confirm_untrusted") {
		t.Fatalf("quota probe without body: %d %s %s", rec.Code, body.Error.Code, body.Error.Message)
	}
	rec = enablePluginVersion(t, h, "acme-probe", "1.0.0", map[string]any{"confirm_hosts": []string{"api.example.com"}})
	body = decodeEnableError(t, rec)
	if rec.Code != http.StatusBadRequest || body.Error.Code != "js_plugin_confirm_required" || body.Error.Code == "js_plugin_hosts_required" {
		t.Fatalf("quota probe confirm_hosts: %d %s", rec.Code, body.Error.Code)
	}
	rec = enablePluginVersion(t, h, "acme-probe", "1.0.0", map[string]any{"confirm_untrusted": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("quota probe enable: %d %s", rec.Code, rec.Body.String())
	}
	var okBody struct {
		OK         bool  `json:"ok"`
		Generation int64 `json:"generation"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &okBody); err != nil || !okBody.OK || okBody.Generation == 0 {
		t.Fatalf("quota probe success: %v %s", err, rec.Body.String())
	}

	rec = enablePluginVersion(t, h, "acme.example", "1.0.0", map[string]any{"confirm_untrusted": true, "confirm_hosts": []string{"api.example.com"}})
	body = decodeEnableError(t, rec)
	if rec.Code != http.StatusForbidden || body.Error.Code != "js_plugin_untrusted_not_allowed" || body.Error.Message != "site_provider 插件必须由受信密钥签名,confirm_untrusted 不能绕过" {
		t.Fatalf("unsigned site provider: %d %s %s", rec.Code, body.Error.Code, body.Error.Message)
	}
	rec = enablePluginVersion(t, h, "acme.example", "1.1.0", map[string]any{})
	body = decodeEnableError(t, rec)
	if rec.Code != http.StatusBadRequest || body.Error.Code != "js_plugin_hosts_required" || strings.Join(body.Error.Hosts, ",") != "api.example.com" {
		t.Fatalf("missing hosts: %d %s %#v", rec.Code, body.Error.Code, body.Error.Hosts)
	}
	rec = enablePluginVersion(t, h, "acme.example", "1.1.0", map[string]any{"confirm_hosts": []string{"other.example.com"}})
	body = decodeEnableError(t, rec)
	if rec.Code != http.StatusBadRequest || body.Error.Code != "js_plugin_hosts_required" || strings.Join(body.Error.Hosts, ",") != "api.example.com" {
		t.Fatalf("wrong hosts: %d %s %#v", rec.Code, body.Error.Code, body.Error.Hosts)
	}
	rec = enablePluginVersion(t, h, "acme.example", "1.1.0", map[string]any{"confirm_hosts": []string{"api.example.com"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("confirmed hosts: %d %s", rec.Code, rec.Body.String())
	}
}

func enablePluginVersion(t *testing.T, h Handler, id, version string, body any) *httptestResponse {
	t.Helper()
	var req *http.Request
	if body == nil {
		req = adminTestRequest(http.MethodPost, "/", "")
	} else {
		req = adminJSONRequest(t, http.MethodPost, "/", body)
	}
	req = adminRequestWithRouteVars(req, map[string]string{"id": id, "version": version})
	rec := adminPerform(h.EnableJSPluginVersion, req)
	return &httptestResponse{Code: rec.Code, Body: rec.Body}
}

type httptestResponse struct {
	Code int
	Body interface {
		Bytes() []byte
		String() string
	}
}

type enableErrorBody struct {
	Error struct {
		Code    string   `json:"code"`
		Message string   `json:"message"`
		Hosts   []string `json:"hosts"`
	} `json:"error"`
}

func decodeEnableError(t *testing.T, rec *httptestResponse) enableErrorBody {
	t.Helper()
	var body enableErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	return body
}

func quotaProbePackage(t *testing.T) []byte {
	t.Helper()
	source := "export const meta = { apiVersion: 1, id: \"acme-probe\", kind: \"quota_probe\" };\nexport function probe() { return { error: \"empty\" }; }\n"
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

func siteProviderAdminPackage(t *testing.T, version, extra string, _ ed25519.PrivateKey) []byte {
	t.Helper()
	source := "export const meta = { apiVersion: 1, id: \"acme.example\", kind: \"site_provider\" };\n" + extra
	manifest := jsplugin.Manifest{
		ID: "acme.example", Name: "Example", Version: version, APIVersion: 1, HostAPI: 1,
		Kind: jsplugin.KindSiteProvider,
		SiteProvider: jsplugin.SiteProviderSection{
			SiteType:   "acme.example/example",
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

func mustPluginPackage(t *testing.T, manifest jsplugin.Manifest, source string, extra map[string][]byte) []byte {
	t.Helper()
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{"manifest.json": encoded, "plugin.js": []byte(source)}
	for name, body := range extra {
		files[name] = body
	}
	raw, err := jsplugin.BuildPackage(files)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func openAdminJSPluginStore(t *testing.T) *store.Store {
	t.Helper()
	cfg := config.Config{
		AppEnv: "test", DBConnectTimeout: 2 * time.Second, DBMaxConns: 4,
		PostgresDSN: strings.TrimSpace(os.Getenv("POSTGRES_DSN")),
		DBHost:      adminTestEnv("DB_HOST", "127.0.0.1"), DBPort: 5432,
		DBName: adminTestEnv("DB_NAME", "xlyra"), DBUser: adminTestEnv("DB_USER", "postgres"),
		DBPassword: adminTestEnv("DB_PASSWORD", "postgres"), DBSSLMode: adminTestEnv("DB_SSLMODE", "disable"),
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
	schema := "xlyra_admin_jsplugin_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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

func adminTestEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
