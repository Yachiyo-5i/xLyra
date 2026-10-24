package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/accountauth"
	"xlyra/server/internal/adapter"
	"xlyra/server/internal/jsplugin"
	sitepkg "xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

const pluginAccountHTTPToken = "TOKEN-plugin-9f3c"

func TestPluginAccountHTTPFlowHidesSecrets(t *testing.T) {
	st := openAdminJSPluginStore(t)
	ctx := context.Background()
	const siteType = "acme.example/p38http"
	sitepkg.ClearPluginSiteTypes()
	t.Cleanup(sitepkg.ClearPluginSiteTypes)
	if err := sitepkg.SetPluginSiteTypes([]sitepkg.PluginSiteType{{
		Descriptor: sitepkg.SiteTypeDescriptor{
			SiteType:           siteType,
			DisplayName:        "P38 HTTP",
			CredentialKey:      siteType,
			ShowInCreateDialog: true,
		},
		PluginID: "acme.example",
		Enabled:  true,
		Hosts:    []string{"api.example.cn", "api.example.com"},
		Pool:     pluginAccountHTTPSpec(),
	}}); err != nil {
		t.Fatal(err)
	}
	runtime := &fakePluginAccountHTTPRuntime{}
	svc := sitepkg.NewService(st, adminTestMasterKey)
	if err := svc.SetPluginAccountRuntimes(map[string]sitepkg.PluginAccountRuntime{siteType: runtime}); err != nil {
		t.Fatal(err)
	}
	created, err := store.NewSiteRepository(st.DB()).Create(ctx, store.CreateSiteParams{
		Name:            "P38 HTTP",
		Slug:            "p38http-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12],
		SiteType:        siteType,
		BaseURL:         "https://api.example.cn",
		Status:          "active",
		Enabled:         true,
		RoutingPriority: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(nil, nil, svc, nil, nil, nil, nil, nil, nil, nil, nil)
	siteVars := map[string]string{"siteID": created.ID.String()}

	start := adminPerform(h.StartAccountLogin, adminRequestWithRouteParams(http.MethodPost, "/sites/"+created.ID.String()+"/accounts/login", `{"region":"intl"}`, siteVars))
	adminAssertStatus(t, start, http.StatusOK)
	assertPluginHTTPNoSecret(t, "login start", start.Body.String())
	var started struct {
		Login sitepkg.AccountLoginStart `json:"login"`
	}
	if err := json.Unmarshal(start.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	if started.Login.Kind != "custom_poll" || started.Login.Session == "" || started.Login.AuthURL == "" || started.Login.Region != "intl" {
		t.Fatalf("login = %#v", started.Login)
	}

	pending := adminPerform(h.PollAccountLogin, adminRequestWithRouteParams(http.MethodPost, "/sites/"+created.ID.String()+"/accounts/login/poll", `{"session":"`+started.Login.Session+`"}`, siteVars))
	adminAssertStatus(t, pending, http.StatusOK)
	assertPluginHTTPNoSecret(t, "pending poll", pending.Body.String())

	done := adminPerform(h.PollAccountLogin, adminRequestWithRouteParams(http.MethodPost, "/sites/"+created.ID.String()+"/accounts/login/poll", `{"session":"`+started.Login.Session+`"}`, siteVars))
	adminAssertStatus(t, done, http.StatusOK)
	assertPluginHTTPNoSecret(t, "complete poll", done.Body.String())
	credentialID := pluginHTTPCredentialID(t, done.Body.Bytes())

	listed := adminPerform(h.ListAccounts, adminRequestWithRouteParams(http.MethodGet, "/sites/"+created.ID.String()+"/accounts", "", siteVars))
	adminAssertStatus(t, listed, http.StatusOK)
	assertPluginHTTPNoSecret(t, "list", listed.Body.String())
	if !strings.Contains(listed.Body.String(), `"identity":"user-1"`) || !strings.Contains(listed.Body.String(), `"remark":"pro"`) {
		t.Fatalf("list = %s", listed.Body.String())
	}

	vars := map[string]string{"siteID": created.ID.String(), "credentialID": credentialID}
	refreshed := adminPerform(h.RefreshAccount, adminRequestWithRouteParams(http.MethodPost, "/accounts/refresh", "", vars))
	adminAssertStatus(t, refreshed, http.StatusOK)
	assertPluginHTTPNoSecret(t, "refresh", refreshed.Body.String())

	body := `{"files":[{"name":"accounts.json","content":"[{\"subject\":\"user-1\",\"realm\":\"intl\",\"access_token\":\"ACCESS-` + pluginAccountHTTPToken + `-imp\",\"refresh_token\":\"REFRESH-` + pluginAccountHTTPToken + `-imp\"},{\"subject\":\"user-2\",\"realm\":\"cn\",\"access_token\":\"ACCESS-` + pluginAccountHTTPToken + `-u2\",\"refresh_token\":\"REFRESH-` + pluginAccountHTTPToken + `-u2\"}]"}]}`
	imported := adminPerform(h.ImportAccounts, adminRequestWithRouteVars(adminJSONRequest(t, http.MethodPost, "/accounts/import", json.RawMessage(body)), siteVars))
	adminAssertStatus(t, imported, http.StatusOK)
	assertPluginHTTPNoSecret(t, "import", imported.Body.String())
	var importBody struct {
		Meta struct {
			Total    int `json:"total"`
			Imported int `json:"imported"`
			Failed   int `json:"failed"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(imported.Body.Bytes(), &importBody); err != nil {
		t.Fatal(err)
	}
	if importBody.Meta.Total != 2 || importBody.Meta.Imported != 2 || importBody.Meta.Failed != 0 {
		t.Fatalf("import meta = %#v body %s", importBody.Meta, imported.Body.String())
	}

	listed = adminPerform(h.ListAccounts, adminRequestWithRouteParams(http.MethodGet, "/accounts", "", siteVars))
	adminAssertStatus(t, listed, http.StatusOK)
	assertPluginHTTPNoSecret(t, "list after import", listed.Body.String())
	user2 := pluginHTTPCredentialByIdentity(t, listed.Body.Bytes(), "user-2")

	runtime.mu.Lock()
	runtime.failNext = true
	runtime.mu.Unlock()
	failed := adminPerform(h.RefreshAccount, adminRequestWithRouteParams(http.MethodPost, "/accounts/refresh", "", map[string]string{
		"siteID": created.ID.String(), "credentialID": user2,
	}))
	adminAssertStatus(t, failed, http.StatusBadRequest)
	assertPluginHTTPNoSecret(t, "refresh error", failed.Body.String())
	if !strings.Contains(failed.Body.String(), "[redacted]") {
		t.Fatalf("refresh error = %s", failed.Body.String())
	}

	listed = adminPerform(h.ListAccounts, adminRequestWithRouteParams(http.MethodGet, "/accounts", "", siteVars))
	adminAssertStatus(t, listed, http.StatusOK)
	assertPluginHTTPNoSecret(t, "list after reauth", listed.Body.String())
	if !strings.Contains(listed.Body.String(), `"reauth_required":true`) || !strings.Contains(listed.Body.String(), "[redacted]") {
		t.Fatalf("reauth list = %s", listed.Body.String())
	}

	for _, id := range []string{credentialID, user2} {
		deleted := adminPerform(h.DeleteAccount, adminRequestWithRouteParams(http.MethodDelete, "/accounts/"+id, "", map[string]string{
			"siteID": created.ID.String(), "credentialID": id,
		}))
		adminAssertStatus(t, deleted, http.StatusNoContent)
		assertPluginHTTPNoSecret(t, "delete", deleted.Body.String())
	}
	listed = adminPerform(h.ListAccounts, adminRequestWithRouteParams(http.MethodGet, "/accounts", "", siteVars))
	adminAssertStatus(t, listed, http.StatusOK)
	assertPluginHTTPNoSecret(t, "list after delete", listed.Body.String())
	if strings.Contains(listed.Body.String(), credentialID) || strings.Contains(listed.Body.String(), user2) {
		t.Fatalf("deleted accounts still listed: %s", listed.Body.String())
	}
}

type fakePluginAccountHTTPRuntime struct {
	mu        sync.Mutex
	polls     int
	rotations int
	failNext  bool
}

func (f *fakePluginAccountHTTPRuntime) StartLogin(context.Context, map[string]string) (sitepkg.PluginLoginStart, error) {
	return sitepkg.PluginLoginStart{
		ID:              "sess-p38-http",
		Kind:            "custom_poll",
		Status:          "pending",
		UserCode:        "CODE-1",
		VerificationURL: "https://api.example.com/activate",
		Interval:        2,
	}, nil
}

func (f *fakePluginAccountHTTPRuntime) PollLogin(context.Context, string) (sitepkg.PluginLoginPoll, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.polls++
	if f.polls == 1 {
		return sitepkg.PluginLoginPoll{Status: "pending", Interval: 2}, nil
	}
	return sitepkg.PluginLoginPoll{
		Status:   "complete",
		Identity: "user-1",
		Credential: map[string]string{
			"access_token":  "ACCESS-" + pluginAccountHTTPToken,
			"refresh_token": "REFRESH-" + pluginAccountHTTPToken,
			"subject":       "user-1",
			"realm":         "intl",
		},
	}, nil
}

func (f *fakePluginAccountHTTPRuntime) Refresh(_ context.Context, fields map[string]string) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNext {
		f.failNext = false
		return nil, fmt.Errorf("upstream rejected %s: %w", fields["access_token"], accountauth.ErrReauthorizationRequired)
	}
	f.rotations++
	out := map[string]string{}
	for key, value := range fields {
		out[key] = value
	}
	out["access_token"] = fmt.Sprintf("ACCESS-%s-%d", pluginAccountHTTPToken, f.rotations)
	return out, nil
}

func (f *fakePluginAccountHTTPRuntime) Summarize(context.Context, string, map[string]string) (sitepkg.PluginAccountSummary, error) {
	remaining := 12.4
	limit := 20.0
	return sitepkg.PluginAccountSummary{
		Status: "ok",
		Remark: "pro",
		Entries: []sitepkg.QuotaProbeEntry{{
			Label:     "credits",
			Remaining: &remaining,
			Limit:     &limit,
			Unit:      "credits",
		}},
	}, nil
}

func (f *fakePluginAccountHTTPRuntime) ListModels(context.Context, map[string]string) ([]adapter.Model, error) {
	return nil, nil
}

func (f *fakePluginAccountHTTPRuntime) ParseImport(raw []byte) ([]sitepkg.PluginImportedAccount, error) {
	var rows []map[string]string
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, errors.New("account import file is not valid JSON")
	}
	out := make([]sitepkg.PluginImportedAccount, 0, len(rows))
	for _, row := range rows {
		out = append(out, sitepkg.PluginImportedAccount{Fields: row})
	}
	return out, nil
}

func pluginAccountHTTPSpec() *jsplugin.AccountPoolSpec {
	return &jsplugin.AccountPoolSpec{
		Credential: jsplugin.SiteProviderCredential{
			Model: "account_pool",
			Fields: []jsplugin.SiteProviderField{
				{Name: "access_token", Secret: true},
				{Name: "refresh_token", Secret: true},
				{Name: "subject"},
				{Name: "realm"},
			},
			AccessTokenField:  "access_token",
			RefreshTokenField: "refresh_token",
			Identity:          jsplugin.Sources("field.subject"),
		},
		Regions: []jsplugin.SiteProviderRegion{
			{ID: "cn", Label: jsplugin.LocalizedText{"zh": "中国", "en": "China"}, BaseURL: "https://api.example.cn"},
			{ID: "intl", Label: jsplugin.LocalizedText{"zh": "国际", "en": "International"}, BaseURL: "https://api.example.com", Realms: []string{"intl"}},
		},
		DefaultRegion:  "cn",
		Login:          jsplugin.SiteProviderLogin{Kind: "custom_poll"},
		Refresh:        jsplugin.SiteProviderRefresh{Kind: "custom_steps"},
		Import:         jsplugin.SiteProviderImport{Enabled: true},
		QuotaAggregate: "sum",
	}
}

func pluginHTTPCredentialID(t *testing.T, body []byte) string {
	t.Helper()
	var payload struct {
		Login struct {
			Account struct {
				CredentialID string `json:"credential_id"`
			} `json:"account"`
		} `json:"login"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if _, err := uuid.Parse(payload.Login.Account.CredentialID); err != nil {
		t.Fatalf("credential id %q: %v", payload.Login.Account.CredentialID, err)
	}
	return payload.Login.Account.CredentialID
}

func pluginHTTPCredentialByIdentity(t *testing.T, body []byte, identity string) string {
	t.Helper()
	var payload struct {
		Items []struct {
			CredentialID string `json:"credential_id"`
			Identity     string `json:"identity"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	for _, item := range payload.Items {
		if item.Identity == identity {
			return item.CredentialID
		}
	}
	t.Fatalf("missing identity %s in %s", identity, body)
	return ""
}

func assertPluginHTTPNoSecret(t *testing.T, label, body string) {
	t.Helper()
	if strings.Contains(body, pluginAccountHTTPToken) || strings.Contains(body, "access_token") || strings.Contains(body, "refresh_token") || strings.Contains(body, "encrypted_secret") {
		t.Fatalf("%s leaked a secret: %s", label, body)
	}
}
