package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"xlyra/server/internal/admin"
	"xlyra/server/internal/auth"
	"xlyra/server/internal/credential"
	"xlyra/server/internal/gateway"
	"xlyra/server/internal/jsplugin"
	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

func TestPluginAccountPoolChain(t *testing.T) {
	st := openPluginProviderStore(t)
	ctx := context.Background()
	const (
		masterKey = "test-master-key"
		pluginID  = "acme.pool"
		siteType  = "acme.pool/chat"
		modelKey  = "acme.chat"
		host      = "api.example.test"
	)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	oauth := newChainOAuth()
	srv := httptest.NewServer(oauth)
	t.Cleanup(srv.Close)
	dest, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	pin := &http.Client{Transport: chainPinTransport{base: http.DefaultTransport, dest: dest}, Timeout: 20 * time.Second}

	svc := site.NewService(st, masterKey)
	siteProviderMu.Lock()
	prevCurrent := siteProviderCurrent
	siteProviderCurrent = nil
	siteProviderMu.Unlock()
	t.Cleanup(func() {
		siteProviderMu.Lock()
		siteProviderCurrent = nil
		siteProviderMu.Unlock()
		applySiteProviders(nil, svc, logger)
		site.ClearPluginSiteTypes()
		gateway.ClearRuntimeProviderSpecs()
		siteProviderMu.Lock()
		siteProviderCurrent = prevCurrent
		siteProviderMu.Unlock()
	})

	mgr := jsplugin.NewManager(st, nil)
	raw := chainPackage(t, pluginID, siteType, host)
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := jsplugin.AttachSignature(raw, priv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Upload(ctx, "", signed); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.AddTrustedKey(ctx, "", "acme", base64.StdEncoding.EncodeToString(pub)); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Enable(ctx, pluginID, "1.0.0", jsplugin.EnableOptions{ConfirmedHosts: []string{host}}); err != nil {
		t.Fatal(err)
	}
	registry := mgr.Catalog().Registry()
	siteProviderMu.Lock()
	siteProviderCurrent = func() *jsplugin.Registry { return registry }
	siteProviderMu.Unlock()
	applySiteProviders(registry, svc, logger)
	plugin, ok := registry.ByPluginID(pluginID)
	if !ok {
		t.Fatal("enabled plugin is not in the catalog")
	}
	item, ok := site.LookupPluginSiteType(siteType)
	if !ok || item.Pool == nil {
		t.Fatal("account pool site type was not published")
	}
	runtime, err := jsplugin.NewAccountProviderRuntime(plugin, pin)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPluginAccountRuntimes(map[string]site.PluginAccountRuntime{
		siteType: accountPoolRuntimeAdapter{runtime: runtime, spec: item.Pool.Clone()},
	}); err != nil {
		t.Fatal(err)
	}

	adminHandler := admin.NewHandler(logger, auth.NewService(st.DB(), masterKey), svc, nil, nil, nil, nil, nil, nil, nil, nil)
	chainAssertSiteTypeUI(t, adminHandler, oauth.secrets)

	priority := 1.0
	created, _, err := svc.Create(ctx, site.CreateSiteParams{
		Name: "Acme", Slug: "acme-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12],
		SiteType: siteType, BaseURL: "https://" + host, Enabled: true, RoutingPriority: &priority,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.BaseURL != "https://"+host {
		t.Fatalf("site base = %s", created.BaseURL)
	}
	first := chainLogin(t, adminHandler, created.ID, oauth.secrets)
	second := chainLogin(t, adminHandler, created.ID, oauth.secrets)
	chainAssertAccountFields(t, adminHandler, created.ID, oauth.secrets)
	if oauth.refreshCount() != 2 {
		t.Fatalf("login refreshes = %d, want 2", oauth.refreshCount())
	}
	high, low := 2.0, 1.0
	credRepo := store.NewSiteCredentialRepository(st.DB())
	if _, err := credRepo.UpdateRoutingConfig(ctx, first, store.UpdateSiteCredentialRoutingConfigParams{RoutingPriority: &high}); err != nil {
		t.Fatal(err)
	}
	if _, err := credRepo.UpdateRoutingConfig(ctx, second, store.UpdateSiteCredentialRoutingConfigParams{RoutingPriority: &low}); err != nil {
		t.Fatal(err)
	}

	canonical := ensureCanonical(t, st, modelKey)
	siteModel := chainAttachSiteModel(t, st, created.ID, canonical)
	for _, credentialID := range []uuid.UUID{first, second} {
		if _, err := store.NewSiteAPIKeyModelRepository(st.DB()).Upsert(ctx, store.UpsertSiteAPIKeyModelParams{
			SiteID: created.ID, SiteCredentialID: credentialID, SiteModelID: siteModel.ID,
			UpstreamModelName: modelKey, DisplayName: modelKey, Available: true, Enabled: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	apiKey, err := store.NewAPIKeyRepository(st.DB()).Create(ctx, store.CreateAPIKeyParams{
		Name: "chain", KeyHash: uuid.NewString(), Status: "active",
		ModelPolicy: "allow_all", SitePolicy: "allow_all", BillingMultiplier: 1,
		QuotaUnlimited: true, QuotaDailyUnlimited: true, QuotaWeeklyUnlimited: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	routerService := routeengine.NewService(st)
	routerService.SetSiteTypeFilter(site.SiteTypeAvailable)
	handler := gateway.NewHandler(logger, auth.NewService(st.DB(), masterKey), routerService, st, masterKey).
		WithPluginAccountRefresher(pluginAccountRefresher{sites: svc}).
		WithHTTPClient(pin)

	before := oauth.refreshCount()
	chainChat(t, handler, apiKey, modelKey, oauth.secrets)
	if oauth.refreshCount() != before || len(oauth.chats) != 1 || oauth.chats[0].pool != "acme-pool" || oauth.chats[0].auth != "Bearer "+chainPlain(t, st, masterKey, first)["access_token"] {
		t.Fatalf("first chat refreshes=%d chats=%d", oauth.refreshCount()-before, len(oauth.chats))
	}
	chainAge(t, st, masterKey, first, time.Now().Add(10*time.Second))
	chainChat(t, handler, apiKey, modelKey, oauth.secrets)
	refreshed := chainPlain(t, st, masterKey, first)
	if oauth.refreshCount() != before+1 || len(oauth.chats) != 2 || oauth.chats[1].auth != "Bearer "+refreshed["access_token"] || oauth.chats[1].pool != "acme-pool" || oauth.chats[1].auth == oauth.chats[0].auth {
		t.Fatalf("refresh chat refreshes=%d chats=%d", oauth.refreshCount()-before, len(oauth.chats))
	}
	chainAssertCiphertextHides(t, st, first, refreshed["access_token"])
	chainAge(t, st, masterKey, first, time.Now().Add(10*time.Second))
	chainChat(t, handler, apiKey, modelKey, oauth.secrets)
	fallback := chainPlain(t, st, masterKey, second)
	if oauth.refreshCount() != before+2 || len(oauth.chats) != 3 || oauth.chats[2].auth != "Bearer "+fallback["access_token"] || oauth.chats[2].pool != "acme-pool" {
		t.Fatalf("reauth chat refreshes=%d chats=%d", oauth.refreshCount()-before, len(oauth.chats))
	}
	chainAssertReauth(t, st, first, oauth.secrets)
	chainScanSecrets(t, st, logs.String(), oauth.secrets)
}

func chainAssertSiteTypeUI(t *testing.T, h admin.Handler, secrets []string) {
	t.Helper()
	rec := chainCall(t, h.ListSiteTypes, http.MethodGet, "/api/v1/site-types", "", nil, secrets)
	if rec.Code != http.StatusOK {
		t.Fatalf("site types: %d", rec.Code)
	}
	var envelope struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var sawPlugin bool
	for _, raw := range envelope.Items {
		var item struct {
			SiteType string          `json:"site_type"`
			Accounts json.RawMessage `json:"accounts"`
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			t.Fatal(err)
		}
		switch item.SiteType {
		case "grok":
			fields := map[string]json.RawMessage{}
			if err := json.Unmarshal(item.Accounts, &fields); err != nil {
				t.Fatal(err)
			}
			if _, ok := fields["ui"]; ok {
				t.Fatalf("%s accounts gained ui", item.SiteType)
			}
		case "acme.pool/chat":
			sawPlugin = true
			var accounts struct {
				UI struct {
					RowPills []struct {
						Field  string                     `json:"field"`
						Labels map[string]json.RawMessage `json:"labels"`
					} `json:"row_pills"`
					Operations []string                   `json:"operations"`
					Texts      map[string]json.RawMessage `json:"texts"`
				} `json:"ui"`
			}
			if err := json.Unmarshal(item.Accounts, &accounts); err != nil {
				t.Fatal(err)
			}
			if len(accounts.UI.RowPills) != 1 || accounts.UI.RowPills[0].Field != "expires_at" || len(accounts.UI.Operations) != 5 || accounts.UI.Texts["add"] == nil || accounts.UI.Texts["login_hint"] == nil {
				t.Fatalf("accounts.ui = %s", item.Accounts)
			}
			if strings.Contains(string(item.Accounts), "rowPills") {
				t.Fatalf("accounts.ui uses manifest names: %s", item.Accounts)
			}
		}
	}
	if !sawPlugin {
		t.Fatal("plugin site type is missing from the admin list")
	}
}

func chainLogin(t *testing.T, h admin.Handler, siteID uuid.UUID, secrets []string) uuid.UUID {
	t.Helper()
	vars := map[string]string{"siteID": siteID.String()}
	started := chainCall(t, h.StartAccountLogin, http.MethodPost, "/api/admin/sites/"+siteID.String()+"/accounts/login", `{"region":"cn"}`, vars, secrets)
	if started.Code != http.StatusOK {
		chainFail(t, "start login", started, secrets)
	}
	var start struct {
		Login struct {
			Session string `json:"session"`
		} `json:"login"`
	}
	if err := json.Unmarshal(started.Body.Bytes(), &start); err != nil {
		t.Fatal(err)
	}
	pending := chainCall(t, h.PollAccountLogin, http.MethodPost, "/api/admin/sites/"+siteID.String()+"/accounts/login/poll", `{"session":`+strconv.Quote(start.Login.Session)+`}`, vars, secrets)
	if pending.Code != http.StatusOK {
		chainFail(t, "poll login", pending, secrets)
	}
	var poll struct {
		Login struct {
			Status string `json:"status"`
		} `json:"login"`
	}
	if err := json.Unmarshal(pending.Body.Bytes(), &poll); err != nil || poll.Login.Status != "pending" {
		t.Fatalf("pending poll status = %q err=%v", poll.Login.Status, err)
	}
	done := chainCall(t, h.PollAccountLogin, http.MethodPost, "/api/admin/sites/"+siteID.String()+"/accounts/login/poll", `{"session":`+strconv.Quote(start.Login.Session)+`}`, vars, secrets)
	if done.Code != http.StatusOK {
		chainFail(t, "complete login", done, secrets)
	}
	var completed struct {
		Login struct {
			Status  string `json:"status"`
			Account struct {
				CredentialID uuid.UUID `json:"credential_id"`
				Identity     string    `json:"identity"`
			} `json:"account"`
		} `json:"login"`
	}
	if err := json.Unmarshal(done.Body.Bytes(), &completed); err != nil {
		t.Fatal(err)
	}
	if completed.Login.Status != "complete" || completed.Login.Account.CredentialID == uuid.Nil || completed.Login.Account.Identity == "" {
		t.Fatal("login did not complete")
	}
	return completed.Login.Account.CredentialID
}

func chainAssertAccountFields(t *testing.T, h admin.Handler, siteID uuid.UUID, secrets []string) {
	t.Helper()
	rec := chainCall(t, h.ListAccounts, http.MethodGet, "/api/admin/sites/"+siteID.String()+"/accounts", "", map[string]string{"siteID": siteID.String()}, secrets)
	if rec.Code != http.StatusOK {
		chainFail(t, "list accounts", rec, secrets)
	}
	var listed struct {
		Items []struct {
			Fields map[string]string `json:"fields"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Items) != 2 {
		t.Fatalf("accounts = %d", len(listed.Items))
	}
	for _, item := range listed.Items {
		if item.Fields["expires_at"] == "" {
			t.Fatal("account fields omit expires_at")
		}
		if _, ok := item.Fields["access_token"]; ok {
			t.Fatal("account fields include access_token")
		}
	}
}

func chainChat(t *testing.T, h gateway.Handler, apiKey store.APIKey, modelKey string, secrets []string) {
	t.Helper()
	body := `{"model":` + strconv.Quote(modelKey) + `,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(auth.WithAPIKey(req.Context(), apiKey))
	rec := httptest.NewRecorder()
	h.ChatCompletions(rec, req)
	if chainHasSecret(rec.Body.String(), secrets) {
		t.Fatal("chat response contains a credential secret")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("chat status %d: %s", rec.Code, rec.Body.String())
	}
}

func chainAttachSiteModel(t *testing.T, st *store.Store, siteID uuid.UUID, canonical store.CanonicalModel) store.SiteModel {
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
	return model
}

func chainAge(t *testing.T, st *store.Store, masterKey string, credentialID uuid.UUID, expires time.Time) {
	t.Helper()
	fields := chainPlain(t, st, masterKey, credentialID)
	fields["expires_at"] = strconv.FormatInt(expires.Unix(), 10)
	encoded, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	encrypted, _, err := credential.NewService(masterKey).Encrypt(string(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.NewSiteCredentialRepository(st.DB()).UpdateSecret(context.Background(), credentialID, encrypted); err != nil {
		t.Fatal(err)
	}
	chainAssertCiphertextHides(t, st, credentialID, fields["access_token"])
}

func chainPlain(t *testing.T, st *store.Store, masterKey string, credentialID uuid.UUID) map[string]string {
	t.Helper()
	row, err := store.NewSiteCredentialRepository(st.DB()).GetByID(context.Background(), credentialID)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := credential.NewService(masterKey).Decrypt(row.EncryptedSecret)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]string
	if err := json.Unmarshal([]byte(plain), &fields); err != nil {
		t.Fatal(err)
	}
	return fields
}

func chainAssertCiphertextHides(t *testing.T, st *store.Store, credentialID uuid.UUID, secret string) {
	t.Helper()
	row, err := store.NewSiteCredentialRepository(st.DB()).GetByID(context.Background(), credentialID)
	if err != nil {
		t.Fatal(err)
	}
	if secret != "" && (strings.Contains(row.EncryptedSecret, secret) || strings.Contains(string(row.Meta), secret) || strings.Contains(row.MaskedSecret, secret)) {
		t.Fatal("credential row contains a token")
	}
}

func chainAssertReauth(t *testing.T, st *store.Store, credentialID uuid.UUID, secrets []string) {
	t.Helper()
	row, err := store.NewSiteCredentialRepository(st.DB()).GetByID(context.Background(), credentialID)
	if err != nil {
		t.Fatal(err)
	}
	var meta map[string]any
	if err := json.Unmarshal(row.Meta, &meta); err != nil {
		t.Fatal(err)
	}
	if meta["reauth_required"] != true || meta["enabled"] != false {
		t.Fatal("reauth credential was not marked")
	}
	if chainHasSecret(string(row.Meta), secrets) || chainHasSecret(row.EncryptedSecret, secrets) {
		t.Fatal("reauth credential row contains a token")
	}
}

func chainScanSecrets(t *testing.T, st *store.Store, logs string, secrets []string) {
	t.Helper()
	if chainHasSecret(logs, secrets) {
		t.Fatal("gateway log contains a credential secret")
	}
	var schema string
	if err := st.DB().Raw("SELECT current_schema()").Scan(&schema).Error; err != nil {
		t.Fatal(err)
	}
	type column struct {
		Table string `gorm:"column:table_name"`
		Name  string `gorm:"column:column_name"`
		Type  string `gorm:"column:data_type"`
	}
	var columns []column
	if err := st.DB().Raw(`
		SELECT table_name, column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND data_type IN ('text', 'character varying', 'json', 'jsonb', 'bytea')
	`).Scan(&columns).Error; err != nil {
		t.Fatal(err)
	}
	for _, column := range columns {
		query := fmt.Sprintf(`SELECT %s FROM %s.%s`, chainQuote(column.Name), chainQuote(schema), chainQuote(column.Table))
		if column.Type == "bytea" {
			query = fmt.Sprintf(`SELECT encode(%s, 'escape') FROM %s.%s`, chainQuote(column.Name), chainQuote(schema), chainQuote(column.Table))
		}
		var values []sql.NullString
		if err := st.DB().Raw(query).Scan(&values).Error; err != nil {
			t.Fatalf("scan %s.%s: %v", column.Table, column.Name, err)
		}
		for _, value := range values {
			if value.Valid && chainHasSecret(value.String, secrets) {
				t.Fatalf("%s.%s contains a credential secret", column.Table, column.Name)
			}
		}
	}
}

func chainQuote(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func chainCall(t *testing.T, handler func(http.ResponseWriter, *http.Request), method, target, body string, vars map[string]string, secrets []string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	if vars != nil {
		routeCtx := chi.NewRouteContext()
		for key, value := range vars {
			routeCtx.URLParams.Add(key, value)
		}
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	if chainHasSecret(rec.Body.String(), secrets) {
		t.Fatalf("%s %s response contains a credential secret", method, target)
	}
	return rec
}

func chainFail(t *testing.T, label string, rec *httptest.ResponseRecorder, secrets []string) {
	t.Helper()
	if chainHasSecret(rec.Body.String(), secrets) {
		t.Fatalf("%s status %d body contains a credential secret", label, rec.Code)
	}
	t.Fatalf("%s status %d: %s", label, rec.Code, rec.Body.String())
}

func chainHasSecret(text string, secrets []string) bool {
	for _, secret := range secrets {
		if secret != "" && strings.Contains(text, secret) {
			return true
		}
	}
	return false
}

func chainPackage(t *testing.T, pluginID, siteType, host string) []byte {
	t.Helper()
	source := "export const meta = { apiVersion: 1, id: " + strconv.Quote(pluginID) + ", kind: \"site_provider\" };\n" +
		"export function summarizeAccount() { return { result: { kind: \"account_summary\", status: \"ok\", remark: \"pro\", entries: [{ label: \"credits\", remaining: 12, limit: 20, unit: \"credits\" }] } }; }\n"
	lead := 300
	manifest := jsplugin.Manifest{
		ID: pluginID, Name: "Acme Pool", Version: "1.0.0", APIVersion: 1, HostAPI: 1, Kind: jsplugin.KindSiteProvider,
		SiteProvider: jsplugin.SiteProviderSection{
			SiteType: siteType,
			Display:  jsplugin.SiteProviderDisplay{Name: jsplugin.LocalizedText{"zh": "账号池", "en": "Pool"}},
			Hosts:    []string{host},
			Credential: jsplugin.SiteProviderCredential{
				Model: "account_pool",
				Fields: []jsplugin.SiteProviderField{
					{Name: "access_token", Secret: true},
					{Name: "refresh_token", Secret: true},
					{Name: "expires_at", Type: "unix"},
				},
				AccessTokenField: "access_token", RefreshTokenField: "refresh_token", ExpiresAtField: "expires_at",
				Identity: jsplugin.Sources("claims.sub"),
			},
			Protocol: jsplugin.SiteProviderProtocol{
				Kind: "openai_chat", Path: "/v1/chat/completions", OfficialBaseURL: "https://" + host,
				DefaultEndpointTypes: []string{"openai"},
			},
			Regions:       []jsplugin.SiteProviderRegion{{ID: "cn", Label: jsplugin.LocalizedText{"zh": "中国", "en": "China"}, BaseURL: "https://" + host}},
			DefaultRegion: "cn",
			Login: &jsplugin.SiteProviderLogin{
				Kind: "oauth2_device_code", DeviceCodeURL: "https://" + host + "/oauth/device",
				TokenURL: "https://" + host + "/oauth/token", ClientID: "acme-pool",
			},
			Refresh: &jsplugin.SiteProviderRefresh{Kind: "oauth2_refresh", LeadSeconds: &lead, TokenURL: "https://" + host + "/oauth/token"},
			Request: jsplugin.SiteProviderRequest{
				Headers: jsplugin.HeaderMap(map[string]string{
					"Authorization": "Bearer {{cred.access_token}}",
					"X-Pool":        "acme-pool",
				}),
				StripDownstreamHeaders: true,
				BaseURLFrom:            "region",
			},
			UI: &jsplugin.SiteProviderUI{
				RowPills:   []jsplugin.SiteProviderRowPill{{Field: "expires_at"}},
				Operations: []string{"add", "reauth", "refresh", "delete", "toggle"},
				Texts: map[string]jsplugin.LocalizedTextOrString{
					"add":        {"": "登录"},
					"login_hint": {"zh": "用设备码", "en": "Use a device code", "jp": "デバイスコード"},
				},
			},
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

type chainPinTransport struct {
	base http.RoundTripper
	dest *url.URL
}

func (p chainPinTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = p.dest.Scheme
	clone.URL.Host = p.dest.Host
	clone.Host = p.dest.Host
	base := p.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(clone)
}

type chainGrant struct {
	access  string
	refresh string
	dead    bool
}

type chainUpstreamChat struct {
	auth string
	pool string
}

type chainOAuth struct {
	mu        sync.Mutex
	devices   []chainGrant
	deviceN   int
	pending   map[string]chainGrant
	polls     map[string]int
	next      map[string]chainGrant
	secrets   []string
	refreshes int
	chats     []chainUpstreamChat
}

func newChainOAuth() *chainOAuth {
	o := &chainOAuth{pending: map[string]chainGrant{}, polls: map[string]int{}, next: map[string]chainGrant{}}
	access, refresh := o.pair("user-1")
	nextAccess, nextRefresh := o.pair("user-1")
	finalAccess, finalRefresh := o.pair("user-1")
	o.devices = append(o.devices, chainGrant{access: access, refresh: refresh})
	o.next[refresh] = chainGrant{access: nextAccess, refresh: nextRefresh}
	o.next[nextRefresh] = chainGrant{access: finalAccess, refresh: finalRefresh}
	o.next[finalRefresh] = chainGrant{dead: true}
	second, secondRefresh := o.pair("user-2")
	stored, storedRefresh := o.pair("user-2")
	o.devices = append(o.devices, chainGrant{access: second, refresh: secondRefresh})
	o.next[secondRefresh] = chainGrant{access: stored, refresh: storedRefresh}
	return o
}

func (o *chainOAuth) pair(sub string) (string, string) {
	access := chainJWT(sub)
	refresh := "rtk-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	o.secrets = append(o.secrets, access, refresh)
	return access, refresh
}

func (o *chainOAuth) refreshCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.refreshes
}

func (o *chainOAuth) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/oauth/device":
		o.serveDevice(w, r)
	case "/oauth/token":
		o.serveToken(w, r)
	case "/v1/chat/completions":
		o.serveChat(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (o *chainOAuth) serveDevice(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return
	}
	o.mu.Lock()
	if o.deviceN >= len(o.devices) {
		o.mu.Unlock()
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return
	}
	grant := o.devices[o.deviceN]
	code := fmt.Sprintf("dev-%d", o.deviceN)
	o.pending[code] = grant
	o.deviceN++
	o.mu.Unlock()
	_ = grant
	writeJSON(w, http.StatusOK, map[string]any{
		"device_code": code, "user_code": "WDJB-MJHT", "verification_uri": "https://api.example.test/activate",
		"expires_in": 600, "interval": 1,
	})
}

func (o *chainOAuth) serveToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return
	}
	if r.Form.Get("grant_type") == "refresh_token" {
		o.mu.Lock()
		o.refreshes++
		step, ok := o.next[r.Form.Get("refresh_token")]
		o.mu.Unlock()
		if !ok || step.dead {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"access_token": step.access, "refresh_token": step.refresh, "token_type": "Bearer", "expires_in": 7200,
		})
		return
	}
	code := r.Form.Get("device_code")
	o.mu.Lock()
	o.polls[code]++
	n := o.polls[code]
	grant := o.pending[code]
	o.mu.Unlock()
	if n == 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "authorization_pending"})
		return
	}
	if grant.access == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expired_token"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": grant.access, "refresh_token": grant.refresh, "token_type": "Bearer", "expires_in": 7200,
	})
}

func (o *chainOAuth) serveChat(w http.ResponseWriter, r *http.Request) {
	o.mu.Lock()
	o.chats = append(o.chats, chainUpstreamChat{auth: r.Header.Get("Authorization"), pool: r.Header.Get("X-Pool")})
	o.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"id":"chatcmpl-chain","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func chainJWT(sub string) string {
	enc := base64.RawURLEncoding.EncodeToString
	payload := fmt.Sprintf(`{"sub":%q,"jti":%q}`, sub, uuid.NewString())
	return enc([]byte(`{"alg":"none"}`)) + "." + enc([]byte(payload)) + ".sig"
}
