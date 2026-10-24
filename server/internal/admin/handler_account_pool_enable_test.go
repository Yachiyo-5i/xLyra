package admin

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"

	"xlyra/server/internal/jsplugin"
)

func TestEnableAccountPoolPlugin(t *testing.T) {
	st := openAdminJSPluginStore(t)
	manager := jsplugin.NewManager(st, nil)
	h := Handler{}.WithJSPlugins(manager)
	ctx := context.Background()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	source := "export const meta = { apiVersion: 1, id: \"acme.pool\", kind: \"site_provider\" };\nexport function summarizeAccount() { return {}; }\nexport function startLogin() { return {}; }\nexport function pollLogin() { return {}; }\n"
	manifest := jsplugin.Manifest{
		ID: "acme.pool", Name: "Pool", Version: "1.0.0", APIVersion: 1, HostAPI: 1, Kind: jsplugin.KindSiteProvider,
		SiteProvider: jsplugin.SiteProviderSection{
			SiteType: "acme.pool/chat",
			Display:  jsplugin.SiteProviderDisplay{Name: jsplugin.LocalizedText{"zh": "账号池"}},
			Hosts:    []string{"api.example.cn"},
			Credential: jsplugin.SiteProviderCredential{
				Model: "account_pool",
				Fields: []jsplugin.SiteProviderField{
					{Name: "access_token", Secret: true},
					{Name: "realm"},
				},
				Identity: jsplugin.Sources("field.realm"),
			},
			Protocol: jsplugin.SiteProviderProtocol{
				Kind: "openai_chat", Path: "/v1/chat/completions", OfficialBaseURL: "https://api.example.cn",
				DefaultEndpointTypes: []string{"openai"},
			},
			Regions:       []jsplugin.SiteProviderRegion{{ID: "cn", BaseURL: "https://api.example.cn"}},
			DefaultRegion: "cn",
			Login: &jsplugin.SiteProviderLogin{
				Kind:        "custom_poll",
				SecretPaths: map[string]string{"token": "access_token"},
			},
			Refresh: &jsplugin.SiteProviderRefresh{Kind: "none"},
		},
		SHA256: map[string]string{"plugin.js": jsplugin.HashSource(source)},
	}
	raw, err := jsplugin.AttachSignature(mustPluginPackage(t, manifest, source, nil), priv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Upload(ctx, "", raw); err != nil {
		t.Fatalf("upload: %v", err)
	}
	if _, err := manager.AddTrustedKey(ctx, "", "acme", base64.StdEncoding.EncodeToString(pub)); err != nil {
		t.Fatal(err)
	}
	rec := enablePluginVersion(t, h, "acme.pool", "1.0.0", map[string]any{"confirm_hosts": []string{"api.example.cn"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("enable = %d %s", rec.Code, rec.Body.String())
	}
	var okBody struct {
		OK         bool  `json:"ok"`
		Generation int64 `json:"generation"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &okBody); err != nil || !okBody.OK || okBody.Generation == 0 {
		t.Fatalf("enable success: %v %s", err, rec.Body.String())
	}
}
